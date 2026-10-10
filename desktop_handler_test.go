package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestDeskHandlerOpenAsyncDoesNotBlock(t *testing.T) {
	h := newDeskHandler()
	release := make(chan struct{})
	start := time.Now()
	h.openAsync(func() string { return "/nowhere" }, func(string) { <-release })
	if time.Since(start) > time.Second {
		t.Fatal("openAsync blocked on open")
	}

	done := make(chan int, 1)
	go func() {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/meta", nil))
		done <- rec.Code
	}()
	select {
	case <-done:
		t.Fatal("request served before open finished")
	case <-time.After(50 * time.Millisecond):
	}

	close(release)
	select {
	case code := <-done:
		if code != http.StatusServiceUnavailable {
			t.Fatalf("code = %d, want 503 with no server installed", code)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("request still waiting after open finished")
	}
	if h.timedOut.Load() {
		t.Fatal("timedOut set although open finished in time")
	}
}

func TestDeskHandlerWaitIsBounded(t *testing.T) {
	old := deskOpenWait
	deskOpenWait = 20 * time.Millisecond
	t.Cleanup(func() { deskOpenWait = old })

	h := newDeskHandler()
	release := make(chan struct{})
	defer close(release)
	h.openAsync(func() string { return "/nowhere" }, func(string) { <-release })

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("code = %d, want 503", rec.Code)
	}
	if !h.timedOut.Load() {
		t.Fatal("timedOut not recorded")
	}
}
