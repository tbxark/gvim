package ui

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/buffer"
	"github.com/tbxark/gvim/internal/editor"
	"github.com/tbxark/gvim/internal/syntax"
	"github.com/tbxark/gvim/internal/theme"
	"github.com/tbxark/gvim/internal/ui/popup"
	"github.com/tbxark/gvim/internal/ui/prompt"
)

// lspTab is one tab of lspEd: its path, its lines and its caret.
type lspTab struct {
	path   string
	lines  []string
	cursor buffer.Pos
	// large makes the tab's document report large file mode, with size bytes.
	large bool
	size  int64
}

// lspEd is an editor whose tabs have paths. It records the files opened, the
// lines it was sent to, and the edits applied to each path.
type lspEd struct {
	tabs    []*lspTab
	active  int
	opened  []string
	gotos   [][2]int
	applied map[string][]editor.LSPEdit
	// loadErr is what the read of a goto returns, when it is set.
	loadErr error
	// actions records the editor actions the root ran, in order.
	actions []string
}

func (e *lspEd) SetViewport(int, int) {}

// Do records the action. insert_char also types its text into the active tab at
// the caret, so a test can read the document back.
func (e *lspEd) Do(a editor.Action, args map[string]any) editor.Result {
	e.actions = append(e.actions, string(a))
	if a == "insert_char" && len(e.tabs) > 0 {
		t := e.tabs[e.active]
		text, _ := args["text"].(string)
		line := []rune(t.lines[t.cursor.Line])
		col := t.cursor.Col
		t.lines[t.cursor.Line] = string(line[:col]) + text + string(line[col:])
		t.cursor.Col += len([]rune(text))
	}
	return editor.Result{}
}
func (e *lspEd) Active() Document {
	if len(e.tabs) == 0 {
		return nil
	}
	return &lspDoc{t: e.tabs[e.active]}
}
func (e *lspEd) DocAt(i int) Document {
	if i < 0 || i >= len(e.tabs) {
		return nil
	}
	return &lspDoc{t: e.tabs[i]}
}
func (e *lspEd) Tabs() []TabInfo {
	out := make([]TabInfo, len(e.tabs))
	for i, t := range e.tabs {
		out[i] = TabInfo{Name: filepath.Base(t.path), Active: i == e.active}
	}
	return out
}
func (e *lspEd) Status() string { return "" }
func (e *lspEd) SetCursor(p buffer.Pos, _ bool) {
	e.tabs[e.active].cursor = p
}
func (e *lspEd) ExtendSelection(buffer.Pos, editor.Unit) {}
func (e *lspEd) ScrollBy(int)                            {}
func (e *lspEd) ScrollTo(int)                            {}
func (e *lspEd) OpenFile(path string) error {
	e.opened = append(e.opened, path)
	return nil
}
func (e *lspEd) OpenPreview(string) error { return nil }

// LoadFunc returns the read of path: an empty document that carries the path, or
// loadErr when that is set.
func (e *lspEd) LoadFunc(path string) func() (*editor.Document, error) {
	return func() (*editor.Document, error) {
		if e.loadErr != nil {
			return nil, e.loadErr
		}
		return &editor.Document{Path: path}, nil
	}
}

// OpenLoaded records the path of a document that LoadFunc read as opened.
func (e *lspEd) OpenLoaded(d *editor.Document) {
	e.opened = append(e.opened, d.Path)
}
func (e *lspEd) GotoLine(line, col int) error {
	e.gotos = append(e.gotos, [2]int{line, col})
	return nil
}
func (e *lspEd) ApplyLSPEdits(path string, edits []editor.LSPEdit) error {
	if e.applied == nil {
		e.applied = map[string][]editor.LSPEdit{}
	}
	e.applied[path] = append(e.applied[path], edits...)
	return nil
}

// lspDoc is the view of one lspTab that the root reads.
type lspDoc struct{ t *lspTab }

func (d *lspDoc) Name() string         { return filepath.Base(d.t.path) }
func (d *lspDoc) FilePath() string     { return d.t.path }
func (d *lspDoc) Modified() bool       { return false }
func (d *lspDoc) LineCount() int       { return max(len(d.t.lines), 1) }
func (d *lspDoc) Line(i int) string    { return d.t.lines[i] }
func (d *lspDoc) SavedLines() []string { return d.t.lines }
func (d *lspDoc) Top() int             { return 0 }
func (d *lspDoc) Encoding() string     { return "UTF-8" }
func (d *lspDoc) LineEnding() string   { return "LF" }
func (d *lspDoc) Cursor() buffer.Pos   { return d.t.cursor }
func (d *lspDoc) Selection() (a, h buffer.Pos, ok bool) {
	return d.t.cursor, d.t.cursor, false
}
func (d *lspDoc) LineSpans(int) []syntax.Span { return nil }

// Large and Index are the large file view of the document, when its tab is large.
func (d *lspDoc) Large() bool { return d.t.large }
func (d *lspDoc) Index() buffer.IndexInfo {
	return buffer.IndexInfo{Size: d.t.size, Indexed: d.t.size, Lines: d.LineCount(), Complete: true}
}

// fakeLSP is an LSPSession that records its calls and returns canned results.
type fakeLSP struct {
	langs    map[string]bool
	opens    []string
	changes  []string
	closes   []string
	restarts int
	stops    int
	root     string

	defs    []LSPLocation
	refs    []LSPLocation
	hover   string
	sig     string // the signature help text, read by SignatureHelp
	items   []CompletionItem
	renames []FileEdit
	titles  []string
	reqErr  error
	diags   map[string]DiagnosticsEvent

	renameName string
}

func newFakeLSP(langs ...string) *fakeLSP {
	f := &fakeLSP{langs: map[string]bool{}, diags: map[string]DiagnosticsEvent{}}
	for _, l := range langs {
		f.langs[l] = true
	}
	return f
}

func (f *fakeLSP) SetRoot(root string)          { f.root = root }
func (f *fakeLSP) Supports(lang string) bool    { return f.langs[lang] }
func (f *fakeLSP) Open(path, _, _ string) error { f.opens = append(f.opens, path); return nil }
func (f *fakeLSP) Change(_, text string) error  { f.changes = append(f.changes, text); return nil }
func (f *fakeLSP) Close(path string) error      { f.closes = append(f.closes, path); return nil }
func (f *fakeLSP) Restart() error               { f.restarts++; return nil }
func (f *fakeLSP) Stop() error                  { f.stops++; return nil }
func (f *fakeLSP) Diagnostics(path string) (DiagnosticsEvent, bool) {
	ev, ok := f.diags[path]
	return ev, ok
}
func (f *fakeLSP) Definition(string, buffer.Pos) ([]LSPLocation, error) {
	return f.defs, f.reqErr
}
func (f *fakeLSP) Implementation(string, buffer.Pos) ([]LSPLocation, error) {
	return f.defs, f.reqErr
}
func (f *fakeLSP) References(string, buffer.Pos) ([]LSPLocation, error) {
	return f.refs, f.reqErr
}
func (f *fakeLSP) Hover(string, buffer.Pos) (string, error) { return f.hover, f.reqErr }
func (f *fakeLSP) Completion(string, buffer.Pos) ([]CompletionItem, error) {
	return f.items, f.reqErr
}
func (f *fakeLSP) Rename(_ string, _ buffer.Pos, name string) ([]FileEdit, error) {
	f.renameName = name
	return f.renames, f.reqErr
}
func (f *fakeLSP) CodeActions(string, buffer.Pos) ([]string, error) {
	return f.titles, f.reqErr
}

// SignatureHelp returns the canned signature text.
func (f *fakeLSP) SignatureHelp(string, buffer.Pos) (string, error) { return f.sig, f.reqErr }

// lspTest is one model under test, with its fakes.
type lspTest struct {
	t    *testing.T
	m    *Model
	ed   *lspEd
	port *fakeLSP
}

// newLSPTest builds a root model with tabs and port. The debounce and poll timers
// are set to a millisecond, so a command that waits on them returns at once.
func newLSPTest(t *testing.T, port *fakeLSP, tabs ...*lspTab) *lspTest {
	t.Helper()
	lspDebounce, lspPollEvery = time.Millisecond, time.Millisecond
	t.Cleanup(func() { lspDebounce, lspPollEvery = 300*time.Millisecond, 300*time.Millisecond })
	ed := &lspEd{tabs: tabs}
	keys := fakeKeymap{
		"normal|F12|":       {Action: "lsp_goto_definition"},
		"normal|Backspace|": {Action: "delete_backward"},
		"normal|Left|":      {Action: "move_left"},
		"normal|Right|":     {Action: "move_right"},
	}
	// A nil *fakeLSP must not reach Deps as a non-nil interface.
	var session LSPSession
	if port != nil {
		session = port
	}
	m := New(Deps{Editor: ed, Keys: keys, Theme: theme.Theme{}, Clock: testClock, LSP: session})
	lt := &lspTest{t: t, m: m, ed: ed, port: port}
	// The first sync opens the active tab. The window size is a message the root
	// takes like any other, and the commands it starts run as they would in a program.
	lt.settle(lt.touchCmd(), 3)
	return lt
}

// settle runs cmd and feeds the messages it produces back to the model, for at
// most rounds rounds. The poll and the debounce are timers, so the rounds bound
// them. It is how a test lets an open or a change finish.
func (lt *lspTest) settle(cmd tea.Cmd, rounds int) {
	lt.t.Helper()
	for range rounds {
		msgs := deliver(cmd)
		if len(msgs) == 0 {
			return
		}
		cmds := make([]tea.Cmd, 0, len(msgs))
		for _, msg := range msgs {
			_, c := lt.m.Update(msg)
			cmds = append(cmds, c)
		}
		cmd = tea.Batch(cmds...)
	}
}

// deliver runs cmd and returns the messages it produced, without feeding them
// back. A batch is run one command at a time.
func deliver(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		var out []tea.Msg
		for _, c := range batch {
			out = append(out, deliver(c)...)
		}
		return out
	}
	if msg == nil {
		return nil
	}
	return []tea.Msg{msg}
}

// feed sends msg to the model and delivers the commands it starts. It returns the
// messages those commands produced.
func (lt *lspTest) feed(msg tea.Msg) []tea.Msg {
	lt.t.Helper()
	_, cmd := lt.m.Update(msg)
	return deliver(cmd)
}

// touch sends a message that makes the root sync its documents, and returns the
// messages the sync started.
func (lt *lspTest) touch() []tea.Msg {
	lt.t.Helper()
	return lt.feed(tea.WindowSizeMsg{Width: 80, Height: 24})
}

// open runs the first sync, which opens the active tab, and feeds the reply.
func (lt *lspTest) open() {
	lt.t.Helper()
	lt.settle(lt.touchCmd(), 3)
}

// act runs the action, as a keypress would, and delivers the commands it starts.
func (lt *lspTest) act(action string) []tea.Msg {
	lt.t.Helper()
	return deliver(lt.m.dispatch(Command{Action: action}))
}

func (lt *lspTest) popup() *popup.Popup {
	lt.t.Helper()
	if n := len(lt.m.overlays); n > 0 {
		if po, ok := lt.m.overlays[n-1].(popupOverlay); ok {
			return po.p
		}
	}
	lt.t.Fatal("no popup on top of the overlay stack")
	return nil
}

func lspLoc(path string, line, col int) LSPLocation {
	return LSPLocation{Path: path, Start: buffer.Pos{Line: line, Col: col}, End: buffer.Pos{Line: line, Col: col + 1}}
}

func TestLSPOpensTheActiveDocumentOnce(t *testing.T) {
	port := newFakeLSP("go")
	tab := &lspTab{path: "/p/a.go", lines: []string{"package a"}}
	lt := newLSPTest(t, port, tab)

	lt.open()
	lt.touch()
	if got := port.opens; len(got) != 1 || got[0] != "/p/a.go" {
		t.Fatalf("opens = %v, want [/p/a.go] once", got)
	}
}

func TestLSPSendsDebouncedChangesWithTheLatestText(t *testing.T) {
	port := newFakeLSP("go")
	tab := &lspTab{path: "/p/a.go", lines: []string{"package a"}}
	lt := newLSPTest(t, port, tab)
	lt.open()

	// Two edits before the debounce timer fires: the first timer is stale.
	tab.lines = []string{"package b"}
	first := lt.touchCmd()
	tab.lines = []string{"package c"}
	second := lt.touchCmd()

	stale := deliver(first)
	fresh := deliver(second)
	if len(stale) != 1 || len(fresh) != 1 {
		t.Fatalf("timers gave %d and %d messages, want one each", len(stale), len(fresh))
	}
	if msgs := lt.feed(stale[0]); len(msgs) != 0 {
		t.Fatalf("stale timer started %d commands, want none", len(msgs))
	}
	for _, msg := range lt.feed(fresh[0]) {
		lt.feed(msg)
	}
	if len(port.changes) != 1 || port.changes[0] != "package c" {
		t.Fatalf("changes = %q, want one change with the latest text", port.changes)
	}
}

// touchCmd is touch, but it returns the command so the test controls when it runs.
func (lt *lspTest) touchCmd() tea.Cmd {
	lt.t.Helper()
	_, cmd := lt.m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	return cmd
}

func TestLSPRunsNoServerForAnUnconfiguredLanguage(t *testing.T) {
	port := newFakeLSP("rust")
	tab := &lspTab{path: "/p/a.go", lines: []string{"package a"}}
	lt := newLSPTest(t, port, tab)
	lt.touch()
	lt.act("lsp_goto_definition")

	if len(port.opens) != 0 || len(port.defs) != 0 {
		t.Fatalf("opens = %v, want none without a server", port.opens)
	}
	if !strings.Contains(lt.m.notice, "No language server for go") {
		t.Fatalf("notice = %q, want the no-server notice", lt.m.notice)
	}
}

func TestLSPWithoutAPortNoticesEveryAction(t *testing.T) {
	tab := &lspTab{path: "/p/a.go", lines: []string{"package a"}}
	lt := newLSPTest(t, nil, tab)
	for _, action := range []string{"lsp_hover", "lsp_references", "lsp_rename", "lsp_restart"} {
		lt.act(action)
		if lt.m.notice == "" {
			t.Fatalf("%s: no notice without a language server", action)
		}
	}
}

func TestLSPDefinitionOneLocationOpensTheFile(t *testing.T) {
	port := newFakeLSP("go")
	port.defs = []LSPLocation{lspLoc("/p/b.go", 3, 4)}
	lt := newLSPTest(t, port, &lspTab{path: "/p/a.go", lines: []string{"package a"}})
	lt.open()

	// The reply starts the read of the file, and the tab opens when the read's
	// message is fed back.
	var read []tea.Msg
	for _, msg := range lt.act("lsp_goto_definition") {
		read = append(read, lt.feed(msg)...)
	}
	if len(lt.ed.opened) != 0 {
		t.Fatalf("opened = %v before the read arrives, want none", lt.ed.opened)
	}
	for _, msg := range read {
		lt.feed(msg)
	}
	if len(lt.ed.opened) != 1 || lt.ed.opened[0] != "/p/b.go" {
		t.Fatalf("opened = %v, want [/p/b.go]", lt.ed.opened)
	}
	if len(lt.ed.gotos) != 1 || lt.ed.gotos[0] != [2]int{4, 5} {
		t.Fatalf("gotos = %v, want the 1-based position (4, 5)", lt.ed.gotos)
	}
}

func TestLSPDefinitionReadErrorNoticesAndOpensNothing(t *testing.T) {
	port := newFakeLSP("go")
	port.defs = []LSPLocation{lspLoc("/p/b.go", 3, 4)}
	lt := newLSPTest(t, port, &lspTab{path: "/p/a.go", lines: []string{"package a"}})
	lt.open()
	lt.ed.loadErr = errors.New("open /p/b.go: permission denied")

	var read []tea.Msg
	for _, msg := range lt.act("lsp_goto_definition") {
		read = append(read, lt.feed(msg)...)
	}
	for _, msg := range read {
		lt.feed(msg)
	}
	if len(lt.ed.opened) != 0 || len(lt.ed.gotos) != 0 {
		t.Fatalf("opened = %v, gotos = %v after a failed read, want neither", lt.ed.opened, lt.ed.gotos)
	}
	if want := "open /p/b.go: permission denied"; lt.m.notice != want {
		t.Fatalf("notice = %q, want %q", lt.m.notice, want)
	}
}

func TestLSPDefinitionSeveralLocationsOpensAPopup(t *testing.T) {
	port := newFakeLSP("go")
	port.defs = []LSPLocation{lspLoc("/p/b.go", 1, 0), lspLoc("/p/c.go", 7, 2)}
	lt := newLSPTest(t, port, &lspTab{path: "/p/a.go", lines: []string{"package a"}})
	lt.open()

	for _, msg := range lt.act("lsp_goto_definition") {
		lt.feed(msg)
	}
	p := lt.popup()
	if p.Selected() != 0 {
		t.Fatalf("selected = %d, want the first location", p.Selected())
	}
	p.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	var read []tea.Msg
	for _, msg := range deliver(p.Update(tea.KeyPressMsg{Code: tea.KeyEnter})) {
		read = append(read, lt.feed(msg)...)
	}
	for _, msg := range read {
		lt.feed(msg)
	}
	if len(lt.ed.opened) != 1 || lt.ed.opened[0] != "/p/c.go" {
		t.Fatalf("opened = %v, want the chosen location /p/c.go", lt.ed.opened)
	}
}

func TestLSPLocationRequestsNoticeWhenEmptyOrFailed(t *testing.T) {
	tests := []struct {
		name   string
		action string
		err    error
		want   string
	}{
		{"no definition", "lsp_goto_definition", nil, "No definition found"},
		{"no implementation", "lsp_implementation", nil, "No implementation found"},
		{"no references", "lsp_references", nil, "No references found"},
		{"server error", "lsp_goto_definition", errors.New("server died"), "server died"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			port := newFakeLSP("go")
			port.reqErr = tt.err
			lt := newLSPTest(t, port, &lspTab{path: "/p/a.go", lines: []string{"package a"}})
			lt.open()
			for _, msg := range lt.act(tt.action) {
				lt.feed(msg)
			}
			if !strings.Contains(lt.m.notice, tt.want) {
				t.Fatalf("notice = %q, want it to contain %q", lt.m.notice, tt.want)
			}
		})
	}
}

func TestLSPReferencesOpenThePopupList(t *testing.T) {
	port := newFakeLSP("go")
	port.refs = []LSPLocation{lspLoc("/p/a.go", 0, 0), lspLoc("/p/b.go", 2, 2)}
	lt := newLSPTest(t, port, &lspTab{path: "/p/a.go", lines: []string{"package a"}})
	lt.open()

	for _, msg := range lt.act("lsp_references") {
		lt.feed(msg)
	}
	if lt.popup().Selected() != 0 {
		t.Fatal("the references popup did not open with its first row selected")
	}
}

func TestLSPHoverShowsTheTextInAPopup(t *testing.T) {
	port := newFakeLSP("go")
	port.hover = "func A()\n\nA is the first function."
	lt := newLSPTest(t, port, &lspTab{path: "/p/a.go", lines: []string{"package a"}})
	lt.open()

	for _, msg := range lt.act("lsp_hover") {
		lt.feed(msg)
	}
	lt.popup()
	if !frameHas(lt.m, "A is the first function.") {
		t.Fatal("the hover text is not on the frame")
	}
}

func TestLSPCompletionAcceptInsertsTheChosenItem(t *testing.T) {
	// The second item is chosen by moving the highlight down once.
	tests := []struct {
		name string
		item CompletionItem
		want editor.LSPEdit
	}{
		{
			name: "no edit range inserts at the caret",
			item: CompletionItem{Label: "alpha", Insert: "alpha"},
			want: editor.LSPEdit{From: buffer.Pos{Line: 0, Col: 2}, To: buffer.Pos{Line: 0, Col: 2}, Text: "alpha"},
		},
		{
			name: "the server edit range is replaced",
			item: CompletionItem{Label: "beta", Insert: "beta", Range: &LSPRange{Start: buffer.Pos{Line: 0, Col: 0}, End: buffer.Pos{Line: 0, Col: 2}}},
			want: editor.LSPEdit{From: buffer.Pos{Line: 0, Col: 0}, To: buffer.Pos{Line: 0, Col: 2}, Text: "beta"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			port := newFakeLSP("go")
			port.items = []CompletionItem{{Label: "first", Insert: "first"}, tt.item}
			tab := &lspTab{path: "/p/a.go", lines: []string{"pa"}, cursor: buffer.Pos{Line: 0, Col: 2}}
			lt := newLSPTest(t, port, tab)
			lt.open()

			for _, msg := range lt.act("lsp_completion") {
				lt.feed(msg)
			}
			lt.popup().Update(tea.KeyPressMsg{Code: tea.KeyDown})
			lt.feed(tea.KeyPressMsg{Code: tea.KeyTab})
			if got := lt.ed.applied["/p/a.go"]; len(got) != 1 || got[0] != tt.want {
				t.Fatalf("applied = %+v, want [%+v]", got, tt.want)
			}
		})
	}
}

func TestLSPCompletionDismissClosesThePopup(t *testing.T) {
	port := newFakeLSP("go")
	port.items = []CompletionItem{{Label: "alpha", Insert: "alpha"}}
	lt := newLSPTest(t, port, &lspTab{path: "/p/a.go", lines: []string{"pa"}})
	lt.open()
	for _, msg := range lt.act("lsp_completion") {
		lt.feed(msg)
	}
	lt.feed(tea.KeyPressMsg{Code: tea.KeyEscape})
	if len(lt.m.overlays) != 0 {
		t.Fatalf("overlays = %d after Esc, want the popup closed", len(lt.m.overlays))
	}
	if len(lt.ed.applied) != 0 {
		t.Fatalf("applied = %v after dismiss, want none", lt.ed.applied)
	}
}

func TestLSPRenameAppliesTheEditsToTheDocument(t *testing.T) {
	port := newFakeLSP("go")
	port.renames = []FileEdit{{Path: "/p/a.go", Edits: []editor.LSPEdit{{From: buffer.Pos{}, To: buffer.Pos{Col: 3}, Text: "New"}}}}
	tab := &lspTab{path: "/p/a.go", lines: []string{"   "}}
	lt := newLSPTest(t, port, tab)
	lt.open()

	lt.act("lsp_rename")
	lt.feed(tea.KeyPressMsg{Code: 'n', Text: "n"})
	lt.feed(tea.KeyPressMsg{Code: 'e', Text: "e"})
	for _, msg := range lt.feed(tea.KeyPressMsg{Code: tea.KeyEnter}) {
		for _, next := range lt.feed(msg) {
			lt.feed(next)
		}
	}
	if port.renameName != "ne" {
		t.Fatalf("rename name = %q, want the typed name", port.renameName)
	}
	if got := lt.ed.applied["/p/a.go"]; len(got) != 1 || got[0].Text != "New" {
		t.Fatalf("applied = %+v, want the rename edit", got)
	}
}

func TestLSPRenameEmptyNameDoesNothing(t *testing.T) {
	port := newFakeLSP("go")
	lt := newLSPTest(t, port, &lspTab{path: "/p/a.go", lines: []string{"   "}})
	lt.open()
	lt.act("lsp_rename")
	for _, msg := range lt.feed(tea.KeyPressMsg{Code: tea.KeyEnter}) {
		lt.feed(msg)
	}
	if port.renameName != "" {
		t.Fatalf("rename sent %q for an empty name, want no request", port.renameName)
	}
}

func TestLSPCodeActionsChoiceShowsANotice(t *testing.T) {
	port := newFakeLSP("go")
	port.titles = []string{"Add import"}
	lt := newLSPTest(t, port, &lspTab{path: "/p/a.go", lines: []string{"package a"}})
	lt.open()

	for _, msg := range lt.act("lsp_code_actions") {
		lt.feed(msg)
	}
	for _, msg := range deliver(lt.popup().Update(tea.KeyPressMsg{Code: tea.KeyEnter})) {
		lt.feed(msg)
	}
	if !strings.Contains(lt.m.notice, "not supported yet") {
		t.Fatalf("notice = %q, want the unsupported notice", lt.m.notice)
	}
}

func TestJumpToNextAndPreviousErrorWrap(t *testing.T) {
	port := newFakeLSP("go")
	lt := newLSPTest(t, port, &lspTab{path: "/p/a.go", lines: []string{"a", "b", "c", "d", "e", "f"}})
	lt.open()
	lt.feed(lspDiagsMsg{events: []DiagnosticsEvent{{Path: "/p/a.go", Items: []Diagnostic{
		{Start: buffer.Pos{Line: 2, Col: 1}, End: buffer.Pos{Line: 2, Col: 2}, Severity: 2},
		{Start: buffer.Pos{Line: 5, Col: 0}, End: buffer.Pos{Line: 5, Col: 1}, Severity: 1},
		{Start: buffer.Pos{Line: 3, Col: 0}, End: buffer.Pos{Line: 3, Col: 1}, Severity: 4}, // a hint is not an error
	}}}})

	tests := []struct {
		action string
		want   buffer.Pos
	}{
		{"jump_to_next_error", buffer.Pos{Line: 2, Col: 1}},
		{"jump_to_next_error", buffer.Pos{Line: 5, Col: 0}},
		{"jump_to_next_error", buffer.Pos{Line: 2, Col: 1}},
		{"jump_to_previous_error", buffer.Pos{Line: 5, Col: 0}},
	}
	for _, tt := range tests {
		lt.act(tt.action)
		if got := lt.ed.tabs[0].cursor; got != tt.want {
			t.Fatalf("after %s the caret is %+v, want %+v", tt.action, got, tt.want)
		}
	}
}

func TestDiagnosticsReachThePaneAndTheStatusRow(t *testing.T) {
	port := newFakeLSP("go")
	lt := newLSPTest(t, port, &lspTab{path: "/p/a.go", lines: []string{"abc", "def", "ghi"}})
	lt.open()
	lt.feed(lspDiagsMsg{events: []DiagnosticsEvent{{Path: "/p/a.go", Items: []Diagnostic{
		{Start: buffer.Pos{Line: 1, Col: 0}, End: buffer.Pos{Line: 1, Col: 2}, Severity: 1, Message: "bad"},
		{Start: buffer.Pos{Line: 2, Col: 1}, End: buffer.Pos{Line: 2, Col: 3}, Severity: 2, Message: "odd"},
	}}}})

	doc := lt.m.ed.Active()
	spans := lt.m.lspSpans(doc)
	if got := spans(1); len(got) != 1 || got[0].Start != 0 || got[0].End != 2 || got[0].Severity != 1 {
		t.Fatalf("spans(1) = %+v, want one error span over columns 0 to 2", got)
	}
	if got := spans(0); len(got) != 0 {
		t.Fatalf("spans(0) = %+v, want none", got)
	}
	if got := lt.m.statusView().warnings; got != 2 {
		t.Fatalf("warnings = %d, want 2", got)
	}
}

func TestDiagnosticsAreClearedByAnEmptyEvent(t *testing.T) {
	port := newFakeLSP("go")
	lt := newLSPTest(t, port, &lspTab{path: "/p/a.go", lines: []string{"abc"}})
	lt.open()
	lt.feed(lspDiagsMsg{events: []DiagnosticsEvent{{Path: "/p/a.go", Items: []Diagnostic{
		{Start: buffer.Pos{}, End: buffer.Pos{Col: 1}, Severity: 1},
	}}}})
	lt.feed(lspDiagsMsg{events: []DiagnosticsEvent{{Path: "/p/a.go"}}})
	if got := lt.m.statusView().warnings; got != 0 {
		t.Fatalf("warnings = %d after an empty event, want 0", got)
	}
}

func TestLSPRestartReopensTheDocument(t *testing.T) {
	port := newFakeLSP("go")
	lt := newLSPTest(t, port, &lspTab{path: "/p/a.go", lines: []string{"package a"}})
	lt.open()

	for _, msg := range lt.act("lsp_restart") {
		lt.feed(msg)
	}
	if port.restarts != 1 {
		t.Fatalf("restarts = %d, want 1", port.restarts)
	}
	lt.touch()
	if len(port.opens) != 2 {
		t.Fatalf("opens = %v, want the document opened again after the restart", port.opens)
	}
}

func TestLSPStopKeepsTheServerDown(t *testing.T) {
	port := newFakeLSP("go")
	lt := newLSPTest(t, port, &lspTab{path: "/p/a.go", lines: []string{"package a"}})
	lt.open()

	for _, msg := range lt.act("lsp_stop") {
		lt.feed(msg)
	}
	lt.touch()
	lt.touch()
	if port.stops != 1 {
		t.Fatalf("stops = %d, want 1", port.stops)
	}
	if len(port.opens) != 1 {
		t.Fatalf("opens = %v, want no reopen after stop", port.opens)
	}
}

func TestLSPToggleForBufferClosesAndKeepsItClosed(t *testing.T) {
	port := newFakeLSP("go")
	lt := newLSPTest(t, port, &lspTab{path: "/p/a.go", lines: []string{"package a"}})
	lt.open()

	for _, msg := range lt.act("lsp_toggle_for_buffer") {
		lt.feed(msg)
	}
	lt.touch()
	if len(port.closes) != 1 || port.closes[0] != "/p/a.go" {
		t.Fatalf("closes = %v, want the buffer closed", port.closes)
	}
	if len(port.opens) != 1 {
		t.Fatalf("opens = %v, want no reopen while LSP is off for the buffer", port.opens)
	}
	lt.act("lsp_toggle_for_buffer")
	lt.touch()
	if len(port.opens) != 2 {
		t.Fatalf("opens = %v, want the buffer reopened when toggled on", port.opens)
	}
}

func TestShowLSPStatusNamesTheDocumentState(t *testing.T) {
	port := newFakeLSP("go")
	lt := newLSPTest(t, port, &lspTab{path: "/p/a.go", lines: []string{"package a"}})
	lt.open()
	lt.act("show_lsp_status")
	if !strings.Contains(lt.m.notice, "go") || !strings.Contains(lt.m.notice, "open") {
		t.Fatalf("notice = %q, want the language and the open state", lt.m.notice)
	}
}

func TestToggleInlayHintsIsOnlyAFlag(t *testing.T) {
	port := newFakeLSP("go")
	lt := newLSPTest(t, port, &lspTab{path: "/p/a.go", lines: []string{"package a"}})
	lt.open()
	lt.act("toggle_inlay_hints")
	if !lt.m.lsp.inlay {
		t.Fatal("inlay flag is off after the toggle")
	}
	if len(port.opens) != 1 || port.reqErr != nil {
		t.Fatalf("toggle changed the server traffic: opens = %v", port.opens)
	}
	lt.act("toggle_inlay_hints")
	if lt.m.lsp.inlay {
		t.Fatal("inlay flag is still on after the second toggle")
	}
}

func TestLSPRenameDoneForAnotherPromptIsNotTaken(t *testing.T) {
	port := newFakeLSP("go")
	lt := newLSPTest(t, port, &lspTab{path: "/p/a.go", lines: []string{"package a"}})
	lt.open()
	lt.feed(prompt.Done{Tag: "other", Outcome: prompt.Submitted, Text: "x"})
	if port.renameName != "" {
		t.Fatalf("a prompt for another feature started a rename: %q", port.renameName)
	}
}

func TestLSPOpensTheFileThatReplacesTheActiveTab(t *testing.T) {
	// A preview tab is replaced by the next file at the same index, so the tab count
	// and the active index do not change. The new document must still open, and the
	// one it replaced must close.
	port := newFakeLSP("go")
	tab := &lspTab{path: "/p/a.go", lines: []string{"package a"}}
	lt := newLSPTest(t, port, tab)
	lt.open()

	tab.path, tab.lines = "/p/b.go", []string{"package b"}
	lt.open()
	if len(port.opens) != 2 || port.opens[1] != "/p/b.go" {
		t.Fatalf("opens = %v, want the replacing file /p/b.go opened", port.opens)
	}
	if len(port.closes) != 1 || port.closes[0] != "/p/a.go" {
		t.Fatalf("closes = %v, want the replaced file /p/a.go closed", port.closes)
	}
}

func TestLSPCompletionEnterInsertsTheHighlightedItem(t *testing.T) {
	port := newFakeLSP("go")
	port.items = []CompletionItem{{Label: "alpha", Insert: "alpha"}, {Label: "beta", Insert: "beta"}}
	tab := &lspTab{path: "/p/a.go", lines: []string{"pa"}, cursor: buffer.Pos{Line: 0, Col: 2}}
	lt := newLSPTest(t, port, tab)
	lt.open()
	for _, msg := range lt.act("lsp_completion") {
		lt.feed(msg)
	}
	lt.popup().Update(tea.KeyPressMsg{Code: tea.KeyDown})
	for _, msg := range deliver(lt.popup().Update(tea.KeyPressMsg{Code: tea.KeyEnter})) {
		lt.feed(msg)
	}
	want := editor.LSPEdit{From: buffer.Pos{Line: 0, Col: 2}, To: buffer.Pos{Line: 0, Col: 2}, Text: "beta"}
	if got := lt.ed.applied["/p/a.go"]; len(got) != 1 || got[0] != want {
		t.Fatalf("applied = %+v, want [%+v]", got, want)
	}
}

// openCompletion asks for the completions of the active tab and delivers the reply,
// so the completion popup is open on top of the stack.
func openCompletion(lt *lspTest) {
	lt.t.Helper()
	for _, msg := range lt.act("lsp_completion") {
		lt.feed(msg)
	}
	lt.popup()
}

// frameCell returns the cell of the first frame row that contains s.
func frameCell(m *Model, s string) (x, y int, ok bool) {
	for y, row := range frameText(m) {
		if x := strings.Index(row, s); x >= 0 {
			return x, y, true
		}
	}
	return 0, 0, false
}

// TestLSPCompletionChosenThroughTheRootInsertsOnce sends the choice through the
// root, as a key or a click does. The popup closes on the choice, and the item must
// still be inserted, once.
func TestLSPCompletionChosenThroughTheRootInsertsOnce(t *testing.T) {
	tests := []struct {
		name   string
		choose func(lt *lspTest)
	}{
		{"enter", func(lt *lspTest) {
			lt.feed(tea.KeyPressMsg{Code: tea.KeyDown})
			for _, msg := range lt.feed(tea.KeyPressMsg{Code: tea.KeyEnter}) {
				lt.feed(msg)
			}
		}},
		{"click", func(lt *lspTest) {
			x, y, ok := frameCell(lt.m, "beta")
			if !ok {
				lt.t.Fatal("the completion row beta is not on the frame")
			}
			for _, msg := range lt.feed(tea.MouseClickMsg(tea.Mouse{X: x, Y: y, Button: tea.MouseLeft})) {
				lt.feed(msg)
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			port := newFakeLSP("go")
			port.items = []CompletionItem{{Label: "alpha", Insert: "alpha"}, {Label: "beta", Insert: "beta"}}
			tab := &lspTab{path: "/p/a.go", lines: []string{"pa"}, cursor: buffer.Pos{Line: 0, Col: 2}}
			lt := newLSPTest(t, port, tab)
			lt.open()
			openCompletion(lt)

			tt.choose(lt)
			want := editor.LSPEdit{From: buffer.Pos{Line: 0, Col: 2}, To: buffer.Pos{Line: 0, Col: 2}, Text: "beta"}
			if got := lt.ed.applied["/p/a.go"]; len(got) != 1 || got[0] != want {
				t.Fatalf("applied = %+v, want [%+v] once", got, want)
			}
			if len(lt.m.overlays) != 0 {
				t.Fatalf("overlays = %d after the choice, want the popup closed", len(lt.m.overlays))
			}
		})
	}
}

// TestLSPCompletionTypedTextReachesTheDocument checks that a printable key typed
// while the list is open is inserted, and that the list closes, since it was asked
// at an older caret.
func TestLSPCompletionTypedTextReachesTheDocument(t *testing.T) {
	port := newFakeLSP("go")
	port.items = []CompletionItem{{Label: "alpha", Insert: "alpha"}}
	tab := &lspTab{path: "/p/a.go", lines: []string{"pa"}, cursor: buffer.Pos{Line: 0, Col: 2}}
	lt := newLSPTest(t, port, tab)
	lt.open()
	openCompletion(lt)

	lt.feed(tea.KeyPressMsg{Code: 'x', Text: "x"})
	if got := tab.lines[0]; got != "pax" {
		t.Fatalf("line = %q after typing x, want pax", got)
	}
	if len(lt.m.overlays) != 0 || lt.m.lsp.comp != nil {
		t.Fatalf("overlays = %d, comp = %v after typing, want the list closed", len(lt.m.overlays), lt.m.lsp.comp)
	}
}

// TestLSPCompletionCaretKeysCloseTheListAndReachTheEditor checks that Backspace and
// the caret keys, which the list does not take, go to the editor, and that the list
// closes for them.
func TestLSPCompletionCaretKeysCloseTheListAndReachTheEditor(t *testing.T) {
	tests := []struct {
		name   string
		key    tea.KeyPressMsg
		action string
	}{
		{"backspace", tea.KeyPressMsg{Code: tea.KeyBackspace}, "delete_backward"},
		{"left", tea.KeyPressMsg{Code: tea.KeyLeft}, "move_left"},
		{"right", tea.KeyPressMsg{Code: tea.KeyRight}, "move_right"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			port := newFakeLSP("go")
			port.items = []CompletionItem{{Label: "alpha", Insert: "alpha"}}
			lt := newLSPTest(t, port, &lspTab{path: "/p/a.go", lines: []string{"pa"}, cursor: buffer.Pos{Line: 0, Col: 2}})
			lt.open()
			openCompletion(lt)

			lt.feed(tt.key)
			if len(lt.m.overlays) != 0 || lt.m.lsp.comp != nil {
				t.Fatalf("overlays = %d after %s, want the list closed", len(lt.m.overlays), tt.name)
			}
			if len(lt.ed.actions) == 0 || lt.ed.actions[len(lt.ed.actions)-1] != tt.action {
				t.Fatalf("editor actions = %v, want %s to reach the editor", lt.ed.actions, tt.action)
			}
		})
	}
}

// TestLSPCompletionEscForgetsTheList checks that Esc, which pops the popup, also
// clears the completion state, so a later accept cannot insert the old list.
func TestLSPCompletionEscForgetsTheList(t *testing.T) {
	port := newFakeLSP("go")
	port.items = []CompletionItem{{Label: "alpha", Insert: "alpha"}}
	lt := newLSPTest(t, port, &lspTab{path: "/p/a.go", lines: []string{"pa"}, cursor: buffer.Pos{Line: 0, Col: 2}})
	lt.open()
	openCompletion(lt)

	lt.feed(tea.KeyPressMsg{Code: tea.KeyEscape})
	if lt.m.lsp.comp != nil {
		t.Fatal("the completion state survived Esc")
	}
	lt.act("completion_accept")
	if len(lt.ed.applied) != 0 {
		t.Fatalf("applied = %v after Esc and accept, want none", lt.ed.applied)
	}
}
