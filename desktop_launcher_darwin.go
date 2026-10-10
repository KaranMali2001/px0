//go:build darwin && !desktop

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

func launchDesktop(root string) error {
	home, _ := os.UserHomeDir()
	app, err := findDesktopApp(desktopAppCandidates(os.Getenv("PX0_DESKTOP_APP"), home))
	if err != nil {
		return err
	}
	return desktopOpenCommand(app, root).Start()
}

func desktopAppCandidates(override, home string) []string {
	candidates := make([]string, 0, 3)
	if override != "" {
		candidates = append(candidates, override)
	}
	if home != "" {
		candidates = append(candidates, filepath.Join(home, "Applications", "px0.app"))
	}
	return append(candidates, "/Applications/px0.app")
}

func findDesktopApp(candidates []string) (string, error) {
	for _, app := range candidates {
		binary := filepath.Join(app, "Contents", "MacOS", "px0")
		if info, err := os.Stat(binary); err == nil && info.Mode().IsRegular() {
			return app, nil
		}
	}
	return "", fmt.Errorf("native px0 app not found; run make install-desktop or set PX0_DESKTOP_APP")
}

func desktopOpenCommand(app, root string) *exec.Cmd {
	return exec.Command("/usr/bin/open", "-n", app, "--args", root)
}
