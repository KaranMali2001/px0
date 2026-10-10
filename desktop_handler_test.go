package main

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestDeskHandlerOpenAsyncDoesNotBlock(t *testing.T) {
	h := newDeskHandler()
	release := make(chan struct{})
	start := time.Now()
	h.openAsync(func() string { return "/nowhere" }, func(string) { <-release })
	if time.Since(start) > time.Second {
		t.Fatal("openAsync blocked on open")
	}

	done := make(chan int, 1)
	go func() {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/meta", nil))
		done <- rec.Code
	}()
	select {
	case <-done:
		t.Fatal("request served before open finished")
	case <-time.After(50 * time.Millisecond):
	}

	close(release)
	select {
	case code := <-done:
		if code != http.StatusServiceUnavailable {
			t.Fatalf("code = %d, want 503 with no server installed", code)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("request still waiting after open finished")
	}
	if h.timedOut.Load() {
		t.Fatal("timedOut set although open finished in time")
	}
}

func TestDeskHandlerWaitIsBounded(t *testing.T) {
	old := deskOpenWait
	deskOpenWait = 20 * time.Millisecond
	t.Cleanup(func() { deskOpenWait = old })

	h := newDeskHandler()
	release := make(chan struct{})
	defer close(release)
	h.openAsync(func() string { return "/nowhere" }, func(string) { <-release })

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("code = %d, want 503", rec.Code)
	}
	if !h.timedOut.Load() {
		t.Fatal("timedOut not recorded")
	}
}

func TestIndexDesktopMarkerOnlyForDesktopRequests(t *testing.T) {
	srv := agentServer(t, t.TempDir(), "echo")
	get := func(r *http.Request) string {
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, r)
		if rec.Code != http.StatusOK {
			t.Fatalf("index status = %d", rec.Code)
		}
		return rec.Body.String()
	}
	upstream, err := fs.ReadFile(assets, "web/index.html")
	if err != nil {
		t.Fatal(err)
	}

	cli := get(httptest.NewRequest(http.MethodGet, "/", nil))
	if cli != string(upstream) {
		t.Fatal("CLI index differs from web/index.html")
	}
	if strings.Contains(cli, `class="desktop"`) || strings.Contains(cli, "static/static/") {
		t.Fatal("CLI index carries desktop markup")
	}

	desk := get(trustedDesktopRequest(httptest.NewRequest(http.MethodGet, "/", nil)))
	if !strings.Contains(desk, `<html class="desktop" lang="en"`) {
		t.Fatal("desktop index missing html.desktop marker")
	}
	for _, tag := range regexp.MustCompile(`<script\b[^>]*>`).FindAllString(desk, -1) {
		if !strings.Contains(tag, "src=") {
			t.Fatalf("desktop index has inline script %q; CSP script-src 'self' would block it", tag)
		}
	}
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, trustedDesktopRequest(httptest.NewRequest(http.MethodGet, "/", nil)))
	if csp := rec.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "script-src 'self';") {
		t.Fatalf("desktop index CSP = %q", csp)
	}
	if strings.Contains(desk, "https://px0.ai/favicon") || strings.Contains(desk, "https://px0.ai/logo/") || strings.Contains(desk, "fonts.googleapis.com/css2") {
		t.Fatal("desktop index still loads remote fonts/favicons/logos")
	}
	if css := get(httptest.NewRequest(http.MethodGet, "/static/static/fonts/fonts.css", nil)); !strings.Contains(css, "JetBrainsMono-Regular.woff2") {
		t.Fatal("bundled font stylesheet not served")
	}
}

func isClosed(ch <-chan struct{}) bool {
	select {
	case <-ch:
		return true
	default:
		return false
	}
}

func TestServerCloseIdempotentStopsWatcher(t *testing.T) {
	srv := agentServer(t, t.TempDir(), "echo")
	srv.Close()
	srv.Close()
	if !isClosed(srv.gitWatcher.stopCh) {
		t.Fatal("git watcher still running after Close")
	}
	if !isClosed(srv.done) {
		t.Fatal("scavenger not signalled")
	}
}

func TestDeskHandlerSwapClosesOldAndWrapsRequests(t *testing.T) {
	h := newDeskHandler()
	h.openAsync(func() string { return "" }, nil)
	<-h.ready

	a := agentServer(t, t.TempDir(), "echo")
	b := agentServer(t, t.TempDir(), "echo")
	h.install(a)
	h.install(b)
	select {
	case <-a.done:
	case <-time.After(5 * time.Second):
		t.Fatal("old server not closed after swap")
	}
	if isClosed(b.done) {
		t.Fatal("active server closed")
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `<html class="desktop"`) {
		t.Fatalf("desktop request not served as trusted: %d", rec.Code)
	}

	h.shutdown()
	if !isClosed(b.done) || h.cur.Load() != nil {
		t.Fatal("shutdown left the workspace open")
	}
}
