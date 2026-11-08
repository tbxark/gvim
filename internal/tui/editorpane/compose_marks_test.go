package editorpane

import (
	"testing"

	"charm.land/lipgloss/v2"

	"github.com/tbxark/gvim/internal/tui/style"
)

func TestComposeMapsSearchHighlights(t *testing.T) {
	t.Parallel()
	doc := composeDoc("Some **bold** text", "plain end")
	styles := Styles{Match: style.Cell(lipgloss.NewStyle().Background(lipgloss.Color("3")))}
	// The match covers source columns of the hidden "bold" text (7..11).
	p := Pane{
		Doc: doc, PageView: PageView{On: true, Measure: 30, Markdown: true}, Styles: styles,
		Highlights: func(line int) []Highlight {
			if line != 0 {
				return nil
			}
			return []Highlight{{Start: 7, End: 11}}
		},
	}
	g := p.Render(40, 4)
	// The margin is (39-30)/2 = 4; the composed row is "Some bold text", so the
	// match covers composed columns 5..9.
	for x := 4 + 5; x < 4+9; x++ {
		if got := g.At(x, 0).Style.BG; got != styles.Match.BG {
			t.Fatalf("cell %d does not take the match background: %+v", x, g.At(x, 0))
		}
	}
	if bg := g.At(4+4, 0).Style.BG; bg == styles.Match.BG {
		t.Fatal("the space before the match takes the match background")
	}
}

func TestComposeMapsDiagnostics(t *testing.T) {
	t.Parallel()
	doc := composeDoc("Some **bold** text", "plain end")
	styles := Styles{DiagError: style.Cell(lipgloss.NewStyle().Foreground(lipgloss.Color("1")))}
	p := Pane{
		Doc: doc, PageView: PageView{On: true, Measure: 30, Markdown: true}, Styles: styles,
		Diagnostics: func(line int) []DiagnosticSpan {
			if line != 0 {
				return nil
			}
			return []DiagnosticSpan{{Start: 7, End: 11, Severity: SeverityError, Message: "bad"}}
		},
	}
	g := p.Render(40, 4)
	cell := g.At(4+5, 0)
	if !cell.Style.Underline || cell.Style.FG != styles.DiagError.FG {
		t.Fatalf("the diagnostic cell = %+v, want the error underline", cell)
	}
}
