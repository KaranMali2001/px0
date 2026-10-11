//go:build darwin || linux || freebsd || openbsd || netbsd

package main

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestLSPManagerCloseCancelsInitialization(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	def := &lspServerDef{Name: "blocked", Cmd: []string{"/bin/sh", "-c", "sleep 30"}, Exts: []string{".go"}}
	m := &lspManager{root: t.TempDir(), enabled: true, ctx: ctx, cancel: cancel,
		byExt: map[string]*lspServerDef{".go": def}, clients: map[string]*lspClient{},
		starting: map[string]chan struct{}{}, failed: map[string]string{}}
	t.Cleanup(m.Close)
	done := make(chan error, 1)
	go func() { _, err := m.client(context.Background(), "blocked.go"); done <- err }()
	deadline := time.Now().Add(time.Second)
	for {
		m.mu.Lock()
		starting := len(m.starting) > 0
		m.mu.Unlock()
		if starting {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("client did not start")
		}
		time.Sleep(time.Millisecond)
	}
	closed := make(chan struct{})
	go func() { m.Close(); close(closed) }()
	select {
	case <-closed:
	case <-time.After(3 * time.Second):
		t.Fatal("shutdown did not cancel initialization")
	}
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("client error = %v", err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.starting) != 0 || len(m.clients) != 0 {
		t.Fatal("closed manager retained a client/spawn")
	}
}
