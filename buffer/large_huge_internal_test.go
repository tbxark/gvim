package buffer

import (
	"math/rand/v2"
	"strings"
	"testing"
)

// hugeText is a line of n runes made of the runes in alphabet, picked by rng.
func hugeText(rng *rand.Rand, n int, alphabet []rune) string {
	r := make([]rune, n)
	for i := range r {
		r[i] = alphabet[rng.IntN(len(alphabet))]
	}
	return string(r)
}

// A line longer than MaxLine is read to its end through its index: its length, the characters in
// a window of it, a range inside it and ranges that take it whole are what a memory buffer holds,
// whatever the runes are and wherever the chunks of the index scan and its marks fall.
func TestHugeLineIsReadToItsEnd(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		alphabet []rune
		bad      bool // sprinkle invalid UTF-8
	}{
		{"ascii", []rune("abcdefghij klmnopqrstuvwxyz,;{}\"0123456789"), false},
		{"two byte runes", []rune("aé ü日"), false},
		{"all widths", []rune("a é 日本 😀 z"), false},
		{"invalid bytes", []rune("ab c"), true},
	}
	for _, tt := range tests {
		for _, geom := range []struct{ stride, chunk int }{{4096, 4 << 20}, {7, 100}, {1, 64}, {50, 33}} {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				rng := rand.New(rand.NewPCG(uint64(len(tt.name)), uint64(geom.stride*1000+geom.chunk)))
				text := hugeText(rng, 700, tt.alphabet)
				if tt.bad {
					b := []byte(text)
					for i := 5; i < len(b); i += 37 {
						b[i] = 0xff
					}
					text = string(b)
				}
				full := string([]rune(text)) // invalid bytes as U+FFFD, as a decoded line shows them
				src := "head\n" + text + "\ntail"
				b := tinyOpen(t, src)
				b.big.strideOver, b.big.chunkOver = geom.stride, geom.chunk
				indexFully(t, b)
				if b.Lines() != 3 {
					t.Fatalf("Lines = %d", b.Lines())
				}
				runes := []rune(full)
				if got := b.LineLen(1); got != len(runes) {
					t.Fatalf("LineLen = %d, want %d", got, len(runes))
				}
				if got := b.LineBytes(1); got != len(runes) {
					t.Fatalf("LineBytes = %d, want the %d runes", got, len(runes))
				}
				if !b.LineCut(1) {
					t.Error("a line over MaxLine is still cut for the reads that need it whole")
				}
				for k := 0; k < 300; k++ {
					col, n := rng.IntN(len(runes)+20)-10, rng.IntN(150)
					lo := min(max(col, 0), len(runes))
					hi := min(lo+n, len(runes))
					if got, want := b.LineSlice(1, col, n), string(runes[lo:hi]); got != want {
						t.Fatalf("LineSlice(%d, %d) = %q, want %q", col, n, got, want)
					}
					p := Pos{Line: 1, Col: rng.IntN(len(runes) + 3)}
					r, ok := b.RuneAt(p)
					if wantOK := p.Col < len(runes); ok != wantOK || (ok && r != runes[p.Col]) {
						t.Fatalf("RuneAt(%v) = %q, %v", p, r, ok)
					}
					from, to := Pos{Line: 1, Col: rng.IntN(len(runes) + 1)}, Pos{Line: 1, Col: rng.IntN(len(runes) + 1)}
					want := string(runes[min(from.Col, to.Col):max(from.Col, to.Col)])
					if got := b.Range(from, to); got != want {
						t.Fatalf("Range(%v, %v) = %q, want %q", from, to, got, want)
					}
				}
				wholeText := "head\n" + full + "\ntail"
				if got := b.Text(); got != wholeText {
					t.Fatalf("Text differs: %d bytes, want %d", len(got), len(wholeText))
				}
				if got := b.Range(Pos{Line: 0, Col: 2}, Pos{Line: 2, Col: 2}); got != "ad\n"+full+"\nta" {
					t.Fatalf("Range over the line = %q", got)
				}
				if got := b.Range(Pos{Line: 1, Col: 5}, Pos{Line: 2, Col: 1}); got != string(runes[5:])+"\nt" {
					t.Fatalf("Range from inside the line = %q", got)
				}
				if got := b.Range(Pos{Line: 0, Col: 1}, Pos{Line: 1, Col: 9}); got != "ead\n"+string(runes[:9]) {
					t.Fatalf("Range to inside the line = %q", got)
				}
				if got, want := b.End(), (Pos{Line: 2, Col: 4}); got != want {
					t.Fatalf("End = %v", got)
				}
				if got := b.Clamp(Pos{Line: 1, Col: 1 << 30}); got != (Pos{Line: 1, Col: len(runes)}) {
					t.Fatalf("Clamp = %v, want the real end of the line", got)
				}
				if got := b.TextPrefix(300); got != wholeText[:min(300, len(wholeText))] {
					// A prefix is cut at a rune start, so it may be shorter than the bytes asked for.
					if !strings.HasPrefix(wholeText, got) || len(got) < 297 {
						t.Fatalf("TextPrefix(300) = %q", got)
					}
				}
			})
		}
	}
}

// A huge line is edited whole, and an edit elsewhere leaves it as it was.
func TestHugeLineIsEditedAndItsNeighboursToo(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("x", 500)
	b := tinyOpen(t, "a\n"+long+"\nz")
	indexFully(t, b)
	b.Insert(Pos{Line: 1, Col: 300}, "!")
	if err := b.Refusal(); err != nil {
		t.Fatalf("an insert into a huge line was refused: %v", err)
	}
	if got := b.LineLen(1); got != 501 {
		t.Fatalf("the huge line has %d runes after the edit, want 501", got)
	}
	b.Insert(Pos{Line: 2, Col: 1}, "!")
	if err := b.Refusal(); err != nil {
		t.Fatalf("an insert after the huge line was refused: %v", err)
	}
	if got := b.Line(2); got != "z!" {
		t.Fatalf("line 2 = %q", got)
	}
	// A delete that ends inside the huge line goes through, and the rest of the line joins the first.
	b.Delete(Pos{Line: 0, Col: 0}, Pos{Line: 1, Col: 400})
	if err := b.Refusal(); err != nil {
		t.Fatalf("a delete ending inside a huge line was refused: %v", err)
	}
	if b.Lines() != 2 || b.LineLen(0) != 101 || b.Line(1) != "z!" {
		t.Fatalf("after the delete: %d lines, line 0 has %d runes, line 1 %q", b.Lines(), b.LineLen(0), b.Line(1))
	}
}
