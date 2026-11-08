package tui

import (
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/editor"
)

// statusRun returns the status-row run of the segment kind and index.
func statusRun(t *testing.T, m *Model, index int) run {
	t.Helper()
	for _, r := range m.layout().status {
		if r.kind == hitStatusSegment && r.index == index {
			return r
		}
	}
	t.Fatalf("no status segment %d in %+v", index, m.layout().status)
	return run{}
}

func clickAt(m *Model, x, y int) {
	_, cmd := m.Update(tea.MouseClickMsg(tea.Mouse{X: x, Y: y, Button: tea.MouseLeft}))
	for _, msg := range runCmd(cmd) {
		m.Update(msg)
	}
}

func TestLanguageSegmentOpensAFilterableListOfRealLanguages(t *testing.T) {
	ed := newFakeEditor("abc")
	m := newTestModel(t, ed, newFakeTree(3), 100, 30)
	r := statusRun(t, m, statusSegmentLanguage)
	clickAt(m, r.x, m.layout().statusY)
	v := topList(m)
	if v == nil || v.title != "Select Language" {
		t.Fatalf("top overlay = %+v, want the language list", v)
	}
	lefts := shownLefts(v)
	for _, want := range []string{"Plain Text", "Go", "Python"} {
		if !slices.Contains(lefts, want) {
			t.Errorf("the list has no %q", want)
		}
	}
	// Text that names no language shows no row, and Enter chooses nothing.
	for _, c := range "zzzz" {
		m.Update(typed(string(c)))
	}
	if got := shownLefts(v); len(got) != 0 {
		t.Fatalf("filter zzzz shows %v", got)
	}
	_, cmd := m.Update(special(tea.KeyEnter))
	for _, msg := range runCmd(cmd) {
		m.Update(msg)
	}
	if len(ed.actions) != 0 || topList(m) == nil {
		t.Fatalf("Enter on an empty list ran %v, list open %v", ed.actions, topList(m) != nil)
	}
	// A real language is chosen under its own name.
	for range 4 {
		m.Update(special(tea.KeyBackspace))
	}
	for _, c := range "pyth" {
		m.Update(typed(string(c)))
	}
	_, cmd = m.Update(special(tea.KeyEnter))
	for _, msg := range runCmd(cmd) {
		m.Update(msg)
	}
	if !slices.Equal(ed.actions, []editor.Action{"set_language"}) || ed.args[0]["name"] != "Python" {
		t.Fatalf("editor ran %v %v, want set_language Python", ed.actions, ed.args)
	}
	if len(m.overlays) != 0 {
		t.Errorf("%d overlays left after the choice", len(m.overlays))
	}
}

func TestSetLanguageWithAName(t *testing.T) {
	tests := []struct {
		name     string
		wantName string // the name the editor gets; "" when it is not called
		notice   string
	}{
		{"zzzz", "", "Unknown language"},
		{"py", "Python", ""},
		{"go", "Go", ""},
		{"Python", "Python", ""},
		{"text", "", "Unknown language"},
		{"plaintext", "plaintext", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ed := newFakeEditor("abc")
			m := newTestModel(t, ed, newFakeTree(3), 100, 30)
			m.dispatch(Command{Action: "set_language", Args: map[string]any{"name": tt.name}})
			if tt.wantName == "" {
				if len(ed.actions) != 0 {
					t.Fatalf("the editor ran %v for %q", ed.actions, tt.name)
				}
			} else if len(ed.actions) != 1 || ed.args[0]["name"] != tt.wantName {
				t.Fatalf("editor ran %v %v, want name %q", ed.actions, ed.args, tt.wantName)
			}
			if !strings.Contains(m.notice, tt.notice) {
				t.Errorf("notice %q, want it to contain %q", m.notice, tt.notice)
			}
		})
	}
}

func TestLanguageLabelShowsPlainTextAsText(t *testing.T) {
	for in, want := range map[string]string{"plaintext": "Text", "Plaintext": "Text", "Go": "Go", "": ""} {
		if got := languageLabel(in); got != want {
			t.Errorf("languageLabel(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestLineEndingListFitsItsRows: the Select Line Ending box holds its two rows, not 28.
func TestLineEndingListFitsItsRows(t *testing.T) {
	ed := newFakeEditor("abc")
	m := newTestModel(t, ed, newFakeTree(3), 100, 40)
	m.dispatch(Command{Action: "set_line_ending"})
	g := m.paint()
	_ = g
	v := topList(m)
	if v == nil {
		t.Fatal("no list")
	}
	if want := len(v.rows) + 3; v.box.h != want {
		t.Errorf("box height %d, want %d (filter row, %d rows, two borders)", v.box.h, want, len(v.rows))
	}
	if v.box.h > 6 {
		t.Errorf("a two-row list is %d rows tall", v.box.h)
	}
}

func TestLongListsKeepTheirMaximumHeight(t *testing.T) {
	box, rows := listBoxFor(100, 60, 500)
	if box.h != listMaxHeight || rows != listMaxHeight-3 {
		t.Errorf("box %d rows %d, want %d and %d", box.h, rows, listMaxHeight, listMaxHeight-3)
	}
}

func TestPositionSegmentOpensGoToLine(t *testing.T) {
	ed := newFakeEditor("abc\ndef")
	m := newTestModel(t, ed, newFakeTree(3), 100, 30)
	r := statusRun(t, m, statusSegmentPosition)
	clickAt(m, r.x+1, m.layout().statusY)
	if len(m.overlays) != 1 {
		t.Fatalf("%d overlays after a click on Ln, Col, want the Go to Line prompt", len(m.overlays))
	}
	if _, ok := m.overlays[0].(lineOverlay); !ok {
		t.Fatalf("overlay is %T, want the line prompt", m.overlays[0])
	}
}
