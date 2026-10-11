package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestServerCloseCancelsAndDrainsRequests(t *testing.T) {
	s, root := newTestServer(t)
	d, err := Open(filepath.Join(root, "greet.go"), "greet.go")
	if err != nil {
		t.Fatal(err)
	}
	entered := make(chan struct{})
	finished := make(chan struct{})
	s.mux.HandleFunc("/wait", func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		<-r.Context().Done()
	})
	go func() {
		s.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/wait", nil))
		close(finished)
	}()
	<-entered
	closed := make(chan struct{})
	go func() { s.Close(); close(closed) }()
	select {
	case <-closed:
	case <-time.After(3 * time.Second):
		t.Fatal("Close did not drain cancelled work")
	}
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("request still running after Close")
	}
	select {
	case <-s.scavenged:
	default:
		t.Fatal("scavenger still running after Close")
	}
	if cache.get(d.key) != nil {
		t.Fatal("closed workspace retained cached document")
	}
	if code, _ := get(t, s, "/api/meta"); code != http.StatusServiceUnavailable {
		t.Fatalf("closed server status = %d", code)
	}
	s.Close()
}

func TestObservationalRequestsDoNotResetIdleTime(t *testing.T) {
	s, _ := newTestServer(t)
	before := time.Now().Add(-time.Minute).UnixNano()
	s.lastReq.Store(before)
	for _, path := range []string{"/api/metrics", "/api/meta", "/static/style.css"} {
		s.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, path, nil))
		if s.lastReq.Load() != before {
			t.Fatalf("%s reset idle timer", path)
		}
	}
	get(t, s, "/api/tree")
	if s.lastReq.Load() == before {
		t.Fatal("navigation did not record activity")
	}
}

func TestBundledStaticAssetsResolve(t *testing.T) {
	s, _ := newTestServer(t)
	for _, path := range []string{"/static/app.js", "/static/style.css", "/static/favicon.ico", "/static/favicon-16x16.png", "/static/favicon-32x32.png", "/static/fonts/JetBrainsMono-Regular.woff2", "/static/logo/px0-logo-dark.png", "/static/logo/px0-logo-light.png"} {
		r := httptest.NewRecorder()
		s.ServeHTTP(r, httptest.NewRequest(http.MethodGet, path, nil))
		if r.Code != http.StatusOK {
			t.Errorf("%s status = %d", path, r.Code)
		}
	}
}

func TestTreePagesAndSeeksLargeDirectories(t *testing.T) {
	root := t.TempDir()
	kids := make([]Node, 1201)
	for i := range kids {
		kids[i].Path = filepath.ToSlash(filepath.Join("sub", time.Unix(int64(i), 0).Format("150405")))
	}
	ix := NewIndex(root)
	ix.children = map[string][]Node{"sub": kids}
	s := NewServer(ix, nil)
	defer s.Close()
	_, first := get(t, s, "/api/tree?dir=sub")
	if len(first["children"].([]any)) != 500 || first["total"].(float64) != 1201 {
		t.Fatalf("bad first page: %v", first["total"])
	}
	_, last := get(t, s, "/api/tree?dir=sub&seek="+kids[1200].Path)
	if last["offset"].(float64) != 1000 || len(last["children"].([]any)) != 201 {
		t.Fatal("seek did not select final page")
	}
}

func TestClosedManagersRefuseNewWork(t *testing.T) {
	lsp := newLSPManager(t.TempDir(), true)
	lsp.Close()
	if _, err := lsp.client(context.Background(), "x.go"); !errors.Is(err, context.Canceled) {
		t.Fatalf("closed LSP client error = %v", err)
	}
	agent := &agentManager{}
	agent.Close()
	if _, err := agent.Start("missing", "missing", 1, 1, "instruction", false); !errors.Is(err, context.Canceled) {
		t.Fatalf("closed agent Start error = %v", err)
	}
}

func TestFileChunksDoNotRunGit(t *testing.T) {
	s, root := newTestServer(t)
	dir := t.TempDir()
	marker := filepath.Join(dir, "called")
	if err := os.WriteFile(filepath.Join(dir, "git"), []byte("#!/bin/sh\n: > '"+marker+"'\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	gitMu.Lock()
	gitCache[root] = gitInfo{ok: true, toplevel: root}
	gitMu.Unlock()
	for _, url := range []string{"/api/file?path=greet.go&start=0&count=2", "/api/file?path=greet.go&start=2&count=2"} {
		if code, _ := get(t, s, url); code != http.StatusOK {
			t.Fatalf("file status = %d", code)
		}
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("file chunk spawned Git")
	}
}
