package seed

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func empty(v bool) func(context.Context) (bool, error) {
	return func(context.Context) (bool, error) { return v, nil }
}

func TestDecide(t *testing.T) {
	cases := []struct {
		name    string
		env     map[string]string
		isEmpty func(context.Context) (bool, error)
		want    bool
	}{
		{"staging + empty", map[string]string{"BBUX_ENV": "staging"}, empty(true), true},
		{"staging + has data", map[string]string{"BBUX_ENV": "staging"}, empty(false), false},
		{"local dev (unset) + empty", nil, empty(true), true},
		{"dev + empty", map[string]string{"BBUX_ENV": "dev"}, empty(true), true},
		{"prod + empty never seeds", map[string]string{"BBUX_ENV": "prod"}, empty(true), false},
		{"unknown env fails safe", map[string]string{"BBUX_ENV": "qa"}, empty(true), false},
		{"SEED_DATA=false beats staging", map[string]string{"BBUX_ENV": "staging", "SEED_DATA": "false"}, empty(true), false},
		{"SEED_DATA=FALSE case-insensitive", map[string]string{"SEED_DATA": "FALSE"}, nil, false},
		{"no IsEmpty = always (bbux idempotent seed)", nil, nil, true},
		{"bbux prod: SEED_DATA=false, no BBUX_ENV", map[string]string{"SEED_DATA": "false"}, nil, false},
		{"staging, no IsEmpty", map[string]string{"BBUX_ENV": "staging"}, nil, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d, err := Decide(context.Background(), Options{IsEmpty: c.isEmpty, Getenv: env(c.env)})
			if err != nil {
				t.Fatal(err)
			}
			if d.Run != c.want {
				t.Fatalf("Run = %v (%s), want %v", d.Run, d.Reason, c.want)
			}
			if d.Reason == "" {
				t.Fatal("empty reason")
			}
		})
	}
}

func TestEmptinessCheckNotCalledWhenEnvForbids(t *testing.T) {
	called := false
	_, _ = Decide(context.Background(), Options{
		Getenv:  env(map[string]string{"BBUX_ENV": "prod"}),
		IsEmpty: func(context.Context) (bool, error) { called = true; return true, nil },
	})
	if called {
		t.Fatal("IsEmpty must not touch the DB in prod")
	}
}

func TestMaybeRunsAndPropagates(t *testing.T) {
	ctx := context.Background()
	ran := 0
	fn := func(context.Context) error { ran++; return nil }

	if d, err := Maybe(ctx, Options{Getenv: env(nil)}, fn); err != nil || !d.Run || ran != 1 {
		t.Fatalf("Maybe = (%+v, %v), ran=%d", d, err, ran)
	}
	if d, err := Maybe(ctx, Options{Getenv: env(map[string]string{"SEED_DATA": "false"})}, fn); err != nil || d.Run || ran != 1 {
		t.Fatalf("opt-out still ran: (%+v, %v), ran=%d", d, err, ran)
	}
	boom := errors.New("boom")
	if _, err := Maybe(ctx, Options{Getenv: env(nil)}, func(context.Context) error { return boom }); !errors.Is(err, boom) {
		t.Fatalf("err = %v, want wrapped boom", err)
	}
	if _, err := Maybe(ctx, Options{Getenv: env(nil), IsEmpty: func(context.Context) (bool, error) { return false, boom }}, fn); !errors.Is(err, boom) {
		t.Fatalf("emptiness err = %v, want wrapped boom", err)
	}
}

func TestEmptyWhenNoRows(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set — run `make test-go-integration`")
	}
	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1) // TEMP tables are per-session: keep one connection
	ctx := context.Background()
	if _, err := db.Exec(`CREATE TEMP TABLE IF NOT EXISTS seed_probe (id int)`); err != nil {
		t.Fatal(err)
	}
	check := EmptyWhenNoRows(db, `SELECT 1 FROM seed_probe LIMIT 1`)
	if e, err := check(ctx); err != nil || !e {
		t.Fatalf("empty table = (%v, %v), want (true, nil)", e, err)
	}
	if _, err := db.Exec(`INSERT INTO seed_probe VALUES (1)`); err != nil {
		t.Fatal(err)
	}
	if e, err := check(ctx); err != nil || e {
		t.Fatalf("non-empty table = (%v, %v), want (false, nil)", e, err)
	}
}
