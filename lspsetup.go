package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Setting up a language server from the UI: report what is missing for a file,
// run a known installer for it, and pick the result up without restarting px0.

// lspInstall is one way to get a language server's binary.
type lspInstall struct {
	OS   string   // "darwin", "linux" or "windows"; empty for any
	Cmd  []string // Cmd[0] is the installer, which must be present to use this
	Auto bool     // px0 may run it: user-level and non-interactive. Otherwise only shown.
}

func (d lspServerDef) installsFor(goos string) []lspInstall {
	var out []lspInstall
	for _, in := range d.Install {
		if in.OS == "" || in.OS == goos {
			out = append(out, in)
		}
	}
	return out
}

// registryFor lists every server px0 knows for rel's extension, best first,
// installed or not.
func registryFor(rel string) []*lspServerDef {
	ext := strings.ToLower(filepath.Ext(rel))
	var out []*lspServerDef
	for i := range lspRegistry {
		if slices.Contains(lspRegistry[i].Exts, ext) {
			out = append(out, &lspRegistry[i])
		}
	}
	return out
}

// MissingLang names the language of rel when px0 knows servers for it but none
// is installed, and is empty otherwise.
func (m *lspManager) MissingLang(rel string) string {
	if !m.enabled || !m.isDiscovered() || m.defFor(rel) != nil {
		return ""
	}
	if defs := registryFor(rel); len(defs) > 0 {
		return defs[0].Lang
	}
	return ""
}

type lspSetupOption struct {
	Cmd     string `json:"cmd"`
	Auto    bool   `json:"auto"`
	Tool    string `json:"tool"`
	HasTool bool   `json:"hasTool"`
}

type lspSetupServer struct {
	Name    string           `json:"name"`
	Options []lspSetupOption `json:"options"`
	Job     *lspJob          `json:"job,omitempty"`
}

type lspSetup struct {
	Enabled bool             `json:"enabled"`
	Lang    string           `json:"lang"`
	State   string           `json:"state"`
	Server  string           `json:"server"`
	Reason  string           `json:"reason,omitempty"` // why a server failed to start
	Servers []lspSetupServer `json:"servers"`
}

// Setup describes what the UI can offer for rel: the server's state, and each
// known server with the install options that apply to this system.
func (m *lspManager) Setup(rel string) lspSetup {
	st, srv := m.State(rel)
	s := lspSetup{Enabled: m.enabled, State: string(st), Server: srv, Servers: []lspSetupServer{}}
	if st == lspFailed {
		// State reports a failure's reason in place of the server name.
		s.Reason = srv
		if def := m.defFor(rel); def != nil {
			s.Server = def.Name
		}
	}
	dirs := lspBinDirs()
	for _, def := range registryFor(rel) {
		if s.Lang == "" {
			s.Lang = def.Lang
		}
		ss := lspSetupServer{Name: def.Name, Options: []lspSetupOption{}, Job: m.job(def.Name)}
		for _, in := range def.installsFor(runtime.GOOS) {
			_, has := lookPathIn(in.Cmd[0], dirs)
			ss.Options = append(ss.Options, lspSetupOption{
				Cmd: strings.Join(in.Cmd, " "), Auto: in.Auto, Tool: in.Cmd[0], HasTool: has,
			})
		}
		s.Servers = append(s.Servers, ss)
	}
	return s
}

// Rescan looks for servers again and forgets earlier start failures, so a server
// installed while px0 is running is used on the next request.
func (m *lspManager) Rescan() {
	if !m.enabled {
		return
	}
	m.discover()
	m.mu.Lock()
	if !m.closed {
		m.failed = map[string]string{}
		m.restarts = nil
	}
	m.mu.Unlock()
}

// ---------------------------------------------------------------- install jobs

const lspInstallTimeout = 15 * time.Minute

type lspJob struct {
	Server  string `json:"server"`
	Cmd     string `json:"cmd"`
	Running bool   `json:"running"`
	Error   string `json:"error,omitempty"`
	Log     string `json:"log"`

	out *tailBuffer
}

// job returns a snapshot of the latest install run for a server, or nil.
func (m *lspManager) job(name string) *lspJob {
	m.jobMu.Lock()
	defer m.jobMu.Unlock()
	j := m.jobs[name]
	if j == nil {
		return nil
	}
	cp := *j
	cp.Log = j.out.String()
	return &cp
}

// Install starts the option-th installer for a server in the background and
// returns at once. Only recipes from the registry are ever run, never a command
// supplied by the request.
func (m *lspManager) Install(name string, option int) (*lspJob, error) {
	if !m.enabled {
		return nil, errors.New("language servers are turned off (-no-lsp)")
	}
	var def *lspServerDef
	for i := range lspRegistry {
		if lspRegistry[i].Name == name {
			def = &lspRegistry[i]
			break
		}
	}
	if def == nil {
		return nil, fmt.Errorf("unknown language server %q", name)
	}
	opts := def.installsFor(runtime.GOOS)
	if option < 0 || option >= len(opts) {
		return nil, fmt.Errorf("%s has no install option %d on %s", name, option, runtime.GOOS)
	}
	in := opts[option]
	if !in.Auto {
		return nil, fmt.Errorf("px0 does not run %q; run it in a terminal", strings.Join(in.Cmd, " "))
	}
	tool, ok := lookPathIn(in.Cmd[0], lspBinDirs())
	if !ok {
		return nil, fmt.Errorf("%s is not installed", in.Cmd[0])
	}

	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil, context.Canceled
	}
	m.jobMu.Lock()
	if j := m.jobs[name]; j != nil && j.Running {
		m.jobMu.Unlock()
		m.mu.Unlock()
		return m.job(name), nil
	}
	j := &lspJob{Server: name, Cmd: strings.Join(in.Cmd, " "), Running: true, out: &tailBuffer{max: 16 << 10}}
	if m.jobs == nil {
		m.jobs = map[string]*lspJob{}
	}
	m.jobs[name] = j
	m.spawns.Add(1)
	m.jobMu.Unlock()
	m.mu.Unlock()

	go m.runInstall(def, j, tool, in.Cmd[1:])
	return m.job(name), nil
}

func (m *lspManager) runInstall(def *lspServerDef, j *lspJob, tool string, args []string) {
	defer m.spawns.Done()
	parent := m.ctx
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithTimeout(parent, lspInstallTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, tool, args...)
	setProcessGroup(cmd)
	cmd.Cancel = func() error { return killCommand(cmd) }
	cmd.WaitDelay = 500 * time.Millisecond
	// Outside the workspace, so its go.mod or package.json cannot change what is installed.
	if home, err := os.UserHomeDir(); err == nil {
		cmd.Dir = home
	}
	cmd.Stdout, cmd.Stderr = j.out, j.out // stdin stays empty: a prompt fails instead of hanging

	err := cmd.Run()
	if cmd.Process != nil {
		killCommand(cmd)
	}
	switch {
	case errors.Is(ctx.Err(), context.Canceled):
		err = ctx.Err()
	case ctx.Err() != nil:
		err = fmt.Errorf("gave up after %s", lspInstallTimeout)
	case err == nil:
		m.Rescan()
		if _, found := lookPathIn(def.Cmd[0], lspBinDirs()); !found {
			err = fmt.Errorf("installed, but %s is not on PATH or in the usual install folders", def.Cmd[0])
		}
	}

	m.jobMu.Lock()
	j.Running = false
	if err != nil {
		j.Error = err.Error()
	}
	m.jobMu.Unlock()
}

// tailBuffer keeps the last max bytes written to it: enough of an installer's
// output to explain a failure without holding a whole build log.
type tailBuffer struct {
	mu  sync.Mutex
	b   []byte
	max int
}

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.b = append(t.b, p...)
	if len(t.b) > t.max {
		t.b = append([]byte(nil), t.b[len(t.b)-t.max:]...)
	}
	return len(p), nil
}

func (t *tailBuffer) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return string(t.b)
}

// ---------------------------------------------------------------- HTTP

// localPost admits a request that changes the machine only when it is a POST
// from px0's own page. Browsers send Origin on every POST, so a page from another
// site cannot pass. Requiring the Host to be an IP address or localhost also
// shuts out DNS rebinding, where an attacker's domain is pointed at this machine
// and its Origin would otherwise match.
func localPost(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodPost {
		fail(w, http.StatusMethodNotAllowed, "POST only")
		return false
	}
	host := r.Host
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.Trim(host, "[]")
	if isTrustedDesktopRequest(r) {
		return true
	}
	if host != "localhost" && net.ParseIP(host) == nil {
		fail(w, http.StatusForbidden, "open px0 by IP address or localhost to set up language servers")
		return false
	}
	if o, err := url.Parse(r.Header.Get("Origin")); err != nil || o.Host != r.Host {
		fail(w, http.StatusForbidden, "request did not come from px0")
		return false
	}
	return true
}

func (s *Server) handleLSPSetup(w http.ResponseWriter, r *http.Request) {
	_, rel, ok := s.resolvePath(r.URL.Query().Get("path"))
	if !ok {
		fail(w, 400, "bad path")
		return
	}
	writeJSON(w, s.lsp.Setup(rel))
}

func (s *Server) handleLSPInstall(w http.ResponseWriter, r *http.Request) {
	if !localPost(w, r) {
		return
	}
	q := r.URL.Query()
	option, _ := strconv.Atoi(q.Get("option"))
	j, err := s.lsp.Install(q.Get("server"), option)
	if err != nil {
		fail(w, 400, err.Error())
		return
	}
	writeJSON(w, j)
}

// handleLSPStart finds servers installed since startup, clears earlier start
// failures and starts the server for path, reporting where it has got to.
func (s *Server) handleLSPStart(w http.ResponseWriter, r *http.Request) {
	if !localPost(w, r) {
		return
	}
	_, rel, ok := s.resolvePath(r.URL.Query().Get("path"))
	if !ok {
		fail(w, 400, "bad path")
		return
	}
	s.lsp.Rescan()
	ctx, cancel := context.WithTimeout(r.Context(), 1500*time.Millisecond)
	defer cancel()
	s.lsp.client(ctx, rel) // the spawn continues if this gives up waiting
	writeJSON(w, s.lspBrief(rel))
}

// lspBrief is the language server summary sent with a file: its state, and the
// language that lacks a server when none is installed, so the UI can offer one.
func (s *Server) lspBrief(rel string) map[string]any {
	state, srv := s.lsp.State(rel)
	b := map[string]any{"state": string(state), "server": srv}
	if lang := s.lsp.MissingLang(rel); lang != "" {
		b["missing"] = lang
	}
	return b
}
