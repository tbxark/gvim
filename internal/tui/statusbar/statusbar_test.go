package statusbar

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/tbxark/gvim/internal/tui/grid"
	"github.com/tbxark/gvim/internal/tui/style"
	"github.com/tbxark/gvim/theme"
)

// newBar returns the status bar with the default labels and no colors.
func newBar() Model {
	return New(DefaultConfig(), style.New(theme.Theme{}))
}

// doc returns the content of a document at path with the given message.
func doc(msg, path string) Content {
	return Content{
		Message:  msg,
		Path:     path,
		Position: "Ln 1, Col 1",
		Encoding: "UTF-8",
		EOL:      "LF",
		Language: "Text",
	}
}

// rowText paints c on a w-cell row and returns its plain text.
func rowText(m Model, w int, c Content) string {
	g := grid.New(w, 1)
	m.Paint(g, 0, c)
	return g.Plain()[0]
}

// findRun returns the first run whose text contains sub.
func findRun(runs []Run, sub string) (Run, bool) {
	for _, r := range runs {
		if strings.Contains(r.Text, sub) {
			return r, true
		}
	}
	return Run{}, false
}

// checkRuns fails when a run is outside the row or overlaps the run before it.
func checkRuns(t *testing.T, runs []Run, w int) {
	t.Helper()
	end := 0
	for _, r := range runs {
		if r.X < end {
			t.Errorf("run %q at %d overlaps the run before it (ends at %d)", r.Text, r.X, end)
		}
		if r.X+r.W > w {
			t.Errorf("run %q ends at %d, past the %d-cell row", r.Text, r.X+r.W, w)
		}
		if got := ansi.StringWidth(r.Text); got != r.W {
			t.Errorf("run %q is %d cells, want %d", r.Text, got, r.W)
		}
		end = r.X + r.W
	}
}

func TestFixtureAt100Columns(t *testing.T) {
	c := doc("Saved a.txt", "/w/a.txt")
	m := newBar()
	got := rowText(m, 100, c)
	if !strings.HasPrefix(got, " Saved a.txt ") {
		t.Fatalf("message group = %q", got)
	}
	if want := " /w/a.txt "; got[45:55] != want {
		t.Fatalf("path group at column 45 = %q, want %q", got[45:55], want)
	}
	if want := " Ln 1, Col 1  UTF-8  LF  Text "; !strings.HasSuffix(got, want) {
		t.Fatalf("right group = %q, want suffix %q", got, want)
	}
	if ansi.StringWidth(got) != 100 {
		t.Fatalf("row is %d cells, want 100", ansi.StringWidth(got))
	}
	runs := m.Runs(100, c)
	checkRuns(t, runs, 100)
	if len(runs) != 6 {
		t.Fatalf("runs = %d, want message, path and four segments", len(runs))
	}
}

func TestRightGroupIsFlushAndSegmentsKeepTheirKind(t *testing.T) {
	c := doc("", "")
	runs := newBar().Runs(100, c)
	var segs []Run
	for _, r := range runs {
		if r.Kind != KindNone {
			segs = append(segs, r)
		}
	}
	want := []Kind{KindPosition, KindEncoding, KindLineEnding, KindLanguage}
	if len(segs) != len(want) {
		t.Fatalf("segments = %d, want %d", len(segs), len(want))
	}
	for i, s := range segs {
		if s.Kind != want[i] {
			t.Errorf("segment %d has kind %v, want %v", i, s.Kind, want[i])
		}
	}
	last := segs[len(segs)-1]
	if last.X+last.W != 100 {
		t.Fatalf("right group ends at %d, want flush at 100", last.X+last.W)
	}
}

// The right group drops from its end. The language goes first, then the line
// ending, then the encoding. The position stays longest.
func TestRightGroupDropsFromTheEnd(t *testing.T) {
	c := doc("", "")
	cases := []struct {
		w    int
		want []Kind // segments that remain
	}{
		{100, []Kind{KindPosition, KindEncoding, KindLineEnding, KindLanguage}},
		{60, []Kind{KindPosition, KindEncoding, KindLineEnding, KindLanguage}},
		{40, []Kind{KindPosition, KindEncoding}},
		{15, []Kind{KindPosition}},
	}
	for _, tc := range cases {
		var got []Kind
		for _, r := range newBar().Runs(tc.w, c) {
			if r.Kind != KindNone {
				got = append(got, r.Kind)
			}
		}
		if len(got) != len(tc.want) {
			t.Errorf("w=%d segments = %v, want %v", tc.w, got, tc.want)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("w=%d segments = %v, want %v", tc.w, got, tc.want)
				break
			}
		}
	}
}

func TestNarrowRowKeepsMessageAndPathInTheGap(t *testing.T) {
	c := doc("Saved a.txt", "/w/a.txt")
	runs := newBar().Runs(50, c)
	checkRuns(t, runs, 50)
	path, ok := findRun(runs, "/w/")
	if !ok || path.Text != " /w/a.txt " || path.X != 16 {
		t.Fatalf("path run = %+v, want the whole path at column 16", path)
	}
}

func TestPathIsCutFromTheLeftKeepingTheFileName(t *testing.T) {
	c := doc("", "/a/very/long/directory/name/file.go")
	runs := newBar().Runs(30, c)
	checkRuns(t, runs, 30)
	path, ok := findRun(runs, "file.go")
	if !ok || !strings.HasPrefix(path.Text, "…") || !strings.HasSuffix(path.Text, "file.go ") {
		t.Fatalf("cut path = %+v, want the ellipsis and the file name", path)
	}
}

func TestLongMessageIsCutWithEllipsis(t *testing.T) {
	c := doc("Opened a very long status message that does not fit", "/w/a.txt")
	runs := newBar().Runs(40, c)
	checkRuns(t, runs, 40)
	if !strings.HasSuffix(runs[0].Text, "…") {
		t.Fatalf("message run = %q, want the ellipsis", runs[0].Text)
	}
}

func TestHitFindsSegmentsAndNone(t *testing.T) {
	m := newBar()
	c := doc("Saved a.txt", "/w/a.txt")
	const w = 100
	cases := []struct {
		x    int
		kind Kind
	}{
		{70, KindPosition},
		{84, KindEncoding},
		{91, KindLineEnding},
		{95, KindLanguage},
		{50, KindNone}, // the path
		{2, KindNone},  // the message
		{100, KindNone},
	}
	for _, tc := range cases {
		if got := m.Hit(w, c, tc.x); got.Kind != tc.kind {
			t.Errorf("Hit(%d) = %+v, want kind %v", tc.x, got, tc.kind)
		}
	}
}

func TestLoadingStartsAndStopsTheSpinner(t *testing.T) {
	m := newBar()
	m, cmd := m.SetLoading(true)
	if cmd == nil || !m.Loading() {
		t.Fatal("SetLoading(true) did not start the spinner")
	}
	before := rowText(m, 100, doc("", ""))
	if !strings.Contains(before, "Loading files…") {
		t.Fatalf("loading label missing: %q", before)
	}

	m, _ = m.Update(m.spinner.Tick())
	after := rowText(m, 100, doc("", ""))
	if before == after {
		t.Fatal("a tick did not advance the spinner frame")
	}

	m, cmd = m.SetLoading(false)
	if cmd != nil || m.Loading() {
		t.Fatal("SetLoading(false) returned a command or left the state on")
	}
	stopped, cmd := m.Update(m.spinner.Tick())
	if cmd != nil || stopped.Loading() {
		t.Fatal("a tick after stop was not dropped")
	}
	if strings.Contains(rowText(m, 100, doc("", "")), "Loading") {
		t.Fatal("loading label stayed after stop")
	}
}

func TestLoadingKeepsTheMessageWhenSet(t *testing.T) {
	m, _ := newBar().SetLoading(true)
	got := rowText(m, 100, doc("Opening b.txt", ""))
	if !strings.Contains(got, "Opening b.txt") || strings.Contains(got, "Loading files") {
		t.Fatalf("message not kept while loading: %q", got)
	}
}

func TestSetLoadingTwiceIsIdempotent(t *testing.T) {
	m, _ := newBar().SetLoading(true)
	m2, cmd := m.SetLoading(true)
	if cmd != nil || !m2.Loading() {
		t.Fatal("second SetLoading(true) started another tick")
	}
}

func TestUpdateIgnoresOtherMessages(t *testing.T) {
	m := newBar()
	next, cmd := m.Update(tea.WindowSizeMsg{Width: 10, Height: 2})
	if cmd != nil || next.Loading() {
		t.Fatal("a window message changed the status bar")
	}
}

func TestConfigLoadingLabelIsUsed(t *testing.T) {
	m, _ := New(Config{Loading: "Busy"}, style.New(theme.Theme{})).SetLoading(true)
	if got := rowText(m, 100, doc("", "")); !strings.Contains(got, "Busy") {
		t.Fatalf("configured loading label not used: %q", got)
	}
	if def := New(Config{}, style.New(theme.Theme{})); def.cfg != DefaultConfig() {
		t.Fatalf("empty config = %+v, want defaults", def.cfg)
	}
}

func TestPaintPlacesThePositionText(t *testing.T) {
	c := doc("", "")
	g := grid.New(100, 1)
	newBar().Paint(g, 0, c)
	if got := g.At(71, 0).Text; got != "L" {
		t.Fatalf("position text at its run = %q, want L of Ln", got)
	}
}

func TestRunsWithNarrowWidthsDoNotPanic(t *testing.T) {
	m := newBar()
	for w := 0; w <= 3; w++ {
		_ = m.Runs(w, doc("x", "/a"))
		_ = rowText(m, w, doc("x", "/a"))
	}
}

func TestTruncateLeftCellsNeverOverflowsOnWideRunes(t *testing.T) {
	t.Parallel()
	path := "/home/用户/文档/我的-项目/报告-最终版.txt"
	for w := 1; w <= 40; w++ {
		if got := truncateLeftCells(path, w); ansi.StringWidth(got) > w {
			t.Errorf("truncateLeftCells(path, %d) = %q is %d cells wide", w, got, ansi.StringWidth(got))
		}
	}
}
