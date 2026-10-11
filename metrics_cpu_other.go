//go:build !darwin && !linux && !freebsd && !openbsd && !netbsd

package main

import (
	"errors"
	"time"
)

func readProcessCPUTime() (time.Duration, error) { return 0, errors.New("CPU time unavailable") }
