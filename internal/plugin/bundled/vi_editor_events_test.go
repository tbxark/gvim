package bundled_test

import (
	"strings"
	"testing"

	"github.com/tbxark/gvim/internal/pluginapi"
)

// paste plays a paste the way the ui delivers it: the active mode drops it, hands it to its
// text function with paste = true, or lets the editor insert it.
func (v *vi) paste(text string) {
	v.t.Helper()
	info, ok := v.modeInfo()
	if !ok {
		v.t.Fatal("vi has no input mode")
	}
	switch info.Paste {
	case pluginapi.TextSwallow:
	case pluginapi.TextHandler:
		if err := v.h.Run(info.TextAction, map[string]any{"text": text, "paste": true}); err != nil {
			v.t.Fatal(err)
		}
		v.check()
	default:
		v.typeText(text)
	}
}

// selected plays a selection the user made with the mouse: the editor sets it and tells the plugins.
func (v *vi) selected(anchor, caret pluginapi.Pos) {
	v.t.Helper()
	if err := v.host.SetCursor(v.id, anchor, false, false, false); err != nil {
		v.t.Fatal(err)
	}
	if err := v.host.SetCursor(v.id, caret, true, false, false); err != nil {
		v.t.Fatal(err)
	}
	v.selectionChanged()
}

// clicked plays a click: the caret moves and the selection is gone.
func (v *vi) clicked(p pluginapi.Pos) {
	v.t.Helper()
	if err := v.host.SetCursor(v.id, p, false, false, false); err != nil {
		v.t.Fatal(err)
	}
	v.selectionChanged()
}

func (v *vi) selectionChanged() {
	v.t.Helper()
	v.h.Emit(pluginapi.Event{Name: "selection_changed", Data: map[string]any{"buf": v.id}})
	v.h.Settle()
	v.check()
}

const dragText = "alpha beta\ngamma delta\nepsilon\n"

// A selection made with the mouse (a drag, a double click) is the visual mode: the operators act on it.
func TestViAdoptsASelectionTheEditorMade(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		from   pluginapi.Pos
		to     pluginapi.Pos // the caret
		keys   string
		want   string
		cursor pluginapi.Pos
	}{
		{"drag forward, d", pluginapi.Pos{Line: 0, Col: 6}, pluginapi.Pos{Line: 0, Col: 10}, "d", "alpha \ngamma delta\nepsilon\n", pluginapi.Pos{Line: 0, Col: 5}},
		{"drag backward, d", pluginapi.Pos{Line: 0, Col: 10}, pluginapi.Pos{Line: 0, Col: 6}, "d", "alpha \ngamma delta\nepsilon\n", pluginapi.Pos{Line: 0, Col: 5}},
		{"double click on a word, d", pluginapi.Pos{Line: 0, Col: 0}, pluginapi.Pos{Line: 0, Col: 5}, "d", " beta\ngamma delta\nepsilon\n", pluginapi.Pos{Line: 0, Col: 0}},
		{"drag over lines, d", pluginapi.Pos{Line: 0, Col: 6}, pluginapi.Pos{Line: 1, Col: 5}, "d", "alpha  delta\nepsilon\n", pluginapi.Pos{Line: 0, Col: 6}},
		{"y then P", pluginapi.Pos{Line: 0, Col: 0}, pluginapi.Pos{Line: 0, Col: 5}, "yP", "alphaalpha beta\ngamma delta\nepsilon\n", pluginapi.Pos{Line: 0, Col: 4}},
		{"c types over it", pluginapi.Pos{Line: 0, Col: 0}, pluginapi.Pos{Line: 0, Col: 5}, "cX<Esc>", "X beta\ngamma delta\nepsilon\n", pluginapi.Pos{Line: 0, Col: 0}},
		{"extended with a motion", pluginapi.Pos{Line: 0, Col: 0}, pluginapi.Pos{Line: 0, Col: 5}, "ld", "beta\ngamma delta\nepsilon\n", pluginapi.Pos{Line: 0, Col: 0}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			v := newVi(t, dragText)
			v.selected(tt.from, tt.to)
			if got := v.mode(); got != "vi_visual" {
				t.Fatalf("mode = %q after the selection, want vi_visual (label %q)", got, v.label())
			}
			if got := v.label(); got != "-- VISUAL --" {
				t.Fatalf("label = %q", got)
			}
			v.keys(tt.keys)
			if got := v.text(); got != tt.want {
				t.Errorf("text = %q, want %q", got, tt.want)
			}
			if got := v.mode(); got != "vi_normal" {
				t.Errorf("mode = %q after the operator, want vi_normal", got)
			}
			if tt.cursor != (pluginapi.Pos{}) && v.cursor() != tt.cursor {
				t.Errorf("caret = %+v, want %+v", v.cursor(), tt.cursor)
			}
		})
	}
}

// vi's own visual selections come back as events and must not be taken for the user's; a click
// ends the visual mode; an empty selection is nothing.
func TestViSelectionEventsOfItsOwn(t *testing.T) {
	t.Parallel()
	v := newVi(t, dragText)
	v.keys("vl")
	v.selectionChanged()
	if got := v.mode(); got != "vi_visual" {
		t.Fatalf("mode = %q after its own selection event", got)
	}
	v.keys("j")
	v.selectionChanged()
	v.keys("d")
	if got := v.text(); got != "mma delta\nepsilon\n" {
		t.Fatalf("text = %q", got)
	}
	v.selectionChanged() // the selection is gone after the operator
	if got := v.mode(); got != "vi_normal" {
		t.Fatalf("mode = %q, want vi_normal", got)
	}

	// a click in the visual mode ends it, and the caret stays where it was clicked
	w := newVi(t, dragText)
	w.keys("vl")
	w.clicked(pluginapi.Pos{Line: 2, Col: 3})
	if got := w.mode(); got != "vi_normal" {
		t.Fatalf("mode = %q after a click, want vi_normal", got)
	}
	if got := w.cursor(); got != (pluginapi.Pos{Line: 2, Col: 3}) {
		t.Fatalf("caret = %+v, want the clicked place", got)
	}
	w.keys("x")
	if got := w.text(); got != "alpha beta\ngamma delta\nepslon\n" {
		t.Fatalf("text = %q", got)
	}

	// a click in the normal mode is not a selection
	u := newVi(t, dragText)
	u.clicked(pluginapi.Pos{Line: 1, Col: 2})
	if got := u.mode(); got != "vi_normal" {
		t.Fatalf("mode = %q after a click in the normal mode", got)
	}

	// the block mode shows its area with highlights: no selection event ends it
	b := newVi(t, dragText)
	b.keys("<C-v>l")
	b.selectionChanged()
	if got := b.mode(); got != "vi_visual" {
		t.Fatalf("mode = %q: a selection event ended the block mode", got)
	}
}

// A pending command, or the insert mode, is not interrupted by a selection.
func TestViSelectionDuringACommandOrInsert(t *testing.T) {
	t.Parallel()
	v := newVi(t, dragText)
	v.keys("d")
	v.selected(pluginapi.Pos{Line: 0, Col: 0}, pluginapi.Pos{Line: 0, Col: 5})
	if got := v.mode(); got != "vi_normal" {
		t.Fatalf("mode = %q with a command pending", got)
	}
	w := newVi(t, dragText)
	w.keys("i")
	w.selected(pluginapi.Pos{Line: 0, Col: 0}, pluginapi.Pos{Line: 0, Col: 5})
	if got := w.mode(); got != "vi_insert" {
		t.Fatalf("mode = %q during an insert", got)
	}
}

// Text pasted in the normal mode is put before the caret, as P does; in the visual mode it
// replaces the area; in the insert mode the editor inserts it. Nothing is discarded.
func TestViPaste(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		keys   string // to reach the mode
		at     [2]int
		paste  string
		want   string
		mode   string
		cursor pluginapi.Pos
	}{
		{"normal", "", [2]int{0, 6}, "XYZ", "alpha XYZbeta\ngamma delta\nepsilon\n", "vi_normal", pluginapi.Pos{Line: 0, Col: 8}},
		{"normal, several lines", "", [2]int{0, 6}, "dd\nxyz", "alpha dd\nxyzbeta\ngamma delta\nepsilon\n", "vi_normal", pluginapi.Pos{Line: 0, Col: 6}},
		{"normal, whole lines", "", [2]int{1, 3}, "new\n", "alpha beta\nnew\ngamma delta\nepsilon\n", "vi_normal", pluginapi.Pos{Line: 1, Col: 0}},
		{"normal, CRLF", "", [2]int{0, 6}, "a\r\nb", "alpha a\nbbeta\ngamma delta\nepsilon\n", "vi_normal", pluginapi.Pos{Line: 0, Col: 6}},
		{"visual replaces the area", "vl", [2]int{0, 0}, "XYZ", "XYZpha beta\ngamma delta\nepsilon\n", "vi_normal", pluginapi.Pos{Line: 0, Col: 2}},
		{"insert", "i", [2]int{0, 0}, "XYZ", "XYZalpha beta\ngamma delta\nepsilon\n", "vi_insert", pluginapi.Pos{Line: 0, Col: 3}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			v := newVi(t, dragText).at(tt.at[0], tt.at[1])
			v.keys(tt.keys)
			before := v.text()
			v.paste(tt.paste)
			if got := v.text(); got != tt.want {
				t.Errorf("text = %q, want %q (was %q)", got, tt.want, before)
			}
			if got := v.mode(); got != tt.mode {
				t.Errorf("mode = %q, want %q", got, tt.mode)
			}
			if got := v.cursor(); got != tt.cursor {
				t.Errorf("caret = %+v, want %+v", got, tt.cursor)
			}
			if tt.mode == "vi_normal" {
				// one undo step, and . does not repeat a paste
				v.keys("u")
				if got := v.text(); got != before {
					t.Errorf("after u the text is %q, want %q", got, before)
				}
			}
		})
	}
}

// A paste while a command waits for its keys is refused with a message, not run as keys.
func TestViPasteWhileACommandIsPending(t *testing.T) {
	t.Parallel()
	v := newVi(t, dragText)
	v.keys("d")
	v.paste("dd\nxyz")
	if got := v.text(); got != dragText {
		t.Fatalf("text = %q", got)
	}
	if got := v.notices(); !strings.Contains(got, "the paste was ignored") {
		t.Fatalf("notices = %q", got)
	}
}
