// Package devlogin is the LOCAL-ONLY "Continue as <seeded user>" sign-in,
// extracted from bbux apps/api/internal/auth/devlogin.go.
//
// Magic-link auth has no mail provider locally, so a newcomer would have to
// fish the link out of the API logs. This skips only the email round-trip. It
// is deliberately NOT a no-login mode: the login screen still stands, and the
// session minted is a REAL session for a REAL, already-existing user, so every
// permission check behaves exactly as after a magic-link sign-in.
//
// Every guard must pass; New refuses to build handlers otherwise:
//
//	DEV_LOGIN == "true" ?          no  ─▶ ErrDisabled (default off)
//	FLY_APP_NAME unset ?           no  ─▶ ErrDisabled + error log (flag leaked into a deploy)
//	allow-list non-empty ?         no  ─▶ ErrNoEmails
//	          │
//	          ▼  per request (POST /auth/dev-login {"email": "…"})
//	email on allow-list ?          no  ─▶ 403 DEV_LOGIN_NOT_ALLOWED
//	Lookup(email) finds a user ?   no  ─▶ 404 DEV_LOGIN_NO_USER   (can't conjure an account)
//	SignIn(w, r, email)            err ─▶ 500 INTERNAL
//	                               ok  ─▶ 200 {"ok":true,"email":…}
//
// The allow-list is DEV_LOGIN_EMAILS (comma-separated) or, when unset,
// Config.DefaultEmails. GET /auth/dev-login/users lists the allow-listed
// emails that exist, for the login screen's buttons.
//
// Lookup and SignIn are injected so apps with their own user tables can use
// it; SuperTokensSignIn adapts SuperTokens passwordless for SignIn.
package devlogin

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"strings"
)

// Env var names the guards read.
const (
	EnvFlag   = "DEV_LOGIN"
	EnvEmails = "DEV_LOGIN_EMAILS"
	EnvFlyApp = "FLY_APP_NAME"
)

// Default mount paths (what bbux's web DevSignIn calls).
const (
	UsersPath = "/auth/dev-login/users"
	LoginPath = "/auth/dev-login"
)

var (
	// ErrDisabled: DEV_LOGIN isn't "true", or the process is on Fly.
	ErrDisabled = errors.New("devlogin: disabled (needs DEV_LOGIN=true and FLY_APP_NAME unset)")
	// ErrNoEmails: neither DEV_LOGIN_EMAILS nor Config.DefaultEmails name anyone.
	ErrNoEmails = errors.New("devlogin: allow-list is empty (set DEV_LOGIN_EMAILS)")
)

// User is one entry in the sign-in picker.
type User struct {
	Email       string `json:"email"`
	DisplayName string `json:"displayName"`
}

// LookupFunc finds an existing user by (lower-cased) email. found=false means
// no such user; an error is a real failure (DB down).
type LookupFunc func(ctx context.Context, email string) (u User, found bool, err error)

// SignInFunc mints a real session for email on the response (cookies/headers).
type SignInFunc func(w http.ResponseWriter, r *http.Request, email string) error

// Config wires the app-specific parts.
type Config struct {
	Lookup LookupFunc // required
	SignIn SignInFunc // required
	// DefaultEmails is the allow-list used when DEV_LOGIN_EMAILS is unset
	// (bbux: its seeded operator + member).
	DefaultEmails []string
	// Getenv reads environment variables. Nil → os.Getenv.
	Getenv func(string) string
	// Logger. Nil → slog.Default().
	Logger *slog.Logger
}

// Handlers are the two endpoints, built only when every guard passes.
type Handlers struct {
	Users  http.HandlerFunc // GET  UsersPath
	Login  http.HandlerFunc // POST LoginPath
	Emails []string         // the effective (lower-cased) allow-list
}

// Enabled reports whether the environment allows dev login: DEV_LOGIN=true
// AND not on Fly. It logs an error when the flag is set on Fly.
func Enabled(getenv func(string) string, log *slog.Logger) bool {
	if getenv == nil {
		getenv = os.Getenv
	}
	if log == nil {
		log = slog.Default()
	}
	flag := getenv(EnvFlag) == "true"
	onFly := getenv(EnvFlyApp) != ""
	if flag && onFly {
		log.Error("DEV_LOGIN is set in a deployed environment — IGNORING it; remove the secret")
	}
	return flag && !onFly
}

// AllowList returns DEV_LOGIN_EMAILS (comma-separated) or, when that is empty,
// defaults — trimmed, lower-cased, blanks dropped.
func AllowList(getenv func(string) string, defaults []string) []string {
	if getenv == nil {
		getenv = os.Getenv
	}
	list := normalize(strings.Split(getenv(EnvEmails), ","))
	if len(list) == 0 {
		list = normalize(defaults)
	}
	return list
}

// New checks every guard and returns the handlers, or ErrDisabled /
// ErrNoEmails. Apps mount the handlers only when err == nil:
//
//	if h, err := devlogin.New(cfg); err == nil { h.Register(mux) }
func New(cfg Config) (*Handlers, error) {
	if cfg.Lookup == nil || cfg.SignIn == nil {
		return nil, errors.New("devlogin: Config.Lookup and Config.SignIn are required")
	}
	log := cfg.Logger
	if log == nil {
		log = slog.Default()
	}
	if !Enabled(cfg.Getenv, log) {
		return nil, ErrDisabled
	}
	allowed := AllowList(cfg.Getenv, cfg.DefaultEmails)
	if len(allowed) == 0 {
		return nil, ErrNoEmails
	}
	log.Warn("DEV LOGIN ENABLED — passwordless sign-in for seeded users; never set DEV_LOGIN in prod",
		"emails", allowed)
	return &Handlers{
		Users:  usersHandler(cfg.Lookup, allowed),
		Login:  loginHandler(cfg.Lookup, cfg.SignIn, allowed, log),
		Emails: allowed,
	}, nil
}

// Register mounts both handlers on a std mux at the default paths.
func (h *Handlers) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET "+UsersPath, h.Users)
	mux.HandleFunc("POST "+LoginPath, h.Login)
}

// usersHandler lists the allow-listed users that exist. A missing user (or a
// lookup error) just omits that entry — a misconfigured list degrades to "no
// dev users", never a 500.
func usersHandler(lookup LookupFunc, allowed []string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		users := make([]User, 0, len(allowed))
		for _, email := range allowed {
			u, found, err := lookup(r.Context(), email)
			if err != nil || !found {
				continue
			}
			u.Email = email
			if u.DisplayName == "" {
				u.DisplayName = email
			}
			users = append(users, u)
		}
		writeJSON(w, http.StatusOK, map[string]any{"users": users})
	}
}

// loginHandler mints a session for an allow-listed existing user.
// POST {"email":"…"}; an empty/missing body uses the first allow-listed email.
func loginHandler(lookup LookupFunc, signIn SignInFunc, allowed []string, log *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Email string `json:"email"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body) // missing/invalid body = default user

		email := strings.ToLower(strings.TrimSpace(body.Email))
		if email == "" {
			email = allowed[0]
		}
		// Local-only endpoint: no anti-enumeration concern, so errors say what's wrong.
		if !contains(allowed, email) {
			writeJSON(w, http.StatusForbidden, map[string]any{
				"code": "DEV_LOGIN_NOT_ALLOWED", "message": "email is not in DEV_LOGIN_EMAILS",
			})
			return
		}
		_, found, err := lookup(r.Context(), email)
		if err != nil {
			log.Error("dev login: user lookup failed", "email", email, "err", err)
			writeJSON(w, http.StatusInternalServerError, map[string]any{"code": "INTERNAL"})
			return
		}
		if !found {
			writeJSON(w, http.StatusNotFound, map[string]any{
				"code": "DEV_LOGIN_NO_USER", "message": "no user with that email — is seed data on?",
			})
			return
		}
		if err := signIn(w, r, email); err != nil {
			log.Error("dev login: sign-in failed", "email", email, "err", err)
			writeJSON(w, http.StatusInternalServerError, map[string]any{"code": "INTERNAL"})
			return
		}
		log.Warn("DEV LOGIN — session minted without a magic link", "email", email)
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "email": email})
	}
}

func normalize(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s = strings.ToLower(strings.TrimSpace(s)); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
