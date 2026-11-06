package editorpane

import "testing"

func TestGutterIsDrawnUnlessHidden(t *testing.T) {
	shown := rowsOf(Pane{Doc: newDoc("hello")}.Render(20, 2))
	if shown[0] != "  1 │ hello" {
		t.Fatalf("row 0 %q with the gutter shown, want the line number", shown[0])
	}
	hidden := rowsOf(Pane{Doc: newDoc("hello", "world"), HideGutter: true}.Render(20, 3))
	if hidden[0] != "hello" || hidden[1] != "world" {
		t.Fatalf("rows %q with the gutter hidden, want the text from column 0", hidden[:2])
	}
	if hidden[2] != "~" {
		t.Fatalf("row 2 %q past the end, want the tilde at column 0", hidden[2])
	}
}

func TestHiddenGutterHitsAndCaretUseColumnZero(t *testing.T) {
	p := Pane{Doc: newDoc("hello"), HideGutter: true}
	if got := p.Hit(20, 3, 0, 0); got.Kind != HitText || got.Pos.Col != 0 {
		t.Fatalf("Hit(0, 0) = %+v, want text at column 0", got)
	}
	if x, y, ok := p.Caret(20, 3); !ok || x != 0 || y != 0 {
		t.Fatalf("Caret = (%d, %d, %v), want (0, 0, true)", x, y, ok)
	}
}
