package editorpane

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"

	"github.com/tbxark/gvim/internal/tui/style"
)

// The page view deliberately keeps the source-column decorations off: the rulers
// and the indent guides would sit at text columns the compose has rewritten.
func TestPageViewKeepsRulersAndGuidesOff(t *testing.T) {
	t.Parallel()
	doc := composeDoc("# Title", "alpha beta gamma", "tail")
	styles := Styles{Ruler: style.Cell(lipgloss.NewStyle().Background(lipgloss.Color("4")))}
	p := Pane{
		Doc: doc, PageView: PageView{On: true, Measure: 30, Markdown: true},
		Styles: styles, Rulers: []int{3, 9}, IndentGuides: true,
	}
	g := p.Render(40, 8)
	rows := g.Plain()
	if strings.Contains(strings.Join(rows, "\n"), "▏") {
		t.Fatalf("the page view drew an indent guide: %q", rows)
	}
	for y := 0; y < g.Height(); y++ {
		for x := 0; x < g.Width(); x++ {
			if c := g.At(x, y); c.Style.BG == styles.Ruler.BG {
				t.Fatalf("the page view drew a ruler at (%d,%d): %q", x, y, rows)
			}
		}
	}
}
