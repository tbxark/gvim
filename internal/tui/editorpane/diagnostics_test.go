package editorpane

import (
	"testing"

	"github.com/tbxark/gvim/internal/tui/grid"
)

func TestDiagnosticUnderlinesItsSpanInTheSeverityColor(t *testing.T) {
	styles := Styles{
		DiagError:   grid.Style{FG: cRed},
		DiagWarning: grid.Style{FG: cGreen},
		DiagInfo:    grid.Style{FG: cBlue},
		DiagHint:    grid.Style{FG: cGray},
	}
	tests := []struct {
		name     string
		severity int
		want     any
	}{
		{"error", SeverityError, cRed},
		{"warning", SeverityWarning, cGreen},
		{"info", SeverityInfo, cBlue},
		{"hint", SeverityHint, cGray},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := Pane{
				Doc:    newDoc("let x = 1"),
				Styles: styles,
				Diagnostics: func(line int) []DiagnosticSpan {
					return []DiagnosticSpan{{Start: 4, End: 5, Severity: tc.severity}}
				},
			}
			g := p.Render(20, 1)
			c := cellAt(t, g, 10, 0)
			if c.Text != "x" || !c.Style.Underline || c.Style.FG != tc.want {
				t.Errorf("span cell = %q underline %v fg %v, want x underlined in %v", c.Text, c.Style.Underline, c.Style.FG, tc.want)
			}
			if u := cellAt(t, g, 9, 0); u.Style.Underline {
				t.Error("cell before the span is underlined")
			}
		})
	}
}

func TestDiagnosticOnlyTouchesItsLine(t *testing.T) {
	p := Pane{
		Doc:    newDoc("a", "b"),
		Styles: Styles{DiagError: grid.Style{FG: cRed}},
		Diagnostics: func(line int) []DiagnosticSpan {
			if line == 1 {
				return []DiagnosticSpan{{Start: 0, End: 1, Severity: SeverityError}}
			}
			return nil
		},
	}
	g := p.Render(20, 2)
	if cellAt(t, g, 6, 0).Style.Underline {
		t.Error("line without a diagnostic is underlined")
	}
	if !cellAt(t, g, 6, 1).Style.Underline {
		t.Error("line with a diagnostic is not underlined")
	}
}

func TestNoDiagnosticHookPaintsNothing(t *testing.T) {
	g := Pane{Doc: newDoc("abc"), Styles: Styles{DiagError: grid.Style{FG: cRed}}}.Render(20, 1)
	if cellAt(t, g, 6, 0).Style.Underline {
		t.Error("underline without a hook")
	}
}
