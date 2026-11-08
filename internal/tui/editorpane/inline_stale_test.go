package editorpane

import (
	"strings"
	"testing"
)

// TestInlineDiagnosticsDimStaleMessages checks the staleness rendering: a
// diagnostic published for an older version of the document is drawn faint, so the
// message reads as out of date, while a current one keeps its severity colour.
func TestInlineDiagnosticsDimStaleMessages(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		stale bool
		want  bool
	}{
		{"a current diagnostic is not dimmed", false, false},
		{"a stale diagnostic is dimmed", true, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc := newDoc("hello")
			p := diagPane(doc, []DiagnosticSpan{{Start: 0, End: 5, Severity: SeverityInfo, Message: "old", Stale: tt.stale}}, Styles{})
			g := p.Render(30, 2)
			row := rowsOf(g)[0]
			if !strings.HasSuffix(row, "old") {
				t.Fatalf("row = %q, want the message at its end", row)
			}
			x := len([]rune(row)) - len("old")
			cell := g.At(x, 0)
			if cell.Style.Faint != tt.want {
				t.Fatalf("message faint = %v, want %v", cell.Style.Faint, tt.want)
			}
		})
	}
}
