//go:build desktop

package main

import (
	"io"
	"net"
	"net/http"
	"net/http/pprof"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
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
	mux := http.NewServeMux()
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	// UI tests drive the page without focusing the window: /debug/js runs a
	// script in it, and the script reports back through /debug/result.
	mux.HandleFunc("/debug/js", func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		ctx := desktopCtx()
		if r.Method != http.MethodPost || ctx == nil {
			http.Error(w, "POST a script once the window is up", http.StatusBadRequest)
			return
		}
		benchResult.Store([]byte{})
		wruntime.WindowExecJS(ctx, string(b))
	})
	mux.HandleFunc("/debug/result", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(benchResult.Load().([]byte))
	})
	benchResult.Store([]byte{})
	benchEnabled.Store(true)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
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
	})
	go func() { _ = http.Serve(ln, mux) }()
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

// The page's CSP only allows its own origin, so scripts run by /debug/js
// report back through benchResultPath on the webview's origin.
const benchResultPath = "/__px0bench/result"

var (
	benchEnabled atomic.Bool
	benchResult  atomic.Value
)

// serveBenchResult handles benchResultPath when the bench listener is on.
func serveBenchResult(w http.ResponseWriter, r *http.Request) bool {
	if !benchEnabled.Load() || r.URL.Path != benchResultPath || r.Method != http.MethodPost {
		return false
	}
	b, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	benchResult.Store(b)
	return true
}

func benchNoLSP() bool { return os.Getenv("PX0_BENCH_NO_LSP") == "1" }
