package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// resolveTarget turns a directory or file into a workspace root, along with an optional
// initial file to open and optional line number.
// If the target is inside a git repository, that repository root is used as the workspace root.
// Otherwise, for relative paths within the current working directory, the working directory
// is used. Standalone files fall back to their parent directory.
func resolveTarget(target string) (root, initialFile string, initialLine int, err error) {
	cleanedTarget, line := splitTargetLine(target)
	abs, err := filepath.Abs(cleanedTarget)
	if err != nil {
		return "", "", 0, err
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", "", 0, fmt.Errorf("invalid target %s: %w", abs, err)
	}
	st, err := os.Stat(resolved)
	if err != nil {
		return "", "", 0, fmt.Errorf("invalid target %s: %w", abs, err)
	}
	if st.IsDir() {
		return resolved, "", 0, nil
	}
	if !st.Mode().IsRegular() {
		return "", "", 0, fmt.Errorf("not a regular file or directory: %s", abs)
	}

	parentDir := filepath.Dir(resolved)

	// If the file is inside a git repository, use the git repo root.
	if info := gitProbe(parentDir); info.ok && info.toplevel != "" {
		if rel, err := filepath.Rel(info.toplevel, resolved); err == nil && !strings.HasPrefix(rel, "..") && rel != "." {
			return info.toplevel, filepath.ToSlash(rel), line, nil
		}
	}

	// If the target was specified as a relative path within the current working directory,
	// use the current working directory as the workspace root.
	if !filepath.IsAbs(cleanedTarget) {
		if wd, err := os.Getwd(); err == nil {
			if resolvedWd, err := filepath.EvalSymlinks(wd); err == nil {
				if rel, err := filepath.Rel(resolvedWd, resolved); err == nil && !strings.HasPrefix(rel, "..") && rel != "." {
					return resolvedWd, filepath.ToSlash(rel), line, nil
				}
			}
		}
	}

	return parentDir, filepath.Base(resolved), line, nil
}

// splitTargetLine separates trailing :line or :line:col from target if the candidate path exists.
func splitTargetLine(target string) (path string, line int) {
	if _, err := os.Stat(target); err == nil {
		return target, 0
	}
	lastColon := strings.LastIndex(target, ":")
	if lastColon <= 0 {
		return target, 0
	}
	vol := filepath.VolumeName(target)
	if lastColon <= len(vol) {
		return target, 0
	}
	suffix := target[lastColon+1:]
	num, err := strconv.Atoi(suffix)
	if err != nil || num <= 0 {
		return target, 0
	}
	rest := target[:lastColon]
	secondColon := strings.LastIndex(rest, ":")
	if secondColon > len(vol) {
		secondSuffix := rest[secondColon+1:]
		if lineNum, err := strconv.Atoi(secondSuffix); err == nil && lineNum > 0 {
			candidate := rest[:secondColon]
			if _, err := os.Stat(candidate); err == nil {
				return candidate, lineNum
			}
		}
	}
	if _, err := os.Stat(rest); err == nil {
		return rest, num
	}
	return target, 0
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "px0:", err)
	os.Exit(1)
}
