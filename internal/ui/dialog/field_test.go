package dialog

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/ui/grid"
)

func TestFieldScrollsTailIntoView(t *testing.T) {
	t.Parallel()
	f := newField("0123456789abcdefghij")
	g := grid.New(12, 1)
	cx, ok := f.draw(g, 0, 0, 12, stField)
	if !ok || cx >= 12 {
		t.Fatalf("caret %d ok=%v, want visible inside 12 cells", cx, ok)
	}
	line := g.Plain()[0]
	if line[len(line)-1:] != " " && line[:1] == "0" {
		t.Fatalf("field still shows the head: %q", line)
	}
}

func TestFieldCaretHiddenWithoutRoom(t *testing.T) {
	t.Parallel()
	f := newField("abc")
	g := grid.New(4, 1)
	if _, ok := f.draw(g, 0, 0, 0, stField); ok {
		t.Fatal("caret reported in a zero-width field")
	}
}

func TestFieldKeysEditValue(t *testing.T) {
	t.Parallel()
	f := newField("ab")
	f.key(key("c"))
	f.key(key("backspace"))
	f.key(key("x"))
	if got := f.value(); got != "abx" {
		t.Fatalf("value = %q, want abx", got)
	}
}

func TestRecentListSelectionAndWindow(t *testing.T) {
	t.Parallel()
	empty := newRecentList(nil)
	empty.move(1)
	if empty.selected() != "" {
		t.Fatal("empty list has a selection")
	}
	l := newRecentList([]string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j"})
	for range 20 {
		l.move(1)
	}
	if l.sel != 9 || l.top != 4 {
		t.Fatalf("sel=%d top=%d, want sel=9 top=4", l.sel, l.top)
	}
	for range 20 {
		l.move(-1)
	}
	if l.sel != 0 || l.top != 0 {
		t.Fatalf("sel=%d top=%d, want 0 and 0", l.sel, l.top)
	}
}

func TestFieldFirstTypedKeyReplacesUntouchedPrefill(t *testing.T) {
	t.Parallel()
	f := newField("/a/Untitled-1.txt")
	f.selectAll()
	f.key(key("n"))
	if got := f.value(); got != "n" {
		t.Fatalf("value = %q, want n", got)
	}
	f.key(key("x"))
	if got := f.value(); got != "nx" {
		t.Fatalf("value = %q, want nx", got)
	}
}

func TestFieldPasteReplacesUntouchedPrefill(t *testing.T) {
	t.Parallel()
	f := newField("old")
	f.selectAll()
	f.update(tea.PasteMsg{Content: "pasted"})
	if got := f.value(); got != "pasted" {
		t.Fatalf("value = %q, want pasted", got)
	}
}

func TestFieldCtrlKeyKeepsUntouchedPrefill(t *testing.T) {
	t.Parallel()
	f := newField("abc")
	f.selectAll()
	f.key(tea.KeyPressMsg{Code: 'e', Mod: tea.ModCtrl})
	f.key(key("z"))
	if got := f.value(); got != "abcz" {
		t.Fatalf("value = %q, want abcz", got)
	}
}
