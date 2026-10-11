//go:build desktop

package main

import (
	"context"
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
			TitleBar:             mac.TitleBarHidden(),
			Appearance:           mac.DefaultAppearance,
			WebviewIsTransparent: true,
			WindowIsTranslucent:  true,
			About:                &mac.AboutInfo{Title: "px0 " + version, Message: "Code navigator with agent editing"},
		},
		Menu:       buildMenu(h),
		OnShutdown: func(context.Context) { h.shutdown() },
		OnStartup: func(ctx context.Context) {
			enableHighRefreshRate()
			deskCtx.Store(&ctx)
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
	ctx := desktopCtx()
	if ctx == nil {
		return
	}
	dir, err := wruntime.OpenDirectoryDialog(ctx, wruntime.OpenDialogOptions{Title: "Open Folder"})
	if err != nil || dir == "" {
		return
	}
	root, _, _, err := resolveTarget(dir)
	if err != nil {
		return
	}
	h.open(root)
	wruntime.WindowReload(ctx)
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
