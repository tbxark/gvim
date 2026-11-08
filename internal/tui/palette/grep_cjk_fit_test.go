package palette

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// A grep row with wide characters before the match keeps the whole match on screen from
// 25 columns up, since the cut counts cells, not runes. Narrower, the match is cut too.
func TestFitGrepKeepsTheMatchAfterWideText(t *testing.T) {
	t.Parallel()
	r := grepRow(GrepHit{Rel: "docs/notes.md", Line: 12, Col: 17, Text: "这是一个很长的中文句子用于测试 match 之后的文字"}, "match")
	for width := 25; width <= 60; width++ {
		got, _ := fitGrep(r, width)
		if w := ansi.StringWidth(got); w > width {
			t.Fatalf("width %d: %q is %d cells", width, got, w)
		}
		if !strings.Contains(got, "match") {
			t.Errorf("width %d: the match is not on the row: %q", width, got)
		}
	}
}
