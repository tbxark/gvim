package bundled_test

import (
	"io/fs"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/tbxark/gvim/internal/plugin/bundled"
	"github.com/tbxark/gvim/internal/plugin/plugintest"
	"github.com/tbxark/gvim/internal/pluginapi"
)

// newMCRealClock is merge_conflict with the real clock and a plugin timeout, so an event
// slice gets the 50 ms budget it has in the editor (min(50 ms, timeout)).
func newMCRealClock(t *testing.T, text string, timeout time.Duration) *mcEnv {
	t.Helper()
	src, err := fs.ReadFile(bundled.FS, "plugins/merge_conflict/init.lua")
	if err != nil {
		t.Fatal(err)
	}
	host := plugintest.NewFakeHost()
	id := host.AddBuffer("/w/a.txt", text)
	h := plugintest.New(t, plugintest.Files{"merge_conflict/init.lua": string(src)},
		plugintest.Options{Host: host, Timeout: timeout, Now: time.Now})
	h.Start()
	return &mcEnv{h: h, buf: id, b: host.Buffers[0]}
}

func timedOut(e *mcEnv) []string {
	var out []string
	for _, l := range e.h.LogText("merge_conflict") {
		if strings.Contains(l, "timed out") || strings.Contains(l, "slow host call") {
			out = append(out, l)
		}
	}
	return out
}

// killedScan opens the buffer while the editor is slow to give lines, so the handler is
// killed in the middle of its scan, then makes the editor fast again.
func killedScan(t *testing.T) *mcEnv {
	t.Helper()
	e := newMCRealClock(t, mcTwo, 100*time.Millisecond)
	e.h.Host.LinesDelay = 150 * time.Millisecond
	e.open()
	e.h.Host.LinesDelay = 0
	if len(timedOut(e)) == 0 {
		t.Fatalf("the scan was not killed; log: %v", e.h.LogText("merge_conflict"))
	}
	return e
}

// TestMergeConflictRecoversFromAKilledScan: a handler killed by the slice budget (a timeout
// or a slow host call) cannot clear its busy flag. The next event or command must not find
// the buffer "still scanning" forever.
func TestMergeConflictRecoversFromAKilledScan(t *testing.T) {
	t.Parallel()
	t.Run("a command", func(t *testing.T) {
		t.Parallel()
		e := killedScan(t)
		e.setCaret(2)
		if err := e.h.Run("merge_conflict.use_ours", nil); err != nil {
			t.Fatal(err)
		}
		if got := e.lastNotice(); strings.Contains(got, "still scanning") {
			t.Fatalf("notice = %q", got)
		}
		if got, want := e.b.Lines[1], "ours1"; got != want {
			t.Errorf("line 1 = %q, want %q: the conflict was not resolved (notice %q)", got, want, e.lastNotice())
		}
	})
	t.Run("an activation", func(t *testing.T) {
		t.Parallel()
		e := killedScan(t)
		e.h.Emit(pluginapi.Event{Name: "buffer_activated", Data: map[string]any{"buf": e.buf}})
		if got, want := e.markerLines(), oracleMarkerLines(e.b.Lines); !slices.Equal(got, want) {
			t.Errorf("marker lines = %v, want %v", got, want)
		}
	})
	t.Run("an edit", func(t *testing.T) {
		t.Parallel()
		e := killedScan(t)
		edit := ins(7, 0, "<<<<<<< a\nx\n=======\ny\n>>>>>>> b\n")
		e.apply(edit)
		e.h.Emit(pluginapi.Event{Name: "buffer_changed", Data: map[string]any{
			"buf": e.buf, "version": e.b.Info.Version,
			"edits": []pluginapi.ChangeRec{{Range: edit.Range, Text: edit.Text}},
		}})
		// The plugin debounces buffer_changed on the clock of the runtime, which is the real one.
		time.Sleep(310 * time.Millisecond)
		e.h.Advance(300 * time.Millisecond)
		if got, want := e.markerLines(), oracleMarkerLines(e.b.Lines); !slices.Equal(got, want) || len(got) == 0 {
			t.Errorf("marker lines = %v, want %v", got, want)
		}
	})
}

// TestMergeConflictScanFitsTheEventBudget: with the real clock and the 50 ms event budget a
// buffer of thousands of markers is scanned without a timeout, and every FindLines chunk is
// bounded. The budget is wall-clock time, so a scheduler stall on a loaded machine can kill a
// slice that is fine: the test fails only when ten runs in a row time out, which a slice
// that really costs more than the budget does every time.
func TestMergeConflictScanFitsTheEventBudget(t *testing.T) {
	t.Parallel()
	const conflicts = 2000 // 6000 markers in 10 000 lines
	var sb strings.Builder
	for i := 0; i < conflicts; i++ {
		sb.WriteString("<<<<<<< HEAD\nours\n=======\ntheirs\n>>>>>>> other\n")
	}
	text := sb.String()
	var tos []string
	var e *mcEnv
	for attempt := 0; attempt < 10; attempt++ {
		e = newMCRealClock(t, text, 500*time.Millisecond)
		e.open()
		e.drain()
		if tos = timedOut(e); len(tos) == 0 {
			break
		}
	}
	if len(tos) != 0 {
		t.Fatalf("handlers timed out in ten runs: %v", tos)
	}
	if got := len(e.markerLines()); got != 3000 { // the first 1000 conflicts are decorated
		t.Errorf("decorated marker lines = %d, want 3000 (log %v)", got, e.h.LogText("merge_conflict"))
	}
	for _, q := range e.findLinesCalls() {
		if q.Max <= 0 || q.Max > 1000 {
			t.Fatalf("FindLines max = %d, want a bounded chunk", q.Max)
		}
	}
}
