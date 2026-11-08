package editorpane

import (
	"strings"
	"unicode"

	"github.com/charmbracelet/x/ansi"

	"github.com/tbxark/gvim/internal/tui/grid"
)

// inlineDiagMin is the fewest cells an end-of-line diagnostic message may take.
// A narrower gap is not a message any more, so the line shows none: the text
// keeps its room.
const inlineDiagMin = 8

// paintInlineDiagnostics writes the most severe diagnostic message of line i at
// the right end of its last row (editor.diagnostics_inline_text). The message
// starts after one blank cell from the text, right-aligned against the column
// before the scrollbar, and is cut with an ellipsis when the row is narrow. The
// severity colour is the message's, so a warning and an error read apart; a
// severity without a colour falls back to the muted text style.
//
// The port carries no diagnostic version, so the message is never dimmed for
// staleness: gvim's diagnostics are the language server's latest reply, not a
// snapshot of the text at a version (docs/lsp.md records the difference from
// Fresh).
func (f frame) paintInlineDiagnostics(g *grid.Grid, i, y, end int, cur bool) {
	if !f.inlineDiag || f.diags == nil || f.win != nil {
		return
	}
	sev, msg := mostSevereDiagnostic(f.diags(i))
	if msg == "" {
		return
	}
	// One blank between the text and the message, and one at the screen edge.
	right := f.w - 2
	room := right - max(end, f.gutter) - 1
	if room < inlineDiagMin {
		return
	}
	text := ansi.Truncate(msg, room, "…")
	st := f.styles.Text
	if cur && f.styles.CurrentLine.BG != nil {
		st.BG = f.styles.CurrentLine.BG
	}
	if fg := f.diagnosticFG(sev); fg != nil {
		st.FG = fg
	} else {
		st.Faint = true
	}
	x := right - ansi.StringWidth(text) + 1
	if x <= end || x < f.gutter {
		return
	}
	g.Put(x, y, right+1, text, st)
}

// mostSevereDiagnostic returns the severity and message of the most severe
// diagnostic of one line. A message is a single row: control characters and line
// breaks become spaces, and the ends are trimmed. A diagnostic with no message is
// skipped, as is a severity outside the LSP range.
func mostSevereDiagnostic(spans []DiagnosticSpan) (severity int, message string) {
	severity = -1
	for _, s := range spans {
		if s.Severity < SeverityError || s.Severity > SeverityHint {
			continue
		}
		msg := cleanDiagnosticMessage(s.Message)
		if msg == "" {
			continue
		}
		if severity < 0 || s.Severity < severity {
			severity, message = s.Severity, msg
		}
	}
	if severity < 0 {
		return 0, ""
	}
	return severity, message
}

// cleanDiagnosticMessage makes a diagnostic message fit one pane row.
func cleanDiagnosticMessage(msg string) string {
	msg = strings.TrimSpace(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.Is(unicode.Zs, r) && r != ' ' {
			return ' '
		}
		return r
	}, msg))
	return strings.Join(strings.Fields(msg), " ")
}
