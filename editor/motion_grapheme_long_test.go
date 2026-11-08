package editor

import (
	"math/rand/v2"
	"strings"
	"testing"

	"github.com/rivo/uniseg"

	"github.com/tbxark/gvim/buffer"
)

// refBoundaries returns the rune columns of the cluster boundaries of text, from its start.
func refBoundaries(text string) []int {
	out := []int{0}
	n := 0
	for rest := text; rest != ""; {
		cluster, tail, _, _ := uniseg.FirstGraphemeClusterInString(rest, -1)
		n += len([]rune(cluster))
		out = append(out, n)
		rest = tail
	}
	return out
}

// On a line far longer than clusterWholeLine, the boundary before and after a column is what a
// walk of the line from its start finds, for text with marks, joined emoji and wide characters.
func TestClusterBoundariesOfAVeryLongLine(t *testing.T) {
	t.Parallel()
	pieces := []string{"abc ", "日本語", "é", "👨‍👩‍👧", "é", "漢字かな", "ñ", " x ", "กำ", "ab"}
	rng := rand.New(rand.NewPCG(6, 1))
	var sb strings.Builder
	for len([]rune(sb.String())) < 3*clusterWholeLine {
		sb.WriteString(pieces[rng.IntN(len(pieces))])
	}
	text := sb.String()
	b := buffer.New("head\n" + text)
	bounds := refBoundaries(text)
	isBoundary := make(map[int]bool, len(bounds))
	for _, c := range bounds {
		isBoundary[c] = true
	}
	n := len([]rune(text))
	for trial := 0; trial < 2000; trial++ {
		col := 1 + rng.IntN(n-1)
		// The reference: the largest boundary below col, and the smallest above it.
		prev, next := 0, n
		for c := col - 1; c >= 0; c-- {
			if isBoundary[c] {
				prev = c
				break
			}
		}
		for c := col + 1; c <= n; c++ {
			if isBoundary[c] {
				next = c
				break
			}
		}
		if got := prevBoundary(b, 1, col); got != prev {
			t.Fatalf("prevBoundary(%d) = %d, want %d", col, got, prev)
		}
		if got := nextBoundary(b, 1, col); got != next {
			t.Fatalf("nextBoundary(%d) = %d, want %d", col, got, next)
		}
	}
}
