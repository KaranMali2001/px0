//go:build desktop

package main

import (
	"context"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/menu"
	"github.com/wailsapp/wails/v2/pkg/menu/keys"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// deskHandler routes every webview request to the current workspace's *Server.
// Swapped atomically when the user opens a different folder.
type deskHandler struct {
	cur    atomic.Pointer[Server]
	mu     sync.Mutex
	closed bool
}

func (h *deskHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s := h.cur.Load()
	if s == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error":"no workspace open"}`))
		return
	}
	s.ServeHTTP(w, trustedDesktopRequest(r))
}

// open builds a fresh engine for root and installs it as the active server.
func (h *deskHandler) open(root string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return
	}
	h.cur.Swap(nil).Close()
	ix := NewIndex(root)
	lsp := newLSPManager(root, true)
	srv := NewServer(ix, lsp)
	if ag, err := newAgentManager(root, "", lsp); err == nil {
		srv.SetAgent(ag)
	}
	h.cur.Store(srv)
	srv.buildIndex()
	rememberFolder(root)
}

func (h *deskHandler) close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.closed = true
	h.cur.Swap(nil).Close()
}

var deskCtx context.Context

func main() {
	augmentPATH()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h := &deskHandler{}
	defer h.close()

	root, openedFromArgs := startupRoot()
	if root != "" {
		h.open(root)
	}

	app := &options.App{
		Title:     "px0",
		Width:     1200,
		Height:    800,
		MinWidth:  600,
		MinHeight: 400,
		AssetServer: &assetserver.Options{
			Handler: h,
		},
		Mac: &mac.Options{
			TitleBar:             mac.TitleBarHiddenInset(),
			Appearance:           mac.DefaultAppearance,
			WebviewIsTransparent: true,
			WindowIsTranslucent:  true,
			About:                &mac.AboutInfo{Title: "px0", Message: "Read-only code navigator"},
		},
		Menu: buildMenu(h),
		OnStartup: func(runtimeCtx context.Context) {
			enableHighRefreshRate()
			deskCtx = runtimeCtx
			go func() {
				discoverShellPATH(ctx)
				if srv := h.cur.Load(); srv != nil && ctx.Err() == nil {
					srv.lsp.Rescan()
				}
			}()
			if h.cur.Load() == nil {
				// First ever launch — no remembered folder; must pick one now.
				openFolderDialog(h)
				return
			}
			if !openedFromArgs {
				go openFolderDialog(h)
			}
		},
		OnShutdown: func(context.Context) {
			cancel()
			h.close()
		},
	}
	if err := wails.Run(app); err != nil {
		fatal(err)
	}
}

func buildMenu(h *deskHandler) *menu.Menu {
	m := menu.NewMenu()
	m.Append(menu.AppMenu())
	file := m.AddSubmenu("File")
	file.AddText("Open Folder…", keys.CmdOrCtrl("o"), func(_ *menu.CallbackData) {
		openFolderDialog(h)
	})
	m.Append(menu.EditMenu())
	m.Append(menu.WindowMenu())
	return m
}

func openFolderDialog(h *deskHandler) {
	if deskCtx == nil {
		return
	}
	dir, err := wruntime.OpenDirectoryDialog(deskCtx, wruntime.OpenDialogOptions{Title: "Open Folder"})
	if err != nil || dir == "" {
		return
	}
	h.open(dir)
	wruntime.WindowReload(deskCtx)
}

func startupRoot() (string, bool) {
	for _, a := range os.Args[1:] {
		if !strings.HasPrefix(a, "-") {
			if root, _, _, err := resolveTarget(a); err == nil {
				return root, true
			}
		}
	}
	return lastFolder(), false
}

func folderStatePath() string {
	return filepath.Join(filepath.Dir(settingsPath()), "last-folder")
}

func rememberFolder(root string) {
	_ = os.MkdirAll(filepath.Dir(folderStatePath()), 0o755)
	_ = os.WriteFile(folderStatePath(), []byte(root), 0o644)
}

func lastFolder() string {
	b, err := os.ReadFile(folderStatePath())
	if err != nil {
		return ""
	}
	p := strings.TrimSpace(string(b))
	if fi, err := os.Stat(p); err == nil && fi.IsDir() {
		return p
	}
	return ""
}

func augmentPATH() {
	extra := []string{"/opt/homebrew/bin", "/usr/local/bin"}
	if home, err := os.UserHomeDir(); err == nil {
		extra = append(extra,
			filepath.Join(home, "go", "bin"),
			filepath.Join(home, ".local", "bin"),
			filepath.Join(home, ".cargo", "bin"))
	}

	prependPATH(extra)
}

func discoverShellPATH(parent context.Context) {
	sh := os.Getenv("SHELL")
	if sh == "" {
		return
	}
	ctx, cancel := context.WithTimeout(parent, 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, sh, "-lc", "printf '%s' \"$PATH\"")
	setProcessGroup(cmd)
	cmd.Cancel = func() error { return killCommand(cmd) }
	cmd.WaitDelay = 100 * time.Millisecond
	out := &tailBuffer{max: 64 << 10}
	cmd.Stdout, cmd.Stderr = out, io.Discard
	err := cmd.Run()
	if cmd.Process != nil {
		killCommand(cmd)
	}
	if err == nil && ctx.Err() == nil {
		prependPATH(filepath.SplitList(strings.TrimSpace(out.String())))
	}
}

func prependPATH(extra []string) {
	seen := map[string]bool{}
	var parts []string
	for _, p := range append(extra, filepath.SplitList(os.Getenv("PATH"))...) {
		if p != "" && !seen[p] {
			seen[p] = true
			parts = append(parts, p)
		}
	}
	_ = os.Setenv("PATH", strings.Join(parts, string(os.PathListSeparator)))
}
