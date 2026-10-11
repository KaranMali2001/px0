package main

import "fmt"

func desktopWorkspace(target string) (string, error) {
	root, initialFile, _, err := resolveTarget(target)
	if err != nil {
		return "", err
	}
	if initialFile != "" {
		return "", fmt.Errorf("-d requires a directory")
	}
	return root, nil
}
