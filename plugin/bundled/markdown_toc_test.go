package bundled_test

import (
	"fmt"
	"io/fs"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/tbxark/gvim/plugin/bundled"
	"github.com/tbxark/gvim/plugin/plugintest"
	"github.com/tbxark/gvim/pluginapi"
)

// Scenario tests of the markdown_toc port on the fake host, loading the Lua from the
// embedded files as the loader does.

const (
	tocShow  = "markdown_toc.show"
	tocClose = "markdown_toc.close"
)

type tocEnv struct {
	t    *testing.T
	h    *plugintest.Harness
	host *plugintest.FakeHost
	doc  pluginapi.BufferID
}

// newToc loads the plugin with the Markdown buffer /w/doc.md holding text. settings are the
// plugin's user settings.
func newToc(t *testing.T, text string, settings map[string]any) *tocEnv {
	t.Helper()
	src, err := fs.ReadFile(bundled.FS, "plugins/markdown_toc/init.lua")
	if err != nil {
		t.Fatalf("read the bundled plugin: %v", err)
	}
	host := plugintest.NewFakeHost()
	doc := host.AddBuffer("/w/doc.md", text)
	h := plugintest.New(t, plugintest.Files{"markdown_toc/init.lua": string(src)}, plugintest.Options{Host: host, Timeout: 20 * time.Second, EventBudget: 5 * time.Second})
	h.Sources[0].Settings = settings
	h.Start()
	return &tocEnv{t: t, h: h, host: host, doc: doc}
}

// show runs Markdown: Show Outline.
func (e *tocEnv) show() {
	e.t.Helper()
	if err := e.h.Run(tocShow, nil); err != nil {
		e.t.Fatal(err)
	}
}

// outline returns the virtual buffer of the outline, or 0.
func (e *tocEnv) outline() pluginapi.BufferID {
	for _, b := range e.host.ListBuffers() {
		if b.Virtual {
			return b.ID
		}
	}
	return 0
}

// text returns the text of the outline.
func (e *tocEnv) text() string {
	e.t.Helper()
	id := e.outline()
	if id == 0 {
		e.t.Fatal("there is no outline buffer")
	}
	return e.textOf(id)
}

// textOf reads the fake buffer directly, so that the read is not one of the plugin's calls.
func (e *tocEnv) textOf(id pluginapi.BufferID) string {
	for _, b := range e.host.Buffers {
		if b.Info.ID == id {
			return strings.Join(b.Lines, "\n")
		}
	}
	e.t.Fatalf("buffer %d is gone", id)
	return ""
}

// rowLines returns the source line each outline row stands for (the props of the row).
func (e *tocEnv) rowLines() []int {
	e.t.Helper()
	id := e.outline()
	info, _ := e.host.Buffer(id)
	var out []int
	for row := range info.LineCount {
		props, err := e.host.PropsAt(id, pluginapi.Pos{Line: row})
		if err != nil {
			e.t.Fatal(err)
		}
		var n int
		switch v := props["line"].(type) {
		case int:
			n = v
		case int64:
			n = int(v)
		case float64:
			n = int(v)
		default:
			return out
		}
		out = append(out, n)
	}
	return out
}

// edit replaces the range of the document with text as an editor would and reports it in
// the change records an editor gives (a delete, then an insert), without raising the event.
func (e *tocEnv) edit(fl, fc, tl, tc int, text string) []pluginapi.ChangeRec {
	e.t.Helper()
	r := pluginapi.Range{From: pluginapi.Pos{Line: fl, Col: fc}, To: pluginapi.Pos{Line: tl, Col: tc}}
	if err := e.host.Edit(e.doc, []pluginapi.Edit{{Range: r, Text: text}}, pluginapi.EditOptions{}); err != nil {
		e.t.Fatal(err)
	}
	var recs []pluginapi.ChangeRec
	if r.From != r.To {
		recs = append(recs, pluginapi.ChangeRec{Range: r})
	}
	if text != "" {
		recs = append(recs, pluginapi.ChangeRec{Range: pluginapi.Range{From: r.From, To: r.From}, Text: text})
	}
	return recs
}

func (e *tocEnv) version() pluginapi.Version {
	info, _ := e.host.Buffer(e.doc)
	return info.Version
}

// changed raises buffer_changed for the document with the records and lets the 300 ms
// debounce run out.
func (e *tocEnv) changed(recs ...pluginapi.ChangeRec) {
	e.t.Helper()
	e.h.Emit(pluginapi.Event{Name: "buffer_changed", Data: map[string]any{
		"buf": e.doc, "version": e.version(), "edits": recs, "undo": false, "overflow": false,
	}})
	e.h.Advance(300 * time.Millisecond)
}

// overflowed raises buffer_changed with no records, as the editor does after a big or
// coarse change.
func (e *tocEnv) overflowed() {
	e.t.Helper()
	e.h.Emit(pluginapi.Event{Name: "buffer_changed", Data: map[string]any{
		"buf": e.doc, "version": e.version(), "undo": false, "overflow": true,
	}})
	e.h.Advance(300 * time.Millisecond)
}

// findQueries returns the FindLines queries the host got after call number from.
func (e *tocEnv) findQueries(from int) []pluginapi.FindLinesQuery {
	var out []pluginapi.FindLinesQuery
	for _, c := range e.host.Calls()[from:] {
		if c.Method == "FindLines" {
			out = append(out, c.Args[1].(pluginapi.FindLinesQuery))
		}
	}
	return out
}

// linesRead returns the number of lines the plugin read with b:line(s) and b:lines after
// call number from, and the most one call read (-1 for a call that reads to the end).
func (e *tocEnv) linesRead(from int) (total, widest int) {
	for _, c := range e.host.Calls()[from:] {
		if c.Method != "Lines" {
			continue
		}
		n := c.Args[2].(int)
		if n < 0 {
			return 1 << 30, -1
		}
		total += n
		widest = max(widest, n)
	}
	return total, widest
}

func (e *tocEnv) mark() int { return len(e.host.Calls()) }

// finish runs the manual clock, a millisecond at a time, until the plugin waits for nothing
// (a scan yields between chunks), and returns the time that took.
func (e *tocEnv) finish() time.Duration {
	start := e.h.Now()
	for e.h.PendingTimers() > 0 && e.h.Now().Sub(start) < time.Second {
		e.h.Advance(time.Millisecond)
	}
	return e.h.Now().Sub(start)
}

func TestMarkdownTocRegistersItsCommands(t *testing.T) {
	t.Parallel()
	e := newToc(t, "# A", nil)
	var got []string
	for _, c := range e.host.Commands["markdown_toc"] {
		if !strings.Contains(c.ID, ".__fn") { // the functions of the mode's keys
			got = append(got, c.ID+"="+c.Title)
		}
	}
	want := []string{"markdown_toc.close=Markdown: Close Outline", "markdown_toc.show=Markdown: Show Outline"}
	if !slices.Equal(got, want) {
		t.Fatalf("commands = %v, want %v", got, want)
	}
	if len(e.host.Modes["markdown_toc"]) != 1 || e.host.Modes["markdown_toc"][0].Name != "markdown_outline" {
		t.Fatalf("modes = %+v", e.host.Modes["markdown_toc"])
	}
}

func TestMarkdownTocOutlineText(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, doc, want string
		lines           []int // source line of each row
	}{
		{"nesting is relative to the shallowest", "# Title\n\ntext\n## Part 1\n### Detail\n## Part 2\n",
			"Title\n  Part 1\n    Detail\n  Part 2", []int{0, 3, 4, 5}},
		{"base level", "## A\n### B\n## C", "A\n  B\nC", []int{0, 1, 2}},
		{"no headings", "just text\n\nmore", "(no headings)", nil},
		{"empty document", "", "(no headings)", nil},
		{"backtick fence", "# Real\n```bash\n# comment\n```\n## After", "Real\n  After", []int{0, 4}},
		{"tilde fence holding backticks", "~~~\n```\n# x\n```\n~~~\n# Out", "Out", []int{5}},
		{"a fence closes at least as long", "````\n```\n# inner\n````\n# Out", "Out", []int{4}},
		{"a closing fence has no info string", "```\n```js\n# inner\n```\n# Out", "Out", []int{4}},
		{"unclosed fence hides the rest", "# A\n```\n# B\n## C", "A", []int{0}},
		{"indented fence", "# A\n  ```\n# B\n  ```\n# C", "A\nC", []int{0, 4}},
		{"not headings", "#hashtag\n####### seven\n    # indented code\n#\n# \n#\t", "(no headings)", nil},
		{"closing hashes", "## Title ##\n# C#\n# trailing   #  ", "  Title\nC#\ntrailing", []int{0, 1, 2}},
		{"tab after the hashes", "#\tTabbed", "Tabbed", []int{0}},
		{"three spaces of indent", "   ### Three\n    #### Four", "Three", []int{0}},
		{"six levels", "###### Six\n# One", "          Six\nOne", []int{0, 1}},
		{"unicode", "# Überblick 日本語\n## Ünï", "Überblick 日本語\n  Ünï", []int{0, 1}},
		{"setext headings are not listed", "Title\n=====\n\nSub\n---", "(no headings)", nil},
		{"long title is cut", "# " + strings.Repeat("x", 300), strings.Repeat("x", 119) + "…", []int{0}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e := newToc(t, tt.doc, nil)
			e.show()
			if got := e.text(); got != tt.want {
				t.Fatalf("outline = %q, want %q (log %v)", got, tt.want, e.h.LogText(""))
			}
			if got := e.rowLines(); !slices.Equal(got, tt.lines) {
				t.Fatalf("row lines = %v, want %v", got, tt.lines)
			}
		})
	}
}

func TestMarkdownTocWhitespaceRunsDoNotBlankTheOutline(t *testing.T) {
	t.Parallel()
	sp, tab := strings.Repeat(" ", 5000), strings.Repeat("\t", 5000)
	tests := []struct {
		name, doc, want string
	}{
		{"long run inside the title", "# Title\n\n## a" + sp + "x\n## b\n", "Title\n  a\n  b"},
		{"spaces then the title", "# Title\n## " + sp + "x\n## b\n", "Title\n  x\n  b"},
		{"trailing tabs", "# Title\n## a" + tab + "\n## b\n", "Title\n  a\n  b"},
		{"spaces then closing hashes", "# Title\n## a" + sp + "##\n## b\n", "Title\n  a\n  b"},
		{"inside a fence", "# Title\n```\n## a" + sp + "x\n```\n## b\n", "Title\n  b"},
		{"fence line with a long run", "# Title\n```" + sp + "\n## a\n```\n## b\n", "Title\n  b"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e := newToc(t, tt.doc, nil)
			e.show()
			e.finish()
			got := e.text()
			if got != tt.want {
				t.Fatalf("outline = %.80q, want %q (log %v)", got, tt.want, e.h.LogText(""))
			}
		})
	}
}

func TestMarkdownTocMaxDepth(t *testing.T) {
	t.Parallel()
	doc := "# A\n## B\n### C\n#### D\n## E"
	tests := []struct {
		depth float64
		want  string
	}{
		{1, "A"},
		{2, "A\n  B\n  E"},
		{3, "A\n  B\n    C\n  E"},
		{6, "A\n  B\n    C\n      D\n  E"},
	}
	for _, tt := range tests {
		e := newToc(t, doc, map[string]any{"max_depth": tt.depth})
		e.show()
		if got := e.text(); got != tt.want {
			t.Errorf("max_depth %v: outline = %q, want %q", tt.depth, got, tt.want)
		}
	}
}

func TestMarkdownTocShowsASplitWithTheOutline(t *testing.T) {
	t.Parallel()
	e := newToc(t, "# A\n## B", nil)
	e.show()
	id := e.outline()
	info, ok := e.host.Buffer(id)
	if !ok || info.Name != "*Outline*" || !info.ReadOnly || info.Mode != "markdown_outline" || info.Modified {
		t.Fatalf("outline buffer = %+v", info)
	}
	var split pluginapi.SplitOptions
	for _, c := range e.host.Calls() {
		if c.Method == "SplitWindow" {
			split = c.Args[1].(pluginapi.SplitOptions)
		}
	}
	if split.Dir != "right" || !split.Before || !split.Focus || split.Buffer != id || split.Ratio != 0.3 {
		t.Fatalf("split = %+v, want the outline on the left, focused", split)
	}
	wins := e.host.Windows()
	if len(wins) != 2 || wins[0].Buffer != id || wins[1].Buffer != e.doc || !wins[0].Active {
		t.Fatalf("windows = %+v", wins)
	}
	if got := e.host.ActiveBuffer(); got != id {
		t.Fatalf("active buffer = %d, want the outline %d", got, id)
	}
	// the second call focuses the open outline instead of making another
	e.show()
	if len(e.host.Windows()) != 2 || e.host.CallCount("CreateBuffer") != 1 {
		t.Fatalf("a second show opened more: %d windows, %d buffers", len(e.host.Windows()), e.host.CallCount("CreateBuffer"))
	}
}

func TestMarkdownTocRefusesOtherBuffers(t *testing.T) {
	t.Parallel()
	e := newToc(t, "# A", nil)
	txt := e.host.AddBuffer("/w/notes.txt", "# not markdown")
	e.host.SetActive(txt)
	e.show()
	if got := e.h.NoticeTexts(); !slices.Equal(got, []string{"Not a Markdown buffer"}) || e.outline() != 0 || len(e.host.Windows()) != 1 {
		t.Fatalf("notices = %v, outline = %d, windows = %d", got, e.outline(), len(e.host.Windows()))
	}
	// a buffer is Markdown by language or by extension
	for _, tc := range []struct{ path, lang string }{{"/w/a.markdown", ""}, {"/w/B.MD", ""}, {"/w/readme", "markdown"}} {
		id := e.host.AddBuffer(tc.path, "# Yes")
		e.host.SetActive(id)
		if err := e.host.SetLanguage(id, tc.lang); err != nil {
			t.Fatal(err)
		}
		e.show()
		if e.outline() == 0 {
			t.Errorf("%+v: no outline", tc)
			continue
		}
		if err := e.h.Run(tocClose, nil); err != nil {
			t.Fatal(err)
		}
	}
}

func TestMarkdownTocLargeFile(t *testing.T) {
	t.Parallel()
	e := newToc(t, "# A\n## B", nil)
	e.host.Buffers[0].Info.Large = true
	from := e.mark()
	e.show()
	if got := e.text(); got != "(the file is too large for an outline)" {
		t.Fatalf("outline = %q", got)
	}
	if n := len(e.findQueries(from)); n != 0 {
		t.Fatalf("a large file was scanned (%d FindLines calls)", n)
	}
}

func TestMarkdownTocSplitFailureLeavesNothing(t *testing.T) {
	t.Parallel()
	e := newToc(t, "# A", nil)
	e.host.Screen = [2]int{2, 24}
	e.host.Win().Wins[0].Width = 2
	e.show()
	if got := e.h.NoticeTexts(); len(got) != 1 || !strings.HasPrefix(got[0], "Cannot open the outline: ") {
		t.Fatalf("notices = %v", got)
	}
	for _, r := range e.host.Resources() {
		if strings.HasPrefix(r, "vbuf:") {
			t.Fatalf("the virtual buffer stays: %v", e.host.Resources())
		}
	}
	if e.outline() != 0 || len(e.host.Windows()) != 1 {
		t.Fatalf("outline = %d, windows = %d", e.outline(), len(e.host.Windows()))
	}
	if e.h.RT.Wants("buffer_changed") {
		t.Fatal("the plugin listens to edits with no outline")
	}
}

func TestMarkdownTocEdits(t *testing.T) {
	t.Parallel()
	const doc = "# One\ntext\n## Two\nmore\n```\n# code\n```\n## Three\nend"
	tests := []struct {
		name        string
		edit        func(e *tocEnv) []pluginapi.ChangeRec
		want        string
		lines       []int
		scanFrom    int // the scan starts here ...
		scanBefore  int // ... and ends before this line
		noRedrawing bool
	}{
		{"retitle a heading", func(e *tocEnv) []pluginapi.ChangeRec { return e.edit(2, 3, 2, 6, "2nd") },
			"One\n  2nd\n  Three", []int{0, 2, 7}, 2, 3, false},
		{"typing in a plain line changes nothing", func(e *tocEnv) []pluginapi.ChangeRec { return e.edit(3, 4, 3, 4, "!") },
			"One\n  Two\n  Three", []int{0, 2, 7}, 3, 4, true},
		{"a heading is added and the rest shifts", func(e *tocEnv) []pluginapi.ChangeRec { return e.edit(1, 0, 1, 0, "### New\n") },
			"One\n    New\n  Two\n  Three", []int{0, 1, 3, 8}, 1, 3, false},
		{"a heading is deleted", func(e *tocEnv) []pluginapi.ChangeRec { return e.edit(2, 0, 3, 0, "") },
			"One\n  Three", []int{0, 6}, 2, 3, false},
		{"a line is made a heading", func(e *tocEnv) []pluginapi.ChangeRec { return e.edit(1, 0, 1, 0, "## ") },
			"One\n  text\n  Two\n  Three", []int{0, 1, 2, 7}, 1, 2, false},
		{"a heading is made text", func(e *tocEnv) []pluginapi.ChangeRec { return e.edit(0, 0, 0, 2, "") },
			"Two\nThree", []int{2, 7}, 0, 1, false},
		// the old closing fence now opens one, and hides "## Three"
		{"the fence opener is removed and the code heading shows", func(e *tocEnv) []pluginapi.ChangeRec { return e.edit(4, 0, 5, 0, "") },
			"One\n  Two\ncode", []int{0, 2, 4}, 4, 5, false},
		// the new fence closes at the old opener, so "# code" is outside, and the old closer opens one
		{"a fence opened above changes what the code is", func(e *tocEnv) []pluginapi.ChangeRec { return e.edit(1, 0, 1, 0, "```\n") },
			"One\ncode", []int{0, 6}, 1, 3, false},
		{"lines are joined", func(e *tocEnv) []pluginapi.ChangeRec { return e.edit(1, 4, 2, 0, "") },
			"One\n  Three", []int{0, 6}, 1, 2, false},
		{"a paste of several lines", func(e *tocEnv) []pluginapi.ChangeRec {
			return e.edit(8, 3, 8, 3, "\n# P1\ntext\n## P2")
		}, "One\n  Two\n  Three\nP1\n  P2", []int{0, 2, 7, 9, 11}, 8, 12, false},
		{"two edits in one event are followed in order", func(e *tocEnv) []pluginapi.ChangeRec {
			a := e.edit(0, 0, 0, 0, "intro\n")
			b := e.edit(0, 0, 0, 0, "# Zero\n")
			return append(a, b...)
		}, "Zero\nOne\n  Two\n  Three", []int{0, 2, 4, 9}, 0, 3, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e := newToc(t, doc, nil)
			e.show()
			before := e.text()
			recs := tt.edit(e)
			from := e.mark()
			e.changed(recs...)
			if got := e.text(); got != tt.want {
				t.Fatalf("outline = %q, want %q (log %v)", got, tt.want, e.h.LogText(""))
			}
			if tt.lines != nil {
				if got := e.rowLines(); !slices.Equal(got, tt.lines) {
					t.Fatalf("row lines = %v, want %v", got, tt.lines)
				}
			}
			if tt.noRedrawing && (e.text() != before || e.host.CallCount("SetContent") != 1) {
				t.Fatalf("the outline was drawn again for an edit that left it alone (%d SetContent calls)", e.host.CallCount("SetContent"))
			}
			for _, q := range e.findQueries(from) {
				if q.From == 0 && q.To <= 0 {
					t.Fatalf("an edit scanned the whole document: %+v", q)
				}
			}
			if tt.scanBefore > 0 {
				qs := e.findQueries(from)
				if len(qs) != 1 || qs[0].From != tt.scanFrom || qs[0].To != tt.scanBefore {
					t.Fatalf("scans = %+v, want one of lines [%d, %d)", qs, tt.scanFrom, tt.scanBefore)
				}
			}
		})
	}
}

// Undo and redo records, and events that carry no records, make the plugin scan afresh.
func TestMarkdownTocOverflowScansAfresh(t *testing.T) {
	t.Parallel()
	e := newToc(t, "# One\n## Two", nil)
	e.show()
	e.edit(0, 0, 1, 6, "# Alpha\n## Beta\n### Gamma")
	from := e.mark()
	e.overflowed()
	if got, want := e.text(), "Alpha\n  Beta\n    Gamma"; got != want {
		t.Fatalf("outline = %q, want %q", got, want)
	}
	qs := e.findQueries(from)
	if len(qs) != 1 || qs[0].From != 0 || qs[0].To > 0 {
		t.Fatalf("scans = %+v, want one of the whole document", qs)
	}
}

// Events the plugin never saw show as a document of another length, or as a version it
// did not follow; both are repaired by a new scan.
func TestMarkdownTocRepairsMissedEvents(t *testing.T) {
	t.Parallel()
	t.Run("a dropped event shows in the line count", func(t *testing.T) {
		t.Parallel()
		e := newToc(t, "# One\n## Two\n## Three", nil)
		e.show()
		e.edit(0, 0, 0, 0, "# Zero\n") // never reported
		recs := e.edit(3, 8, 3, 8, "!")
		e.changed(recs...)
		if got, want := e.text(), "Zero\nOne\n  Two\n  Three!"; got != want {
			t.Fatalf("outline = %q, want %q", got, want)
		}
	})
	t.Run("an edit while the buffer was in the background", func(t *testing.T) {
		t.Parallel()
		e := newToc(t, "# One", nil)
		e.show()
		e.edit(0, 5, 0, 5, "\n## Two") // never reported
		e.h.Emit(pluginapi.Event{Name: "buffer_activated", Data: map[string]any{"buf": e.doc}})
		if got, want := e.text(), "One\n  Two"; got != want {
			t.Fatalf("outline = %q, want %q", got, want)
		}
	})
	t.Run("a scan that finds the text ahead of the events waits for them", func(t *testing.T) {
		t.Parallel()
		e := newToc(t, "# One\ntext", nil)
		e.show()
		recs := e.edit(1, 0, 1, 0, "## Two\n")
		e.edit(2, 0, 2, 0, "### Three\n") // the second event has not arrived
		e.h.Emit(pluginapi.Event{Name: "buffer_changed", Data: map[string]any{
			"buf": e.doc, "version": pluginapi.Version{Gen: 1, Ver: 1}, "edits": recs,
		}})
		e.h.Advance(300 * time.Millisecond)
		if got := e.text(); got != "One" {
			t.Fatalf("outline = %q, want it to wait for the event of the second edit", got)
		}
		// the events never come: the plugin looks again on its own
		e.h.Advance(2 * time.Second)
		if got, want := e.text(), "One\n  Two\n    Three"; got != want {
			t.Fatalf("outline after the resync = %q, want %q", got, want)
		}
	})
}

func TestMarkdownTocFollowsTheCursor(t *testing.T) {
	t.Parallel()
	e := newToc(t, "# One\ntext\n## Two\nmore\ntext\n## Three\nend", nil)
	e.show()
	cursorRow := func() int {
		cs, _ := e.host.Cursors(e.outline())
		return cs[0].Pos.Line
	}
	move := func(line int) {
		e.h.Emit(pluginapi.Event{Name: "cursor_moved", Data: map[string]any{
			"buf": e.doc, "pos": pluginapi.Pos{Line: line}, "previous": pluginapi.Pos{},
		}})
		e.h.Advance(100 * time.Millisecond)
	}
	for _, tt := range []struct{ line, row int }{{0, 0}, {1, 0}, {2, 1}, {4, 1}, {5, 2}, {6, 2}} {
		move(tt.line)
		if got := cursorRow(); got != tt.row {
			t.Errorf("cursor on line %d: outline row = %d, want %d", tt.line, got, tt.row)
		}
	}
	// another buffer's cursor is none of its business
	move(0)
	other := e.host.AddBuffer("/w/other.md", "# x")
	e.h.Emit(pluginapi.Event{Name: "cursor_moved", Data: map[string]any{"buf": other, "pos": pluginapi.Pos{Line: 5}}})
	e.h.Advance(100 * time.Millisecond)
	if got := cursorRow(); got != 0 {
		t.Errorf("a cursor in another buffer moved the outline to row %d", got)
	}
}

func TestMarkdownTocFollowsTheScroll(t *testing.T) {
	t.Parallel()
	e := newToc(t, "# One\ntext\n## Two\nmore\ntext\n## Three\nend", map[string]any{"follow": "scroll"})
	e.show()
	cursorRow := func() int {
		cs, _ := e.host.Cursors(e.outline())
		return cs[0].Pos.Line
	}
	// the outline window took the focus; the document is in window 1 (the first one made)
	src := e.host.Windows()[1].ID
	e.host.SetVisible(src, []pluginapi.VisibleRow{{Line: 5, ColTo: 9}, {Line: 6, ColTo: 3}})
	e.h.Emit(pluginapi.Event{Name: "viewport_changed", Data: map[string]any{"buf": e.doc, "top_line": 5}})
	e.h.Advance(100 * time.Millisecond)
	if got := cursorRow(); got != 2 {
		t.Fatalf("the window shows line 5 on top: outline row = %d, want 2", got)
	}
	// moving the cursor does not matter in this mode
	e.h.Emit(pluginapi.Event{Name: "cursor_moved", Data: map[string]any{"buf": e.doc, "pos": pluginapi.Pos{Line: 0}}})
	e.h.Advance(100 * time.Millisecond)
	if got := cursorRow(); got != 2 {
		t.Fatalf("the cursor moved the outline to row %d in scroll mode", got)
	}
}

// binding returns the action the outline mode binds chord to.
func (e *tocEnv) binding(chord string) string {
	e.t.Helper()
	act := e.host.Modes["markdown_toc"][0].Bindings[chord]
	if act == "" {
		e.t.Fatalf("the outline mode does not bind %q: %+v", chord, e.host.Modes["markdown_toc"][0].Bindings)
	}
	return act
}

func TestMarkdownTocEnterJumpsToTheHeading(t *testing.T) {
	t.Parallel()
	e := newToc(t, "# One\ntext\n## Two\nmore\n## Three\nend", nil)
	e.show()
	outline := e.outline()
	if err := e.host.SetCursor(outline, pluginapi.Pos{Line: 2}, false, false, false); err != nil {
		t.Fatal(err)
	}
	from := e.mark()
	if err := e.h.Run(e.binding("enter"), nil); err != nil {
		t.Fatal(err)
	}
	cs, _ := e.host.Cursors(e.doc)
	if cs[0].Pos != (pluginapi.Pos{Line: 4}) {
		t.Fatalf("cursor of the document = %+v, want line 4 (Three)", cs[0].Pos)
	}
	var centered bool
	for _, c := range e.host.Calls()[from:] {
		if c.Method == "SetCursor" && c.Args[0] == e.doc {
			centered = c.Args[4].(bool)
		}
	}
	if !centered {
		t.Fatal("the heading was not centered")
	}
	if e.host.ActiveBuffer() != e.doc || e.host.CurrentWindow() != e.host.Windows()[1].ID {
		t.Fatalf("the focus is on buffer %d, window %d; want the document's window", e.host.ActiveBuffer(), e.host.CurrentWindow())
	}
	if e.outline() == 0 || len(e.host.Windows()) != 2 {
		t.Fatal("the outline closed on a jump")
	}
}

func TestMarkdownTocEnterWithoutAnEntryDoesNothing(t *testing.T) {
	t.Parallel()
	e := newToc(t, "no headings", nil)
	e.show()
	if err := e.h.Run(e.binding("enter"), nil); err != nil {
		t.Fatal(err)
	}
	if e.host.ActiveBuffer() != e.outline() {
		t.Fatal("Enter on the placeholder row left the outline")
	}
}

func TestMarkdownTocEscapeGoesBack(t *testing.T) {
	t.Parallel()
	e := newToc(t, "# One", nil)
	e.show()
	if err := e.h.Run(e.binding("escape"), nil); err != nil {
		t.Fatal(err)
	}
	if e.host.ActiveBuffer() != e.doc || len(e.host.Windows()) != 2 || e.outline() == 0 {
		t.Fatalf("active %d, %d windows, outline %d", e.host.ActiveBuffer(), len(e.host.Windows()), e.outline())
	}
}

func TestMarkdownTocCloseGivesEverythingBack(t *testing.T) {
	t.Parallel()
	closers := map[string]func(e *tocEnv){
		"q key":   func(e *tocEnv) { _ = e.h.Run(e.binding("q"), nil) },
		"command": func(e *tocEnv) { _ = e.h.Run(tocClose, nil) },
		"unload":  func(e *tocEnv) { e.h.Unload("markdown_toc") },
	}
	for name, closeIt := range closers {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			e := newToc(t, "# One", nil)
			base := len(e.host.Resources())
			e.show()
			if len(e.host.Resources()) <= base {
				t.Fatal("the outline holds no resources")
			}
			closeIt(e)
			if e.outline() != 0 || len(e.host.Windows()) != 1 || e.host.ActiveBuffer() != e.doc {
				t.Fatalf("outline %d, %d windows, active %d", e.outline(), len(e.host.Windows()), e.host.ActiveBuffer())
			}
			for _, ev := range []string{"buffer_changed", "buffer_activated", "buffer_closed", "cursor_moved", "viewport_changed"} {
				if e.h.RT.Wants(ev) {
					t.Errorf("still listening to %s", ev)
				}
			}
			if name != "unload" {
				// the plugin works again afterwards
				e.show()
				if got := e.text(); got != "One" {
					t.Fatalf("outline after reopening = %q", got)
				}
			}
		})
	}
}

func TestMarkdownTocFollowsTheActiveMarkdownBuffer(t *testing.T) {
	t.Parallel()
	e := newToc(t, "# First", nil)
	e.show()
	activate := func(id pluginapi.BufferID) {
		e.host.SetActive(id)
		e.h.Emit(pluginapi.Event{Name: "buffer_activated", Data: map[string]any{"buf": id}})
	}
	second := e.host.AddBuffer("/w/second.md", "# Second\n## Part")
	plain := e.host.AddBuffer("/w/plain.txt", "# not markdown")
	activate(second)
	if got := e.text(); got != "Second\n  Part" {
		t.Fatalf("after the second document: %q", got)
	}
	activate(plain)
	if got := e.text(); got != "Second\n  Part" {
		t.Fatalf("a plain text buffer changed the outline: %q", got)
	}
	activate(e.outline())
	if got := e.text(); got != "Second\n  Part" {
		t.Fatalf("activating the outline changed it: %q", got)
	}
	// edits of the document that was left do not touch the outline of the new one
	recs := e.edit(0, 7, 0, 7, "!")
	e.changed(recs...)
	if got := e.text(); got != "Second\n  Part" {
		t.Fatalf("an edit of another document changed it: %q", got)
	}
	// and back: the edit it missed is seen
	activate(e.doc)
	if got := e.text(); got != "First!" {
		t.Fatalf("after going back: %q", got)
	}
}

func TestMarkdownTocClosingTheDocumentClosesTheOutline(t *testing.T) {
	t.Parallel()
	e := newToc(t, "# One", nil)
	e.show()
	e.host.RemoveBuffer(e.doc)
	e.h.Emit(pluginapi.Event{Name: "buffer_closed", Data: map[string]any{"id": e.doc}})
	if e.outline() != 0 || e.h.RT.Wants("buffer_changed") {
		t.Fatalf("outline %d, still listening: %v", e.outline(), e.h.RT.Wants("buffer_changed"))
	}
}

func TestMarkdownTocClosingTheOutlineBufferEndsIt(t *testing.T) {
	t.Parallel()
	e := newToc(t, "# One", nil)
	e.show()
	id := e.outline()
	e.host.RemoveBuffer(id)
	e.h.Emit(pluginapi.Event{Name: "buffer_closed", Data: map[string]any{"id": id}})
	if e.h.RT.Wants("buffer_changed") {
		t.Fatal("still listening after the outline buffer was closed")
	}
	if len(e.host.Windows()) != 1 {
		t.Fatalf("%d windows left", len(e.host.Windows()))
	}
	e.host.SetActive(e.doc)
	e.show()
	if got := e.text(); got != "One" {
		t.Fatalf("outline after reopening = %q", got)
	}
}

func TestMarkdownTocShowAfterTheWindowWasClosed(t *testing.T) {
	t.Parallel()
	e := newToc(t, "# One", nil)
	e.show()
	if err := e.host.CloseWindow(e.host.Windows()[0].ID); err != nil {
		t.Fatal(err)
	}
	e.host.SetActive(e.doc)
	e.show()
	if len(e.host.Windows()) != 2 || e.host.CallCount("CreateBuffer") != 1 {
		t.Fatalf("%d windows, %d buffers made", len(e.host.Windows()), e.host.CallCount("CreateBuffer"))
	}
}

// bigDoc is a 150 000 line document with a section every 400 lines and a code block with a
// "# comment" in it every 1 000 lines.
func bigDoc() (doc string, headings int) {
	lines := make([]string, 150_000)
	for i := range lines {
		switch {
		case i%1000 == 100:
			lines[i] = "```sh"
		case i%1000 == 101:
			lines[i] = "# comment in code"
		case i%1000 == 102:
			lines[i] = "```"
		case i%400 == 0:
			lines[i] = fmt.Sprintf("## Section %d", i/400)
			headings++
		default:
			lines[i] = "plain text of line " + fmt.Sprint(i)
		}
	}
	return strings.Join(lines, "\n"), headings
}

// The plugin never reads the whole buffer: a handler asks the host for the lines that look
// like headings and reads only those, and finishes inside 50 ms of the (manual) clock even
// when the host scans in small chunks.
func TestMarkdownTocBigBufferReadsLittle(t *testing.T) {
	t.Parallel()
	doc, headings := bigDoc()
	e := newToc(t, doc, nil)
	e.host.SetFindLinesChunk(20_000)

	finish := e.finish

	from := e.mark()
	e.show()
	if took := finish(); took > 50*time.Millisecond {
		t.Fatalf("the first scan took %v of clock", took)
	}
	rows := strings.Count(e.text(), "\n") + 1
	if rows != headings {
		t.Fatalf("outline has %d rows, want %d", rows, headings)
	}
	if got := strings.Count(e.text(), "comment in code"); got != 0 {
		t.Fatalf("a comment in a code block is listed")
	}
	if total, widest := e.linesRead(from); total >= 2000 || widest != 1 {
		t.Fatalf("the first scan read %d lines (widest call %d), want < 2000 one at a time", total, widest)
	}

	// an edit near the end
	recs := e.edit(149_000, 0, 149_000, 0, "## Added\n")
	from = e.mark()
	e.h.Emit(pluginapi.Event{Name: "buffer_changed", Data: map[string]any{
		"buf": e.doc, "version": e.version(), "edits": recs,
	}})
	e.h.Advance(300 * time.Millisecond)
	took := finish()
	if total, _ := e.linesRead(from); total >= 20 {
		t.Fatalf("an edit made the plugin read %d lines", total)
	}
	for _, q := range e.findQueries(from) {
		if q.From < 148_000 || q.To <= 0 || q.To-q.From > 10 {
			t.Fatalf("an edit scanned %+v", q)
		}
	}
	if took > 50*time.Millisecond {
		t.Fatalf("the edit took %v of clock after its debounce", took)
	}
	if got := e.text(); !strings.Contains(got, "\nAdded\n") && !strings.Contains(got, "Added\n") {
		t.Fatalf("the new heading is missing from the outline")
	}

	// a change with no records: a new scan of everything, still without reading the buffer
	from = e.mark()
	e.overflowed()
	took = finish()
	if total, widest := e.linesRead(from); total >= 2000 || widest != 1 {
		t.Fatalf("the rescan read %d lines (widest call %d)", total, widest)
	}
	if took > 50*time.Millisecond {
		t.Fatalf("the rescan took %v of clock after its debounce", took)
	}
	if rows := strings.Count(e.text(), "\n") + 1; rows != headings+1 {
		t.Fatalf("outline has %d rows after the rescan, want %d", rows, headings+1)
	}
}

// While the first scan of a huge document is still going the outline says so, instead of
// showing an empty window, and the headings replace the note when the scan is done.
func TestMarkdownTocShowsScanningDuringTheFirstScan(t *testing.T) {
	t.Parallel()
	var sb strings.Builder
	for i := range 5000 {
		fmt.Fprintf(&sb, "# H%d\n", i)
	}
	e := newToc(t, sb.String(), nil)
	e.show()
	if got := e.text(); got != "(scanning…)" {
		t.Fatalf("outline during the scan = %q", got)
	}
	e.finish()
	if got := e.text(); !strings.HasPrefix(got, "H0\nH1\n") || strings.Contains(got, "scanning") {
		t.Fatalf("outline after the scan starts %.20q", got)
	}
}

// A document with more headings than the plugin keeps is cut short, with a note, and the
// plugin does not read all of them.
func TestMarkdownTocCapsHugeOutlines(t *testing.T) {
	t.Parallel()
	var sb strings.Builder
	for i := range 30_000 {
		fmt.Fprintf(&sb, "# H%d\n", i)
	}
	e := newToc(t, sb.String(), nil)
	e.show()
	e.finish()
	got := e.text()
	if !strings.HasSuffix(got, "… the outline is cut short") {
		t.Fatalf("no note at the end of %q (log %v, notices %v)", got[max(0, len(got)-60):], e.h.LogText(""), e.h.NoticeTexts())
	}
	if rows := strings.Count(got, "\n"); rows != 5000 { // 4999 between the rows, one before the note
		t.Fatalf("%d rows", rows)
	}
	if total, _ := e.linesRead(0); total > 20_000 {
		t.Fatalf("read %d lines", total)
	}
}
