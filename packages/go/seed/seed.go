// Package seed decides WHEN an app seeds itself with fake data; the app
// supplies WHAT (its own seed function) and what "empty" means for it.
//
//	Maybe(ctx, opts, seedFn)
//	  │
//	  ├─ SEED_DATA=false?                 ─▶ skip  (explicit opt-out; bbux prod sets it)
//	  ├─ BBUX_ENV
//	  │    "staging"                      ─▶ allowed
//	  │    "" | "dev" | "local" | "development"
//	  │                                   ─▶ allowed (local dev; nothing sets BBUX_ENV)
//	  │    "prod" | anything else         ─▶ skip  (fail safe: prod never gets fake data)
//	  ├─ opts.IsEmpty == nil              ─▶ run   (seed is idempotent, e.g. bbux's
//	  │                                             ON CONFLICT DO NOTHING fixtures)
//	  ├─ opts.IsEmpty() == false          ─▶ skip  (already has data)
//	  └─ otherwise                        ─▶ run seedFn
//
// Call it after migrations and before marking /health ready.
//
// bbux's current rule ("seed every boot unless SEED_DATA=false") is
// Maybe(ctx, seed.Options{}, bbuxSeedAll): no IsEmpty, and BBUX_ENV is unset
// locally and in its prod (where SEED_DATA=false already opts out).
package seed

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"strings"
)

// Env var names the gate reads.
const (
	EnvBbuxEnv  = "BBUX_ENV"
	EnvSeedData = "SEED_DATA"
)

// Options tunes the gate. The zero value is valid.
type Options struct {
	// IsEmpty reports whether the database counts as empty (no real data yet).
	// Nil means "always seed when the environment allows" — right for
	// idempotent seeds.
	IsEmpty func(ctx context.Context) (bool, error)
	// Getenv reads environment variables. Nil → os.Getenv.
	Getenv func(string) string
	// Logger receives the decision. Nil → slog.Default().
	Logger *slog.Logger
}

// Decision is the gate's verdict with a human-readable reason for the logs.
type Decision struct {
	Run    bool
	Reason string
}

// Decide evaluates the gate without running anything.
func Decide(ctx context.Context, opts Options) (Decision, error) {
	getenv := opts.Getenv
	if getenv == nil {
		getenv = os.Getenv
	}
	if strings.EqualFold(strings.TrimSpace(getenv(EnvSeedData)), "false") {
		return Decision{false, "SEED_DATA=false"}, nil
	}
	env := strings.ToLower(strings.TrimSpace(getenv(EnvBbuxEnv)))
	switch env {
	case "staging", "", "dev", "local", "development":
	default:
		return Decision{false, fmt.Sprintf("BBUX_ENV=%s never seeds", env)}, nil
	}
	if opts.IsEmpty == nil {
		return Decision{true, "environment allows seeding (" + envLabel(env) + ")"}, nil
	}
	empty, err := opts.IsEmpty(ctx)
	if err != nil {
		return Decision{}, fmt.Errorf("seed: emptiness check: %w", err)
	}
	if !empty {
		return Decision{false, "database already has data"}, nil
	}
	return Decision{true, "empty database (" + envLabel(env) + ")"}, nil
}

// Maybe runs seedFn when Decide says so, and returns the decision.
func Maybe(ctx context.Context, opts Options, seedFn func(ctx context.Context) error) (Decision, error) {
	log := opts.Logger
	if log == nil {
		log = slog.Default()
	}
	d, err := Decide(ctx, opts)
	if err != nil {
		return d, err
	}
	if !d.Run {
		log.Info("seed skipped", "reason", d.Reason)
		return d, nil
	}
	if err := seedFn(ctx); err != nil {
		return d, fmt.Errorf("seed: %w", err)
	}
	log.Info("seed data applied", "reason", d.Reason)
	return d, nil
}

// EmptyWhenNoRows builds an IsEmpty check from a query: the database is empty
// when the query returns no rows, e.g. "SELECT 1 FROM users LIMIT 1".
// The query is app-authored SQL — never build it from user input.
func EmptyWhenNoRows(db *sql.DB, query string) func(ctx context.Context) (bool, error) {
	return func(ctx context.Context) (bool, error) {
		rows, err := db.QueryContext(ctx, query)
		if err != nil {
			return false, err
		}
		defer rows.Close()
		has := rows.Next()
		return !has, rows.Err()
	}
}

func envLabel(env string) string {
	if env == "" {
		return "BBUX_ENV unset: local dev"
	}
	return "BBUX_ENV=" + env
}
