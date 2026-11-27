// Package migrate is the gold-standard schema runner: numbered plain-SQL files
// (0001_init.sql, 0002_….sql) applied once each, in lexical order, and recorded
// in schema_migrations(filename). Extracted from bbux internal/db; the table
// shape and lock key are unchanged, so a bbux database already migrated by the
// old runner is a no-op here.
//
//	Up(ctx, db, fsys, opts)
//	  │
//	  ├─ pg_advisory_lock(LockKey)          one migrator at a time (many machines
//	  │                                     booting, parallel tests sharing a DB)
//	  ├─ schema_migrations exists?
//	  │    no ── opts.Baseline set AND the schema already has tables?
//	  │    │       yes ─▶ ADOPT: create schema_migrations and record every file
//	  │    │              <= Baseline as applied WITHOUT running it (one tx)
//	  │    │       no  ─▶ create schema_migrations (empty)
//	  │    yes ─▶ continue
//	  ├─ for each *.sql file not yet recorded, in lexical order:
//	  │      BEGIN; <file>; INSERT INTO schema_migrations; COMMIT   (own tx each;
//	  │      a failing file rolls back alone and stops the run)
//	  └─ pg_advisory_unlock
//
// Baseline adoption exists for apps moving off ORM auto-migrate (Ent's
// Schema.Create): 0001_baseline.sql is the current prod schema, and a prod
// database that already has those tables but no schema_migrations is marked
// as being at the baseline instead of re-running it. A fresh database runs the
// baseline file normally.
//
// The runner talks database/sql, so any Postgres driver works. pgx-pool users
// wrap their pool: db := stdlib.OpenDBFromPool(pool). Migration files come from
// an fs.FS: os.DirFS("infra/migrations") or an embed.FS.
//
// LatestVersion returns the newest recorded filename — the data editor shows it
// as the schema version.
package migrate

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"sort"
	"strings"
)

// DefaultLockKey is the Postgres advisory-lock key bbux has always used
// ("bbux1"), kept so old and new runners still exclude each other.
const DefaultLockKey int64 = 0x6262757831

// Options tunes Up. The zero value is valid.
type Options struct {
	// Baseline, when set, names a migration file (e.g. "0001_baseline.sql").
	// If the database has no schema_migrations table but already has tables,
	// every file up to and including Baseline is recorded as applied without
	// running. It must exist in the migrations FS.
	Baseline string
	// LockKey overrides DefaultLockKey (only needed to let two unrelated apps
	// share one database and migrate concurrently).
	LockKey int64
	// Logger receives one line per applied/adopted file. Nil → slog.Default().
	Logger *slog.Logger
}

// Result reports what Up did.
type Result struct {
	Applied []string // files executed this run
	Adopted []string // files recorded as applied by baseline adoption (not executed)
}

// Files lists the *.sql files at the root of fsys in the order Up applies them.
func Files(fsys fs.FS) ([]string, error) {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, fmt.Errorf("migrate: read migrations: %w", err)
	}
	var files []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			files = append(files, e.Name())
		}
	}
	sort.Strings(files)
	return files, nil
}

// Up applies every pending migration in fsys. See the package doc for the flow.
func Up(ctx context.Context, db *sql.DB, fsys fs.FS, opts Options) (Result, error) {
	var res Result
	log := opts.Logger
	if log == nil {
		log = slog.Default()
	}
	lockKey := opts.LockKey
	if lockKey == 0 {
		lockKey = DefaultLockKey
	}

	files, err := Files(fsys)
	if err != nil {
		return res, err
	}
	if opts.Baseline != "" && !contains(files, opts.Baseline) {
		return res, fmt.Errorf("migrate: baseline %q is not a migration file", opts.Baseline)
	}

	// One dedicated connection: the advisory lock is session-scoped.
	conn, err := db.Conn(ctx)
	if err != nil {
		return res, fmt.Errorf("migrate: acquire conn: %w", err)
	}
	defer conn.Close()

	if _, err := conn.ExecContext(ctx, `SELECT pg_advisory_lock($1)`, lockKey); err != nil {
		return res, fmt.Errorf("migrate: acquire lock: %w", err)
	}
	// Unlock even if ctx is cancelled — the conn goes back to the pool and a
	// leaked session lock would block every later migrator.
	defer func() {
		_, _ = conn.ExecContext(context.WithoutCancel(ctx), `SELECT pg_advisory_unlock($1)`, lockKey)
	}()

	hasTable, err := tableExists(ctx, conn)
	if err != nil {
		return res, err
	}
	if !hasTable {
		adopt := false
		if opts.Baseline != "" {
			if adopt, err = schemaHasTables(ctx, conn); err != nil {
				return res, err
			}
		}
		var toAdopt []string
		if adopt {
			for _, f := range files {
				if f <= opts.Baseline {
					toAdopt = append(toAdopt, f)
				}
			}
		}
		if err := createTable(ctx, conn, toAdopt); err != nil {
			return res, err
		}
		for _, f := range toAdopt {
			log.Info("migration adopted (baseline, not executed)", "filename", f)
		}
		res.Adopted = toAdopt
	}

	applied, err := appliedSet(ctx, conn)
	if err != nil {
		return res, err
	}
	for _, name := range files {
		if applied[name] {
			continue
		}
		body, err := fs.ReadFile(fsys, name)
		if err != nil {
			return res, fmt.Errorf("migrate: read %q: %w", name, err)
		}
		if err := applyOne(ctx, conn, name, string(body)); err != nil {
			return res, err
		}
		log.Info("migration applied", "filename", name)
		res.Applied = append(res.Applied, name)
	}
	return res, nil
}

// LatestVersion returns the newest applied migration filename, or "" when
// nothing is applied or schema_migrations doesn't exist yet.
func LatestVersion(ctx context.Context, db *sql.DB) (string, error) {
	var reg sql.NullString
	if err := db.QueryRowContext(ctx, `SELECT to_regclass('schema_migrations')::text`).Scan(&reg); err != nil {
		return "", fmt.Errorf("migrate: check schema_migrations: %w", err)
	}
	if !reg.Valid {
		return "", nil
	}
	var latest sql.NullString
	if err := db.QueryRowContext(ctx, `SELECT max(filename) FROM schema_migrations`).Scan(&latest); err != nil {
		return "", fmt.Errorf("migrate: latest version: %w", err)
	}
	return latest.String, nil
}

func applyOne(ctx context.Context, conn *sql.Conn, name, body string) error {
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("migrate: begin %q: %w", name, err)
	}
	if _, err := tx.ExecContext(ctx, body); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("migrate: apply %q: %w", name, err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations (filename) VALUES ($1)`, name); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("migrate: record %q: %w", name, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("migrate: commit %q: %w", name, err)
	}
	return nil
}

// createTable creates schema_migrations and records adopted files in ONE
// transaction, so a crash can't leave an empty table that skips adoption.
func createTable(ctx context.Context, conn *sql.Conn, adopted []string) error {
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("migrate: begin create: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			filename   text PRIMARY KEY,
			applied_at timestamptz NOT NULL DEFAULT now()
		)`); err != nil {
		return fmt.Errorf("migrate: create schema_migrations: %w", err)
	}
	for _, f := range adopted {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO schema_migrations (filename) VALUES ($1) ON CONFLICT DO NOTHING`, f); err != nil {
			return fmt.Errorf("migrate: adopt %q: %w", f, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("migrate: commit create: %w", err)
	}
	return nil
}

func tableExists(ctx context.Context, conn *sql.Conn) (bool, error) {
	var reg sql.NullString
	if err := conn.QueryRowContext(ctx, `SELECT to_regclass('schema_migrations')::text`).Scan(&reg); err != nil {
		return false, fmt.Errorf("migrate: check schema_migrations: %w", err)
	}
	return reg.Valid, nil
}

// schemaHasTables reports whether the current schema already holds tables —
// the signal that a pre-existing (e.g. Ent-created) database needs adopting.
func schemaHasTables(ctx context.Context, conn *sql.Conn) (bool, error) {
	var exists bool
	err := conn.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM information_schema.tables
			WHERE table_schema = current_schema()
			  AND table_type = 'BASE TABLE'
			  AND table_name <> 'schema_migrations')`).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("migrate: inspect schema: %w", err)
	}
	return exists, nil
}

func appliedSet(ctx context.Context, conn *sql.Conn) (map[string]bool, error) {
	rows, err := conn.QueryContext(ctx, `SELECT filename FROM schema_migrations`)
	if err != nil {
		return nil, fmt.Errorf("migrate: list applied: %w", err)
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var f string
		if err := rows.Scan(&f); err != nil {
			return nil, err
		}
		out[f] = true
	}
	if err := rows.Err(); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	return out, nil
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
