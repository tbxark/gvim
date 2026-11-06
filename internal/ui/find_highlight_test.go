package ui

import (
	"slices"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/buffer"
	"github.com/tbxark/gvim/internal/search"
	"github.com/tbxark/gvim/internal/ui/editorpane"
)

// openBarWith opens the find bar and types text into its search field.
func openBarWith(m *Model, text string) {
	press(m, ctrl('f'))
	for _, r := range text {
		press(m, typed(string(r)))
	}
}

func TestFindLineHighlights(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		setup func(m *Model)
		line  int
		want  []search.Highlight
	}{
		{
			name:  "the open bar marks the selected match and not the others",
			setup: func(m *Model) { openBarWith(m, "foo") },
			line:  0,
			want: []search.Highlight{
				{Line: 0, Start: 0, End: 3, Current: true},
				{Line: 0, Start: 8, End: 11},
			},
		},
		{
			name:  "a later line gets its own matches",
			setup: func(m *Model) { openBarWith(m, "foo") },
			line:  1,
			want:  []search.Highlight{{Line: 1, Start: 2, End: 5}},
		},
		{
			name: "no selection marks no match",
			setup: func(m *Model) {
				openBarWith(m, "foo")
				m.ed.SetCursor(buffer.Pos{Line: 1, Col: 0}, false)
			},
			line: 0,
			want: []search.Highlight{
				{Line: 0, Start: 0, End: 3},
				{Line: 0, Start: 8, End: 11},
			},
		},
		{
			name: "a closed bar shows no highlight",
			setup: func(m *Model) {
				openBarWith(m, "foo")
				press(m, special(tea.KeyEscape))
			},
			line: 0,
			want: nil,
		},
		{
			name:  "an empty search shows no highlight",
			setup: func(m *Model) { press(m, ctrl('f')) },
			line:  0,
			want:  nil,
		},
		{
			name:  "a search with no match shows no highlight",
			setup: func(m *Model) { openBarWith(m, "zzz") },
			line:  0,
			want:  nil,
		},
		{
			name: "an invalid regex shows no highlight",
			setup: func(m *Model) {
				press(m, ctrl('f'))
				press(m, tea.KeyPressMsg{Code: 'r', Mod: tea.ModAlt})
				press(m, typed("("))
			},
			line: 0,
			want: nil,
		},
		{
			name:  "a line past the end shows no highlight",
			setup: func(m *Model) { openBarWith(m, "foo") },
			line:  9,
			want:  nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m, _ := newDefaultSearchModel(t, "foo bar foo", "x foo")
			tt.setup(m)
			got := m.findLineHighlights()(tt.line)
			if !slices.Equal(got, tt.want) {
				t.Fatalf("highlights of line %d = %v, want %v", tt.line, got, tt.want)
			}
		})
	}
}

// paneRuns returns the highlight runs the editor pane paints on line. A pane
// without a highlight function paints none.
func paneRuns(p editorpane.Pane, line int) []editorpane.Highlight {
	if p.Highlights == nil {
		return nil
	}
	return p.Highlights(line)
}

func TestPaneHighlightsAreTheFindRuns(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		setup func(m *Model)
		line  int
		want  []editorpane.Highlight
	}{
		{
			name:  "an open bar gives the match runs of the line, the selected one marked",
			setup: func(m *Model) { openBarWith(m, "foo") },
			line:  0,
			want: []editorpane.Highlight{
				{Start: 0, End: 3, Current: true},
				{Start: 8, End: 11},
			},
		},
		{
			name:  "a later line gets its own runs",
			setup: func(m *Model) { openBarWith(m, "foo") },
			line:  1,
			want:  []editorpane.Highlight{{Start: 2, End: 5}},
		},
		{
			name: "a closed bar gives no runs",
			setup: func(m *Model) {
				openBarWith(m, "foo")
				press(m, special(tea.KeyEscape))
			},
			line: 0,
			want: nil,
		},
		{
			name:  "an empty search gives no runs",
			setup: func(m *Model) { press(m, ctrl('f')) },
			line:  0,
			want:  nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m, _ := newDefaultSearchModel(t, "foo bar foo", "x foo")
			tt.setup(m)
			got := paneRuns(m.pane(m.ed.Active()), tt.line)
			if !slices.Equal(got, tt.want) {
				t.Fatalf("pane runs of line %d = %v, want %v", tt.line, got, tt.want)
			}
		})
	}
}
