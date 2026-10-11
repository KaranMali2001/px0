//go:build desktop

package main

import (
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
)

// startBenchListener lets benchmark-desktop.sh drive the real app process with
// benchmark.sh. Opt-in via PX0_BENCH_PORT, loopback only, and requests are not
// marked trusted, so they get exactly what the web server would serve.
func startBenchListener(h *deskHandler) {
	port := os.Getenv("PX0_BENCH_PORT")
	if port == "" {
		return
	}
	ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", port))
	if err != nil {
		fatal(err)
	}
	go func() {
		_ = http.Serve(ln, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			select {
			case <-h.ready:
			case <-r.Context().Done():
				return
			}
			s := h.cur.Load()
			if s == nil {
				http.Error(w, `{"error":"no workspace open"}`, http.StatusServiceUnavailable)
				return
			}
			s.ServeHTTP(w, r)
		}))
	}()
	// The harness stops each run with SIGTERM; tear down like a normal quit so
	// language servers do not outlive the app.
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-stop
		h.shutdown()
		os.Exit(0)
	}()
}

func benchNoLSP() bool { return os.Getenv("PX0_BENCH_NO_LSP") == "1" }
