package main

import (
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
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

func TestWorktreeScan(t *testing.T) {
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

	var s worktreeScan
	if !s.tick(ix) {
		t.Fatal("first scan of a built index must report a change")
	}
	if s.tick(ix) {
		t.Fatal("scan must be stable with nothing changed")
	}
	past := time.Now().Add(-time.Hour)
	if err := os.Chtimes(f, past, past); err != nil {
		t.Fatal(err)
	}
	if !s.tick(ix) {
		t.Fatal("editing a file must be seen")
	}
	if err := os.Chtimes(filepath.Join(root, "sub"), past, past); err != nil {
		t.Fatal(err)
	}
	s.tick(ix)
	if err := os.WriteFile(filepath.Join(root, "sub", "new.txt"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if !s.tick(ix) {
		t.Fatal("creating a file must be seen")
	}
}

// With the worktree spread over several windows, an edit outside the
// current window is invisible to the next tick but still reaches the client
// through the backoff git run.
func TestWorktreeScanWindowMissCaughtByBackoff(t *testing.T) {
	if !gitInstalled() {
		t.Skip("git not installed")
	}
	defer func(n int) { worktreeWindow = n }(worktreeWindow)
	worktreeWindow = 1

	root := gitRepo(t)
	ix := NewIndex(root)
	ix.Build()
	var s worktreeScan
	s.tick(ix)
	for range len(s.sums) - 1 {
		s.tick(ix)
	}
	if s.cursor != 0 || len(s.sums) < 3 {
		t.Fatalf("cursor %d over %d windows", s.cursor, len(s.sums))
	}
	// keep.go sits in a later window than the one the next tick checks.
	past := time.Now().Add(-time.Hour)
	if err := os.Chtimes(filepath.Join(root, "keep.go"), past, past); err != nil {
		t.Fatal(err)
	}
	if s.tick(ix) {
		t.Fatal("edit outside the current window must not be seen this tick")
	}

	gw := NewGitWatcher(ix)
	gw.Start(t.Context())
	defer gw.Stop()
	gw.SetIdlePolicy()
	ch, cancel := gw.Subscribe()
	defer cancel()
	<-ch
	time.Sleep(2500 * time.Millisecond)
	if err := os.WriteFile(filepath.Join(root, "keep.go"), []byte("keep edited\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Only the backoff git run can see the edit now, so it must be due.
	p := gw.idle.Load()
	p.mu.Lock()
	p.nextFull = time.Now().Add(1500 * time.Millisecond)
	p.mu.Unlock()
	for len(ch) > 0 {
		<-ch
	}
	deadline := time.After(4 * time.Second)
	for {
		select {
		case msg := <-ch:
			if strings.Contains(string(msg), "keep.go") {
				return
			}
		case <-deadline:
			t.Fatal("edit outside the scan window never reached the client")
		}
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

func TestGitWatcherRetriesTimedOutStart(t *testing.T) {
	if !gitInstalled() {
		t.Skip("git not installed")
	}
	root := gitRepo(t)
	gitMu.Lock()
	gitTimedOut[root] = time.Now().Add(time.Hour)
	gitMu.Unlock()
	t.Cleanup(func() {
		gitMu.Lock()
		delete(gitTimedOut, root)
		gitMu.Unlock()
	})

	var mu sync.Mutex
	var delays []time.Duration
	fired := make(chan time.Time)
	close(fired)
	oldAfter, oldProbe := gitWatchAfter, gitWatchProbe
	gitWatchAfter = func(d time.Duration) <-chan time.Time {
		mu.Lock()
		delays = append(delays, d)
		mu.Unlock()
		return fired
	}
	calls := 0
	probed := make(chan struct{})
	gitWatchProbe = func(r string) bool {
		if calls++; calls < 6 {
			return false
		}
		defer close(probed)
		return probeGit(r, true).ok
	}
	t.Cleanup(func() { gitWatchAfter, gitWatchProbe = oldAfter, oldProbe })

	gw := NewGitWatcher(NewIndex(root))
	gw.Start(t.Context())
	<-probed
	gw.Stop()
	select {
	case <-gw.loopDone:
	case <-time.After(5 * time.Second):
		t.Fatal("watcher loop never started after the probe succeeded")
	}
	mu.Lock()
	defer mu.Unlock()
	s := time.Second
	want := []time.Duration{2 * s, 5 * s, 10 * s, 30 * s, 30 * s, 30 * s}
	if len(delays) != len(want) {
		t.Fatalf("delays = %v, want %v", delays, want)
	}
	for i := range want {
		if delays[i] != want[i] {
			t.Fatalf("delays = %v, want %v", delays, want)
		}
	}
}

func TestGitWatcherRetryStopsOnClose(t *testing.T) {
	root := t.TempDir()
	gitMu.Lock()
	gitTimedOut[root] = time.Now().Add(time.Hour)
	gitMu.Unlock()
	t.Cleanup(func() {
		gitMu.Lock()
		delete(gitTimedOut, root)
		gitMu.Unlock()
	})
	oldAfter, oldProbe := gitWatchAfter, gitWatchProbe
	never := make(chan time.Time)
	asked := make(chan struct{}, 8)
	gitWatchAfter = func(time.Duration) <-chan time.Time { asked <- struct{}{}; return never }
	gitWatchProbe = func(string) bool { t.Error("probe after Stop"); return false }
	t.Cleanup(func() { gitWatchAfter, gitWatchProbe = oldAfter, oldProbe })

	gw := NewGitWatcher(NewIndex(root))
	gw.Start(t.Context())
	<-asked
	gw.Stop()
	time.Sleep(50 * time.Millisecond)
	select {
	case <-gw.loopDone:
		t.Fatal("retry must stop without starting the loop")
	default:
	}
}

func benchTree(b *testing.B, n int) *Index {
	root := b.TempDir()
	for i := 0; i < n; i++ {
		dir := filepath.Join(root, fmt.Sprintf("d%03d", i/100))
		if i%100 == 0 {
			if err := os.Mkdir(dir, 0o755); err != nil {
				b.Fatal(err)
			}
		}
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("f%03d.txt", i%100)), nil, 0o644); err != nil {
			b.Fatal(err)
		}
	}
	ix := NewIndex(root)
	ix.Build()
	return ix
}

// One foreground cheap-check tick over a 50,000-file worktree.
func BenchmarkWorktreeCheapTick(b *testing.B) {
	ix := benchTree(b, 50000)
	b.ReportAllocs()
	b.ResetTimer()
	var s worktreeScan
	s.tick(ix)
	for b.Loop() {
		s.tick(ix)
	}
}
