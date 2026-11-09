package dock

import (
	"regexp"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/i18n"
	"github.com/tbxark/gvim/internal/terminal"
)

// The notices of the read-only scrollback view.
// The read-only view's notices, translated at display time.

func altScrollbackNotice() string { return i18n.T("", "gvim.scrollback.alt_screen", nil) }
func nothingToCopyNotice() string { return i18n.T("", "gvim.scrollback.nothing_to_copy", nil) }
func copiedLineNotice() string    { return i18n.T("", "gvim.scrollback.copied_line", nil) }
func copiedScreenNotice() string  { return i18n.T("", "gvim.scrollback.copied_screen", nil) }
func searchNoMatchNotice() string { return i18n.T("", "gvim.scrollback.no_match", nil) }

// scrollMargin is the extra rows a snapshot asks for around the window, so a
// small scroll does not need another fetch.
const scrollMargin = 8

// scrollState is the dock's read-only scrollback view: a window over the shell
// screen's history followed by the live screen rows, the selected row, the search
// and the last snapshot the session sent.
type scrollState struct {
	on bool
	// top is the first visible combined row: history rows first, then the live
	// screen rows. selected is the row y copies; -1 before one is chosen.
	top, selected int
	// total is the number of history rows in the last snapshot, and lines are
	// the history rows [from, from+len(lines)).
	total int
	lines []terminal.Line
	from  int
	// gen is the shell generation the snapshot belongs to.
	gen int
	// pending is a snapshot request in flight.
	pending bool
	// typing says the search input has the keys; search is the committed query,
	// and regex/matchCase are its options.
	typing      bool
	searchInput string
	search      string
	regex       bool
	matchCase   bool
	// matches are the absolute combined rows that match search; match is the
	// index into them the view is on.
	matches []int
	match   int
	// searchSeq orders the asynchronous searches: a reply from an older keystroke
	// is dropped, so typing can never be overwritten by a late answer.
	searchSeq int
	// preview holds the matches of the in-progress incremental search (the typing
	// input). It is separate from the committed matches, so Esc can drop the
	// preview and return to the committed search and its view.
	preview      []int
	previewQuery string
	previewIdx   int
	// previewStep is a next/previous request that arrived before the answer for
	// the current query: the answer then lands on its first (or last) match.
	previewStep int
	// searchSaved/searchSavedTop/searchSavedSelected remember the view before the
	// search input opened; Esc restores it (Fresh cancels an incremental search
	// without moving the view).
	searchSaved         bool
	searchSavedTop      int
	searchSavedSelected int
	// searchStale marks a committed search whose match rows no longer hold because
	// the transcript was re-wrapped at a new width. Sync re-runs it in a command
	// (never in Update/Draw) so a restored transcript keeps its search after a
	// resize.
	searchStale bool
	// screenRows is the number of live screen rows, from the last snapshot.
	screenRows int
}

// scrollbackMsg carries the history snapshot the dock asked for.
type scrollbackMsg struct {
	gen   int
	from  int
	lines []terminal.Line
	total int
}

// searchMsg carries the matches of a scrollback search.
type searchMsg struct {
	gen     int
	query   string
	seq     int  // the request this answers; an older seq is dropped
	commit  bool // false = incremental preview while typing
	matches []int
	err     string
}

// Scrolling reports whether the dock shows the read-only scrollback view.
func (p *Panel) Scrolling() bool { return p.scroll.on }

// ScrollTop is the first visible combined row of the scrollback view.
func (p *Panel) ScrollTop() int { return p.scroll.top }

// ScrollSelected is the combined row y copies, or -1.
func (p *Panel) ScrollSelected() int { return p.scroll.selected }

// SearchQuery is the committed scrollback search, or "".
func (p *Panel) SearchQuery() string { return p.scroll.search }

// SearchMatches is the number of rows the scrollback search matches.
func (p *Panel) SearchMatches() int { return len(p.scroll.matches) }

// bodyRows is the number of shell rows the layout gives the dock.
func (p *Panel) bodyRows() int {
	_, rows := p.contentSize()
	return max(rows, 0)
}

// combined is the rows the scrollback view scrolls through: a restarted shell's
// prior transcript, then the history, then the live screen rows.
func (p *Panel) combined() int {
	if marker := p.exitMarkerRow(); marker >= 0 {
		// The marker follows the frozen screen's real rows; rows the pager window
		// keeps below it are not reachable while the marker is the transcript end.
		return marker + 1
	}
	return len(p.prior) + p.scroll.total + p.scroll.screenRows
}

// maxTop is the largest first visible row the view can have.
func (p *Panel) maxTop() int { return max(p.combined()-p.bodyRows(), 0) }

// enterScrollback switches the dock to the read-only scrollback view, if the shell
// has a main screen with history and the dock has rows to show. The window starts
// at the newest rows.
func (p *Panel) enterScrollback() tea.Cmd {
	if p.term == nil {
		p.noticeOf(NoTerminalNotice())
		return nil
	}
	if p.term.AltScreen() && !p.exitMarkerPending() {
		// A running full-screen program's own drawing has no scrollback; only an
		// EXITED alternate screen can be shown as the frozen-frame transcript (the
		// visible rows plus the exit marker, with no history).
		p.noticeOf(altScrollbackNotice())
		return nil
	}
	if p.scroll.on {
		return nil
	}
	p.ClearHover()
	total := p.term.ScrollbackLen()
	rows := p.bodyRows()
	p.scroll = scrollState{
		on: true, total: total, screenRows: rows, gen: p.gen,
	}
	p.scroll.top = max(p.combined()-rows, 0)
	p.scroll.selected = max(p.combined()-1, 0)
	p.clearSelection()
	return p.requestScrollback()
}

// maybeShowExitScrollback opens the read-only transcript of an exited shell the
// first time the dock can lay it out, so the frozen output and Fresh's exit marker
// are what the user sees (Fresh turns an exited terminal into read-only
// scrollback). A pending auto-view that cannot be shown yet (hidden dock without
// rows) waits for the next Sync/Show. An alternate-screen exit has no pager, so
// its frozen frame stays and the marker remains in the saved transcript.
func (p *Panel) maybeShowExitScrollback() tea.Cmd {
	if !p.autoExitScroll || p.term == nil || p.scroll.on || p.restored {
		return nil
	}
	if p.bodyRows() <= 0 {
		return nil
	}
	p.autoExitScroll = false
	return p.enterScrollback()
}

// leaveScrollback returns to the live screen.
func (p *Panel) leaveScrollback() tea.Cmd {
	p.scroll = scrollState{}
	p.autoExitScroll = false
	p.clearSelection()
	return nil
}

// ToggleScrollback is the Ctrl+Space switch: it enters the scrollback view, or
// returns to the live screen.
func (p *Panel) ToggleScrollback() tea.Cmd {
	if p.scroll.on {
		return p.leaveScrollback()
	}
	return p.enterScrollback()
}

// covers reports whether the snapshot holds rows [from, to).
func (p *Panel) covers(from, to int) bool {
	return from >= p.scroll.from && to <= p.scroll.from+len(p.scroll.lines)
}

// requestScrollback starts a fetch of the rows the window shows when the snapshot
// does not hold them. The request runs in a tea.Cmd; Update never waits for the
// session.
func (p *Panel) requestScrollback() tea.Cmd {
	if !p.scroll.on || p.term == nil || p.scroll.pending {
		return nil
	}
	rows := max(p.bodyRows(), 1)
	// The window is in combined rows; the history part starts after the prior
	// transcript.
	prior := len(p.prior)
	histFrom := min(max(p.scroll.top-prior, 0), p.scroll.total)
	histTo := min(max(p.scroll.top+rows-prior, 0), p.scroll.total)
	if histTo <= histFrom || p.covers(histFrom, histTo) {
		return nil
	}
	from := max(histFrom-scrollMargin, 0)
	to := min(histTo+scrollMargin, p.scroll.total)
	t, gen := p.term, p.gen
	p.scroll.pending = true
	count := to - from
	return func() tea.Msg {
		lines, total := t.Scrollback(from, count)
		return scrollbackMsg{gen: gen, from: from, lines: lines, total: total}
	}
}

// applyScrollback stores a snapshot. It reports whether it belongs to the shell
// the dock shows now.
func (p *Panel) applyScrollback(msg scrollbackMsg) bool {
	if msg.gen != p.gen || !p.scroll.on {
		return false
	}
	p.scroll.pending = false
	p.scroll.from, p.scroll.lines, p.scroll.total = msg.from, msg.lines, msg.total
	p.scroll.screenRows = p.bodyRows()
	p.clampScroll()
	return true
}

// clampScroll keeps the window and the selected row inside the view.
func (p *Panel) clampScroll() {
	combined := p.combined()
	if combined <= 0 {
		p.scroll.top, p.scroll.selected = 0, -1
		return
	}
	p.scroll.top = min(max(p.scroll.top, 0), p.maxTop())
	if p.scroll.selected < 0 || p.scroll.selected >= combined {
		p.scroll.selected = min(p.scroll.top, combined-1)
	}
}

// revealScroll moves the window so the selected row is inside it.
func (p *Panel) revealScroll() {
	if !p.scroll.on || p.scroll.selected < 0 {
		return
	}
	rows := max(p.bodyRows(), 1)
	switch {
	case p.scroll.selected < p.scroll.top:
		p.scroll.top = p.scroll.selected
	case p.scroll.selected >= p.scroll.top+rows:
		p.scroll.top = p.scroll.selected - rows + 1
	}
	p.scroll.top = min(max(p.scroll.top, 0), p.maxTop())
}

// scrollBy moves the selected row by delta and reveals it.
func (p *Panel) scrollBy(delta int) {
	if !p.scroll.on {
		return
	}
	combined := p.combined()
	if combined == 0 {
		return
	}
	p.scroll.selected = min(max(p.scroll.selected+delta, 0), combined-1)
	p.revealScroll()
}

// afterScroll is the command a scroll key leaves behind: the snapshot fetch for
// the rows that came into view.
func (p *Panel) afterScroll() tea.Cmd { return p.requestScrollback() }

// ScrollKey handles a key while the dock shows the scrollback view, and the
// Ctrl+Space that enters it from the live screen. It reports whether it took the
// key; the caller sends anything else to the shell.
func (p *Panel) ScrollKey(k tea.Key) (tea.Cmd, bool) {
	if !p.scroll.on {
		switch {
		case isCtrlSpace(k), k.Code == tea.KeyPgUp && k.Mod&tea.ModShift != 0:
			// Ctrl+Space toggles the view on the main screen, and Shift+PageUp
			// enters it as Fresh does.
			return p.enterScrollback(), true
		case k.Mod&tea.ModCtrl != 0 && k.Code == 'f':
			cmd := p.enterScrollback()
			if p.scroll.on {
				p.startSearchInput()
			}
			return cmd, true
		}
		return nil, false
	}
	if p.scroll.typing {
		return p.searchInputKey(k), true
	}
	switch {
	case isCtrlSpace(k):
		if p.restored && p.term == nil {
			return nil, true
		}
		return p.leaveScrollback(), true
	case k.Code == tea.KeyEscape:
		if p.restored && p.term == nil {
			return nil, true
		}
		return p.leaveScrollback(), true
	case k.Mod == 0 && k.Code == '/':
		p.startSearchInput()
		return nil, true
	case k.Mod == 0 && k.Code == 'n':
		return p.nextMatch(1), true
	case k.Mod == 0 && k.Code == 'N':
		return p.nextMatch(-1), true
	case k.Mod == 0 && k.Code == 'y':
		if p.sel.active {
			return p.copySelection(), true
		}
		return p.copySelected(), true
	case k.Mod&tea.ModCtrl != 0 && k.Code == 'c':
		if p.sel.active {
			return p.copySelection(), true
		}
		return p.copyScreen(), true
	case k.Mod&tea.ModCtrl != 0 && k.Code == 'f':
		p.startSearchInput()
		return nil, true
	case k.Code == tea.KeyUp:
		p.scrollBy(-1)
		return p.afterScroll(), true
	case k.Code == tea.KeyDown:
		p.scrollBy(1)
		return p.afterScroll(), true
	case k.Code == tea.KeyPgUp:
		p.scrollBy(-max(p.bodyRows(), 1))
		return p.afterScroll(), true
	case k.Code == tea.KeyPgDown:
		p.scrollBy(max(p.bodyRows(), 1))
		return p.afterScroll(), true
	case k.Code == tea.KeyHome:
		p.scroll.selected = 0
		p.scroll.top = 0
		return p.afterScroll(), true
	case k.Code == tea.KeyEnd:
		p.scroll.selected = max(p.combined()-1, 0)
		p.revealScroll()
		return p.afterScroll(), true
	}
	return nil, true
}

// isCtrlSpace reports the Ctrl+Space chord.
func isCtrlSpace(k tea.Key) bool {
	return k.Mod&tea.ModCtrl != 0 && (k.Code == tea.KeySpace || k.Code == ' ')
}

// searchInputKey edits the search input line.
// startSearchInput opens the search input: a fresh query, an ordered preview, and
// the view saved for Esc (an incremental search previews matches while typing but
// cancelling restores the previous view and committed search).
func (p *Panel) startSearchInput() {
	p.scroll.typing = true
	p.scroll.searchInput = ""
	p.scroll.preview = nil
	p.scroll.previewQuery = ""
	p.scroll.previewIdx = 0
	p.scroll.previewStep = 0
	p.scroll.searchSaved = true
	p.scroll.searchSavedTop = p.scroll.top
	p.scroll.searchSavedSelected = p.scroll.selected
	p.scroll.searchSeq++
}

// incrementalSearchCmd runs the in-progress query asynchronously and returns one
// ordered command. An empty input cancels pending previews and jumps nothing; a
// late answer for an older input is dropped by applySearch (searchSeq).
func (p *Panel) incrementalSearchCmd() tea.Cmd {
	query := strings.TrimSpace(p.scroll.searchInput)
	p.scroll.previewStep = 0
	if query == "" {
		p.scroll.searchSeq++
		p.scroll.preview = nil
		p.scroll.previewQuery = ""
		p.scroll.previewIdx = 0
		return nil
	}
	return p.searchCommand(query, p.scroll.regex, p.scroll.matchCase, false)
}

// previewStepCmd is F3/Shift+F3 while the incremental search input is open: step
// the preview through the current query's matches without closing the input (Fresh
// keeps the search prompt open and binds F3/Shift+F3 to find_next/find_previous
// while it is up). When the answer for the current query has not arrived yet, the
// request is ordered and the answer lands on the first (or last) match, like the
// first press of a fresh query in Fresh.
func (p *Panel) previewStepCmd(dir int) tea.Cmd {
	query := strings.TrimSpace(p.scroll.searchInput)
	if query == "" {
		return nil
	}
	if p.scroll.previewQuery == query && len(p.scroll.preview) > 0 {
		n := len(p.scroll.preview)
		p.scroll.previewIdx = (p.scroll.previewIdx + dir + n) % n
		p.jumpToRow(p.scroll.preview[p.scroll.previewIdx])
		return p.requestScrollback()
	}
	p.scroll.previewStep = dir
	return p.searchCommand(query, p.scroll.regex, p.scroll.matchCase, false)
}

func (p *Panel) searchInputKey(k tea.Key) tea.Cmd {
	switch {
	case k.Code == tea.KeyF3:
		dir := 1
		if k.Mod&tea.ModShift != 0 {
			dir = -1
		}
		return p.previewStepCmd(dir)
	case k.Code == tea.KeyEscape:
		p.scroll.typing, p.scroll.searchInput = false, ""
		p.scroll.preview = nil
		p.scroll.previewQuery, p.scroll.previewIdx, p.scroll.previewStep = "", 0, 0
		p.scroll.searchSeq++ // pending previews must not land after the cancel
		if p.scroll.searchSaved {
			p.scroll.top = p.scroll.searchSavedTop
			p.scroll.selected = p.scroll.searchSavedSelected
			p.scroll.searchSaved = false
			p.clampScroll()
		}
		// Cancelling the input can reveal that the committed search is stale after a
		// re-wrap that happened while the input was open.
		return p.takeStaleSearch()
	case k.Code == tea.KeyEnter:
		p.scroll.typing = false
		p.scroll.preview = nil
		p.scroll.previewQuery, p.scroll.previewIdx, p.scroll.previewStep = "", 0, 0
		p.scroll.searchSaved = false
		p.scroll.search = strings.TrimSpace(p.scroll.searchInput)
		if p.scroll.search == "" {
			p.scroll.matches, p.scroll.match = nil, 0
			return nil
		}
		return p.searchCmd(p.scroll.search, p.scroll.regex, p.scroll.matchCase)
	case k.Mod&tea.ModCtrl != 0 && k.Code == 'r':
		p.scroll.regex = !p.scroll.regex
		return p.incrementalSearchCmd()
	case k.Mod&tea.ModCtrl != 0 && k.Code == 'a':
		p.scroll.matchCase = !p.scroll.matchCase
		return p.incrementalSearchCmd()
	case k.Code == tea.KeyBackspace:
		if r := []rune(p.scroll.searchInput); len(r) > 0 {
			p.scroll.searchInput = string(r[:len(r)-1])
		}
		return p.incrementalSearchCmd()
	case k.Mod == 0 && k.Text != "":
		p.scroll.searchInput += k.Text
		return p.incrementalSearchCmd()
	}
	return nil
}

// takeStaleSearch rebuilds a committed search after the transcript was re-wrapped.
// It returns nil when nothing is stale, no query is committed, or the search input
// is open (the flag is kept for the commit/cancel). It is called from Sync and from
// cancelling the search input, never from Update's normal path or Draw; the search
// itself runs in a tea.Cmd.
func (p *Panel) takeStaleSearch() tea.Cmd {
	if !p.scroll.searchStale || p.scroll.typing {
		return nil
	}
	p.scroll.searchStale = false
	if !p.scroll.on || p.scroll.search == "" {
		return nil
	}
	p.scroll.matches, p.scroll.match = nil, 0
	return p.refreshSearch()
}

// refreshSearch runs the committed search again after an option toggle, when a
// query is committed.
func (p *Panel) refreshSearch() tea.Cmd {
	if p.scroll.search == "" || p.scroll.typing {
		return nil
	}
	return p.searchCmd(p.scroll.search, p.scroll.regex, p.scroll.matchCase)
}

// searchCmd runs one committed search (the code path refreshSearch and a commit
// use). The asynchronous request is ordered like an incremental preview.
func (p *Panel) searchCmd(query string, regex, matchCase bool) tea.Cmd {
	return p.searchCommand(query, regex, matchCase, true)
}

// searchCommand searches the prior transcript, the history and the live screen for
// query, in a command, and tags the answer with a fresh request sequence. commit
// says whether the answer redefines the committed search or is an incremental
// preview while the user types.
func (p *Panel) searchCommand(query string, regex, matchCase, commit bool) tea.Cmd {
	p.scroll.searchSeq++
	seq := p.scroll.searchSeq
	match, err := matcher(query, regex, matchCase)
	if err != nil {
		return func() tea.Msg { return searchMsg{gen: p.gen, query: query, seq: seq, commit: commit, err: err.Error()} }
	}
	t, gen := p.term, p.gen
	prior := slices.Clone(p.prior)
	total := p.scroll.total
	rows := p.scroll.screenRows
	return func() tea.Msg {
		var matches []int
		// The prior transcript comes first, from the dock's own copy.
		for i, ln := range prior {
			if match(ln.Text()) {
				matches = append(matches, i)
			}
		}
		if t != nil {
			found, _, err := t.SearchScrollbackMode(query, regex, matchCase, 200)
			if err != nil {
				return searchMsg{gen: gen, query: query, seq: seq, commit: commit, err: err.Error()}
			}
			for _, i := range found {
				matches = append(matches, len(prior)+i)
			}
			sc := t.Screen()
			for r := 0; r < min(sc.Rows, rows); r++ {
				if match(screenRowText(sc, r)) {
					matches = append(matches, len(prior)+total+r)
				}
			}
		}
		return searchMsg{gen: gen, query: query, seq: seq, commit: commit, matches: matches}
	}
}

// matcher returns the test a scrollback search applies to one row's text.
func matcher(query string, regex, matchCase bool) (func(string) bool, error) {
	query = strings.TrimSpace(query)
	if regex {
		if !matchCase {
			query = "(?i)" + query
		}
		re, err := regexp.Compile(query)
		if err != nil {
			return nil, err
		}
		return re.MatchString, nil
	}
	if !matchCase {
		query = strings.ToLower(query)
	}
	return func(text string) bool {
		if !matchCase {
			text = strings.ToLower(text)
		}
		return strings.Contains(text, query)
	}, nil
}

// applySearch stores the matches of a search and jumps to the first one. An answer
// whose request sequence is not the newest is dropped: fast typing and a commit can
// have several searches in flight, and only the latest may win. An incremental
// preview jumps the view but never replaces the committed search/matches; a commit
// replaces them and clears the preview.
func (p *Panel) applySearch(msg searchMsg) tea.Cmd {
	if msg.gen != p.gen || !p.scroll.on || msg.seq != p.scroll.searchSeq {
		return nil
	}
	if msg.err != "" {
		p.noticeOf(dockTextVars("gvim.dock.bad_pattern", "Bad pattern: %{error}", map[string]any{"error": msg.err}))
		return nil
	}
	if !msg.commit {
		p.scroll.preview = msg.matches
		p.scroll.previewQuery = msg.query
		idx := 0
		if p.scroll.previewStep < 0 && len(msg.matches) > 0 {
			idx = len(msg.matches) - 1
		}
		p.scroll.previewStep = 0
		p.scroll.previewIdx = idx
		if len(msg.matches) > 0 {
			p.jumpToRow(msg.matches[idx])
		}
		return p.requestScrollback()
	}
	p.scroll.preview = nil
	p.scroll.matches, p.scroll.match = msg.matches, 0
	if len(msg.matches) == 0 {
		p.noticeOf(searchNoMatchNotice())
		return nil
	}
	p.jumpToMatch(0)
	return p.requestScrollback()
}

// jumpToRow centres the view on one absolute row (an incremental preview match),
// without touching the committed match list.
func (p *Panel) jumpToRow(abs int) {
	if abs < 0 || abs >= p.combined() {
		return
	}
	p.scroll.selected = abs
	p.scroll.top = max(abs-max(p.bodyRows(), 1)/2, 0)
	p.revealScroll()
}

// nextMatch moves to the next (dir 1) or previous (-1) match.
func (p *Panel) nextMatch(dir int) tea.Cmd {
	n := len(p.scroll.matches)
	if n == 0 {
		return nil
	}
	p.jumpToMatch((p.scroll.match + dir + n) % n)
	return p.requestScrollback()
}

// jumpToMatch makes match i the selected row, roughly centred.
func (p *Panel) jumpToMatch(i int) {
	if i < 0 || i >= len(p.scroll.matches) {
		return
	}
	p.scroll.match = i
	p.scroll.selected = p.scroll.matches[i]
	p.scroll.top = max(p.scroll.selected-max(p.bodyRows(), 1)/2, 0)
	p.revealScroll()
}

// combinedText returns the text of a combined row: a history row from the
// snapshot, or a live screen row. ok is false when the row is not loaded yet.
func (p *Panel) combinedText(abs int) (string, bool) {
	if abs < 0 || abs >= p.combined() {
		return "", false
	}
	if abs == p.exitMarkerRow() {
		return terminalExitMarker, true
	}
	if abs < len(p.prior) {
		return p.prior[abs].Text(), true
	}
	hist := abs - len(p.prior)
	if hist < p.scroll.total {
		i := hist - p.scroll.from
		if i < 0 || i >= len(p.scroll.lines) {
			return "", false
		}
		return p.scroll.lines[i].Text(), true
	}
	if p.term == nil {
		return "", false
	}
	sc := p.term.Screen()
	r := hist - p.scroll.total
	if r < 0 || r >= sc.Rows {
		return "", false
	}
	return screenRowText(sc, r), true
}

// copySelected copies the selected row through the host's clipboard path.
func (p *Panel) copySelected() tea.Cmd {
	text, ok := p.combinedText(p.scroll.selected)
	if !ok || text == "" {
		p.noticeOf(nothingToCopyNotice())
		return nil
	}
	if p.host.Copy == nil {
		p.noticeOf(noClipboardNotice())
		return nil
	}
	p.noticeOf(copiedLineNotice())
	return p.host.Copy(text)
}

// copyScreen copies the visible rows of the scrollback view.
func (p *Panel) copyScreen() tea.Cmd {
	if p.term == nil {
		p.noticeOf(NoTerminalNotice())
		return nil
	}
	if p.host.Copy == nil {
		p.noticeOf(noClipboardNotice())
		return nil
	}
	rows := max(p.bodyRows(), 1)
	var lines []string
	for abs := p.scroll.top; abs < p.scroll.top+rows; abs++ {
		if abs >= p.combined() {
			break
		}
		text, ok := p.combinedText(abs)
		if !ok {
			continue
		}
		lines = append(lines, text)
	}
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) == 0 {
		p.noticeOf(nothingToCopyNotice())
		return nil
	}
	p.noticeOf(copiedScreenNotice())
	return p.host.Copy(strings.Join(lines, "\n"))
}

// screenRowText is the text of row r of a screen, trailing blanks trimmed.
func screenRowText(sc terminal.Screen, r int) string {
	var b strings.Builder
	for c := 0; c < sc.Cols; c++ {
		ch := sc.Cell(r, c).Rune
		if ch == 0 {
			b.WriteByte(' ')
			continue
		}
		b.WriteRune(ch)
	}
	return strings.TrimRight(b.String(), " ")
}
