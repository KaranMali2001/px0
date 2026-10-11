package main

import (
	"context"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

var rssSample struct {
	sync.Mutex
	at    time.Time
	bytes uint64
	err   error
}

func readProcessRSS() (uint64, error) {
	rssSample.Lock()
	defer rssSample.Unlock()
	if time.Since(rssSample.at) < time.Second {
		return rssSample.bytes, rssSample.err
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/bin/ps", "-o", "rss=", "-p", strconv.Itoa(os.Getpid()))
	cmd.WaitDelay = 100 * time.Millisecond
	out, err := cmd.Output()
	var kb uint64
	if err == nil {
		kb, err = strconv.ParseUint(strings.TrimSpace(string(out)), 10, 64)
	}
	rssSample.at, rssSample.bytes, rssSample.err = time.Now(), kb*1024, err
	return rssSample.bytes, rssSample.err
}
