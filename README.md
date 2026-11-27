# bbux-shared

Reusable React + Go building blocks: a floating-panel modal, UI primitives
(Kbd, Tooltip), hooks, list filter/table-setting types, an in-memory row
filter/sort engine, and the Go runtime plumbing every bbux app shares
(migrations, seed gate, /health, dev sign-in, secret sealing). Public, so any consumer's Docker/CI can install it
anonymously (no token/git auth).

## Layout

```
packages/
  ts/
    ui/      @bbux/ui    — ModalProvider/ModalPanel/useModals + Kbd, Tooltip, cn (+ @bbux/ui/modal.css)
    hooks/   @bbux/hooks — useIsMobile
    types/   @bbux/types — list filter + table-settings types/helpers
  go/
    filters/   — Operator semantics + in-memory FilterRows / SortRows
    auth/      — SuperTokens passwordless (magic link + OTP) wiring
    migrate/   — numbered SQL runner + schema_migrations, baseline adoption
    seed/      — "seed when staging/local dev + empty" gate
    health/    — /health: 503 until MarkReady, then 200 (+ optional checks)
    devlogin/  — local-only "Continue as <seeded user>" sign-in
    secretbox/ — AES-256-GCM seal/open with a SHA-256-derived key
```

Each `packages/go/<name>` is its **own Go module**
(`github.com/matthewblackburn/bbux-shared/packages/go/<name>`); consumers pin
each one by commit pseudo-version.

## Install

```jsonc
// package.json
"@bbux/ui":    "github:matthewblackburn/bbux-shared#path:packages/ts/ui",
"@bbux/hooks": "github:matthewblackburn/bbux-shared#path:packages/ts/hooks",
"@bbux/types": "github:matthewblackburn/bbux-shared#path:packages/ts/types"
```

```bash
go get github.com/matthewblackburn/bbux-shared/packages/go/<name>@<commit>
```

## Go packages at a glance

```go
// migrate: apply infra/migrations/*.sql once each (pgx pool: stdlib.OpenDBFromPool(pool))
res, err := migrate.Up(ctx, db, os.DirFS("infra/migrations"), migrate.Options{Baseline: "0001_baseline.sql"})
version, err := migrate.LatestVersion(ctx, db) // "0007_add_x.sql"

// seed: run the app's seed only on staging/local dev, when empty, unless SEED_DATA=false
_, err = seed.Maybe(ctx, seed.Options{IsEmpty: seed.EmptyWhenNoRows(db, "SELECT 1 FROM users LIMIT 1")}, mySeed)

// health: 503 until ready
h := health.New(health.WithCheck("db", db.PingContext)); mux.Handle("/health", h); /* migrate… */ h.MarkReady()

// devlogin: only built when DEV_LOGIN=true, not on Fly, and the allow-list is set
if dl, err := devlogin.New(devlogin.Config{Lookup: findUser, SignIn: devlogin.SuperTokensSignIn(linkUser)}); err == nil { dl.Register(mux) }

// secretbox: seal secrets at rest
key := secretbox.DeriveKey(os.Getenv("DEPLOY_KEY_SECRET")); sealed, err := secretbox.Encrypt(key, token)
```

Go targets: `make test-go` (pure tests; DB tests skip), `make test-go-integration`
(all tests vs a throwaway Postgres on :5499), `make lint-go`, `make tidy-go`.
`MOD=<name>` narrows to one module, `RUN=<regex>` to some tests, `V=1` for verbose.

The TS packages ship raw source consumed by the consumer's Vite/esbuild (no build step).

## Examples playground

A standalone Vite app under [`examples/`](./examples) renders the real shared
components (`EntityTable`, `ListHeader`, `FilterChips`) against sample data so
you can eyeball them — selection highlight, keyboard nav, sort, grouping, the
row "⋯" menu — without either consuming app.

```bash
make examples   # installs the workspace + opens http://localhost:5199
```

Other targets: `make build` (verify it compiles), `make help`, `make clean`, and the Go targets above.

The packages are linked locally via the pnpm workspace, so edits to
`packages/ts/*` are reflected live.
