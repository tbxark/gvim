package editor

import (
	"strings"
	"testing"

	"github.com/rivo/uniseg"

	"github.com/tbxark/gvim/internal/buffer"
)

// refCellColumn is the display column of rune column col of line, walking the whole line.
func refCellColumn(line string, col, tab int) int {
	cells, n := 0, 0
	for rest := line; rest != "" && n < col; {
		cluster, tail, w, _ := uniseg.FirstGraphemeClusterInString(rest, -1)
		cells += clusterCells(cluster, w, cells, tab)
		n += len([]rune(cluster))
		rest = tail
	}
	return cells
}

// refColAtCell is the rune column of the first cluster boundary at or after target cells.
func refColAtCell(line string, target, tab int) int {
	cells, col := 0, 0
	for rest := line; rest != "" && cells < target; {
		cluster, tail, w, _ := uniseg.FirstGraphemeClusterInString(rest, -1)
		cells += clusterCells(cluster, w, cells, tab)
		col += len([]rune(cluster))
		rest = tail
	}
	return col
}

// The chunked walk gives what a walk of the whole line gives, whatever the line holds at the
// edges of a chunk: a combining mark, a joined emoji, a wide character, a tab.
func TestClusterWalkMatchesAWalkOfTheWholeLine(t *testing.T) {
	t.Parallel()
	pad := func(n int) string { return strings.Repeat("a", n) }
	lines := map[string]string{
		"ascii":            pad(5000),
		"mark at the edge": pad(walkChunk-1) + "é" + pad(100),
		"mark after edge":  pad(walkChunk) + "́" + pad(100),
		"emoji at edge":    pad(walkChunk-2) + "👨‍👩‍👧" + pad(50),
		"wide at edge":     pad(walkChunk-1) + "日本語日本語" + pad(10),
		"tabs":             "\t" + pad(10) + "\t\t" + pad(walkChunk) + "\tx",
		"mixed":            strings.Repeat("ab日\té́", 900),
		"short":            "hello",
		"empty":            "",
	}
	for name, text := range lines {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			b := buffer.New("first\n" + text)
			n := len([]rune(text))
			for _, tab := range []int{4, 8} {
				for col := 0; col <= n+2; col += max(n/97, 1) {
					if got, want := cellColumn(b, buffer.Pos{Line: 1, Col: col}, tab), refCellColumn(text, col, tab); got != want {
						t.Fatalf("tab %d: cellColumn(col %d) = %d, want %d", tab, col, got, want)
					}
				}
				total := refCellColumn(text, n, tab)
				for target := 0; target <= total+2; target += max(total/89, 1) {
					if got, want := colAtCell(b, 1, target, tab), refColAtCell(text, target, tab); got != want {
						t.Fatalf("tab %d: colAtCell(target %d) = %d, want %d", tab, target, got, want)
					}
				}
			}
		})
	}
}

// A motion on a very long ASCII line gives the same cells as on a short one.
func TestMotionOnAVeryLongLine(t *testing.T) {
	t.Parallel()
	b := buffer.New(strings.Repeat("x", 3_000_000))
	if got := cellColumn(b, buffer.Pos{Col: 10}, 4); got != 10 {
		t.Fatalf("cellColumn = %d, want 10", got)
	}
	if got := colAtCell(b, 0, 25, 4); got != 25 {
		t.Fatalf("colAtCell = %d, want 25", got)
	}
}
