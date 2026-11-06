package app

import (
	"fmt"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// TestBigFileAcceptance is the manual acceptance of large file mode, never run in CI:
// it skips unless GVIM_BIG_FILE names a UTF-8 text file with many lines (a file of
// about 2 GB is the target). It opens the file with the real runtime and the default
// settings, and logs the time to the first screen, to a complete line index, to a random
// line, to a goto and an edit, to a save, and the memory after each step. The file IS
// edited and saved, so pass a copy that can be thrown away.
//
//	GVIM_BIG_FILE=/tmp/gvim-big/big.txt go test ./internal/app -run TestBigFileAcceptance -v -timeout 30m
func TestBigFileAcceptance(t *testing.T) {
	path := os.Getenv("GVIM_BIG_FILE")
	if path == "" {
		t.Skip("set GVIM_BIG_FILE to a big text file to run the acceptance")
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	size := fi.Size()
	peak := new(atomic.Int64)
	stop := make(chan struct{})
	go func() {
		for {
			if kb, ok := rssKB(); ok && kb > peak.Load() {
				peak.Store(kb)
			}
			select {
			case <-stop:
				return
			case <-time.After(10 * time.Millisecond):
			}
		}
	}()
	t.Cleanup(func() {
		close(stop)
		t.Logf("peak rss over the run %.1f MiB", float64(peak.Load())/1024)
	})
	mem := func(step string) {
		var ms goruntime.MemStats
		goruntime.GC()
		goruntime.ReadMemStats(&ms)
		t.Logf("memory after %-12s heap %6.1f MiB, runtime total %6.1f MiB, rss %s",
			step, float64(ms.HeapAlloc)/(1<<20), float64(ms.Sys)/(1<<20), rss())
	}
	timed := func(step string, f func()) time.Duration {
		start := time.Now()
		f()
		d := time.Since(start)
		t.Logf("%-34s %v", step, d)
		return d
	}

	// Open to the first screen: the runtime, a window, one frame.
	root := filepath.Dir(path)
	isolateConfig(t)
	var rt *runtime
	var first string
	var cmd tea.Cmd
	timed("open to first screen", func() {
		r, err := buildRuntime(t.Context(), Options{Root: root, Files: []FileTarget{{Path: path}}}, false)
		if err != nil {
			t.Fatal(err)
		}
		rt = r
		_, cmd = rt.model.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
		first = ansi.Strip(rt.model.View().Content)
	})
	t.Cleanup(func() { rt.close(); rt.ed.Close() })
	d := rt.ed.Active()
	if !d.Large() {
		t.Fatalf("%d bytes did not open in large file mode", size)
	}
	t.Logf("file %d bytes; first screen has %d rows; status chip LARGE: %v",
		size, strings.Count(first, "\n")+1, strings.Contains(first, "LARGE"))
	mem("first screen")

	// Index to the end, as the root does it, one batch per command.
	steps := 0
	timed("index to the last line", func() {
		for cmd != nil {
			_, cmd = rt.model.Update(cmd())
			steps++
		}
	})
	info := d.Index()
	t.Logf("index: complete %v, %d lines, %d steps", info.Complete, info.Lines, steps)
	if !info.Complete {
		t.Fatal("the index did not complete")
	}
	mem("index")

	// Random lines, then a goto and an edit, each followed by a frame.
	rng := rand.New(rand.NewPCG(5, 6))
	const probes = 20000
	dur := timed(fmt.Sprintf("%d random Line(i)", probes), func() {
		for range probes {
			_ = d.Buffer.Line(rng.IntN(info.Lines))
		}
	})
	t.Logf("random Line(i): %v each", dur/probes)
	mid := info.Lines/2 + 1
	timed(fmt.Sprintf("goto line %d (cold)", mid), func() { must(t, rt.ed.GotoLine(mid, 1)) })
	timed("frame at that line (cold)", func() { rt.model.View() })
	const warmRuns = 1000
	dur = timed(fmt.Sprintf("%d x goto line %d (warm)", warmRuns, mid), func() {
		for range warmRuns {
			must(t, rt.ed.GotoLine(mid, 1))
		}
	})
	t.Logf("warm goto: %v each", dur/warmRuns)
	timed("frame at that line (warm)", func() { rt.model.View() })
	timed("goto the last line + frame", func() {
		must(t, rt.ed.GotoLine(info.Lines, 1))
		rt.model.View()
	})
	must(t, rt.ed.GotoLine(mid, 1))
	timed("type a character + frame", func() {
		rt.ed.Do("insert_char", map[string]any{"text": "Q"})
		rt.model.View()
	})
	mem("edit")
	timed("undo + redo + frame", func() {
		rt.ed.Do("undo", nil)
		rt.ed.Do("redo", nil)
		rt.model.View()
	})

	// Save streams the whole file next to itself and renames it over.
	timed("save", func() { must(t, rt.ed.Save()) })
	if d.Modified() {
		t.Error("the document is modified after the save")
	}
	if _, ok := d.Buffer.Undo(); ok {
		t.Error("undo has history after the save")
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("size %d -> %d bytes (one character typed)", size, after.Size())
	mem("save")
	timed("frame after the save", func() { rt.model.View() })
	st := d.Buffer.Stats()
	t.Logf("buffer stats: %+v", st)
	if st.OverCap != 0 {
		t.Errorf("OverCap = %d", st.OverCap)
	}
}

// rssKB is the resident size of this process in KiB, as ps reports it.
func rssKB() (int64, bool) {
	out, err := exec.Command("ps", "-o", "rss=", "-p", strconv.Itoa(os.Getpid())).Output()
	if err != nil {
		return 0, false
	}
	kb, err := strconv.ParseInt(strings.TrimSpace(string(out)), 10, 64)
	return kb, err == nil
}

// rss is the resident size of this process as text, or "?" where there is no ps.
func rss() string {
	kb, ok := rssKB()
	if !ok {
		return "?"
	}
	return fmt.Sprintf("%.1f MiB", float64(kb)/1024)
}
