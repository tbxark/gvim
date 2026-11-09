package tui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/tbxark/gvim/buffer"
	commands "github.com/tbxark/gvim/commands"
	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/explorer"
	"github.com/tbxark/gvim/internal/tui/browser"
	"github.com/tbxark/gvim/internal/tui/ctxmenu"
	"github.com/tbxark/gvim/internal/tui/dialog"
	"github.com/tbxark/gvim/internal/tui/dock"
	"github.com/tbxark/gvim/internal/tui/editorpane"
	expanel "github.com/tbxark/gvim/internal/tui/explorer"
	"github.com/tbxark/gvim/internal/tui/find"
	"github.com/tbxark/gvim/internal/tui/frame"
	"github.com/tbxark/gvim/internal/tui/grid"
	"github.com/tbxark/gvim/internal/tui/menubar"
	"github.com/tbxark/gvim/internal/tui/palette"
	"github.com/tbxark/gvim/internal/tui/prompt"
	"github.com/tbxark/gvim/internal/tui/searchpanel"
	"github.com/tbxark/gvim/internal/tui/sidebar"
	"github.com/tbxark/gvim/internal/tui/space"
	"github.com/tbxark/gvim/internal/tui/style"
	"github.com/tbxark/gvim/internal/tui/tabstrip"
	"github.com/tbxark/gvim/syntax"
	"github.com/tbxark/gvim/theme"
)

// wheelRows is the number of rows one wheel notch scrolls (blueprint section 3.2).
const wheelRows = 3

// doubleClickWindow is the time within which presses in one cell count as a
// double or triple press (blueprint section 3.4).
const doubleClickWindow = 500 * time.Millisecond

// Model is the root tea.Model. It routes input to the editor, the explorer and
// the sub-models, holds the focus, and lays out the frame. The root does no file
// IO itself. Explorer moves and scrolls are in memory. Editor actions run
// synchronously in Update through EditorState.Do, because the editor is owned by
// one goroutine and View reads it too. An action that writes a file (save) does
// its write there. Moving that work into a tea.Cmd needs a snapshot of the
// document first, which the editor pane phase adds.
type Model struct {
	ed     EditorState
	tree   ExplorerTree // nil when there is no explorer
	keys   Keymap
	styles style.Styles
	panes  editorpane.Styles
	now    func() time.Time
	theme  theme.Theme
	// themeRef is the portable config value of the active theme, and
	// themeOverrides are the session color overrides layered over it.
	themeRef       string
	themeOverrides map[string]string

	// The palette's sources. The commands always have a table, and files and
	// ranker are nil when Deps leaves them out.
	commands []commands.Entry
	// updateCommands are the Fresh updater rows (Update fresh / Show update output),
	// offered in the palette only when UpdateHooks.Command is configured.
	updateCommands []commands.Entry
	files          palette.Files
	ranker         palette.Ranker
	recorder       fileRecorder // the ranker's frecency record, when it has one

	// fileRoot is the palette file-mode root when it is not the explorer's root (a
	// remote workspace's remote path); empty means the explorer root. A non-empty
	// value also marks the palette as remote (slash joins, no local preview).
	fileRoot string
	// proj is the project-search state: the last panel, kept after it closes so the
	// next and previous match actions can walk its results, and the replacement
	// waiting for its confirmation.
	proj projectSearchState
	// findBar is the last find bar. It stays after the bar closes, so find_next and
	// find_previous can run on its search text. The bar is on the overlay stack
	// only while findOpen reports it.
	findBar *find.Bar
	// searchSeed is the find state of an earlier run, for the first find bar. It is
	// nil when there is none.
	searchSeed *find.State

	width, height int
	// statusRestartHover is the pointer over the clickable {terminal_restart}
	// status element; its hover hit area is the same run the click uses.
	statusRestartHover bool
	explorerOpen       bool // the panel is shown
	explorerFocus      bool // the panel has keyboard focus
	tabFocus           bool // the tab strip has keyboard focus (F6 cycle)
	// The Contents sidebar section (see markdown_toc.go): contents is its tree
	// and settings, contentsFocus says it holds the keyboard, and
	// explorerCollapsed folds the tree when the section is stacked under it.
	contents          sidebar.Contents
	contentsFocus     bool
	explorerCollapsed bool
	// diag is the native Diagnostics sidebar section (see diagpanel.go). It
	// keeps the section's open/collapsed/rows/selection state; the diagnostics
	// themselves are in the root's workspace store (lspState.wsDiags).
	diag       diagState
	toc        tocState
	divider    dividerState
	chord      []KeyPress // the first keys of a multi-key binding, waiting for the next
	clockArmed bool       // a {clock} redraw tick is in flight (see clockCmd)
	dialogs    Dialogs    // the file dialogs; nil disables them
	// hideMenuBar comes from Deps and stays put. hideLineNumbers starts from
	// Deps and is flipped by toggle_line_numbers.
	hideMenuBar     bool
	hideLineNumbers bool
	// tabSize is the columns per tab stop from Deps.TabSize. Zero means the pane's default.
	tabSize int
	// explorerWidth and explorerSide are file_explorer.width and .side as the
	// session runs them: the config value, then the changes a grip drag and
	// toggle_file_explorer_side make. The zero width is the frame's automatic
	// width and the zero side is the left edge.
	explorerWidth explorer.Width
	explorerSide  explorer.Side
	// explorerAutoOpened reports that auto_open_on_last_buffer_close focused the
	// explorer; a buffer opening afterwards gives the keyboard back to the editor.
	explorerAutoOpened bool
	// restoredScroll skips the first follow of a restored tree: the save kept the
	// selection and scroll, and the startup reveal must not scroll back to row 0.
	restoredScroll bool

	overlays []Overlay // the stack; the last one is on top and gets the input first

	// rowTop is the view's offset into a wrapped top line, see viewrow.go.
	rowTop rowTop

	menu          menubar.Model // the menu bar and its open dropdown
	explorerPanel expanel.Panel // paints the explorer and answers its key actions
	ops           explorerOps   // file operations; nil when Deps.Ops lacks them
	clip          *clipboard    // the explorer clipboard; nil when empty
	lastYank      string        // the last text copied or cut, for the terminal's Ctrl+V fallback
	quitting      bool          // set by a menu row that quit the editor
	detach        func()        // ends the attached daemon client; nil when not hosted
	pending       tea.Cmd       // commands a menu row started, returned by routeMenu
	notice        string        // replaces the editor status until the next press
	// startupOpen is a remote path the app parked for the root's first command (see
	// Deps.StartupOpen); startupPending stays set until the asynchronous read succeeds,
	// so an authority reconnect retries a failed startup open and a failure never turns
	// into an empty buffer.
	startupOpen             string
	startupLine, startupCol int
	startupPending          bool
	// updates is the app release check and update the release it found, painted by
	// the status row element {update} (see update_status.go).
	updates    UpdateHooks
	updateInfo *UpdateInfo

	clicks    clickCounter
	lastPress int // the count of the last editor press, see pressEditor
	drag      drag
	mx        mouseExtra // modifier clicks and the sideways pan, see motion_mouse.go
	// langs memoizes languageFor by file name.
	langs map[string]string

	// tabFirst is the tab strip's scroll position, the index of its first tab.
	// placeStrip clamps it; setTabFirst keeps the clamped value here.
	tabFirst int
	// recents are the recent folders the welcome screen lists.
	recents []string
	// welcomeSel is the highlighted row of the welcome screen.
	welcomeSel int
	// splits is the editor's pane layout: the split tree, the focused pane, and
	// whether the panes are a compare view (split.go, compare.go).
	splits splitSet
	// util is the state of the help, settings, keymap, tour and diagnostic overlays.
	util utilities
	// root is the state of the root features (see root.go).
	root rootState
	// trust is the workspace-trust state (see trust_state.go).
	trust trustState
	// env is the workspace environment port for the gvim.env plugin API; nil when
	// the app wired none.
	env EnvPort
	// notes is the log of the notices shown this session (see notice_warnings.go).
	notes noticeLog
	// lsp is the language-server state (see lsp.go).
	lsp lspState
	// dock is the terminal dock under the body (see dock_glue.go and the dock package).
	dock dock.Tabs
	// discover scans for terminal sessions outside gvim, for the Discover Agent
	// Sessions picker (see agent_discover.go). Nil disables the scan.
	discover AgentDiscoverer
	// view is the display state the view commands change (see view.go).
	view viewState
	// occ pauses the occurrence highlight after an edit (see occurrence.go).
	occ occTrack
	// fix is the state of the root fixes (see root_fix.go).
	fix fixState
	// large is the state of the root for documents in large file mode (see large.go).
	large largeState
	// largeWindows loads windows of a remote windowed large document (see
	// large_windows.go). Nil for a local session.
	largeWindows LargeWindowLoader
	// plug is the plugin host state (see plugin_pump.go). Its runtime is nil when
	// plugins are off, and every hook is a nil check then.
	plug pluginState
	// auth is the workspace-authority hook set (see authority_status.go); both are
	// zero for an ordinary local launch.
	auth         AuthorityHooks
	authOpenDock bool
	// Background authority reconnect (authority_status.go): one dial in flight, the
	// capped backoff, the one-shot credential prompt flag and the 2FA answer queue.
	authorityBusy bool
	// authorityRefreshNotice defers a successful reconnect's status notice until the
	// app's post-reconnect work (cache refresh/server restart) reports its outcome, so
	// one notice describes the refreshed workspace instead of two.
	authorityRefreshNotice bool
	authorityBackoff       time.Duration
	authorityPrompted      bool
	authorityQuestions     []string
	authorityAnswers       []string
	authorityEchos         []bool
	// shellRun, when set, runs shell_command/format_buffer jobs on the workspace
	// authority instead of the local editor runner (see authority_status.go).
	shellRun func(editor.ShellJob) (string, error)
	// workspace is the daemon's workspace layer and workspaceLabel the active
	// workspace's status-row label (see workspace_ui.go); both zero for a direct launch.
	workspace      WorkspacePort
	workspaceLabel string
}

// drag is the pointer gesture in progress: a text selection, or a grab of the
// scrollbar thumb, or a split line (split_drag.go). The zero value is no gesture.
type drag struct {
	active bool
	thumb  bool         // the scrollbar thumb: motion moves the viewport
	hbar   bool         // the horizontal scrollbar: motion pans the view sideways
	tab    *tabDrag     // a tab of the strip: motion moves it along the strip
	unit   editor.Unit  // the unit a text drag grows the selection by
	border *splitBorder // the split line the press took: motion moves its split's ratio
	// last is the pointer of the latest motion of a text drag, and tick is the number
	// of the autoscroll tick in flight, or 0 when none is (see dragSelect).
	last tea.Mouse
	tick int
}

var _ tea.Model = (*Model)(nil)

// New returns the root model for deps. The editor, keymap and theme are
// required. A nil Explorer hides the explorer for good. The model starts with
// the explorer hidden and the editor focused. New does not start a program.
func New(deps Deps) *Model {
	now := deps.Clock
	if now == nil {
		now = time.Now
	}
	st := style.New(deps.Theme)
	cmds := deps.Commands
	if cmds == nil {
		cmds = commands.Commands(nil)
	}
	updateRows := hiddenCommandRows(cmds)
	cmds = withoutHidden(cmds)
	m := &Model{
		updateCommands:  updateRows,
		ed:              deps.Editor,
		tree:            deps.Explorer,
		keys:            deps.Keys,
		styles:          st,
		panes:           editorpane.NewStyles(deps.Theme),
		now:             now,
		theme:           deps.Theme,
		themeRef:        deps.ThemeRef,
		themeOverrides:  map[string]string{},
		commands:        cmds,
		recents:         deps.Recents,
		hideMenuBar:     deps.HideMenuBar,
		hideLineNumbers: deps.HideLineNumbers,
		tabSize:         deps.TabSize,
		notice:          deps.Notice,
		updates:         deps.Updates,
		startupOpen:     deps.StartupOpen,
		startupLine:     deps.StartupLine,
		startupCol:      deps.StartupCol,
		startupPending:  deps.StartupOpen != "",
		splits:          newSplitSet(),
		util:            utilities{settings: deps.Settings, settingsPort: deps.SettingsPort, themeCatalog: deps.ThemeCatalog, switchKeymap: deps.SwitchKeymap},
		root:            rootState{ports: deps.Root},
		trust:           newTrustState(deps.Trust),
		detach:          deps.Detach,
		lsp:             lspState{port: deps.LSP},
		env:             deps.Env,
		view:            newViewState(viewOptionsOf(deps.View)),
		largeWindows:    deps.LargeWindows,
	}
	m.view.languages = deps.Languages
	// The inlay-hints toggle starts from the configured display option; the Toggle
	// Inlay Hints command flips it for the rest of the session.
	m.lsp.inlay = m.view.opts.EnableInlayHints
	m.dock = dockNewPanel(m)
	m.notes.sink = deps.NoticeSink
	m.notes.record("", deps.Notice)
	for _, w := range deps.Warnings {
		m.notes.keep(w)
	}
	// Assign only a non-nil source: a nil concrete value in an interface would
	// look like a file index or ranker that is present.
	m.fileRoot = deps.FileRoot
	if deps.Files != nil {
		m.files = deps.Files
	}
	if deps.Ranker != nil {
		m.ranker = rankerAdapter{r: deps.Ranker}
		if rec, ok := deps.Ranker.(fileRecorder); ok {
			m.recorder = rec
		}
	}
	// The menu bar reads its rows and context keys through the root, so it gets
	// the root pointer.
	m.menu = menubar.New(m, st).WithMnemonics(m.view.opts.MenuBarMnemonics)
	// The explorer's config: its width and side live on the model because the
	// grip and toggle_file_explorer_side change them, and compact directories go
	// to the tree itself.
	m.explorerWidth, m.explorerSide = m.view.opts.Explorer.Width, m.view.opts.Explorer.Side
	if c, ok := deps.Explorer.(interface{ SetCompactDirectories(bool) }); ok {
		c.SetCompactDirectories(m.view.opts.Explorer.CompactDirectories)
	}
	if deps.Explorer != nil {
		m.explorerPanel = expanel.New(deps.Explorer, st, now)
	}
	m.initContents(deps)
	m.dialogs = deps.Dialogs
	if deps.ShowExplorer && deps.Explorer != nil {
		m.explorerOpen = true
		m.expandRoot()
	}
	if ops, ok := deps.Ops.(explorerOps); ok {
		m.ops = ops
	}
	m.installRowMapper()
	m.plug = newPluginState(m, deps.Root)
	m.plug.panels = pluginPanels{}
	// The restored session's sidebar state is applied after the config defaults and
	// the explorer root, and only when the app restored the saved layout.
	if deps.Sidebar != nil {
		m.restoreSidebarState(*deps.Sidebar)
	}
	// An undecided workspace that can execute code asks for the decision before
	// the first frame, as Fresh's open-time trust prompt does.
	m.startTrustPrompt()
	return m
}

// Run runs m in the alternate screen until it quits or ctx is canceled. A cancel is
// reported as the context's error. Run leaves the language servers and the dock
// running: the caller saves what it must, and then calls Close.
func Run(ctx context.Context, m *Model) error {
	return runProgram(ctx, m, os.Stdout)
}

// RunWithInput runs m with in as the keyboard, when the process's standard input was
// consumed as buffer content (gvim -/--stdin): the caller has reopened the controlling
// terminal and passes it here. The normal Run path is unchanged, and both share the same
// runProgram so the cursor-shape reset and the window-size handshake behave alike.
func RunWithInput(ctx context.Context, m *Model, in io.Reader) error {
	return runProgram(ctx, m, os.Stdout, tea.WithInput(in))
}

// RunWithIO runs m with in as the keyboard and out as the frame output, and calls
// onProgram once with the program handle before it runs. The daemon hosts one program
// this way: in and out are the client socket, and onProgram turns an attach or a resize
// into a window-size message so the daemon repaints the attached client.
func RunWithIO(ctx context.Context, m *Model, in io.Reader, out io.Writer, onProgram func(*tea.Program)) error {
	return runProgramIO(ctx, m, out, onProgram, tea.WithInput(in), tea.WithOutput(out))
}

// RunWorkspaceHost runs a WorkspaceHost as the daemon's one program: the host swaps the
// active workspace model on switch without restarting the program, and onProgram receives
// the program (the daemon uses it for attach resizes and repaints).
func RunWorkspaceHost(ctx context.Context, h *WorkspaceHost, in io.Reader, out io.Writer, onProgram func(*tea.Program)) error {
	err := runProgramIO(ctx, h, out, func(p *tea.Program) {
		h.setProgram(p)
		if onProgram != nil {
			onProgram(p)
		}
	}, tea.WithInput(in), tea.WithOutput(out))
	// The program is over: Do would block on a dead message queue.
	h.stop()
	return err
}

// OpenFileMessage is the message a hosted daemon sends to open a path a client asked
// for. It is the message the explorer sends when a row is activated.
func OpenFileMessage(path string) tea.Msg { return expanel.OpenFileMsg{Path: path} }

// resetCursorStyle is the terminal's own caret shape. The frame sets a blinking block
// as its caret shape, and the renderer restores only the shapes it did not start with,
// so the shape is reset here after the program ends, as it is after a signal.
var resetCursorStyle = ansi.SetCursorStyle(0)

// runProgram runs m with the options opts, and then resets the caret shape on out, which
// is where the program writes.
func runProgram(ctx context.Context, m *Model, out io.Writer, opts ...tea.ProgramOption) error {
	return runProgramIO(ctx, m, out, nil, opts...)
}

// runProgramIO is the shared program loop: onProgram, when set, receives the program
// before it starts, so the daemon can send attach resizes and request repaints.
func runProgramIO(ctx context.Context, m tea.Model, out io.Writer, onProgram func(*tea.Program), opts ...tea.ProgramOption) error {
	p := tea.NewProgram(m, append([]tea.ProgramOption{tea.WithContext(ctx)}, opts...)...)
	if onProgram != nil {
		onProgram(p)
	}
	stop := onContinue(func() { p.Send(tea.RequestWindowSize()) })
	_, err := p.Run()
	stop()
	_, _ = io.WriteString(out, resetCursorStyle)
	if err != nil && ctx.Err() != nil {
		return ctx.Err()
	}
	return err
}

// dockCloseWait bounds the closing of the shell at exit. It is a variable so a test can
// shorten it.
var dockCloseWait = 2 * time.Second

// Close stops the writes of large files in flight, and waits for them to remove their
// temporary files (at most quitWait). It stops the language servers, and then closes the
// dock. Each of the last two steps has its own
// deadline: the port bounds the shutdown of the servers, and the shell gets dockCloseWait.
// So a hung server or shell delays only its own step. The caller runs Close after Run,
// once the session is saved. A shell that does not close in time is left to the exit of
// the process.
func (m *Model) Close() {
	// A signal ends the program without a quit, so the writes in flight are stopped here
	// and waited for: they remove their temporary files before the process exits.
	m.large.stop()
	m.waitSaves()
	m.cancelStream()
	m.lspClose()
	// A shared-runtime workspace that closes cannot show its plugin prompts again: resolve
	// their waits with a cancellation (and a notice) so plugin tasks do not wait forever.
	m.cancelSharedPluginWaits()
	m.pluginStop()
	done := make(chan struct{})
	go func() {
		m.closeDock()
		close(done)
	}()
	timer := time.NewTimer(dockCloseWait)
	defer timer.Stop()
	select {
	case <-done:
	case <-timer.C:
	}
}

// Init implements tea.Model. It starts the root features' commands: the git status,
// the auto-save tick and the file watch.
func (m *Model) Init() tea.Cmd {
	return tea.Batch(m.rootInit(), m.indexSync(), m.pluginInit(), m.clockCmd(), m.authorityInitCmd(), m.startupOpenCmd(), m.updateCheckCmd())
}

// Update implements tea.Model. It runs update, then keeps the split panes on tabs
// that still exist, then tells the editor its new size when the body changed height
// (the status row or the find bar came or went), then scrolls the tab strip to the
// active tab when the active tab or the number of tabs changed, and when the frame
// size changes (the first size shows the active tab too). Scrolling by the wheel or a
// marker changes neither, so it is not undone here. The viewport is not re-synced on
// every message, because the editor moves it to the caret.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if _, ok := msg.(clockTickMsg); ok {
		// A {clock} tick only asks for the next frame (the clock is read while the
		// status row is built) and arms the tick for the minute after it.
		return m, m.clockCmd()
	}
	if _, ok := msg.(tea.FocusMsg); ok {
		// Activation of the terminal window is the reconnect-on-activate point of a
		// remote workspace authority.
		return m, m.beginAuthorityReconnect()
	}
	if forced, ok := msg.(updateForcedMsg); ok {
		m.applyUpdateStatus(forced.status)
		return m, nil
	}
	if checked, ok := msg.(updateCheckedMsg); ok {
		// The release check finished: the status row paints the answer from here
		// on (no notice: an available update is not an interruption).
		m.updateInfo = checked.info
		return m, nil
	}
	if done, ok := msg.(authorityReconnectedMsg); ok {
		m.authorityBusy = false
		if done.err != nil {
			m.reportAuthorityReconnect(done.err)
			// Keep the backoff from the poll: it schedules the next attempt.
			return m, nil
		}
		m.resetAuthorityRetry()
		// One notice reports the refreshed workspace. When the app's post-reconnect work
		// runs it finishes the sentence (and any restart count/failure); without that hook
		// the reconnect outcome itself is the notice.
		m.authorityRefreshNotice = m.auth.AfterReconnect != nil
		if !m.authorityRefreshNotice {
			m.reportAuthorityReconnect(nil)
		}
		// A startup open that failed while the authority was disconnected is retried
		// once the connection is back: failures stay retryable, never a silently empty
		// local buffer. The app's post-reconnect work (restarting the remote language
		// servers that were running, reloading the remote tree) runs in commands too,
		// never on the update goroutine.
		// The git marks are re-read with the same reconnect (a remote read through the
		// authority; a local read stays the ordinary one).
		cmds := []tea.Cmd{m.startupOpenCmd(), m.gitCmd()}
		if explorer := m.remoteExplorerReconnectCmd(); explorer != nil {
			cmds = append(cmds, explorer)
		}
		if m.auth.AfterReconnect != nil {
			after := m.auth.AfterReconnect
			cmds = append(cmds, func() tea.Msg {
				count, err := after()
				return authorityRestartDoneMsg{count: count, err: err}
			})
		}
		return m, tea.Batch(cmds...)
	}
	if done, ok := msg.(authorityRestartDoneMsg); ok {
		m.reportAuthorityRestart(done.count, done.err)
		return m, nil
	}
	if _, ok := msg.(authorityPollTickMsg); ok {
		return m, m.authorityPollTick()
	}
	if _, ok := msg.(authorityRetryTickMsg); ok {
		return m, m.authorityRetryTick()
	}
	prevChrome := m.chromeFocusOf()
	active, count, rows := m.activeTab(), len(m.ed.Tabs()), m.bodyRows()
	activePath := m.activeFilePath()
	at, top := m.caretNow(), m.viewTop()
	snap := m.pluginSnapshot()
	_, resized := msg.(tea.WindowSizeMsg)
	m.util.recordEvent(msg)
	before, beforeStatus := m.notice, m.ed.Status()
	m.plug.ui.updates++
	next, cmd := m.update(msg)
	cmd = tea.Batch(cmd, m.contentsSync())
	cmd = tea.Batch(cmd, m.lspUpdate(msg))
	cmd = tea.Batch(cmd, m.dockSync())
	cmd = tea.Batch(cmd, m.watchSync())
	cmd = tea.Batch(cmd, m.indexSync(), m.searchSync(), m.windowSync())
	m.notes.record(before, m.notice)
	m.noteStatus(beforeStatus)
	m.reconcileTabs()
	cmd = tea.Batch(cmd, m.pluginTail(snap, msg), m.pluginIdle(msg))
	m.trackOccurrences()
	sized := m.bodyRows() != rows
	moved := m.caretNow() != at
	// A scroll that moves the view but not the caret can still change the body
	// size, when a line wider than the pane enters or leaves the view and the
	// horizontal scrollbar row comes or goes. The scroll is kept then.
	scrolled := !resized && !moved && m.viewTop() != top
	if sized {
		keep := m.viewTop()
		m.syncViewport()
		if scrolled {
			m.ed.ScrollTo(keep)
		}
	}
	// A move of the caret, or a new body size, keeps the caret's display row on
	// screen. A wheel notch moves neither, so it is not undone here.
	if resized || (sized && !scrolled) || moved || m.bandAppeared(before, beforeStatus) {
		margin := 0
		if moved {
			margin = m.scrollMarginFor(msg)
		}
		m.revealCaretVisual(margin)
	}
	m.syncCompare()
	m.syncDual()
	if resized || m.activeTab() != active || len(m.ed.Tabs()) != count {
		m.revealActiveTab()
	}
	// The explorer follows the same update: auto_open_on_last_buffer_close focuses
	// it when the last tab closed, a buffer opening again gives the keyboard back,
	// and follow_active_buffer takes the tree to the file just activated.
	if count > 0 && len(m.ed.Tabs()) == 0 {
		cmd = tea.Batch(cmd, m.autoOpenExplorerOnLastClose())
	} else if count == 0 && len(m.ed.Tabs()) > 0 && m.explorerAutoOpened {
		m.explorerAutoOpened = false
		if m.explorerFocus {
			m.focusEditor()
		}
	}
	if now := m.activeFilePath(); now != activePath {
		cmd = tea.Batch(cmd, m.followActiveBuffer())
	}
	// After the root's own focus moves of this update (F6, commands, auto-focus) report
	// the chrome region change once; the queued event is pumped by a command, never
	// inline.
	if cur := m.chromeFocusOf(); cur != prevChrome {
		m.emitChromeFocusChanged(prevChrome, cur)
		cmd = tea.Batch(cmd, m.pumpAfter(func() {}))
	}
	// A {clock} tick is re-armed whenever the row is visible and configured but no
	// tick is in flight: the first frame, after an overlay hid the row, or after a
	// runtime config change added the element. clockCmd is idempotent per tick.
	if !m.clockArmed {
		cmd = tea.Batch(cmd, m.clockCmd())
	}
	cmd = tea.Batch(cmd, m.pluginViewTail(snap))
	m.syncScrollGroups()
	m.applyGroupFixedHeights()
	// A workspace switch queued shared-runtime plugin Work (a per-workspace project
	// plugin config reload): pump it here, so the switch itself did no IO on this loop.
	if m.plug.pumpPending {
		m.plug.pumpPending = false
		cmd = tea.Batch(cmd, m.pumpAfter(func() {}))
	}
	return next, cmd
}

// bandAppeared reports whether this update gave the status row a message that needs
// the notice band, so the caret has to be kept above it. A message the status row
// shows whole, and the band staying while the user scrolls, do not count.
func (m *Model) bandAppeared(notice, status string) bool {
	return (m.notice != notice || m.ed.Status() != status) && len(m.layout().Notice) > 0
}

// update handles one message. While an overlay is open, key and mouse messages
// go to the top overlay (see Overlay). Otherwise key presses go through the
// keymap in the focused context, and text that no binding claims is inserted
// when the editor has focus. Mouse presses, drags and wheel notches go through
// the layout's hit map.
func (m *Model) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if cmd, ok := m.lspKey(msg); ok {
		return m, cmd
	}
	if m.overlayInput(msg) {
		// handleKey never sees a key an overlay takes, so the panic chord is checked here.
		if k, ok := msg.(tea.KeyPressMsg); ok {
			if cmd, panicked := m.pluginPanicKey(tea.Key(k)); panicked {
				return m, cmd
			}
		}
		barWasOpen := m.findOpen()
		cmd := m.routeOverlay(msg)
		// An overlay that opened or closed the find bar changes the editor body.
		m.syncViewport()
		if _, isKey := msg.(tea.KeyPressMsg); isKey && (barWasOpen || m.findOpen()) {
			m.revealCaret()
		}
		return m, cmd
	}
	// A plugin key layer (a mode or a key wait) sees the key before a mounted widget
	// panel's smart dispatch: the panel is the fallback while a plugin holds the keyboard.
	if cmd, ok := m.pluginMessage(msg); ok {
		return m, cmd
	}
	// A mounted widget panel with the keyboard takes the keys before the editor keymap.
	if cmd, ok := m.pluginPanelKey(msg); ok {
		return m, cmd
	}
	// A message that is not input still goes to the top overlay, because an overlay
	// may have started the work that sends it (the palette's file ranking). The
	// root handles the message too.
	var overlayCmd tea.Cmd
	if len(m.overlays) > 0 {
		overlayCmd = m.routeOverlay(msg)
	}
	fixCmd, taken := m.fixMessage(msg)
	if taken {
		return m, tea.Batch(overlayCmd, fixCmd)
	}
	overlayCmd = tea.Batch(overlayCmd, fixCmd)
	if taken, cmd := m.routeMenu(msg); taken {
		return m, tea.Batch(overlayCmd, cmd)
	}
	if cmd, ok := m.dock.Update(msg); ok {
		return m, tea.Batch(overlayCmd, cmd)
	}
	if done, ok := msg.(saveDoneMsg); ok {
		return m, tea.Batch(overlayCmd, m.saveDone(done))
	}
	if cmd, ok := m.largeMessage(msg); ok {
		return m, tea.Batch(overlayCmd, cmd)
	}
	if cmd, ok := m.rootMessage(msg); ok {
		return m, tea.Batch(overlayCmd, cmd)
	}
	if cmd, ok := m.featureMessage(msg); ok {
		return m, tea.Batch(overlayCmd, cmd)
	}
	if cmd, ok := m.lspMessage(msg); ok {
		return m, tea.Batch(overlayCmd, cmd)
	}
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.syncMenu()
		m.syncViewport()
		m.followSelection()
		m.pluginReady()
	case tea.KeyPressMsg:
		m.expireStatus()
		return m, m.handleKey(tea.Key(msg))
	case tea.KeyReleaseMsg:
		// Terminals report key release only through a keyboard-reporting backend
		// (the Windows console API, or kitty-protocol event types the host does not
		// request from legacy terminals). A release never edits or moves anything; it
		// is only visible to plugins that subscribe.
		if m.plugWants("key_release") {
			m.plugEmit("key_release", keyEventData(tea.Key(msg)))
		}
		return m, nil
	case tea.MouseClickMsg:
		// The band is hit-tested before the message expires: once it is gone the
		// click would land on the body cell that was under it.
		onBand := m.layout().OnNotice(msg.X, msg.Y)
		m.expireStatus()
		m.chord = nil
		if onBand {
			break
		}
		if panelCmd, taken := m.pluginPanelMouse(msg); taken {
			cmd = panelCmd
			break
		}
		cmd = m.handleClick(msg)
		m.rebaseFind()
	case tea.MouseMotionMsg:
		if panelCmd, taken := m.pluginPanelMouse(msg); taken {
			cmd = panelCmd
		} else if dockCmd, taken := m.dockMouse(msg, msg.Y, 1); taken {
			cmd = dockCmd
		} else {
			cmd = tea.Batch(m.handleDrag(msg.Mouse()), m.explorerMouse(msg))
		}
		m.updateStatusRestartHover(msg.Mouse())
	case dragScrollMsg:
		cmd = m.dragScroll(msg)
	case tea.MouseReleaseMsg:
		if panelCmd, taken := m.pluginPanelMouse(msg); taken {
			cmd = panelCmd
			break
		}
		td := m.drag.tab
		m.drag = drag{}
		if td != nil {
			m.dropTab(td)
		}
		m.releaseDivider()
		if dockCmd, taken := m.dockMouse(msg, msg.Y, 1); taken {
			cmd = dockCmd
		} else {
			cmd = m.explorerMouse(msg)
		}
	case tea.MouseWheelMsg:
		// Scrolling is navigation, so a message ends with it as it does with a key or a click.
		m.expireStatus()
		if panelCmd, taken := m.pluginPanelMouse(msg); taken {
			cmd = panelCmd
		} else {
			cmd = m.handleWheel(msg)
		}
	case dialog.Result:
		cmd = m.dialogDone(msg)
	case menubar.Failed:
		m.notice = failedNotice(msg)
	case ctxmenu.Chosen:
		cmd = m.contextChosen(msg)
	case ctxmenu.Reroute:
		cmd = m.rightClick(msg.Press)
	case expanel.OpenFileMsg:
		cmd = m.openFileCmd(msg.Path, msg.Preview, 0, 0)
		if !msg.Preview {
			m.focusEditor()
		}
	case expanel.ReloadMsg:
		cmd = m.refreshExplorerCmd()
	case expanel.CloseMsg:
		m.closeExplorer()
	case expanel.FocusMsg:
		// The panel sends it for a mouse press only, so the view stays where it is.
		m.takeExplorerFocus()
	case expanel.ResizeMsg:
		// A grip drag moves the width in the configured form: a percentage stays
		// a percentage (Fresh's with_cols), a column count is exact.
		m.explorerWidth = m.explorerWidth.WithCols(msg.Width, m.width)
	case expanel.ErrorMsg:
		m.notice = msg.Err.Error()
	case remoteTreeToggleMsg:
		cmd = m.remoteTreeToggleDone(msg)
	case remoteTreeLoadedMsg:
		cmd = m.remoteTreeLoadDone(msg)
	case expanel.NewFileMsg:
		m.askName(msg.Dir, false)
	case expanel.NewFolderMsg:
		m.askName(msg.Dir, true)
	case dock.OpenPathMsg:
		// The dock resolved a Ctrl+Click path; open it as the explorer would and
		// give the keyboard back to the editor.
		cmd = m.openFileCmd(msg.Path, false, msg.Line, msg.Col)
		m.focusEditor()
	case expanel.RenameMsg:
		m.askRename(msg.Path)
	case expanel.DeleteMsg:
		if len(msg.Paths) > 0 {
			m.askDelete(msg.Paths...)
		} else {
			m.askDelete(msg.Path)
		}
	case expanel.CopyMsg:
		cmd = m.askRemoteClipboard(remoteClipboardCopy, msg.Path, msg.Paths, "")
	case expanel.CutMsg:
		cmd = m.askRemoteClipboard(remoteClipboardCut, msg.Path, msg.Paths, "")
	case expanel.PasteMsg:
		cmd = m.askRemoteClipboard(remoteClipboardPaste, "", nil, msg.Dir)
	case prompt.Done:
		cmd = m.answer(msg)
	case mixedExtractSelectionMsg:
		cmd = m.afterMixedExtractSelection(msg.selection)
	case ExtractConflictMsg:
		// The extraction command detected target conflicts BEFORE writing anything.
		// Ask here; the answer retries with replace or cancels untouched.
		m.pushOverlay(prompt.NewConfirm(m.styles, extractConflictQuestion(msg), extractConflictAnswer{
			selection: msg.Selection, targetID: msg.TargetID,
		}))
	case fileResultMsg:
		cmd = tea.Batch(m.fileDone(msg), m.gitCmd())
	case urlOpenedMsg:
		m.openURLDone(msg)
	case browser.Open:
		if m.splits.pick != nil {
			m.comparePicked(msg.Path)
			break
		}
		cmd = m.openFileCmd(msg.Path, false, msg.Line, msg.Col)
		m.focusEditor()
	case palette.RunCommandMsg:
		cmd = m.runCommand(msg.Action)
	case palette.OpenFileMsg:
		if m.recorder != nil {
			m.recorder.Record(msg.Rel)
		}
		cmd = m.openFileCmd(msg.Path, false, msg.Line, msg.Col)
		m.focusEditor()
	case palette.OpenBufferLineMsg:
		// A Live Grep hit inside an open buffer: its tab is already there, so activate
		// it and put the caret at the match.
		m.activateTab(msg.Index)
		m.gotoLine(msg.Line, msg.Col)
		m.focusEditor()
	case palette.GotoLineMsg:
		// The ':' mode jumps at once, as Fresh's does: one Enter reaches the line.
		m.gotoLine(msg.Line, msg.Col)
		m.focusEditor()
	case palette.ActivateBufferMsg:
		m.activateTab(msg.Index)
	case searchpanel.ReplaceRequestMsg:
		cmd = m.askProjectReplace(msg.Request)
	case searchpanel.OpenMsg:
		cmd = m.projectJump(msg)
	case searchpanel.ExportMsg:
		cmd = m.openProjectResultsList()
	case projectReplaceDoneMsg:
		cmd = m.projectReplaceDone(msg)
	}
	return m, tea.Batch(overlayCmd, cmd)
}

// routeMenu gives msg to the menu bar when the bar takes it. A key the open
// dropdown answers, a left press on a label, and a press on the open dropdown
// are taken. A press outside an open dropdown closes it and is not taken, so it
// still reaches the frame. It returns the quit command when a menu row quit the
// editor.
func (m *Model) routeMenu(msg tea.Msg) (bool, tea.Cmd) {
	if m.hideMenuBar {
		return false, nil
	}
	m.syncMenu()
	if k, ok := msg.(tea.KeyPressMsg); ok && m.menu.IsOpen() {
		if cmd, handled := m.menuKeymap(tea.Key(k)); handled {
			return true, cmd
		}
	}
	onLabel := isLabelPress(m.menu, msg)
	if !m.menu.IsOpen() && !onLabel {
		return false, nil
	}
	taken := onLabel || m.menu.Consumes(msg)
	var cmd tea.Cmd
	m.menu, cmd = m.menu.Update(msg)
	cmd = tea.Batch(cmd, m.pending)
	m.pending = nil
	if m.quitting {
		m.quitting = false
		return true, tea.Batch(cmd, m.quit())
	}
	// A message that is not taken only closes the dropdown. Its command is nil,
	// unless a row ran an explorer action that started one.
	return taken, cmd
}

// isLabelPress reports whether msg is a left press on a label of the menu bar, or on
// its overflow marker.
func isLabelPress(bar menubar.Model, msg tea.Msg) bool {
	mo, ok := msg.(tea.MouseClickMsg)
	if !ok || mo.Button != tea.MouseLeft {
		return false
	}
	k := bar.Hit(mo.X, mo.Y).Kind
	return k == menubar.TargetLabel || k == menubar.TargetMore
}

// syncMenu gives the menu bar the frame size and the explorer focus of the
// current state. Closing the dropdown when its label leaves the bar is part of
// WithExplorerFocus.
func (m *Model) syncMenu() {
	m.menu = m.menu.SetSize(m.width, m.height).
		WithExplorerFocus(m.explorerFocus).
		WithMnemonics(m.view.opts.MenuBarMnemonics)
}

// MenuMnemonic implements menubar.Mnemonicer: the letter a menu-bar label underlines,
// asked of the active keymap (the alt+letter binding of menu_open for that label). A
// keymap port that does not provide the method has no mnemonics, which is how the
// test fakes behave.
func (m *Model) MenuMnemonic(name string) (rune, bool) {
	if mm, ok := m.keys.(interface {
		MenuMnemonic(string) (rune, bool)
	}); ok {
		return mm.MenuMnemonic(name)
	}
	return 0, false
}

// suppressedMnemonic reports whether a resolved command is a menu accelerator while
// editor.menu_bar_mnemonics is off. Fresh drops those bindings at resolution, so with
// the setting off an Alt+letter the keymap only uses for menu_open does nothing; a
// user binding on the same chord resolves to its own action and is not affected.
func (m *Model) suppressedMnemonic(kp KeyPress, cmd Command) bool {
	if m.view.opts.MenuBarMnemonics || cmd.Action != "menu_open" {
		return false
	}
	return len(kp.Mods) == 1 && kp.Mods[0] == "alt" && utf8.RuneCountInString(kp.Name) == 1
}

// failedNotice is the status text for a menu row that could not run.
func failedNotice(f menubar.Failed) string {
	if errors.Is(f.Err, menubar.ErrUnsupported) {
		return "Not supported in this port: " + f.Label
	}
	return f.Label + ": " + f.Err.Error()
}

// Run implements menubar.Ports. Explorer and focus actions are handled by the
// root, and every other action goes to the editor. An action the editor does not
// know is reported as menubar.ErrUnsupported. A quit is sent after the menu
// closes, through quitting.
func (m *Model) Run(action string) error {
	args := map[string]any(nil)
	if name, ok := strings.CutPrefix(action, menubar.CopyThemePrefix); ok {
		action, args = "copy_with_theme", map[string]any{"theme": name}
	}
	defer m.plugSource(srcMenu)()
	m.recordCommand(Command{Action: action})
	defer m.plugCommand(action, nil)()
	if cmd, ok := m.uiAction(action, nil); ok {
		m.pending = tea.Batch(m.pending, cmd)
		return nil
	}
	if cmd, ok := m.explorerAction(action); ok {
		m.pending = tea.Batch(m.pending, cmd)
		return nil
	}
	if m.explorerFocus && clipboardActions[action] {
		return nil
	}
	if cmd, ok := m.editorPaste(action, nil); ok {
		m.pending = tea.Batch(m.pending, cmd)
		return nil
	}
	res := m.editorDo(editor.Action(action), args)
	m.plugFailed(res.Err)
	m.syncViewport()
	if errors.Is(res.Err, editor.ErrUnknownAction) {
		return menubar.ErrUnsupported
	}
	if res.Quit {
		m.quitting = true
	}
	m.pending = tea.Batch(m.pending, m.clipboardOutArgs(action, args, res))
	return nil
}

// Holds implements menubar.Ports. It evaluates the legacy context keys that the
// menu rows name, against the current state. A key the port does not model does
// not hold, so its rows stay disabled.
func (m *Model) Holds(name string) bool {
	if on, ok := m.viewHolds(name); ok {
		return on
	}
	doc := m.ed.Active()
	switch name {
	case "has_buffer", "has_text_buffer":
		return doc != nil
	case "can_edit", "can_paste", "line_numbers", "vertical_scrollbar",
		"horizontal_scrollbar", "mouse_capture":
		return true
	case "keymap_default", "keymap_emacs", "keymap_vscode", "keymap_macos_gui":
		return m.util.keymapChecked(name)
	case "inlay_hints":
		return m.lsp.inlay
	case "can_save", "can_revert":
		return doc != nil && hasPath(doc)
	case "can_save_all":
		return slices.ContainsFunc(m.ed.Tabs(), func(t TabInfo) bool { return t.Modified })
	case "read_only":
		ro, ok := doc.(interface{ ReadOnly() bool })
		return ok && ro.ReadOnly()
	case "modified":
		return doc != nil && doc.Modified()
	case "large":
		return doc != nil && isLargeDoc(doc)
	case "formatter_available":
		f, ok := m.ed.(formatterEditor)
		return ok && f.HasFormatter() || m.lspCanFormat()
	case "has_selection", "can_cut", "can_copy":
		if doc == nil {
			return false
		}
		_, _, ok := doc.Selection()
		return ok
	case "file_explorer":
		return m.explorerOpen && m.tree != nil
	case "file_explorer_focused":
		return m.explorerFocus && m.explorerOpen && m.tree != nil
	case "file_explorer_show_hidden":
		return m.tree != nil && m.tree.ShowHidden()
	case "file_explorer_show_gitignored":
		return m.tree != nil && m.tree.ShowIgnored()
	}
	if lang, ok := strings.CutPrefix(name, "language:"); ok {
		return doc != nil && m.lspLanguage(doc) == strings.ToLower(lang)
	}
	on, _ := m.pluginContext(name)
	return on
}

// hasPath reports whether the document has a file name on disk. A Document that
// does not say so has no path.
func hasPath(doc Document) bool {
	p, ok := doc.(interface{ HasPath() bool })
	return ok && p.HasPath()
}

// fileEditor is the file-opening surface of the editor. The app adapter
// implements it. The root asserts it on EditorState, so the EditorState port
// stays as it is. An editor without it cannot open files.
type fileEditor interface {
	OpenFile(path string) error
	OpenPreview(path string) error
	GotoLine(line, col int) error
}

// selectingGoto is the go-to-line prompt's jump. An editor that has it extends the
// selection to the line while mark mode is on; the other jumps (LSP, session restore,
// file:line arguments) go through GotoLine, which clears the selection.
type selectingGoto interface {
	GotoLineSelecting(line, col int) error
}

// openFile opens path as a preview tab or a permanent one. The editor reports
// a failure in its status row.
func (m *Model) openFile(path string, preview bool) {
	_ = m.openFileErr(path, preview)
}

// openFileErr is openFile that also returns why the file did not open.
func (m *Model) openFileErr(path string, preview bool) error {
	f, ok := m.ed.(fileEditor)
	if !ok {
		m.notice = "Opening files is not supported in this port"
		return errors.New("opening files is not supported in this port")
	}
	var err error
	if preview {
		err = f.OpenPreview(path)
	} else {
		err = f.OpenFile(path)
	}
	m.syncViewport()
	// A large file that is not UTF-8 opens only if the user agrees to load it whole.
	m.maybeAskFullLoad(err)
	return err
}

// openFileAt opens path and moves the caret to line and col when line is set.
func (m *Model) openFileAt(path string, line, col int) {
	m.openAt(path, false, line, col)
}

// openAt is openFileAt for a permanent tab or a preview.
func (m *Model) openAt(path string, preview bool, line, col int) {
	_ = m.openAtErr(path, preview, line, col)
}

// openAtErr is openAt that also returns why the file did not open.
func (m *Model) openAtErr(path string, preview bool, line, col int) error {
	err := m.openFileErr(path, preview)
	if f, ok := m.ed.(fileEditor); ok && line > 0 {
		if gerr := f.GotoLine(line, col); gerr != nil {
			m.notice = gerr.Error()
		}
	}
	return err
}

// reloadTree rereads the explorer tree from disk. A failure is shown as a notice.
func (m *Model) reloadTree() {
	if m.tree == nil {
		return
	}
	if err := m.tree.Reload(); err != nil {
		m.notice = err.Error()
	}
}

// explorerOps is the file-operation port of the explorer. Deps.Ops is typed
// FileOps, which holds the three disk operations the ports declare. New asserts
// this wider set on it, so the app adapter implements both. The disk methods
// run in a tea.Cmd. FollowRename and CloseDeleted change the editor, so the
// root calls them from Update only.
type explorerOps interface {
	FileOps
	Mkdir(ctx context.Context, dir, name string) (string, error)
	Move(ctx context.Context, src, dstDir string) (string, error)
	Copy(ctx context.Context, src, dstDir string, c expanel.Conflict) (string, error)
	FollowRename(oldPath, newPath string) error
	CloseDeleted(path string) error
	// CheckRename reports whether a rename or move from oldPath to newPath would land
	// on an open tab. The root asks in Update, before the command touches the disk.
	CheckRename(oldPath, newPath string) error
}

// copyAsOps is the optional remote part for the conflict prompt's rename choice: copy
// under an explicit destination name with the chosen conflict policy.
type copyAsOps interface {
	CopyAs(ctx context.Context, src, dstDir, name string, c expanel.Conflict) (string, error)
}

// moveAsOps is the optional remote part for a cut paste's conflict choice: move with a
// conflict policy and an optional destination name.
type moveAsOps interface {
	MoveAs(ctx context.Context, src, dstDir, name string, c expanel.Conflict) (string, error)
}

// clipboard is the explorer clipboard: the paths copied or cut, in display order.
// A path leaves it when its entry is trashed, renamed or moved away.
type clipboard struct {
	paths []string
	cut   bool
}

// newClipboard returns the clipboard of a copy or cut message. The message names
// one path in path, or several in paths.
func newClipboard(path string, paths []string, cut bool) *clipboard {
	if len(paths) == 0 {
		paths = []string{path}
	}
	return &clipboard{paths: paths, cut: cut}
}

// jobKind names one disk operation of the explorer.
type jobKind int

const (
	jobCreate jobKind = iota + 1
	jobMkdir
	jobRename
	jobMove
	jobCopy
	jobTrash
)

// fileJob is the input of one disk operation. It comes back with its result, so
// the root can run it again after a conflict. rest holds the jobs of the same
// multi-item paste that run after this one, in order. A conflict prompt keeps them
// with the job, so an Esc at the prompt drops them too.
type fileJob struct {
	kind    jobKind
	path    string           // the entry: rename, move, copy source, trash
	dir     string           // the directory: create, mkdir, move and copy destination
	name    string           // the new name: create, mkdir, rename
	policy  expanel.Conflict // copy and move conflict choice
	dstName string           // copy/move destination name after a conflict rename
	rest    []fileJob        // the jobs of the same paste after this one
}

// fileResultMsg is the result of a fileJob. out is the path the operation
// produced, and it is empty for a trash.
type fileResultMsg struct {
	job fileJob
	out string
	err error
}

// The tags of the prompts the root opens. The prompt returns its tag in Done.
type (
	newNameTag struct {
		dir    string
		folder bool
	}
	renameTag    struct{ path string }
	deleteTag    struct{ paths []string }
	overwriteTag struct{ job fileJob }
	// remotePasteConflictTag asks a remote paste conflict's overwrite/rename choice;
	// remotePasteRenameTag carries the new destination name.
	remotePasteConflictTag struct{ job fileJob }
	remotePasteRenameTag   struct{ job fileJob }
	gotoTag                struct{}
)

// fileRecorder is the ranker's frecency record. A ranker that has one gets the
// path of each file the picker opens.
type fileRecorder interface {
	Record(rel string)
}

// openGoto shows the go-to-line prompt with input as its text. The prompt takes
// "N" or "N:COL", and Enter moves the caret there.
func (m *Model) openGoto(input string) {
	m.pushOverlay(prompt.NewLine(m.styles, prompt.LineOptions{
		Label:    uiText("gvim.prompt.goto_line", "Go to line:"),
		Input:    input,
		Tag:      gotoTag{},
		Validate: prompt.ValidateLineCol,
	}))
}

// gotoText applies the text of the go-to-line prompt. The prompt has already
// checked it, and the parse is repeated here for the line and the column.
func (m *Model) gotoText(text string) {
	line, col, ok := editor.ParseLineCol(text)
	if !ok {
		m.notice = "Not a line number: " + text
		return
	}
	m.gotoLine(line, col)
}

// The remote explorer clipboard's confirmation kinds. A local workspace acts directly
// (no confirmation, as before); a remote one confirms and names the target, because the
// entries live on another host and a cut/paste permanently changes that tree.
const (
	remoteClipboardCopy      = "copy"
	remoteClipboardCut       = "cut"
	remoteClipboardPaste     = "paste"
	remoteClipboardDuplicate = "duplicate"
)

// remoteClipboardTag is the pending clipboard action of a remote confirmation.
type remoteClipboardTag struct {
	kind  string
	paths []string
	dir   string
}

// remoteClipboardQuestion is the confirmation text; it always names the remote target.
func remoteClipboardQuestion(kind string, paths []string, dir, target string) string {
	what := ""
	switch {
	case len(paths) == 1:
		what = filepath.Base(paths[0])
	case len(paths) > 1:
		what = fmt.Sprintf("%d entries", len(paths))
	}
	switch kind {
	case remoteClipboardCopy:
		return fmt.Sprintf("Copy %s on %s?", what, target)
	case remoteClipboardCut:
		return fmt.Sprintf("Cut %s on %s? Remote entries are removed only after a successful paste.", what, target)
	case remoteClipboardDuplicate:
		return fmt.Sprintf("Duplicate %s on %s?", what, target)
	}
	return ""
}

// pasteQuestion is the remote paste confirmation, naming the destination and target.
func pasteQuestion(count int, dir, target string) string {
	noun := "entry"
	if count != 1 {
		noun = "entries"
	}
	return fmt.Sprintf("Paste %d %s into %s on %s?", count, noun, dir, target)
}

// askRemoteClipboard runs a local clipboard action directly, or asks first on a remote
// workspace and then runs it. The confirmation names the remote target.
func (m *Model) askRemoteClipboard(kind, path string, paths []string, dir string) tea.Cmd {
	if len(paths) == 0 && path != "" {
		paths = []string{path}
	}
	target := m.remoteExplorerTarget()
	if target == "" {
		return m.runRemoteClipboard(remoteClipboardTag{kind: kind, paths: paths, dir: dir})
	}
	q := remoteClipboardQuestion(kind, paths, dir, target)
	if kind == remoteClipboardPaste {
		count := 0
		if m.clip != nil {
			count = len(m.clip.paths)
		}
		q = pasteQuestion(count, dir, target)
	}
	m.pushOverlay(prompt.NewConfirm(m.styles, q, remoteClipboardTag{kind: kind, paths: paths, dir: dir}))
	return nil
}

// runRemoteClipboard applies a confirmed clipboard action: a copy/cut fills the
// explorer clipboard (nothing is written until paste), a paste starts the jobs, and a
// duplicate starts one same-directory copy per selected entry (Fresh's naming rule).
func (m *Model) runRemoteClipboard(tag remoteClipboardTag) tea.Cmd {
	switch tag.kind {
	case remoteClipboardCopy:
		m.clip = newClipboard("", tag.paths, false)
		return nil
	case remoteClipboardCut:
		m.clip = newClipboard("", tag.paths, true)
		return nil
	case remoteClipboardPaste:
		return m.paste(tag.dir)
	case remoteClipboardDuplicate:
		return m.duplicatePaths(tag.paths)
	}
	return nil
}

// duplicatePaths starts a same-directory copy of every path: the adapter gives each a
// free "name copy[.ext]" name (Fresh's Duplicate).
func (m *Model) duplicatePaths(paths []string) tea.Cmd {
	jobs := make([]fileJob, 0, len(paths))
	for _, p := range paths {
		jobs = append(jobs, fileJob{kind: jobCopy, path: p, dir: filepath.Dir(p), policy: expanel.ConflictCancel})
	}
	return m.runJobs(jobs)
}

// needOps reports whether the file operations are wired. When they are not, the
// notice row says so and the operation does nothing.
func (m *Model) needOps() bool {
	if m.ops == nil {
		m.notice = "File operations are not supported in this port"
	}
	return m.ops != nil
}

// remoteExplorerTarget returns the remote target of the file-operation adapter, or ""
// for a local workspace. Confirmations state it so a remote write is never mistaken for
// a local one.
func (m *Model) remoteExplorerTarget() string {
	if ta, ok := m.ops.(interface{ RemoteTarget() string }); ok {
		return ta.RemoteTarget()
	}
	return ""
}

// newEntryLabel is the name prompt's label for a new file or folder; a remote target is
// stated in the label.
func newEntryLabel(folder bool, target string) string {
	label := uiText("gvim.remote.new_file_label", "New file:")
	if folder {
		label = uiText("gvim.remote.new_folder_label", "New folder:")
	}
	if target != "" {
		label = uiTextVars("gvim.remote.label_on_target", "%{label} on %{target}:", map[string]any{"label": strings.TrimSuffix(label, ":"), "target": target})
	}
	return label
}

// renamePromptLabel is the rename prompt's label; a remote target is stated in the label.
func renamePromptLabel(target string) string {
	if target == "" {
		return uiText("gvim.prompt.rename_to", "Rename to:")
	}
	return uiTextVars("gvim.remote.rename_on_target", "Rename to on %{target}:", map[string]any{"target": target})
}

// deleteQuestion is the delete confirmation. A remote delete is permanent (no trash on
// the remote host) and the question states the target.
func deleteQuestion(paths []string, target string) string {
	q := uiTextVars("gvim.remote.delete_file", "Delete %{name}?", map[string]any{"name": filepath.Base(paths[0])})
	if len(paths) > 1 {
		q = uiTextVars("gvim.remote.delete_items", "Delete %{count} items?", map[string]any{"count": len(paths)})
	}
	if target != "" {
		if len(paths) > 1 {
			q = uiTextVars("gvim.remote.permanent_delete_items", "Permanently delete %{count} items on %{target}?", map[string]any{"count": len(paths), "target": target})
		} else {
			q = uiTextVars("gvim.remote.permanent_delete_file", "Permanently delete %{name} on %{target}?", map[string]any{"name": filepath.Base(paths[0]), "target": target})
		}
		q += uiText("gvim.remote.no_trash_suffix", " Remote deletes have no trash.")
	}
	return q
}

// askName opens the name prompt for a new file or folder in dir.
func (m *Model) askName(dir string, folder bool) {
	if !m.needOps() {
		return
	}
	m.pushOverlay(prompt.NewLine(m.styles, prompt.LineOptions{
		Label:    newEntryLabel(folder, m.remoteExplorerTarget()),
		Tag:      newNameTag{dir: dir, folder: folder},
		Validate: validateName,
	}))
}

// askRename opens the rename prompt for path, with its current name filled in.
func (m *Model) askRename(path string) {
	if !m.needOps() {
		return
	}
	m.pushOverlay(prompt.NewLine(m.styles, prompt.LineOptions{
		Label:    renamePromptLabel(m.remoteExplorerTarget()),
		Input:    filepath.Base(path),
		Tag:      renameTag{path: path},
		Validate: validateName,
	}))
}

// askDelete opens the Yes/No confirm for trashing paths. The focus starts on No. With
// several paths the question counts them.
func (m *Model) askDelete(paths ...string) {
	if !m.needOps() || len(paths) == 0 {
		return
	}
	q := deleteQuestion(paths, m.remoteExplorerTarget())
	m.pushOverlay(prompt.NewConfirm(m.styles, q, deleteTag{paths: paths}))
}

// askOverwrite asks whether a local copy that hit an existing destination may replace
// it. Yes runs the copy with the overwrite policy, and No runs it with skip.
func (m *Model) askOverwrite(job fileJob) {
	q := "Overwrite " + filepath.Base(job.path) + "?"
	m.pushOverlay(prompt.NewConfirm(m.styles, q, overwriteTag{job: job}))
}

// askRemotePasteConflict asks what a remote paste should do when its destination
// exists: Overwrite replaces it, Rename opens a prefilled name prompt (Esc cancels the
// paste, including the remaining jobs). Fresh also offers Skip/Skip All; gvim keeps the
// bounded per-item choice.
func (m *Model) askRemotePasteConflict(job fileJob) {
	name := job.dstName
	if name == "" {
		name = filepath.Base(job.path)
	}
	q := "Remote destination exists on " + m.remoteExplorerTarget() + ": overwrite " + name + "?"
	m.pushOverlay(prompt.NewConfirmWith(m.styles, q, remotePasteConflictTag{job: job}, prompt.ConfirmOptions{
		Yes: "Overwrite",
		No:  "Rename",
	}))
}

// askRemotePasteRename opens the new destination name prompt of a remote conflict.
func (m *Model) askRemotePasteRename(job fileJob) {
	name := job.dstName
	if name == "" {
		name = filepath.Base(job.path)
	}
	m.pushOverlay(prompt.NewLine(m.styles, prompt.LineOptions{
		Label:    pasteAsLabel(m.remoteExplorerTarget()),
		Input:    name,
		Tag:      remotePasteRenameTag{job: job},
		Validate: validateName,
	}))
}

// pasteAsLabel names the conflict rename prompt, including the remote target when there
// is one.
func pasteAsLabel(target string) string {
	if target == "" {
		return "Paste as:"
	}
	return "Paste as on " + target + ":"
}

// validateName is the name check of the explorer prompts. It keeps the prompt
// open for a name that cannot be a single path element. The app adapter checks
// the rest, against the disk, through fsops.Validate.
func validateName(text string) error {
	switch {
	case strings.TrimSpace(text) == "":
		return errors.New(uiText("gvim.prompt.name_empty", "name is empty"))
	case text == "." || text == "..":
		return errors.New(uiText("gvim.prompt.name_reserved", "reserved name"))
	case strings.ContainsAny(text, "/\x00"):
		return errors.New(uiText("gvim.prompt.name_invalid", "name cannot contain / or NUL"))
	}
	return nil
}

// answer applies a prompt's Done. A text prompt submits a name, and a confirm
// answers Yes or No. It returns the disk operation the answer starts, if any.
func (m *Model) answer(d prompt.Done) tea.Cmd {
	if cmd, ok := m.pluginAnswer(d); ok {
		return cmd
	}
	switch tag := d.Tag.(type) {
	case viewTag:
		return m.viewAnswer(tag, d)
	case terminalTabRenameTag:
		m.renameDockTab(d.Text)
	case agentRunDirTag:
		return m.agentDirAnswer(d)
	case newNameTag:
		kind := jobCreate
		if tag.folder {
			kind = jobMkdir
		}
		return m.runJob(fileJob{kind: kind, dir: tag.dir, name: d.Text})
	case renameTag:
		return m.runJob(fileJob{kind: jobRename, path: tag.path, name: d.Text})
	case deleteTag:
		if d.Outcome == prompt.Yes {
			jobs := make([]fileJob, len(tag.paths))
			for i, p := range tag.paths {
				jobs[i] = fileJob{kind: jobTrash, path: p}
			}
			return m.runJobs(jobs)
		}
	case remoteClipboardTag:
		return m.runRemoteClipboard(tag)
	case overwriteTag:
		job := tag.job
		job.policy = expanel.ConflictSkip
		if d.Outcome == prompt.Yes {
			job.policy = expanel.ConflictOverwrite
		}
		return m.runJob(job)
	case remotePasteConflictTag:
		job := tag.job
		if d.Outcome == prompt.Yes {
			job.policy = expanel.ConflictOverwrite
			return m.runJob(job)
		}
		m.askRemotePasteRename(job)
		return nil
	case remotePasteRenameTag:
		job := tag.job
		if d.Outcome != prompt.Submitted {
			return nil
		}
		job.dstName = d.Text
		job.policy = expanel.ConflictCancel
		return m.runJob(job)
	case overwriteSaveTag:
		return m.overwriteAnswer(tag, d)
	case fullLoadTag:
		return m.fullLoadAnswer(tag, d)
	case authorityPasswordTag:
		if d.Outcome == prompt.Submitted {
			return m.authorityReconnectCmd(d.Text)
		}
		m.resetAuthorityRetry()
	case authorityPassphraseTag:
		if d.Outcome == prompt.Submitted {
			return m.authorityPassphraseCmd(d.Text)
		}
		m.resetAuthorityRetry()
		m.quietNotice("SSH reconnect cancelled")
	case authorityChallengeTag:
		if d.Outcome == prompt.Submitted {
			m.authorityAnswers = append(m.authorityAnswers, d.Text)
			return m.askAuthorityChallenge()
		}
		m.resetAuthorityRetry()
		m.quietNotice("SSH reconnect cancelled")
	default:
		if cmd, ok := m.workspaceActionAnswer(d); ok {
			return cmd
		}
	case gotoTag:
		if d.Outcome == prompt.Submitted {
			m.gotoText(d.Text)
		}
	case bookmarkTag:
		return m.bookmarkAnswer(tag, d)
	case projectReplaceTag:
		return m.projectReplaceAnswer(tag, d)
	case urlOpenTag:
		return m.openURLAnswer(tag, d)
	}
	return nil
}

// paste starts the paste of the clipboard into dir: one job for each path, in
// order. A cut moves each entry, except an entry under another cut entry, which
// moves with that one. A copy runs first with the cancel policy, so a conflict can
// be asked about. The jobs run one after another, see runJobs.
func (m *Model) paste(dir string) tea.Cmd {
	if m.clip == nil {
		m.notice = "Nothing to paste"
		return nil
	}
	c := *m.clip
	paths := c.paths
	if c.cut {
		paths = outermost(paths)
	}
	jobs := make([]fileJob, 0, len(paths))
	for _, p := range paths {
		if c.cut {
			jobs = append(jobs, fileJob{kind: jobMove, path: p, dir: dir})
		} else {
			jobs = append(jobs, fileJob{kind: jobCopy, path: p, dir: dir, policy: expanel.ConflictCancel})
		}
	}
	return m.runJobs(jobs)
}

// outermost returns the paths that do not lie under another path of paths. A cut of
// a folder and one of its children moves the folder, which takes the child along, so
// the child's own move would find nothing once the folder has moved.
func outermost(paths []string) []string {
	var out []string
	for _, p := range paths {
		if !slices.ContainsFunc(paths, func(q string) bool { return q != p && expanel.Within(p, q) }) {
			out = append(out, p)
		}
	}
	return out
}

// runJobs starts the first of jobs and hands the rest to it. The next job starts
// when the one before it succeeds, so a multi-item paste runs in order and asks at
// each conflict. A failure ends the chain, and its notice shows the error.
func (m *Model) runJobs(jobs []fileJob) tea.Cmd {
	if len(jobs) == 0 {
		return nil
	}
	first := jobs[0]
	first.rest = jobs[1:]
	return m.runJob(first)
}

// checkRename asks the editor, in Update, whether a rename or move job would
// land on an open tab. The destination is the one the adapter will create, so the
// answer matches the disk operation. Other jobs need no check.
func checkRename(ops explorerOps, job fileJob) error {
	switch job.kind {
	case jobRename:
		return ops.CheckRename(job.path, filepath.Join(filepath.Dir(job.path), job.name))
	case jobMove:
		name := job.dstName
		if name == "" {
			name = filepath.Base(job.path)
		}
		return ops.CheckRename(job.path, filepath.Join(job.dir, name))
	}
	return nil
}

// runJob returns the command that runs job on the disk. The command does not
// touch the editor or the tree. Its result comes back as a fileResultMsg.
func (m *Model) runJob(job fileJob) tea.Cmd {
	if !m.needOps() {
		return nil
	}
	ops := m.ops
	if err := checkRename(ops, job); err != nil {
		m.notice = err.Error()
		return nil
	}
	return func() tea.Msg {
		ctx := context.Background()
		var (
			out string
			err error
		)
		switch job.kind {
		case jobCreate:
			out, err = ops.CreateFile(ctx, job.dir, job.name)
		case jobMkdir:
			out, err = ops.Mkdir(ctx, job.dir, job.name)
		case jobRename:
			out, err = ops.Rename(ctx, job.path, job.name)
		case jobMove:
			if as, ok := ops.(moveAsOps); ok {
				policy := job.policy
				if policy == 0 {
					policy = expanel.ConflictCancel
				}
				out, err = as.MoveAs(ctx, job.path, job.dir, job.dstName, policy)
			} else {
				out, err = ops.Move(ctx, job.path, job.dir)
			}
		case jobCopy:
			if job.dstName != "" {
				as, ok := ops.(copyAsOps)
				if !ok {
					err = errors.New("this port cannot rename a paste destination")
					break
				}
				out, err = as.CopyAs(ctx, job.path, job.dir, job.dstName, job.policy)
				break
			}
			out, err = ops.Copy(ctx, job.path, job.dir, job.policy)
		case jobTrash:
			err = ops.Trash(ctx, job.path)
		}
		return fileResultMsg{job: job, out: out, err: err}
	}
}

// fileDone applies the result of a disk operation, in Update, and returns the
// command of the next job of the same paste, if there is one. A copy that hit an
// existing destination asks before it overwrites. Any other failure goes to the
// notice row and ends the paste. A success moves the open tabs of a renamed or moved
// entry, closes the tabs of a trashed one, drops the clipboard paths and the picked
// rows that named it, reloads the tree in the background and reveals the new entry
// once the listing is applied. A cut paste does not go on when the clipboard was
// cleared, or replaced, while the move ran.
func (m *Model) fileDone(r fileResultMsg) tea.Cmd {
	if r.err != nil {
		conflict := errors.Is(r.err, expanel.ErrConflict)
		// A remote paste conflict offers overwrite, rename or cancel (Esc). The local
		// adapter keeps its existing overwrite/skip prompt.
		if conflict && m.remoteExplorerTarget() != "" && (r.job.kind == jobCopy || r.job.kind == jobMove) {
			m.askRemotePasteConflict(r.job)
			return nil
		}
		if r.job.kind == jobCopy && r.job.policy == expanel.ConflictCancel && conflict {
			m.askOverwrite(r.job)
			return nil
		}
		m.notice = r.err.Error()
		return nil
	}
	job := r.job
	switch job.kind {
	case jobRename, jobMove:
		if r.out != job.path {
			if err := m.ops.FollowRename(job.path, r.out); err != nil {
				m.notice = err.Error()
			}
			m.forgetClipboard(job.path)
			m.explorerPanel.DropPicked(job.path)
		} else if job.kind == jobMove {
			// A move into the directory the entry is in moves nothing, but it still
			// cancels a cut, as in Fresh. A rename to the same name keeps the clipboard.
			m.forgetClipboard(job.path)
		}
	case jobTrash:
		if err := m.ops.CloseDeleted(job.path); err != nil {
			m.notice = err.Error()
		}
		m.forgetClipboard(job.path)
		m.explorerPanel.DropPicked(job.path)
	}
	reveal := ""
	if job.kind != jobTrash {
		reveal = r.out
	}
	reload := m.reloadToReveal(reveal)
	if job.kind == jobMove && len(job.rest) > 0 && !m.cutHolds(job.rest[0].path) {
		m.notice = "Paste stopped: the cut was cancelled"
		return reload
	}
	return tea.Batch(reload, m.runJobs(job.rest))
}

// cutHolds reports whether the clipboard is still a cut that holds path.
func (m *Model) cutHolds(path string) bool {
	return m.clip != nil && m.clip.cut && slices.Contains(m.clip.paths, path)
}

// forgetClipboard drops path, and every path under it, from the clipboard and from
// the explorer's cut dimming. It runs when the entry is trashed, renamed or moved
// away, so the clipboard never names a path that a new file could take. A clipboard
// with no paths left is emptied.
func (m *Model) forgetClipboard(path string) {
	m.explorerPanel.DropCut(path)
	if m.clip == nil {
		return
	}
	var kept []string
	for _, p := range m.clip.paths {
		if !expanel.Within(p, path) {
			kept = append(kept, p)
		}
	}
	if len(kept) == 0 {
		m.clip = nil
		return
	}
	m.clip.paths = kept
}

// reveal selects the entry at path in the tree, expanding its parents. A failure
// goes to the notice row. A tree that can reveal from its loaded folders does so, so
// the update loop reads nothing; the listing that came before has loaded them.
func (m *Model) reveal(path string) {
	if m.tree == nil || path == "" {
		return
	}
	revealPath := m.tree.Reveal
	if r, ok := m.tree.(treeRevealer); ok {
		revealPath = r.RevealLoaded
	}
	if err := revealPath(path); err != nil {
		m.notice = err.Error()
	}
}

// openBrowser shows the file browser over the frame. folderOnly is the Switch
// Project mode: the list has folders only, and Enter chooses one.
func (m *Model) openBrowser(folderOnly bool) {
	m.splits.pick = nil
	dir := ""
	if m.tree != nil {
		dir = m.tree.Root()
	}
	b := browser.New(browser.Options{
		Dir:        dir,
		FolderOnly: folderOnly,
		Styles:     m.styles,
		Now:        m.now,
	})
	m.pushOverlay(b)
}

// openPalette shows the command palette with query as its input and returns the
// command that ranks its files, if the query is a file query. The file mode paints the
// selected file's preview when a Preview port is wired.
func (m *Model) openPalette(query string) tea.Cmd {
	st := m.paletteState()
	root := m.rootDir()
	remote := m.fileRoot != ""
	if remote {
		root = m.fileRoot
	}
	// The port is the launch's previewer: the local reader for a local launch, the
	// authority-backed remote reader for a remote one (run.go installs it).
	preview := m.root.ports.Preview
	p := palette.New(palette.Options{
		Query:    query,
		Root:     root,
		Remote:   remote,
		Commands: translateCommands(m.allCommands()),
		State:    &st,
		Buffers:  m.paletteBuffers(),
		Files:    m.files,
		Ranker:   m.ranker,
		Preview:  preview,
		Theme:    m.theme,
	})
	m.pushOverlay(p)
	return p.Init()
}

// paletteState describes the editor for the palette, which marks the commands the
// state does not allow as disabled.
func (m *Model) paletteState() commands.State {
	st := commands.State{
		AnyModified: slices.ContainsFunc(m.ed.Tabs(), func(t TabInfo) bool { return t.Modified }),
	}
	doc := m.ed.Active()
	if doc == nil {
		return st
	}
	st.TextBuffer, st.Editable, st.HasPath = true, true, hasPath(doc)
	st.Modified = doc.Modified()
	_, _, st.SelectionActive = doc.Selection()
	if m.lsp.port != nil {
		st.HasLSPConfig = m.lspStartable(m.lspLanguage(doc))
	}
	return st
}

// rootDir is the explorer's root, or the empty string when there is no explorer.
func (m *Model) rootDir() string {
	if m.tree == nil {
		return ""
	}
	return m.tree.Root()
}

// paletteBuffers lists the open tabs for the palette's buffer mode.
func (m *Model) paletteBuffers() []palette.Buffer {
	tabs := m.ed.Tabs()
	out := make([]palette.Buffer, len(tabs))
	for i, t := range tabs {
		out[i] = palette.Buffer{Name: t.Name, Modified: t.Modified}
	}
	return out
}

// rankerAdapter maps the root's FileRanker to the palette's Ranker. The two
// Ranked types are distinct, so the rows are copied.
type rankerAdapter struct {
	r FileRanker
}

// Rank ranks files against query and copies the matches into palette rows.
func (a rankerAdapter) Rank(query string, files []string, limit int) []palette.Ranked {
	matches := a.r.Rank(query, files, limit)
	out := make([]palette.Ranked, len(matches))
	for i, r := range matches {
		out[i] = palette.Ranked{Path: r.Path, Indexes: r.Indexes}
	}
	return out
}

// runCommand runs the action of a palette row. It returns the command the action
// started, and tea.Quit when the action quit the editor.
func (m *Model) runCommand(action string) tea.Cmd {
	defer m.plugSource(srcPalette)()
	if err := m.Run(action); err != nil {
		m.notice = failedNotice(menubar.Failed{Label: action, Err: err})
	}
	cmd := m.pending
	m.pending = nil
	if m.quitting {
		m.quitting = false
		return tea.Batch(cmd, m.quit())
	}
	return cmd
}

// gotoLine moves the caret to line and col, both one-based, in the active file.
func (m *Model) gotoLine(line, col int) {
	f, ok := m.ed.(fileEditor)
	if !ok {
		m.notice = "Go to line is not supported in this port"
		return
	}
	goTo := f.GotoLine
	if sg, ok := m.ed.(selectingGoto); ok {
		goTo = sg.GotoLineSelecting
	}
	if err := goTo(line, col); err != nil {
		m.notice = err.Error()
	}
	m.syncViewport()
}

// activateTab makes the tab at index i active. The editor steps through its tabs
// with next_buffer, so this takes at most one step per tab.
func (m *Model) activateTab(i int) {
	if t := m.tabPort(); t != nil {
		t.ActivateTab(i)
		m.syncViewport()
		return
	}
	for range len(m.ed.Tabs()) {
		tabs := m.ed.Tabs()
		if i < 0 || i >= len(tabs) || tabs[i].Active {
			break
		}
		m.ed.Do(editor.Action("next_buffer"), nil)
	}
	m.syncViewport()
}

// handleKey resolves one key press and returns the command it starts, which is
// tea.Quit when the editor reports quit.
func (m *Model) handleKey(k tea.Key) tea.Cmd {
	if cmd, panicked := m.pluginPanicKey(k); panicked {
		return cmd
	}
	kp, text := translateKey(k)
	if len(kp.Mods) == 0 && kp.Name == "F6" {
		m.cycleFocus()
		return nil
	}
	if m.dockKeyboard() {
		return m.dockKey(k, kp)
	}
	// While the tab strip has focus it takes every key that types text, shifted
	// letters included, so no text reaches the editor.
	if m.tabFocus && (len(kp.Mods) == 0 || text != "") {
		return m.tabKey(kp)
	}
	if len(kp.Mods) == 0 && !m.explorerFocus && m.welcomeVisible() {
		if cmd, ok := m.welcomeKey(kp.Name); ok {
			return cmd
		}
	}
	if cmd, ok := m.pluginKey(k); ok {
		return cmd
	}
	// The Contents section has its own key set: it is a tree, not the editor, so
	// its keys never type and the app-wide ones keep their Normal bindings.
	if m.contentsFocus {
		return m.contentsKey(kp, text)
	}
	// The Diagnostics section is a fix list: its keys move the selection and Enter
	// jumps, and the app-wide keys keep their Normal bindings.
	if m.diag.focus {
		return m.diagKey(kp, text)
	}
	ctx := KeyContextNormal
	if m.explorerFocus {
		ctx = KeyContextFileExplorer
	}
	if m.keys != nil {
		if cmd, handled := m.chordKey(kp, ctx); handled {
			return cmd
		}
		if cmd, ok := m.keys.Resolve(kp, ctx); ok {
			if m.suppressedMnemonic(kp, cmd) {
				return nil
			}
			if m.pluginGate(kp, ctx, cmd) {
				return nil
			}
			return m.dispatch(cmd)
		}
		if ctx == KeyContextFileExplorer {
			// The explorer keymap binds only its own keys. An application-wide key
			// that it does not bind keeps its Normal binding, as in Fresh.
			if cmd, ok := m.keys.Resolve(kp, KeyContextNormal); ok && explorerFallthrough[cmd.Action] {
				if m.suppressedMnemonic(kp, cmd) {
					return nil
				}
				return m.dispatch(cmd)
			}
		}
	}
	if m.explorerFocus {
		if cmd, ok := m.explorerEdgeKey(kp); ok {
			return cmd
		}
		// A typed letter that no binding claims searches the tree. It never
		// reaches the text.
		if r, ok := singleRune(text); ok {
			m.explorerPanel.TypeSearch(r, m.explorerGeometry(m.layout()))
		}
		return nil
	}
	if text != "" {
		insert := m.dispatch(Command{Action: "insert_char", Args: map[string]any{"text": text}})
		// The character is in the document now; schedule the auto-popup debounce
		// (or cancel a pending one) and keep whatever command the insert returned.
		return tea.Batch(insert, m.afterTypedCompletionText(text))
	}
	return nil
}

// explorerFallthrough are the actions that keep their Normal binding while the
// explorer has focus: Fresh's application-wide actions and its file-explorer UI set
// (is_application_wide_action and is_ui_fallthrough_action in
// refs/fresh/crates/fresh-editor/src/input/keybindings.rs), under gvim's names. The
// list holds only the names gvim's keymap binds. Some have no handler yet, such as
// show_help and split_vertical. They are listed anyway, so the explorer keeps
// Fresh's behaviour when they are implemented. Fresh's ForceQuit,
// ShowKeyboardShortcuts and ToggleMenuBar have no gvim binding, so they are not.
var explorerFallthrough = map[string]bool{
	// Application-wide.
	"quit": true, "save": true, "save_as": true, "save_all": true, "show_help": true,
	"prompt_cancel": true, "popup_cancel": true,
	// Global UI: the palette, the menu, the settings, the docks and live grep.
	"command_palette": true, "quick_open": true, "quick_open_buffers": true, "quick_open_files": true,
	"menu_activate": true, "menu_open": true, "open_settings": true,
	"toggle_utility_dock": true, "open_terminal_in_dock": true, "toggle_dock_focus": true,
	"focus_next_sidebar_section": true, "focus_prev_sidebar_section": true,
	"open_live_grep": true, "resume_live_grep": true, "cycle_live_grep_provider": true,
	// Layout: buffers, tabs and splits.
	"next_buffer": true, "prev_buffer": true, "close_tab": true, "close": true,
	"scroll_tabs_left": true, "scroll_tabs_right": true,
	"next_split": true, "prev_split": true, "split_horizontal": true, "split_vertical": true, "close_split": true,
	"next_pane": true, "prev_pane": true, "next_window": true, "prev_window": true,
	"increase_split_size": true, "decrease_split_size": true, "toggle_maximize_split": true,
	"switch_to_previous_tab": true,
	// Terminal control.
	"terminal_escape": true, "toggle_keyboard_capture": true, "terminal_paste": true,
	"open_terminal": true, "open_terminal_below": true, "open_terminal_right": true,
	// The explorer's own toggle.
	"toggle_file_explorer": true,
}

// clipboardActions are the editor actions the explorer takes over while it has
// focus: copy, cut and paste work on the selected entry, not on the text.
var clipboardActions = map[string]bool{"copy": true, "cut": true, "paste": true}

// dispatch runs one command. Focus and explorer actions are handled here. The
// editor runs everything else.
func (m *Model) dispatch(cmd Command) tea.Cmd {
	defer m.plugSource(srcKey)()
	m.recordCommand(cmd)
	defer m.plugCommand(cmd.Action, cmd.Args)()
	// A forced close is the Discard of the unsaved dialog, so it goes to the
	// editor. Any other close asks first.
	if forced, _ := cmd.Args["force"].(bool); cmd.Action != "close_tab" || !forced {
		if c, ok := m.uiAction(cmd.Action, cmd.Args); ok {
			return c
		}
	}
	if c, ok := m.explorerAction(cmd.Action); ok {
		return c
	}
	if m.explorerFocus && clipboardActions[cmd.Action] {
		return nil
	}
	if c, ok := m.editorPaste(cmd.Action, cmd.Args); ok {
		return c
	}
	if c, ok := m.lineTargetAction(cmd.Action, cmd.Args); ok {
		return c
	}
	res := m.editorDo(editor.Action(cmd.Action), cmd.Args)
	m.plugFailed(res.Err)
	m.plugUnknownAction(cmd.Action, res.Err)
	m.syncViewport()
	if res.Quit {
		return m.quit()
	}
	return m.clipboardOutArgs(cmd.Action, cmd.Args, res)
}

// uiAction runs the command when the root handles it. It reports whether it did,
// and the command the action starts, if any. args are the command's arguments, and
// nil when the action comes from Run, which takes no arguments. The file browser
// opens for "open" (Ctrl+O and File > Open File) and, in folder mode, for
// "switch_project". The palette opens for "quick_open" (Ctrl+P) and
// "command_palette" in command mode, for "quick_open_files" in file mode and for
// "quick_open_buffers" in buffer mode. "menu_activate" (F10) opens the menu bar,
// and "menu_open" opens the dropdown named by its "name" argument. Both do nothing
// while the menu bar is hidden. "toggle_line_numbers" flips the gutter, and
// "scroll_tabs_left" and "scroll_tabs_right" move the tab strip one tab.
// "file_explorer_search_clear" (Esc in the explorer) gives the focus to the editor
// unless the panel had something to clear, and a cut that it clears also empties
// the clipboard. "file_explorer_search_backspace" shortens the explorer's search
// text, and only while the explorer has focus. The split and compare actions go to
// splitAction.
func (m *Model) uiAction(action string, args map[string]any) (tea.Cmd, bool) {
	if cmd, ok := m.pluginAction(action, args); ok {
		return cmd, true
	}
	if cmd, ok := m.fixAction(action, args); ok {
		return cmd, true
	}
	if cmd, ok := m.settingsAction(action, args); ok {
		return cmd, true
	}
	if m.utilityAction(action) {
		return nil, true
	}
	if cmd, ok := m.shellAction(action, args); ok {
		return cmd, true
	}
	if cmd, ok := m.viewAction(action, args); ok {
		return cmd, true
	}
	if cmd, ok := m.updateAction(action); ok {
		return cmd, true
	}
	if m.encodingAction(action, args) {
		return nil, true
	}
	switch action {
	case "toggle_file_explorer":
		return m.toggleExplorer(), true
	case "focus_file_explorer":
		return m.focusExplorer(), true
	case "focus_editor":
		m.focusEditor()
	case "new":
		// A new tab takes the keyboard, as Fresh's new buffer does. The editor runs
		// the action, so it is not handled here.
		m.focusEditor()
		return nil, false
	case "menu_activate":
		if m.hideMenuBar {
			m.notice = menuHiddenNotice
			break
		}
		m.syncMenu()
		m.menu = m.menu.Activate()
	case "menu_open":
		name, _ := args["name"].(string)
		m.openMenu(name)
	case "toggle_line_numbers":
		m.hideLineNumbers = !m.hideLineNumbers
	case "scroll_tabs_left":
		m.scrollTabs(-1)
	case "scroll_tabs_right":
		m.scrollTabs(1)
	case "file_explorer_search_clear":
		cut := m.clip != nil && m.clip.cut
		if !m.explorerPanel.ClearTransient() {
			m.focusEditor()
		}
		if cut && !m.explorerPanel.CutPending() {
			m.clip = nil
		}
	case "file_explorer_search_backspace":
		if !m.explorerOpen || !m.explorerFocus {
			return nil, false
		}
		m.explorerPanel.SearchBackspace(m.explorerGeometry(m.layout()))
	case "markdown_toc_show":
		m.markdownTOCShown()
	case "markdown_toc_close":
		m.markdownTOCClosed()
	case "markdown_toc_toggle_follow":
		m.markdownTOCToggleFollow()
	case "markdown_toc_focus":
		m.focusContents()
	case "markdown_mode_source", "markdown_mode_rendered", "markdown_mode_dual", "markdown_toggle_dual", "markdown_cycle_mode":
		m.markdownModeAction(action)
	case "open":
		m.openBrowser(false)
	case "switch_project":
		m.openBrowser(true)
	case "quick_open", "command_palette":
		return m.openPalette(">"), true
	case "quick_open_files":
		return m.openPalette(""), true
	case "quick_open_buffers":
		return m.openPalette("#"), true
	case "goto_line":
		m.openGoto("")
	case "search", "find", "replace", "find_next", "find_previous":
		if !m.hasDocument() {
			m.notice = noDocumentNotice
			return nil, true
		}
		switch action {
		case "search", "find":
			m.openFind(false)
		case "replace":
			m.openFind(true)
		case "find_next":
			m.findNavigate(true)
		default:
			m.findNavigate(false)
		}
	case "close_tab":
		if !m.hasDocument() {
			m.notice = noDocumentNotice
			return nil, true
		}
		return m.requestClose(m.activeTab()), true
	case "save":
		return m.save(), true
	case "save_as":
		m.askSaveAs(flow{})
	case "save_all":
		return m.saveAll(), true
	case "quit":
		return m.quitLoop(), true
	case "detach_daemon":
		if m.detach == nil {
			m.notice = "Not attached to a daemon"
			break
		}
		m.detach()
		m.notice = "Detached from the daemon"
	case "split_horizontal", "split_vertical", "next_split", "prev_split", "close_split",
		"next_pane", "prev_pane", "next_window", "prev_window",
		"increase_split_size", "decrease_split_size", "toggle_maximize_split", "switch_to_previous_tab",
		"compare_next_difference", "compare_prev_difference", "compare_switch_side",
		"compare_files", "compare_with_file", "compare_copy_change", "compare_take_change", "compare_close":
		m.splitAction(action)
	default:
		if cmd, ok := m.workspaceAction(action); ok {
			return cmd, true
		}
		if cmd, ok := m.lspAction(action); ok {
			return cmd, true
		}
		if cmd, ok := m.dockAction(action); ok {
			return cmd, true
		}
		if cmd, ok := m.rootAction(action); ok {
			return cmd, true
		}
		if cmd, ok := m.featureAction(action); ok {
			return cmd, true
		}
		return m.findAction(action)
	}
	return nil, true
}

// menuHiddenNotice is what Open Menu Bar says while the bar is hidden, so the command is not
// silent.
const menuHiddenNotice = "The menu bar is hidden: run Toggle Menu Bar to show it"

// openMenu opens the dropdown of the bar label called name, the way Fresh's
// menu_open does. The name is matched without case. A name that is not a label of
// the bar leaves the menu as it is, so Explorer opens nothing while the explorer
// has no focus. A hidden menu bar opens nothing.
func (m *Model) openMenu(name string) {
	if m.hideMenuBar {
		m.notice = menuHiddenNotice
		return
	}
	m.syncMenu()
	i := slices.IndexFunc(menubar.Labels(m.explorerFocus), func(t string) bool {
		return strings.EqualFold(t, name)
	})
	m.menu = m.menu.Open(i)
}

// explorerAction runs one file_explorer_* key action, or a clipboard action
// while the explorer has focus, through the explorer panel. It reports whether
// the panel handled it. The panel answers only while it is open, and its
// command, if any, is returned for the caller.
func (m *Model) explorerAction(action string) (tea.Cmd, bool) {
	if !m.explorerOpen {
		return nil, false
	}
	fromClipboard := m.explorerFocus && clipboardActions[action]
	if !fromClipboard && !strings.HasPrefix(action, "file_explorer_") {
		return nil, false
	}
	next, cmd, ok := m.explorerPanel.Action(action, m.explorerGeometry(m.layout()))
	m.explorerPanel = next
	return cmd, ok
}

// explorerGeometry is the panel's place in the frame laid out as l.
func (m *Model) explorerGeometry(l layout) expanel.Geometry {
	top, bottom := l.ExTitleY, l.ExBottom
	if len(l.Sections) > 0 {
		// The panel is the explorer's section now: its header row is the title,
		// and the row after its body is the next section's header.
		sec := l.Sections[0]
		top, bottom = sec.HeaderY, sec.BodyBottom+1
	}
	return expanel.Geometry{
		X: l.ExX, Width: l.Ex, Top: top, Bottom: bottom, Right: l.ExRight,
		Focused: m.explorerFocus, Marks: m.root.gitMarks, Icons: m.view.icons,
		Decor: m.explorerRowDecor,
	}
}

// sidebarSection returns the layout section at index, for the mouse routing that
// has to know which kind of section a row belongs to.
func (m *Model) sidebarSection(index int) (sidebar.Section, bool) {
	sections := m.layout().Sections
	if index < 0 || index >= len(sections) {
		return sidebar.Section{}, false
	}
	return sections[index], true
}

// handleClick routes a left press to the part of the frame under it, and a
// middle press on a tab closes the tab. A press on the explorer goes to the
// panel, which answers with its own messages. Presses that have no effect yet
// are dropped here.
func (m *Model) handleClick(msg tea.MouseClickMsg) tea.Cmd {
	mo := tea.Mouse(msg)
	// A left press may be a double or a triple click; the dock's word/line
	// selection and the editor's word/line selection both read the count once.
	count := 0
	if mo.Button == tea.MouseLeft {
		count = m.clicks.press(m.now(), mo.X, mo.Y)
	}
	// The dock's rows belong to the shell: the dock decides whether the press is
	// forwarded, opens a link, scrolls the scrollback or only takes the focus.
	if cmd, taken := m.dockMouse(msg, mo.Y, count); taken {
		return cmd
	}
	t := m.layout().Hit(mo.X, mo.Y)
	if mo.Button == tea.MouseMiddle {
		if t.Kind == hitTab || t.Kind == hitTabClose {
			return m.requestClose(t.Index)
		}
		return nil
	}
	if mo.Button == tea.MouseRight {
		// Ctrl+Right-click inspects the theme colors at the clicked cell; a plain
		// right click opens the context menu.
		if mo.Mod&tea.ModCtrl != 0 {
			m.inspectThemeAt(mo.X, mo.Y)
			return nil
		}
		return m.rightClick(msg)
	}
	if mo.Button != tea.MouseLeft {
		return nil
	}
	m.noteMods(mo)
	m.drag = drag{}
	switch t.Kind {
	case hitTab:
		m.focusEditor()
		m.activateTab(t.Index)
		m.drag = drag{active: true, tab: &tabDrag{index: t.Index, startX: mo.X, startY: mo.Y}}
	case hitTabClose:
		return m.requestClose(t.Index)
	case hitTabNew:
		// As Fresh's "+", the button opens a small menu of what a new tab can be.
		m.showContextMenu(mo, plusMenuItems())
	case hitTabScrollBefore:
		m.scrollTabs(-1)
	case hitTabScrollAfter:
		m.scrollTabs(1)
	case hitWelcomeRow:
		m.focusEditor()
		return m.runWelcome(t.Index)
	case hitExplorerRow, hitExplorerBody:
		// The press is resolved against the tree as it is scrolled now. The focus
		// comes after it and does not move the view, so a wheel scroll is kept.
		cmd := m.explorerMouse(msg)
		m.takeExplorerFocus()
		return cmd
	case hitExplorerTitleClose, hitExplorerTitleReload, hitExplorerGrip:
		return m.explorerMouse(msg)
	case hitSidebarHeader:
		if sec, ok := m.sidebarSection(t.Section); ok && sec.Kind == sidebar.KindDiagnostics {
			m.pressDiagHeader()
			return nil
		}
		return m.pressSidebarHeader(t.Section, mo.Y)
	case hitSidebarClose:
		if sec, ok := m.sidebarSection(t.Section); ok && sec.Kind == sidebar.KindDiagnostics {
			// The Diagnostics section has no close button (NoClose); a stray press
			// on that cell closes it, as the palette row does.
			m.diag.open = false
			m.focusEditor()
			return nil
		}
		m.closeContents()
		return nil
	case hitSidebarRow:
		if sec, ok := m.sidebarSection(t.Section); ok && sec.Kind == sidebar.KindDiagnostics {
			return m.pressDiagRow(t.Index)
		}
		return m.pressContentsRow(t.Index, mo.X)
	case hitSidebarBody:
		if sec, ok := m.sidebarSection(t.Section); ok && sec.Kind == sidebar.KindDiagnostics {
			m.takeDiagFocus()
			return nil
		}
		m.takeContentsFocus()
		return nil
	case hitStatusPalette:
		return m.openPalette(">")
	case hitStatusSegment:
		switch t.Index {
		case frame.StatusSegmentMessages:
			// Fresh's {messages} opens the status log; gvim's is the warnings list.
			return m.dispatch(Command{Action: "show_warnings"})
		case frame.StatusSegmentTerminalRestart:
			return m.dispatch(Command{Action: "restart_terminal"})
		}
		return m.statusSegmentClick(t.Index)
	case hitStatusPlugin:
		return m.plugStatusClick(t.Index)
	case hitEditorText, hitGutter, hitScrollbarTrack:
		m.pressPane(t.Pane, mo.X, mo.Y, count)
	case hitSplitBorder:
		m.pressBorder(t.Index)
	}
	return nil
}

// explorerMouse gives a mouse message to the explorer panel, which runs its
// press, drag, release and wheel handling. The panel ignores what is not its
// own, so the root sends every mouse message here. A hidden panel gets zero
// geometry and ignores the message. closeExplorer resets the panel, so a drag
// that was in progress does not survive a hide.
func (m *Model) explorerMouse(msg tea.Msg) tea.Cmd {
	if m.tree == nil {
		return nil
	}
	g := expanel.Geometry{}
	if m.explorerOpen {
		g = m.explorerGeometry(m.layout())
	}
	next, cmd := m.explorerPanel.Update(msg, g)
	m.explorerPanel = next
	return cmd
}

// pressEditor is a press in the editor body, already resolved to hit in pane p by
// pressPane. A text or gutter cell places the caret, a count of 2 selects the word
// and a count of 3 the line. A press on the scrollbar track moves the viewport to
// the press, and a press on the thumb grabs it for the drag.
func (m *Model) pressEditor(p editorpane.Pane, hit editorpane.Hit, count int) tea.Cmd {
	m.lastPress = count
	m.focusEditor()
	switch hit.Kind {
	case editorpane.HitScrollbarThumb:
		m.drag = drag{active: true, thumb: true}
	case editorpane.HitScrollbarTrack:
		m.ed.ScrollTo(p.ScrollTopFor(m.layout().FocusedPane().W, m.bodyRows(), hit.Row))
		m.drag = drag{active: true, thumb: true}
	case editorpane.HitHScrollbar:
		m.drag = drag{active: true, hbar: true}
		m.panBar(hit.Col + m.layout().FocusedPane().X)
	case editorpane.HitText, editorpane.HitGutter:
		if hit.Fold && m.toggleFoldAt(hit.Pos.Line) {
			return nil
		}
		if count == 1 && !m.mx.extend {
			if t, ok := m.lineTargetAt(m.ed.Active(), hit.Pos.Line); ok {
				m.drag = drag{}
				return m.runLineTarget(t, m.ed.Active())
			}
		}
		if hit.Below > 0 && count == 1 && !m.mx.extend && m.placeVirtualLine(hit) {
			return nil
		}
		m.drag = m.placeCaret(hit.Pos, count, hit.Virtual)
	}
	return nil
}

// placeCaret moves the caret to pos, or selects the word or the line under it
// for counts 2 and 3. It returns the drag that follows the press, which grows
// the selection by the same unit.
func (m *Model) placeCaret(pos buffer.Pos, count int, virtual int) drag {
	if count == 1 && m.mx.extend {
		m.ed.SetCursor(pos, true)
		return drag{active: true, unit: editor.UnitChar}
	}
	m.collapseCarets()
	switch count {
	case 2:
		m.ed.SetCursor(pos, false)
		m.ed.Do("select_word", nil)
		return drag{active: true, unit: editor.UnitWord}
	case 3:
		m.ed.SetCursor(buffer.Pos{Line: pos.Line}, false)
		m.ed.ExtendSelection(pos, editor.UnitLine)
		return drag{active: true, unit: editor.UnitLine}
	}
	m.ed.SetCursor(pos, false)
	m.parkVirtualCaret(virtual)
	return drag{active: true, unit: editor.UnitChar}
}

// placeVirtualLine parks the caret on the virtual line below the buffer end that a
// press asked for, when the editor has vertical virtual space on. It reports
// whether it took the press; false falls back to the clamped caret.
func (m *Model) placeVirtualLine(hit editorpane.Hit) bool {
	if m.pageViewActive() {
		return false
	}
	vl, ok := m.ed.(virtualLinesEditor)
	if !ok {
		return false
	}
	m.collapseCarets()
	m.ed.SetCursor(hit.Pos, false)
	if !vl.SetVirtualLines(hit.Below, hit.LineCol) {
		return false
	}
	m.drag = drag{active: true, unit: editor.UnitChar}
	return true
}

// parkVirtualCaret puts the caret n display columns past the end of its line when
// a press asked for it and the editor has virtual space on. The click on the line
// end has already put the caret there.
func (m *Model) parkVirtualCaret(n int) {
	if n <= 0 {
		return
	}
	if m.pageViewActive() {
		return
	}
	if vs, ok := m.ed.(VirtualSpaceEditor); ok && vs.VirtualSpaceEnabled() {
		vs.SetVirtualColumns(n)
	}
}

// handleDrag moves the grab of a text selection or of the scrollbar thumb. The
// pointer may leave the pane: the thumb follows the row, and the text end
// follows the nearest cell. It returns the command that keeps a text drag
// scrolling while the pointer is held outside the pane.
func (m *Model) handleDrag(mo tea.Mouse) tea.Cmd {
	if m.divider.active && mo.Button == tea.MouseLeft {
		m.dragDivider(mo.Y)
		return nil
	}
	if !m.drag.active || mo.Button != tea.MouseLeft {
		return nil
	}
	if m.drag.border != nil {
		m.dragBorder(mo.X, mo.Y)
		return nil
	}
	if m.drag.tab != nil {
		m.dragTab(mo)
		return nil
	}
	doc := m.ed.Active()
	if doc == nil {
		return nil
	}
	fp := m.layout().FocusedPane()
	w, h := fp.W, fp.H
	if h <= 0 || w <= 0 {
		return nil
	}
	if m.drag.hbar {
		m.panBar(mo.X)
		return nil
	}
	if m.drag.thumb {
		// The track is the text rows, as the press maps it: the horizontal bar under
		// them is not part of it.
		rows := m.bodyRows()
		if rows <= 0 {
			return nil
		}
		y := min(max(mo.Y-fp.Y, 0), rows-1)
		m.ed.ScrollTo(m.paneFor(fp, doc).ScrollTopFor(w, rows, y))
		return nil
	}
	m.drag.last = mo
	return m.dragSelect(false)
}

// handleWheel scrolls the panel's tree over the explorer, and the editor
// viewport over the editor body, by wheelRows per notch. The caret stays where
// it is in both cases, and a wheel over an unfocused split pane scrolls that pane.
func (m *Model) handleWheel(msg tea.MouseWheelMsg) tea.Cmd {
	mo := tea.Mouse(msg)
	l := m.layout()
	if cmd, taken := m.dockMouse(msg, mo.Y, 1); taken {
		return cmd
	}
	if mo.Y == l.StripY && mo.X >= l.EditorX {
		// The tab row scrolls the strip one tab per notch.
		switch mo.Button {
		case tea.MouseWheelUp:
			m.scrollTabs(-1)
		case tea.MouseWheelDown:
			m.scrollTabs(1)
		}
		return nil
	}
	t := l.Hit(mo.X, mo.Y)
	switch t.Kind {
	case hitExplorerRow, hitExplorerBody, hitExplorerTitleClose, hitExplorerTitleReload, hitExplorerGrip:
		return m.explorerMouse(msg)
	case hitSidebarRow, hitSidebarBody, hitSidebarHeader:
		m.wheelContents(mo.Button == tea.MouseWheelUp)
		return nil
	case hitSidebarClose:
		return nil
	case hitEditorText, hitGutter, hitScrollbarTrack:
		if m.panWheel(t.Pane, mo) {
			return nil
		}
		delta := 0
		switch mo.Button {
		case tea.MouseWheelUp:
			delta = -wheelRows
		case tea.MouseWheelDown:
			delta = wheelRows
		}
		if delta != 0 {
			m.scrollPane(t.Pane, delta)
		}
	}
	return nil
}

// toggleExplorer hides the panel when it is open, focused or not, and shows,
// reveals and focuses it when it is hidden. Hiding from the panel's own focus
// gives the focus to the editor, as Fresh's Ctrl+B does. With no explorer, it does
// nothing. The returned command is the background folder load a reveal needed.
func (m *Model) toggleExplorer() tea.Cmd {
	switch {
	case m.tree == nil:
		return nil
	case m.explorerFocus || m.explorerOpen:
		m.closeExplorer()
		return nil
	default:
		m.explorerAutoOpened = false
		return m.focusExplorer()
	}
}

// focusExplorer shows the panel when it is hidden, gives it focus, reveals the
// active file with Fresh's feedback ("File explorer focused; ..." when there is
// nothing to reveal) and scrolls the tree so its row is in view. A mouse press
// takes the focus with takeExplorerFocus instead, so the view stays.
func (m *Model) focusExplorer() tea.Cmd {
	if m.tree == nil {
		return nil
	}
	// The keyboard does not go to a folded section with no rows on screen.
	m.explorerCollapsed = false
	m.takeExplorerFocus()
	cmd := m.revealActiveFile(true)
	m.followSelection()
	return cmd
}

// takeExplorerFocus shows the panel when it is hidden and gives it keyboard focus.
// It leaves the tree's scroll position alone.
func (m *Model) takeExplorerFocus() {
	if m.tree == nil {
		return
	}
	if !m.explorerOpen {
		m.explorerOpen = true
		m.syncViewport()
	}
	m.tabFocus = false
	m.dock.SetFocus(false)
	m.explorerFocus = true
	m.contentsFocus = false
	m.contents.SetFocus(false)
	m.diag.focus = false
}

// focusEditor gives keyboard focus back to the editor. The panel stays visible.
func (m *Model) focusEditor() {
	m.explorerFocus = false
	m.contentsFocus = false
	m.contents.SetFocus(false)
	m.diag.focus = false
	m.tabFocus = false
	m.dock.SetFocus(false)
}

// The places keyboard focus can be, in F6 order. The menu is open while it has
// focus, and the explorer is skipped while it is hidden.
type focusStage int

const (
	stageEditor focusStage = iota
	stageMenu
	stageTabs
	stageExplorer
	stageContents
	stageDiagnostics
)

// stage reports where keyboard focus is now.
func (m *Model) stage() focusStage {
	switch {
	case m.menu.IsOpen():
		return stageMenu
	case m.tabFocus:
		return stageTabs
	case m.explorerFocus:
		return stageExplorer
	case m.contentsFocus:
		return stageContents
	case m.diag.focus:
		return stageDiagnostics
	}
	return stageEditor
}

// cycleFocus moves keyboard focus to the next stage: menu, tabs, explorer,
// editor, and back to the menu. F6 sends it. The menu is skipped while the menu
// bar is hidden.
func (m *Model) cycleFocus() {
	var order []focusStage
	if !m.hideMenuBar {
		order = append(order, stageMenu)
	}
	order = append(order, stageTabs)
	if m.tree != nil {
		order = append(order, stageExplorer)
	}
	if m.contents.Open() {
		order = append(order, stageContents)
	}
	// The Diagnostics section follows Contents in the F6 order, matching its place
	// in the column.
	if m.diag.Open() {
		order = append(order, stageDiagnostics)
	}
	order = append(order, stageEditor)
	cur := m.stage()
	next := stageEditor
	for i := range order {
		if order[i] == cur {
			next = order[(i+1)%len(order)]
			break
		}
	}
	if next == stageExplorer && (!m.explorerOpen || m.tree == nil) {
		next = stageEditor
	}
	m.setStage(next)
}

// setStage gives keyboard focus to the stage s.
func (m *Model) setStage(s focusStage) {
	m.menu = m.menu.Close()
	switch s {
	case stageMenu:
		m.syncMenu()
		m.menu = m.menu.Activate()
		m.focusEditor()
	case stageTabs:
		m.focusEditor()
		m.tabFocus = true
	case stageExplorer:
		m.focusExplorer()
	case stageContents:
		m.focusContents()
	case stageDiagnostics:
		m.focusDiagnostics()
	default:
		m.focusEditor()
	}
}

// tabKey handles a key while the tab strip has focus: Left and Right step
// through the tabs, and Enter, Down or Escape returns focus to the editor. The
// other keys are ignored, so they do not type into the text.
func (m *Model) tabKey(kp KeyPress) tea.Cmd {
	switch kp.Name {
	case "Left":
		return m.dispatch(Command{Action: "prev_buffer"})
	case "Right":
		return m.dispatch(Command{Action: "next_buffer"})
	case "Enter", "Down", "Escape":
		m.focusEditor()
	}
	return nil
}

// closeExplorer hides the panel and gives up its focus. The panel is rebuilt, so
// a grip or scrollbar gesture in progress ends with it. The pending cut carries over,
// because the clipboard still holds it and the rows must keep their dimming.
func (m *Model) closeExplorer() {
	m.explorerOpen = false
	m.explorerFocus = false
	m.explorerAutoOpened = false
	m.contentsFocus = false
	m.contents.SetFocus(false)
	m.divider = dividerState{}
	if m.tree != nil {
		m.explorerPanel = expanel.New(m.tree, m.styles, m.now).CarryCut(m.explorerPanel)
	}
	m.syncViewport()
}

// bodyRows is the number of text rows of the focused editor pane in the current frame.
func (m *Model) bodyRows() int {
	return m.focusedBodyRows()
}

// explorerBodyRows is the number of tree rows the explorer panel shows: the rows
// between its title and its bottom border, as the panel counts them.
func (m *Model) explorerBodyRows() int {
	l := m.layout()
	return max(l.ExBottom-l.ExBodyTop, 0)
}

// syncViewport tells the editor how big its pane is: the focused pane's text rows, and
// its columns right of the gutter.
func (m *Model) syncViewport() {
	if m.width == 0 {
		return
	}
	fp := m.layout().FocusedPane()
	m.ed.SetViewport(m.focusedBodyRows(), fp.TextW)
}

// layoutInput gathers the state the geometry depends on.
func (m *Model) layoutInput() layoutInput {
	split, leaves := m.splitInput()
	in := layoutInput{
		ExplorerFocus:   m.explorerFocus,
		ExplorerWidth:   m.explorerWidth,
		ExplorerSide:    m.explorerSide,
		Split:           split,
		Leaves:          leaves,
		Tabs:            m.stripTabs(),
		TabFirst:        m.tabFirst,
		Welcome:         m.welcomeVisible(),
		Recents:         m.recents,
		DoorKeys:        m.welcomeDoorKeys(),
		Status:          m.statusView(),
		HideMenuBar:     m.hideMenuBar,
		HideStatus:      m.statusHidden(),
		HideLineNumbers: m.hideLineNumbers,
		DockOpen:        m.dock.Open(),
		Maximized:       m.splits.maximized,
	}
	if m.findOpen() {
		in.FindRows = m.findBar.Rows(m.height)
	}
	if doc := m.ed.Active(); doc != nil {
		in.Lines = displayLines(doc)
	}
	if m.explorerOpen && m.tree != nil {
		in.ExplorerOpen = true
		in.ExplorerRows = len(m.tree.Rows()) - m.tree.ScrollOffset()
		in.Sidebar = m.sidebarSpecs()
	}
	m.viewLayout(&in)
	return in
}

// layout is the geometry of the current frame. Painting and mouse hit testing
// both read it.
func (m *Model) layout() layout {
	return frame.ComputeLayout(m.width, m.height, m.layoutInput())
}

// statusHidden reports whether the status row gives way to a suggestion list or to
// the file browser. Both are overlays on the stack, and the palette is the
// suggestion list.
func (m *Model) statusHidden() bool {
	return slices.ContainsFunc(m.overlays, func(o Overlay) bool {
		switch o.(type) {
		case *palette.Palette, *browser.Browser:
			return true
		}
		return false
	})
}

// statusView gathers the status row contents for the active document. It runs
// on every frame, so it does no lexer or text work: the encoding comes from the
// document and the language is memoized by file name.
func (m *Model) statusView() statusView {
	s := statusView{Message: m.ed.Status(), EOL: "LF", Encoding: "UTF-8", Language: "Text"}
	if m.notice != "" {
		s.Message = m.notice
	}
	s.Trust, s.TrustWarn = m.trustStatus()
	s.DiagSlot, s.Diags = m.lspShowsDiags(), m.lspDiagCounts()
	s.LSP = m.lspPill()
	s.Row = m.statusRow()
	s.Cursors = 1
	s.Update = m.updateText()
	if m.dock.Open() && m.dock.Exited() {
		// Fresh's {terminal_restart} for a terminal whose process quit: gvim's
		// terminal is the dock, so its exited shell is the affordance.
		if info, ok := m.dock.ExitedRestartInfo(); ok {
			s.TerminalRestart = terminalRestartText(info)
		}
	}
	if doc := m.ed.Active(); doc != nil {
		line, col := virtualPosition(doc)
		s.Position = "Ln " + strconv.Itoa(line+1) + ", Col " + strconv.Itoa(col+1)
		s.Filename, s.FilenameModified = m.statusFilename(doc), doc.Modified()
		s.Line, s.Col = line+1, col+1
		s.Lines = displayLines(doc)
		s.Large = largeLabel(doc)
		s.EOL = doc.LineEnding()
		s.Encoding = doc.Encoding()
		s.Language = m.languageFor(doc.Name())
		if ro, ok := doc.(interface{ ReadOnly() bool }); ok {
			s.ReadOnly = ro.ReadOnly()
		}
		if cl, ok := doc.(editorpane.CaretLister); ok {
			s.Cursors += len(cl.Cursors())
		}
	}
	m.viewStatus(&s)
	s.PaletteHint = m.paletteHint()
	s.Clock = m.now().Format(clockFormat)
	s.Chord = m.statusChord()
	s.Extra = m.plugStatusItems()
	if item, ok := m.authoritySegment(); ok {
		s.Extra = append(s.Extra, item)
	}
	if item, ok := m.workspaceSegment(); ok {
		s.Extra = append(s.Extra, item)
	}
	return s
}

// terminalRestartText is Fresh's {terminal_restart} label: the arrow, "Resume
// <program>" when the restart rejoins an agent, "Restart <program>" for a launch
// command, "Restart terminal" for a plain shell, and " (exit N)" only for a
// non-zero code (exit 0 is omitted, as Fresh does). A signal exit shows
// " (signal)" (gvim's extension: the Rust indicator has no signal field).
func terminalRestartText(info dock.RestartInfo) string {
	label := uiText("gvim.status.terminal_restart_shell", "⟳ Restart terminal")
	if info.Program != "" {
		if info.ResumesAgent {
			label = uiTextVars("gvim.status.terminal_resume", "⟳ Resume %{program}", map[string]any{"program": info.Program})
		} else {
			label = uiTextVars("gvim.status.terminal_restart", "⟳ Restart %{program}", map[string]any{"program": info.Program})
		}
	}
	if info.ExitKnown {
		if info.ExitCode < 0 {
			label = uiTextVars("gvim.status.terminal_restart_signal", "%{label} (signal)", map[string]any{"label": label})
		} else if info.ExitCode != 0 {
			label = uiTextVars("gvim.status.terminal_restart_code", "%{label} (exit %{code})", map[string]any{"label": label, "code": info.ExitCode})
		}
	}
	return label
}

// virtualPosition returns the line and column the status row shows for the
// document caret: the byte position adjusted for virtual space, as Fresh's
// virtual_space_adjusted_position does. A caret on a virtual line advances the
// line by that many rows and takes the row's column; a horizontal virtual caret
// reads the line's column plus its virtual columns, which read as spaces. A
// document without virtual space (or a caret on text) is unchanged.
func virtualPosition(doc Document) (line, col int) {
	c := doc.Cursor()
	line, col = c.Line, c.Col
	if vld, ok := doc.(editorpane.VirtualLinesDocument); ok {
		if n := vld.VirtualLines(); n > 0 {
			return line + n, vld.VirtualLineColumn()
		}
	}
	if vd, ok := doc.(editorpane.VirtualDocument); ok {
		col += vd.VirtualColumns()
	}
	return line, col
}

// statusRow is the configured editor.status_bar of the session: Fresh's default
// element lists when the view options leave them unset (a TUI-only test, a hand-built
// model), and the configured lists — an empty one paints nothing — when the app
// passes them.
func (m *Model) statusRow() frame.StatusRow {
	opts := m.view.opts.StatusBar
	left := frame.ParseStatusElements(opts.Left)
	if left == nil {
		left = frame.DefaultStatusLeft()
	}
	right := frame.ParseStatusElements(opts.Right)
	if right == nil {
		right = frame.DefaultStatusRight()
	}
	return frame.StatusRow{Left: left, Right: right, Separator: opts.Separator}
}

// noDocumentNotice is the status text for an action that needs an open tab.
const noDocumentNotice = "No open document. Use File > New File to create one."

// untitledName is the display name the editor gives a buffer with no path.
const untitledName = "[No Name]"

// languageFor returns the language label for a document named name. The lexer
// is found from the file name alone, and the result is cached per name, because
// matching a name against every lexer is too slow to repeat on each frame. An
// unnamed buffer and plain text are "Text".
func (m *Model) languageFor(name string) string {
	if name == untitledName {
		return "Text"
	}
	if lang, ok := m.langs[name]; ok {
		return lang
	}
	lang := "Text"
	if lx := syntax.Detect(name, nil); lx != nil {
		if n := lx.Config().Name; n != "" && !strings.EqualFold(n, "plaintext") && !strings.EqualFold(n, "text only") && n != "fallback" {
			lang = n
		}
	}
	if m.langs == nil {
		m.langs = make(map[string]string)
	}
	m.langs[name] = lang
	return lang
}

// View implements tea.Model. It paints a full frame into the alternate screen.
// The mouse is in all-motion mode, so hover and drag reach the model. The
// terminal caret sits on the editor caret when the editor has the focus and no
// overlay is open.
func (m *Model) View() tea.View {
	content := ""
	if m.width > 0 && m.height > 0 {
		content = m.paint().String()
	}
	v := tea.NewView(content)
	v.AltScreen = true
	// A remote authority reconnects when the terminal window is activated, so the
	// program must report focus.
	v.ReportFocus = m.auth.Status != nil
	v.MouseMode = tea.MouseModeAllMotion
	if x, y, ok := m.caret(); ok {
		v.Cursor = tea.NewCursor(x, y)
	}
	m.viewTerminal(&v)
	return v
}

// paint draws the frame into a new grid. The editor pane fills the body to the
// right of the explorer. Each other band is painted from the layout runs, so the
// hit map and the paint cannot drift apart. The open overlays are drawn last.
func (m *Model) paint() *grid.Grid {
	g := grid.New(m.width, m.height)
	m.paintInto(g)
	return g
}

// paintInto draws the frame into g. The theme inspector calls it once with key
// capture enabled on a fresh grid, so the paint path itself is the same in both
// cases; normal frames capture nothing.
func (m *Model) paintInto(g *grid.Grid) {
	l := m.layout()
	if l.Gated {
		g.Put(0, 0, g.Width(), gateText, grid.Style{})
		return
	}
	body := style.TaggedCell(m.styles.Editor, "editor", "fg", "editor", "bg")
	for y := range g.Height() {
		g.Fill(0, g.Width(), y, body)
	}
	m.paintEditor(g, l)
	m.paintDropZone(g)
	m.dock.Draw(g, l, m.styles)
	tabstrip.Draw(g, m.styles, l.EditorX, l.StripY, l.Strip)
	if l.ExplorerOpen {
		m.paintSidebar(g, l)
	}
	m.paintStatus(g, l)
	// The menu bar comes last among the frame bands, so an open dropdown covers
	// the rows under it. The band is the label row and one padding row of the menu
	// background; the bar paints its labels and dropdowns over it. The overlays
	// are drawn over the menu. A hidden bar paints nothing, so row 0 stays with
	// the tab strip or the body.
	if l.MenuOn {
		band := style.TaggedCell(m.styles.Menu, "ui", "menu_fg", "ui", "menu_bg")
		for y := l.MenuTop; y <= l.MenuBottom; y++ {
			g.Fill(0, g.Width(), y, band)
		}
		m.menu = m.menu.SetRow(l.MenuLabelY).WithPluginRows(m.plugMenuRows())
		m.menu.Paint(g)
	}
	m.paintPluginPanels(g, l)
	for _, o := range m.overlays {
		o.Draw(g)
	}
}

// pane is the editor pane for doc that fills the focused pane's place. Its highlights
// are the find bar's matches. While the bar is open it takes the input and the caret,
// so the paint is the only caller that searches the matches, once per frame.
func (m *Model) pane(doc Document) editorpane.Pane {
	return m.paneFor(splitPane{Focused: true, Top: doc.Top()}, doc)
}

// paintEditor paints the editor panes into the body, to the right of the explorer.
// Each pane draws its visible lines, the gutter, the text, the selection, the change
// marks and the scrollbar, and the lines between the panes are drawn in their gaps.
// With no tab open, the welcome screen is painted instead.
func (m *Model) paintEditor(g *grid.Grid, l layout) {
	if m.ed.Active() == nil {
		paintWelcome(g, m.styles, l, m.welcomeSel)
		return
	}
	m.paintPanes(g, l)
}

// caret is the terminal cursor position in frame cells. ok is false when the
// editor does not have the focus, an overlay is open, or the caret is outside
// the body.
func (m *Model) caret() (x, y int, ok bool) {
	// The overlays come before the explorer check, so a bar or dialog opened from the
	// explorer keeps its caret. The find bar, a dialog's text field and a prompt report
	// their caret through overlay.Caret.
	if n := len(m.overlays); n > 0 {
		if c, ok := m.overlays[n-1].(Caret); ok {
			return c.Cursor(m.width, m.height)
		}
		return 0, 0, false
	}
	if m.explorerFocus || m.contentsFocus {
		return 0, 0, false
	}
	// A mounted widget panel with the keyboard owns it: the editor caret under the panel
	// must not show.
	if m.plug.panels.focus != 0 {
		return 0, 0, false
	}
	if m.dockKeyboard() {
		return m.dock.Caret(m.layout())
	}
	doc := m.ed.Active()
	if doc == nil || m.tabFocus {
		return 0, 0, false
	}
	l := m.layout()
	if l.Gated {
		return 0, 0, false
	}
	fp := l.FocusedPane()
	cx, cy, ok := m.paneFor(fp, doc).Caret(fp.W, fp.H)
	if !ok {
		return 0, 0, false
	}
	return fp.X + cx, fp.Y + cy, true
}

// overlayInput reports whether msg is input that the top overlay takes: a key
// or a mouse message, while an overlay is open.
func (m *Model) overlayInput(msg tea.Msg) bool {
	if len(m.overlays) == 0 {
		return false
	}
	if mo, isMouse := mouseOf(msg); isMouse {
		if cv, ok := m.overlays[len(m.overlays)-1].(interface{ Covers(x, y, w, h int) bool }); ok && !cv.Covers(mo.X, mo.Y, m.width, m.height) {
			// A press or a wheel notch off the overlay's card is not the overlay's: it
			// reaches the frame, as Fresh's prompt layer lets it through.
			return false
		}
		if m.topFindBar() != nil {
			// The find bar takes the mouse only over its own rows.
			return m.findMouse(msg)
		}
	}
	switch msg.(type) {
	case tea.KeyPressMsg, tea.MouseClickMsg, tea.MouseMotionMsg, tea.MouseReleaseMsg, tea.MouseWheelMsg:
		return true
	}
	return false
}

// pushOverlay puts o on top of the stack, so it gets the input next. An overlay
// that is already inactive is not pushed.
func (m *Model) pushOverlay(o Overlay) {
	if o != nil && o.Active() {
		m.overlays = append(m.overlays, o)
	}
}

// routeOverlay gives msg to the top overlay and applies the result: the
// overlay it returns takes its place, a nil return pops it, and an overlay that
// is no longer active is popped. Esc pops the top overlay and is not forwarded,
// unless the overlay asks for it (see takesEscape).
func (m *Model) routeOverlay(msg tea.Msg) tea.Cmd {
	if k, ok := msg.(tea.KeyPressMsg); ok {
		// The overlay contexts consult the keymap before the auto-cancel and the built-in
		// handler; an unbound key falls through to both.
		if cmd, handled := m.overlayKey(tea.Key(k)); handled {
			return cmd
		}
	}
	if k, ok := msg.(tea.KeyPressMsg); ok && m.overlayCancels(tea.Key(k)) {
		if d, ok := m.overlays[len(m.overlays)-1].(interface{ Dismiss() }); ok {
			d.Dismiss()
		}
		m.popOverlay()
		return m.plugDismissed()
	}
	top := m.overlays[len(m.overlays)-1]
	next, cmd := top.Update(msg)
	if next == nil {
		m.popOverlay()
	} else {
		m.overlays[len(m.overlays)-1] = next
	}
	for len(m.overlays) > 0 && !m.overlays[len(m.overlays)-1].Active() {
		m.popOverlay()
	}
	return cmd
}

// popOverlay removes the top overlay. It does nothing on an empty stack.
func (m *Model) popOverlay() {
	if n := len(m.overlays); n > 0 {
		m.overlays[n-1] = nil
		m.overlays = m.overlays[:n-1]
	}
}

// updateStatusRestartHover keeps the {terminal_restart} hover in step with the
// pointer. The hover hit area is the element's own status run, the same one the
// click handler matches, so hover and click cannot drift apart.
func (m *Model) updateStatusRestartHover(mo tea.Mouse) {
	hover := false
	l := m.layout()
	if l.StatusOn && mo.Y == l.StatusY {
		for i := range l.Status {
			r := &l.Status[i]
			if r.Kind != hitStatusSegment || r.Index != frame.StatusSegmentTerminalRestart {
				continue
			}
			if mo.X >= r.X && mo.X < r.X+max(ansi.StringWidth(r.Text), 1) {
				hover = true
			}
			break
		}
	}
	m.statusRestartHover = hover
}

// paintStatus fills the status row and writes its segments. A hidden status row
// paints nothing.
func (m *Model) paintStatus(g *grid.Grid, l layout) {
	if !l.StatusOn {
		return
	}
	bar := style.TaggedCell(m.styles.Status, "ui", "status_bar_fg", "ui", "status_bar_bg")
	palette := style.TaggedCell(m.styles.Accent, "ui", "accent_fg", "ui", "accent_bg")
	// A warning run, such as a Restricted or Blocked workspace-trust pill, keeps the
	// bar's fill and takes the theme's warning foreground, as Fresh paints the trust
	// pill with its WARNING role.
	warning := bar
	if fg := style.Cell(m.styles.DiagWarning).FG; fg != nil {
		warning.FG = fg
		warning.FGKey = style.KeyIDFor("diagnostic", "warning_fg")
	}
	warning.Bold = true
	// A muted run, such as the pending {chord} sequence, keeps the bar's colours
	// and turns faint: a secondary value beside the rest of the row.
	muted := bar
	muted.Faint = true
	// Fresh paints {terminal_restart} with the ERROR role; gvim uses the theme's
	// error foreground over the bar, and the hovered element takes an underline
	// (Fresh's Hover::Swap equivalent on a bar with no background to swap).
	restart := bar
	if fg := style.Cell(m.styles.DiagError).FG; fg != nil {
		restart.FG, restart.Bold = fg, true
	} else if fg := style.Cell(m.styles.DiagWarning).FG; fg != nil {
		restart.FG, restart.Bold = fg, true
	}
	g.Fill(0, g.Width(), l.StatusY, bar)
	for _, r := range l.Status {
		st := bar
		switch {
		case r.Style == frame.StyleWarning:
			st = warning
		case r.Style == frame.StyleMuted:
			st = muted
		case r.Kind == hitStatusSegment && r.Index == frame.StatusSegmentTerminalRestart:
			st = restart
			if m.statusRestartHover {
				st.Underline = true
				st.Bold = true
			}
		case r.Kind == hitStatusPalette:
			st = palette
		}
		g.Put(r.X, l.StatusY, g.Width(), r.Text, st)
	}
	for i, text := range l.Notice {
		y := l.NoticeY + i
		g.Fill(l.NoticeX, g.Width(), y, bar)
		g.Put(l.NoticeX+space.PadX, y, g.Width(), text, bar)
	}
}

// tabEditor is the tab and quit surface of the editor. The app adapter
// implements it. The root asserts it on EditorState, as it does fileEditor.
// Without it the tab and quit actions have nothing to act on.
type tabEditor interface {
	ActivateTab(i int)
	CloseTab(i int, force bool) error
	CloseNeedsConfirm(i int) (bool, error)
	Quit(force bool) []editor.Dirty
}

// pathEditor is the optional path of a Document. The Save As prompt is
// prefilled with it when the document has one.
type pathEditor interface {
	FilePath() string
}

// tabPort returns the tab surface of the editor, or nil.
func (m *Model) tabPort() tabEditor {
	t, _ := m.ed.(tabEditor)
	return t
}

// hasDocument reports whether a tab is open.
func (m *Model) hasDocument() bool {
	return m.ed.Active() != nil
}

// activeTab is the index of the active tab, or -1.
func (m *Model) activeTab() int {
	for i, t := range m.ed.Tabs() {
		if t.Active {
			return i
		}
	}
	return -1
}

// scrollTabs moves the tab strip by delta tabs: negative scrolls left.
func (m *Model) scrollTabs(delta int) {
	m.setTabFirst(m.layout().Strip.First + delta)
}

// setTabFirst sets the scroll position of the tab strip. The stored value is the
// clamped one, so a scroll back starts from the end the strip shows.
func (m *Model) setTabFirst(first int) {
	m.tabFirst = first
	m.tabFirst = m.layout().Strip.First
}

// revealActiveTab scrolls the tab strip only as far as needed to show the active
// tab in full, or from its start when it is wider than the strip.
func (m *Model) revealActiveTab() {
	i := m.activeTab()
	if i < 0 {
		return
	}
	l := m.layout()
	m.setTabFirst(frame.RevealedFirst(m.stripTabs(), l.EditorX, l.W, l.Strip.First, i))
}

// next says what a file action does once a save or a discard is done. The
// unsaved-changes flow of a close, and of a quit, use it.
type next int

const (
	nextNone  next = iota // nothing more: a Save As from Ctrl+S
	nextClose             // close the tab at index
	nextQuit              // run the quit loop again
)

// flow is the state of an unsaved-changes flow: what follows it, and the tab it
// is about. The tags of the dialogs carry it back to the root.
type flow struct {
	then  next
	index int
}

// The tags the file dialogs carry back. Each one holds the flow it belongs to.
type (
	unsavedTag struct{ f flow }
	saveAsTag  struct{ f flow }
)

// requestClose closes the tab at index. A clean tab closes at once. A modified
// tab is activated and the unsaved-changes dialog asks about it first.
func (m *Model) requestClose(index int) tea.Cmd {
	t := m.tabPort()
	if t == nil {
		m.notice = "Closing tabs is not supported in this port"
		return nil
	}
	needs, err := t.CloseNeedsConfirm(index)
	if err != nil {
		m.notice = err.Error()
		return nil
	}
	if !needs {
		if err := m.closeTab(t, index, false); err != nil {
			m.notice = err.Error()
		}
		m.syncViewport()
		return nil
	}
	t.ActivateTab(index)
	m.syncViewport()
	m.askUnsaved(flow{then: nextClose, index: index}, m.ed.Tabs()[index].Name)
	return nil
}

// quitLoop quits when no tab has unsaved edits. Otherwise it activates the
// first dirty tab and asks about it. The dialog's answer comes back through
// dialogDone, which runs the loop again. Cancel stops it.
func (m *Model) quitLoop() tea.Cmd {
	t := m.tabPort()
	if t == nil {
		return m.quit()
	}
	dirty := t.Quit(false)
	if len(dirty) == 0 {
		return m.quit()
	}
	// A tab stays modified until the results of its write are applied, so the question
	// would be stale by the time the write ends. Wait for the write instead, and ask
	// then if the tab is still modified. A second quit asks at once, for a write that
	// does not end.
	if len(m.large.saves) > 0 && !m.large.quitAfterSave {
		m.large.quitAfterSave = true
		m.notice = "Quit when the save has finished"
		return nil
	}
	d := dirty[0]
	t.ActivateTab(d.Index)
	m.syncViewport()
	m.askUnsaved(flow{then: nextQuit, index: d.Index}, d.Name)
	return nil
}

// askUnsaved opens the unsaved-changes dialog for the tab called name.
func (m *Model) askUnsaved(f flow, name string) {
	if m.dialogs == nil {
		m.notice = "Unsaved changes in " + name
		return
	}
	m.pushOverlay(m.dialogs.Unsaved(name, unsavedTag{f: f}, m.styles.Overlay))
}

// askSaveAs opens the Save As prompt. It is prefilled with the path of the
// active document, or with a .txt name under the explorer root for an
// untitled one.
func (m *Model) askSaveAs(f flow) {
	if m.dialogs == nil {
		m.notice = "Save As is not supported in this port"
		return
	}
	doc := m.ed.Active()
	if doc == nil {
		return
	}
	prefill := filepath.Join(m.rootDir(), doc.Name()+".txt")
	if p, ok := doc.(pathEditor); ok && p.FilePath() != "" {
		prefill = p.FilePath()
	}
	m.pushOverlay(m.dialogs.SaveAs(prefill, saveAsTag{f: f}, m.styles.Overlay))
}

// dialogDone applies the answer of a file dialog. Each tag names the flow the
// dialog belongs to, and the choice decides the next step. Save As sends Save
// for a new name and Replace once the user confirms an existing one.
func (m *Model) dialogDone(d dialog.Result) tea.Cmd {
	switch tag := d.Tag.(type) {
	case unsavedTag:
		return m.unsavedAnswer(tag.f, d.Choice)
	case saveAsTag:
		switch d.Choice {
		case dialog.ChoiceSave:
			return m.saveAs(d.Text, false, tag.f)
		case dialog.ChoiceReplace:
			return m.saveAs(d.Text, true, tag.f)
		}
	}
	return nil
}

// unsavedAnswer applies the choice of the unsaved-changes dialog for flow f.
// Save writes the tab, through Save As when it is untitled. Discard closes it
// without saving. Either way the flow goes on, and Cancel ends it.
func (m *Model) unsavedAnswer(f flow, c dialog.Choice) tea.Cmd {
	t := m.tabPort()
	if t == nil {
		return nil
	}
	switch c {
	case dialog.ChoiceSave:
		if !hasPath(m.ed.Active()) {
			m.askSaveAs(f)
			return nil
		}
		return m.saveThen(f)
	case dialog.ChoiceDiscard:
		if err := m.closeTab(t, f.index, true); err != nil {
			m.notice = err.Error()
			return nil
		}
		m.syncViewport()
		if f.then == nextQuit {
			return m.quitLoop()
		}
	}
	return nil
}

// afterSave runs the step that follows a saved tab: the tab closes, or the quit
// loop goes on.
func (m *Model) afterSave(f flow) tea.Cmd {
	switch f.then {
	case nextClose:
		if t := m.tabPort(); t != nil {
			if err := m.closeTab(t, f.index, false); err != nil {
				m.notice = err.Error()
			}
			m.syncViewport()
		}
	case nextQuit:
		return m.quitLoop()
	}
	return nil
}

// expandRoot opens the explorer root, so the tree shows its entries at startup.
// A tree without an Expand method is left as it is.
func (m *Model) expandRoot() {
	if e, ok := m.tree.(interface{ Expand(path string) error }); ok {
		_ = e.Expand(m.tree.Root())
	}
}

// keyNames maps the bubbletea key codes that have a fixed keymap name.
var keyNames = map[rune]string{
	tea.KeyEnter:     "Enter",
	tea.KeyBackspace: "Backspace",
	tea.KeyDelete:    "Delete",
	tea.KeyEscape:    "Escape",
	tea.KeyUp:        "Up",
	tea.KeyDown:      "Down",
	tea.KeyLeft:      "Left",
	tea.KeyRight:     "Right",
	tea.KeyHome:      "Home",
	tea.KeyEnd:       "End",
	tea.KeyPgUp:      "PageUp",
	tea.KeyPgDown:    "PageDown",
}

// translateKey converts a bubbletea key press into the keymap vocabulary. It
// also returns the text the press types, which is empty when the press is not
// text entry. Keymap names follow Fresh's default.json.
func translateKey(k tea.Key) (KeyPress, string) {
	var kp KeyPress
	if k.Mod&tea.ModCtrl != 0 {
		kp.Mods = append(kp.Mods, "ctrl")
	}
	if k.Mod&tea.ModAlt != 0 {
		kp.Mods = append(kp.Mods, "alt")
	}
	shift := k.Mod&tea.ModShift != 0
	// Super is the Command key. A terminal that does not use the kitty keyboard
	// protocol sends it as xterm's modifier 9, which the decoder reads as Meta, so
	// Meta stands for it here: the keymap has no meta modifier.
	super := k.Mod&(tea.ModSuper|tea.ModMeta) != 0

	named := true
	switch {
	case k.Code == tea.KeyTab:
		// Shift+Tab is the BackTab key; the keymap binds it with and without shift.
		kp.Name = "Tab"
		if shift {
			kp.Name = "BackTab"
		}
	case k.Code >= tea.KeyF1 && k.Code <= tea.KeyF12:
		kp.Name = "F" + strconv.Itoa(int(k.Code-tea.KeyF1)+1)
	case keyNames[k.Code] != "":
		kp.Name = keyNames[k.Code]
	case k.Code == tea.KeySpace:
		kp.Name = " "
	default:
		named = false
		if k.Code != 0 {
			kp.Name = lowerIfLetter(k.Code)
		}
	}
	// A legacy terminal folds Shift into the character (and Caps Lock produces an
	// uppercase letter without a Shift modifier). Fresh's normalize_key infers Shift for
	// such a letter unless CONTROL is set, so "Shift+P" matches how both kitty and
	// legacy terminals report it; CapsLock+Ctrl+A keeps firing Ctrl+A. The lock bits
	// themselves are keyboard state, not chord modifiers, and are ignored as before.
	if !named && !shift && unicode.IsUpper(k.Code) && k.Mod&tea.ModCtrl == 0 {
		shift = true
	}
	// Shift stays on named keys and shifted letters. A shifted symbol is bound by its own character.
	if shift && (named || unicode.IsLetter(k.Code)) {
		kp.Mods = append(kp.Mods, "shift")
	}
	if super {
		kp.Mods = append(kp.Mods, "super")
	}
	var text string
	switch {
	case k.Text == "":
	case k.Mod&(tea.ModCtrl|tea.ModAlt|tea.ModSuper|tea.ModMeta) == 0:
		text = k.Text
	case isAltGr(k.Mod):
		// Windows reports AltGr as Ctrl+Alt; a single printable rune is text input
		// (Fresh's text-input-modifier check), so international layouts can type
		// @ [ ] { }. A keymap binding for the chord still wins before typing.
		if _, ok := singleRune(k.Text); ok {
			text = k.Text
		}
	}
	return kp, text
}

// singleRune returns the rune that text types, and false when text is empty, has
// more than one rune, or is not printable.
func singleRune(text string) (rune, bool) {
	rs := []rune(text)
	if len(rs) != 1 || !unicode.IsPrint(rs[0]) {
		return 0, false
	}
	return rs[0], true
}

// lowerIfLetter returns the lower-case letter for r, or r itself as a string.
func lowerIfLetter(r rune) string {
	if unicode.IsLetter(r) {
		return string(unicode.ToLower(r))
	}
	return string(r)
}

// clickCounter counts presses in one cell. The count runs 1, 2, 3, then 1, and
// a press restarts it when it is outside the cell or the window.
type clickCounter struct {
	at   time.Time
	x, y int
	n    int
}

// press records a left press at (x, y) at time at and returns its count.
func (c *clickCounter) press(at time.Time, x, y int) int {
	if c.n > 0 && at.Sub(c.at) <= doubleClickWindow && c.x == x && c.y == y {
		c.n = c.n%3 + 1
	} else {
		c.n = 1
	}
	c.at, c.x, c.y = at, x, y
	return c.n
}

// isAltGr reports Windows AltGr, which its console reports as Ctrl+Alt. It is a
// text-input modifier, not a chord: the caller passes only a printable single
// rune on to the text path. Super/Meta (Command) cannot be part of AltGr.
func isAltGr(mod tea.KeyMod) bool {
	return mod&(tea.ModCtrl|tea.ModAlt) == tea.ModCtrl|tea.ModAlt && mod&(tea.ModSuper|tea.ModMeta) == 0
}
