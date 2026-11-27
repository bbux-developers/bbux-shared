package migrate

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// ── pure tests ────────────────────────────────────────────────────────

func TestFilesSortedAndFiltered(t *testing.T) {
	fsys := fstest.MapFS{
		"0002_b.sql":     {Data: []byte("")},
		"0001_a.sql":     {Data: []byte("")},
		"README.md":      {Data: []byte("")},
		"sub/0000_x.sql": {Data: []byte("")},
	}
	got, err := Files(fsys)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, ",") != "0001_a.sql,0002_b.sql" {
		t.Fatalf("Files = %v", got)
	}
}

func TestUnknownBaselineRejectedBeforeTouchingDB(t *testing.T) {
	fsys := fstest.MapFS{"0001_a.sql": {Data: []byte("")}}
	_, err := Up(context.Background(), nil, fsys, Options{Baseline: "0009_nope.sql"})
	if err == nil || !strings.Contains(err.Error(), "baseline") {
		t.Fatalf("err = %v, want baseline error", err)
	}
}

// ── Postgres-backed tests (skip unless TEST_DATABASE_URL) ─────────────

// testDB returns a *sql.DB whose search_path is a fresh schema, dropped on
// cleanup, so each test sees an empty "database".
func testDB(t *testing.T) *sql.DB {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set — run `make test-go-integration`")
	}
	admin, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatal(err)
	}
	schema := fmt.Sprintf("mt_%d", time.Now().UnixNano())
	if _, err := admin.Exec(`CREATE SCHEMA ` + schema); err != nil {
		t.Fatal(err)
	}
	sep := "?"
	if strings.Contains(url, "?") {
		sep = "&"
	}
	db, err := sql.Open("pgx", url+sep+"search_path="+schema)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = db.Close()
		_, _ = admin.Exec(`DROP SCHEMA ` + schema + ` CASCADE`)
		_ = admin.Close()
	})
	return db
}

func count(t *testing.T, db *sql.DB, q string) int {
	t.Helper()
	var n int
	if err := db.QueryRow(q).Scan(&n); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
	return n
}

var twoFiles = fstest.MapFS{
	// Multi-statement files must work (bbux's do).
	"0001_init.sql": {Data: []byte("CREATE TABLE a (id int PRIMARY KEY); INSERT INTO a VALUES (1);")},
	"0002_more.sql": {Data: []byte("CREATE TABLE b (id int PRIMARY KEY);")},
	"notes.txt":     {Data: []byte("ignored")},
}

func TestUpAppliesInOrderAndIsIdempotent(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	if v, err := LatestVersion(ctx, db); err != nil || v != "" {
		t.Fatalf("LatestVersion on fresh db = (%q, %v), want empty", v, err)
	}
	res, err := Up(ctx, db, twoFiles, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(res.Applied, ",") != "0001_init.sql,0002_more.sql" || len(res.Adopted) != 0 {
		t.Fatalf("first run = %+v", res)
	}
	if count(t, db, `SELECT count(*) FROM a`) != 1 {
		t.Fatal("0001 data missing")
	}
	res, err = Up(ctx, db, twoFiles, Options{})
	if err != nil || len(res.Applied) != 0 {
		t.Fatalf("second run = (%+v, %v), want no-op", res, err)
	}
	if v, _ := LatestVersion(ctx, db); v != "0002_more.sql" {
		t.Fatalf("LatestVersion = %q", v)
	}
}

func TestFailingFileRollsBackAloneAndStops(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	fsys := fstest.MapFS{
		"0001_ok.sql":   {Data: []byte("CREATE TABLE ok (id int PRIMARY KEY);")},
		"0002_bad.sql":  {Data: []byte("CREATE TABLE half (id int); SELECT no_such_fn();")},
		"0003_next.sql": {Data: []byte("CREATE TABLE never (id int);")},
	}
	if _, err := Up(ctx, db, fsys, Options{}); err == nil || !strings.Contains(err.Error(), "0002_bad.sql") {
		t.Fatalf("err = %v, want failure naming 0002_bad.sql", err)
	}
	if count(t, db, `SELECT count(*) FROM schema_migrations`) != 1 {
		t.Fatal("only 0001 should be recorded")
	}
	if count(t, db, `SELECT count(*) FROM information_schema.tables WHERE table_schema = current_schema() AND table_name IN ('half','never')`) != 0 {
		t.Fatal("0002's partial DDL must roll back and 0003 must not run")
	}
	if v, _ := LatestVersion(ctx, db); v != "0001_ok.sql" {
		t.Fatalf("LatestVersion = %q", v)
	}
}

func TestBaselineAdoptsExistingSchema(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	// Simulate an Ent-created prod DB: tables exist, no schema_migrations.
	if _, err := db.Exec(`CREATE TABLE users (id int PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}
	fsys := fstest.MapFS{
		// Would fail if re-run against the existing table.
		"0001_baseline.sql": {Data: []byte("CREATE TABLE users (id int PRIMARY KEY);")},
		"0002_add.sql":      {Data: []byte("ALTER TABLE users ADD COLUMN email text;")},
	}
	res, err := Up(ctx, db, fsys, Options{Baseline: "0001_baseline.sql"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(res.Adopted, ",") != "0001_baseline.sql" || strings.Join(res.Applied, ",") != "0002_add.sql" {
		t.Fatalf("res = %+v", res)
	}
	if count(t, db, `SELECT count(*) FROM information_schema.columns WHERE table_schema = current_schema() AND table_name='users' AND column_name='email'`) != 1 {
		t.Fatal("post-baseline migration not applied")
	}
	if v, _ := LatestVersion(ctx, db); v != "0002_add.sql" {
		t.Fatalf("LatestVersion = %q", v)
	}
}

func TestBaselineRunsNormallyOnEmptyDB(t *testing.T) {
	db := testDB(t)
	fsys := fstest.MapFS{"0001_baseline.sql": {Data: []byte("CREATE TABLE users (id int PRIMARY KEY);")}}
	res, err := Up(context.Background(), db, fsys, Options{Baseline: "0001_baseline.sql"})
	if err != nil || len(res.Adopted) != 0 || len(res.Applied) != 1 {
		t.Fatalf("= (%+v, %v), want baseline executed", res, err)
	}
	if count(t, db, `SELECT count(*) FROM users`) != 0 {
		t.Fatal("users table should exist")
	}
}

func TestBaselineIgnoredOnceTracked(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	fsys := fstest.MapFS{"0001_baseline.sql": {Data: []byte("CREATE TABLE users (id int PRIMARY KEY);")}}
	if _, err := Up(ctx, db, fsys, Options{}); err != nil {
		t.Fatal(err)
	}
	fsys["0002_x.sql"] = &fstest.MapFile{Data: []byte("CREATE TABLE x (id int);")}
	res, err := Up(ctx, db, fsys, Options{Baseline: "0001_baseline.sql"})
	if err != nil || len(res.Adopted) != 0 || strings.Join(res.Applied, ",") != "0002_x.sql" {
		t.Fatalf("= (%+v, %v)", res, err)
	}
}

func TestConcurrentUpAppliesOnce(t *testing.T) {
	db := testDB(t)
	fsys := fstest.MapFS{"0001_a.sql": {Data: []byte("CREATE TABLE a (id int PRIMARY KEY);")}}
	var wg sync.WaitGroup
	errs := make([]error, 4)
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, errs[i] = Up(context.Background(), db, fsys, Options{})
		}(i)
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			t.Fatalf("concurrent Up: %v", err)
		}
	}
	if count(t, db, `SELECT count(*) FROM schema_migrations`) != 1 {
		t.Fatal("expected exactly one recorded migration")
	}
}
