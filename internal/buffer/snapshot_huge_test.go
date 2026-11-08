package buffer_test

import (
	"strings"
	"testing"
)

// A line longer than MaxLine is huge. A snapshot reads it by windows of runes, where Line and
// Chunk give only its beginning. The windows agree with the runes of the whole line.
func TestSnapshotReadsAHugeLineInWindows(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("ab", 200) + "NEEDLE" + strings.Repeat("c", 150) + "\xff" + "z"
	runes := []rune(long)
	b, _ := openTiny(t, []byte("short\n"+long+"\nlast\n"), 0)
	indexAll(t, b)
	tests := []struct {
		name       string
		touchFirst bool // read the line through the buffer first, so the snapshot copies its index
		line       int
		col, n     int
		want       string
	}{
		{"start", false, 1, 0, 10, "ababababab"},
		{"needle", false, 1, 400, 6, "NEEDLE"},
		{"past the cap of the line reader", false, 1, 500, 40, string(runes[500:540])},
		{"invalid byte is one rune", false, 1, len(runes) - 2, 2, "�z"},
		{"clamped at the end", false, 1, len(runes) - 3, 100, string(runes[len(runes)-3:])},
		{"clamped at the start", false, 1, -5, 4, "abab"},
		{"through the buffer's index", true, 1, 400, 6, "NEEDLE"},
		{"short line", false, 0, 1, 3, "hor"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.touchFirst {
				if b.LineLen(tc.line) != len(runes) {
					t.Fatalf("LineLen = %d, want %d", b.LineLen(tc.line), len(runes))
				}
			}
			s := b.Snapshot()
			defer s.Release()
			if got := s.LineRunes(tc.line, tc.col, tc.n); got != tc.want {
				t.Fatalf("LineRunes(%d, %d, %d) = %q, want %q", tc.line, tc.col, tc.n, got, tc.want)
			}
		})
	}
	s := b.Snapshot()
	defer s.Release()
	if n, ok := s.LongLine(1); !ok || n != len(runes) {
		t.Errorf("LongLine(1) = %d, %v; want %d, true", n, ok, len(runes))
	}
	for _, i := range []int{0, 2} {
		if _, ok := s.LongLine(i); ok {
			t.Errorf("LongLine(%d) reports a huge line", i)
		}
	}
}

// Chunk does not join a huge line to the lines before it, so the caller can read it by windows.
func TestSnapshotChunkStopsBeforeAHugeLine(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("q", 300)
	b, _ := openTiny(t, []byte("one\ntwo\n"+long+"\nlast\n"), 0)
	indexAll(t, b)
	s := b.Snapshot()
	defer s.Release()
	first, n, text := s.Chunk(0, 1<<20)
	if first != 0 || n != 2 || text != "one\ntwo" {
		t.Errorf("Chunk(0) = %d, %d, %q; want 0, 2, the two short lines", first, n, text)
	}
	first, n, text = s.ChunkBefore(3, 1<<20)
	if first != 2 || n != 1 || len(text) == len(long) {
		t.Errorf("ChunkBefore(3) = %d, %d, len %d; want the huge line alone, cut as Line cuts it", first, n, len(text))
	}
}
