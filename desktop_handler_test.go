package main

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
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
	cfg := os.Getenv("XDG_CONFIG_HOME")
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
	if !strings.Contains(desk, `<html class="desktop native" lang="en"`) {
		t.Fatal("desktop index missing html.desktop.native marker")
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
	if strings.Contains(desk, "https://px0.ai/favicon") || strings.Contains(desk, "https://px0.ai/logo/") || strings.Contains(desk, "fonts.googleapis.com") || strings.Contains(desk, "fonts.gstatic.com") {
		t.Fatal("desktop index still loads remote fonts/favicons/logos")
	}
	css := get(httptest.NewRequest(http.MethodGet, "/static/static/fonts/fonts.css", nil))
	urls := regexp.MustCompile(`url\('([^']+)'\)`).FindAllStringSubmatch(css, -1)
	if len(urls) == 0 {
		t.Fatal("bundled font stylesheet not served")
	}
	for _, url := range urls {
		if font := get(httptest.NewRequest(http.MethodGet, "/static/static/fonts/"+url[1], nil)); !strings.HasPrefix(font, "wOF2") {
			t.Fatalf("bundled font %s not served", url[1])
		}
	}
	if !strings.Contains(css, "font-weight: 600") {
		t.Fatal("bundled fonts miss upstream's 600 weight")
	}

	if err := os.MkdirAll(filepath.Join(cfg, "px0"), 0o755); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(desk, "side-right") {
		t.Fatal("default desktop index marks body.side-right")
	}
	if err := os.WriteFile(filepath.Join(cfg, "px0", "settings.json"), []byte(`{"desktop.nativeStyling": false, "workbench.sideBar.location": "right"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	desk = get(trustedDesktopRequest(httptest.NewRequest(http.MethodGet, "/", nil)))
	if !strings.Contains(desk, `<html class="desktop" lang="en"`) {
		t.Fatal("desktop.nativeStyling=false still marks html.native")
	}
	if !strings.Contains(desk, `<body class="side-right">`) {
		t.Fatal("right sidebar setting not applied to desktop index body")
	}
	if cli := get(httptest.NewRequest(http.MethodGet, "/", nil)); cli != string(upstream) {
		t.Fatal("CLI index changed by desktop settings")
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
	root, _ := filepath.EvalSymlinks(t.TempDir())
	if out, err := exec.Command("git", "init", root).CombinedOutput(); err != nil {
		t.Skipf("git init: %v\n%s", err, out)
	}
	srv := agentServer(t, root, "echo")
	if isClosed(srv.gitWatcher.loopDone) {
		t.Fatal("git watcher loop never ran")
	}
	srv.Close()
	srv.Close()
	select {
	case <-srv.gitWatcher.loopDone:
	case <-time.After(5 * time.Second):
		t.Fatal("git watcher loop still running after Close")
	}
	if !isClosed(srv.done) {
		t.Fatal("scavenger not signalled")
	}
}

func TestStreamHandlersExitOnClose(t *testing.T) {
	srv := agentServer(t, t.TempDir(), "echo")
	streams := map[string]http.HandlerFunc{
		"/api/stream":         srv.handleEventStream,
		"/api/git/stream":     srv.handleEventStream,
		"/api/threads/stream": srv.handleThreadStream,
	}
	exited := make(chan string, len(streams))
	for path, h := range streams {
		go func() {
			h(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, path, nil))
			exited <- path
		}()
	}
	time.Sleep(50 * time.Millisecond)
	srv.Close()
	for range streams {
		select {
		case <-exited:
		case <-time.After(5 * time.Second):
			t.Fatal("stream handler still running after Close")
		}
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
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `<html class="desktop`) {
		t.Fatalf("desktop request not served as trusted: %d", rec.Code)
	}

	h.shutdown()
	if !isClosed(b.done) || h.cur.Load() != nil {
		t.Fatal("shutdown left the workspace open")
	}
}
