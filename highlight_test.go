package main

import (
	"container/list"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
)

func TestTypeScriptMethodMarkupPreservesSource(t *testing.T) {
	strip := regexp.MustCompile(`<[^>]*>`)
	for _, src := range []string{
		"  i() {",
		"  s() {",
		"  async s() {",
		"  private i() {",
		"  static staticMethod() {",
		"  get getValue() {",
	} {
		t.Run(src, func(t *testing.T) {
			d := newDoc(src+"\n", "methods.ts")
			line := d.tokenise(d.src, d.Total)[0]
			if got := html.UnescapeString(strip.ReplaceAllString(line, "")); got != src {
				t.Fatalf("source changed: %q, markup %q", got, line)
			}
			if strings.Count(line, `<i class=nf>`) != 1 {
				t.Fatalf("method not styled exactly once: %q", line)
			}
		})
	}
	for _, src := range []string{"// i() {", "const value = 's() {'", "/*\n  i() {\n*/"} {
		d := newDoc(src, "comments.ts")
		for _, line := range d.tokenise(src, d.Total) {
			if strings.Contains(line, `<i class=nf>`) {
				t.Fatalf("styled a comment/string: %q", line)
			}
		}
	}
}

func TestLineOffsetsPreserveEmptyAndUnicodeLines(t *testing.T) {
	for _, src := range []string{"", "\n", "one\n", "one\n\nthree", "é\n\t世界"} {
		d := newDoc(src, "plain.txt")
		lines := strings.Split(src, "\n")
		if d.Total != len(lines) {
			t.Fatalf("total = %d, want %d", d.Total, len(lines))
		}
		for i, line := range lines {
			if d.Raw(i+1) != line {
				t.Fatalf("line %d = %q, want %q", i+1, d.Raw(i+1), line)
			}
		}
	}
}

func TestHighlightCacheRejectsOversizedNewestDocument(t *testing.T) {
	c := &hlCache{ll: list.New(), items: map[string]*list.Element{}}
	d := &Doc{bytes: cacheBudget + 1}
	c.put("oversized", d)
	if c.used > cacheBudget || c.get("oversized") != nil {
		t.Fatal("cache retained oversized newest document")
	}
	if d.bytes != cacheBudget+1 {
		t.Fatal("eviction changed active request's document")
	}
}

func TestCacheCoalescesConcurrentOpenDocuments(t *testing.T) {
	abs := filepath.Join(t.TempDir(), "concurrent.go")
	if err := os.WriteFile(abs, []byte("package main\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { Evict(abs) })
	var wg sync.WaitGroup
	docs := make(chan *Doc, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			d, err := Open(abs, "concurrent.go")
			if err != nil {
				t.Error(err)
				return
			}
			docs <- d
		}()
	}
	wg.Wait()
	close(docs)
	var first *Doc
	for d := range docs {
		if first == nil {
			first = d
		}
		if d != first {
			t.Fatal("concurrent opens returned uncached duplicates")
		}
	}
}

func TestRetiredDocumentDoesNotRefine(t *testing.T) {
	d := newDoc("package main\n", "retired.go")
	d.retire()
	d.backgroundPass()
	if done, coming := d.Exact(); done || coming {
		t.Fatal("retired document still scheduled refinement")
	}
}

func TestTypeScriptMethodNamesAreHighlighted(t *testing.T) {
	d := newDoc("  async getPaymentById(paymentId: number) {\n    return this.paymentDao.getPaymentById(paymentId)\n  }\n", "PaymentService.ts")
	lines := d.tokenise(d.src, d.Total)
	if !strings.Contains(lines[0], `<i class=nf>getPaymentById</i>`) {
		t.Fatalf("method declaration was not highlighted: %q", lines[0])
	}
	if strings.Contains(lines[1], `<i class=nf>getPaymentById</i>`) {
		t.Fatalf("method call must remain unstyled: %q", lines[1])
	}
}

// Windowed lexing must agree with a full-file pass, or the view changes under
// the user when the background pass lands.
func fixtures(t *testing.T) []string {
	var files []string
	root := os.Getenv("PX0_FIXTURES")
	if root == "" {
		root = "."
	}
	filepath.WalkDir(root, func(p string, de os.DirEntry, err error) error {
		if err != nil || de.IsDir() {
			return nil
		}
		switch strings.ToLower(filepath.Ext(p)) {
		case ".go", ".py", ".js", ".ts", ".c", ".h", ".rs", ".java", ".css", ".html", ".md", ".sh", ".json", ".yaml":
			if st, err := os.Stat(p); err == nil && st.Size() > 400 && st.Size() < 3<<20 {
				files = append(files, p)
			}
		}
		return nil
	})
	if len(files) > 400 {
		files = files[:400]
	}
	if len(files) == 0 {
		t.Skip("no fixtures")
	}
	return files
}

func TestWindowMatchesFullPass(t *testing.T) {
	files := fixtures(t)
	var checked, mismatchFiles, mismatchLines, totalLines int
	for _, p := range files {
		data, err := os.ReadFile(p)
		if err != nil || isBinary(data) {
			continue
		}
		src := strings.ReplaceAll(string(data), "\r\n", "\n")
		d := newDoc(src, p)
		if d.Total < hlChunk+2 {
			continue // single chunk: nothing to compare
		}
		checked++
		full := d.tokenise(src, d.Total)

		bad := 0
		for c := 0; c*hlChunk < d.Total; c++ {
			win, exact := d.chunk(c)
			if exact {
				t.Fatalf("%s: chunk %d claimed exact before any full pass ran", p, c)
			}
			for i, got := range win {
				line := c*hlChunk + i
				totalLines++
				if got != full[line] {
					bad++
					if bad == 1 {
						t.Logf("%s:%d\n  window: %q\n  full:   %q", p, line+1, got, full[line])
					}
				}
			}
		}
		if bad > 0 {
			mismatchFiles++
			mismatchLines += bad
		}
	}
	t.Logf("windowed vs full: %d multi-chunk files, %d lines; %d files differ, %d lines differ (%.4f%%)",
		checked, totalLines, mismatchFiles, mismatchLines, float64(mismatchLines)*100/float64(max(totalLines, 1)))
	// Windows are an approximation by construction: a string or comment longer
	// than hlContext cannot be resolved from a window alone. Hold the line at a
	// small fraction so a regression in context handling still shows up.
	if totalLines > 0 && float64(mismatchLines)/float64(totalLines) > 0.01 {
		t.Errorf("windowed lexing drifts too far from the full pass: %d/%d lines", mismatchLines, totalLines)
	}
}

// Whatever the windows guessed, the background pass is authoritative and every
// chunk must match it once that pass has run.
func TestBackgroundPassIsExact(t *testing.T) {
	for _, p := range fixtures(t) {
		data, err := os.ReadFile(p)
		if err != nil || isBinary(data) {
			continue
		}
		src := strings.ReplaceAll(string(data), "\r\n", "\n")
		d := newDoc(src, p)
		if d.Total < hlChunk+2 {
			continue
		}
		d.chunk(0) // seed a window first, so the pass has something to overwrite
		d.backgroundPass()
		full := d.tokenise(src, d.Total)
		for c := 0; c*hlChunk < d.Total; c++ {
			win, exact := d.chunk(c)
			if !exact {
				t.Fatalf("%s: chunk %d not exact after background pass", p, c)
			}
			for i, got := range win {
				if line := c*hlChunk + i; got != full[line] {
					t.Fatalf("%s:%d after background pass\n got %q\nwant %q", p, line+1, got, full[line])
				}
			}
		}
	}
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// Windows and the background pass race on the same chunk map by design.
func TestConcurrentChunkAccess(t *testing.T) {
	var sb strings.Builder
	for i := 0; i < 5000; i++ {
		sb.WriteString("func f() { /* c */ s := `raw` }\n")
	}
	d := newDoc(sb.String(), "x.go")

	done := make(chan struct{})
	go func() { d.backgroundPass(); close(done) }()

	errs := make(chan error, 64)
	var wg sync.WaitGroup
	for w := 0; w < 8; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < 40; i++ {
				start := ((w*13 + i*7) % 5) * hlChunk
				lines, _ := d.Lines(start, start+hlChunk)
				if len(lines) == 0 {
					errs <- fmt.Errorf("empty window at %d", start)
					return
				}
				for _, l := range lines {
					if !strings.Contains(l, "func") {
						errs <- fmt.Errorf("garbled line at %d: %q", start, l)
						return
					}
				}
			}
		}(w)
	}
	wg.Wait()
	<-done
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
}

// Generated files exist with a few multi-megabyte lines. A window measured in
// lines would cover the whole file there, so the byte cap has to take over.
func TestGiantLinesDoNotBlowUpTheWindow(t *testing.T) {
	var sb strings.Builder
	sb.WriteString("package x\n")
	for i := 0; i < 8; i++ {
		sb.WriteString("var s")
		sb.WriteString(strings.Repeat("0", 1))
		sb.WriteString(" = \"")
		sb.WriteString(strings.Repeat("abcdefgh", 200_000)) // 1.6 MB on one line
		sb.WriteString("\"\n")
	}
	src := sb.String()
	d := newDoc(src, "giant.go")

	if d.MaxCols > maxReportedCols {
		t.Errorf("MaxCols = %d, must be capped at %d so the client can size itself", d.MaxCols, maxReportedCols)
	}

	lines, _ := d.Lines(0, d.Total)
	if len(lines) != d.Total {
		t.Fatalf("got %d lines, want %d", len(lines), d.Total)
	}
	// Content must survive even though this window is not highlighted.
	if !strings.Contains(lines[0], "package") {
		t.Errorf("first line lost its content: %q", lines[0])
	}
	// The last element is the empty string after the file's final newline.
	for i, l := range lines[1 : len(lines)-1] {
		if len(l) == 0 {
			t.Errorf("line %d came back empty", i+2)
			break
		}
	}
	if lines[len(lines)-1] != "" {
		t.Errorf("trailing line = %q, want empty", lines[len(lines)-1])
	}
	// The whole point: no chunk may hold vastly more than the source it covers.
	total := 0
	for _, l := range lines {
		total += len(l)
	}
	if total > 4*len(src) {
		t.Errorf("rendered %d bytes for %d bytes of source", total, len(src))
	}
}

func TestEvictRemovesDocument(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "test.go")
	if err := os.WriteFile(tmp, []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	d, err := Open(tmp, "test.go")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	if d == nil {
		t.Fatal("expected doc, got nil")
	}
	cache.mu.Lock()
	usedBefore := cache.used
	_, foundBefore := cache.items[d.key]
	cache.mu.Unlock()
	if !foundBefore || usedBefore == 0 {
		t.Fatalf("expected doc in cache, found=%v used=%d", foundBefore, usedBefore)
	}

	if !Evict(tmp) {
		t.Fatalf("expected Evict(%q) to return true", tmp)
	}

	cache.mu.Lock()
	usedAfter := cache.used
	_, foundAfter := cache.items[d.key]
	cache.mu.Unlock()
	if foundAfter {
		t.Fatalf("expected doc to be removed from cache")
	}
	if usedAfter >= usedBefore {
		t.Fatalf("expected used bytes to decrease, before=%d after=%d", usedBefore, usedAfter)
	}

	// Evict again on already evicted file returns false
	if Evict(tmp) {
		t.Fatalf("expected second Evict to return false")
	}
}
