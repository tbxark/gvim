package dock

import (
	"strings"
	"unicode/utf8"

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
	prior, width := captureTranscript(p.term)
	if len(prior) == 0 {
		return
	}
	if width > 0 {
		p.priorWidth = width
	}
	p.prior = append(p.prior, prior...)
	p.trimPrior()
}

// reflowPrior re-wraps the stored transcript when the pane's content width
// changed. The live screen is never reflowed (x/vt clips it); this is the text
// re-wrap of stored rows only, see terminal.ReflowLines.
func (p *Panel) reflowPrior(cols int) {
	if cols <= 0 {
		return
	}
	if p.priorWidth <= 0 {
		// A transcript saved before the width was recorded: adopt the current width
		// and leave the rows as they are.
		p.priorWidth = cols
		return
	}
	if p.priorWidth == cols {
		return
	}
	if len(p.prior) > 0 {
		p.prior = terminal.ReflowLines(p.prior, p.priorWidth, cols)
		p.trimPrior()
		p.clearSelection()
		// The saved snapshot's row mapping (history + visible tail + cursor) no
		// longer holds after a re-wrap: drop the metadata rather than paint a cursor
		// on a different row.
		p.savedCursor = terminal.SavedCursor{}
		p.savedVisibleRows = 0
		p.savedAlt = false
		if p.restored {
			p.reflowed = true
		}
		if p.scroll.on {
			p.scroll.top = max(p.combined()-max(p.bodyRows(), 1), 0)
			p.scroll.selected = max(p.combined()-1, 0)
		}
	}
	p.priorWidth = cols
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
func captureTranscript(t Terminal) ([]terminal.Line, int) {
	if t == nil {
		return nil, 0
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
	return out, sc.Cols
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
	// Live is whether a process was running at the capture: only such a tab may be
	// revived on a dive after the restore. A restored tab that was not revived is
	// dead and is saved as dead, so a later restore never starts it automatically.
	// A restored-but-not-yet-revived tab keeps its saved liveness: saving it again (or
	// extracting its record into another workspace) must not turn a dormant live tab
	// into a dead one. A dive clears wasLive, so an attempted revive is recorded as the
	// resulting state.
	st.Live = (p.term != nil && !p.exited) || p.wasLive
	// The styled screen snapshot preserves the visible screen's colours/attributes
	// across the restore (Fresh's backing file keeps the screen too). A live terminal
	// snapshots its screen; a restored tab without a shell keeps the styled tail of
	// its prior transcript, so another save does not wash its styles out.
	switch {
	case p.term != nil:
		sc := p.term.Screen()
		if sc.Alt {
			st.Screen = terminal.SnapshotScreen(sc)
			break
		}
		total := p.term.ScrollbackLen()
		keep := min(total, terminal.MaxSavedHistoryRows)
		var history []terminal.Line
		if keep > 0 {
			history, _ = p.term.Scrollback(total-keep, keep)
		}
		st.Screen = terminal.SnapshotScreenWithHistory(sc, history)
	case len(p.prior) > 0:
		// A restored tab keeps its full snapshot (styles history + cursor + alt)
		// across another save: SnapshotPriorScreen rebuilds it from the styled prior
		// rows captured when the record was applied.
		if snap := terminal.SnapshotPriorScreen(p.prior, p.savedVisibleRows, p.savedCursor, p.savedAlt); snap != nil {
			st.Screen = snap
		} else {
			st.Screen = terminal.SnapshotLines(p.prior)
		}
	}
	// The launch record of the terminal that produced the transcript, so the
	// user-initiated restart can rejoin it after an editor restart.
	launch := p.launch.Clean()
	st.LaunchArgv = launch.Argv
	st.ResumeArgv = launch.ResumeArgv
	st.Agent = launch.Agent
	st.Ephemeral = launch.Ephemeral
	st.LaunchDir = clipSavedDir(p.launchDir)
	st.Source = launch.Source
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
	// The styled snapshot (history + visible screen + cursor + alt) replaces the
	// text tail it was captured from, but only when every row matches exactly; a
	// mismatched/corrupt snapshot is ignored whole so a restore never paints
	// different content than the transcript rows.
	p.applySavedScreen(st.Screen)
	p.restored = true
	p.wasLive = st.Live
	p.reflowed = false
	p.priorWidth = st.Width
	p.open = st.Open || len(p.prior) > 0
	p.focus = false
	p.dir = st.Cwd
	p.launchDir = clipSavedDir(st.LaunchDir)
	if p.launchDir == "" {
		// An older record has no launch_dir: its cwd is the best available launch
		// directory (the migration keeps the old behaviour rather than inventing a
		// path).
		p.launchDir = clipSavedDir(st.Cwd)
	}
	p.term = nil
	p.exited = true
	p.notice = ""
	// The saved record never starts anything here: it only arms the user's
	// restart/resume, which applies terminal.resume_agents at that point.
	p.launch = terminal.Launch{
		Argv:       append([]string(nil), st.LaunchArgv...),
		ResumeArgv: append([]string(nil), st.ResumeArgv...),
		Agent:      st.Agent,
		Ephemeral:  st.Ephemeral,
		Source:     st.Source,
	}.Clean()
	p.scroll = scrollState{on: true, screenRows: 0, gen: p.gen}
	p.scroll.top = max(p.combined()-max(p.bodyRows(), 1), 0)
	p.scroll.selected = max(p.combined()-1, 0)
	p.clearSelection()
}

// maxSavedDirBytes bounds a saved directory field, so a hand-edited record cannot
// pass an unbounded path to a start.
const maxSavedDirBytes = 4096

// clipSavedDir bounds a saved directory on a rune boundary.
func clipSavedDir(dir string) string {
	if len(dir) <= maxSavedDirBytes {
		return dir
	}
	cut := dir[:maxSavedDirBytes]
	for len(cut) > 0 && !utf8.ValidString(cut) {
		cut = cut[:len(cut)-1]
	}
	return cut
}

// applySavedScreen overlays the styled saved screen and its styled history onto
// the text prior, and records the saved cursor/alt/visible-row count. The
// snapshot is used only when it is structurally valid, its rows fit the prior and
// every visible AND history row's text matches the corresponding transcript row
// exactly; otherwise the plain rows stay and the saved metadata is cleared
// (migration and corruption safety: a corrupt record is ignored whole).
func (p *Panel) applySavedScreen(screen *terminal.SavedScreen) {
	p.savedCursor = terminal.SavedCursor{}
	p.savedAlt = false
	p.savedVisibleRows = 0
	if screen == nil || !screen.Valid() {
		return
	}
	lines := screen.Lines()
	history := screen.HistoryLines()
	if len(lines) == 0 || len(lines) > len(p.prior) {
		return
	}
	if len(history) > len(p.prior)-len(lines) {
		return
	}
	visTail := p.prior[len(p.prior)-len(lines):]
	for i := range lines {
		if lines[i].Text() != visTail[i].Text() {
			return
		}
	}
	histStart := len(p.prior) - len(lines) - len(history)
	for i := range history {
		if history[i].Text() != p.prior[histStart+i].Text() {
			return
		}
	}
	copy(visTail, lines)
	if len(history) > 0 {
		copy(p.prior[histStart:], history)
	}
	p.savedCursor = screen.Cursor
	p.savedAlt = screen.Alt
	p.savedVisibleRows = len(lines)
}

// Restored reports whether the dock shows a restored transcript without a shell.
func (p *Panel) Restored() bool { return p.restored }
