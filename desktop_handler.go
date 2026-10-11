package main

import (
	"net/http"
	"sync"
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

	mu     sync.Mutex
	closed bool

	openMu sync.Mutex
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
// A server installed after shutdown (a slow startup open) is closed at once.
func (h *deskHandler) install(srv *Server) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		go srv.Close()
		return
	}
	if old := h.cur.Swap(srv); old != nil && old != srv {
		go old.Close()
	}
}

func (h *deskHandler) shutdown() {
	h.mu.Lock()
	h.closed = true
	s := h.cur.Swap(nil)
	h.mu.Unlock()
	if s != nil {
		s.Close()
	}
}

// openAfterStartup lets a still-running startup open finish first, so its
// late install cannot replace the folder the user just chose. Opens run one
// at a time so install order matches rememberFolder's recent-list order.
func (h *deskHandler) openAfterStartup(root string, open func(string)) {
	<-h.ready
	h.openMu.Lock()
	defer h.openMu.Unlock()
	open(root)
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
