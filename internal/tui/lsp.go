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
	"github.com/tbxark/gvim/i18n"
	"github.com/tbxark/gvim/internal/tui/editorpane"
	"github.com/tbxark/gvim/internal/tui/frame"
	"github.com/tbxark/gvim/internal/tui/list"
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

// CompletionResolver is the optional LSPSession part that fills an incomplete
// completion item (completionItem/resolve) before it can be accepted: the server
// supplies the additional edits of an auto-import lazily. The root calls it from a
// command, never on the update goroutine.
type CompletionResolver interface {
	ResolveCompletion(path string, token int) (CompletionItem, error)
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
	// diags holds the latest diagnostics of each document, by path, and
	// diagVersion the document version each set was published for. A set whose
	// version is older than the document's is stale, and its inline messages are
	// dimmed.
	diags       map[string][]Diagnostic
	diagVersion map[string]diagVersion
	// wsDiags is the workspace diagnostics the panel and the quickfix list show:
	// every file a server reported on, open or not, as the port merged it at the
	// last workspace poll. The poll replaces the whole list, so a publish or pull
	// update is reflected; it is cleared on stop.
	wsDiags []FileDiagnostics
	// progress is the live servers' unfinished $/progress entries, as text, for the
	// diagnostics panel. The poll refreshes it; stopping or restarting the servers
	// empties it.
	progress []LSPProgress
	// off holds the documents with LSP turned off by toggle.
	off map[string]bool
	// muted holds the languages the user muted from the status popup: no server
	// of theirs starts and the pill says so.
	muted map[string]bool
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
	// inlay is the Toggle Inlay Hints flag: whether the root asks the servers
	// for inlay hints and paints them.
	inlay bool
	// hints holds the inlay hints of each document, by path, and hintSeq orders
	// the refresh timers. hintErr holds the reason a language's hints cannot be
	// requested, so the toggle can say it once.
	hints   map[string][]InlayHint
	hintSeq int
	hintErr map[string]string
	// tokens holds the semantic tokens of each document, by path, and tokenSeq
	// orders the refresh timers. tokenErr holds the reason a language has no
	// tokens, so the refresh stops asking.
	tokens   map[string][]SemanticToken
	tokenSeq int
	tokenErr map[string]string
	tokenTop map[string]int
	// folds holds the folding ranges each document's server last gave, by path,
	// and foldSeq orders the refresh timers. The ranges are pushed into the
	// editor, which merges them with its indentation folds; the map is kept for
	// the refresh bookkeeping and the tests.
	folds   map[string][]FoldRange
	foldSeq int
	foldErr map[string]string
	// comp is the open completion popup, or nil.
	comp *lspCompletion
	// popupPath is the document a code-action popup was opened for.
	popupPath string
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

// diagVersion is the protocol version a document's diagnostics were published
// for. versioned is false when the server sent no version, and then the
// diagnostics are never stale.
type diagVersion struct {
	version   int32
	versioned bool
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
	// lspVersion is the protocol version the server has the document at, from the
	// open or change that sent it, when lspVersionOK is set. It detects stale
	// diagnostics and a workspace edit computed against an older text.
	lspVersion   int32
	lspVersionOK bool
}

// lspCompletion is the open completion popup, with the items it lists and the
// caret it was asked at.
type lspCompletion struct {
	path string
	at   buffer.Pos
	// all is the complete candidate pool the re-filter works from (server items
	// plus buffer words); items is the subset currently shown. Keeping all separate
	// is what makes a candidate filtered out by a longer word reappear after
	// Backspace, as Fresh's completion_items does.
	all []CompletionItem
	// origins[i] is the index in all of the item shown at row i; acceptance and
	// highlight retention use that identity across a re-filter.
	origins []int
	items   []CompletionItem
	pop     *popup.Popup
	// version is the document version the list was asked at, and versionOK says it
	// is set. Acceptance passes it as the expected version so a changed document is
	// never edited with an item computed against older text.
	version   editor.Version
	versionOK bool
}

// completionResolvedMsg is the answer of a completionItem/resolve request started by
// accepting an incomplete item (auto-imports arrive in the resolved item).
type completionResolvedMsg struct {
	comp *lspCompletion
	item CompletionItem
	err  error
}

// completionMultiResolve is the document state a multi-caret resolve must still find
// when its reply arrives: the version and every caret at the moment of acceptance.
// The reply is applied only while all of it is unchanged, so the lazy edits cannot
// land at positions computed for a different document or a different caret set.
type completionMultiResolve struct {
	path    string
	version editor.Version
	carets  []buffer.Pos // the primary caret first, then the extra carets
}

// completionMultiResolvedMsg is the answer of a completionItem/resolve started with
// extra carets. state is the snapshot the accept took before the request.
type completionMultiResolvedMsg struct {
	state completionMultiResolve
	item  CompletionItem
	err   error
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
	lspAskResolveCodeAction
)

// lspPick is the tag of a popup row. Chosen runs what the row stands for. A
// completion row carries its list, so the choice still inserts after the root has
// dropped the list from its state.
type lspPick struct {
	ask    lspAsk
	loc    LSPLocation
	action CodeAction
	comp   *lspCompletion
	// item is the row's index in comp.items. A re-filtered list has different rows
	// for the same candidates, so acceptance goes by this index, not the row number
	// (labels repeat across auto-import candidates).
	item int
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
		version    int32
		err        error
	}
	lspChangedMsg struct {
		path, text string
		version    int32
		err        error
	}
	lspDebounceMsg struct{ seq int }
	lspPollMsg     struct{}
	lspDiagsMsg    struct {
		events         []DiagnosticsEvent
		workspace      []WorkspaceEdit
		workspaceDiags []FileDiagnostics
		progress       []LSPProgress
	}
	lspReplyMsg struct {
		ask     lspAsk
		path    string
		at      buffer.Pos
		locs    []LSPLocation
		text    string
		items   []CompletionItem
		edits   WorkspaceEdit
		actions []CodeAction
		action  CodeAction
		// version is the document version the request was asked at, captured when
		// the request started. versionOK says it is set; a completion reply without
		// it (one that opened the popup in Update) captures the current version when
		// the popup opens.
		version   editor.Version
		versionOK bool
		err       error
	}
	lspServerMsg struct {
		action string
		lang   string // the language a restart was asked for, "" for all
		err    error
	}
	lspErrMsg struct{ err error }
	// lspLoadedMsg is the file a goto read. Its tab opens, and the caret moves to
	// line and col, one-based.
	lspLoadedMsg struct {
		doc       *editor.Document
		err       error
		line, col int
	}
	// lspStatusMsg is the server list the status popup opens with.
	lspStatusMsg struct{ servers []LSPServerStatus }
	// lspServerActionMsg is the answer of a status-popup server action. action is
	// restart, stop, mute or unmute; language and name say which server.
	lspServerActionMsg struct {
		action, language, name string
		err                    error
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
	// A semantic-token request covers the viewport: a scroll asks again for the
	// lines that came into view.
	if doc := m.ed.Active(); doc != nil {
		path := docPath(doc)
		if top := doc.Top(); m.lsp.tokenTop[path] != top {
			if m.lsp.tokenTop == nil {
				m.lsp.tokenTop = make(map[string]int)
			}
			m.lsp.tokenTop[path] = top
			cmds = append(cmds, m.lspDataArm(lspDataTokens))
		}
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
		m.lspDropData(path)
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
		version, err := port.Open(path, lang, text)
		return lspOpenedMsg{path: path, text: text, version: version, err: err}
	}
}

// lspChangeCmd sends the full text of the open document at path.
func (m *Model) lspChangeCmd(path, text string) tea.Cmd {
	port := m.lsp.port
	return func() tea.Msg {
		version, err := port.Change(path, text)
		return lspChangedMsg{path: path, text: text, version: version, err: err}
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
		cmds = append(cmds, m.lspDataArm(lspDataHints), m.lspDataArm(lspDataTokens), m.lspDataArm(lspDataFolds))
		return tea.Batch(cmds...), true
	case lspDataMsg:
		return m.lspDataRefresh(msg), true
	case lspHintsDoneMsg:
		m.lspHints(msg)
		return nil, true
	case lspTokensDoneMsg:
		m.lspTokens(msg)
		return nil, true
	case lspFoldsDoneMsg:
		m.lspFolds(msg)
		return nil, true
	case lspPollMsg:
		return m.lspPoll(), true
	case lspDiagsMsg:
		return m.lspDiags(msg), true
	case lspReplyMsg:
		return m.lspReply(msg), true
	case completionResolvedMsg:
		return m.completionResolved(msg), true
	case completionMultiResolvedMsg:
		return m.completionMultiResolved(msg), true
	case lspLoadedMsg:
		m.lspLoaded(msg)
		return nil, true
	case fileEditsLoadedMsg:
		return m.fileEditsLoaded(msg), true
	case workspaceStepLoadedMsg:
		return m.workspaceStepLoaded(msg), true
	case workspaceStepOpMsg:
		return m.workspaceStepOp(msg), true
	case lspStatusMsg:
		m.openLSPStatus(msg)
		return nil, true
	case lspServerActionMsg:
		return m.lspServerActionDone(msg), true
	case lspFormatMsg:
		m.lspFormatted(msg)
		return nil, true
	case lspServerMsg:
		return m.lspServerDone(msg), true
	case lspErrMsg:
		m.notice = msg.err.Error()
		// Plugin API tier 1a: a server or request failed; plugins (lsp_help and friends)
		// can surface it.
		m.plugEmit("lsp_server_error", map[string]any{"error": msg.err.Error()})
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
	tr.lspVersion, tr.lspVersionOK = msg.version, true
	var cmds []tea.Cmd
	if !s.polling {
		s.polling = true
		cmds = append(cmds, m.lspPollArm())
	}
	if tr.dirty || tr.want != tr.have {
		cmds = append(cmds, m.lspArm())
	}
	cmds = append(cmds, m.lspDataArm(lspDataHints), m.lspDataArm(lspDataTokens), m.lspDataArm(lspDataFolds))
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
	tr.lspVersion, tr.lspVersionOK = msg.version, true
	if tr.dirty || tr.want != tr.have {
		return m.lspArm()
	}
	return tea.Batch(m.lspDataArm(lspDataHints), m.lspDataArm(lspDataTokens), m.lspDataArm(lspDataFolds))
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
		port := s.port
		if port == nil {
			return nil
		}
		// Drain the edits a server asked for and take a last workspace diagnostics
		// snapshot: a server may report on files that are not open, and the panel
		// lists them after the last document closed.
		return func() tea.Msg {
			return lspDiagsMsg{
				workspace:      port.WorkspaceEdits(),
				workspaceDiags: port.WorkspaceDiagnostics(),
				progress:       port.Progress(),
			}
		}
	}
	port := s.port
	return func() tea.Msg {
		var events []DiagnosticsEvent
		for _, p := range paths {
			if ev, ok := port.Diagnostics(p); ok {
				events = append(events, ev)
			}
		}
		return lspDiagsMsg{
			events:         events,
			workspace:      port.WorkspaceEdits(),
			workspaceDiags: port.WorkspaceDiagnostics(),
			progress:       port.Progress(),
		}
	}
}

// lspWorkspacePoll is the on-demand workspace diagnostics refresh: the panel and the
// quickfix list call it when they open, because the poll loop stops while no
// document is open.
func (m *Model) lspWorkspacePoll() tea.Cmd {
	port := m.lsp.port
	if port == nil {
		return nil
	}
	return func() tea.Msg {
		return lspDiagsMsg{
			workspace:      port.WorkspaceEdits(),
			workspaceDiags: port.WorkspaceDiagnostics(),
			progress:       port.Progress(),
		}
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
			delete(s.diagVersion, ev.Path)
			continue
		}
		if s.diags == nil {
			s.diags = make(map[string][]Diagnostic)
		}
		s.diags[ev.Path] = ev.Items
		if s.diagVersion == nil {
			s.diagVersion = make(map[string]diagVersion)
		}
		s.diagVersion[ev.Path] = diagVersion{version: ev.Version, versioned: ev.Versioned}
	}
	// The workspace list is authoritative for the panel: a server may drop a
	// diagnostic, so replace it rather than merge. The progress list is replaced the
	// same way: an entry the server ended is not reported again.
	s.wsDiags = msg.workspaceDiags
	s.progress = msg.progress
	var cmds []tea.Cmd
	for _, edit := range msg.workspace {
		cmds = append(cmds, m.applyServerWorkspaceEdit(edit))
	}
	for _, tr := range s.tracks {
		if tr.state == lspOpen {
			cmds = append(cmds, m.lspPollArm())
			return tea.Batch(cmds...)
		}
	}
	s.polling = false
	return tea.Batch(cmds...)
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
			m.notice = uiText("gvim.notice.lsp.restarted", "Language server restarted")
		}
	case "stop":
		s.stopped = true
		s.tracks = nil
		s.wsDiags = nil
		s.progress = nil
		for _, path := range slices.Sorted(maps.Keys(s.diags)) {
			m.lspDropDiags(path)
		}
		s.diags = nil
		s.polling = false
		if msg.err == nil {
			m.notice = uiText("gvim.notice.lsp.stopped", "Language server stopped")
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
		return m.applyWorkspaceEdit(r.edits, m.workspaceEditNotice("Renamed"))
	case lspAskCodeActions:
		m.lspCodeActions(r.path, r.actions)
	case lspAskResolveCodeAction:
		return m.applyCodeAction(r.path, r.action)
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
			m.notice = uiText("gvim.notice.lsp.no_hover", "No hover information")
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
	m.openCompletionItems(r.path, r.at, r.items, r.version, r.versionOK, 0)
}

// openCompletionItems is the one completion-list renderer: the server list, the
// buffer-word list and a re-filtered list all come through it, so row data and the
// acceptance guard stay the same. selected is the row to highlight; a re-filter
// passes the row the candidate had before so Backspace/typing keep the user's choice.
// versionOK false means the caller has no request version (a local list or a
// re-filter), so the current document version is captured as the guard.
func (m *Model) openCompletionItems(path string, at buffer.Pos, items []CompletionItem, version editor.Version, versionOK bool, selected int) *lspCompletion {
	return m.openCompletionList(path, at, items, items, identityOrigins(len(items)), version, versionOK, selected)
}

// identityOrigins is the origins slice for a list shown exactly as it is stored.
func identityOrigins(n int) []int {
	out := make([]int, n)
	for i := range out {
		out[i] = i
	}
	return out
}

// openCompletionList renders all (the complete candidate pool) with shown (the
// rows that match the current prefix) and origins (each shown row's index in all).
// The server list, the local buffer-word list, a merge reply and a re-filter all use
// it, so identity, acceptance and the version guard stay in one place.
func (m *Model) openCompletionList(path string, at buffer.Pos, all, shown []CompletionItem, origins []int, version editor.Version, versionOK bool, selected int) *lspCompletion {
	if len(shown) == 0 {
		m.notice = uiText("gvim.notice.lsp.no_completions", "No completions")
		return nil
	}
	c := &lspCompletion{path: path, at: at, all: all, items: shown, origins: origins}
	c.version, c.versionOK = version, versionOK
	if !c.versionOK {
		if ve, ok := m.ed.(lspVersionedEditor); ok {
			c.version, c.versionOK = ve.DocumentVersion(path)
		}
	}
	rows := make([]popup.Item, len(shown))
	for i, it := range shown {
		rows[i] = popup.Item{Label: it.Label, Detail: it.Detail, Data: lspPick{ask: lspAskCompletion, comp: c, item: i}}
	}
	c.pop = m.lspPopupSelected("Completions", rows, selected)
	m.lsp.comp = c
	return c
}

// lspCodeActions lists the code actions in a popup: the title, and the server and
// kind of the action, with a preferred action marked.
func (m *Model) lspCodeActions(path string, actions []CodeAction) {
	if len(actions) == 0 {
		m.notice = "No code actions"
		return
	}
	items := make([]popup.Item, len(actions))
	for i, a := range actions {
		detail := a.Kind
		if a.Source != "" {
			detail = "[" + a.Source + "] " + detail
		}
		if a.Preferred {
			detail = strings.TrimSpace("preferred " + detail)
		}
		items[i] = popup.Item{Label: a.Title, Detail: detail, Data: lspPick{ask: lspAskCodeActions, action: a}}
	}
	m.lsp.popupPath = path
	m.lspPopup("Code Actions", items)
}

// lspChosen runs the choice of a popup row. index is the row's position. It returns
// the command the choice starts, if there is one.
func (m *Model) lspChosen(pick lspPick, index int) tea.Cmd {
	switch pick.ask {
	case lspAskDefinition, lspAskImplementation, lspAskReferences:
		return m.lspGoto(pick.loc)
	case lspAskCompletion:
		return m.acceptCompletion(pick.comp, pick.item)
	case lspAskCodeActions:
		return m.chooseCodeAction(m.lsp.popupPath, pick.action)
	}
	return nil
}

// chooseCodeAction starts applying a chosen code action. An action that has
// neither an edit nor a command needs a resolve request first, and the resolved
// action is applied when its reply arrives.
func (m *Model) chooseCodeAction(path string, action CodeAction) tea.Cmd {
	if action.NeedsResolve {
		return m.resolveCodeAction(path, action)
	}
	return m.applyCodeAction(path, action)
}

// resolveCodeAction asks the server to complete a code action.
func (m *Model) resolveCodeAction(path string, action CodeAction) tea.Cmd {
	port := m.lsp.port
	return func() tea.Msg {
		resolved, err := port.ResolveCodeAction(path, action)
		return lspReplyMsg{ask: lspAskResolveCodeAction, path: path, action: resolved, err: err}
	}
}

// applyCodeAction applies one code action: its edit first, then its command, in
// that order even when the edit has to read files. It returns the command that
// runs after the edit.
func (m *Model) applyCodeAction(path string, action CodeAction) tea.Cmd {
	after := func(FileEditResult) tea.Cmd {
		if action.Command == nil {
			return nil
		}
		return m.executeCommand(path, *action.Command)
	}
	if action.Edit == nil {
		return after(FileEditResult{})
	}
	m.notice = "Applying " + action.Title + "…"
	return m.applyWorkspaceEdit(*action.Edit, after)
}

// executeCommand runs a code action's command on the server that offered it. Some
// commands ask for an edit, which arrives through the workspace-edit queue.
func (m *Model) executeCommand(path string, command LSPCommand) tea.Cmd {
	port := m.lsp.port
	return func() tea.Msg {
		if err := port.ExecuteCommand(path, command); err != nil {
			return lspErrMsg{err: err}
		}
		return nil
	}
}

// applyServerWorkspaceEdit applies an edit a server asked for with
// workspace/applyEdit, and reports what it did.
func (m *Model) applyServerWorkspaceEdit(edit WorkspaceEdit) tea.Cmd {
	what := "The server's edit"
	if edit.Label != "" {
		what = edit.Label
	}
	return m.applyWorkspaceEdit(edit, m.workspaceEditNotice(what))
}

// workspaceEditNotice is the notice for a workspace edit: what changed, how many
// files were opened as unsaved tabs, and what the server asked that was left
// alone. A partly applied edit (a refused operation stopped the rest) says so: the
// first two reasons are shown, so the refusal and the count of later changes that
// were not applied are both visible.
func (m *Model) workspaceEditNotice(what string) func(FileEditResult) tea.Cmd {
	return func(res FileEditResult) tea.Cmd {
		parts := []string{what}
		var changed []string
		if res.Files > 0 {
			changed = append(changed, plural(res.Files, "file changed", "files changed"))
		}
		if res.Operations > 0 {
			changed = append(changed, plural(res.Operations, "file operation", "file operations"))
		}
		if len(changed) == 0 && len(res.Skipped) == 0 {
			changed = append(changed, "changed nothing")
		}
		parts = append(parts, changed...)
		if res.Opened > 0 {
			parts = append(parts, fmt.Sprintf("%d opened with unsaved changes", res.Opened))
		}
		if len(res.Skipped) > 0 {
			reasons := res.Skipped
			if len(reasons) > 2 {
				reasons = reasons[:2]
			}
			parts = append(parts, fmt.Sprintf("%d left alone (%s)", len(res.Skipped), strings.Join(reasons, "; ")))
		}
		m.notice = strings.Join(parts, "; ")
		return nil
	}
}

// plural renders n with the singular or plural form.
func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}

// lspPopup opens a popup at the caret with title and items. It returns the popup,
// so the completion list can read its highlight later.
func (m *Model) lspPopup(title string, items []popup.Item) *popup.Popup {
	return m.lspPopupSelected(title, items, 0)
}

// lspPopupSelected is lspPopup with the highlight on row selected: a rebuilt
// completion list keeps the candidate the user had highlighted.
func (m *Model) lspPopupSelected(title string, items []popup.Item, selected int) *popup.Popup {
	var anchor popup.Point
	if x, y, ok := m.caret(); ok {
		anchor = popup.Point{X: x, Y: y}
	}
	p := popup.New(popupStyles(m.styles))
	p.Show(anchor, title, items)
	p.Select(selected)
	m.pushOverlay(p)
	return p
}

// acceptCompletion accepts the completion at index of c. An item whose lazy fields
// the server still has to fill in (an auto-import with completionItem/resolve) is
// resolved in a command first; everything else is inserted now. The item's range is
// replaced when the server gave one, otherwise the text goes at the caret. All edits
// of the item are applied as one edit, guarded by the version the list was asked at,
// so a changed document or overlapping ranges writes nothing and says why.
func (m *Model) acceptCompletion(c *lspCompletion, index int) tea.Cmd {
	if c == nil || index < 0 || index >= len(c.items) {
		return nil
	}
	item := c.items[index]
	if cmd, handled := m.resolveMultiCursorAccept(c, item); handled {
		return cmd
	}
	if m.multiCursorAccept(c, item) {
		return nil
	}
	if item.ResolveToken != 0 {
		resolver, ok := m.lsp.port.(CompletionResolver)
		if !ok {
			m.closeCompletion(c)
			m.notice = uiText("gvim.notice.completion.unresolved", "completion: this server item cannot be resolved")
			return nil
		}
		m.closeCompletion(c)
		return m.resolveCompletion(resolver, c, item.ResolveToken)
	}
	err := m.applyCompletionItem(c, item)
	m.closeCompletion(c)
	if err != nil {
		m.notice = err.Error()
	}
	return nil
}

// resolveCompletion asks the server for the lazy fields of an incomplete item in a
// command. The reply applies the resolved item with the same guard and atomic edit.
func (m *Model) resolveCompletion(resolver CompletionResolver, c *lspCompletion, token int) tea.Cmd {
	comp, path := c, c.path
	return func() tea.Msg {
		item, err := resolver.ResolveCompletion(path, token)
		return completionResolvedMsg{comp: comp, item: item, err: err}
	}
}

// completionResolved applies the item a completionItem/resolve reply filled in. A
// failed resolve changes nothing and says why.
func (m *Model) completionResolved(msg completionResolvedMsg) tea.Cmd {
	if msg.err != nil {
		m.notice = uiTextVars("gvim.notice.completion.error", "completion: %{error}", map[string]any{"error": msg.err.Error()})
		return nil
	}
	if err := m.applyCompletionItem(msg.comp, msg.item); err != nil {
		m.notice = err.Error()
	}
	return nil
}

// completionDocumentVersioner is the optional editor view that reports an open
// document's version. A multi-caret resolve refuses to start without it: the async
// reply needs a version captured at acceptance to prove the document did not change.
type completionDocumentVersioner interface {
	DocumentVersion(path string) (editor.Version, bool)
}

// resolveMultiCursorAccept handles an item whose lazy fields need
// completionItem/resolve while the document has extra carets. It captures the
// document version and every caret before the request and applies the reply only
// when both are still identical. It reports whether it handled the acceptance.
func (m *Model) resolveMultiCursorAccept(c *lspCompletion, item CompletionItem) (tea.Cmd, bool) {
	if item.ResolveToken == 0 {
		return nil, false
	}
	doc := m.ed.Active()
	if doc == nil || docPath(doc) != c.path {
		return nil, false
	}
	lister, ok := doc.(editorpane.CaretLister)
	if !ok || len(lister.Cursors()) == 0 {
		return nil, false
	}
	resolver, ok := m.lsp.port.(CompletionResolver)
	if !ok {
		m.closeCompletion(c)
		m.notice = uiText("gvim.notice.completion.unresolved", "completion: this server item cannot be resolved")
		return nil, true
	}
	if _, ok := m.ed.(completionItemAtCarets); !ok {
		m.closeCompletion(c)
		m.notice = uiText("gvim.notice.completion.resolve_unsupported", "completion: multi-cursor resolved acceptance is not available in this editor")
		return nil, true
	}
	versioner, ok := m.ed.(completionDocumentVersioner)
	if !ok {
		m.closeCompletion(c)
		m.notice = uiText("gvim.notice.completion.resolve_version_unverifiable", "completion: a multi-cursor resolve cannot verify the document version here; collapse the extra carets and accept with one cursor")
		return nil, true
	}
	version, versionOK := versioner.DocumentVersion(c.path)
	if !versionOK {
		m.closeCompletion(c)
		m.notice = uiText("gvim.notice.completion.resolve_version_unverifiable", "completion: a multi-cursor resolve cannot verify the document version here; collapse the extra carets and accept with one cursor")
		return nil, true
	}
	state := completionMultiResolve{path: c.path, version: version, carets: completionCaretHeads(doc)}
	if len(state.carets) < 2 {
		return nil, false
	}
	m.closeCompletion(c)
	return m.resolveMultiCompletion(resolver, state, item.ResolveToken), true
}

// resolveMultiCompletion asks the server for the lazy fields in a command; the reply
// carries the snapshot taken by the accept, never a pointer into a live document.
func (m *Model) resolveMultiCompletion(resolver CompletionResolver, state completionMultiResolve, token int) tea.Cmd {
	path := state.path
	return func() tea.Msg {
		item, err := resolver.ResolveCompletion(path, token)
		return completionMultiResolvedMsg{state: state, item: item, err: err}
	}
}

// completionCaretHeads returns the primary caret followed by the extra carets, in the
// order the editor reports them. It is both the snapshot a resolve validates and the
// current state it compares against.
func completionCaretHeads(doc Document) []buffer.Pos {
	heads := []buffer.Pos{doc.Cursor()}
	if lister, ok := doc.(editorpane.CaretLister); ok {
		for _, c := range lister.Cursors() {
			heads = append(heads, c.Cursor)
		}
	}
	return heads
}

// completionMultiResolved applies a multi-caret resolve reply. The lazy edits were
// computed against the document the accept saw, so it writes nothing when the active
// document, its version or any caret changed while the request ran. The resolved
// insert, snippet stop and additional (import) edits still go through the editor's one
// atomic, version-guarded multi-caret path, so the whole accept is one undo step.
func (m *Model) completionMultiResolved(msg completionMultiResolvedMsg) tea.Cmd {
	if msg.err != nil {
		m.notice = uiTextVars("gvim.notice.completion.error", "completion: %{error}", map[string]any{"error": msg.err.Error()})
		return nil
	}
	doc := m.ed.Active()
	if doc == nil || docPath(doc) != msg.state.path {
		m.notice = uiText("gvim.notice.completion.resolve_inactive", "completion: the document is no longer active, so the resolved completion was not applied")
		return nil
	}
	versioner, ok := m.ed.(completionDocumentVersioner)
	if !ok {
		m.notice = uiText("gvim.notice.completion.resolve_version_lost", "completion: the document version can no longer be verified, so the resolved completion was not applied")
		return nil
	}
	version, versionOK := versioner.DocumentVersion(msg.state.path)
	if !versionOK || version != msg.state.version {
		m.notice = uiText("gvim.notice.completion.resolve_stale", "completion is stale: the document changed while the item was being resolved, so nothing was applied")
		return nil
	}
	if heads := completionCaretHeads(doc); !slices.Equal(heads, msg.state.carets) {
		m.notice = uiText("gvim.notice.completion.resolve_cursors_changed", "completion: the cursors changed while the item was being resolved, so nothing was applied")
		return nil
	}
	accepter, ok := m.ed.(completionItemAtCarets)
	if !ok {
		m.notice = uiText("gvim.notice.completion.resolve_unsupported", "completion: multi-cursor resolved acceptance is not available in this editor")
		return nil
	}
	additional := make([]editor.LSPEdit, 0, len(msg.item.Additional))
	for _, add := range msg.item.Additional {
		additional = append(additional, editor.LSPEdit{From: add.Range.Start, To: add.Range.End, Text: add.Text})
	}
	cursorOffset := -1
	if msg.item.Snippet && msg.item.CursorOffset >= 0 {
		cursorOffset = msg.item.CursorOffset
	}
	wordChars := m.docLanguageSettings(doc).WordCharacters
	expect := msg.state.version
	if _, err := accepter.ApplyCompletionItemAtCarets(msg.state.path, msg.item.Insert, cursorOffset, additional, wordChars, &expect); err != nil {
		m.notice = completionAcceptNotice(err)
	}
	return nil
}

// completionAtCarets is the optional editor view that accepts one completion at every
// caret as one atomic edit with a version guard. The app adapter implements it; a fake
// or an editor without it keeps the single-caret accept path.
type completionAtCarets interface {
	ApplyCompletionAtCarets(path, text, wordChars string, expect *editor.Version) (int, error)
}

// completionItemAtCarets is the optional editor view for a snippet/auto-import accept at
// every caret: one atomic edit with the per-caret insert text, the snippet's rune stop and
// the additional (import) edits, guarded by the request version. An editor without it
// keeps the explicit refusal.
type completionItemAtCarets interface {
	ApplyCompletionItemAtCarets(path, text string, cursorOffset int, additional []editor.LSPEdit, wordChars string, expect *editor.Version) (int, error)
}

// multiCursorAccept accepts the completion at every caret when the active document has
// extra carets. It reports whether it handled the acceptance. It follows Fresh's
// insert_completion_text: each caret replaces its own identifier prefix with the same
// text in one atomic edit (one undo step), and the version captured at the request stays
// the guard. A snippet's rune stop places every caret inside its own insertion; the
// additional (auto-import) edits are applied once in the same atomic edit. A resolve
// item is handled earlier by resolveMultiCursorAccept, which validates the snapshot its
// async reply is applied against.
func (m *Model) multiCursorAccept(c *lspCompletion, item CompletionItem) bool {
	doc := m.ed.Active()
	if doc == nil || docPath(doc) != c.path {
		return false
	}
	lister, ok := doc.(editorpane.CaretLister)
	if !ok || len(lister.Cursors()) == 0 {
		return false
	}
	var expect *editor.Version
	if c.versionOK {
		expect = &c.version
	}
	wordChars := m.docLanguageSettings(doc).WordCharacters
	var err error
	if item.Snippet || len(item.Additional) > 0 {
		accepter, ok := m.ed.(completionItemAtCarets)
		if !ok {
			m.closeCompletion(c)
			m.notice = uiText("gvim.notice.completion.accept_snippet_import_unsupported", "completion: multi-cursor snippet/import acceptance is not available in this editor")
			return true
		}
		additional := make([]editor.LSPEdit, 0, len(item.Additional))
		for _, add := range item.Additional {
			additional = append(additional, editor.LSPEdit{From: add.Range.Start, To: add.Range.End, Text: add.Text})
		}
		cursorOffset := -1
		if item.Snippet && item.CursorOffset >= 0 {
			cursorOffset = item.CursorOffset
		}
		_, err = accepter.ApplyCompletionItemAtCarets(c.path, item.Insert, cursorOffset, additional, wordChars, expect)
	} else {
		accepter, ok := m.ed.(completionAtCarets)
		if !ok {
			m.closeCompletion(c)
			m.notice = uiText("gvim.notice.completion.accept_unsupported", "completion: multi-cursor acceptance is not available in this editor")
			return true
		}
		_, err = accepter.ApplyCompletionAtCarets(c.path, item.Insert, wordChars, expect)
	}
	m.closeCompletion(c)
	if err != nil {
		m.notice = completionAcceptNotice(err)
	}
	return true
}

// completionAcceptNotice is the status-row text for a refused multi-caret accept.
func completionAcceptNotice(err error) string {
	switch {
	case errors.Is(err, editor.ErrStale):
		return uiText("gvim.notice.completion.offer_stale", "completion is stale: the document changed, so nothing was applied")
	case errors.Is(err, editor.ErrOverlappingEdits):
		return uiText("gvim.notice.completion.offer_overlap", "completion edits overlap, so nothing was applied")
	}
	return err.Error()
}

// applyCompletionItem inserts one accepted item and its additional edits (an
// auto-import) as a single edit. The ranges are checked for overlap first, and the
// document version captured at the request is passed as the expected version, so
// stale items and overlapping edits write nothing.
func (m *Model) applyCompletionItem(c *lspCompletion, item CompletionItem) error {
	from, to := c.at, c.at
	if item.Range != nil {
		from, to = item.Range.Start, item.Range.End
	}
	edits := make([]editor.LSPEdit, 0, len(item.Additional)+1)
	edits = append(edits, editor.LSPEdit{From: from, To: to, Text: item.Insert})
	for _, add := range item.Additional {
		edits = append(edits, editor.LSPEdit{From: add.Range.Start, To: add.Range.End, Text: add.Text})
	}
	if err := completionEditConflict(edits); err != nil {
		return err
	}
	var expect *editor.Version
	if c.versionOK {
		expect = &c.version
	}
	err := m.applyLSPEditsExpect(c.path, edits, expect)
	if err != nil {
		switch {
		case errors.Is(err, editor.ErrStale):
			return errors.New("completion is stale: the document changed, so nothing was applied")
		case errors.Is(err, editor.ErrOverlappingEdits):
			return errors.New("completion edits overlap, so nothing was applied")
		}
		return err
	}
	if doc := m.ed.Active(); doc != nil && docPath(doc) == c.path {
		m.ed.SetCursor(completionCaret(from, item), false)
	}
	return nil
}

// completionEditConflict reports whether the main edit and the additional edits of
// an item overlap (or two zero-width edits sit at the same place). An ambiguous
// edit is refused before it touches the document.
func completionEditConflict(edits []editor.LSPEdit) error {
	for i := range edits {
		if posLess(edits[i].To, edits[i].From) {
			return errors.New("completion edit has an inverted range, so nothing was applied")
		}
		for j := i + 1; j < len(edits); j++ {
			if completionRangesOverlap(edits[i].From, edits[i].To, edits[j].From, edits[j].To) {
				return errors.New("completion edits overlap, so nothing was applied")
			}
		}
	}
	return nil
}

// completionRangesOverlap reports whether the half-open ranges [aFrom,aTo) and
// [bFrom,bTo) intersect. Two insertions at the same point also conflict, because
// their order is ambiguous.
func completionRangesOverlap(aFrom, aTo, bFrom, bTo buffer.Pos) bool {
	if aFrom == bFrom && aTo == aFrom && bTo == bFrom {
		return true
	}
	return posLess(aFrom, bTo) && posLess(bFrom, aTo)
}

// posLess reports whether a is before b.
func posLess(a, b buffer.Pos) bool {
	if a.Line != b.Line {
		return a.Line < b.Line
	}
	return a.Col < b.Col
}

// posAfterText returns the position just after text inserted at from.
func posAfterText(from buffer.Pos, text string) buffer.Pos {
	p := from
	for _, r := range text {
		if r == '\n' {
			p.Line++
			p.Col = 0
			continue
		}
		p.Col++
	}
	return p
}

// completionCaret is where the caret goes after the item's edits: inside the
// inserted text at the snippet's final stop (or its end), shifted down/right by the
// additional edits that apply before the main position (auto-imports are whole lines
// above it). An additional edit that does not end before the main edit never moves
// the caret.
func completionCaret(from buffer.Pos, item CompletionItem) buffer.Pos {
	at := from
	for _, add := range item.Additional {
		if !posLess(add.Range.End, from) && add.Range.End != from {
			continue
		}
		if add.Range.End.Line < at.Line {
			at.Line += countNewlines(add.Text) - (add.Range.End.Line - add.Range.Start.Line)
			continue
		}
		if add.Range.End.Line == at.Line && add.Range.Start.Line == add.Range.End.Line {
			at.Col += len([]rune(add.Text)) - (add.Range.End.Col - add.Range.Start.Col)
		}
	}
	insert := []rune(item.Insert)
	offset := len(insert)
	if item.Snippet && item.CursorOffset >= 0 && item.CursorOffset <= offset {
		offset = item.CursorOffset
	}
	return posAfterText(at, string(insert[:offset]))
}

// countNewlines returns the number of line breaks in s.
func countNewlines(s string) int {
	n := 0
	for _, r := range s {
		if r == '\n' {
			n++
		}
	}
	return n
}

// closeCompletion closes the list c: its popup leaves the stack, and the root forgets
// the list when it is still the open one.
func (m *Model) closeCompletion(c *lspCompletion) {
	if m.lsp.comp == c {
		m.lsp.comp = nil
	}
	// Closing the list (Esc, dismiss, accepting, typing) also cancels a pending
	// auto-popup trigger, so it cannot reopen a list the user closed.
	m.cancelAutoCompletion()
	m.closePopup(c.pop)
}

// closePopup removes the popup overlay that holds p from the stack.
func (m *Model) closePopup(p *popup.Popup) {
	m.overlays = slices.DeleteFunc(m.overlays, func(o Overlay) bool {
		po, ok := o.(*popup.Popup)
		return ok && po == p
	})
}

// onStack reports whether the popup overlay that holds p is on the overlay stack.
func (m *Model) onStack(p *popup.Popup) bool {
	return slices.ContainsFunc(m.overlays, func(o Overlay) bool {
		po, ok := o.(*popup.Popup)
		return ok && po == p
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
	if top, ok := m.overlays[len(m.overlays)-1].(*popup.Popup); !ok || top != c.pop {
		return nil, false
	}
	switch tea.Key(k).Code {
	case tea.KeyTab:
		return m.acceptCompletion(c, c.pop.Selected()), true
	case tea.KeyBackspace:
		if cmd, handled := m.completionBackspace(c); handled {
			return cmd, true
		}
		// The list belongs to an older caret/document: dismiss and let the normal
		// key path delete.
		m.closeCompletion(c)
		return nil, false
	case tea.KeyUp, tea.KeyDown, tea.KeyPgUp, tea.KeyPgDown, tea.KeyHome, tea.KeyEnd, tea.KeyEnter, tea.KeyEscape:
		return nil, false
	}
	// A printable word character narrows the open list instead of closing it
	// (Fresh's live re-filter): the character is typed and the rows are filtered by
	// the longer prefix. Anything else keeps the old dismissal.
	if _, text := translateKey(tea.Key(k)); text != "" {
		if cmd, handled := m.completionTyped(c, text); handled {
			return cmd, true
		}
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
		return m.completionAsked(), true
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
		// Plugin API tier 1a: the status pill was clicked; plugins that put their own
		// server state in the UI can follow.
		m.plugEmit("lsp_status_clicked", map[string]any{})
		return m.lspStatusCmd(), true
	case "toggle_diagnostics_panel":
		return m.toggleDiagnosticsPanel(), true
	case "open_diagnostics_list":
		return m.openDiagnosticsList(), true
	case "lsp_enable_language":
		return m.lspSetLanguageEnabled(true), true
	case "lsp_disable_language":
		return m.lspSetLanguageEnabled(false), true
	case "toggle_inlay_hints":
		return m.lspToggleInlayHints(), true
	case "completion_accept":
		if c := m.lsp.comp; c != nil {
			return m.acceptCompletion(c, c.pop.Selected()), true
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
			if ve, ok := m.ed.(lspVersionedEditor); ok {
				r.version, r.versionOK = ve.DocumentVersion(path)
			}
			r.items, r.err = port.Completion(path, at)
		case lspAskRename:
			r.edits, r.err = port.Rename(path, at, name)
		case lspAskCodeActions:
			r.actions, r.err = port.CodeActions(path, at)
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
	m.pushOverlay(prompt.NewLine(m.styles, prompt.LineOptions{
		Label: uiText("gvim.prompt.new_name", "New name:"),
		Input: lspWordAt(doc.Line(at.Line), at.Col),
		Tag:   lspRenameTag{path: path, at: at},
	}))
}

// lspRenameDone starts the rename when the prompt was submitted with a name.
func (m *Model) lspRenameDone(tag lspRenameTag, d prompt.Done) tea.Cmd {
	name := strings.TrimSpace(d.Text)
	if d.Outcome != prompt.Submitted || name == "" {
		return nil
	}
	return m.lspRequest(lspAskRename, tag.path, tag.at, name)
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
			err = port.Restart(lang)
		} else {
			err = port.Stop()
		}
		return lspServerMsg{action: action, lang: lang, err: err}
	}
}

// lspStatusSep joins the parts of a status-popup row value.
const lspStatusSep = "\x00"

// lspStatusCmd opens the status popup: the configured servers with their state and
// a restart or stop row each, and a mute row for the active document's language.
// A document the servers cannot serve (too large, LSP off for the buffer, no
// document) gets the reason as a notice instead: the popup would not explain it.
func (m *Model) lspStatusCmd() tea.Cmd {
	port := m.lsp.port
	if port == nil {
		m.notice = "LSP: no language server is configured"
		return nil
	}
	doc := m.ed.Active()
	if doc == nil {
		m.notice = "LSP: no open document"
		return nil
	}
	lang, path := m.lspLanguage(doc), docPath(doc)
	switch {
	case m.lsp.unavailable[lang] != nil:
		m.notice = "LSP: " + m.lsp.unavailable[lang].Error()
		return nil
	case m.lsp.stopped:
		m.notice = "LSP: stopped"
		return nil
	case m.lsp.muted[lang]:
		m.notice = "LSP: muted for " + lang + " (the status popup enables it)"
		return nil
	case lspLargeReason(doc) != "":
		m.notice = "LSP: " + lspLargeReason(doc)
		return nil
	case path != "" && m.lsp.off[path]:
		m.notice = "LSP: off for this buffer"
		return nil
	}
	return func() tea.Msg { return lspStatusMsg{servers: port.Servers()} }
}

// openLSPStatus lists the configured servers in a view: one block per server with
// its state and a restart or stop row, then a row that disables or enables the
// language of the active document (mute). A row with no value, such as a server
// header or an install hint, does nothing when chosen.
func (m *Model) openLSPStatus(msg lspStatusMsg) {
	rows := make([]list.Row, 0, len(msg.servers)+8)
	group := ""
	for _, s := range msg.servers {
		name := s.Language
		if s.Universal {
			name = "universal"
		}
		if name != group {
			group = name
			rows = append(rows, list.Row{Header: true, Left: "Language servers: " + group})
		}
		rows = append(rows, list.Row{Left: lspServerGlyph(s.State) + " " + s.Name, Right: lspServerStateLabel(s.State)})
		if s.Error != "" {
			rows = append(rows, list.Row{Left: "    " + s.Error})
		} else if s.State == LSPServerStopped && s.Install != "" {
			rows = append(rows, list.Row{Left: "    install: " + s.Install})
		}
		if s.Limits != "" {
			rows = append(rows, list.Row{Left: "    " + s.Limits})
		}
		key := s.Language + lspStatusSep + s.Name
		if s.State == LSPServerRunning || s.State == LSPServerStarting {
			rows = append(rows,
				list.Row{Left: "    Restart " + s.Name, Value: "restart" + lspStatusSep + key},
				list.Row{Left: "    Stop " + s.Name, Value: "stop" + lspStatusSep + key},
			)
		} else {
			rows = append(rows, list.Row{Left: "    Start " + s.Name, Value: "restart" + lspStatusSep + key})
		}
	}
	lang := ""
	if doc := m.ed.Active(); doc != nil {
		lang = m.lspLanguage(doc)
	}
	if lang != "" {
		if m.lsp.muted[lang] {
			rows = append(rows, list.Row{Left: "Unmute LSP for " + lang, Value: "unmute" + lspStatusSep + lang})
		} else {
			rows = append(rows, list.Row{Left: "Mute LSP for " + lang, Value: "mute" + lspStatusSep + lang})
		}
	}
	if len(rows) == 0 {
		m.notice = "No language servers are configured"
		return
	}
	v := list.New("Language Servers", rows, m.listChrome(), func(v *list.View, r list.Row) tea.Cmd {
		v.Close()
		if r.Value == "" {
			return nil
		}
		return m.lspStatusRowChosen(r.Value)
	})
	v.Fit = true
	m.pushOverlay(v)
}

// lspStatusRowChosen runs the action of a status-popup row.
func (m *Model) lspStatusRowChosen(value string) tea.Cmd {
	parts := strings.Split(value, lspStatusSep)
	switch parts[0] {
	case "restart", "stop":
		if len(parts) != 3 {
			return nil
		}
		return m.lspServerAction(parts[0], parts[1], parts[2])
	case "mute":
		if len(parts) != 2 {
			return nil
		}
		return m.lspSetLanguage(true, parts[1])
	case "unmute":
		if len(parts) != 2 {
			return nil
		}
		return m.lspSetLanguage(false, parts[1])
	}
	return nil
}

// lspServerAction is the command that restarts or stops one server from the popup.
func (m *Model) lspServerAction(action, language, name string) tea.Cmd {
	port := m.lsp.port
	return func() tea.Msg {
		var err error
		if action == "restart" {
			err = port.RestartServer(language, name)
		} else {
			err = port.StopServer(language, name)
		}
		return lspServerActionMsg{action: action, language: language, name: name, err: err}
	}
}

// lspSetLanguage mutes or unmutes a language. mute is the action's meaning: the
// language-level switch of Fresh's status popup.
func (m *Model) lspSetLanguage(mute bool, language string) tea.Cmd {
	port := m.lsp.port
	enabled := !mute
	return func() tea.Msg {
		return lspServerActionMsg{action: map[bool]string{true: "mute", false: "unmute"}[mute], language: language, err: port.SetLanguageEnabled(language, enabled)}
	}
}

// lspSetLanguageEnabled runs the mute or unmute action for the language of the
// active document.
func (m *Model) lspSetLanguageEnabled(enabled bool) tea.Cmd {
	doc := m.ed.Active()
	if doc == nil || docPath(doc) == "" {
		m.notice = noDocumentNotice
		return nil
	}
	lang := m.lspLanguage(doc)
	if enabled && m.lsp.port == nil {
		m.notice = m.lspNoServer(lang)
		return nil
	}
	return m.lspSetLanguage(!enabled, lang)
}

// lspServerActionDone applies the end of a status-popup action.
func (m *Model) lspServerActionDone(msg lspServerActionMsg) tea.Cmd {
	s := &m.lsp
	if msg.err != nil {
		m.notice = msg.err.Error()
	}
	switch msg.action {
	case "restart":
		s.stale = true
		if msg.err == nil {
			m.notice = "Started " + msg.name
		}
	case "stop":
		s.stale = true
		if msg.err == nil {
			m.notice = "Stopped " + msg.name
		}
	case "mute":
		if s.muted == nil {
			s.muted = make(map[string]bool)
		}
		s.muted[msg.language] = true
		m.lspForgetLanguage(msg.language)
		s.stale = true
		if msg.err == nil {
			m.notice = uiTextVars("gvim.notice.lsp.muted", "LSP is muted for %{language}", map[string]any{"language": msg.language})
		}
	case "unmute":
		delete(s.muted, msg.language)
		s.stale = true
		if msg.err == nil {
			m.notice = uiTextVars("gvim.notice.lsp.on", "LSP is on for %{language}", map[string]any{"language": msg.language})
		}
	}
	return nil
}

// lspForgetLanguage forgets the tracks and diagnostics of a language whose
// servers were muted.
func (m *Model) lspForgetLanguage(language string) {
	s := &m.lsp
	var forgotten []string
	for path, tr := range s.tracks {
		if tr.lang != language {
			continue
		}
		forgotten = append(forgotten, path)
		delete(s.tracks, path)
	}
	for _, path := range forgotten {
		m.lspDropDiags(path)
	}
	s.polling = false
}

// lspServerGlyph is the one-cell state mark of a server row.
func lspServerGlyph(state LSPServerRunState) string {
	switch state {
	case LSPServerRunning:
		return "●"
	case LSPServerStarting:
		return "◌"
	case LSPServerFailed:
		return "✗"
	}
	return "○"
}

// lspServerStateLabel is the state of a server as a row says it.
func lspServerStateLabel(state LSPServerRunState) string {
	switch state {
	case LSPServerRunning:
		return "ready"
	case LSPServerStarting:
		return "starting"
	case LSPServerFailed:
		return "error"
	}
	return "not running"
}

// lspToggleInlayHints turns the inlay hints on or off. Turning them off drops the
// hints the root holds, so the next frame paints none; turning them on asks the
// server again.
func (m *Model) lspToggleInlayHints() tea.Cmd {
	s := &m.lsp
	s.inlay = !s.inlay
	if !s.inlay {
		s.hints = nil
		m.notice = i18n.T("", "gvim.lsp.inlay_hints_off", nil)
		return nil
	}
	doc := m.ed.Active()
	if doc == nil || s.port == nil {
		m.notice = i18n.T("", "gvim.lsp.inlay_hints_on", nil)
		return nil
	}
	lang := m.lspLanguage(doc)
	if reason := s.hintErr[lang]; reason != "" {
		m.notice = "Inlay hints are not available: " + reason
		return nil
	}
	m.notice = i18n.T("", "gvim.lsp.inlay_hints_on", nil)
	return m.lspDataArm(lspDataHints)
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
		m.notice = uiText("gvim.notice.lsp.on_buffer", "LSP is on for this buffer")
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
	m.lspDropData(path)
	if tr != nil && tr.state != lspClosed {
		return m.lspCloseCmd(path)
	}
	return nil
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
	path := docPath(doc)
	ds := m.lsp.diags[path]
	if len(ds) == 0 {
		return nil
	}
	stale := m.lspDiagsStale(path)
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
			out = append(out, editorpane.DiagnosticSpan{Start: start, End: end, Severity: d.Severity, Message: d.Message, Stale: stale})
		}
		return out
	}
}

// lspDiagsStale reports whether the stored diagnostics of path were published for
// an older version of the document than the server now has. Such diagnostics are
// shown dimmed: the text has moved on since the server found them.
func (m *Model) lspDiagsStale(path string) bool {
	dv, ok := m.lsp.diagVersion[path]
	if !ok || !dv.versioned {
		return false
	}
	tr := m.lsp.tracks[path]
	if tr == nil || !tr.lspVersionOK {
		return false
	}
	return dv.version < tr.lspVersion
}

// lspDiagCounts counts the diagnostics of the active document, for the status row's
// badge. It splits them as Fresh's diagnostics element does: errors, warnings, and info
// and hints together.
func (m *Model) lspDiagCounts() frame.DiagCounts {
	doc := m.ed.Active()
	if doc == nil {
		return frame.DiagCounts{}
	}
	var c frame.DiagCounts
	for _, d := range m.lsp.diags[docPath(doc)] {
		switch d.Severity {
		case editorpane.SeverityError:
			c.Errors++
		case editorpane.SeverityWarning:
			c.Warnings++
		default:
			c.Info++
		}
	}
	return c
}

// lspClose stops the servers when the program ends.
func (m *Model) lspClose() {
	if m.lsp.port != nil {
		_ = m.lsp.port.Stop()
	}
}

// enabled reports whether a server serves language lang and the root has not
// stopped or muted it.
func (s *lspState) enabled(lang string) bool {
	return s.port != nil && !s.stopped && !s.muted[lang] && s.unavailable[lang] == nil && (s.forced[lang] || s.port.Supports(lang))
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
