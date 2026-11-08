package bundled_test

import (
	"strings"
	"testing"

	"github.com/tbxark/gvim/internal/pluginapi"
)

// selectWith sets the selection the way the editor does for a mouse drag and tells the plugin.
func (v *vi) selectWith(anchor, caret pluginapi.Pos) {
	v.t.Helper()
	if err := v.host.SetSelection(v.id, pluginapi.Range{From: anchor, To: caret}); err != nil {
		v.t.Fatal(err)
	}
	r := pluginapi.Range{From: anchor, To: caret}
	if r.From.Line > r.To.Line || (r.From.Line == r.To.Line && r.From.Col > r.To.Col) {
		r.From, r.To = r.To, r.From
	}
	v.h.Emit(pluginapi.Event{Name: "selection_changed", Data: map[string]any{"buf": v.id, "range": r}})
}

func (v *vi) clearSelection(at pluginapi.Pos) {
	v.t.Helper()
	v.setCursor(at)
	v.h.Emit(pluginapi.Event{Name: "selection_changed", Data: map[string]any{"buf": v.id}})
}

func pos(l, c int) pluginapi.Pos { return pluginapi.Pos{Line: l, Col: c} }

func TestViPasteRows(t *testing.T) {
	t.Parallel()
	const text = "alpha beta\ngamma\n"
	tests := []struct {
		name   string
		setup  string
		at     pluginapi.Pos
		paste  string
		want   string
		cursor pluginapi.Pos
		mode   string
	}{
		{"normal pastes before the caret", "", pos(0, 6), "XY", "alpha XYbeta\ngamma\n", pos(0, 7), "vi_normal"},
		// a paste of several lines leaves the caret at its start, as TestViPaste pins
		{"normal pastes lines", "", pos(0, 6), "dd\nxyz", "alpha dd\nxyzbeta\ngamma\n", pos(0, 6), "vi_normal"},
		{"normal paste does not run keys", "", pos(0, 0), "dd", "ddalpha beta\ngamma\n", pos(0, 1), "vi_normal"},
		{"insert pastes text", "i", pos(0, 6), "XY", "alpha XYbeta\ngamma\n", pos(0, 8), "vi_insert"},
		{"visual replaces the selection", "vee", pos(0, 0), "X", "X\ngamma\n", pos(0, 0), "vi_normal"},
		{"visual line replaces the lines", "V", pos(1, 0), "one\ntwo", "alpha beta\none\ntwo\n", pos(1, 0), "vi_normal"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			v := newVi(t, text).at(tt.at.Line, tt.at.Col)
			v.keys(tt.setup)
			v.paste(tt.paste)
			if got := v.text(); got != tt.want {
				t.Errorf("text = %q, want %q", got, tt.want)
			}
			if got := v.cursor(); got != tt.cursor {
				t.Errorf("caret = %+v, want %+v", got, tt.cursor)
			}
			if got := v.mode(); got != tt.mode {
				t.Errorf("mode = %q, want %q", got, tt.mode)
			}
		})
	}
}

func TestViMouseSelectionEntersVisualMode(t *testing.T) {
	t.Parallel()
	const text = "alpha beta\ngamma delta\n"
	tests := []struct {
		name          string
		anchor, caret pluginapi.Pos
		keys          string
		want          string
	}{
		{"drag right then d", pos(0, 6), pos(0, 10), "d", "alpha \ngamma delta\n"},
		{"drag left then d", pos(0, 10), pos(0, 6), "d", "alpha \ngamma delta\n"},
		{"drag over lines then d", pos(0, 6), pos(1, 5), "d", "alpha  delta\n"},
		{"drag then y then p", pos(0, 0), pos(0, 5), "y$p", "alpha betaalpha\ngamma delta\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			v := newVi(t, text)
			v.selectWith(tt.anchor, tt.caret)
			if got := v.mode(); got != "vi_visual" {
				t.Fatalf("mode = %q after the drag, want vi_visual", got)
			}
			if got := v.label(); !strings.Contains(got, "VISUAL") {
				t.Errorf("status = %q", got)
			}
			v.keys(tt.keys)
			if got := v.text(); got != tt.want {
				t.Errorf("text = %q, want %q", got, tt.want)
			}
			if got := v.mode(); got != "vi_normal" {
				t.Errorf("mode = %q after the command", got)
			}
		})
	}
}

// The selection vi itself makes in visual mode comes back as an event: it changes nothing.
func TestViOwnSelectionIsNotAMouseSelection(t *testing.T) {
	t.Parallel()
	v := newVi(t, "alpha beta\n")
	v.keys("vl")
	sel, _ := v.selection()
	v.h.Emit(pluginapi.Event{Name: "selection_changed", Data: map[string]any{"buf": v.id, "range": sel}})
	v.keys("d")
	if got := v.text(); got != "pha beta\n" {
		t.Errorf("text = %q, want the two characters vi selected deleted", got)
	}
	// on an empty text the selection of one character is empty: no range
	w := newVi(t, "")
	w.keys("v")
	w.h.Emit(pluginapi.Event{Name: "selection_changed", Data: map[string]any{"buf": w.id}})
	if got := w.mode(); got != "vi_visual" {
		t.Errorf("mode = %q, want vi_visual to stay", got)
	}
}

func TestViClickEndsVisualMode(t *testing.T) {
	t.Parallel()
	v := newVi(t, "alpha beta\ngamma delta\n")
	v.keys("vl")
	v.clearSelection(pos(1, 2))
	if got := v.mode(); got != "vi_normal" {
		t.Fatalf("mode = %q, want vi_normal after a click", got)
	}
	if got := v.cursor(); got != pos(1, 2) {
		t.Errorf("caret = %+v, want the click kept", got)
	}
	v.keys("x")
	if got := v.text(); got != "alpha beta\ngama delta\n" {
		t.Errorf("text = %q, want x to delete under the click", got)
	}
}

// A selection made while a command is pending does not replace the command: the pending command
// is kept and the mode stays normal (TestViSelectionDuringACommandOrInsert pins the same rule).
func TestViMouseSelectionCancelsAPendingCommand(t *testing.T) {
	t.Parallel()
	v := newVi(t, "alpha beta\n")
	v.keys("d")
	v.selectWith(pos(0, 0), pos(0, 5))
	if got := v.mode(); got != "vi_normal" {
		t.Fatalf("mode = %q, want the pending command kept in vi_normal", got)
	}
	v.keys("d")
	if got := v.text(); got != "" {
		t.Errorf("text = %q, want dd to delete the line: the selection did not take part", got)
	}
}
