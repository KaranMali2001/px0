//go:build !darwin && !linux

package main

import "errors"

func readProcessRSS() (uint64, error) { return 0, errors.New("RSS unavailable") }
