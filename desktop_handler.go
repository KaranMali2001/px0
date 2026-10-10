package main

import (
	"net/http"
	"sync/atomic"
	"time"
)

// deskHandler routes every webview request to the current workspace's *Server.
// Swapped atomically when the user opens a different folder.
type deskHandler struct {
	cur   atomic.Pointer[Server]
	ready chan struct{} // closed once the startup workspace open has finished
	// timedOut records that a request gave up waiting on ready, so the
	// window must reload once the open finishes.
	timedOut atomic.Bool
}

// deskOpenWait caps how long a webview request waits for the startup open.
// Holding requests (rather than 503ing them) means the unchanged web UI boots
// against a ready server; the cap keeps a wedged open from hanging the page.
var deskOpenWait = 30 * time.Second

func newDeskHandler() *deskHandler {
	return &deskHandler{ready: make(chan struct{})}
}

// openAsync resolves and opens the startup root off the caller's goroutine so
// wails.Run (and the window) never waits on filesystem or git work.
func (h *deskHandler) openAsync(resolve func() string, open func(string)) {
	go func() {
		defer close(h.ready)
		if root := resolve(); root != "" {
			open(root)
		}
	}()
}

// install makes srv the active workspace. The old server is closed off the
// caller's goroutine because LSP shutdown can take a few seconds.
func (h *deskHandler) install(srv *Server) {
	if old := h.cur.Swap(srv); old != nil && old != srv {
		go old.Close()
	}
}

// shutdown closes the active workspace when the app quits.
func (h *deskHandler) shutdown() {
	if s := h.cur.Swap(nil); s != nil {
		s.Close()
	}
}

func (h *deskHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	select {
	case <-h.ready:
	case <-r.Context().Done():
		return
	case <-time.After(deskOpenWait):
		h.timedOut.Store(true)
	}
	s := h.cur.Load()
	if s == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error":"no workspace open"}`))
		return
	}
	s.ServeHTTP(w, trustedDesktopRequest(r))
}
