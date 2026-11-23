package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// TestPreviewReaderReadWindow checks the Live Grep preview window is bounded around the
// hit line, reports the first line, and honors context cancellation.
func TestPreviewReaderReadWindow(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "big.txt")
	var b strings.Builder
	for i := 1; i <= 100; i++ {
		b.WriteString("line ")
		b.WriteString(strings.Repeat("x", i%7))
		b.WriteString("\n")
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	data, first, truncated, err := previewReader{}.ReadWindow(context.Background(), path, 50, 4096)
	if err != nil {
		t.Fatalf("ReadWindow: %v", err)
	}
	if first != 45 {
		t.Fatalf("firstLine = %d, want 45 (five before the hit)", first)
	}
	lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	if len(lines) != 12 {
		t.Fatalf("window lines = %d, want 12", len(lines))
	}
	if !truncated {
		t.Fatal("a 100-line file's middle window is not marked truncated")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, _, err := (previewReader{}).ReadWindow(ctx, path, 50, 4096); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled ReadWindow err = %v, want context.Canceled", err)
	}
}

func TestPreviewHighlighterWindowedAndMultibyte(t *testing.T) {
	t.Parallel()
	lines := make([]string, 400)
	lines[0] = "package main"
	for i := 1; i < len(lines); i++ {
		lines[i] = `var x = "hé"`
	}
	// Multi-line comment opened inside the 60-line context above line 250.
	lines[230] = "/* open"
	lines[255] = "end */"
	lines[256] = `var y = "日本😀" // c`
	text := strings.Join(lines, "\n") + "\n"

	var cache previewHighlighterCache
	spans := cache.spans("/w/main.go", text, lines, 250, 10)
	if len(spans) == 0 {
		t.Fatal("expected syntax spans in windowed preview")
	}
	var sawLine250Comment, sawLine256String, sawLine256Comment bool
	for _, s := range spans {
		if s.Line < 250 || s.Line >= 260 {
			t.Fatalf("span outside requested window [250,260): %+v", s)
		}
		if s.Line == 250 && s.Kind == "comment" {
			sawLine250Comment = true
		}
		if s.Line == 256 && s.Kind == "string" {
			// `"日本😀"` starts at rune col 8 (`var y = `) and ends at rune col 13 (`"日本😀"`).
			if s.Start == 8 && s.End == 13 {
				sawLine256String = true
			}
		}
		if s.Line == 256 && s.Kind == "comment" {
			// `// c` starts at rune col 14 and ends at 18.
			if s.Start == 14 && s.End == 18 {
				sawLine256Comment = true
			}
		}
	}
	if !sawLine250Comment {
		t.Fatalf("context above window did not carry multi-line comment to line 250: %+v", spans)
	}
	if !sawLine256String || !sawLine256Comment {
		t.Fatalf("multibyte rune columns wrong on line 256: %+v", spans)
	}

	// Concurrent hits and misses on the cache must be race-free.
	var wg sync.WaitGroup
	for g := range 8 {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			p := "/w/main.go"
			if id%2 == 1 {
				p = "/w/other.go"
			}
			got := cache.spans(p, text, lines, 250, 10)
			if len(got) == 0 {
				t.Errorf("goroutine %d got empty spans", id)
			}
		}(g)
	}
	wg.Wait()
}
