package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestSearchContextCancellation(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 20; i++ {
		content := "package main\n\nfunc SearchTarget() {}\n"
		if err := os.WriteFile(filepath.Join(root, "file_"+string(rune('a'+i))+".go"), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	ix := NewIndex(root)
	ix.Build()

	// 1. Normal search should find all files
	res, _, err := SearchContext(context.Background(), ix, SearchOpts{Query: "SearchTarget"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res) != 20 {
		t.Fatalf("expected 20 file hits, got %d", len(res))
	}

	// 2. Search with already canceled context returns immediately
	ctxCanceled, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err = SearchContext(ctxCanceled, ix, SearchOpts{Query: "SearchTarget"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}

	// 3. Search cancelled while in-flight terminates worker goroutines
	ctxTimed, cancelTimed := context.WithTimeout(context.Background(), 10*time.Microsecond)
	defer cancelTimed()
	_, _, err = SearchContext(ctxTimed, ix, SearchOpts{Query: "SearchTarget"})
	if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected cancellation or deadline error, got %v", err)
	}
}

func TestHandleSearchWithCanceledRequest(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n\nfunc Hello() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ix := NewIndex(root)
	ix.Build()
	s := NewServer(ix, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/search?q=Hello", nil)
	ctx, cancel := context.WithCancel(req.Context())
	cancel() // cancel immediately to simulate aborted request
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	s.handleSearch(rec, req)

	// Since context was canceled, handleSearch should return cleanly without writing a 400 error
	if rec.Code == http.StatusBadRequest {
		t.Fatalf("expected canceled search to return quietly, got status %d", rec.Code)
	}
}

func TestSearchLargeFilesStreamed(t *testing.T) {
	root := t.TempDir()
	write := func(name string, b []byte) {
		if err := os.WriteFile(filepath.Join(root, name), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	line := []byte(strings.Repeat("x", 63) + "\n")
	filler := bytes.Repeat(line, (rejectChunk*2)/len(line))

	// The needle straddles the first chunk boundary, in a different case.
	straddle := append([]byte{}, filler[:rejectChunk-3]...)
	straddle = append(straddle, "NeedleWord\n"...)
	straddle = append(straddle, filler...)
	write("straddle.txt", straddle)
	write("none.txt", filler)
	bin := append([]byte{0}, filler...)
	write("binary.txt", append(bin, "needleword\n"...))
	write("small.txt", []byte("a needleword here\n"))

	ix := NewIndex(root)
	ix.Build()
	res, _, err := Search(ix, SearchOpts{Query: "needleword"})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]int{}
	for _, f := range res {
		got[f.Path] = f.Matches[0].Line
	}
	wantLine := bytes.Count(straddle[:rejectChunk-3], []byte("\n")) + 1
	if len(got) != 2 || got["small.txt"] != 1 || got["straddle.txt"] != wantLine {
		t.Fatalf("got %v, want small.txt:1 and straddle.txt:%d", got, wantLine)
	}

	res, _, err = Search(ix, SearchOpts{Query: "needleword", Case: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 1 || res[0].Path != "small.txt" {
		t.Fatalf("case-sensitive search must skip NeedleWord, got %+v", res)
	}
}

// Rejecting large non-matching files must not allocate a buffer per file.
func TestSearchLargeFilesRejectWithoutAllocating(t *testing.T) {
	root := t.TempDir()
	big := bytes.Repeat([]byte(strings.Repeat("y", 63)+"\n"), (2<<20)/64)
	for i := range 8 {
		if err := os.WriteFile(filepath.Join(root, fmt.Sprintf("big%d.txt", i)), big, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	ix := NewIndex(root)
	ix.Build()
	search := func() {
		if _, _, err := Search(ix, SearchOpts{Query: "zzqqxx_no_such_token"}); err != nil {
			t.Fatal(err)
		}
	}
	search()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	search()
	runtime.ReadMemStats(&after)
	// Whole-file reads would allocate 8 x 2 MB twice (read + case fold).
	if got := after.TotalAlloc - before.TotalAlloc; got > 4<<20 {
		t.Fatalf("search over 16 MB of non-matching files allocated %d bytes", got)
	}
}
