package tui

import (
	"errors"
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/internal/tui/editorpane"
	"github.com/tbxark/gvim/internal/tui/popup"
	"github.com/tbxark/gvim/internal/tui/prompt"
	"github.com/tbxark/gvim/internal/tui/space"
)

// lspDebounce is how long the root waits after the last edit before it sends the
// text to the server. lspPollEvery is how often it asks the port for diagnostics
// while a document is open. Tests shorten both.
var (
	lspDebounce  = 300 * time.Millisecond
	lspPollEvery = 300 * time.Millisecond
)

// lspEditor is the optional editor view that applies the edits of a server: the
// rename and the completion. The app adapter implements it.
type lspEditor interface {
	ApplyLSPEdits(path string, edits []editor.LSPEdit) error
}

// gotoEditor is the optional file-opening view that a goto uses to read the file
// off the UI goroutine. LoadFunc returns the read, and OpenLoaded opens its result
// in Update. An editor without it opens the file in Update, as it did before.
type gotoEditor interface {
	fileEditor
	LoadFunc(path string) func() (*editor.Document, error)
	OpenLoaded(d *editor.Document)
}

// lspState is the root's language-server state. Its zero value runs no server.
// The port is set in New, and every other field is filled in as documents open.
type lspState struct {
	port LSPSession
	// root is the workspace root last given to the port.
	root string
	// tracks holds the documents sent to a server, by path.
	tracks map[string]*lspTrack
	// diags holds the latest diagnostics of each document, by path.
	diags map[string][]Diagnostic
	// off holds the documents with LSP turned off by toggle.
	off map[string]bool
	// layout is the tab layout the last sync saw, and stale asks for a full
	// reconcile on the next update.
	layout lspLayout
	stale  bool
	// stopped is set by lsp_stop, and no document opens until lsp_restart.
	stopped bool
	// seq is the generation of the latest debounce timer. An older timer is ignored.
	seq int
	// polling is set while the diagnostics poll is running.
	polling bool
	// inlay is the Toggle Inlay Hints flag. Inlay hints are not requested.
	inlay bool
	// comp is the open completion popup, or nil.
	comp *lspCompletion
	// unavailable holds the languages whose server cannot run (its program is not installed,
	// or it failed to start), with the reason. No document of such a language is sent until
	// lsp_restart. noted holds the languages whose reason was already shown.
	unavailable map[string]*LSPUnavailableError
	noted       map[string]bool
	// forced holds the languages the user started by hand (Start/Restart LSP Server), whose
	// server then runs although it does not start on its own: lsp_enabled is off, the language
	// is muted, or the server is manual.
	forced map[string]bool
}

// lspLayout is the tab layout a sync saw: the number of tabs, the active one and
// the path of the active document. The path changes when a preview tab is replaced
// by another file, while the count and the index stay the same.
type lspLayout struct {
	count, active int
	path          string
}

// lspDocState is where a document stands with its server.
type lspDocState int

const (
	lspClosed  lspDocState = iota // not sent to the server
	lspOpening                    // didOpen is in flight
	lspOpen                       // the server has the document
)

// lspTrack is the root's record of one document it has sent to a server. want is
// the latest text, and have is the text the server holds once it is open.
type lspTrack struct {
	path, lang string
	state      lspDocState
	busy       bool // an open or change command is in flight
	want, have string
	// dirty is set when the active document changed since want was read. The text is read
	// when the debounce timer fires (lspFresh), not on each key press.
	dirty bool
	// ver is the version of the document at the last look, when verOK is set. A look at the
	// same version does nothing.
	ver   uint64
	verOK bool
}

// lspCompletion is the open completion popup, with the items it lists and the
// caret it was asked at.
type lspCompletion struct {
	path  string
	at    buffer.Pos
	items []CompletionItem
	pop   *popup.Popup
}

// lspAsk is a request to a server that gets a reply.
type lspAsk int

const (
	lspAskDefinition lspAsk = iota + 1
	lspAskImplementation
	lspAskReferences
	lspAskHover
	lspAskCompletion
	lspAskRename
	lspAskCodeActions
)

// lspPick is the tag of a popup row. Chosen runs what the row stands for. A
// completion row carries its list, so the choice still inserts after the root has
// dropped the list from its state.
type lspPick struct {
	ask   lspAsk
	loc   LSPLocation
	title string
	comp  *lspCompletion
}

// lspRenameTag is the tag of the rename prompt. It names the document and the
// caret the rename is for.
type lspRenameTag struct {
	path string
	at   buffer.Pos
}

// The messages the language-server commands send back to the root.
type (
	lspOpenedMsg struct {
		path, text string
		err        error
	}
	lspChangedMsg struct {
		path, text string
		err        error
	}
	lspDebounceMsg struct{ seq int }
	lspPollMsg     struct{}
	lspDiagsMsg    struct{ events []DiagnosticsEvent }
	lspReplyMsg    struct {
		ask    lspAsk
		path   string
		at     buffer.Pos
		locs   []LSPLocation
		text   string
		items  []CompletionItem
		edits  []FileEdit
		titles []string
		err    error
	}
	lspServerMsg struct {
		action string
		lang   string // the language a restart was asked for, "" for all
		err    error
	}
	lspErrMsg struct{ err error }
	// lspRenameLoadedMsg is the files of a rename that were not open, read in a
	// command. docs and errs run parallel to edits. files counts the files already
	// changed, and back is the path of the tab the rename started in.
	lspRenameLoadedMsg struct {
		edits []FileEdit
		docs  []*editor.Document
		errs  []error
		files int
		back  string
	}
	// lspLoadedMsg is the file a goto read. Its tab opens, and the caret moves to
	// line and col, one-based.
	lspLoadedMsg struct {
		doc       *editor.Document
		err       error
		line, col int
	}
)

// lspUpdate runs after each root update. It starts the commands that keep the
// servers in step with the documents: the opens of the tabs, the debounced sends of
// the active text, the closes of the tabs that went away and the diagnostics poll.
// It reads the text of the active document, but it does no IO itself.
func (m *Model) lspUpdate(msg tea.Msg) tea.Cmd {
	s := &m.lsp
	if s.port == nil {
		return nil
	}
	if root := m.rootDir(); root != s.root {
		s.root = root
		s.port.SetRoot(root)
		s.stale = true
	}
	if c := s.comp; c != nil && !m.onStack(c.pop) {
		s.comp = nil
	}
	var cmds []tea.Cmd
	layout := lspLayout{count: len(m.ed.Tabs()), active: m.activeTab()}
	if doc := m.ed.Active(); doc != nil {
		layout.path = docPath(doc)
	}
	if layout != s.layout || s.stale {
		s.layout, s.stale = layout, false
		cmds = append(cmds, m.lspReconcile())
	}
	switch msg.(type) {
	case tea.MouseMotionMsg, tea.MouseWheelMsg, tea.MouseReleaseMsg:
		// A pointer move or a scroll does not change the text.
	default:
		cmds = append(cmds, m.lspWatchActive())
	}
	return tea.Batch(cmds...)
}

// lspReconcile brings every tab's document in step with its server. It records
// the text of each open tab, closes the documents of tabs that went away, and
// starts the open or the change each document needs.
func (m *Model) lspReconcile() tea.Cmd {
	s := &m.lsp
	tabs, ok := m.ed.(tabDocs)
	if !ok {
		return nil
	}
	var cmds []tea.Cmd
	seen := make(map[string]bool)
	for i := range len(m.ed.Tabs()) {
		doc := tabs.DocAt(i)
		if doc == nil {
			continue
		}
		path := docPath(doc)
		lang := m.lspLanguage(doc)
		if path == "" || isVirtualDoc(doc) || !s.enabled(lang) || s.off[path] || isLargeDoc(doc) {
			// A server is sent the whole text of a document on each change, which a
			// large document cannot give without reading the file.
			continue
		}
		seen[path] = true
		tr := s.tracks[path]
		if tr == nil {
			tr = &lspTrack{path: path, lang: lang}
			if s.tracks == nil {
				s.tracks = make(map[string]*lspTrack)
			}
			s.tracks[path] = tr
		}
		tr.want, tr.dirty = lspDocText(doc), false
		tr.ver, tr.verOK = docVersion(doc)
	}
	for path, tr := range s.tracks {
		if seen[path] {
			continue
		}
		if tr.state != lspClosed {
			cmds = append(cmds, m.lspCloseCmd(path))
		}
		delete(s.tracks, path)
		m.lspDropDiags(path)
	}
	for _, path := range sortedKeys(s.tracks) {
		cmds = append(cmds, m.lspSendFor(s.tracks[path]))
	}
	return tea.Batch(cmds...)
}

// sortedKeys returns the keys of the track map in order, so the commands start in
// a stable order.
func sortedKeys(m map[string]*lspTrack) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

// lspWatchActive notices an edit to the active document. An edit marks the track dirty and arms
// the debounce timer. The text is read when the timer fires (lspFresh), not here: reading it costs
// the length of the file, and every key press comes through the watch. A look at a document whose
// version has not moved does nothing.
func (m *Model) lspWatchActive() tea.Cmd {
	doc := m.ed.Active()
	if doc == nil {
		return nil
	}
	tr := m.lsp.tracks[docPath(doc)]
	if tr == nil || isLargeDoc(doc) {
		return nil
	}
	ver, versioned := docVersion(doc)
	if versioned && tr.verOK && tr.ver == ver {
		return nil
	}
	tr.ver, tr.verOK = ver, versioned
	tr.dirty = true
	return m.lspArm()
}

// lspFresh reads the text of the active document into tr.want when the document changed since
// its last read, so that a send, or the check that a reply needs one, has the latest text. It
// does nothing while the document is not the active one: a switch of tabs runs the reconcile,
// which reads every open document.
func (m *Model) lspFresh(tr *lspTrack) {
	if !tr.dirty {
		return
	}
	doc := m.ed.Active()
	if doc == nil || docPath(doc) != tr.path {
		return
	}
	tr.want, tr.dirty = lspDocText(doc), false
}

// docVersion returns the version of the text of doc, and false when doc does not report one.
func docVersion(doc Document) (uint64, bool) {
	v, ok := doc.(versionedDoc)
	if !ok {
		return 0, false
	}
	return v.Version(), true
}

// lspArm starts a debounce timer. The newest timer is the only one that sends.
func (m *Model) lspArm() tea.Cmd {
	s := &m.lsp
	s.seq++
	seq := s.seq
	return tea.Tick(lspDebounce, func(time.Time) tea.Msg { return lspDebounceMsg{seq: seq} })
}

// lspSendFor returns the command that brings tr in step with its server: an open
// for a closed document, or a change when the text differs. Nothing is sent while
// a command for tr is in flight, or after lsp_stop.
func (m *Model) lspSendFor(tr *lspTrack) tea.Cmd {
	m.lspFresh(tr)
	if tr.busy || m.lsp.stopped {
		return nil
	}
	switch tr.state {
	case lspClosed:
		tr.busy, tr.state = true, lspOpening
		return m.lspOpenCmd(tr.path, tr.lang, tr.want)
	case lspOpen:
		if tr.want != tr.have {
			tr.busy = true
			return m.lspChangeCmd(tr.path, tr.want)
		}
	}
	return nil
}

// lspOpenCmd sends didOpen for path with text.
func (m *Model) lspOpenCmd(path, lang, text string) tea.Cmd {
	port := m.lsp.port
	return func() tea.Msg {
		return lspOpenedMsg{path: path, text: text, err: port.Open(path, lang, text)}
	}
}

// lspChangeCmd sends the full text of the open document at path.
func (m *Model) lspChangeCmd(path, text string) tea.Cmd {
	port := m.lsp.port
	return func() tea.Msg {
		return lspChangedMsg{path: path, text: text, err: port.Change(path, text)}
	}
}

// lspCloseCmd sends didClose for path. An error is shown as a notice.
func (m *Model) lspCloseCmd(path string) tea.Cmd {
	port := m.lsp.port
	return func() tea.Msg {
		if err := port.Close(path); err != nil {
			return lspErrMsg{err: err}
		}
		return nil
	}
}

// lspMessage handles the language-server messages and the requests of the
// popups and prompts. It reports whether it took msg, and the command the message
// starts.
func (m *Model) lspMessage(msg tea.Msg) (tea.Cmd, bool) {
	s := &m.lsp
	switch msg := msg.(type) {
	case lspOpenedMsg:
		return m.lspOpened(msg), true
	case lspChangedMsg:
		return m.lspChanged(msg), true
	case lspDebounceMsg:
		if msg.seq != s.seq {
			return nil, true
		}
		var cmds []tea.Cmd
		for _, path := range sortedKeys(s.tracks) {
			cmds = append(cmds, m.lspSendFor(s.tracks[path]))
		}
		return tea.Batch(cmds...), true
	case lspPollMsg:
		return m.lspPoll(), true
	case lspDiagsMsg:
		return m.lspDiags(msg), true
	case lspReplyMsg:
		return m.lspReply(msg), true
	case lspLoadedMsg:
		m.lspLoaded(msg)
		return nil, true
	case lspRenameLoadedMsg:
		m.lspRenameLoaded(msg)
		return nil, true
	case lspFormatMsg:
		m.lspFormatted(msg)
		return nil, true
	case lspServerMsg:
		return m.lspServerDone(msg), true
	case lspErrMsg:
		m.notice = msg.err.Error()
		return nil, true
	case popup.Chosen:
		if cmd, ok := m.pluginPopupChosen(msg); ok {
			return cmd, true
		}
		if pick, ok := msg.Item.Data.(lspPick); ok {
			return m.lspChosen(pick, msg.Index), true
		}
	case prompt.Done:
		if tag, ok := msg.Tag.(lspRenameTag); ok {
			return m.lspRenameDone(tag, msg), true
		}
	}
	return nil, false
}

// lspOpened records the reply to a didOpen. A document is open once its reply
// arrives without an error.
func (m *Model) lspOpened(msg lspOpenedMsg) tea.Cmd {
	s := &m.lsp
	tr := s.tracks[msg.path]
	if tr == nil {
		return nil
	}
	tr.busy = false
	if msg.err != nil {
		tr.state = lspClosed
		m.lspOpenFailed(tr, msg.err)
		return nil
	}
	tr.state, tr.have = lspOpen, msg.text
	var cmds []tea.Cmd
	if !s.polling {
		s.polling = true
		cmds = append(cmds, m.lspPollArm())
	}
	if tr.dirty || tr.want != tr.have {
		cmds = append(cmds, m.lspArm())
	}
	return tea.Batch(cmds...)
}

// lspChanged records the reply to a didChange. A failed change closes the document
// so that the next sync opens it again.
func (m *Model) lspChanged(msg lspChangedMsg) tea.Cmd {
	s := &m.lsp
	tr := s.tracks[msg.path]
	if tr == nil {
		return nil
	}
	tr.busy = false
	if msg.err != nil {
		tr.state = lspClosed
		s.stale = true
		m.notice = msg.err.Error()
		return nil
	}
	tr.have = msg.text
	if tr.dirty || tr.want != tr.have {
		return m.lspArm()
	}
	return nil
}

// lspPollArm starts the diagnostics poll after lspPollEvery.
func (m *Model) lspPollArm() tea.Cmd {
	return tea.Tick(lspPollEvery, func(time.Time) tea.Msg { return lspPollMsg{} })
}

// lspPoll asks the port for the diagnostics of every open document. It runs in a
// command, because the port may take a lock on the server.
func (m *Model) lspPoll() tea.Cmd {
	s := &m.lsp
	var paths []string
	for _, path := range sortedKeys(s.tracks) {
		if s.tracks[path].state == lspOpen {
			paths = append(paths, path)
		}
	}
	if len(paths) == 0 {
		s.polling = false
		return nil
	}
	port := s.port
	return func() tea.Msg {
		var events []DiagnosticsEvent
		for _, p := range paths {
			if ev, ok := port.Diagnostics(p); ok {
				events = append(events, ev)
			}
		}
		return lspDiagsMsg{events: events}
	}
}

// lspDiags stores the diagnostics the poll found, and polls again while documents
// are open.
func (m *Model) lspDiags(msg lspDiagsMsg) tea.Cmd {
	s := &m.lsp
	for _, ev := range msg.events {
		m.plugDiagnosticsChanged(ev.Path, s.diags[ev.Path], ev.Items)
		if len(ev.Items) == 0 {
			delete(s.diags, ev.Path)
			continue
		}
		if s.diags == nil {
			s.diags = make(map[string][]Diagnostic)
		}
		s.diags[ev.Path] = ev.Items
	}
	for _, tr := range s.tracks {
		if tr.state == lspOpen {
			return m.lspPollArm()
		}
	}
	s.polling = false
	return nil
}

// lspDropDiags forgets the diagnostics of a document, and tells the plugins that they
// went (diagnostics_updated with zero counts) when it had some.
func (m *Model) lspDropDiags(path string) {
	old, ok := m.lsp.diags[path]
	if !ok {
		return
	}
	m.plugDiagnosticsChanged(path, old, nil)
	delete(m.lsp.diags, path)
}

// lspServerDone applies the end of lsp_restart or lsp_stop. A restart closes every
// document, and the next sync opens them again. A stop drops them all and keeps
// them down until a restart.
func (m *Model) lspServerDone(msg lspServerMsg) tea.Cmd {
	s := &m.lsp
	if msg.err != nil {
		m.notice = msg.err.Error()
	}
	switch msg.action {
	case "restart":
		s.stopped = false
		s.unavailable, s.noted = nil, nil
		if msg.lang != "" {
			if s.forced == nil {
				s.forced = make(map[string]bool)
			}
			s.forced[msg.lang] = true
		}
		for _, tr := range s.tracks {
			tr.state, tr.busy, tr.have = lspClosed, false, ""
		}
		s.stale = true
		if msg.err == nil {
			m.notice = "Language server restarted"
		}
	case "stop":
		s.stopped = true
		s.tracks = nil
		for _, path := range slices.Sorted(maps.Keys(s.diags)) {
			m.lspDropDiags(path)
		}
		s.diags = nil
		s.polling = false
		if msg.err == nil {
			m.notice = "Language server stopped"
		}
	}
	return nil
}

// lspReply shows the reply to a request: it jumps to one location, lists several in a
// popup, shows the hover text or the completions, applies a rename, or lists the
// code actions.
func (m *Model) lspReply(r lspReplyMsg) tea.Cmd {
	if r.err != nil {
		m.notice = r.err.Error()
		return nil
	}
	switch r.ask {
	case lspAskDefinition, lspAskImplementation, lspAskReferences:
		return m.lspLocations(r)
	case lspAskHover:
		m.lspHover(r.path, r.at, r.text)
	case lspAskCompletion:
		m.lspCompletions(r)
	case lspAskRename:
		return m.lspRenamed(r.edits)
	case lspAskCodeActions:
		m.lspCodeActions(r.titles)
	case lspAskSignature:
		m.lspSignature(r.text)
	}
	return nil
}

// lspLocations jumps to the only location of a reply, or lists several in a popup.
// It returns the command of the jump, if there is one.
func (m *Model) lspLocations(r lspReplyMsg) tea.Cmd {
	none := map[lspAsk]string{
		lspAskDefinition:     "No definition found",
		lspAskImplementation: "No implementation found",
		lspAskReferences:     "No references found",
	}
	switch len(r.locs) {
	case 0:
		m.notice = none[r.ask]
	case 1:
		return m.lspGoto(r.locs[0])
	default:
		items := make([]popup.Item, len(r.locs))
		for i, loc := range r.locs {
			items[i] = popup.Item{
				Label: fmt.Sprintf("%s:%d:%d", m.lspRel(loc.Path), loc.Start.Line+1, loc.Start.Col+1),
				Data:  lspPick{ask: r.ask, loc: loc},
			}
		}
		title := "Definitions"
		switch r.ask {
		case lspAskImplementation:
			title = "Implementations"
		case lspAskReferences:
			title = "References"
		}
		m.lspPopup(title, items)
	}
	return nil
}

// lspGoto opens the file of loc with the caret on the start of the range. With a
// gotoEditor the file is read in the returned command, and its tab opens when the
// lspLoadedMsg arrives.
func (m *Model) lspGoto(loc LSPLocation) tea.Cmd {
	line, col := loc.Start.Line+1, loc.Start.Col+1
	ge, ok := m.ed.(gotoEditor)
	if !ok {
		m.openFileAt(loc.Path, line, col)
		m.focusEditor()
		return nil
	}
	load := ge.LoadFunc(loc.Path)
	return func() tea.Msg {
		doc, err := load()
		return lspLoadedMsg{doc: doc, err: err, line: line, col: col}
	}
}

// lspLoaded opens the file a goto read in a tab, and moves the caret to the location.
// A failed read is shown as a notice. The editor takes the focus either way, as a
// goto always did.
func (m *Model) lspLoaded(msg lspLoadedMsg) {
	ge, ok := m.ed.(gotoEditor)
	if !ok {
		return
	}
	if msg.err != nil {
		m.notice = msg.err.Error()
	} else {
		ge.OpenLoaded(msg.doc)
		m.syncViewport()
		if err := ge.GotoLine(msg.line, msg.col); err != nil {
			m.notice = err.Error()
		}
	}
	m.focusEditor()
}

// lspHover shows the hover text in a popup, one row per line, and a line wider than the popup
// over several rows, so that no message is cut. The diagnostics that cover the position come
// first, with their severity and source, so one popup says both what the server knows of the
// symbol and what it found wrong with it, as Fresh's hover does. Without any text there is a
// notice.
func (m *Model) lspHover(path string, at buffer.Pos, text string) {
	var items []popup.Item
	width := popup.TextWidth(m.width)
	add := func(line string) {
		for _, row := range space.Wrap(line, width, len(line)+1) {
			items = append(items, popup.Item{Label: row})
		}
	}
	diags := m.diagsAt(path, at)
	for _, d := range diags {
		for i, line := range strings.Split(strings.TrimSpace(d.Message), "\n") {
			if i == 0 {
				line = diagText(Diagnostic{Severity: d.Severity, Source: d.Source, Message: line})
			}
			add(line)
		}
	}
	if strings.TrimSpace(text) == "" {
		if len(items) == 0 {
			m.notice = "No hover information"
			return
		}
	} else {
		if len(diags) > 0 {
			items = append(items, popup.Item{Label: ""})
		}
		for line := range strings.SplitSeq(text, "\n") {
			add(line)
		}
	}
	m.lspPopup("Hover", items)
}

// lspCompletions lists the completion items in a popup. The choice is accepted by
// Enter, by Tab, or by completion_accept.
func (m *Model) lspCompletions(r lspReplyMsg) {
	if len(r.items) == 0 {
		m.notice = "No completions"
		return
	}
	c := &lspCompletion{path: r.path, at: r.at, items: r.items}
	items := make([]popup.Item, len(r.items))
	for i, it := range r.items {
		items[i] = popup.Item{Label: it.Label, Detail: it.Detail, Data: lspPick{ask: lspAskCompletion, comp: c}}
	}
	c.pop = m.lspPopup("Completions", items)
	m.lsp.comp = c
}

// lspCodeActions lists the code action titles. Applying one is not supported yet,
// so a choice only shows a notice.
func (m *Model) lspCodeActions(titles []string) {
	if len(titles) == 0 {
		m.notice = "No code actions"
		return
	}
	items := make([]popup.Item, len(titles))
	for i, t := range titles {
		items[i] = popup.Item{Label: t, Data: lspPick{ask: lspAskCodeActions, title: t}}
	}
	m.lspPopup("Code Actions", items)
}

// lspChosen runs the choice of a popup row. index is the row's position. It returns
// the command the choice starts, if there is one.
func (m *Model) lspChosen(pick lspPick, index int) tea.Cmd {
	switch pick.ask {
	case lspAskDefinition, lspAskImplementation, lspAskReferences:
		return m.lspGoto(pick.loc)
	case lspAskCompletion:
		m.acceptCompletion(pick.comp, index)
	case lspAskCodeActions:
		m.notice = "Applying code actions is not supported yet: " + pick.title
	}
	return nil
}

// lspPopup opens a popup at the caret with title and items. It returns the popup,
// so the completion list can read its highlight later.
func (m *Model) lspPopup(title string, items []popup.Item) *popup.Popup {
	var anchor popup.Point
	if x, y, ok := m.caret(); ok {
		anchor = popup.Point{X: x, Y: y}
	}
	p := popup.New(popupStyles(m.styles))
	p.Show(anchor, title, items)
	m.pushOverlay(popupOverlay{p: p})
	return p
}

// acceptCompletion inserts the completion at index of c. The item's range is replaced
// when the server gave one, and otherwise the text goes at the caret the completion
// was asked at. The caret then moves to the end of the inserted text. The list
// closes after the insert, whether or not the insert failed.
func (m *Model) acceptCompletion(c *lspCompletion, index int) {
	if c == nil || index < 0 || index >= len(c.items) {
		return
	}
	item := c.items[index]
	from, to := c.at, c.at
	if item.Range != nil {
		from, to = item.Range.Start, item.Range.End
	}
	err := m.applyLSPEdits(c.path, []editor.LSPEdit{{From: from, To: to, Text: item.Insert}})
	m.closeCompletion(c)
	if err != nil {
		m.notice = err.Error()
		return
	}
	if doc := m.ed.Active(); doc != nil && docPath(doc) == c.path {
		m.ed.SetCursor(insertEnd(from, item.Insert), false)
	}
}

// closeCompletion closes the list c: its popup leaves the stack, and the root forgets
// the list when it is still the open one.
func (m *Model) closeCompletion(c *lspCompletion) {
	if m.lsp.comp == c {
		m.lsp.comp = nil
	}
	m.closePopup(c.pop)
}

// insertEnd returns the position just after text inserted at from.
func insertEnd(from buffer.Pos, text string) buffer.Pos {
	parts := strings.Split(text, "\n")
	if len(parts) == 1 {
		return buffer.Pos{Line: from.Line, Col: from.Col + len([]rune(text))}
	}
	last := parts[len(parts)-1]
	return buffer.Pos{Line: from.Line + len(parts) - 1, Col: len([]rune(last))}
}

// closePopup removes the popup overlay that holds p from the stack.
func (m *Model) closePopup(p *popup.Popup) {
	m.overlays = slices.DeleteFunc(m.overlays, func(o Overlay) bool {
		po, ok := o.(popupOverlay)
		return ok && po.p == p
	})
}

// onStack reports whether the popup overlay that holds p is on the overlay stack.
func (m *Model) onStack(p *popup.Popup) bool {
	return slices.ContainsFunc(m.overlays, func(o Overlay) bool {
		po, ok := o.(popupOverlay)
		return ok && po.p == p
	})
}

// lspKey handles a key while the completion popup is on top. Tab accepts the
// highlighted item. The keys the list takes (the moves, Enter and Esc) go to the
// overlay as usual. Any other key is typed text, Backspace or a caret key: it goes
// on to the editor, and the list closes, since it was asked at an older caret.
func (m *Model) lspKey(msg tea.Msg) (tea.Cmd, bool) {
	k, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil, false
	}
	c := m.lsp.comp
	if c == nil || len(m.overlays) == 0 {
		return nil, false
	}
	if top, ok := m.overlays[len(m.overlays)-1].(popupOverlay); !ok || top.p != c.pop {
		return nil, false
	}
	switch tea.Key(k).Code {
	case tea.KeyTab:
		m.acceptCompletion(c, c.pop.Selected())
		return nil, true
	case tea.KeyUp, tea.KeyDown, tea.KeyPgUp, tea.KeyPgDown, tea.KeyHome, tea.KeyEnd, tea.KeyEnter, tea.KeyEscape:
		return nil, false
	}
	m.closeCompletion(c)
	return nil, false
}

// lspAction runs the language-server action named action. It reports whether it
// handled the action, and the command the action starts.
func (m *Model) lspAction(action string) (tea.Cmd, bool) {
	switch action {
	case "lsp_goto_definition":
		return m.lspAsked(lspAskDefinition), true
	case "lsp_implementation":
		return m.lspAsked(lspAskImplementation), true
	case "lsp_references":
		return m.lspAsked(lspAskReferences), true
	case "lsp_hover":
		return m.lspAsked(lspAskHover), true
	case "lsp_completion":
		return m.lspAsked(lspAskCompletion), true
	case "lsp_code_actions":
		return m.lspAsked(lspAskCodeActions), true
	case "lsp_signature_help":
		return m.lspAsked(lspAskSignature), true
	case "lsp_rename":
		m.askLSPRename()
	case "jump_to_next_error":
		m.jumpToError(true)
	case "jump_to_previous_error":
		m.jumpToError(false)
	case "lsp_restart":
		return m.lspRestart(), true
	case "lsp_stop":
		return m.lspServerCmd("stop", ""), true
	case "lsp_toggle_for_buffer":
		return m.lspToggle(), true
	case "show_lsp_status":
		m.notice = m.lspStatus()
	case "toggle_inlay_hints":
		m.lsp.inlay = !m.lsp.inlay
		m.notice = "Inlay hints are not requested by this port"
	case "completion_accept":
		if c := m.lsp.comp; c != nil {
			m.acceptCompletion(c, c.pop.Selected())
		}
	case "completion_dismiss":
		if c := m.lsp.comp; c != nil {
			m.closeCompletion(c)
		}
	default:
		return nil, false
	}
	return nil, true
}

// noServerNotice is the notice for a language that no server is configured for. It
// names the config key that starts one.
func noServerNotice(lang string) string {
	return fmt.Sprintf("No language server for %s: set lsp.%s.command in config.json", lang, lang)
}

// lspAsked starts a request of kind ask for the caret of the active document. A
// request needs a configured server and a document the server holds.
func (m *Model) lspAsked(ask lspAsk) tea.Cmd {
	doc := m.ed.Active()
	if doc == nil {
		m.notice = noDocumentNotice
		return nil
	}
	path := docPath(doc)
	if path == "" {
		m.notice = "The document has no file name, so no language server serves it"
		return nil
	}
	lang := m.lspLanguage(doc)
	if m.lsp.port == nil || !m.lsp.enabled(lang) {
		m.notice = m.lspNoServer(lang)
		return nil
	}
	if why := lspLargeReason(doc); why != "" {
		m.notice = "LSP: " + why
		return nil
	}
	switch tr := m.lsp.tracks[path]; {
	case tr != nil && tr.state == lspOpen:
	case tr != nil && tr.state == lspOpening:
		m.notice = "The language server is starting"
		return nil
	default:
		// A failed start was shown as a notice when it failed.
		m.notice = "The language server is not running for this document"
		return nil
	}
	return m.lspRequest(ask, path, doc.Cursor(), "")
}

// lspRequest is the command that asks the port one request. The reply comes back
// as an lspReplyMsg.
func (m *Model) lspRequest(ask lspAsk, path string, at buffer.Pos, name string) tea.Cmd {
	port := m.lsp.port
	return func() tea.Msg {
		r := lspReplyMsg{ask: ask, path: path, at: at}
		switch ask {
		case lspAskDefinition:
			r.locs, r.err = port.Definition(path, at)
		case lspAskImplementation:
			r.locs, r.err = port.Implementation(path, at)
		case lspAskReferences:
			r.locs, r.err = port.References(path, at)
		case lspAskHover:
			r.text, r.err = port.Hover(path, at)
		case lspAskCompletion:
			r.items, r.err = port.Completion(path, at)
		case lspAskRename:
			r.edits, r.err = port.Rename(path, at, name)
		case lspAskCodeActions:
			r.titles, r.err = port.CodeActions(path, at)
		case lspAskSignature:
			r.text, r.err = signatureOf(port, path, at)
		}
		return r
	}
}

// askLSPRename opens the rename prompt, with the word under the caret filled in.
func (m *Model) askLSPRename() {
	doc := m.ed.Active()
	if doc == nil {
		m.notice = noDocumentNotice
		return
	}
	path := docPath(doc)
	if m.lsp.port == nil || path == "" || !m.lsp.enabled(m.lspLanguage(doc)) {
		m.notice = m.lspNoServer(m.lspLanguage(doc))
		return
	}
	if why := lspLargeReason(doc); why != "" {
		m.notice = "LSP: " + why
		return
	}
	at := doc.Cursor()
	m.pushOverlay(lineOverlay{l: prompt.NewLine(m.styles, prompt.LineOptions{
		Label: "New name:",
		Input: lspWordAt(doc.Line(at.Line), at.Col),
		Tag:   lspRenameTag{path: path, at: at},
	})})
}

// lspRenameDone starts the rename when the prompt was submitted with a name.
func (m *Model) lspRenameDone(tag lspRenameTag, d prompt.Done) tea.Cmd {
	name := strings.TrimSpace(d.Text)
	if d.Outcome != prompt.Submitted || name == "" {
		return nil
	}
	return m.lspRequest(lspAskRename, tag.path, tag.at, name)
}

// lspRenamed applies the edits of a rename to the open documents, one undo step
// each. A file that is not open is read in the returned command, and
// lspRenameLoadedMsg opens it and applies its edits, as Fresh does. Those files stay
// unsaved, like the open ones.
func (m *Model) lspRenamed(edits []FileEdit) tea.Cmd {
	files, skipped := 0, 0
	var closed []FileEdit
	for _, fe := range edits {
		err := m.applyLSPEdits(fe.Path, fe.Edits)
		switch {
		case errors.Is(err, editor.ErrNotOpen):
			closed = append(closed, fe)
		case err != nil:
			m.notice = err.Error()
			return nil
		default:
			files++
		}
	}
	m.lsp.stale = true
	ge, ok := m.ed.(gotoEditor)
	if !ok {
		skipped, closed = len(closed), nil
	}
	if len(closed) == 0 {
		m.lspRenameNotice(files, skipped, 0)
		return nil
	}
	back := ""
	if doc := m.ed.Active(); doc != nil {
		back = docPath(doc)
	}
	loads := make([]func() (*editor.Document, error), len(closed))
	for i, fe := range closed {
		loads[i] = ge.LoadFunc(fe.Path)
	}
	return func() tea.Msg {
		msg := lspRenameLoadedMsg{edits: closed, files: files, back: back}
		for _, load := range loads {
			doc, err := load()
			msg.docs = append(msg.docs, doc)
			msg.errs = append(msg.errs, err)
		}
		return msg
	}
}

// lspRenameLoaded opens the files a rename read and applies their edits. The tab the
// rename started in is active again at the end, so a refactor does not move the user.
func (m *Model) lspRenameLoaded(msg lspRenameLoadedMsg) {
	ge, ok := m.ed.(gotoEditor)
	if !ok {
		return
	}
	files, opened := msg.files, 0
	for i, fe := range msg.edits {
		if msg.errs[i] != nil {
			m.notice = msg.errs[i].Error()
			continue
		}
		ge.OpenLoaded(msg.docs[i])
		if err := m.applyLSPEdits(fe.Path, fe.Edits); err != nil {
			m.notice = err.Error()
			continue
		}
		files++
		opened++
	}
	if msg.back != "" {
		for i, t := range m.ed.Tabs() {
			if t.Path == msg.back {
				m.activateTab(i)
				break
			}
		}
	}
	m.lsp.stale = true
	m.syncViewport()
	if opened == len(msg.edits) {
		m.lspRenameNotice(files, 0, opened)
	}
}

// lspRenameNotice says how many files a rename changed, how many it skipped, and how
// many of the changed ones it had to open.
func (m *Model) lspRenameNotice(files, skipped, opened int) {
	m.notice = fmt.Sprintf("Renamed in %d files", files)
	if opened > 0 {
		m.notice += fmt.Sprintf("; %d opened with unsaved changes", opened)
	}
	if skipped > 0 {
		m.notice += fmt.Sprintf("; %d not open, not changed", skipped)
	}
}

// applyLSPEdits applies edits to the open document at path through the editor, if
// it can apply them.
func (m *Model) applyLSPEdits(path string, edits []editor.LSPEdit) error {
	ed, ok := m.ed.(lspEditor)
	if !ok {
		return errors.New("editing by a language server is not supported in this port")
	}
	return ed.ApplyLSPEdits(path, edits)
}

// lspRestart is Start/Restart LSP Server. It restarts the servers, and for the language of the
// active document it also starts a server that does not start on its own, as Fresh's manual
// start does. A language with no server at all gets the notice that says so.
func (m *Model) lspRestart() tea.Cmd {
	lang := ""
	if doc := m.ed.Active(); doc != nil && m.lsp.port != nil {
		lang = m.lspLanguage(doc)
		if !m.lspStartable(lang) {
			m.notice = m.lspNoServer(lang)
			return nil
		}
	}
	return m.lspServerCmd("restart", lang)
}

// lspStartable reports whether a server can run for lang: it starts on its own, or the
// configuration knows a program for it and only a switch keeps it off.
func (m *Model) lspStartable(lang string) bool {
	port := m.lsp.port
	if port == nil {
		return false
	}
	if port.Supports(lang) {
		return true
	}
	d, ok := port.(lspDescriber)
	if !ok {
		return false
	}
	info := d.Server(lang)
	return info.State != LSPServerNone && info.Command != ""
}

// lspServerCmd is the command for lsp_restart and lsp_stop. lang is the language a restart
// starts by hand, or "".
func (m *Model) lspServerCmd(action, lang string) tea.Cmd {
	port := m.lsp.port
	if port == nil {
		m.notice = `No language server is configured: add an "lsp" entry to config.json`
		return nil
	}
	return func() tea.Msg {
		var err error
		if action == "restart" {
			err = port.Restart()
		} else {
			err = port.Stop()
		}
		return lspServerMsg{action: action, lang: lang, err: err}
	}
}

// lspToggle turns LSP on or off for the active document. Turning it off closes the
// document on its server, and turning it on opens it at the next sync.
func (m *Model) lspToggle() tea.Cmd {
	doc := m.ed.Active()
	if doc == nil || docPath(doc) == "" {
		m.notice = noDocumentNotice
		return nil
	}
	if why := lspLargeReason(doc); why != "" {
		m.notice = "LSP: " + why
		return nil
	}
	path := docPath(doc)
	s := &m.lsp
	if s.off[path] {
		delete(s.off, path)
		s.stale = true
		m.notice = "LSP is on for this buffer"
		return nil
	}
	if s.off == nil {
		s.off = make(map[string]bool)
	}
	s.off[path] = true
	m.notice = "LSP is off for this buffer"
	tr := s.tracks[path]
	delete(s.tracks, path)
	m.lspDropDiags(path)
	if tr != nil && tr.state != lspClosed {
		return m.lspCloseCmd(path)
	}
	return nil
}

// lspStatus describes the language server of the active document in one line.
func (m *Model) lspStatus() string {
	if m.lsp.port == nil {
		return "LSP: no language server is configured"
	}
	doc := m.ed.Active()
	if doc == nil {
		return "LSP: no open document"
	}
	lang := m.lspLanguage(doc)
	path := docPath(doc)
	switch {
	case m.lsp.unavailable[lang] != nil:
		return "LSP: " + m.lsp.unavailable[lang].Error()
	case !m.lsp.enabled(lang):
		return "LSP: " + lowerFirst(m.lspNoServer(lang))
	case lspLargeReason(doc) != "":
		return "LSP: " + lspLargeReason(doc)
	case m.lsp.off[path]:
		return "LSP: off for this buffer"
	case m.lsp.stopped:
		return "LSP: stopped"
	}
	state := "not started"
	if tr := m.lsp.tracks[path]; tr != nil {
		state = map[lspDocState]string{lspClosed: "closed", lspOpening: "starting", lspOpen: "open"}[tr.state]
	}
	if note := m.lspServerNote(lang); note != "" {
		return "LSP: " + lang + " " + note + " " + state
	}
	return "LSP: " + lang + " server " + state
}

// jumpToError moves the caret to the next or the previous error or warning of the
// active document, and wraps around at the ends.
func (m *Model) jumpToError(forward bool) {
	doc := m.ed.Active()
	if doc == nil {
		m.notice = noDocumentNotice
		return
	}
	var list []Diagnostic
	for _, d := range m.lsp.diags[docPath(doc)] {
		if d.Severity <= editorpane.SeverityWarning {
			list = append(list, d)
		}
	}
	if len(list) == 0 {
		m.notice = "No errors or warnings"
		return
	}
	slices.SortFunc(list, func(a, b Diagnostic) int {
		switch {
		case a.Start.Less(b.Start):
			return -1
		case b.Start.Less(a.Start):
			return 1
		}
		return 0
	})
	cur := doc.Cursor()
	// list is in document order. Forward takes the first start after the caret,
	// backward the last start before it, and each wraps to the other end.
	var target Diagnostic
	if forward {
		target = list[0]
		for _, d := range list {
			if cur.Less(d.Start) {
				target = d
				break
			}
		}
	} else {
		target = list[len(list)-1]
		for _, l := range slices.Backward(list) {
			if l.Start.Less(cur) {
				target = l
				break
			}
		}
	}
	m.ed.SetCursor(target.Start, false)
	m.notice = diagText(target) + " (" + diagPosition(list, target) + ")"
}

// lspSpans returns the diagnostic underlines of doc for the editor pane. It is nil
// when the document has none.
func (m *Model) lspSpans(doc Document) func(line int) []editorpane.DiagnosticSpan {
	ds := m.lsp.diags[docPath(doc)]
	if len(ds) == 0 {
		return nil
	}
	return func(line int) []editorpane.DiagnosticSpan {
		var out []editorpane.DiagnosticSpan
		for _, d := range ds {
			if line < d.Start.Line || line > d.End.Line {
				continue
			}
			start, end := 0, len([]rune(doc.Line(line)))
			if line == d.Start.Line {
				start = d.Start.Col
			}
			if line == d.End.Line {
				end = d.End.Col
			}
			if end <= start {
				end = start + 1
			}
			out = append(out, editorpane.DiagnosticSpan{Start: start, End: end, Severity: d.Severity})
		}
		return out
	}
}

// lspWarnings counts the errors and warnings of the active document, for the status
// row.
func (m *Model) lspWarnings() int {
	doc := m.ed.Active()
	if doc == nil {
		return 0
	}
	n := 0
	for _, d := range m.lsp.diags[docPath(doc)] {
		if d.Severity <= editorpane.SeverityWarning {
			n++
		}
	}
	return n
}

// lspClose stops the servers when the program ends.
func (m *Model) lspClose() {
	if m.lsp.port != nil {
		_ = m.lsp.port.Stop()
	}
}

// enabled reports whether a server serves language lang and the root has not
// stopped it.
func (s *lspState) enabled(lang string) bool {
	return s.port != nil && !s.stopped && s.unavailable[lang] == nil && (s.forced[lang] || s.port.Supports(lang))
}

// lspLanguage is the language ID of doc: the lower-case name of its language, the
// name the status row shows.
func (m *Model) lspLanguage(doc Document) string {
	return strings.ToLower(m.languageFor(doc.Name()))
}

// lspRel returns path relative to the workspace root when it is inside it.
func (m *Model) lspRel(path string) string {
	if root := m.rootDir(); root != "" {
		if rel, err := filepath.Rel(root, path); err == nil && !strings.HasPrefix(rel, "..") {
			return rel
		}
	}
	return path
}

// lspLargeReason is why no language server serves doc, or "" when its size does not
// stop one. A document in large file mode is never sent to a server: the server is
// given the whole text on each change, and the text is not held.
func lspLargeReason(doc Document) string {
	ld, ok := doc.(LargeDoc)
	if !ok || !ld.Large() {
		return ""
	}
	return "File too large (" + strconv.FormatInt(ld.Index().Size, 10) + " bytes)"
}

// lspDocText is the text of doc, lines joined with a newline, as the server holds it.
// Nothing calls it for a document in large file mode; it gives no text for one, in
// case a call is added, rather than reading the file.
func lspDocText(doc Document) string {
	if isLargeDoc(doc) {
		return ""
	}
	var b strings.Builder
	for i := range doc.LineCount() {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(doc.Line(i))
	}
	return b.String()
}

// docPath is the file path of doc, or "" for an unnamed document.
func docPath(doc Document) string {
	if p, ok := doc.(pathEditor); ok {
		return p.FilePath()
	}
	return ""
}

// lspWordAt returns the identifier that covers rune column col of line, or "" when
// the caret is not on one.
func lspWordAt(line string, col int) string {
	rs := []rune(line)
	isWord := func(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' }
	start, end := min(max(col, 0), len(rs)), min(max(col, 0), len(rs))
	for start > 0 && isWord(rs[start-1]) {
		start--
	}
	for end < len(rs) && isWord(rs[end]) {
		end++
	}
	return string(rs[start:end])
}
