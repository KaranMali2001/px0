package main

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestIdlePolicyBackoff(t *testing.T) {
	p := &idlePolicy{}
	s := time.Second
	want := []time.Duration{s, s, 2 * s, 2 * s, 2 * s, 4 * s, 4 * s, 4 * s, 5 * s, 5 * s, 5 * s}
	for i, w := range want {
		if got := p.next(false); got != w {
			t.Fatalf("quiet tick %d: got %v, want %v", i+1, got, w)
		}
	}
	if got := p.next(true); got != s {
		t.Fatalf("change must reset to 1s, got %v", got)
	}

	p.setForeground(false)
	for i := 0; i < 3; i++ {
		if got := p.next(i == 1); got != idleCap {
			t.Fatalf("background poll %d: got %v, want %v", i, got, idleCap)
		}
	}
	p.setForeground(true)
	if got := p.next(false); got != s {
		t.Fatalf("foreground must resume at 1s, got %v", got)
	}
}

func TestDefaultServerKeepsUpstreamIntervals(t *testing.T) {
	if gitMetaPoll != time.Second || gitWorktreePollMin != time.Second || gitWorktreePollMax != 10*time.Second {
		t.Fatalf("upstream intervals changed: meta=%v worktree=%v..%v", gitMetaPoll, gitWorktreePollMin, gitWorktreePollMax)
	}
	s := NewServer(NewIndex(t.TempDir()), nil)
	defer s.Close()
	if s.gitWatcher.idle.Load() != nil {
		t.Fatal("default server must not use the desktop idle policy")
	}
	s.gitWatcher.SetForeground(true)
	select {
	case <-s.gitWatcher.triggerCh:
		t.Fatal("SetForeground without an idle policy must be a no-op")
	default:
	}
}

func TestIdlePolicyFocusRefreshesImmediately(t *testing.T) {
	if !gitInstalled() {
		t.Skip("git not installed")
	}
	root := gitRepo(t)
	ix := NewIndex(root)
	ix.Build()
	gw := NewGitWatcher(ix)
	gw.Start(t.Context())
	defer gw.Stop()
	gw.SetIdlePolicy()

	ch, cancel := gw.Subscribe()
	defer cancel()
	<-ch
	gw.SetForeground(false)

	// Past the first 1s tick, the background policy has the next poll ~5s out,
	// so only the focus trigger can deliver the change within the deadline.
	time.Sleep(1200 * time.Millisecond)
	if err := os.WriteFile(filepath.Join(root, "focus_new.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gw.SetForeground(true)

	deadline := time.After(1500 * time.Millisecond)
	for {
		select {
		case msg := <-ch:
			if strings.Contains(string(msg), "focus_new.txt") {
				return
			}
		case <-deadline:
			t.Fatal("focus did not refresh git status within 1.5s")
		}
	}
}

// The desktop web UI drops its SSE stream when hidden and POSTs
// /api/git/refresh on focus; those must drive the idle policy.
func TestIdlePolicyFollowsWebSignals(t *testing.T) {
	s := NewServer(NewIndex(t.TempDir()), nil)
	defer s.Close()
	gw := s.gitWatcher
	gw.SetIdlePolicy()
	p := gw.idle.Load()
	bg := func() bool { p.mu.Lock(); defer p.mu.Unlock(); return p.background }

	_, cancel := gw.Subscribe()
	if bg() {
		t.Fatal("connected client must be foreground")
	}
	cancel()
	if !bg() {
		t.Fatal("last client disconnecting (page hidden) must go background")
	}
	_, cancel = gw.Subscribe()
	if bg() {
		t.Fatal("reconnecting (page visible) must return to foreground")
	}
	cancel()

	s.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("POST", "/api/git/refresh", nil))
	if bg() {
		t.Fatal("focus refresh must return to foreground")
	}
}
