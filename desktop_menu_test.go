//go:build desktop

package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/wailsapp/wails/v2/pkg/menu"
	"github.com/wailsapp/wails/v2/pkg/menu/keys"
)

func TestRecentFoldersNewestFirstDedupedAndCapped(t *testing.T) {
	isolateSettings(t)
	var dirs []string
	for i := 0; i < maxRecentFolders+2; i++ {
		dirs = append(dirs, t.TempDir())
		rememberFolder(dirs[i])
	}
	rememberFolder(dirs[3])
	got := recentFolders()
	if len(got) != maxRecentFolders {
		t.Fatalf("len = %d, want %d", len(got), maxRecentFolders)
	}
	if got[0] != dirs[3] || got[1] != dirs[len(dirs)-1] || lastFolder() != dirs[3] {
		t.Fatalf("order = %v", got)
	}
	seen := map[string]bool{}
	for _, d := range got {
		if seen[d] {
			t.Fatalf("duplicate %s", d)
		}
		seen[d] = true
	}
}

func TestRecentFoldersReadsLegacyFileAndSkipsMissing(t *testing.T) {
	isolateSettings(t)
	dir := t.TempDir()
	_ = os.MkdirAll(filepath.Dir(folderStatePath()), 0o755)
	if err := os.WriteFile(folderStatePath(), []byte(filepath.Join(dir, "gone")+"\n"+dir), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := recentFolders(); len(got) != 1 || got[0] != dir {
		t.Fatalf("recentFolders = %v", got)
	}
}

func TestWindowStateRoundTripRejectsTooSmall(t *testing.T) {
	isolateSettings(t)
	if _, ok := loadWindowState(); ok {
		t.Fatal("state without a file")
	}
	want := windowState{X: 40, Y: 60, W: 1300, H: 900}
	saveWindowState(want)
	if got, ok := loadWindowState(); !ok || got != want {
		t.Fatalf("loadWindowState = %v %v", got, ok)
	}
	saveWindowState(windowState{W: 100, H: 900})
	if _, ok := loadWindowState(); ok {
		t.Fatal("accepted a size below the window minimum")
	}
}

func TestClampWindowState(t *testing.T) {
	for _, c := range []struct {
		in, want windowState
		ok       bool
	}{
		{windowState{40, 60, 1000, 700}, windowState{40, 60, 1000, 700}, true},
		{windowState{1000, 150, 1200, 800}, windowState{240, 100, 1200, 800}, true},
		{windowState{-300, -50, 1200, 800}, windowState{0, 0, 1200, 800}, true},
		{windowState{100, 100, 3000, 2000}, windowState{0, 0, 1440, 900}, true},
		{windowState{3000, 100, 1200, 800}, windowState{}, false},
		{windowState{1350, 100, 1200, 800}, windowState{}, false},
		{windowState{-1150, 100, 1200, 800}, windowState{}, false},
	} {
		if got, ok := clampWindowState(c.in, 1440, 900); got != c.want || ok != c.ok {
			t.Errorf("clamp(%v) = %v %v, want %v %v", c.in, got, ok, c.want, c.ok)
		}
	}
}

func TestBuildMenuWiring(t *testing.T) {
	isolateSettings(t)
	rememberFolder(t.TempDir())
	m := buildMenu(newDeskHandler())

	var roles []menu.Role
	subs := map[string]*menu.Menu{}
	for _, it := range m.Items {
		if it.Role != 0 {
			roles = append(roles, it.Role)
		} else {
			subs[it.Label] = it.SubMenu
		}
	}
	if len(roles) != 3 || roles[0] != menu.AppMenuRole || roles[1] != menu.EditMenuRole || roles[2] != menu.WindowMenuRole {
		t.Fatalf("roles = %v", roles)
	}

	item := func(sub, label string) *menu.MenuItem {
		t.Helper()
		s := subs[sub]
		if s == nil {
			t.Fatalf("no %s menu", sub)
		}
		for _, it := range s.Items {
			if it.Label == label {
				return it
			}
		}
		t.Fatalf("no %s > %s", sub, label)
		return nil
	}
	for _, c := range []struct {
		sub, label, key string
	}{
		{"File", "Open Folder…", "o"},
		{"File", "Close Tab", "w"},
		{"View", "Toggle Sidebar", "b"},
		{"View", "Reload", "r"},
	} {
		it := item(c.sub, c.label)
		if it.Accelerator == nil || it.Accelerator.Key != c.key || it.Accelerator.Modifiers[0] != keys.CmdOrCtrlKey {
			t.Errorf("%s > %s accelerator = %+v", c.sub, c.label, it.Accelerator)
		}
		if it.Click == nil {
			t.Errorf("%s > %s has no action", c.sub, c.label)
		}
	}
	item("View", "Toggle Full Screen")
	item("Help", "px0 on GitHub")
	if recent := item("File", "Open Recent").SubMenu; recent == nil || len(recent.Items) != 1 {
		t.Fatalf("Open Recent = %+v", recent)
	}
}
