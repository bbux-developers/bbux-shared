// Package health serves the gold-standard GET /health endpoint: it stays 503
// while the app is booting and turns 200 only once the app marks itself ready
// (after migrations, and seeding if any). Fly's health check and bbux-apps'
// release gate both poll it, so a half-migrated app never takes traffic.
//
//	boot ──▶ listen ──▶ migrate ──▶ seed ──▶ h.MarkReady()
//	          │                                  │
//	   GET /health = 503 {"status":"starting"}   GET /health = run checks
//	                                              ├─ all nil ─▶ 200 {"status":"ok"}
//	                                              └─ any err ─▶ 503 {"status":"unhealthy","checks":{name: err}}
//
// Checks are optional (e.g. a DB ping). Each runs with a per-request timeout;
// with no checks a ready app always answers 200 {"status":"ok"} — the exact
// body bbux's inline handler returned.
//
// Usage:
//
//	h := health.New(health.WithCheck("db", func(ctx context.Context) error { return db.PingContext(ctx) }))
//	mux.Handle("/health", h)
//	// ...migrate...
//	h.MarkReady()
package health

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"sync/atomic"
	"time"
)

// CheckFunc reports a dependency's health; a non-nil error means unhealthy.
type CheckFunc func(ctx context.Context) error

// DefaultTimeout bounds all checks of one request.
const DefaultTimeout = 2 * time.Second

// Handler is an http.Handler for /health. The zero value is not usable; use New.
type Handler struct {
	ready   atomic.Bool
	checks  map[string]CheckFunc
	timeout time.Duration
}

// Option configures a Handler.
type Option func(*Handler)

// WithCheck adds a named check that runs on every request once ready.
func WithCheck(name string, fn CheckFunc) Option {
	return func(h *Handler) { h.checks[name] = fn }
}

// WithTimeout overrides DefaultTimeout for the checks of one request.
func WithTimeout(d time.Duration) Option {
	return func(h *Handler) { h.timeout = d }
}

// New returns a Handler that is NOT ready until MarkReady is called.
func New(opts ...Option) *Handler {
	h := &Handler{checks: map[string]CheckFunc{}, timeout: DefaultTimeout}
	for _, o := range opts {
		o(h)
	}
	return h
}

// MarkReady flips the handler to ready (call after migrations succeed).
func (h *Handler) MarkReady() { h.ready.Store(true) }

// Ready reports whether MarkReady has been called.
func (h *Handler) Ready() bool { return h.ready.Load() }

// ServeHTTP answers 503 until ready, then 200 if every check passes.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !h.ready.Load() {
		write(w, http.StatusServiceUnavailable, map[string]any{"status": "starting"})
		return
	}
	if len(h.checks) == 0 {
		write(w, http.StatusOK, map[string]any{"status": "ok"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), h.timeout)
	defer cancel()

	names := make([]string, 0, len(h.checks))
	for n := range h.checks {
		names = append(names, n)
	}
	sort.Strings(names)
	failed := map[string]string{}
	for _, n := range names {
		if err := h.checks[n](ctx); err != nil {
			failed[n] = err.Error()
		}
	}
	if len(failed) > 0 {
		write(w, http.StatusServiceUnavailable, map[string]any{"status": "unhealthy", "checks": failed})
		return
	}
	write(w, http.StatusOK, map[string]any{"status": "ok"})
}

func write(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
