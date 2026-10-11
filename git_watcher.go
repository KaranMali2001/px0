package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// GitStatusPayload contains the complete serialized git status event sent to the browser
// over Server-Sent Events (SSE). It includes working tree status, staged files, dirty directories,
// current branch, commit ahead/behind counts, and recent commits.
type GitStatusPayload struct {
	Git           bool              `json:"git"`                     // True if root is inside a git repository
	GitChanges    int               `json:"gitChanges"`              // Total count of modified, added, deleted, or untracked files
	GitFiles      []string          `json:"gitFiles"`                // List of modified file paths relative to root
	Statuses      map[string]string `json:"statuses"`                // Relative path -> status code (e.g. "M", "A", "D", "U")
	DirtyDirs     map[string]bool   `json:"dirtyDirs,omitempty"`     // Relative directory path -> true if any descendant is modified
	Staged        map[string]bool   `json:"staged,omitempty"`        // Relative path -> true if file has staged changes
	YourStatuses  map[string]string `json:"yourStatuses,omitempty"`  // PR mode: relative path -> reviewer modification status
	YourDirtyDirs map[string]bool   `json:"yourDirtyDirs,omitempty"` // PR mode: directory path -> contains reviewer changes
	Branch        string            `json:"branch,omitempty"`        // Active branch name
	RecentCommits []GitCommit       `json:"recentCommits,omitempty"` // Recent git commits for commit list UI
	CommitsURL    string            `json:"commitsUrl,omitempty"`    // Web URL to view commits on GitHub/forge
	Ahead         int               `json:"ahead"`                   // Commits ahead of upstream tracking branch
	Behind        int               `json:"behind"`                  // Commits behind upstream tracking branch
}

// GitWatcher monitors git state changes in the background and broadcasts updates
// to connected browser clients over SSE (/api/stream). It uses a hybrid detection strategy:
//  1. Fast path: 1-second polling of .git metadata files (HEAD, index, packed-refs) to instantly
//     detect CLI operations (git commit, git checkout, git add).
//  2. Worktree path: adaptive polling of working tree file modifications.
//  3. Manual triggers: immediate re-evaluations triggered on user UI actions.
type GitWatcher struct {
	ix   *Index
	root string

	mu      sync.Mutex
	clients map[chan []byte]struct{}
	active  atomic.Int32

	triggerCh chan struct{}
	stopCh    chan struct{}
	loopDone  chan struct{}
	// idleCh hands the running loop an idle policy (desktop only); idle is
	// the installed one, so SetForeground can reach it.
	idleCh chan *idlePolicy
	idle   atomic.Pointer[idlePolicy]

	lastIndexMod   time.Time
	lastIndexSize  int64
	lastHeadMod    time.Time
	lastPackedMod  time.Time
	lastHeadCommit string
	lastAhead      int
	lastBehind     int
}

// NewGitWatcher constructs a new GitWatcher attached to the given workspace index.
func NewGitWatcher(ix *Index) *GitWatcher {
	gw := &GitWatcher{
		ix:        ix,
		root:      ix.Root(),
		clients:   make(map[chan []byte]struct{}),
		triggerCh: make(chan struct{}, 1),
		stopCh:    make(chan struct{}),
		loopDone:  make(chan struct{}),
		idleCh:    make(chan *idlePolicy, 1),
	}
	// Skip HEAD when the probe timed out, so a pending macOS access prompt
	// doesn't stall construction a second time.
	if gitAvailable(gw.root) {
		gw.lastHeadCommit = gitHeadCommit(gw.root)
	}
	return gw
}

// Start begins the background monitoring goroutine.
func (gw *GitWatcher) Start(ctx context.Context) {
	if gitDisabled {
		return
	}
	if !gitAvailable(gw.root) {
		if gitProbeTimedOut(gw.root) {
			go gw.retryStart(ctx)
		}
		return
	}
	gw.run(ctx)
}

// Backoff for re-probing a root whose first probe timed out, e.g. while a
// macOS folder-access prompt is pending; the last delay repeats.
var (
	gitWatchRetry = []time.Duration{2 * time.Second, 5 * time.Second, 10 * time.Second, 30 * time.Second}
	gitWatchAfter = time.After
	gitWatchProbe = func(root string) bool { return probeGit(root, true).ok }
)

// retryStart starts the loop once the user grants access, so live git
// status resumes without reopening the folder.
func (gw *GitWatcher) retryStart(ctx context.Context) {
	for i := 0; ; i++ {
		select {
		case <-ctx.Done():
			return
		case <-gw.stopCh:
			return
		case <-gitWatchAfter(gitWatchRetry[min(i, len(gitWatchRetry)-1)]):
		}
		if gitWatchProbe(gw.root) {
			select {
			case <-gw.stopCh:
				return
			default:
			}
			gw.run(ctx)
			gw.Trigger()
			return
		}
		if !gitProbeTimedOut(gw.root) {
			return
		}
	}
}

func (gw *GitWatcher) run(ctx context.Context) {
	gitdir := gitDir(gw.root)
	gw.recordGitMeta(gitdir)

	go func() {
		defer close(gw.loopDone)
		gw.loop(ctx, gitdir)
	}()
}

func (gw *GitWatcher) Stop() {
	select {
	case <-gw.stopCh:
	default:
		close(gw.stopCh)
	}
}

// Trigger requests an immediate status check and broadcast.
func (gw *GitWatcher) Trigger() {
	select {
	case gw.triggerCh <- struct{}{}:
	default:
	}
}

func (gw *GitWatcher) recordGitMeta(gitdir string) bool {
	if gitdir == "" {
		return false
	}
	changed := false

	indexPath := filepath.Join(gitdir, "index")
	if fi, err := os.Stat(indexPath); err == nil {
		if fi.ModTime() != gw.lastIndexMod || fi.Size() != gw.lastIndexSize {
			gw.lastIndexMod = fi.ModTime()
			gw.lastIndexSize = fi.Size()
			changed = true
		}
	}

	headPath := filepath.Join(gitdir, "HEAD")
	if fi, err := os.Stat(headPath); err == nil {
		if fi.ModTime() != gw.lastHeadMod {
			gw.lastHeadMod = fi.ModTime()
			changed = true
		}
	}

	packedPath := filepath.Join(gitdir, "packed-refs")
	if fi, err := os.Stat(packedPath); err == nil {
		if fi.ModTime() != gw.lastPackedMod {
			gw.lastPackedMod = fi.ModTime()
			changed = true
		}
	}

	return changed
}

// Upstream (CLI) polling intervals. The desktop idle policy never touches these.
const (
	gitMetaPoll        = 1 * time.Second
	gitWorktreePollMin = 1 * time.Second
	gitWorktreePollMax = 10 * time.Second
)

func (gw *GitWatcher) loop(ctx context.Context, gitdir string) {
	metaTicker := time.NewTicker(gitMetaPoll)
	defer metaTicker.Stop()

	adaptiveInterval := gitWorktreePollMin
	worktreeTicker := time.NewTicker(adaptiveInterval)
	defer worktreeTicker.Stop()

	heartbeatTicker := time.NewTicker(15 * time.Second)
	defer heartbeatTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-gw.stopCh:
			return

		case <-metaTicker.C:
			// Fast path: check .git metadata (HEAD, index, packed-refs).
			// If touched by git CLI (commit, checkout, add, etc.), refresh immediately.
			if gw.recordGitMeta(gitdir) {
				gw.checkAndBroadcast()
			}

		case <-worktreeTicker.C:
			// Worktree path: check for modifications in files outside git CLI.
			// Only run when at least one client is actively listening.
			if gw.active.Load() > 0 {
				dur := gw.checkAndBroadcast()
				// Adapt interval based on execution speed: min 1s, max 10s.
				newInterval := dur * 5
				if newInterval < gitWorktreePollMin {
					newInterval = gitWorktreePollMin
				} else if newInterval > gitWorktreePollMax {
					newInterval = gitWorktreePollMax
				}
				if newInterval != adaptiveInterval {
					adaptiveInterval = newInterval
					worktreeTicker.Reset(adaptiveInterval)
				}
			}

		case <-gw.triggerCh:
			// Debounce rapid triggers
			time.Sleep(50 * time.Millisecond)
			// Drain any pending triggers queued during debounce
			select {
			case <-gw.triggerCh:
			default:
			}
			gw.checkAndBroadcast()

		case <-heartbeatTicker.C:
			gw.broadcast([]byte(": ping\n\n"))

		case p := <-gw.idleCh:
			gw.idleLoop(ctx, gitdir, p)
			return
		}
	}
}

// Desktop idle policy. In the foreground it polls every idleBase like
// upstream, but a poll only stats .git metadata and the indexed worktree;
// git runs when those stats change or when the full-refresh backoff expires.
// That backoff starts at idleBase, doubles every idleStep quiet refreshes up
// to idleCap and snaps back on any change, so edits the stats miss still show
// within idleCap. In the background it polls metadata every idleCap.
const (
	idleBase = 1 * time.Second
	idleCap  = 5 * time.Second
	idleStep = 3
)

type idlePolicy struct {
	mu         sync.Mutex
	quiet      int
	background bool
	nextFull   time.Time
	floor      time.Duration
	// safety, when set, replaces the foreground backoff: change notifications
	// report edits, so a full refresh only backstops dropped events.
	safety time.Duration
}

// next records whether the last refresh saw a change and returns the
// full-refresh backoff delay.
func (p *idlePolicy) next(changed bool) time.Duration {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.nextLocked(changed)
}

func (p *idlePolicy) nextLocked(changed bool) time.Duration {
	if changed {
		p.quiet = 0
	} else if p.quiet < idleStep*3 {
		p.quiet++
	}
	if p.background {
		return idleCap
	}
	if p.safety > 0 {
		return p.safety
	}
	if d := idleBase << (p.quiet / idleStep); d < idleCap {
		return d
	}
	return idleCap
}

// shouldRefresh reports whether a poll at now must run git: the cheap stats
// changed, or (foreground only) the backoff deadline has passed.
func (p *idlePolicy) shouldRefresh(now time.Time, statChanged bool) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return statChanged || !p.background && !now.Before(p.nextFull)
}

// refreshed records a git refresh that started at now and took dur.
func (p *idlePolicy) refreshed(now time.Time, changed bool, dur time.Duration) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.nextFull = now.Add(p.nextLocked(changed))
	p.floor = dur * 5
}

// interval returns the delay until the next poll. The upstream adaptive
// floor still applies, so a slow repo is not re-scanned faster than five
// times its last git or stat scan.
func (p *idlePolicy) interval(statDur time.Duration) time.Duration {
	p.mu.Lock()
	defer p.mu.Unlock()
	d := idleBase
	if p.background {
		d = idleCap
	}
	floor := p.floor
	if statDur*5 > floor {
		floor = statDur * 5
	}
	if floor > d {
		d = floor
	}
	if d > gitWorktreePollMax {
		d = gitWorktreePollMax
	}
	return d
}

// setForeground reports whether this call brought the policy out of the
// background.
func (p *idlePolicy) setForeground(fg bool) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	was := p.background
	p.background = !fg
	if fg {
		p.quiet = 0
	}
	return fg && was
}

// worktreeWindow caps the lstats one cheap-check tick spends on the worktree.
var worktreeWindow = 2000

// worktreeScan fingerprints the indexed worktree one window at a time: a
// create, delete or rename bumps its directory's mtime and an edit bumps the
// file's mtime or size. A change outside the current window is caught when
// the cursor reaches it or by the idle policy's backoff git run, whichever
// comes first.
type worktreeScan struct {
	files  []FileEntry
	dirs   []string
	sums   []uint64
	seen   int
	cursor int
}

// tick checks the next window and reports whether it changed since the last
// pass. A rebuilt index counts as a change.
func (s *worktreeScan) tick(ix *Index) bool {
	ix.mu.RLock()
	files := ix.files
	rebuilt := len(files) != len(s.files) || len(files) > 0 && &files[0] != &s.files[0]
	if rebuilt {
		s.dirs = s.dirs[:0]
		for d := range ix.children {
			s.dirs = append(s.dirs, d)
		}
	}
	ix.mu.RUnlock()
	if rebuilt {
		slices.Sort(s.dirs)
		s.files = files
		s.sums = make([]uint64, (len(s.dirs)+len(files)+worktreeWindow-1)/worktreeWindow)
		s.seen, s.cursor = 0, 0
	}
	if len(s.sums) == 0 {
		return rebuilt
	}

	w := s.cursor
	lo, hi := w*worktreeWindow, min((w+1)*worktreeWindow, len(s.dirs)+len(files))
	var sum uint64
	for i := lo; i < hi; i++ {
		rel := ""
		if i < len(s.dirs) {
			rel = s.dirs[i]
		} else {
			rel = files[i-len(s.dirs)].Path
		}
		var mod, size uint64
		if fi, err := os.Lstat(ix.root + string(filepath.Separator) + rel); err == nil {
			mod, size = uint64(fi.ModTime().UnixNano()), uint64(fi.Size())
		}
		sum += entryHash(rel, mod, size)
	}
	changed := rebuilt || w < s.seen && s.sums[w] != sum
	s.sums[w] = sum
	s.seen = max(s.seen, w+1)
	s.cursor = (w + 1) % len(s.sums)
	return changed
}

// entryHash is FNV-1a over rel, mod and size, inlined to avoid allocating.
func entryHash(rel string, mod, size uint64) uint64 {
	const prime = 1099511628211
	h := uint64(14695981039346656037)
	for i := 0; i < len(rel); i++ {
		h = (h ^ uint64(rel[i])) * prime
	}
	for _, v := range [2]uint64{mod, size} {
		for i := 0; i < 64; i += 8 {
			h = (h ^ (v >> i & 0xff)) * prime
		}
	}
	return h
}

// SetIdlePolicy switches the watcher to the desktop idle policy. The CLI
// server never calls it and keeps the upstream fixed-interval polling.
func (gw *GitWatcher) SetIdlePolicy() {
	p := &idlePolicy{}
	if !gw.idle.CompareAndSwap(nil, p) {
		return
	}
	gw.idleCh <- p
}

// SetForeground reports whether the app window is visible and focused.
// Foreground resumes fast polling; coming out of the background also
// refreshes immediately. The desktop web UI already signals both: it drops
// its SSE stream when hidden (Subscribe/cancel) and POSTs /api/git/refresh
// on focus (handleGitRefresh).
func (gw *GitWatcher) SetForeground(fg bool) {
	p := gw.idle.Load()
	if p == nil {
		return
	}
	if p.setForeground(fg) {
		gw.Trigger()
	}
}

// treeEvent is a batch of directories, relative to the root, whose entries
// changed. Rescan means notifications were dropped, so anything may have.
type treeEvent struct {
	Dirs   []string
	Rescan bool
}

// startTreeEvents, when set (FSEvents in the macOS desktop build), replaces
// the idle policy's worktree polling with change notifications.
var startTreeEvents func(root string) (<-chan treeEvent, func(), error)

// eventSafety is the foreground full-refresh interval under notifications.
const eventSafety = time.Minute

// idleLoop is loop under the desktop idle policy: one timer drives both the
// metadata and worktree checks, and git only runs when they see a change or
// the backoff says a full refresh is due. With change notifications the
// timer only stats .git metadata, which notifications do not cover when the
// git dir lives outside the root.
func (gw *GitWatcher) idleLoop(ctx context.Context, gitdir string, p *idlePolicy) {
	var events <-chan treeEvent
	if startTreeEvents != nil {
		if ch, stop, err := startTreeEvents(gw.root); err == nil {
			events = ch
			defer stop()
			p.mu.Lock()
			p.safety = eventSafety
			p.mu.Unlock()
		}
	}

	timer := time.NewTimer(idleBase)
	defer timer.Stop()

	heartbeatTicker := time.NewTicker(15 * time.Second)
	defer heartbeatTicker.Stop()

	var scan worktreeScan
	if events == nil {
		scan.tick(gw.ix)
	}
	var lastRefresh time.Time
	pending := false
	refresh := func(now time.Time, metaChanged bool) {
		_, changed := gw.refresh()
		p.refreshed(now, metaChanged || changed, time.Since(now))
		lastRefresh, pending = time.Now(), false
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-gw.stopCh:
			return

		case <-timer.C:
			if !gw.ix.Ready() {
				timer.Reset(idleBase)
				continue
			}
			now := time.Now()
			changed := gw.recordGitMeta(gitdir)
			var statDur time.Duration
			listening := gw.active.Load() > 0
			if events != nil {
				changed = changed || pending && listening
			} else if listening {
				changed = scan.tick(gw.ix) || changed
				statDur = time.Since(now)
			}
			if (listening || changed) && p.shouldRefresh(now, changed) {
				refresh(time.Now(), changed)
			}
			timer.Reset(p.interval(statDur))

		case ev := <-events:
			if !gw.relevant(ev) {
				continue
			}
			pending = true
			if gw.active.Load() == 0 {
				continue // the focus refresh picks it up
			}
			// At most one git run per idleBase, however busy the tree is.
			if wait := idleBase - time.Since(lastRefresh); wait > 0 {
				timer.Reset(wait)
				continue
			}
			refresh(time.Now(), true)
			timer.Reset(p.interval(0))

		case <-gw.triggerCh:
			time.Sleep(50 * time.Millisecond)
			select {
			case <-gw.triggerCh:
			default:
			}
			gw.recordGitMeta(gitdir)
			if events == nil {
				scan.tick(gw.ix)
			}
			refresh(time.Now(), false)
			timer.Reset(p.interval(0))

		case <-heartbeatTicker.C:
			gw.broadcast([]byte(": ping\n\n"))
		}
	}
}

// relevant reports whether a change notification can affect git status:
// changes inside .git are covered by recordGitMeta, and directories the
// index skipped as ignored cannot change it.
func (gw *GitWatcher) relevant(ev treeEvent) bool {
	if ev.Rescan {
		return true
	}
	gw.ix.mu.RLock()
	defer gw.ix.mu.RUnlock()
	for _, d := range ev.Dirs {
		if d == ".git" || strings.HasPrefix(d, ".git/") {
			continue
		}
		if _, ok := gw.ix.children[d]; ok || !gw.ix.underIgnoredLocked(d) {
			return true
		}
	}
	return false
}

func statusMsg(payload GitStatusPayload) []byte {
	data, err := json.Marshal(payload)
	if err != nil {
		return nil
	}
	return []byte(fmt.Sprintf("event: git-status\ndata: %s\n\n", data))
}

func (gw *GitWatcher) sendWhenReady(ch chan []byte) {
	select {
	case <-gw.ix.readyCh:
	case <-gw.stopCh:
		return
	}
	msg := statusMsg(gw.Refresh())
	gw.mu.Lock()
	defer gw.mu.Unlock()
	if _, ok := gw.clients[ch]; ok && msg != nil {
		select {
		case ch <- msg:
		default:
		}
	}
}

// Refresh runs an immediate UpdateGitStatus, broadcasts to subscribers if changed,
// and returns the latest git status payload.
func (gw *GitWatcher) Refresh() GitStatusPayload {
	payload, _ := gw.refresh()
	return payload
}

// refresh is Refresh that also reports whether anything changed.
func (gw *GitWatcher) refresh() (GitStatusPayload, bool) {
	count, files, changed, statuses, dirtyDirs, staged, yourStatuses, yourDirtyDirs := gw.ix.UpdateGitStatus()
	var recentCommits []GitCommit
	var ahead, behind int
	if gitAvailable(gw.root) {
		recentCommits = gitRecentCommits(gw.root, 5)
		headCommit := ""
		if len(recentCommits) > 0 {
			headCommit = recentCommits[0].Hash
		}
		ahead, behind = gw.aheadBehind()
		gw.mu.Lock()
		if headCommit != gw.lastHeadCommit || ahead != gw.lastAhead || behind != gw.lastBehind {
			gw.lastHeadCommit = headCommit
			gw.lastAhead = ahead
			gw.lastBehind = behind
			changed = true
		}
		gw.mu.Unlock()
	}

	branch := gitCurrentBranch(gw.root)
	payload := GitStatusPayload{
		Git:           gitAvailable(gw.root),
		GitChanges:    count,
		GitFiles:      files,
		Statuses:      statuses,
		DirtyDirs:     dirtyDirs,
		Staged:        staged,
		YourStatuses:  yourStatuses,
		YourDirtyDirs: yourDirtyDirs,
		Branch:        branch,
		RecentCommits: recentCommits,
		CommitsURL:    gitCommitsWebURL(gw.root, branch),
		Ahead:         ahead,
		Behind:        behind,
	}
	if changed {
		data, err := json.Marshal(payload)
		if err == nil {
			msg := []byte(fmt.Sprintf("event: git-status\ndata: %s\n\n", data))
			gw.broadcast(msg)
		}
	}
	return payload, changed
}

// aheadBehind counts local commits not yet on the remote. In a PR review the
// checkout is a detached HEAD with no upstream, so "ahead" is measured from the
// PR head it was checked out at (or last pushed to) -- exactly the reviewer's
// own commits. Behind stays 0 there: knowing it needs a fetch, which Pull does.
func (gw *GitWatcher) aheadBehind() (ahead, behind int) {
	if head := gw.ix.PushedHead(); head != "" {
		return gitCountSince(gw.root, head), 0
	}
	ahead, behind, _ = gitAheadBehind(gw.root)
	return ahead, behind
}

// checkAndBroadcast runs UpdateGitStatus and broadcasts to subscribers if changed.
// Returns duration of the status check.
func (gw *GitWatcher) checkAndBroadcast() time.Duration {
	start := time.Now()
	gw.Refresh()
	return time.Since(start)
}

func (gw *GitWatcher) broadcast(msg []byte) {
	gw.mu.Lock()
	defer gw.mu.Unlock()
	for ch := range gw.clients {
		select {
		case ch <- msg:
		default:
		}
	}
}

// Subscribe registers a new SSE listener channel and returns an unregister cancel function.
func (gw *GitWatcher) Subscribe() (<-chan []byte, func()) {
	ch := make(chan []byte, 16)

	gw.mu.Lock()
	gw.clients[ch] = struct{}{}
	first := gw.active.Add(1) == 1
	gw.mu.Unlock()
	if first {
		gw.SetForeground(true)
	}

	// Immediately send current state to newly connected client
	inGit := gitAvailable(gw.root)
	var initialMsg []byte
	if inGit && !gw.ix.Ready() {
		// The index build runs git status itself; another run now would race
		// its tree walk for the disk. This client gets the state once it is in.
		go gw.sendWhenReady(ch)
	} else if inGit {
		initialMsg = statusMsg(gw.Refresh())
	} else {
		payload := GitStatusPayload{
			Git:        false,
			GitChanges: 0,
			GitFiles:   []string{},
			Statuses:   map[string]string{},
		}
		if data, err := json.Marshal(payload); err == nil {
			initialMsg = []byte(fmt.Sprintf("event: git-status\ndata: %s\n\n", data))
		}
	}

	if len(initialMsg) > 0 {
		ch <- initialMsg
	}

	cancel := func() {
		gw.mu.Lock()
		if _, ok := gw.clients[ch]; ok {
			delete(gw.clients, ch)
			if gw.active.Add(-1) == 0 {
				gw.SetForeground(false)
			}
			close(ch)
		}
		gw.mu.Unlock()
	}

	return ch, cancel
}
