package dock

import (
	"strings"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"

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

// terminalExitMarker is Fresh's transcript line for a terminal whose process
// quit; a restart/resume continues below it.
const terminalExitMarker = "[Terminal process exited]"

// capturePrior appends the transcript of the shell the dock holds to the prior
// rows and trims the prior rows to the newest. A restarted shell keeps its
// transcript: the old session's history and screen are captured before the
// session closes and painted above the new shell's output. When the old shell had
// exited, the exit marker is appended after its last output (once), matching
// Fresh's backing-file order.
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
	if p.exitMarker {
		p.prior = appendExitMarker(p.prior)
		p.exitMarker = false
	}
	p.trimPrior()
}

// exitMarkerPending reports whether the exited shell's marker still needs its
// synthetic transcript row. When the shell exits, the marker is part of the
// scrollback transcript immediately (Fresh appends it to the backing file at the
// exit instant); until a restart captures it into the prior rows (or a restore
// brings it back as an ordinary row), it is appended virtually after the frozen
// screen so it is visible, copyable and saved exactly once.
func (p *Panel) exitMarkerPending() bool { return p.exited && p.exitMarker }

// exitMarkerRow is the absolute combined row of the pending exit marker: right
// after the frozen screen's real rows (the pager's screen window is trimmed at the
// marker, so no blank rows separate the marker from the last output). It is -1
// when no marker is pending.
func (p *Panel) exitMarkerRow() int {
	if !p.exitMarkerPending() {
		return -1
	}
	rows := p.scroll.screenRows
	if p.term != nil {
		rows = min(rows, p.term.Screen().Rows)
	}
	return len(p.prior) + p.scroll.total + rows
}

// appendExitMarker appends the exit marker line unless the transcript already
// ends with it, so a restore/restart cycle never writes it twice.
func appendExitMarker(lines []terminal.Line) []terminal.Line {
	if n := len(lines); n > 0 && lines[n-1].Text() == terminalExitMarker {
		return lines
	}
	return append(lines, lineFromText(terminalExitMarker))
}

// exitMarkerRows counts the trailing exit-marker rows of a restored transcript.
func exitMarkerRows(rows []string) int {
	n := 0
	for i := len(rows) - 1; i >= 0 && rows[i] == terminalExitMarker; i-- {
		n++
	}
	return n
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
			// The row indexes in a committed search are no longer valid after the
			// re-wrap; Sync (or cancelling an open search input) re-runs the query
			// over the new rows.
			if p.scroll.search != "" {
				p.scroll.searchStale = true
				p.scroll.matches, p.scroll.match = nil, 0
			}
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

// transcriptRows returns the bounded transcript window and the combined scrollback
// row (prior transcript, then history, then the live screen, then the exit marker) of
// its first row. SavedState persists these rows; Live Grep searches them and maps a
// result line back to a scrollback row, so both must use the same construction.
func (p *Panel) transcriptRows() (rows []string, first int) {
	add := func(text string, combined int) {
		if len(rows) == 0 {
			first = combined
		}
		rows = append(rows, cleanSavedText(text))
	}
	prior := len(p.prior)
	for i := range p.prior {
		add(p.prior[i].Text(), i)
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
				add(lines[i].Text(), prior+from+i)
			}
		}
	}
	screenRows := 0
	if p.term != nil {
		sc := p.term.Screen()
		screenRows = sc.Rows
		for r := 0; r < sc.Rows; r++ {
			add(screenRowText(sc, r), prior+total+r)
		}
	}
	// An exited-but-not-restarted shell saves Fresh's marker after its frozen
	// screen; a restored transcript already carries its marker in the prior rows.
	if p.exited && p.exitMarker {
		if n := len(rows); n > 0 && rows[n-1] != terminalExitMarker {
			add(terminalExitMarker, prior+total+screenRows)
		}
	}
	// Bound to the newest rows and bytes, adjusting First by the dropped prefix.
	if drop := len(rows) - maxSavedRows; drop > 0 {
		rows = rows[drop:]
		first += drop
	}
	size := 0
	drop := 0
	for i := len(rows) - 1; i >= 0; i-- {
		size += len(rows[i]) + 1
		if size > maxSavedBytes {
			drop = i + 1
			break
		}
	}
	if drop > 0 {
		rows = rows[drop:]
		first += drop
	}
	return rows, first
}

// RevealTranscriptRow opens the read-only scrollback pager and selects the hit's
// combined transcript row. Snapshot hits are validated as a whole: an unchanged
// scrollback total with the expected line and known neighbours at the recorded row is
// exact; otherwise exactly one current row must match the expected text (including an
// empty blank line) and the known neighbours, which is the relocation case. Missing,
// truncated, ambiguous or invalid snapshots refuse (false) so the pager never lands on
// a wrong row. A legacy Snapshot=false hit uses the row as given. It reports false when
// there is no pager (restored no-session tab, running alternate-screen program, empty
// dock) and starts no process; the returned command fetches the pager window.
func (p *Panel) RevealTranscriptRow(h TranscriptHit) (tea.Cmd, bool) {
	if p.term == nil {
		return nil, false
	}
	target := h.Row
	if h.Snapshot {
		if !h.Valid {
			return nil, false
		}
		lines, first := p.transcriptRows()
		current := p.term.ScrollbackLen()
		match := func(i int) bool {
			if lines[i] != h.Text {
				return false
			}
			if h.BeforeKnown && (i == 0 || lines[i-1] != h.Before) {
				return false
			}
			if h.AfterKnown && (i+1 >= len(lines) || lines[i+1] != h.After) {
				return false
			}
			return true
		}
		// Collect every row that matches the expected text and known neighbours. Even
		// when the scrollback total is unchanged and the recorded row matches, a second
		// equally matching row means the snapshot is ambiguous (duplicate blank/output
		// lines); refuse instead of trusting the stale absolute row.
		found := -1
		for i := range lines {
			if !match(i) {
				continue
			}
			if found >= 0 {
				return nil, false
			}
			found = i
		}
		if found < 0 {
			return nil, false
		}
		if h.Total == current && h.Row >= first && h.Row < first+len(lines) && found == h.Row-first {
			target = h.Row
		} else {
			target = first + found
		}
	}
	var cmd tea.Cmd
	if !p.scroll.on {
		cmd = p.enterScrollback()
		if !p.scroll.on {
			return cmd, false
		}
	}
	combined := p.combined()
	if combined <= 0 {
		return cmd, false
	}
	p.scroll.selected = min(max(target, 0), combined-1)
	p.revealScroll()
	return tea.Batch(cmd, p.requestScrollback()), true
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
	// into a dead one. A dive clears wasLive, so an attempted revive is recorded as
	// the resulting state.
	st.Live = (p.term != nil && !p.exited) || p.wasLive
	// The terminal title the child set (OSC 0/2), persisted so a restored tab keeps
	// its name and label; a restored-but-not-revived tab re-saves the recorded one.
	st.Title = p.TitleText()
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
	// The saved cwd is untrusted input too (OSC 7 today, a hand-edited record
	// tomorrow): clean its controls and bound it like the launch directory.
	st.Cwd = clipSavedDir(cleanSavedText(st.Cwd))
	st.Rows, _ = p.transcriptRows()
	if p.exited {
		st.ExitCode, st.ExitKnown = p.exitCode, p.exitKnown
	}
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
	// The exit marker line sits after the frozen screen, so it is not part of the
	// visible-screen snapshot tail: hold the trailing marker(s) aside while the
	// styled snapshot is matched against the rows, then put them back after it.
	markers := exitMarkerRows(rows)
	p.prior = p.prior[:len(p.prior)-markers]
	// The styled snapshot (history + visible screen + cursor + alt) replaces the
	// text tail it was captured from, but only when every row matches exactly; a
	// mismatched/corrupt snapshot is ignored whole so a restore never paints
	// different content than the transcript rows.
	p.applySavedScreen(st.Screen)
	for range markers {
		p.prior = append(p.prior, lineFromText(terminalExitMarker))
	}
	p.exitCode, p.exitKnown = st.ExitCode, st.ExitKnown
	p.exitMarker = false
	p.titleText = terminal.CleanTitle(st.Title)
	p.restored = true
	p.wasLive = st.Live
	p.reflowed = false
	p.priorWidth = st.Width
	p.open = st.Open || len(p.prior) > 0
	p.focus = false
	p.dir = clipSavedDir(cleanSavedText(st.Cwd))
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
