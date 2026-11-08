package editor

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/buffer/buffertest"
)

// largeOptions returns options with large file mode on from 1 KiB and a tiny
// geometry, so that a file of a few KiB spans many chunks and index batches.
func largeOptions() Options {
	return Options{
		LargeFileMode:           true,
		LargeFileThresholdBytes: 1024,
		indexBatch:              64,
		largeGeom:               buffer.LargeOptions{Chunk: 16, CachePages: 2, MaxLine: 64, InitialBytes: 32},
	}
}

// lineText returns n lines "line 0000\n", "line 0001\n"... as one text. The last
// line has a line break.
func lineText(n int) string {
	var sb strings.Builder
	for i := range n {
		fmt.Fprintf(&sb, "line %04d\n", i)
	}
	return sb.String()
}

// writeLargeFile writes content to name in a new temp dir and returns its path.
func writeLargeFile(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// openLarge opens content as a file in an editor built from opts and returns the
// editor and the opened document.
func openLarge(t *testing.T, opts Options, content string) (*Editor, *Document) {
	t.Helper()
	e := New(opts)
	if err := e.OpenFile(writeLargeFile(t, "big.txt", content)); err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	return e, e.Active()
}

// withSource returns opts with the bytes of every large file read from src, not the disk.
func withSource(opts Options, src buffer.Source) Options {
	opts.openSrc = &sourceOpener{Open: func(string) (buffer.Source, error) { return src, nil }}
	return opts
}

// openMem opens a file of content whose bytes are read from a MemSource, which
// the test can fail, truncate or count.
func openMem(t *testing.T, opts Options, content string) (*Editor, *Document, *buffertest.MemSource) {
	t.Helper()
	src := buffertest.NewMemSource([]byte(content))
	e := New(withSource(opts, src))
	if err := e.OpenFile(writeLargeFile(t, "big.txt", content)); err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	return e, e.Active(), src
}

// indexStep runs one index job, if there is one, and applies it. It reports whether
// a job ran and whether another is needed.
func indexStep(e *Editor) (ran, more bool) {
	job, ok := e.PlanIndex()
	if !ok {
		return false, false
	}
	return true, e.ApplyIndex(job.Run(context.Background()))
}

// indexAll runs index jobs until none is needed and returns how many ran.
func indexAll(t *testing.T, e *Editor) int {
	t.Helper()
	steps := 0
	for {
		ran, more := indexStep(e)
		if ran {
			steps++
		}
		if !more {
			return steps
		}
		if steps > 100000 {
			t.Fatal("the index does not complete")
		}
	}
}
