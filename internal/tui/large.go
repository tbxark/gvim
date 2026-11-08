package tui

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"strconv"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/internal/tui/editorpane"
	"github.com/tbxark/gvim/internal/tui/find"
	"github.com/tbxark/gvim/internal/tui/prompt"
	"github.com/tbxark/gvim/internal/tui/style"
	"github.com/tbxark/gvim/search"
)

// Large file mode in the root. A document whose text stays on disk builds its line
// index in the background, is searched in a stream, and is saved by a write that
// can take a long time. The root runs that work in commands and keeps the state it
// needs to follow them; the editor does the work itself. Everything here is optional:
// an editor without these ports behaves as before.

// indexer is the editor's part in building the line index of large documents. The
// root asks PlanIndex for the next step after every update, runs the step in a
// command, and gives the result back to ApplyIndex. The app adapter implements it
// over *editor.Editor.
type indexer interface {
	PlanIndex() (editor.IndexJob, bool)
	ApplyIndex(r editor.IndexResult) bool
}

// streamSearcher is the search surface of the editor over a large active document.
// The matches of such a document are not listed: a scan counts them in slices of
// time, FindNext goes to one match, and VisibleMatches gives the matches on the rows
// that are painted. The app adapter implements it.
type streamSearcher interface {
	// SearchKey identifies the text a scan would search: the active document, its
	// version, and whether its line index is complete. ok is false when the active
	// document is not large, or there is none.
	SearchKey() (doc any, version uint64, indexComplete bool, ok bool)
	// StartScan starts a scan of the active document for q from line startLine. The
	// scan holds a snapshot of the text, so it is not changed by later edits. The
	// caller closes it.
	StartScan(q search.Query, startLine int) (*search.Scan, error)
	// FindNext returns the match to go to from the position from: the first match
	// that starts at or after it, or with backward the last one that starts before
	// it. scanned is false when the search ran out of its budget before it knew;
	// next then says where it stopped, and passing it back as at goes on from there.
	FindNext(q search.Query, from buffer.Pos, backward bool, at search.Cursor) (m search.Match, found, scanned bool, next search.Cursor, err error)
	// VisibleMatches returns the matches that touch lines first through last.
	VisibleMatches(q search.Query, first, last int) ([]search.Match, error)
}

// fullLoader is the editor's whole-file load of a file that large file mode cannot
// open lazily: a file in another encoding than UTF-8. The app adapter implements it.
type fullLoader interface {
	LoadFullFunc(path string) func() (*editor.Document, error)
	OpenLoaded(d *editor.Document)
}

// pathClassifier is the optional editor view that says whether a path would open in large
// file mode, from a stat. With gotoEditor it lets the root read such a file in a command.
type pathClassifier interface {
	IsLargePath(path string) bool
}

// ctxSaver is the part of the editor's save that a command can stop. The write of a
// large document takes a long time, and a quit must not wait for it. An editor without
// it writes through asyncSaver.WriteSave, which cannot be stopped.
type ctxSaver interface {
	WriteSaveContext(ctx context.Context, p editor.SavePlan) []editor.SaveResult
}

// The messages of large file mode.
type (
	// indexMsg is the result of one step of the line index of a large document.
	indexMsg struct {
		r   editor.IndexResult
		gen uint64
	}
	// searchStepMsg is the end of one time slice of a scan.
	searchStepMsg struct{ run *searchRun }
	// fullLoadPromptMsg asks the root to ask whether to load a file whole, because
	// it is not UTF-8 and so cannot be opened lazily.
	fullLoadPromptMsg struct{ err editor.ErrLargeNeedsFullLoad }
	// fullLoadedMsg is the result of loading such a file whole.
	fullLoadedMsg struct {
		doc *editor.Document
		err error
	}
	// openLoadedMsg is a large file that a command read to open in a tab; line and
	// col, when line is set, are where the caret goes.
	openLoadedMsg struct {
		doc       *editor.Document
		err       error
		path      string
		line, col int
	}
	// quitWaitMsg ends the wait of a quit for the saves it cancelled.
	quitWaitMsg struct{}
)

const (
	// scanSlice is the time one command spends searching, as Fresh slices its scans.
	scanSlice = 8 * time.Millisecond
	// quitWait is how long a quit waits for a cancelled save to remove its temporary
	// file.
	quitWait = 2 * time.Second
	// savingNotice is the notice row while a large document is being written.
	savingNotice = "Saving..."
)

// largeState is the state of the root for large documents.
type largeState struct {
	// ctx stops the background work of the root when the model is closed. gen counts
	// the times it was stopped, so a result that was started before is dropped.
	ctx    context.Context
	cancel context.CancelFunc
	gen    uint64
	// indexBusy is set while a command runs a step of the line index. One runs at a
	// time, whichever document it is for.
	indexBusy bool
	// stream is the scan that counts the matches for the find bar, or nil.
	stream *searchRun
	// nav is where the last F3, Enter or Shift+F3 search ran out of its budget, or nil.
	nav *navResume
	// saves are the writes in flight, by number.
	saves    map[int]*saveRun
	nextSave int
	quitting bool // a quit waits for the cancelled saves
	// quitAfterSave is set by a quit that found a write in flight: the tab it would ask
	// about is being saved, so the quit waits for the write and asks again if needed.
	quitAfterSave bool
	// quitStash writes the unsaved edits of large documents before a quit, or nil.
	quitStash func() error
}

// context returns the context of the root's background work, and makes it on first use.
func (s *largeState) context() context.Context {
	if s.ctx == nil {
		s.ctx, s.cancel = context.WithCancel(context.Background())
	}
	return s.ctx
}

// stop ends the background work: the commands in flight stop at their next step, and
// their results are dropped.
func (s *largeState) stop() {
	if s.cancel != nil {
		s.cancel()
	}
	s.ctx, s.cancel = nil, nil
	s.gen++
	s.indexBusy = false
}

// isLargeDoc reports whether doc is in large file mode.
func isLargeDoc(doc Document) bool {
	ld, ok := doc.(LargeDoc)
	return ok && ld.Large()
}

// displayLines is the line count that sizes the gutter and the scrollbar thumb of
// doc. See editorpane.DisplayLines. The layout, the status row and the pane all call
// it, so the hit map and the painting use the same gutter.
func displayLines(doc Document) int {
	return editorpane.DisplayLines(doc)
}

// indexSync starts the next step of the line index, when a large document needs one
// and no step is running. The root calls it after every update, so the chain starts
// when a document opens, goes on after each result, and starts again when a tab
// with an incomplete index becomes active or the scan_line_index action cleared an
// error.
func (m *Model) indexSync() tea.Cmd {
	if m.large.indexBusy {
		return nil
	}
	ix, ok := m.ed.(indexer)
	if !ok {
		return nil
	}
	job, ok := ix.PlanIndex()
	if !ok {
		return nil
	}
	m.large.indexBusy = true
	ctx, gen := m.large.context(), m.large.gen
	return func() tea.Msg {
		return indexMsg{r: job.Run(ctx), gen: gen}
	}
}

// indexDone applies one step of the line index. A result from before the work was
// stopped is dropped, and so is one for a document that was closed meanwhile: the
// editor checks that. The next step is started by indexSync, after the update.
func (m *Model) indexDone(msg indexMsg) {
	if msg.gen != m.large.gen {
		return
	}
	m.large.indexBusy = false
	if ix, ok := m.ed.(indexer); ok {
		ix.ApplyIndex(msg.r)
	}
}

// scanProgress is the status of a line index that is being built, "Scanning... 43%",
// as the editor words it.
func scanProgress(info buffer.IndexInfo) string {
	pct := 0
	if info.Size > 0 {
		pct = int(min(info.Indexed*100/info.Size, 99))
	}
	return "Scanning... " + strconv.Itoa(pct) + "%"
}

// largeLabel is the status row chip of a large document, "LARGE", and while its line
// index is built the progress after it. It is empty for a document that is not large.
func largeLabel(doc Document) string {
	ld, ok := doc.(LargeDoc)
	if !ok || !ld.Large() {
		return ""
	}
	if info := ld.Index(); !info.Complete {
		return "LARGE " + scanProgress(info)
	}
	return "LARGE"
}

// largeMessage handles the messages of large file mode. It reports whether msg was one.
func (m *Model) largeMessage(msg tea.Msg) (tea.Cmd, bool) {
	switch msg := msg.(type) {
	case indexMsg:
		m.indexDone(msg)
		return nil, true
	case searchStepMsg:
		return m.searchStepped(msg.run), true
	case fullLoadPromptMsg:
		m.askFullLoad(msg.err)
		return nil, true
	case fullLoadedMsg:
		m.fullLoaded(msg)
		return nil, true
	case quitWaitMsg:
		if m.large.quitting {
			return tea.Quit, true
		}
		return nil, true
	}
	return nil, false
}

// ---- The question about a file that is not UTF-8.

// fullLoadTag is the tag of the question whether to load a file whole.
type fullLoadTag struct{ err editor.ErrLargeNeedsFullLoad }

// askFullLoad asks whether to load the file of err whole. A large file in an
// encoding other than UTF-8 cannot be read lazily, so loading it means decoding all
// of it. The question is a Yes/No confirm; No leaves the notice, so the user sees why
// the file did not open.
func (m *Model) askFullLoad(err editor.ErrLargeNeedsFullLoad) {
	if _, ok := m.ed.(fullLoader); !ok {
		m.notice = err.Error()
		return
	}
	m.notice = err.Error()
	q := err.Error() + ". Load it?"
	m.pushOverlay(prompt.NewConfirm(m.styles, q, fullLoadTag{err: err}))
}

// maybeAskFullLoad asks the question when err is the error of a large file that is not
// UTF-8, and reports whether it was.
func (m *Model) maybeAskFullLoad(err error) bool {
	var need editor.ErrLargeNeedsFullLoad
	if !errors.As(err, &need) {
		return false
	}
	m.askFullLoad(need)
	return true
}

// fullLoadAnswer applies the answer to the question: Yes reads the file whole in a
// command.
func (m *Model) fullLoadAnswer(tag fullLoadTag, d prompt.Done) tea.Cmd {
	fl, ok := m.ed.(fullLoader)
	if !ok || d.Outcome != prompt.Yes {
		return nil
	}
	m.notice = "Loading " + tag.err.Path + "..."
	load := fl.LoadFullFunc(tag.err.Path)
	return func() tea.Msg {
		doc, err := load()
		return fullLoadedMsg{doc: doc, err: err}
	}
}

// fullLoaded opens the file that was read whole. A file over the limit comes back
// with an error, which is shown.
func (m *Model) fullLoaded(msg fullLoadedMsg) {
	if msg.err != nil {
		m.notice = msg.err.Error()
		return
	}
	if fl, ok := m.ed.(fullLoader); ok && msg.doc != nil {
		fl.OpenLoaded(msg.doc)
		m.notice = ""
		m.syncViewport()
	}
}

// ---- Writes that can be stopped.

// saveRun is a write in flight. paths are the files of the tabs it saves, so that
// closing one of them stops it.
type saveRun struct {
	cancel  context.CancelFunc
	paths   []string
	stopped bool
	// done is closed when the command that writes has returned, and so has removed the
	// temporary file of a stopped write.
	done chan struct{}
}

// stop stops the write.
func (r *saveRun) stop() {
	r.stopped = true
	r.cancel()
}

// ctxDone reports whether the write was stopped.
func (r *saveRun) ctxDone() bool { return r.stopped }

// beginSave registers a write that is about to start and returns its number and the
// context that stops it. The write saves the active tab, or every modified tab for
// Save All.
func (m *Model) beginSave(all bool) (int, context.Context) {
	ctx, cancel := context.WithCancel(m.large.context())
	if m.large.saves == nil {
		m.large.saves = make(map[int]*saveRun)
	}
	run := &saveRun{cancel: cancel, done: make(chan struct{})}
	for i, t := range m.ed.Tabs() {
		if t.Path != "" && (t.Active || (all && t.Modified)) {
			run.paths = append(run.paths, m.tabPath(i))
		}
	}
	m.large.nextSave++
	m.large.saves[m.large.nextSave] = run
	return m.large.nextSave, ctx
}

// waitSaves waits until the writes in flight have returned, for at most quitWait. A write
// that was stopped removes its temporary file before it returns.
func (m *Model) waitSaves() {
	timer := time.NewTimer(quitWait)
	defer timer.Stop()
	for _, run := range m.large.saves {
		select {
		case <-run.done:
		case <-timer.C:
			return
		}
	}
}

// endSave forgets a write that ended.
func (m *Model) endSave(id int) {
	if run := m.large.saves[id]; run != nil {
		run.cancel()
	}
	delete(m.large.saves, id)
}

// tabPath is the file of tab i, or "" when it has none or there is no tab i.
func (m *Model) tabPath(i int) string {
	tabs := m.ed.Tabs()
	if i < 0 || i >= len(tabs) {
		return ""
	}
	return tabs[i].Path
}

// cancelSavesOf stops the writes that save the file at path, because its tab closed.
// The write would otherwise end after the document it belongs to was released.
func (m *Model) cancelSavesOf(path string) {
	if path == "" {
		return
	}
	for _, run := range m.large.saves {
		if slices.Contains(run.paths, path) {
			run.stop()
		}
	}
}

// writeSave runs the writes of a plan, as a command does. A write that can be stopped
// is stopped by ctx.
func writeSave(ctx context.Context, as asyncSaver, p editor.SavePlan) []editor.SaveResult {
	if cs, ok := as.(ctxSaver); ok {
		return cs.WriteSaveContext(ctx, p)
	}
	return as.WriteSave(p)
}

// SetQuitStash gives the root the function that writes the unsaved edits of the large
// documents to the session before the program ends, or nil. The app calls it once, after
// New. A large document is not held whole, so its edits survive a quit only as a stash.
// When the function fails the quit does not happen: the user is told why and can save the
// file or close its tab first.
func (m *Model) SetQuitStash(fn func() error) { m.large.quitStash = fn }

// errorDialogs is the optional part of Dialogs that shows a message with one OK button.
type errorDialogs interface {
	Error(message string, tag any, ov style.Overlay) Overlay
}

// errorDialogTag is the tag of the message that says a quit was stopped.
type errorDialogTag struct{}

// stashBeforeQuit writes the stash of the unsaved large documents. When that fails it
// stops the quit, shows the error in a dialog (or in the notice row when there are no
// dialogs), and reports that the quit must not go on.
func (m *Model) stashBeforeQuit() (ok bool) {
	if m.large.quitStash == nil {
		return true
	}
	err := m.large.quitStash()
	if err == nil {
		return true
	}
	msg := "Quit cancelled: the unsaved changes of a large file could not be stashed: " + err.Error() +
		". Save the file, or close its tab, and quit again."
	m.notice = msg
	if d, ok := m.dialogs.(errorDialogs); ok {
		m.pushOverlay(d.Error(msg, errorDialogTag{}, m.styles.Overlay))
	}
	return false
}

// quit ends the program. The unsaved edits of large documents are stashed first, and a
// failed stash stops the quit. The writes in flight are stopped next, and the program
// waits, until they have removed their temporary files, for at most quitWait: a quit
// must not leave a half-written copy of a large file next to the original.
func (m *Model) quit() tea.Cmd {
	if !m.stashBeforeQuit() {
		return nil
	}
	if len(m.large.saves) == 0 {
		return tea.Quit
	}
	for _, run := range m.large.saves {
		run.stop()
	}
	m.large.quitting = true
	return tea.Tick(quitWait, func(time.Time) tea.Msg { return quitWaitMsg{} })
}

// quitIfWaiting ends the program when a quit was waiting for the saves it cancelled and
// the last of them has ended.
func (m *Model) quitIfWaiting() tea.Cmd {
	if m.large.quitting && len(m.large.saves) == 0 {
		return tea.Quit
	}
	return nil
}

// ---- Search.

// searchKey is what a scan searches: the document and its text, as the editor
// versions it, and the query.
type searchKey struct {
	doc      any
	version  uint64
	complete bool
	q        search.Query
}

// searchRun is one scan of a large document for the find bar. A command runs its
// steps one after another, so only one of them touches the scan at a time; the root
// reads the scan between the steps, in the update that the step's message starts.
type searchRun struct {
	key  searchKey
	scan *search.Scan
	// busy is set while a command steps the scan. dead says the run was replaced or
	// cancelled: its scan is closed when the command ends, not before.
	busy, dead bool
	err        error
}

// stepCmd steps the scan for one time slice in a command.
func (r *searchRun) stepCmd() tea.Cmd {
	r.busy = true
	return func() tea.Msg {
		_, r.err = r.scan.Step(scanSlice)
		return searchStepMsg{run: r}
	}
}

// cancelStream stops the scan of the find bar, if one runs. A scan that a command is
// stepping is closed when the command ends.
func (m *Model) cancelStream() {
	r := m.large.stream
	if r == nil {
		return
	}
	m.large.stream = nil
	r.dead = true
	if !r.busy {
		r.scan.Close()
	}
}

// streamSearch returns the stream surface of the editor when the active document is
// large.
func (m *Model) streamSearch() (streamSearcher, bool) {
	ss, ok := m.ed.(streamSearcher)
	if !ok {
		return nil, false
	}
	if _, _, _, large := ss.SearchKey(); !large {
		return nil, false
	}
	return ss, true
}

// searchSync keeps the scan of the find bar in step with the document and the query.
// The bar counts the matches of a large document by a scan that runs in slices. After
// every update the root compares what the scan searches with what the bar wants: a
// new query, a toggle, an edit, a switch of tab or a finished line index starts a new
// scan from the caret line, and the old one is cancelled. With the bar closed, or the
// query empty, no scan runs.
func (m *Model) searchSync() tea.Cmd {
	ss, large := m.streamSearch()
	var want searchKey
	wanted := false
	if large && m.findBar != nil && m.findOpen() && !m.findBar.Replacing() {
		if q := m.findBar.Query(); q.Text != "" {
			doc, ver, complete, _ := ss.SearchKey()
			want, wanted = searchKey{doc: doc, version: ver, complete: complete, q: q}, true
		}
	}
	if r := m.large.stream; r != nil && (!wanted || r.key != want) {
		m.cancelStream()
	}
	if !wanted || m.large.stream != nil {
		return nil
	}
	line := 0
	if doc := m.ed.Active(); doc != nil {
		line = doc.Cursor().Line
	}
	scan, err := ss.StartScan(want.q, line)
	if err != nil {
		// A pattern that does not compile shows its error where the count goes.
		m.findBar.SetStream(find.StreamInfo{Err: err})
		return nil
	}
	r := &searchRun{key: want, scan: scan}
	m.large.stream = r
	m.publishStream(r)
	return r.stepCmd()
}

// publishStream gives the find bar the progress of the scan.
func (m *Model) publishStream(r *searchRun) {
	sc := r.scan
	info := find.StreamInfo{
		Progress: sc.Progress(),
		Count:    len(sc.Matches()),
		Done:     sc.Done(),
		Capped:   sc.Capped(),
		Partial:  sc.Partial(),
		Regex:    r.key.q.Regex,
		Elapsed:  sc.Stats().Elapsed,
		Err:      r.err,
	}
	if info.Done {
		info.Matches = sc.Matches()
	}
	m.findBar.SetStream(info)
}

// searchStepped takes the end of a time slice of a scan. A run that was cancelled
// meanwhile is closed. Otherwise the bar shows the progress, and the next slice
// starts unless the scan ended.
func (m *Model) searchStepped(r *searchRun) tea.Cmd {
	r.busy = false
	if r.dead {
		r.scan.Close()
		return nil
	}
	if m.findBar != nil {
		m.publishStream(r)
	}
	if r.scan.Done() || r.err != nil {
		// The matches stay readable after Close, which gives the snapshot back.
		r.scan.Close()
		return nil
	}
	return r.stepCmd()
}

// largeFindNavigate is findNavigate for a large document: F3 goes to the next
// match, and Shift+F3 to the previous one, found from the caret without a list of
// matches.
func (m *Model) largeFindNavigate(ss streamSearcher, q search.Query, forward bool) {
	h := findHost{m: m}
	start, end, ok := h.Selection()
	if !ok {
		start, end = h.Cursor(), h.Cursor()
	}
	from := end
	if !forward {
		from = start
	}
	match, found, scanned, err := m.findNext(ss, q, from, !forward)
	if forward && found && match.Start == match.End && match.Start == end {
		// The caret is on an empty match, which is the current one.
		match, found, scanned, err = m.findNext(ss, q, buffer.Pos{Line: end.Line, Col: end.Col + 1}, false)
	}
	m.afterLargeFind(h, match, found, scanned, err, from, forward)
}

// navResume is a navigation search that ran out of its budget: the text, the
// position and the direction it searched, and where it stopped.
type navResume struct {
	key      searchKey
	from     buffer.Pos
	backward bool
	at       search.Cursor
}

// findNext is ss.FindNext for the keys that move to a match. A search over more text
// than one budget stops with scanned false, and the same search asked again (same
// text, query, position and direction) goes on from where it stopped, so repeating
// F3 or Enter reaches a match that is farther than one budget.
func (m *Model) findNext(ss streamSearcher, q search.Query, from buffer.Pos, backward bool) (search.Match, bool, bool, error) {
	doc, ver, complete, _ := ss.SearchKey()
	key := searchKey{doc: doc, version: ver, complete: complete, q: q}
	var at search.Cursor
	if r := m.large.nav; r != nil && r.key == key && r.from == from && r.backward == backward {
		at = r.at
		m.large.nav = nil
	}
	match, found, scanned, next, err := ss.FindNext(q, from, backward, at)
	if err == nil && !found && !scanned {
		m.large.nav = &navResume{key: key, from: from, backward: backward, at: next}
	}
	return match, found, scanned, err
}

// afterLargeFind selects the match a large search found, or says why none was.
func (m *Model) afterLargeFind(h findHost, match search.Match, found, scanned bool, err error, from buffer.Pos, forward bool) {
	switch {
	case err != nil:
		m.notice = err.Error()
	case !found && !scanned:
		m.notice = find.SearchLimitText
	case !found:
		m.notice = search.StatusText(0, 0)
		if h.IndexPartial() {
			m.notice += find.PartialIndexText
		}
	default:
		h.Select(match)
		m.revealCaretCentered()
		m.notice = ""
		if forward == match.Start.Less(from) {
			m.notice = "Search wrapped"
		}
	}
}

// largeVisibleHighlights returns the search highlights of the rows on screen for a
// large document: its matches are not listed, so the ones that touch the painted rows
// are found for each frame.
func (m *Model) largeVisibleHighlights(ss streamSearcher, doc Document, q search.Query) func(line int) []search.Highlight {
	rows := m.bodyRows()
	top := doc.Top()
	matches, err := ss.VisibleMatches(q, top, top+rows-1)
	if err != nil || len(matches) == 0 {
		return noHighlights
	}
	var current *search.Match
	if start, end, ok := (findHost{m: m}).Selection(); ok {
		for i := range matches {
			if matches[i].Start == start && matches[i].End == end {
				current = &matches[i]
				break
			}
		}
	}
	lines := docLines{d: doc}
	return func(line int) []search.Highlight {
		return search.Highlights(lines, matches, line, current)
	}
}

// savingStatus is shown while a large document is written, and cleared when the
// write ends.
func (m *Model) savingStatus() {
	if doc := m.ed.Active(); doc != nil && isLargeDoc(doc) {
		m.notice = savingNotice
	}
}

// clearSaving removes the "Saving..." notice, if it is still the notice.
func (m *Model) clearSaving() {
	if m.notice == savingNotice {
		m.notice = ""
	}
}

// ---- Opening a large file.

// openFileCmd opens path in a permanent tab, or a preview, and moves the caret to line and
// col when line is set. A large file is read in the returned command: the read samples
// its head, indexes the start and lists its directory, which Update must not wait for.
// Any other file is opened at once.
func (m *Model) openFileCmd(path string, preview bool, line, col int) tea.Cmd {
	cmd, _ := m.openFileCmdErr(path, preview, line, col)
	return cmd
}

// openFileCmdErr is openFileCmd that also returns why a file opened at once did not open.
// A large file's read has no error yet; it arrives in the message of the command.
func (m *Model) openFileCmdErr(path string, preview bool, line, col int) (tea.Cmd, error) {
	ge, ok := m.ed.(gotoEditor)
	pc, isPC := m.ed.(pathClassifier)
	if !ok || !isPC || preview || !pc.IsLargePath(path) {
		return nil, m.openAtErr(path, preview, line, col)
	}
	m.notice = "Opening " + filepath.Base(path) + "..."
	load := ge.LoadFunc(path)
	return func() tea.Msg {
		doc, err := load()
		return openLoadedMsg{doc: doc, err: err, path: path, line: line, col: col}
	}, nil
}

// startupOpenCmd opens the path a remote launch parked in Deps.StartupOpen through the
// asynchronous file path, so Init never blocks on remote I/O. The open is retried after a
// successful authority reconnect; a disconnected or failed read is a notice, and the
// editor stays empty rather than opening a local file for the remote path.
func (m *Model) startupOpenCmd() tea.Cmd {
	if !m.startupPending || m.startupOpen == "" {
		return nil
	}
	cmd, _ := m.openFileCmdErr(m.startupOpen, false, 0, 0)
	return cmd
}

// openLoaded opens the large file that openFileCmd read. A file that is not UTF-8 asks
// whether to load it whole, and any other failure is the notice.
func (m *Model) openLoaded(msg openLoadedMsg) {
	ge, ok := m.ed.(gotoEditor)
	if !ok {
		return
	}
	if msg.err != nil {
		// A failed startup open stays pending so a reconnect retries it; it never turns
		// into an empty local buffer.
		if !m.maybeAskFullLoad(msg.err) {
			m.notice = msg.err.Error()
		}
		return
	}
	if msg.path == m.startupOpen {
		m.startupPending = false
	}
	m.notice = ""
	ge.OpenLoaded(msg.doc)
	m.syncViewport()
	if msg.line > 0 {
		if err := ge.GotoLine(msg.line, msg.col); err != nil {
			m.notice = err.Error()
		}
	}
	m.focusEditor()
}
