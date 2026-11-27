package devlogin

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// bbux had no tests for its devlogin.go; these cover every guard plus the
// handler contract its web DevSignIn relies on.

var seeded = map[string]User{
	"operator@bbux.dev": {DisplayName: "Operator"},
	"member@bbux.dev":   {DisplayName: ""}, // falls back to the email
}

func lookup(_ context.Context, email string) (User, bool, error) {
	u, ok := seeded[email]
	return u, ok, nil
}

type signInRecorder struct{ emails []string }

func (s *signInRecorder) fn(w http.ResponseWriter, _ *http.Request, email string) error {
	s.emails = append(s.emails, email)
	w.Header().Set("St-Access-Token", "minted")
	return nil
}

func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func newHandlers(t *testing.T, e map[string]string, rec *signInRecorder) *Handlers {
	t.Helper()
	h, err := New(Config{Lookup: lookup, SignIn: rec.fn, Getenv: env(e),
		DefaultEmails: []string{"operator@bbux.dev", "member@bbux.dev"}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return h
}

func TestGuards(t *testing.T) {
	rec := &signInRecorder{}
	cases := []struct {
		name     string
		env      map[string]string
		defaults []string
		want     error
	}{
		{"flag unset", nil, []string{"a@x"}, ErrDisabled},
		{"flag not exactly true", map[string]string{"DEV_LOGIN": "1"}, []string{"a@x"}, ErrDisabled},
		{"on Fly", map[string]string{"DEV_LOGIN": "true", "FLY_APP_NAME": "bbux-cms"}, []string{"a@x"}, ErrDisabled},
		{"empty allow-list", map[string]string{"DEV_LOGIN": "true", "DEV_LOGIN_EMAILS": " , "}, nil, ErrNoEmails},
		{"all guards pass", map[string]string{"DEV_LOGIN": "true"}, []string{"a@x"}, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h, err := New(Config{Lookup: lookup, SignIn: rec.fn, Getenv: env(c.env), DefaultEmails: c.defaults})
			if !errors.Is(err, c.want) {
				t.Fatalf("err = %v, want %v", err, c.want)
			}
			if (h != nil) != (c.want == nil) {
				t.Fatalf("handlers = %v with err %v", h, err)
			}
		})
	}
	if _, err := New(Config{Getenv: env(map[string]string{"DEV_LOGIN": "true"})}); err == nil {
		t.Fatal("missing Lookup/SignIn must error")
	}
}

func TestAllowListEnvOverridesDefaults(t *testing.T) {
	got := AllowList(env(map[string]string{"DEV_LOGIN_EMAILS": " A@X.com ,, b@y "}), []string{"d@z"})
	if strings.Join(got, ",") != "a@x.com,b@y" {
		t.Fatalf("AllowList = %v", got)
	}
	if got := AllowList(env(nil), []string{" D@Z "}); strings.Join(got, ",") != "d@z" {
		t.Fatalf("defaults = %v", got)
	}
}

func post(h http.HandlerFunc, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h(rec, httptest.NewRequest(http.MethodPost, LoginPath, strings.NewReader(body)))
	return rec
}

func code(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var b map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &b)
	c, _ := b["code"].(string)
	return c
}

func TestLoginNotOnAllowList(t *testing.T) {
	rec := &signInRecorder{}
	h := newHandlers(t, map[string]string{"DEV_LOGIN": "true", "DEV_LOGIN_EMAILS": "member@bbux.dev"}, rec)
	// operator exists but isn't allow-listed (env list replaces defaults).
	r := post(h.Login, `{"email":"operator@bbux.dev"}`)
	if r.Code != 403 || code(t, r) != "DEV_LOGIN_NOT_ALLOWED" || len(rec.emails) != 0 {
		t.Fatalf("= %d %s, signIns=%v", r.Code, r.Body, rec.emails)
	}
}

func TestLoginAllowListedButNoUser(t *testing.T) {
	rec := &signInRecorder{}
	h := newHandlers(t, map[string]string{"DEV_LOGIN": "true", "DEV_LOGIN_EMAILS": "ghost@bbux.dev"}, rec)
	r := post(h.Login, `{"email":"ghost@bbux.dev"}`)
	if r.Code != 404 || code(t, r) != "DEV_LOGIN_NO_USER" || len(rec.emails) != 0 {
		t.Fatalf("= %d %s, signIns=%v", r.Code, r.Body, rec.emails)
	}
}

func TestLoginMintsSession(t *testing.T) {
	rec := &signInRecorder{}
	h := newHandlers(t, map[string]string{"DEV_LOGIN": "true"}, rec)
	r := post(h.Login, `{"email":"  Member@BBUX.dev "}`)
	if r.Code != 200 || r.Header().Get("St-Access-Token") != "minted" {
		t.Fatalf("= %d %s", r.Code, r.Body)
	}
	if strings.Join(rec.emails, ",") != "member@bbux.dev" {
		t.Fatalf("signed in %v, want normalised member email", rec.emails)
	}
}

func TestLoginEmptyBodyUsesFirstAllowListed(t *testing.T) {
	rec := &signInRecorder{}
	h := newHandlers(t, map[string]string{"DEV_LOGIN": "true"}, rec)
	if r := post(h.Login, ``); r.Code != 200 || rec.emails[0] != "operator@bbux.dev" {
		t.Fatalf("= %d %s, signIns=%v", r.Code, r.Body, rec.emails)
	}
}

func TestLoginErrorsAre500(t *testing.T) {
	failing := func(context.Context, string) (User, bool, error) { return User{}, false, errors.New("db down") }
	h, _ := New(Config{Lookup: failing, SignIn: (&signInRecorder{}).fn,
		Getenv: env(map[string]string{"DEV_LOGIN": "true", "DEV_LOGIN_EMAILS": "a@x"})})
	if r := post(h.Login, `{"email":"a@x"}`); r.Code != 500 {
		t.Fatalf("lookup error = %d, want 500", r.Code)
	}
	h, _ = New(Config{Lookup: lookup,
		SignIn: func(http.ResponseWriter, *http.Request, string) error { return errors.New("st down") },
		Getenv: env(map[string]string{"DEV_LOGIN": "true", "DEV_LOGIN_EMAILS": "operator@bbux.dev"})})
	if r := post(h.Login, `{}`); r.Code != 500 {
		t.Fatalf("sign-in error = %d, want 500", r.Code)
	}
}

func TestUsersListsOnlyExisting(t *testing.T) {
	h := newHandlers(t, map[string]string{"DEV_LOGIN": "true",
		"DEV_LOGIN_EMAILS": "operator@bbux.dev,ghost@bbux.dev,member@bbux.dev"}, &signInRecorder{})
	rec := httptest.NewRecorder()
	h.Users(rec, httptest.NewRequest(http.MethodGet, UsersPath, nil))
	var body struct{ Users []User }
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	want := []User{{"operator@bbux.dev", "Operator"}, {"member@bbux.dev", "member@bbux.dev"}}
	if rec.Code != 200 || len(body.Users) != 2 || body.Users[0] != want[0] || body.Users[1] != want[1] {
		t.Fatalf("= %d %+v, want %+v", rec.Code, body.Users, want)
	}
}

func TestRegisterMountsMethodScopedRoutes(t *testing.T) {
	h := newHandlers(t, map[string]string{"DEV_LOGIN": "true"}, &signInRecorder{})
	mux := http.NewServeMux()
	h.Register(mux)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, LoginPath, nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET %s = %d, want 405", LoginPath, rec.Code)
	}
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, UsersPath, nil))
	if rec.Code != 200 {
		t.Fatalf("GET %s = %d", UsersPath, rec.Code)
	}
}
