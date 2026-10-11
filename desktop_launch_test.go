package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDesktopWorkspace(t *testing.T) {
	dir := t.TempDir()
	root, err := desktopWorkspace(dir)
	if err != nil {
		t.Fatal(err)
	}
	want, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	if root != want {
		t.Fatalf("desktopWorkspace() = %q, want %q", root, want)
	}
}

func TestDesktopWorkspaceRejectsFile(t *testing.T) {
	file := filepath.Join(t.TempDir(), "file.go")
	if err := os.WriteFile(file, []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := desktopWorkspace(file); err == nil {
		t.Fatal("desktopWorkspace accepted a file")
	}
}
