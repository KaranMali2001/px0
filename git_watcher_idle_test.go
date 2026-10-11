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

func TestIdlePolicyForegroundCheapTicks(t *testing.T) {
	p := &idlePolicy{}
	t0 := time.Unix(1000, 0)
	s := time.Second

	if !p.shouldRefresh(t0, false) {
		t.Fatal("first poll must run git")
	}
	now := t0
	for i := 0; i < idleStep*3; i++ {
		p.refreshed(now, false, 0)
		now = p.nextFull
	}
	if got := now.Sub(t0); got != 25*s {
		t.Fatalf("quiet refreshes should back off 1,1,2,2,2,4,4,4,5s, total %v", got)
	}

	// Backed off: still poll every 1s, but a quiet poll skips git until the cap.
	p.refreshed(now, false, 0)
	if got := p.nextFull.Sub(now); got != idleCap {
		t.Fatalf("backoff should reach the cap, got %v", got)
	}
	for at := now.Add(s); at.Before(now.Add(idleCap)); at = at.Add(s) {
		if got := p.interval(0); got != idleBase {
			t.Fatalf("foreground poll interval %v, want %v", got, idleBase)
		}
		if p.shouldRefresh(at, false) {
			t.Fatalf("quiet poll at +%v must not run git", at.Sub(now))
		}
		if !p.shouldRefresh(at, true) {
			t.Fatalf("stat change at +%v must run git", at.Sub(now))
		}
	}
	if !p.shouldRefresh(now.Add(idleCap), false) {
		t.Fatal("full refresh must run once the backoff expires")
	}

	p.refreshed(now, false, 400*time.Millisecond)
	if got := p.interval(0); got != 2*s {
		t.Fatalf("slow git run must floor the interval at 5x, got %v", got)
	}
	p.refreshed(now, false, 0)
	if got := p.interval(300 * time.Millisecond); got != 1500*time.Millisecond {
		t.Fatalf("slow stat scan must floor the interval at 5x, got %v", got)
	}

	p.setForeground(false)
	if got := p.interval(0); got != idleCap {
		t.Fatalf("background interval %v, want %v", got, idleCap)
	}
	if p.shouldRefresh(now.Add(time.Hour), false) {
		t.Fatal("background must only run git on a metadata change")
	}
	if !p.shouldRefresh(now, true) {
		t.Fatal("background metadata change must run git")
	}
}

func TestWorktreeStamp(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	f := filepath.Join(root, "sub", "a.txt")
	if err := os.WriteFile(f, []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ix := NewIndex(root)
	ix.Build()

	s0 := ix.worktreeStamp()
	if s0 != ix.worktreeStamp() {
		t.Fatal("stamp must be stable with nothing changed")
	}
	past := time.Now().Add(-time.Hour)
	if err := os.Chtimes(f, past, past); err != nil {
		t.Fatal(err)
	}
	s1 := ix.worktreeStamp()
	if s1 == s0 {
		t.Fatal("editing a file must change the stamp")
	}
	if err := os.Chtimes(filepath.Join(root, "sub"), past, past); err != nil {
		t.Fatal(err)
	}
	s2 := ix.worktreeStamp()
	if err := os.WriteFile(filepath.Join(root, "sub", "new.txt"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if ix.worktreeStamp() == s2 {
		t.Fatal("creating a file must change the stamp")
	}
}

// Backed off to the cap with no git run due, a foreground file creation must
// still reach the client on the next 1s stat poll.
func TestIdlePolicyForegroundChangeLatency(t *testing.T) {
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
	// Let git's racy-index rewrites of .git/index settle so only the
	// worktree stamp can notice the new file.
	time.Sleep(2500 * time.Millisecond)
	p := gw.idle.Load()
	p.mu.Lock()
	p.quiet = idleStep * 3
	p.nextFull = time.Now().Add(time.Hour)
	p.mu.Unlock()
	for len(ch) > 0 {
		<-ch
	}

	if err := os.WriteFile(filepath.Join(root, "fg_new.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	deadline := time.After(1500 * time.Millisecond)
	for {
		select {
		case msg := <-ch:
			if strings.Contains(string(msg), "fg_new.txt") {
				return
			}
		case <-deadline:
			t.Fatal("foreground change did not reach the client within 1.5s")
		}
	}
}
