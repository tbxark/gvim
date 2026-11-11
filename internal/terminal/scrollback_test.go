package terminal

import "testing"

func TestLineTextSkipsTheRightHalfOfAWideCharacter(t *testing.T) {
	l := Line{Cells: []Cell{{Rune: '中'}, {}, {Rune: '文'}, {}, {Rune: '!'}, {Rune: ' '}}}
	if got := l.Text(); got != "中文!" {
		t.Errorf("Text() = %q, want %q", got, "中文!")
	}
	if got := l.ColumnText(); got != "中 文 !" {
		t.Errorf("ColumnText() = %q, want one rune per column %q", got, "中 文 !")
	}
}
