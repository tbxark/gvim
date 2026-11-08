package tui

import (
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/internal/tui/list"
)

// shownLefts returns the left text of each row of v that passes the filter.
func shownLefts(v *list.View) []string {
	var out []string
	for _, r := range v.Visible() {
		out = append(out, r.Left)
	}
	return out
}

// topList returns the list view on top of the overlay stack, or nil.
func topList(m *Model) *list.View {
	if len(m.overlays) == 0 {
		return nil
	}
	if o, ok := m.overlays[len(m.overlays)-1].(*list.View); ok {
		return o
	}
	return nil
}

func TestEncodingActionsOpenAChooser(t *testing.T) {
	t.Parallel()
	tests := []struct {
		action    string
		title     string
		filter    string
		wantArgs  map[string]any
		wantInRow string
	}{
		{"set_encoding", "Select Encoding", "shift", map[string]any{"name": "shift_jis"}, "Shift-JIS"},
		{"reload_with_encoding", "Reload with Encoding", "gbk", map[string]any{"name": "gbk"}, ""},
		{"set_encoding", "Select Encoding", "1250", map[string]any{"name": "windows-1250"}, ""},
		{"set_line_ending", "Select Line Ending", "crlf", map[string]any{"value": "crlf"}, ""},
		{"set_line_ending", "Select Line Ending", "lf", map[string]any{"value": "lf"}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.action+"/"+tt.filter, func(t *testing.T) {
			t.Parallel()
			ed := newFakeEditor("abc")
			m := newTestModel(t, ed, newFakeTree(3), 100, 30)
			m.dispatch(Command{Action: tt.action})
			if len(ed.actions) != 0 {
				t.Fatalf("the action reached the editor without an argument: %v", ed.actions)
			}
			v := topList(m)
			if v == nil || v.Title != tt.title {
				t.Fatalf("top overlay = %+v, want the %q list", v, tt.title)
			}
			for _, r := range tt.filter {
				m.Update(typed(string(r)))
			}
			if tt.wantInRow != "" && !slices.ContainsFunc(shownLefts(v), func(s string) bool { return strings.Contains(s, tt.wantInRow) }) {
				t.Errorf("rows %q do not show %q", shownLefts(v), tt.wantInRow)
			}
			_, cmd := m.Update(special(tea.KeyEnter))
			for _, msg := range runCmd(cmd) {
				m.Update(msg)
			}
			if !slices.Equal(ed.actions, []editor.Action{editor.Action(tt.action)}) {
				t.Fatalf("editor actions = %v, want [%s]", ed.actions, tt.action)
			}
			if got := ed.args[0]; len(got) != len(tt.wantArgs) || got["name"] != tt.wantArgs["name"] || got["value"] != tt.wantArgs["value"] {
				t.Errorf("args = %v, want %v", got, tt.wantArgs)
			}
			if len(m.overlays) != 0 {
				t.Errorf("overlays left after the choice: %d", len(m.overlays))
			}
		})
	}
}

func TestPaletteAndMenuRunOpenTheChoosers(t *testing.T) {
	t.Parallel()
	for _, action := range []string{"set_encoding", "reload_with_encoding", "set_line_ending"} {
		ed := newFakeEditor("abc")
		m := newTestModel(t, ed, newFakeTree(3), 100, 30)
		if err := m.Run(action); err != nil {
			t.Fatalf("Run(%s): %v", action, err)
		}
		if topList(m) == nil || len(ed.actions) != 0 {
			t.Errorf("Run(%s): list open = %v, editor actions %v, want a chooser and no editor call", action, topList(m) != nil, ed.actions)
		}
	}
}

func TestEncodingActionsWithArgumentsGoStraightToTheEditor(t *testing.T) {
	t.Parallel()
	ed := newFakeEditor("abc")
	m := newTestModel(t, ed, newFakeTree(3), 100, 30)
	m.dispatch(Command{Action: "set_encoding", Args: map[string]any{"name": "gbk"}})
	if len(m.overlays) != 0 || !slices.Equal(ed.actions, []editor.Action{"set_encoding"}) {
		t.Fatalf("overlays %d, actions %v, want the editor to run it", len(m.overlays), ed.actions)
	}
}

func TestStatusBarSegmentsOpenTheirChoosers(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		index int
		title string
	}{
		{"line ending", statusSegmentEOL, "Select Line Ending"},
		{"encoding", statusSegmentEncoding, "Select Encoding"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ed := newFakeEditor("abc")
			m := newTestModel(t, ed, newFakeTree(3), 100, 30)
			l := m.layout()
			var click *run
			for i, r := range l.Status {
				if r.Kind == hitStatusSegment && r.Index == tt.index {
					click = &l.Status[i]
				}
			}
			if click == nil {
				t.Fatalf("no status segment %d in %+v", tt.index, l.Status)
			}
			m.Update(tea.MouseClickMsg{X: click.X, Y: l.StatusY, Button: tea.MouseLeft})
			if v := topList(m); v == nil || v.Title != tt.title {
				t.Fatalf("top overlay after the click = %+v, want the %q list", v, tt.title)
			}
		})
	}
}

// roDoc is a document that reports read-only.
type roDoc struct {
	*fakeDoc
	ro bool
}

func (d roDoc) ReadOnly() bool { return d.ro }

// roEditor serves a roDoc as its active document.
type roEditor struct {
	*fakeEditor
	doc roDoc
}

func (e roEditor) Active() Document { return e.doc }

func TestStatusBarShowsReadOnly(t *testing.T) {
	t.Parallel()
	for _, ro := range []bool{true, false} {
		ed := roEditor{fakeEditor: newFakeEditor("abc"), doc: roDoc{fakeDoc: &fakeDoc{lines: []string{"abc"}}, ro: ro}}
		m := newTestModel(t, ed.fakeEditor, newFakeTree(3), 100, 30)
		m.ed = ed
		l := m.layout()
		var texts []string
		for _, r := range l.Status {
			texts = append(texts, r.Text)
		}
		if got := slices.Contains(texts, " [RO] "); got != ro {
			t.Errorf("ReadOnly() = %v: status runs %q, [RO] shown = %v", ro, texts, got)
		}
	}
}
