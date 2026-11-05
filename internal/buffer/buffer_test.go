package buffer

import (
	"testing"
)

func TestNewRoundTrip(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		text   string
		lines  int
		wantTx string
	}{
		{"empty", "", 1, ""},
		{"single", "héllo", 1, "héllo"},
		{"trailing newline", "a\n", 2, "a\n"},
		{"crlf kept", "a\r\nb\r\n", 3, "a\r\nb\r\n"},
		{"blank lines", "\n\n", 3, "\n\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			b := New(tt.text)
			if got := b.Lines(); got != tt.lines {
				t.Fatalf("Lines() = %d, want %d", got, tt.lines)
			}
			if got := b.Text(); got != tt.wantTx {
				t.Fatalf("Text() = %q, want %q", got, tt.wantTx)
			}
		})
	}
}

func TestInsertAndDelete(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		text    string
		insert  Pos
		s       string
		wantEnd Pos
		want    string
	}{
		{"start", "bc", Pos{0, 0}, "a", Pos{0, 1}, "abc"},
		{"middle", "ac", Pos{0, 1}, "b", Pos{0, 2}, "abc"},
		{"end of line", "ab", Pos{0, 2}, "c", Pos{0, 3}, "abc"},
		{"runes not bytes", "éa", Pos{0, 1}, "x", Pos{0, 2}, "éxa"},
		{"multi-line paste", "ab", Pos{0, 1}, "1\n2", Pos{1, 1}, "a1\n2b"},
		{"newline splits", "abc", Pos{0, 1}, "\n", Pos{1, 0}, "a\nbc"},
		{"clamped column", "ab", Pos{0, 99}, "c", Pos{0, 3}, "abc"},
		{"clamped line", "ab", Pos{9, 0}, "c", Pos{0, 1}, "cab"},
		{"empty insert", "ab", Pos{0, 1}, "", Pos{0, 1}, "ab"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			b := New(tt.text)
			if end := b.Insert(tt.insert, tt.s); end != tt.wantEnd {
				t.Fatalf("Insert end = %+v, want %+v", end, tt.wantEnd)
			}
			if got := b.Text(); got != tt.want {
				t.Fatalf("Text() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDelete(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		text     string
		from, to Pos
		removed  string
		want     string
	}{
		{"single line", "abcd", Pos{0, 1}, Pos{0, 3}, "bc", "ad"},
		{"reversed order", "abcd", Pos{0, 3}, Pos{0, 1}, "bc", "ad"},
		{"join lines", "ab\ncd", Pos{0, 1}, Pos{1, 1}, "b\nc", "ad"},
		{"whole text", "x\ny", Pos{0, 0}, Pos{1, 1}, "x\ny", ""},
		{"empty range", "abc", Pos{0, 1}, Pos{0, 1}, "", "abc"},
		{"clamped past end", "ab", Pos{0, 1}, Pos{0, 50}, "b", "a"},
		{"multibyte", "héllo", Pos{0, 1}, Pos{0, 2}, "é", "hllo"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			b := New(tt.text)
			if got := b.Delete(tt.from, tt.to); got != tt.removed {
				t.Fatalf("Delete returned %q, want %q", got, tt.removed)
			}
			if got := b.Text(); got != tt.want {
				t.Fatalf("Text() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRangeAndAccessors(t *testing.T) {
	t.Parallel()
	b := New("héllo\nwörld\n")
	if got := b.Range(Pos{0, 1}, Pos{1, 2}); got != "éllo\nwö" {
		t.Fatalf("Range = %q", got)
	}
	if got := b.Line(1); got != "wörld" {
		t.Fatalf("Line(1) = %q", got)
	}
	if got := b.LineLen(0); got != 5 {
		t.Fatalf("LineLen(0) = %d", got)
	}
	if r, ok := b.RuneAt(Pos{0, 1}); !ok || r != 'é' {
		t.Fatalf("RuneAt = %q, %v", r, ok)
	}
	if _, ok := b.RuneAt(Pos{0, 5}); ok {
		t.Fatal("RuneAt at line end should report false")
	}
	if got := b.End(); got != (Pos{2, 0}) {
		t.Fatalf("End = %+v", got)
	}
}

func TestPosAndSelection(t *testing.T) {
	t.Parallel()
	if !(Pos{0, 5}).Less(Pos{1, 0}) || (Pos{1, 0}).Less(Pos{0, 5}) {
		t.Fatal("Pos.Less ordering is wrong across lines")
	}
	s := Selection{Anchor: Pos{1, 2}, Head: Pos{0, 1}}
	from, to := s.Ordered()
	if from != (Pos{0, 1}) || to != (Pos{1, 2}) {
		t.Fatalf("Ordered = %+v %+v", from, to)
	}
}

func TestUndoRedoGroupsTyping(t *testing.T) {
	t.Parallel()
	clock := newStepClock()
	b := New("")
	b.SetClock(clock.now)
	at := Pos{}
	for _, r := range "hello world" {
		at = b.Insert(at, string(r))
	}
	if got := b.Text(); got != "hello world" {
		t.Fatalf("after typing: %q", got)
	}

	// Every keystroke sits in one bucket, so a single Undo reverts the run,
	// spaces included. Whitespace does not split groups.
	if p, ok := b.Undo(); !ok || p != (Pos{0, 0}) {
		t.Fatalf("undo typing run: pos=%+v ok=%v", p, ok)
	}
	if got := b.Text(); got != "" {
		t.Fatalf("after undo: %q", got)
	}
	if _, ok := b.Undo(); ok {
		t.Fatal("undo on empty history should report false")
	}

	if p, ok := b.Redo(); !ok || p != (Pos{0, 11}) {
		t.Fatalf("redo: pos=%+v ok=%v", p, ok)
	}
	if got := b.Text(); got != "hello world" {
		t.Fatalf("after redo: %q", got)
	}
	if _, ok := b.Redo(); ok {
		t.Fatal("redo with nothing undone should report false")
	}
}

func TestBreakSeparatesGroups(t *testing.T) {
	t.Parallel()
	clock := newStepClock()
	b := New("")
	b.SetClock(clock.now)
	b.Insert(Pos{}, "a")
	b.Break()
	b.Insert(Pos{0, 1}, "b")
	b.Undo()
	if got := b.Text(); got != "a" {
		t.Fatalf("Break should split the typing run, got %q", got)
	}
}

func TestBackspaceAndForwardDeleteMerge(t *testing.T) {
	t.Parallel()
	clock := newStepClock()
	b := New("abcdef")
	b.SetClock(clock.now)
	b.Delete(Pos{0, 2}, Pos{0, 3}) // c, cursor now at 2
	b.Delete(Pos{0, 1}, Pos{0, 2}) // b, backspace-style
	if got := b.Text(); got != "adef" {
		t.Fatalf("text = %q", got)
	}
	b.Undo()
	if got := b.Text(); got != "abcdef" {
		t.Fatalf("backspace run not undone as one group: %q", got)
	}

	f := New("abcdef")
	f.SetClock(clock.now)
	f.Delete(Pos{0, 1}, Pos{0, 2}) // b
	f.Delete(Pos{0, 1}, Pos{0, 2}) // c, forward-delete style
	f.Undo()
	if got := f.Text(); got != "abcdef" {
		t.Fatalf("forward-delete run not undone as one group: %q", got)
	}
}

func TestNewEditClearsRedo(t *testing.T) {
	t.Parallel()
	b := New("")
	b.Insert(Pos{}, "a")
	b.Break()
	b.Undo()
	b.Insert(Pos{}, "z")
	if _, ok := b.Redo(); ok {
		t.Fatal("redo should be cleared by a new edit")
	}
}

func TestUndoMultiLineRestoresExactly(t *testing.T) {
	t.Parallel()
	b := New("one\ntwo\nthree")
	b.Break()
	b.Delete(Pos{0, 1}, Pos{2, 3})
	b.Break()
	b.Insert(Pos{0, 1}, "x\ny\n")
	b.Undo()
	b.Undo()
	if got := b.Text(); got != "one\ntwo\nthree" {
		t.Fatalf("exact restore failed: %q", got)
	}
}

func TestModifiedTracksSaveAcrossUndo(t *testing.T) {
	t.Parallel()
	b := New("x")
	if b.Modified() {
		t.Fatal("fresh buffer should not be modified")
	}
	b.Insert(Pos{0, 1}, "y")
	if !b.Modified() {
		t.Fatal("edit should mark buffer modified")
	}
	b.MarkSaved()
	if b.Modified() {
		t.Fatal("MarkSaved should clear modified")
	}
	b.Break()
	b.Insert(Pos{0, 2}, "z")
	if !b.Modified() {
		t.Fatal("edit after save should be modified")
	}
	b.Undo()
	if b.Modified() {
		t.Fatal("undo back to saved content should not be modified")
	}
	b.Redo()
	if !b.Modified() {
		t.Fatal("redo past saved content should be modified")
	}
}

func TestModifiedWhenMergedAfterSave(t *testing.T) {
	t.Parallel()
	b := New("")
	b.Insert(Pos{}, "a")
	b.MarkSaved()
	// Merges into the saved group; must still count as modified.
	b.Insert(Pos{0, 1}, "b")
	if !b.Modified() {
		t.Fatal("merged edit after save must be modified")
	}
}
