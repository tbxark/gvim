package buffer

import "testing"

// TestNonASCIITracksEveryEdit checks the count of non-ASCII runes after loads, typed
// and deleted text, and undo and redo, which all go through the same raw edits.
func TestNonASCIITracksEveryEdit(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		text string
		edit func(b *Buffer)
		want bool
	}{
		{"ascii load", "hello\nworld", func(*Buffer) {}, false},
		{"non-ascii load", "héllo", func(*Buffer) {}, true},
		{"typed ascii", "abc", func(b *Buffer) { b.Insert(Pos{0, 3}, "d") }, false},
		{"typed cjk", "abc", func(b *Buffer) { b.Insert(Pos{0, 3}, "中") }, true},
		{"typed cjk then deleted", "abc", func(b *Buffer) {
			end := b.Insert(Pos{0, 3}, "中")
			b.Delete(Pos{0, 3}, end)
		}, false},
		{"multi-line cjk typed then deleted", "abc", func(b *Buffer) {
			end := b.Insert(Pos{0, 1}, "x\n中\ny")
			b.Delete(Pos{0, 1}, end)
		}, false},
		{"deleted the only accent", "café", func(b *Buffer) { b.Delete(Pos{0, 3}, Pos{0, 4}) }, false},
		{"deleted across lines the only cjk", "a中\n中b", func(b *Buffer) {
			b.Delete(Pos{0, 1}, Pos{1, 1})
		}, false},
		{"undo of typed cjk", "abc", func(b *Buffer) {
			b.Insert(Pos{0, 3}, "中")
			b.Undo()
		}, false},
		{"redo of typed cjk", "abc", func(b *Buffer) {
			b.Insert(Pos{0, 3}, "中")
			b.Undo()
			b.Redo()
		}, true},
		{"cjk in the middle of a line typed then ascii", "ab", func(b *Buffer) {
			b.Insert(Pos{0, 1}, "中")
			b.Insert(Pos{0, 0}, "z")
		}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			b := New(tt.text)
			tt.edit(b)
			if got := b.NonASCII(); got != tt.want {
				t.Errorf("NonASCII() = %v, want %v for text %q", got, tt.want, b.Text())
			}
		})
	}
}
