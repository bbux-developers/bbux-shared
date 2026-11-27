package health

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func get(t *testing.T, h http.Handler) (int, map[string]any) {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body not JSON: %q", rec.Body.String())
	}
	return rec.Code, body
}

func TestStartingUntilReady(t *testing.T) {
	h := New()
	if code, body := get(t, h); code != 503 || body["status"] != "starting" {
		t.Fatalf("before ready = %d %v, want 503 starting", code, body)
	}
	h.MarkReady()
	if !h.Ready() {
		t.Fatal("Ready() = false after MarkReady")
	}
	if code, body := get(t, h); code != 200 || body["status"] != "ok" {
		t.Fatalf("after ready = %d %v, want 200 ok", code, body)
	}
}

func TestChecksNotRunBeforeReady(t *testing.T) {
	ran := false
	h := New(WithCheck("db", func(context.Context) error { ran = true; return nil }))
	get(t, h)
	if ran {
		t.Fatal("checks must not run before MarkReady")
	}
}

func TestFailingCheckIs503(t *testing.T) {
	h := New(
		WithCheck("db", func(context.Context) error { return errors.New("down") }),
		WithCheck("cache", func(context.Context) error { return nil }),
	)
	h.MarkReady()
	code, body := get(t, h)
	if code != 503 || body["status"] != "unhealthy" {
		t.Fatalf("= %d %v, want 503 unhealthy", code, body)
	}
	checks := body["checks"].(map[string]any)
	if checks["db"] != "down" || checks["cache"] != nil {
		t.Fatalf("checks = %v, want only db=down", checks)
	}
}

func TestCheckTimeout(t *testing.T) {
	h := New(WithTimeout(20*time.Millisecond), WithCheck("slow", func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	}))
	h.MarkReady()
	if code, _ := get(t, h); code != 503 {
		t.Fatalf("slow check = %d, want 503", code)
	}
}

func TestPassingChecksIs200(t *testing.T) {
	h := New(WithCheck("db", func(context.Context) error { return nil }))
	h.MarkReady()
	if code, _ := get(t, h); code != 200 {
		t.Fatalf("= %d, want 200", code)
	}
}
