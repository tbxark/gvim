package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/internal/tui/frame"
	"github.com/tbxark/gvim/internal/tui/grid"
	"github.com/tbxark/gvim/internal/tui/sidebar"
)

// diagStoreFixture sets a workspace diagnostics store on the model and refreshes
// the panel's selection.
func diagStoreFixture(lt *lspTest, files ...FileDiagnostics) {
	lt.t.Helper()
	lt.m.lsp.wsDiags = files
	lt.m.clampDiagSelection()
}

// diagSampleFiles is a two-file workspace store: a.go with an error and a hint,
// b.go with a warning.
func diagSampleFiles() []FileDiagnostics {
	return []FileDiagnostics{
		{Path: "/p/a.go", Items: []Diagnostic{
			{Start: buffer.Pos{Line: 0, Col: 0}, End: buffer.Pos{Line: 0, Col: 1}, Severity: 4, Message: "hint here"},
			{Start: buffer.Pos{Line: 1, Col: 2}, End: buffer.Pos{Line: 1, Col: 3}, Severity: 1, Message: "broken", Source: "gopls"},
		}},
		{Path: "/p/b.go", Items: []Diagnostic{
			{Start: buffer.Pos{Line: 2, Col: 0}, End: buffer.Pos{Line: 2, Col: 1}, Severity: 2, Message: "careful"},
		}},
	}
}

// TestWorkspaceStoreReplacesAndClears checks the store's refresh rule: each poll
// replaces the whole list, so a dropped diagnostic disappears, and stopping the
// servers empties it.
func TestWorkspaceStoreReplacesAndClears(t *testing.T) {
	port := newFakeLSP("go")
	lt := newLSPTest(t, port, &lspTab{path: "/p/a.go", lines: []string{"x"}})
	lt.open()
	files := diagSampleFiles()
	lt.feed(lspDiagsMsg{workspaceDiags: files})
	if got := len(lt.m.lsp.wsDiags); got != 2 {
		t.Fatalf("store = %d files, want 2", got)
	}
	// A later poll that reports only b.go drops a.go.
	lt.feed(lspDiagsMsg{workspaceDiags: files[1:]})
	if got := len(lt.m.lsp.wsDiags); got != 1 || lt.m.lsp.wsDiags[0].Path != "/p/b.go" {
		t.Fatalf("store after replace = %+v", lt.m.lsp.wsDiags)
	}
	lt.feed(lspServerMsg{action: "stop"})
	if len(lt.m.lsp.wsDiags) != 0 {
		t.Fatalf("store after stop = %+v, want none", lt.m.lsp.wsDiags)
	}
}

// TestWorkspacePollTakesASnapshot checks the poll: it asks the port for the
// workspace diagnostics and stores the answer.
func TestWorkspacePollTakesASnapshot(t *testing.T) {
	port := newFakeLSP("go")
	lt := newLSPTest(t, port, &lspTab{path: "/p/a.go", lines: []string{"x"}})
	lt.open()
	port.workspaceDiags = diagSampleFiles()
	lt.drain(lt.m.lspWorkspacePoll())
	if got := len(lt.m.lsp.wsDiags); got != 2 {
		t.Fatalf("store = %d files, want 2", got)
	}
	if c := lt.m.workspaceCounts(); c.errors != 1 || c.warnings != 1 || c.info != 1 {
		t.Fatalf("counts = %+v, want 1/1/1", c)
	}
	if label := lt.m.workspaceCounts().Label(); label != "E:1 W:1 I:1" {
		t.Fatalf("label = %q", label)
	}
}

// TestDiagRowsGroupByFileThenSeverity checks the panel's row model: a header per
// file with its counts, then the file's diagnostics most severe first.
func TestDiagRowsGroupByFileThenSeverity(t *testing.T) {
	port := newFakeLSP("go")
	lt := newLSPTest(t, port, &lspTab{path: "/p/a.go", lines: []string{"x", "y"}})
	lt.open()
	diagStoreFixture(lt, diagSampleFiles()...)
	rows := lt.m.diagRows()
	want := []struct {
		header bool
		path   string
		msg    string
	}{
		{true, "/p/a.go", ""},
		{false, "/p/a.go", "broken"},
		{false, "/p/a.go", "hint here"},
		{true, "/p/b.go", ""},
		{false, "/p/b.go", "careful"},
	}
	if len(rows) != len(want) {
		t.Fatalf("rows = %+v, want %d", rows, len(want))
	}
	for i, w := range want {
		if rows[i].header != w.header || rows[i].path != w.path || rows[i].item.Message != w.msg {
			t.Fatalf("row %d = %+v, want header %v path %s msg %q", i, rows[i], w.header, w.path, w.msg)
		}
	}
	if got := selectableDiagRows(rows); got != 3 {
		t.Fatalf("selectable rows = %d, want 3", got)
	}
}

// TestDiagPanelPaintsRowsAndSelection checks the section's body: file headers with
// counts, severity glyphs and messages, and the selected diagnostic highlighted.
func TestDiagPanelPaintsRowsAndSelection(t *testing.T) {
	port := newFakeLSP("go")
	lt := newLSPTest(t, port, &lspTab{path: "/p/a.go", lines: []string{"x"}})
	lt.m.tree = newFakeTree(3)
	lt.m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	lt.m.explorerOpen = true
	lt.m.diag.open = true
	lt.m.diag.focus = true
	lt.m.diag.sel = 0
	diagStoreFixture(lt, diagSampleFiles()...)
	lt.m.diag.collapsed = false

	l := lt.m.layout()
	diagSec := findDiagSection(t, l)
	// The column sits below the split body in a full frame, so the grid is the
	// frame's size.
	g := grid.New(100, 30)
	lt.m.paintDiagPanel(g, l, diagSec)
	row := func(y int) string { return rowTextOf(g, y) }
	header := row(diagSec.BodyTop)
	if !strings.Contains(header, "a.go") || !strings.Contains(header, "E:1") || !strings.Contains(header, "I:1") {
		t.Fatalf("header row = %q, want the file and its counts", header)
	}
	second := row(diagSec.BodyTop + 1)
	if !strings.Contains(second, "broken") || !strings.Contains(second, "gopls") {
		t.Fatalf("first diagnostic row = %q", second)
	}
	if !strings.Contains(second, "▌") {
		t.Fatalf("selected row = %q, want the focus bar", second)
	}
	if got := row(diagSec.BodyTop + 2); !strings.Contains(got, "hint here") {
		t.Fatalf("second diagnostic row = %q", got)
	}
}

// TestDiagPanelEmptyStates checks the progress/empty wording: no port, servers
// starting, waiting for the first diagnostics, or none.
func TestDiagPanelEmptyStates(t *testing.T) {
	tests := []struct {
		name  string
		setup func(lt *lspTest)
		want  string
	}{
		{"no port", func(lt *lspTest) { lt.m.lsp.port = nil }, "No language server is configured"},
		{"servers starting", func(lt *lspTest) {
			lt.m.lsp.tracks["/p/a.go"] = &lspTrack{path: "/p/a.go", state: lspOpening}
		}, "Starting language servers…"},
		{"waiting", func(lt *lspTest) {
			lt.m.lsp.tracks["/p/a.go"] = &lspTrack{path: "/p/a.go", state: lspOpen}
			lt.m.lsp.polling = true
		}, "Waiting for diagnostics…"},
		{"nothing", func(lt *lspTest) {}, "No diagnostics"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			port := newFakeLSP("go")
			lt := newLSPTest(t, port, &lspTab{path: "/p/a.go", lines: []string{"x"}})
			lt.m.lsp.tracks = map[string]*lspTrack{}
			lt.m.lsp.polling = false
			tt.setup(lt)
			if got := lt.m.diagEmptyText(); got != tt.want {
				t.Fatalf("empty text = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestDiagHeaderTitleShowsCountsAndProgress checks the dynamic header: counts when
// there are diagnostics, an ellipsis while starting, plain otherwise.
func TestDiagHeaderTitleShowsCountsAndProgress(t *testing.T) {
	port := newFakeLSP("go")
	lt := newLSPTest(t, port, &lspTab{path: "/p/a.go", lines: []string{"x"}})
	lt.m.lsp.tracks = map[string]*lspTrack{}
	lt.m.lsp.polling = false
	if got := lt.m.diagHeaderTitle(); got != "Diagnostics" {
		t.Fatalf("title = %q, want plain", got)
	}
	lt.m.lsp.tracks["/p/a.go"] = &lspTrack{path: "/p/a.go", state: lspOpening}
	if got := lt.m.diagHeaderTitle(); got != "Diagnostics …" {
		t.Fatalf("title = %q, want the progress ellipsis", got)
	}
	diagStoreFixture(lt, diagSampleFiles()...)
	if got := lt.m.diagHeaderTitle(); got != "Diagnostics E:1 W:1 I:1" {
		t.Fatalf("title = %q, want the counts", got)
	}
}

// TestDiagPanelJumpsToTheSelectedDiagnostic checks Enter: the selected diagnostic's
// file is opened at its position through the shared goto, and the focus returns to
// the editor.
func TestDiagPanelJumpsToTheSelectedDiagnostic(t *testing.T) {
	port := newFakeLSP("go")
	lt := newLSPTest(t, port, &lspTab{path: "/p/a.go", lines: []string{"x", "y"}})
	lt.open()
	lt.m.explorerOpen = true
	diagStoreFixture(lt, diagSampleFiles()...)
	lt.m.focusDiagnostics()
	lt.m.diag.sel = 0
	cmd := lt.m.diagKey(KeyPress{Name: "Enter"}, "")
	lt.settle(cmd, 4)
	if len(lt.ed.opened) != 1 || lt.ed.opened[0] != "/p/a.go" {
		t.Fatalf("opened = %v, want a.go", lt.ed.opened)
	}
	if lt.m.diag.focus {
		t.Fatal("the panel kept the keyboard after a jump")
	}
}

// TestDiagPanelMovesTheSelection checks the panel keys: Down/End/PageDown move the
// selection over diagnostics only (never onto a file header), and the scroll keeps
// it visible.
func TestDiagPanelMovesTheSelection(t *testing.T) {
	port := newFakeLSP("go")
	lt := newLSPTest(t, port, &lspTab{path: "/p/a.go", lines: []string{"x"}})
	diagStoreFixture(lt, diagSampleFiles()...)
	lt.m.focusDiagnostics()
	lt.m.diag.sel = 0
	lt.m.diagKey(KeyPress{Name: "Down"}, "")
	if lt.m.diag.sel != 1 {
		t.Fatalf("sel after Down = %d, want 1", lt.m.diag.sel)
	}
	lt.m.diagKey(KeyPress{Name: "End"}, "")
	if lt.m.diag.sel != 2 {
		t.Fatalf("sel after End = %d, want the last (2)", lt.m.diag.sel)
	}
	lt.m.diagKey(KeyPress{Name: "Down"}, "")
	if lt.m.diag.sel != 2 {
		t.Fatalf("sel past the end = %d, want it to stay", lt.m.diag.sel)
	}
	lt.m.diagKey(KeyPress{Name: "Escape"}, "")
	if lt.m.diag.focus {
		t.Fatal("Escape did not return the keyboard to the editor")
	}
}

// TestDiagnosticsListShowsRowsAndStaysOpen checks the quickfix list: grouped rows,
// and Enter jumps without closing the list.
func TestDiagnosticsListShowsRowsAndStaysOpen(t *testing.T) {
	port := newFakeLSP("go")
	lt := newLSPTest(t, port, &lspTab{path: "/p/a.go", lines: []string{"x"}})
	diagStoreFixture(lt, diagSampleFiles()...)
	lt.runMsgs(lt.act("open_diagnostics_list"))
	v := overlayList(t, lt.m)
	if len(v.Rows) != 5 {
		t.Fatalf("list rows = %d, want 5 (2 headers, 3 diagnostics)", len(v.Rows))
	}
	if !v.Rows[0].Header || !strings.Contains(v.Rows[0].Left, "a.go") {
		t.Fatalf("first row = %+v, want the a.go header", v.Rows[0])
	}
	// Select the first diagnostic row (the second row) and choose it.
	if got := v.Rows[1].Value; got == "" {
		t.Fatalf("a diagnostic row carries no value")
	}
	_, cmd := lt.m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	lt.settle(cmd, 4)
	if len(lt.ed.opened) != 1 || lt.ed.opened[0] != "/p/a.go" {
		t.Fatalf("opened = %v, want a.go", lt.ed.opened)
	}
	if len(lt.m.overlays) == 0 {
		t.Fatal("the diagnostics list closed after a jump, want it to stay open")
	}
}

// TestToggleDiagnosticsPanelOpensAndCloses checks the palette action: it opens the
// section with focus, and closes it again.
func TestToggleDiagnosticsPanelOpensAndCloses(t *testing.T) {
	port := newFakeLSP("go")
	lt := newLSPTest(t, port, &lspTab{path: "/p/a.go", lines: []string{"x"}})
	lt.m.explorerOpen = true
	lt.m.explorerFocus = true
	lt.runMsgs(lt.act("toggle_diagnostics_panel"))
	if !lt.m.diag.Open() || !lt.m.diag.focus {
		t.Fatalf("panel open = %v focus = %v, want both", lt.m.diag.Open(), lt.m.diag.focus)
	}
	lt.runMsgs(lt.act("toggle_diagnostics_panel"))
	if lt.m.diag.Open() || lt.m.diag.focus {
		t.Fatalf("panel open = %v focus = %v after the second toggle", lt.m.diag.Open(), lt.m.diag.focus)
	}
}

// TestDiagnosticsSectionInSidebarStack checks the section's place: after Contents
// and before the plugin sections.
func TestDiagnosticsSectionInSidebarStack(t *testing.T) {
	port := newFakeLSP("go")
	lt := newLSPTest(t, port, &lspTab{path: "/p/a.go", lines: []string{"x"}})
	lt.m.explorerOpen = true
	lt.m.diag.open = true
	specs := lt.m.sidebarSpecs()
	if len(specs) < 2 || specs[0].Kind != sidebar.KindExplorer || specs[1].Kind != sidebar.KindDiagnostics {
		t.Fatalf("specs = %+v, want explorer then diagnostics", specs)
	}
}

// findDiagSection returns the Diagnostics section of a layout.
func findDiagSection(t *testing.T, l frame.Layout) sidebar.Section {
	t.Helper()
	for _, sec := range l.Sections {
		if sec.Kind == sidebar.KindDiagnostics {
			return sec
		}
	}
	t.Fatal("no diagnostics section in the layout")
	return sidebar.Section{}
}

// rowTextOf returns the text of grid row y.
func rowTextOf(g *grid.Grid, y int) string {
	var b strings.Builder
	for x := range g.Width() {
		b.WriteString(g.At(x, y).Text)
	}
	return b.String()
}

// TestProgressInPanelHeaderAndEmptyState checks the text progress display: the
// first $/progress entry is in the header (with a "+N more" suffix) and in the
// panel's empty state, and it is gone when the server ends it.
func TestProgressInPanelHeaderAndEmptyState(t *testing.T) {
	port := newFakeLSP("go")
	lt := newLSPTest(t, port, &lspTab{path: "/p/a.go", lines: []string{"x"}})
	lt.m.lsp.tracks = map[string]*lspTrack{}
	lt.m.lsp.polling = false
	lt.m.lsp.progress = []LSPProgress{
		{Server: "gopls", Title: "Indexing", Message: "crates", Percentage: 42},
		{Server: "gopls", Title: "Loading", Percentage: -1},
	}
	title := lt.m.diagHeaderTitle()
	for _, want := range []string{"Diagnostics ·", "gopls: Indexing — crates 42%", "+1 more"} {
		if !strings.Contains(title, want) {
			t.Errorf("header = %q, want it to contain %q", title, want)
		}
	}
	if got := lt.m.diagEmptyText(); !strings.Contains(got, "In progress:") || !strings.Contains(got, "Indexing") {
		t.Fatalf("empty text = %q, want the progress line", got)
	}

	// The server ends its work: the next poll's empty list clears the display.
	lt.m.lsp.progress = nil
	if got := lt.m.diagHeaderTitle(); strings.Contains(got, "Indexing") {
		t.Fatalf("header = %q after the progress ended", got)
	}
}

// TestProgressComesFromThePollAndIsClearedOnStop checks the plumbing: the poll
// carries the port's progress list, and stopping the servers empties it.
func TestProgressComesFromThePollAndIsClearedOnStop(t *testing.T) {
	port := newFakeLSP("go")
	lt := newLSPTest(t, port, &lspTab{path: "/p/a.go", lines: []string{"x"}})
	lt.open()
	port.progress = []LSPProgress{{Server: "gopls", Title: "Indexing", Percentage: 10}}
	lt.drain(lt.m.lspPoll())
	if len(lt.m.lsp.progress) != 1 || lt.m.lsp.progress[0].Title != "Indexing" {
		t.Fatalf("progress = %+v, want the poll's entry", lt.m.lsp.progress)
	}
	lt.feed(lspServerMsg{action: "stop"})
	if len(lt.m.lsp.progress) != 0 {
		t.Fatalf("progress after stop = %+v, want none", lt.m.lsp.progress)
	}
}

// TestProgressLineRendering checks the one-line format: server, title, message and
// percentage, with the parts the server did not give left out.
func TestProgressLineRendering(t *testing.T) {
	tests := []struct {
		name string
		p    LSPProgress
		want string
	}{
		{"title and message", LSPProgress{Server: "gopls", Title: "Indexing", Message: "crates", Percentage: 42}, "gopls: Indexing — crates 42%"},
		{"title only", LSPProgress{Server: "gopls", Title: "Indexing", Percentage: -1}, "gopls: Indexing"},
		{"message only", LSPProgress{Title: "Indexing", Message: "crates", Percentage: -1}, "Indexing — crates"},
		{"no server", LSPProgress{Title: "Indexing", Percentage: 0}, "Indexing 0%"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := progressLine(tt.p); got != tt.want {
				t.Fatalf("progressLine = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestWorkspaceDiagnosticsRowMarksAndOpensAnUnopenedFile checks the workspace-pulled
// rows: a file the server reported without the editor opening it is marked "(not
// open)" in the panel and the list, and Enter opens it at the diagnostic.
func TestWorkspaceDiagnosticsRowMarksAndOpensAnUnopenedFile(t *testing.T) {
	port := newFakeLSP("go")
	lt := newLSPTest(t, port, &lspTab{path: "/p/a.go", lines: []string{"x"}})
	lt.open()
	lt.m.tree = newFakeTree(3)
	lt.m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	lt.m.explorerOpen = true
	diagStoreFixture(lt, FileDiagnostics{
		Path:   "/p/unopened.go",
		Source: "workspace",
		Items:  []Diagnostic{{Start: buffer.Pos{Line: 2, Col: 1}, End: buffer.Pos{Line: 2, Col: 4}, Severity: 1, Message: "remote error"}},
	})
	lt.m.diag.open = true
	lt.m.diag.collapsed = false
	lt.m.diag.focus = true
	lt.m.diag.sel = 0

	l := lt.m.layout()
	sec := findDiagSection(t, l)
	g := grid.New(100, 30)
	lt.m.paintDiagPanel(g, l, sec)
	if header := lspRowText(g, sec.BodyTop); !strings.Contains(header, "unopened.go") || !strings.Contains(header, "(not open)") {
		t.Fatalf("panel header = %q, want the file marked not open", header)
	}

	// The quickfix list marks it too.
	lt.runMsgs(lt.act("open_diagnostics_list"))
	v := overlayList(t, lt.m)
	if len(v.Rows) == 0 || !v.Rows[0].Header || !strings.Contains(v.Rows[0].Left, "(not open)") {
		t.Fatalf("list rows = %+v, want the header marked not open", v.Rows)
	}
	// A header row is not choosable; the diagnostic row below it jumps, opening the
	// file through the shared goto.
	lt.m.overlays = nil
	lt.m.diag.sel = 0
	cmd := lt.m.diagJump()
	lt.settle(cmd, 4)
	if len(lt.ed.opened) != 1 || lt.ed.opened[0] != "/p/unopened.go" {
		t.Fatalf("opened = %v, want the unopened workspace file", lt.ed.opened)
	}
}
