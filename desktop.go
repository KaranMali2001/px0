//go:build desktop

package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

// open builds a fresh engine for root and installs it as the active server.
func (h *deskHandler) open(root string) {
	ix := NewIndex(root)
	lsp := newLSPManager(root, true)
	srv := NewServer(ix, lsp)
	srv.gitWatcher.SetIdlePolicy()
	if ag, err := newAgentManager(root, "", lsp); err == nil {
		srv.SetAgent(ag)
	}
	h.install(srv)
	go ix.Build()
	rememberFolder(root)
}

// deskCtx is set in OnStartup and read from menu callbacks and goroutines.
var deskCtx atomic.Pointer[context.Context]

func desktopCtx() context.Context {
	if p := deskCtx.Load(); p != nil {
		return *p
	}
	return nil
}

func main() {
	augmentPATH()
	h := newDeskHandler()

	var openedFromArgs atomic.Bool
	h.openAsync(func() string {
		root, fromArgs := startupRoot()
		openedFromArgs.Store(fromArgs)
		return root
	}, h.open)
	go func() {
		<-h.ready
		if ctx := desktopCtx(); h.timedOut.Load() && ctx != nil {
			wruntime.WindowReload(ctx)
		}
	}()

	win, hasWin := loadWindowState()
	if !hasWin {
		win = windowState{W: 1200, H: 800}
	}
	app := &options.App{
		Title:     "px0",
		Width:     win.W,
		Height:    win.H,
		MinWidth:  600,
		MinHeight: 400,
		AssetServer: &assetserver.Options{
			Handler: h,
		},
		Mac: &mac.Options{
			TitleBar:             mac.TitleBarHidden(),
			Appearance:           mac.DefaultAppearance,
			WebviewIsTransparent: true,
			WindowIsTranslucent:  true,
			About:                &mac.AboutInfo{Title: "px0 " + version, Message: "Code navigator with agent editing"},
		},
		Menu:       buildMenu(h),
		OnShutdown: func(context.Context) { h.shutdown() },
		OnBeforeClose: func(ctx context.Context) bool {
			if wruntime.WindowIsNormal(ctx) {
				x, y := wruntime.WindowGetPosition(ctx)
				w, ht := wruntime.WindowGetSize(ctx)
				saveWindowState(windowState{X: x, Y: y, W: w, H: ht})
			}
			return false
		},
		OnDomReady: func(context.Context) { addSettingsMenuItem() },
		OnStartup: func(ctx context.Context) {
			enableHighRefreshRate()
			deskCtx.Store(&ctx)
			if hasWin {
				wruntime.WindowSetPosition(ctx, win.X, win.Y)
			}
			addSettingsMenuItem()
			go func() {
				<-h.ready
				// No folder from args or memory (first launch) must pick one;
				// a remembered folder still offers the picker.
				if h.cur.Load() == nil || !openedFromArgs.Load() {
					openFolderDialog(h)
				}
			}()
		},
	}
	if err := wails.Run(app); err != nil {
		fatal(err)
	}
}

// menuEvent is how native menu items reach the web UI, which owns the
// matching commands (settings, tabs, sidebar).
const menuEvent = "px0:menu"

const repoURL = "https://github.com/px0-ai/px0"

func emitMenu(cmd string) {
	if ctx := desktopCtx(); ctx != nil {
		wruntime.EventsEmit(ctx, menuEvent, cmd)
	}
}

// buildMenu adds Settings… to the app menu natively (addSettingsMenuItem),
// since Wails' AppMenu role has no slot for it.
func buildMenu(h *deskHandler) *menu.Menu {
	m := menu.NewMenu()
	m.Append(menu.AppMenu())

	file := m.AddSubmenu("File")
	file.AddText("Open Folder…", keys.CmdOrCtrl("o"), func(*menu.CallbackData) { openFolderDialog(h) })
	recent := file.AddSubmenu("Open Recent")
	for _, dir := range recentFolders() {
		dir := dir
		recent.AddText(dir, nil, func(*menu.CallbackData) { openFolder(h, dir) })
	}
	file.AddSeparator()
	file.AddText("Close Tab", keys.CmdOrCtrl("w"), func(*menu.CallbackData) { emitMenu("close-tab") })

	m.Append(menu.EditMenu())

	view := m.AddSubmenu("View")
	view.AddText("Toggle Sidebar", keys.CmdOrCtrl("b"), func(*menu.CallbackData) { emitMenu("toggle-sidebar") })
	view.AddSeparator()
	view.AddText("Reload", keys.CmdOrCtrl("r"), func(*menu.CallbackData) {
		if ctx := desktopCtx(); ctx != nil {
			wruntime.WindowReload(ctx)
		}
	})
	view.AddText("Toggle Full Screen", nil, func(*menu.CallbackData) {
		ctx := desktopCtx()
		if ctx == nil {
			return
		}
		if wruntime.WindowIsFullscreen(ctx) {
			wruntime.WindowUnfullscreen(ctx)
		} else {
			wruntime.WindowFullscreen(ctx)
		}
	})

	m.Append(menu.WindowMenu())

	help := m.AddSubmenu("Help")
	help.AddText("px0 on GitHub", nil, func(*menu.CallbackData) {
		if ctx := desktopCtx(); ctx != nil {
			wruntime.BrowserOpenURL(ctx, repoURL)
		}
	})
	return m
}

func openFolderDialog(h *deskHandler) {
	ctx := desktopCtx()
	if ctx == nil {
		return
	}
	dir, err := wruntime.OpenDirectoryDialog(ctx, wruntime.OpenDialogOptions{Title: "Open Folder"})
	if err != nil || dir == "" {
		return
	}
	openFolder(h, dir)
}

func openFolder(h *deskHandler, dir string) {
	ctx := desktopCtx()
	if ctx == nil {
		return
	}
	root, _, _, err := resolveTarget(dir)
	if err != nil {
		return
	}
	h.openAfterStartup(root, h.open)
	wruntime.WindowReload(ctx)
	wruntime.MenuSetApplicationMenu(ctx, buildMenu(h))
	wruntime.MenuUpdateApplicationMenu(ctx)
	addSettingsMenuItem()
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

const maxRecentFolders = 10

// folderStatePath lists recent folders, newest first. The first line alone
// is the pre-Open Recent format, so old files still restore.
func folderStatePath() string {
	return filepath.Join(filepath.Dir(settingsPath()), "last-folder")
}

func rememberFolder(root string) {
	list := []string{root}
	for _, p := range recentFolders() {
		if p != root && len(list) < maxRecentFolders {
			list = append(list, p)
		}
	}
	_ = os.MkdirAll(filepath.Dir(folderStatePath()), 0o755)
	_ = os.WriteFile(folderStatePath(), []byte(strings.Join(list, "\n")+"\n"), 0o644)
}

// recentFolders drops folders that no longer exist.
func recentFolders() []string {
	b, err := os.ReadFile(folderStatePath())
	if err != nil {
		return nil
	}
	var out []string
	for _, p := range strings.Split(string(b), "\n") {
		p = strings.TrimSpace(p)
		if fi, err := os.Stat(p); p != "" && err == nil && fi.IsDir() {
			out = append(out, p)
		}
	}
	return out
}

func lastFolder() string {
	if r := recentFolders(); len(r) > 0 {
		return r[0]
	}
	return ""
}

type windowState struct{ X, Y, W, H int }

func windowStatePath() string {
	return filepath.Join(filepath.Dir(settingsPath()), "window.json")
}

func saveWindowState(s windowState) {
	b, _ := json.Marshal(s)
	_ = os.MkdirAll(filepath.Dir(windowStatePath()), 0o755)
	_ = os.WriteFile(windowStatePath(), b, 0o644)
}

// loadWindowState rejects sizes below the window minimum (600x400).
func loadWindowState() (windowState, bool) {
	var s windowState
	b, err := os.ReadFile(windowStatePath())
	if err != nil || json.Unmarshal(b, &s) != nil || s.W < 600 || s.H < 400 {
		return windowState{}, false
	}
	return s, true
}

// augmentPATH makes LSP servers and coding agents discoverable when launched
// from Finder (GUI apps inherit only /usr/bin:/bin:/usr/sbin:/sbin).
// The login-shell probe is done synchronously but with a short timeout.
func augmentPATH() {
	extra := []string{"/opt/homebrew/bin", "/usr/local/bin"}
	if home, err := os.UserHomeDir(); err == nil {
		extra = append(extra,
			filepath.Join(home, "go", "bin"),
			filepath.Join(home, ".local", "bin"),
			filepath.Join(home, ".cargo", "bin"))
	}

	// Pull the real login-shell PATH (brew, asdf, nvm…) with a hard timeout
	// so a slow shell profile never delays startup.
	if sh := os.Getenv("SHELL"); sh != "" {
		type result struct{ paths []string }
		ch := make(chan result, 1)
		go func() {
			cmd := exec.Command(sh, "-lc", "echo $PATH")
			if out, err := cmd.Output(); err == nil {
				ch <- result{strings.Split(strings.TrimSpace(string(out)), ":")}
			} else {
				ch <- result{}
			}
		}()
		select {
		case r := <-ch:
			extra = append(extra, r.paths...)
		case <-time.After(3 * time.Second):
			// shell probe timed out; static extras still cover the common cases
		}
	}

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
