package find

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/buffer"
)

func TestPasteEditsTheFocusedField(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		setup       func(b *Bar)
		paste       string
		wantSearch  string
		wantReplace string
	}{
		{name: "into the search field", paste: "foo", wantSearch: "foo"},
		{name: "line breaks become spaces", paste: "foo\r\nbar\nbaz\rqux", wantSearch: "foo bar baz qux"},
		{
			name:        "into the replacement field",
			setup:       func(b *Bar) { press(b, "tab") },
			paste:       "new",
			wantReplace: "new",
		},
		{
			name:        "a replacement paste leaves the search alone",
			setup:       func(b *Bar) { typeText(b, "ab"); press(b, "tab") },
			paste:       "x\ny",
			wantSearch:  "ab",
			wantReplace: "x y",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			b := Open(newFake("foo bar"), testStyles(), false)
			if tt.setup != nil {
				tt.setup(b)
			}
			_, cmd := b.Update(tea.PasteMsg{Content: tt.paste})
			if cmd != nil {
				t.Fatal("a paste started a command")
			}
			if got := b.search.Value(); got != tt.wantSearch {
				t.Errorf("search = %q, want %q", got, tt.wantSearch)
			}
			if got := b.repl.Value(); got != tt.wantReplace {
				t.Errorf("replace = %q, want %q", got, tt.wantReplace)
			}
		})
	}
}

func TestPasteIntoSearchSearchesAgain(t *testing.T) {
	t.Parallel()
	f := newFake("foo bar foo")
	f.cursor = buffer.Pos{Line: 0, Col: 4}
	b := Open(f, testStyles(), false)
	b.Update(tea.PasteMsg{Content: "foo"})
	// Typing and pasting search without moving the caret or the selection.
	if f.sel != nil {
		t.Fatalf("selection = %v, want it untouched", f.sel)
	}
	if got := b.statusText(); got != "Match 2 of 2" {
		t.Fatalf("status = %q, want %q", got, "Match 2 of 2")
	}
}

func TestPasteIsIgnoredInQueryReplace(t *testing.T) {
	t.Parallel()
	b := Open(newFake("foo foo"), testStyles(), false)
	typeText(b, "foo")
	b.StartQueryReplace()
	b.Update(tea.PasteMsg{Content: "zzz"})
	if got := b.search.Value(); got != "foo" {
		t.Fatalf("search = %q, want it unchanged", got)
	}
}

func TestCtrlVReturnsTheClipboardRead(t *testing.T) {
	t.Parallel()
	b := Open(newFake("foo"), testStyles(), false)
	_, cmd := b.Update(tea.KeyPressMsg{Code: 'v', Mod: tea.ModCtrl})
	if cmd == nil {
		t.Fatal("Ctrl+V dropped the clipboard read")
	}
	_, cmd = b.Update(key("a"))
	if cmd != nil {
		t.Fatal("typing started a command")
	}
}
