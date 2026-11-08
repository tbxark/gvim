package dock

import (
	"strings"

	"github.com/tbxark/gvim/internal/terminal"
)

// The transcript limits: a restarted shell keeps its old output above the new
// one, and the saved terminal keeps its transcript in the session directory. Both
// are bounded to the newest rows, so neither can grow without end.
const (
	maxPriorRows  = 500
	maxSavedRows  = 500
	maxSavedBytes = 256 << 10
)

// capturePrior appends the transcript of the shell the dock holds to the prior
// rows and trims the prior rows to the newest. A restarted shell keeps its
// transcript: the old session's history and screen are captured before the
// session closes and painted above the new shell's output.
func (p *Panel) capturePrior() {
	if p.term == nil {
		return
	}
	prior := captureTranscript(p.term)
	if len(prior) == 0 {
		return
	}
	p.prior = append(p.prior, prior...)
	p.trimPrior()
}

// trimPrior keeps the newest maxPriorRows prior rows.
func (p *Panel) trimPrior() {
	if len(p.prior) > maxPriorRows {
		p.prior = p.prior[len(p.prior)-maxPriorRows:]
	}
}

// captureTranscript returns the bounded transcript of t: the newest history rows
// followed by the live screen rows, as cells. It runs on the update loop, so the
// copy is bounded to MaxScrollbackSnapshot rows and one screen.
func captureTranscript(t Terminal) []terminal.Line {
	if t == nil {
		return nil
	}
	sc := t.Screen()
	total := t.ScrollbackLen()
	var out []terminal.Line
	if total > 0 {
		from := max(total-maxPriorRows, 0)
		count := min(total-from, terminal.MaxScrollbackSnapshot)
		lines, _ := t.Scrollback(from, count)
		out = append(out, lines...)
	}
	for r := 0; r < sc.Rows; r++ {
		cells := make([]terminal.Cell, sc.Cols)
		for c := range sc.Cols {
			cells[c] = sc.Cell(r, c)
		}
		out = append(out, terminal.Line{Cells: cells})
	}
	// The newest rows are the screen's; trim only the oldest history rows.
	if len(out) > maxPriorRows {
		out = out[len(out)-maxPriorRows:]
	}
	return out
}

// lineFromText turns a saved transcript line into cells, one rune per cell.
func lineFromText(text string) terminal.Line {
	runes := []rune(text)
	cells := make([]terminal.Cell, len(runes))
	for i, r := range runes {
		cells[i] = terminal.Cell{Rune: r}
	}
	return terminal.Line{Cells: cells}
}

// SavedState returns the terminal state the session file keeps: the dock's open
// flag and directory, the pane width the transcript was captured at, and the
// newest transcript rows as text. The transcript is the prior rows (if any), then
// the history window, then the live screen rows. It is bounded and stripped of
// control characters so a saved answer can never move the cursor of the loader's
// terminal.
func (p *Panel) SavedState() terminal.SavedState {
	st := terminal.SavedState{
		Version: terminal.SavedStateVersion,
		Open:    p.open || p.restored || len(p.prior) > 0,
		Cwd:     p.dir,
		Width:   p.contentCols(),
	}
	if p.term != nil {
		if cwd := p.term.WorkingDirectory(); cwd != "" {
			st.Cwd = cwd
		}
	}
	var rows []string
	add := func(text string) {
		rows = append(rows, cleanSavedText(text))
	}
	for i := range p.prior {
		add(p.prior[i].Text())
	}
	total := 0
	if p.term != nil {
		total = p.term.ScrollbackLen()
	}
	if total > 0 {
		from := max(total-maxSavedRows, 0)
		count := min(total-from, terminal.MaxScrollbackSnapshot)
		if lines, _ := p.term.Scrollback(from, count); len(lines) > 0 {
			for i := range lines {
				add(lines[i].Text())
			}
		}
	}
	if p.term != nil {
		sc := p.term.Screen()
		for r := 0; r < sc.Rows; r++ {
			add(screenRowText(sc, r))
		}
	}
	// Bound to the newest rows and bytes.
	if len(rows) > maxSavedRows {
		rows = rows[len(rows)-maxSavedRows:]
	}
	size := 0
	for i := len(rows) - 1; i >= 0; i-- {
		size += len(rows[i]) + 1
		if size > maxSavedBytes {
			rows = rows[i+1:]
			break
		}
	}
	st.Rows = rows
	return st
}

// cleanSavedText strips control characters from a saved transcript row, so a
// shell's output cannot inject escape sequences into the loader.
func cleanSavedText(text string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, text)
}

// Restore puts a saved terminal state back: the dock is shown, read-only, over
// the saved transcript, dead until the user restarts it. Nothing runs
// automatically; Restart starts a shell in the saved directory and appends it
// below the restored transcript.
func (p *Panel) Restore(st terminal.SavedState) {
	rows := st.Rows
	if len(rows) > maxSavedRows {
		rows = rows[len(rows)-maxSavedRows:]
	}
	p.prior = p.prior[:0]
	for _, text := range rows {
		p.prior = append(p.prior, lineFromText(text))
	}
	p.restored = true
	p.open = st.Open || len(p.prior) > 0
	p.focus = false
	p.dir = st.Cwd
	p.term = nil
	p.exited = true
	p.notice = ""
	p.scroll = scrollState{on: true, screenRows: 0, gen: p.gen}
	p.scroll.top = max(p.combined()-max(p.bodyRows(), 1), 0)
	p.scroll.selected = max(p.combined()-1, 0)
	p.clearSelection()
}

// Restored reports whether the dock shows a restored transcript without a shell.
func (p *Panel) Restored() bool { return p.restored }
