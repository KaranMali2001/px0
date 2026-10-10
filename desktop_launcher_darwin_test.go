//go:build darwin && !desktop

package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestDesktopAppCandidates(t *testing.T) {
	home := "/Users/tester"
	want := []string{"/tmp/custom.app", "/Users/tester/Applications/px0.app", "/Applications/px0.app"}
	if got := desktopAppCandidates("/tmp/custom.app", home); !reflect.DeepEqual(got, want) {
		t.Fatalf("desktopAppCandidates() = %q, want %q", got, want)
	}
}

func TestFindDesktopApp(t *testing.T) {
	root := t.TempDir()
	missing := filepath.Join(root, "missing.app")
	app := filepath.Join(root, "px0.app")
	binary := filepath.Join(app, "Contents", "MacOS", "px0")
	if err := os.MkdirAll(filepath.Dir(binary), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(binary, nil, 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := findDesktopApp([]string{missing, app})
	if err != nil {
		t.Fatal(err)
	}
	if got != app {
		t.Fatalf("findDesktopApp() = %q, want %q", got, app)
	}
}

func TestFindDesktopAppMissing(t *testing.T) {
	if _, err := findDesktopApp([]string{filepath.Join(t.TempDir(), "missing.app")}); err == nil || !strings.Contains(err.Error(), "make install-desktop") {
		t.Fatalf("findDesktopApp() error = %v", err)
	}
}

func TestDesktopOpenCommand(t *testing.T) {
	cmd := desktopOpenCommand("/Applications/px0.app", "/tmp/workspace")
	want := []string{"/usr/bin/open", "-n", "/Applications/px0.app", "--args", "/tmp/workspace"}
	if !reflect.DeepEqual(cmd.Args, want) {
		t.Fatalf("desktopOpenCommand() args = %q, want %q", cmd.Args, want)
	}
}
