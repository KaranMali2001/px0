//go:build !darwin && !desktop

package main

import "fmt"

func launchDesktop(string) error {
	return fmt.Errorf("-d is supported only on macOS")
}
