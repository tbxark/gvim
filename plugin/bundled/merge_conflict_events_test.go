package bundled_test

import (
	"math/rand"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/tbxark/gvim/pluginapi"
)

// oracleMarkerLines is the specification of what merge_conflict decorates, written the
// obvious way over the whole text: the lines of the markers of every complete conflict.
func oracleMarkerLines(lines []string) []int {
	kind := func(s string) string {
		if len(s) < 7 {
			return ""
		}
		head, rest := s[:7], s[7:]
		switch {
		case head == "=======":
			if rest == "" {
				return "mid"
			}
			return ""
		case rest != "" && rest[0] != ' ':
			return ""
		}
		switch head {
		case "<<<<<<<":
			return "start"
		case "|||||||":
			return "base"
		case ">>>>>>>":
			return "finish"
		}
		return ""
	}
	var out []int
	start, base, mid := -1, -1, -1
	for i, l := range lines {
		switch kind(l) {
		case "start":
			start, base, mid = i, -1, -1
		case "base":
			if start >= 0 && base < 0 && mid < 0 {
				base = i
			}
		case "mid":
			if start >= 0 && mid < 0 {
				mid = i
			}
		case "finish":
			if start >= 0 && mid >= 0 {
				out = append(out, start, mid, i)
				if base >= 0 {
					out = append(out, base)
				}
				start, base, mid = -1, -1, -1
			}
		}
	}
	slices.Sort(out)
	return out
}

// change applies the edits to the host buffer one after the other and raises the one
// buffer_changed event the ui would raise for them (sequential records), then lets the
// 300 ms debounce of the plugin run out.
func (e *mcEnv) change(edits ...pluginapi.Edit) {
	e.h.T.Helper()
	e.apply(edits...)
	e.raise(edits...)
}

func (e *mcEnv) apply(edits ...pluginapi.Edit) {
	e.h.T.Helper()
	for _, ed := range edits {
		if err := e.h.Host.Edit(e.buf, []pluginapi.Edit{ed}, pluginapi.EditOptions{}); err != nil {
			e.h.T.Fatal(err)
		}
	}
}

func (e *mcEnv) raise(edits ...pluginapi.Edit) {
	e.h.T.Helper()
	recs := make([]pluginapi.ChangeRec, len(edits))
	for i, ed := range edits {
		recs[i] = pluginapi.ChangeRec{Range: ed.Range, Text: ed.Text}
	}
	e.h.Emit(pluginapi.Event{Name: "buffer_changed", Data: map[string]any{
		"buf": e.buf, "version": e.b.Info.Version, "edits": recs,
	}})
	e.h.Advance(300 * time.Millisecond)
}

func ins(line, col int, text string) pluginapi.Edit {
	p := pluginapi.Pos{Line: line, Col: col}
	return pluginapi.Edit{Range: pluginapi.Range{From: p, To: p}, Text: text}
}

func rep(l1, c1, l2, c2 int, text string) pluginapi.Edit {
	return pluginapi.Edit{Range: pluginapi.Range{
		From: pluginapi.Pos{Line: l1, Col: c1}, To: pluginapi.Pos{Line: l2, Col: c2},
	}, Text: text}
}

func (e *mcEnv) checkOracle(t *testing.T) {
	t.Helper()
	if got, want := e.markerLines(), oracleMarkerLines(e.b.Lines); !slices.Equal(got, want) {
		t.Errorf("marker lines = %v, want %v for\n%s", got, want, e.text())
	}
}

// TestMergeConflictFollowsEdits: after an edit the decorations describe the new text, and
// the rescan covers only the lines the edit touched.
func TestMergeConflictFollowsEdits(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		edits []pluginapi.Edit
		// scans are the line ranges FindLines was asked for after the edit: [from, to), to 0
		// meaning the end of the buffer.
		scans [][2]int
	}{
		{"typing far from every marker", []pluginapi.Edit{ins(15, 4, "!")}, [][2]int{{15, 16}}},
		{"lines inserted above shift the markers", []pluginapi.Edit{ins(0, 0, "x\ny\n")}, [][2]int{{0, 3}}},
		{"lines deleted above shift the markers", []pluginapi.Edit{rep(0, 0, 1, 0, "")}, [][2]int{{0, 1}}},
		{"a separator is spoiled", []pluginapi.Edit{rep(4, 0, 4, 7, "x")}, [][2]int{{4, 5}}},
		{"a marker is typed", []pluginapi.Edit{rep(7, 0, 7, 6, "=======")}, [][2]int{{7, 8}}},
		{"a whole conflict is pasted", []pluginapi.Edit{ins(7, 0, "<<<<<<< a\nx\n=======\ny\n>>>>>>> b\n")}, [][2]int{{7, 13}}},
		{"two conflicts are joined", []pluginapi.Edit{rep(6, 0, 9, 0, "")}, [][2]int{{6, 7}}},
		{"a line is joined to its marker", []pluginapi.Edit{rep(0, 4, 1, 0, " ")}, [][2]int{{0, 1}}},
		{"a marker line is split", []pluginapi.Edit{ins(4, 3, "\n")}, [][2]int{{4, 6}}},
		{"two edits in one event", []pluginapi.Edit{ins(0, 0, "a\n"), ins(10, 0, "b\nc\n")}, [][2]int{{0, 13}}},
		{"two edits far apart", []pluginapi.Edit{ins(0, 0, "a\nb\nc\n"), rep(19, 0, 19, 4, "t")}, [][2]int{{0, 20}}},
		{"the end of the buffer is deleted", []pluginapi.Edit{rep(13, 0, 15, 4, "")}, [][2]int{{13, 14}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e := newMC(t, mcTwo)
			e.open()
			before := len(e.findLinesCalls())
			e.change(tt.edits...)
			e.checkOracle(t)
			var scans [][2]int
			for _, q := range e.findLinesCalls()[before:] {
				scans = append(scans, [2]int{q.From, q.To})
			}
			if !slices.Equal(scans, tt.scans) {
				t.Errorf("FindLines ranges = %v, want %v", scans, tt.scans)
			}
			if w := e.warnings(); len(w) != 0 {
				t.Errorf("log: %v", w)
			}
		})
	}
}

func TestMergeConflictFullRescans(t *testing.T) {
	t.Parallel()
	t.Run("an overflowed event", func(t *testing.T) {
		t.Parallel()
		e := newMC(t, mcTwo)
		e.open()
		before := len(e.findLinesCalls())
		e.apply(rep(4, 0, 4, 7, "x"))
		e.h.Emit(pluginapi.Event{Name: "buffer_changed", Data: map[string]any{
			"buf": e.buf, "version": e.b.Info.Version, "overflow": true,
		}})
		e.h.Advance(300 * time.Millisecond)
		e.checkOracle(t)
		qs := e.findLinesCalls()[before:]
		if len(qs) != 1 || qs[0].From != 0 || qs[0].To != 0 {
			t.Errorf("FindLines = %+v, want one scan of the whole buffer", qs)
		}
	})
	t.Run("the buffer moved on since the event", func(t *testing.T) {
		t.Parallel()
		e := newMC(t, mcTwo)
		e.open()
		before := len(e.findLinesCalls())
		first := ins(0, 0, "x\n")
		e.apply(first)
		ver := e.b.Info.Version
		e.apply(rep(5, 0, 5, 7, "y")) // a second edit whose event has not come yet
		e.h.Emit(pluginapi.Event{Name: "buffer_changed", Data: map[string]any{
			"buf": e.buf, "version": ver, "edits": []pluginapi.ChangeRec{{Range: first.Range, Text: first.Text}},
		}})
		e.h.Advance(300 * time.Millisecond)
		e.checkOracle(t)
		qs := e.findLinesCalls()[before:]
		if len(qs) != 1 || qs[0].To != 0 {
			t.Errorf("FindLines = %+v, want one scan of the whole buffer", qs)
		}
		// The event of the second edit finds the markers already describing that text.
		e.raise(rep(5, 0, 5, 7, "y"))
		if n := len(e.findLinesCalls()) - before; n != 1 {
			t.Errorf("FindLines calls = %d, want the event of a text already scanned to cost none", n)
		}
		e.checkOracle(t)
	})
	t.Run("line counts that disagree", func(t *testing.T) {
		t.Parallel()
		e := newMC(t, mcTwo)
		e.open()
		before := len(e.findLinesCalls())
		// The record says one line was added, the buffer says two: an event was lost.
		e.apply(ins(0, 0, "x\ny\n"))
		e.h.Emit(pluginapi.Event{Name: "buffer_changed", Data: map[string]any{
			"buf": e.buf, "version": e.b.Info.Version,
			"edits": []pluginapi.ChangeRec{{Range: ins(0, 0, "").Range, Text: "x\n"}},
		}})
		e.h.Advance(300 * time.Millisecond)
		e.checkOracle(t)
		if qs := e.findLinesCalls()[before:]; len(qs) != 1 || qs[0].To != 0 {
			t.Errorf("FindLines = %+v, want one scan of the whole buffer", qs)
		}
	})
	t.Run("a change before any scan", func(t *testing.T) {
		t.Parallel()
		e := newMC(t, "a\nb")
		e.change(ins(1, 1, "\n<<<<<<< a\nx\n=======\ny\n>>>>>>> b"))
		e.checkOracle(t)
		if len(e.markerLines()) != 3 {
			t.Errorf("marker lines = %v", e.markerLines())
		}
	})
}

// TestMergeConflictOwnEditIsNotRescanned: the event of the plugin's own resolution finds
// the state already current.
func TestMergeConflictOwnEditIsNotRescanned(t *testing.T) {
	t.Parallel()
	e := newMC(t, mcTwo)
	e.open()
	e.setCaret(2)
	if err := e.h.Run("merge_conflict.use_ours", nil); err != nil {
		t.Fatal(err)
	}
	before := len(e.findLinesCalls())
	e.raise(rep(1, 0, 2, 0, ""), rep(3, 0, 6, 0, "")) // what the host would report for it
	if n := len(e.findLinesCalls()) - before; n != 0 {
		t.Errorf("FindLines calls = %d after the event of the plugin's own edit", n)
	}
	e.checkOracle(t)
}

func TestMergeConflictClosedBufferForgotten(t *testing.T) {
	t.Parallel()
	e := newMC(t, mcTwo)
	e.open()
	e.h.Emit(pluginapi.Event{Name: "buffer_closed", Data: map[string]any{"id": int(e.buf), "path": "/w/a.txt"}})
	before := len(e.findLinesCalls())
	e.open() // a new buffer could get the id of a closed one: the state must start again
	if n := len(e.findLinesCalls()) - before; n != 1 {
		t.Errorf("FindLines calls after reopening = %d, want a fresh scan", n)
	}
}

// TestMergeConflictRandomEdits drives random edits through the events and compares the
// decorations with a full scan of the text after each event.
func TestMergeConflictRandomEdits(t *testing.T) {
	t.Parallel()
	pieces := []string{
		"", "", "x", "\n", "\n\n", "<<<<<<< a\n", "=======\n", ">>>>>>> b\n", "||||||| c\n",
		"<<<<<<< a\nx\n=======\ny\n>>>>>>> b\n", "=======", "<<<<<<<", "y\n=======\nz\n",
	}
	for seed := int64(1); seed <= 6; seed++ {
		t.Run("seed "+string(rune('0'+seed)), func(t *testing.T) {
			t.Parallel()
			rng := rand.New(rand.NewSource(seed))
			e := newMC(t, mcTwo)
			e.open()
			for step := 0; step < 60; step++ {
				var edits []pluginapi.Edit
				for n := 1 + rng.Intn(3); n > 0; n-- {
					lines := e.b.Lines
					if len(edits) > 0 { // later records are relative to the text after the earlier ones
						lines = e.afterApplying(edits)
					}
					l1 := rng.Intn(len(lines))
					l2 := min(l1+rng.Intn(3), len(lines)-1)
					c1 := rng.Intn(len(lines[l1]) + 1)
					c2 := rng.Intn(len(lines[l2]) + 1)
					if l1 == l2 && c2 < c1 {
						c1, c2 = c2, c1
					}
					edits = append(edits, rep(l1, c1, l2, c2, pieces[rng.Intn(len(pieces))]))
				}
				e.change(edits...)
				if t.Failed() {
					return
				}
				e.checkOracle(t)
				if t.Failed() {
					t.Logf("failed at step %d after %+v", step, edits)
					return
				}
			}
			if w := e.warnings(); len(w) != 0 {
				t.Errorf("log: %v", w)
			}
		})
	}
}

// drain lets the timers of the plugin run (the wait between two chunks of a scan).
func (e *mcEnv) drain() {
	e.h.T.Helper()
	for i := 0; i < 1000 && e.h.PendingTimers() > 0; i++ {
		e.h.Advance(time.Millisecond)
	}
}

// afterApplying returns the lines of the buffer if the edits (not applied yet) were made.
func (e *mcEnv) afterApplying(edits []pluginapi.Edit) []string {
	lines := slices.Clone(e.b.Lines)
	for _, ed := range edits {
		text := strings.Join(lines, "\n")
		starts := make([]int, len(lines))
		off := 0
		for i, l := range lines {
			starts[i] = off
			off += len(l) + 1
		}
		from := starts[ed.Range.From.Line] + ed.Range.From.Col
		to := starts[ed.Range.To.Line] + ed.Range.To.Col
		lines = strings.Split(text[:from]+ed.Text+text[to:], "\n")
	}
	return lines
}

// bigText is a buffer of n lines with a conflict at each of the lines given.
func bigText(n int, at ...int) string {
	lines := make([]string, n)
	for i := range lines {
		lines[i] = "line of text number " + string(rune('a'+i%26))
	}
	for _, a := range at {
		copy(lines[a:], []string{"<<<<<<< HEAD", "ours", "=======", "theirs", ">>>>>>> other"})
	}
	return strings.Join(lines, "\n")
}

// TestMergeConflictOnABigBuffer: the cost of an event does not depend on the size of the
// buffer. AMENDMENTS C22: fewer than 2000 lines read per event on 150 000 lines.
func TestMergeConflictOnABigBuffer(t *testing.T) {
	t.Parallel()
	const n = 150_000
	e := newMC(t, bigText(n, 3, 75_000, n-10))
	// The fake host would scan the whole buffer in one call; the real one stops after a time
	// box (20 ms), so the plugin gets the buffer in chunks.
	e.h.Host.SetFindLinesChunk(2000)
	e.open()
	e.drain()
	if got, want := e.markerLines(), []int{3, 5, 7, 75_000, 75_002, 75_004, n - 10, n - 8, n - 6}; !slices.Equal(got, want) {
		t.Fatalf("marker lines = %v, want %v (log %v)", got, want, e.h.LogText("merge_conflict"))
	}
	if got := e.linesRead(); got >= 2000 {
		t.Errorf("lines read by the first scan = %d, want fewer than 2000", got)
	}
	// An edit in the middle: one narrow rescan.
	read := e.linesRead()
	before := len(e.findLinesCalls())
	e.change(ins(100_000, 0, "typed\n"))
	e.checkOracle(t)
	qs := e.findLinesCalls()[before:]
	if len(qs) != 1 || qs[0].From != 100_000 || qs[0].To != 100_002 {
		t.Errorf("FindLines = %+v, want one scan of two lines", qs)
	}
	if got := e.linesRead() - read; got >= 2000 {
		t.Errorf("lines read by the edit event = %d, want fewer than 2000", got)
	}
	// An event that overflowed rescans everything, and still reads only marker lines.
	read = e.linesRead()
	e.apply(ins(0, 0, "x\n"))
	e.h.Emit(pluginapi.Event{Name: "buffer_changed", Data: map[string]any{
		"buf": e.buf, "version": e.b.Info.Version, "overflow": true,
	}})
	e.h.Advance(300 * time.Millisecond)
	e.drain()
	e.checkOracle(t)
	if got := e.linesRead() - read; got >= 2000 {
		t.Errorf("lines read by the full rescan = %d, want fewer than 2000", got)
	}
	// A command on the big buffer reads only the lines it needs too.
	read = e.linesRead()
	e.setCaret(75_002)
	if err := e.h.Run("merge_conflict.take_theirs", nil); err != nil {
		t.Fatal(err)
	}
	e.drain()
	if got := e.linesRead() - read; got >= 2000 {
		t.Errorf("lines read by Take Theirs = %d, want fewer than 2000", got)
	}
	e.checkOracle(t)
	if w := e.warnings(); len(w) != 0 {
		t.Errorf("log: %v", w)
	}
}

// TestMergeConflictChunkedScan: when the host returns after a time box, the plugin goes on
// from where it stopped, and a buffer that changes while it waits is scanned again.
func TestMergeConflictChunkedScan(t *testing.T) {
	t.Parallel()
	const n = 150_000
	e := newMC(t, bigText(n, 120_000))
	e.h.Host.SetFindLinesChunk(2000)
	e.open()
	e.h.Advance(time.Millisecond) // sleep(0) waits one timer tick between chunks
	for i := 0; i < 300 && e.h.PendingTimers() > 0; i++ {
		e.h.Advance(time.Millisecond)
	}
	if got, want := e.markerLines(), []int{120_000, 120_002, 120_004}; !slices.Equal(got, want) {
		t.Fatalf("marker lines = %v, want %v (log %v)", got, want, e.h.LogText("merge_conflict"))
	}
	qs := e.findLinesCalls()
	if len(qs) != 75 { // 150000 / 2000
		t.Fatalf("FindLines calls = %d, want 75 chunks", len(qs))
	}
	for i, q := range qs {
		if q.From != i*2000 {
			t.Fatalf("chunk %d starts at %d, want %d", i, q.From, i*2000)
		}
	}
	if got := e.linesRead(); got >= 2000 {
		t.Errorf("lines read = %d, want fewer than 2000", got)
	}
	if w := e.warnings(); len(w) != 0 {
		t.Errorf("log: %v", w)
	}
}

// TestMergeConflictScanRestartsWhenTheBufferChanges: an edit that lands between two chunks
// makes the chunks inconsistent, so the scan starts again on the new text.
func TestMergeConflictScanRestartsWhenTheBufferChanges(t *testing.T) {
	t.Parallel()
	const n = 20_000
	e := newMC(t, bigText(n, 10_000))
	e.h.Host.SetFindLinesChunk(2000)
	e.open() // scans the first chunk, then waits one tick
	if e.h.PendingTimers() != 1 {
		t.Fatalf("pending timers = %d, want the wait between chunks", e.h.PendingTimers())
	}
	e.apply(ins(0, 0, "new\n")) // everything below moves down one line, the event is not here yet
	for i := 0; i < 200 && e.h.PendingTimers() > 0; i++ {
		e.h.Advance(time.Millisecond)
	}
	e.checkOracle(t)
	if got, want := e.markerLines(), []int{10_001, 10_003, 10_005}; !slices.Equal(got, want) {
		t.Errorf("marker lines = %v, want %v (log %v)", got, want, e.h.LogText("merge_conflict"))
	}
	if w := e.warnings(); len(w) != 0 {
		t.Errorf("log: %v", w)
	}
}

// A plugin that loads after files were opened is told about them by the replay events
// (after_file_open then buffer_activated, both with replay = true) and finds the conflicts
// of the open buffers from those alone.
func TestMergeConflictScansBuffersOpenAtLoad(t *testing.T) {
	t.Parallel()
	e := newMC(t, mcTwo)
	e.h.Emit(pluginapi.Event{Name: "after_file_open", Data: map[string]any{"buf": e.buf, "path": "/w/a.txt", "replay": true}})
	e.h.Emit(pluginapi.Event{Name: "buffer_activated", Data: map[string]any{"buf": e.buf, "replay": true}})
	if got := len(e.decor()); got != 13 {
		t.Fatalf("decorations = %d, want the 13 of the two conflicts", got)
	}
	if n := e.h.Host.CallCount("DecorSet"); n != 1 {
		t.Errorf("DecorSet calls = %d, want 1", n)
	}
}
