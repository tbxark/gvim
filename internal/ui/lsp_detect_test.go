package ui

import (
	"errors"
	"image/color"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/tbxark/gvim/internal/buffer"
	"github.com/tbxark/gvim/internal/editor"
	"github.com/tbxark/gvim/internal/theme"
	"github.com/tbxark/gvim/internal/ui/popup"
)

// detectLSP is a fakeLSP that fails to open the languages of openErrs, describes its
// servers and formats.
type detectLSP struct {
	*fakeLSP
	openErrs map[string]error
	openLang []string // "<lang>:<path>" of every Open
	info     map[string]LSPServerInfo

	fmtEdits []editor.LSPEdit
	fmtErr   error
	fmts     []string // the text of every Format
}

func newDetectLSP(langs ...string) *detectLSP {
	return &detectLSP{fakeLSP: newFakeLSP(langs...), openErrs: map[string]error{}, info: map[string]LSPServerInfo{}}
}

func (f *detectLSP) Open(path, lang, text string) error {
	f.openLang = append(f.openLang, lang+":"+path)
	if err := f.openErrs[lang]; err != nil {
		return err
	}
	return f.fakeLSP.Open(path, lang, text)
}

func (f *detectLSP) Server(lang string) LSPServerInfo { return f.info[lang] }

func (f *detectLSP) Format(_, text string, _ int, _ bool) ([]editor.LSPEdit, error) {
	f.fmts = append(f.fmts, text)
	return f.fmtEdits, f.fmtErr
}

// newDetectTest builds a root model over lspEd tabs and the port, like newLSPTest.
func newDetectTest(t *testing.T, port *detectLSP, tabs ...*lspTab) *lspTest {
	t.Helper()
	lspDebounce, lspPollEvery = time.Millisecond, time.Millisecond
	t.Cleanup(func() { lspDebounce, lspPollEvery = 300*time.Millisecond, 300*time.Millisecond })
	ed := &lspEd{tabs: tabs}
	keys := fakeKeymap{"normal|F12|": {Action: "lsp_goto_definition"}}
	m := New(Deps{Editor: ed, Keys: keys, Theme: theme.Theme{}, Clock: testClock, LSP: port})
	lt := &lspTest{t: t, m: m, ed: ed, port: port.fakeLSP}
	lt.settle(lt.touchCmd(), 3)
	return lt
}

var errGoplsMissing = &LSPUnavailableError{
	Language: "go", Command: "gopls", Install: "go install golang.org/x/tools/gopls@latest", Missing: true,
	Err: errors.New("gopls is not installed (the language server for go): go install golang.org/x/tools/gopls@latest"),
}

func TestMissingServerIsReportedOnceAndNotStartedAgain(t *testing.T) {
	port := newDetectLSP("go")
	port.openErrs["go"] = errGoplsMissing
	a := &lspTab{path: "/p/a.go", lines: []string{"package a"}}
	b := &lspTab{path: "/p/b.go", lines: []string{"package b"}}
	lt := newDetectTest(t, port, a, b)

	if lt.m.notice != errGoplsMissing.Error() {
		t.Fatalf("notice = %q, want the message with the binary and the install command", lt.m.notice)
	}
	tried := len(port.openLang)
	// Edits, new syncs and a new tab of the same language do not start the program again.
	lt.m.notice = ""
	a.lines = []string{"package a // edited"}
	for range 3 {
		lt.settle(lt.touchCmd(), 3)
	}
	lt.ed.tabs = append(lt.ed.tabs, &lspTab{path: "/p/c.go", lines: []string{"package c"}})
	lt.settle(lt.touchCmd(), 3)
	if len(port.openLang) != tried {
		t.Errorf("opens = %v after the failure, want no more than the %d first ones", port.openLang, tried)
	}
	if lt.m.notice != "" {
		t.Errorf("notice = %q on later updates, want the message shown once", lt.m.notice)
	}
	// The status row keeps no slot for a counter that no server will fill.
	if lt.m.statusView().warnSlot {
		t.Error("the status row reserves the warning counter for a language with no server")
	}
}

func TestMissingServerNoticeIsShownOncePerLanguage(t *testing.T) {
	port := newDetectLSP("go", "rust")
	port.openErrs["go"] = errGoplsMissing
	rustMissing := &LSPUnavailableError{Language: "rust", Command: "rust-analyzer", Missing: true, Err: errors.New("rust-analyzer is not installed")}
	port.openErrs["rust"] = rustMissing
	lt := newDetectTest(t, port, &lspTab{path: "/p/a.go", lines: []string{"package a"}})
	if lt.m.notice != errGoplsMissing.Error() {
		t.Fatalf("notice = %q, want go's message", lt.m.notice)
	}
	// A document of another language has its own message.
	lt.ed.tabs = append(lt.ed.tabs, &lspTab{path: "/p/a.rs", lines: []string{"fn main() {}"}})
	lt.ed.active = 1
	lt.settle(lt.touchCmd(), 3)
	if lt.m.notice != rustMissing.Error() {
		t.Errorf("notice = %q, want rust's message", lt.m.notice)
	}
}

func TestLSPActionsRepeatTheReasonWhenAskedFor(t *testing.T) {
	port := newDetectLSP("go")
	port.openErrs["go"] = errGoplsMissing
	lt := newDetectTest(t, port, &lspTab{path: "/p/a.go", lines: []string{"package a"}})
	lt.m.notice = ""
	for _, action := range []string{"lsp_goto_definition", "lsp_hover", "lsp_rename"} {
		lt.m.notice = ""
		lt.act(action)
		if lt.m.notice != errGoplsMissing.Error() {
			t.Errorf("%s: notice = %q, want the reason with the install command", action, lt.m.notice)
		}
	}
	lt.act("show_lsp_status")
	if !strings.HasPrefix(lt.m.notice, "LSP: ") || !strings.Contains(lt.m.notice, "go install golang.org/x/tools/gopls@latest") {
		t.Errorf("status = %q, want the reason", lt.m.notice)
	}
}

func TestLSPRestartTriesAMissingServerAgain(t *testing.T) {
	port := newDetectLSP("go")
	port.openErrs["go"] = errGoplsMissing
	lt := newDetectTest(t, port, &lspTab{path: "/p/a.go", lines: []string{"package a"}})
	tried := len(port.openLang)
	// The user installs gopls and restarts.
	delete(port.openErrs, "go")
	for _, msg := range lt.act("lsp_restart") {
		lt.feed(msg)
	}
	lt.settle(lt.touchCmd(), 3)
	if len(port.openLang) <= tried || len(port.opens) != 1 {
		t.Fatalf("opens %v, successful opens %v; want the document opened after the restart", port.openLang, port.opens)
	}
	if lt.m.lsp.unavailable["go"] != nil {
		t.Error("the language is still marked unavailable")
	}
}

func TestOtherOpenFailuresAreNoticedAndRetried(t *testing.T) {
	port := newDetectLSP("go")
	port.openErrs["go"] = errors.New("lsp: write failed")
	lt := newDetectTest(t, port, &lspTab{path: "/p/a.go", lines: []string{"package a"}})
	if lt.m.notice != "lsp: write failed" {
		t.Fatalf("notice = %q, want the error", lt.m.notice)
	}
	if lt.m.lsp.unavailable["go"] != nil {
		t.Error("a plain failure marked the language unavailable")
	}
}

func TestNoServerNoticesFollowTheConfig(t *testing.T) {
	tests := []struct {
		name string
		info LSPServerInfo
		want []string
	}{
		{"no server", LSPServerInfo{}, []string{"No language server for go", "lsp.go.command"}},
		{"turned off for all", LSPServerInfo{State: LSPServerOffAll}, []string{"lsp_enabled is false"}},
		{"turned off for the language", LSPServerInfo{State: LSPServerOffLanguage, Language: "go"}, []string{"turned off", "lsp.go"}},
		{"manual", LSPServerInfo{State: LSPServerManual, Language: "odin", Command: "ols", Install: "build it"}, []string{"ols does not start on its own", "Start/Restart LSP Server", "lsp.odin.auto_start", "build it"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			port := newDetectLSP() // supports no language, so the actions have no server
			port.info["go"] = tt.info
			lt := newDetectTest(t, port, &lspTab{path: "/p/a.go", lines: []string{"package a"}})
			lt.act("lsp_hover")
			for _, want := range tt.want {
				if !strings.Contains(lt.m.notice, want) {
					t.Errorf("notice = %q, want it to contain %q", lt.m.notice, want)
				}
			}
		})
	}
}

func TestFormatBufferFallsBackToTheLanguageServer(t *testing.T) {
	port := newDetectLSP("go")
	port.fmtEdits = []editor.LSPEdit{{From: buffer.Pos{Line: 0, Col: 0}, To: buffer.Pos{Line: 0, Col: 7}, Text: "package"}}
	tab := &lspTab{path: "/p/a.go", lines: []string{"package a", "func  f() {}"}}
	lt := newDetectTest(t, port, tab)
	lt.ed.formatter = false

	msgs := lt.act("format_buffer")
	if len(msgs) != 1 {
		t.Fatalf("format_buffer produced %d messages, want the server's answer", len(msgs))
	}
	if want := "package a\nfunc  f() {}"; !slices.Equal(port.fmts, []string{want}) {
		t.Fatalf("the server was asked to format %q, want %q", port.fmts, want)
	}
	lt.feed(msgs[0])
	if got := lt.ed.applied["/p/a.go"]; !slices.Equal(got, port.fmtEdits) {
		t.Errorf("applied edits = %+v, want %+v", got, port.fmtEdits)
	}
	if lt.m.notice != "Formatted with the language server" {
		t.Errorf("notice = %q", lt.m.notice)
	}
	if slices.Contains(lt.ed.actions, "format_buffer") {
		t.Error("the editor's own format_buffer ran too")
	}
}

func TestFormatBufferKeepsTheConfiguredFormatterFirst(t *testing.T) {
	port := newDetectLSP("go")
	lt := newDetectTest(t, port, &lspTab{path: "/p/a.go", lines: []string{"package a"}})
	lt.ed.formatter = true
	lt.act("format_buffer")
	if len(port.fmts) != 0 {
		t.Errorf("the server formatted although the editor has a formatter: %q", port.fmts)
	}
	if !slices.Contains(lt.ed.actions, "format_buffer") {
		t.Errorf("the editor did not get format_buffer: %v", lt.ed.actions)
	}
}

func TestFormatBufferSaysWhyNothingCanFormat(t *testing.T) {
	tests := []struct {
		name  string
		setup func(*detectLSP, *lspTab)
		want  []string
	}{
		{"the program is missing", func(p *detectLSP, _ *lspTab) { p.openErrs["go"] = errGoplsMissing },
			[]string{"Format Buffer", "no formatter is set for go", "formatters.go", "gopls is not installed"}},
		{"no server for the language", func(p *detectLSP, tab *lspTab) { tab.path = "/p/a.txt" },
			[]string{"Format Buffer", "no formatter is set for text", "no language server for text"}},
		{"an unnamed document", func(p *detectLSP, tab *lspTab) { tab.path = "" },
			[]string{"Format Buffer", "save the file first"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			port := newDetectLSP("go")
			tab := &lspTab{path: "/p/a.go", lines: []string{"package a"}}
			tt.setup(port, tab)
			lt := newDetectTest(t, port, tab)
			lt.ed.formatter = false
			lt.m.notice = ""
			if msgs := lt.act("format_buffer"); len(msgs) != 0 {
				t.Fatalf("format_buffer started %d commands, want a notice only", len(msgs))
			}
			for _, want := range tt.want {
				if !strings.Contains(lt.m.notice, want) {
					t.Errorf("notice = %q, want it to contain %q", lt.m.notice, want)
				}
			}
			if len(port.fmts) != 0 {
				t.Error("the server was asked to format")
			}
		})
	}
}

func TestFormatBufferDropsEditsForTextThatChanged(t *testing.T) {
	port := newDetectLSP("go")
	port.fmtEdits = []editor.LSPEdit{{To: buffer.Pos{Col: 1}, Text: "X"}}
	tab := &lspTab{path: "/p/a.go", lines: []string{"package a"}}
	lt := newDetectTest(t, port, tab)
	lt.ed.formatter = false
	msgs := lt.act("format_buffer")
	tab.lines = []string{"package a // typed meanwhile"}
	lt.feed(msgs[0])
	if len(lt.ed.applied) != 0 {
		t.Errorf("edits were applied to text they do not fit: %+v", lt.ed.applied)
	}
	if !strings.Contains(lt.m.notice, "changed while") {
		t.Errorf("notice = %q, want it to say the text changed", lt.m.notice)
	}
}

func TestFormatBufferReportsServerErrorsAndNoChanges(t *testing.T) {
	port := newDetectLSP("go")
	lt := newDetectTest(t, port, &lspTab{path: "/p/a.go", lines: []string{"package a"}})
	lt.ed.formatter = false
	msgs := lt.act("format_buffer")
	lt.feed(msgs[0])
	if !strings.Contains(lt.m.notice, "nothing to change") {
		t.Errorf("notice = %q, want it to say the server changed nothing", lt.m.notice)
	}
	port.fmtErr = errors.New("server refused")
	msgs = lt.act("format_buffer")
	lt.feed(msgs[0])
	if lt.m.notice != "Format Buffer: server refused" {
		t.Errorf("notice = %q, want the server's error", lt.m.notice)
	}
}

func TestDiagnosticsMarkTheGutterAndHoverShowsThem(t *testing.T) {
	port := newDetectLSP("go")
	port.hover = "func f()"
	lt := newDetectTest(t, port, &lspTab{path: "/p/a.go", lines: []string{"abc", "def", "ghi"}})
	lt.m.panes.DiagError.FG = color.RGBA{R: 255, A: 255}
	lt.m.panes.DiagWarning.FG = color.RGBA{R: 255, G: 255, A: 255}
	lt.feed(lspDiagsMsg{events: []DiagnosticsEvent{{Path: "/p/a.go", Items: []Diagnostic{
		{Start: buffer.Pos{Line: 1, Col: 0}, End: buffer.Pos{Line: 1, Col: 2}, Severity: 1, Message: "undefined: x", Source: "gopls"},
		{Start: buffer.Pos{Line: 1, Col: 1}, End: buffer.Pos{Line: 1, Col: 3}, Severity: 2, Message: "odd"},
		{Start: buffer.Pos{Line: 2, Col: 1}, End: buffer.Pos{Line: 2, Col: 3}, Severity: 2, Message: "odd too"},
	}}}})

	doc := lt.m.ed.Active()
	pane := lt.m.lspGutter(lt.m.paneGeometry(lt.m.layout().focusedPane(), doc), doc)
	if mark, ok := pane.Gutter(0); ok {
		t.Errorf("line 0 has a gutter mark %+v, want none", mark)
	}
	mark, ok := pane.Gutter(1)
	if !ok || mark.Text != diagGutterGlyph || mark.Style.FG != (color.RGBA{R: 255, A: 255}) {
		t.Errorf("line 1 mark = %+v ok %v, want a dot in the error color (the worst severity there)", mark, ok)
	}
	if mark, ok = pane.Gutter(2); !ok || mark.Style.FG != (color.RGBA{R: 255, G: 255, A: 255}) {
		t.Errorf("line 2 mark = %+v ok %v, want a dot in the warning color", mark, ok)
	}

	// Hover on line 1 column 1 is under both diagnostics: they come before the symbol text.
	lt.ed.tabs[0].cursor = buffer.Pos{Line: 1, Col: 1}
	lt.m.lspHover("/p/a.go", buffer.Pos{Line: 1, Col: 1}, "func f()")
	var labels []string
	for _, it := range lt.popup().Items() {
		labels = append(labels, it.Label)
	}
	want := []string{"error: gopls: undefined: x", "warning: odd", "", "func f()"}
	if !slices.Equal(labels, want) {
		t.Errorf("hover rows = %q, want %q", labels, want)
	}
}

func TestHoverWithOnlyADiagnosticStillShowsIt(t *testing.T) {
	port := newDetectLSP("go")
	lt := newDetectTest(t, port, &lspTab{path: "/p/a.go", lines: []string{"abc"}})
	lt.feed(lspDiagsMsg{events: []DiagnosticsEvent{{Path: "/p/a.go", Items: []Diagnostic{
		{Start: buffer.Pos{}, End: buffer.Pos{Col: 2}, Severity: 1, Message: "undefined: utf8"},
	}}}})
	lt.m.lspHover("/p/a.go", buffer.Pos{Col: 1}, "")
	if lt.m.notice == "No hover information" {
		t.Fatal("hover says there is nothing although a diagnostic covers the caret")
	}
	if rows := lt.popup().Items(); len(rows) != 1 || rows[0].Label != "error: undefined: utf8" {
		t.Errorf("rows = %+v, want the diagnostic", rows)
	}
	lt.m.closePopup(lt.popup())
	// Away from the diagnostic and with no text there is nothing.
	lt.m.lspHover("/p/a.go", buffer.Pos{Col: 3}, "")
	if lt.m.notice != "No hover information" {
		t.Errorf("notice = %q, want the no-information notice", lt.m.notice)
	}
}

func TestJumpToErrorShowsTheMessage(t *testing.T) {
	port := newDetectLSP("go")
	lt := newDetectTest(t, port, &lspTab{path: "/p/a.go", lines: []string{"a", "b", "c"}})
	lt.feed(lspDiagsMsg{events: []DiagnosticsEvent{{Path: "/p/a.go", Items: []Diagnostic{
		{Start: buffer.Pos{Line: 1}, End: buffer.Pos{Line: 1, Col: 1}, Severity: 1, Message: "undefined: utf8\nsecond line", Source: "gopls"},
		{Start: buffer.Pos{Line: 2}, End: buffer.Pos{Line: 2, Col: 1}, Severity: 2, Message: "unused"},
	}}}})
	lt.act("jump_to_next_error")
	if want := "error: gopls: undefined: utf8 (1/2)"; lt.m.notice != want {
		t.Errorf("notice = %q, want %q", lt.m.notice, want)
	}
	lt.act("jump_to_next_error")
	if want := "warning: unused (2/2)"; lt.m.notice != want {
		t.Errorf("notice = %q, want %q", lt.m.notice, want)
	}
}

func TestStatusRowKeepsTheCounterSlotOnlyWhereAServerRuns(t *testing.T) {
	port := newDetectLSP("go")
	lt := newDetectTest(t, port,
		&lspTab{path: "/p/a.go", lines: []string{"package a"}},
		&lspTab{path: "/p/notes.txt", lines: []string{"text"}})
	if !lt.m.statusView().warnSlot {
		t.Error("no counter slot for a document open on a server")
	}
	lt.ed.active = 1
	lt.settle(lt.touchCmd(), 3)
	if lt.m.statusView().warnSlot {
		t.Error("a counter slot for a plain text document")
	}
}

func TestFormatterAvailableCountsTheLanguageServer(t *testing.T) {
	port := newDetectLSP("go")
	lt := newDetectTest(t, port,
		&lspTab{path: "/p/a.go", lines: []string{"package a"}},
		&lspTab{path: "/p/notes.txt", lines: []string{"text"}})
	lt.ed.formatter = false
	holds := func() bool { return lt.m.whenHolds([]string{"formatter_available"}) }
	if !holds() {
		t.Error("Format Buffer is unavailable for a document open on a server that formats")
	}
	lt.ed.active = 1
	lt.settle(lt.touchCmd(), 3)
	if holds() {
		t.Error("Format Buffer is available for a text file with no formatter and no server")
	}
}

// The port is where the PATH search happens (the program is looked up when it is launched),
// so the root must reach it only from a command, never from Update.
func TestPortIsReachedOnlyFromCommands(t *testing.T) {
	port := newDetectLSP("go")
	lspDebounce, lspPollEvery = time.Millisecond, time.Millisecond
	t.Cleanup(func() { lspDebounce, lspPollEvery = 300*time.Millisecond, 300*time.Millisecond })
	ed := &lspEd{tabs: []*lspTab{{path: "/p/a.go", lines: []string{"package a"}}}}
	m := New(Deps{Editor: ed, Keys: fakeKeymap{}, Theme: theme.Theme{}, Clock: testClock, LSP: port})
	_, cmd := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	if len(port.openLang) != 0 {
		t.Fatalf("Update called Open itself: %v", port.openLang)
	}
	if cmd == nil {
		t.Fatal("Update started no command to open the document")
	}
	deliver(cmd)
	if len(port.openLang) != 1 || port.openLang[0] != "go:/p/a.go" {
		t.Errorf("Open calls = %v, want one from the command", port.openLang)
	}
	// Asking whether the language has a server is configuration only: it opens nothing.
	port.openLang = nil
	m.lsp.enabled("go")
	_ = m.lspShowsWarnings()
	if len(port.openLang) != 0 {
		t.Errorf("a query opened a document: %v", port.openLang)
	}
}

var _ tea.Msg = lspFormatMsg{}

// TestRestartStartsAServerThatDoesNotStartOnItsOwn checks the rule of Fresh's docs that a manual
// start overrides lsp_enabled, the mute of a language and the manual servers: Start/Restart LSP
// Server runs the server of the active file's language, and the palette lists the command.
func TestRestartStartsAServerThatDoesNotStartOnItsOwn(t *testing.T) {
	for _, state := range []LSPServerState{LSPServerOffAll, LSPServerOffLanguage, LSPServerManual} {
		port := newDetectLSP() // no language starts on its own
		port.info["go"] = LSPServerInfo{State: state, Language: "go", Command: "gopls"}
		lt := newDetectTest(t, port, &lspTab{path: "/p/a.go", lines: []string{"package a"}})
		if len(port.opens) != 0 {
			t.Fatalf("state %v: the document opened without being asked", state)
		}
		if st := lt.m.paletteState(); !st.HasLSPConfig {
			t.Errorf("state %v: the palette disables Start/Restart LSP Server", state)
		}
		for _, msg := range lt.act("lsp_restart") {
			lt.feed(msg)
		}
		lt.settle(lt.touchCmd(), 3)
		if len(port.opens) != 1 || port.opens[0] != "/p/a.go" {
			t.Errorf("state %v: opens = %v, want the document opened on the server after the restart", state, port.opens)
		}
	}
}

func TestRestartWithNoServerForTheLanguageSaysSo(t *testing.T) {
	port := newDetectLSP() // the language has no entry at all
	lt := newDetectTest(t, port, &lspTab{path: "/p/a.txt", lines: []string{"hello"}})
	if st := lt.m.paletteState(); st.HasLSPConfig {
		t.Error("the palette enables Start/Restart LSP Server for a language with no server")
	}
	lt.m.notice = ""
	lt.act("lsp_restart")
	if !strings.Contains(lt.m.notice, "No language server for text") {
		t.Errorf("notice = %q, want the no-server notice", lt.m.notice)
	}
}

// TestHoverWrapsALongDiagnosticInsteadOfCuttingIt checks that a compiler message wider than the
// popup is shown in full over several rows, each within the width the popup can take.
func TestHoverWrapsALongDiagnosticInsteadOfCuttingIt(t *testing.T) {
	port := newDetectLSP("go")
	lt := newDetectTest(t, port, &lspTab{path: "/p/a.go", lines: []string{"var x int = \"hello\""}})
	msg := `cannot use "hello" (untyped string constant) as int value in variable declaration (and a longer tail so that the message needs three rows at least)`
	lt.feed(lspDiagsMsg{events: []DiagnosticsEvent{{Path: "/p/a.go", Items: []Diagnostic{
		{Start: buffer.Pos{Col: 12}, End: buffer.Pos{Col: 19}, Severity: 1, Message: msg, Source: "compiler"},
	}}}})
	lt.m.lspHover("/p/a.go", buffer.Pos{Col: 14}, "func f()")
	var rows []string
	for _, it := range lt.popup().Items() {
		rows = append(rows, it.Label)
	}
	if len(rows) < 4 {
		t.Fatalf("rows = %q, want the diagnostic over several rows, then a blank and the hover text", rows)
	}
	limit := popup.TextWidth(lt.m.width)
	for _, r := range rows {
		if w := ansi.StringWidth(r); w > limit {
			t.Errorf("row %q is %d cells wide, over the popup text width %d", r, w, limit)
		}
		if strings.Contains(r, "…") {
			t.Errorf("row %q is cut", r)
		}
	}
	diag := strings.Join(rows[:len(rows)-2], " ")
	if want := "error: compiler: " + msg; diag != want {
		t.Errorf("the wrapped rows say %q, want %q", diag, want)
	}
	if rows[len(rows)-1] != "func f()" {
		t.Errorf("last row = %q, want the hover text", rows[len(rows)-1])
	}
}

// TestStatusRowShowsTheServerState checks the LSP pill of the status row, as Fresh has one: it
// is there for a language that has a server, says whether the server runs, is switched off or is
// missing, and a click on it shows the status of the server.
func TestStatusRowShowsTheServerState(t *testing.T) {
	port := newDetectLSP("go")
	port.info["go"] = LSPServerInfo{State: LSPServerOn, Language: "go", Command: "gopls"}
	port.info["odin"] = LSPServerInfo{State: LSPServerManual, Language: "odin", Command: "ols"}
	lt := newDetectTest(t, port,
		&lspTab{path: "/p/a.go", lines: []string{"package a"}},
		&lspTab{path: "/p/b.odin", lines: []string{"package b"}},
		&lspTab{path: "/p/notes.txt", lines: []string{"text"}})
	pill := func(tab int) string {
		lt.ed.active = tab
		lt.settle(lt.touchCmd(), 3)
		return lt.m.statusView().lsp
	}
	if got := pill(0); got != "LSP" {
		t.Errorf("a document open on its server: pill = %q, want LSP", got)
	}
	if got := pill(1); got != "LSP (off)" {
		t.Errorf("a server that does not start on its own: pill = %q, want LSP (off)", got)
	}
	if got := pill(2); got != "" {
		t.Errorf("a language with no server: pill = %q, want none", got)
	}
	// A missing program.
	lt.m.lsp.unavailable = map[string]*LSPUnavailableError{"go": errGoplsMissing}
	if got := pill(0); got != "LSP (missing)" {
		t.Errorf("a missing program: pill = %q, want LSP (missing)", got)
	}
	lt.m.lsp.unavailable = map[string]*LSPUnavailableError{"go": {Language: "go", Command: "gopls", Err: errors.New("failed")}}
	if got := pill(0); got != "LSP (failed)" {
		t.Errorf("a server that failed: pill = %q, want LSP (failed)", got)
	}
	// The pill is a run of the row and a click on it shows the status.
	lt.m.lsp.unavailable = nil
	lt.m.lsp.stale = true
	if got := pill(0); got != "LSP" {
		t.Fatalf("after the failure is forgotten: pill = %q, want LSP", got)
	}
	var found bool
	for _, r := range statusRuns(100, lt.m.statusView()) {
		if r.kind == hitStatusSegment && r.index == statusSegmentLSP {
			found = strings.Contains(r.text, "LSP")
		}
	}
	if !found {
		t.Error("the pill is not a clickable run of the status row")
	}
	lt.m.statusSegmentClick(statusSegmentLSP)
	if !strings.HasPrefix(lt.m.notice, "LSP: ") {
		t.Errorf("notice after a click = %q, want the server status", lt.m.notice)
	}
}
