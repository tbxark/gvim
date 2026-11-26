package tui

import (
	"context"
	"errors"
	"github.com/tbxark/gvim/document"
	"io"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
	"unicode"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

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
	"github.com/tbxark/gvim/internal/tui/split"
	"github.com/tbxark/gvim/internal/tui/style"
	"github.com/tbxark/gvim/internal/tui/tabstrip"
	"github.com/tbxark/gvim/syntax"
	"github.com/tbxark/gvim/theme"
)

// wheelRows is the number of rows one wheel notch scrolls (blueprint section 3.2).
const wheelRows = 3

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
	// statusName caches the status row's file name, so the two symlink resolutions
	// behind it do not run on every frame.
	statusName statusNameCache
	// statusRows caches the parsed editor.status_bar element lists.
	statusRows statusRowCache
	// rulerMarks caches the overview ruler's diagnostic marks per document path, and
	// themeGen counts the style rebuilds so the cache sees a theme change.
	rulerMarks map[string]*rulerMarkCache
	// hoverLayout is the geometry cache of a hover-only mouse-motion update.
	hoverLayout hoverLayoutCache
	// frameLayout is the geometry View computed for the frame it is painting; layout
	// returns it until View ends.
	frameLayout *frame.Layout
	// paintGrid is reused by View across frames to avoid allocating a full cell slice
	// on every redraw.
	paintGrid          grid.Grid
	motionSkipView     bool
	motionCachedView   string
	motionCachedW      int
	motionCachedH      int
	motionCachedCaretX int
	motionCachedCaretY int
	motionCachedCaret  bool
	// findMatches caches the find bar's highlight search (see cachedFindMatches).
	findMatches findMatchCache
	themeGen    uint64
	theme       theme.Theme
	// themeRef is the portable config value of the active theme, and
	// themeOverrides are the session color overrides layered over it.
	themeRef       string
	themeOverrides map[string]string

	// The palette's sources. The commands always have a table, and files and
	// ranker are nil when Deps leaves them out.
	commands []commands.Entry
	// commandHistory is the palette's usage history, most recent action first (max 50).
	commandHistory []string
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
	// The chrome's pointer feedback: the tab-strip run under the pointer, the
	// clickable status run under it, and whether the pointer is on the explorer's
	// resize grip. Each hit area is the one the click handlers read, so feedback
	// and action cannot drift apart.
	statusHover   frame.HitTarget
	stripHover    frame.HitTarget
	gripHover     bool
	borderHover   int
	hover         mouseHoverState
	dockRows      int
	explorerOpen  bool // the panel is shown
	explorerFocus bool // the panel has keyboard focus
	tabFocus      bool // the tab strip has keyboard focus (F7 chrome cycle)
	// The Contents sidebar section (see markdown_toc.go): contents is its tree
	// and settings, contentsFocus says it holds the keyboard, and
	// explorerRows is the explorer section's explicit body height (1..max); 0 means
	// the explorer is the flex remainder, as Fresh's last open section is.
	explorerRows int
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
	stickyScroll    bool
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
	// promptClipToken is the generation of the latest prompt copy/cut with text. A
	// clipboard result carrying another token is stale and must not touch the prompt or
	// the notice; every new prompt copy/cut bumps it. promptClipPending is the
	// single-flight gate: while a native write is in flight no other copy/cut may start,
	// so native writes cannot run concurrently or complete out of order. CancelTransientWork
	// invalidates the token but leaves the gate set until the in-flight command reports.
	promptClipToken        uint64
	promptClipPending      bool
	promptClipPendingToken uint64
	promptClipOp           *promptClipOp

	// rowTop is the view's offset into a wrapped top line, see viewrow.go.
	rowTop rowTop

	menu          menubar.Model // the menu bar and its open dropdown
	explorerPanel expanel.Panel // paints the explorer and answers its key actions
	ops           explorerOps   // file operations; nil when Deps.Ops lacks them
	clip          *clipboard    // the explorer clipboard; nil when empty
	lastYank      string        // the last text copied or cut, for the terminal's Ctrl+V fallback
	quitting      bool          // set by a menu row that quit the editor
	closed        atomic.Bool   // Close ran: late host command results are discarded
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

	clicks    space.ClickCounter
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
	// fileLaunch is the immutable invocation property plugins read through
	// gvim.workspace.file_launch(): true when this editor was started to open
	// command-line files (or --simple/--diff), false for a session or standalone stdin.
	fileLaunch bool
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
	// hostFocused is the outer terminal window's focus (Bubble Tea FocusMsg/BlurMsg,
	// reported because View enables ReportFocus). Terminal children get focus-in only
	// while both the window and the dock have the keyboard.
	hostFocused bool
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
	// img is the host terminal's image-graphics state and the transmissions made
	// (see imagecap.go and imageview.go).
	img imageState
	// hexViews holds the read-only hex state of binary tabs, keyed by file path
	// (see hexview.go). A path with no entry is showing its summary.
	hexViews map[string]*hexView
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
	// authorityPreviewConnected is the last observed carrier connectivity; a
	// connected→disconnected transition drops cached remote previews once.
	authorityPreviewConnected bool
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
	// workspaceOwner identifies this model generation for palette async/open messages.
	// The app sets it per built daemon workspace; the zero value is a direct launch.
	workspaceOwner palette.Owner
	// review is the built-in Review Diff Mode state (see review_mode.go).
	review reviewModeState
}

// drag is the pointer gesture in progress: a text selection, or a grab of the
// scrollbar thumb, or a split line (split_drag.go). The zero value is no gesture.
type drag struct {
	active          bool
	thumb           bool          // the scrollbar thumb: motion moves the viewport
	hbar            bool          // the horizontal scrollbar: motion pans the view sideways
	gutterLine      bool          // a gutter press/drag extending by whole lines once dragged
	gutterStartLine int           // anchor line of a gutter line drag
	tab             *tabDrag      // a tab of the strip: motion moves it along the strip
	unit            editor.Unit   // the unit a text drag grows the selection by
	border          *split.Border // the split line the press took: motion moves its split's ratio
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
	m := &Model{
		ed:              deps.Editor,
		tree:            deps.Explorer,
		keys:            deps.Keys,
		styles:          st,
		panes:           editorpane.NewStyles(deps.Theme),
		now:             now,
		borderHover:     -1,
		theme:           deps.Theme,
		themeRef:        deps.ThemeRef,
		themeOverrides:  map[string]string{},
		commands:        cmds,
		recents:         deps.Recents,
		fileLaunch:      deps.FileLaunch,
		hideMenuBar:     deps.HideMenuBar,
		hideLineNumbers: deps.HideLineNumbers,
		stickyScroll:    true,
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
		hostFocused:     true,
		img:             imageProtocolOf(viewOptionsOf(deps.View).ImageProtocol),
	}
	m.view.languages = deps.Languages
	// The inlay-hints toggle starts from the configured display option; the Toggle
	// Inlay Hints command flips it for the rest of the session.
	m.lsp.inlay = m.view.opts.EnableInlayHints
	m.dock = dockNewPanel(m)
	m.dock.SetHostFocused(m.hostFocused)
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
		m.explorerPanel = expanel.New(deps.Explorer, st, now).WithOptions(m.view.opts.Explorer)
	}
	m.initContents(deps)
	m.initReviewMode(deps.StartReview)
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

// ignoreZeroWindowSize drops non-positive WindowSizeMsg before Bubble Tea's renderer
// or WorkspaceHost sees it. A daemon's output is not a TTY, so Bubble Tea's startup
// goroutine sends an initial WindowSizeMsg{0, 0} that can arrive after an attached
// client's size and wipe both the renderer and the active model back to 0x0.
func ignoreZeroWindowSize(_ tea.Model, msg tea.Msg) tea.Msg {
	if size, ok := msg.(tea.WindowSizeMsg); ok && (size.Width <= 0 || size.Height <= 0) {
		return nil
	}
	return msg
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
	}, tea.WithInput(in), tea.WithOutput(out), tea.WithFilter(ignoreZeroWindowSize))
	// The program is over: Do would block on a dead message queue.
	h.stop()
	return err
}

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
	m.closed.Store(true)
	m.cancelPromptClipOp()
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

// Closed reports whether Close ran. The daemon host uses it to discard command results
// that arrive after the model's runtime was evicted or closed.
func (m *Model) Closed() bool { return m.closed.Load() }

// MarkClosed stops the daemon host from delivering command results to this model. The
// app calls it ON THE UI GOROUTINE (through WorkspaceHost.Do) before tearing down a warm
// model: once it returns, no source.Update is in flight or can start, so the heavier
// runtime teardown may run off the UI goroutine without racing model fields. Idempotent.
func (m *Model) MarkClosed() {
	m.closed.Store(true)
	m.cancelPromptClipOp()
}

// Init implements tea.Model. It starts the root features' commands: the git status,
// the auto-save tick and the file watch.
func (m *Model) Init() tea.Cmd {
	return tea.Batch(m.rootInit(), m.indexSync(), m.pluginInit(), m.clockCmd(), m.authorityInitCmd(), m.startupOpenCmd(), m.updateCheckCmd(), m.reviewInitCmd())
}

// Update implements tea.Model. It runs update, then keeps the split panes on tabs
// that still exist, then tells the editor its new size when the body changed height
// (the status row or the find bar came or went), then scrolls the tab strip to the
// active tab when the active tab or the number of tabs changed, and when the frame
// size changes (the first size shows the active tab too). Scrolling by the wheel or a
// marker changes neither, so it is not undone here. The viewport is not re-synced on
// every message, because the editor moves it to the caret.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	m.motionSkipView = false
	m.hover.pendingTick = nil
	if _, isMotion := msg.(tea.MouseMotionMsg); !isMotion {
		m.motionCachedView = ""
	}
	if owned, ok := msg.(palette.OwnedMsg); ok {
		owner := owned.PaletteOwner()
		// A palette always stamps its workspace/model generation. A zero owner is a
		// direct-launch or a test/legacy caller constructing an action by hand (it cannot
		// be a stale palette result: those are never zero in a daemon model). Drop any
		// nonzero owner that is not this model's before an overlay or action sees it: its
		// seq and terminal tab ids are model-local and can collide with ours.
		if owner != (palette.Owner{}) && owner != m.workspaceOwner {
			return m, nil
		}
	}
	if _, ok := msg.(clockTickMsg); ok {
		// A {clock} tick only asks for the next frame (the clock is read while the
		// status row is built) and arms the tick for the minute after it.
		return m, m.clockCmd()
	}
	if _, ok := msg.(tea.FocusMsg); ok {
		// The outer terminal window gained the focus: merge it into the dock's focus
		// reporting (a child that asked for DECSET 1004 events is focused again when
		// the dock itself has the keyboard) and keep the remote reconnect-on-activate
		// point. The plugin event is only raised on a real state change: repeated
		// FocusMsg reports must not replay window_focused for every plugin.
		changed := !m.hostFocused
		m.hostFocused = true
		m.dock.SetHostFocused(true)
		cmd := m.beginAuthorityReconnect()
		if changed {
			cmd = tea.Batch(cmd, m.emitHostFocusEvent("window_focused"), m.scheduleReviewAutoRefresh())
		}
		return m, cmd
	}
	if _, ok := msg.(tea.BlurMsg); ok {
		// The outer window lost the focus: no terminal child may think it is focused.
		// A blur is not a reconnect point (the connection is unrelated to the OS
		// window). Like focus, the plugin event is only for a real transition.
		changed := m.hostFocused
		m.hostFocused = false
		m.dock.SetHostFocused(false)
		if !changed {
			return m, nil
		}
		return m, m.emitHostFocusEvent("window_blurred")
	}
	if cmd, ok := m.imageMessage(msg); ok {
		return m, cmd
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
			// A failed background/focus reconnect is an attempt boundary too: clear any
			// preview that was cached before the poll observed the disconnect.
			m.noteAuthorityReconnectAttempt()
			m.reportAuthorityReconnect(done.err)
			// Keep the backoff from the poll: it schedules the next attempt.
			return m, nil
		}
		m.resetAuthorityRetry()
		// A reconnect means the remote carrier changed: drop every cached preview so no
		// pre-disconnect read is painted as current (also covers a reconnect that the
		// poll did not observe as a disconnect transition).
		m.invalidateRemotePreviews()
		m.authorityPreviewConnected = true
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
		if m.review.active {
			cmds = append(cmds, m.reviewRefreshCmd())
		}
		if explorer := m.remoteExplorerReloadCmd(); explorer != nil {
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
	moMsg, isMotion := msg.(tea.MouseMotionMsg)
	var prevStrip, prevStatus frame.HitTarget
	var prevGrip, prevDockHover, prevExDrag bool
	prevBorderHover := -1
	if isMotion {
		prevStrip, prevStatus, prevGrip, prevBorderHover, prevDockHover = m.stripHover, m.statusHover, m.gripHover, m.borderHover, m.dock.Hovered()
		prevExDrag = m.explorerPanel.Dragging()
	}
	if isMotion && m.pointerOnly(moMsg) {
		m.hoverLayout = hoverLayoutCache{on: true}
		defer func() { m.hoverLayout = hoverLayoutCache{} }()
	}
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
	m.plugGroupsReconcile()
	cmd = tea.Batch(cmd, m.pluginTail(snap), m.pluginIdle(msg))
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
	// After the root's own focus moves of this update (F7, commands, auto-focus) report
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
	// An image tab that was just opened needs its picture read once. This runs after
	// the update, so the read never blocks an update; a command does the file I/O.
	cmd = tea.Batch(cmd, m.imageReleaseClosed())
	cmd = tea.Batch(cmd, m.imageQueue())
	m.hexReleaseClosed()
	cmd = tea.Batch(cmd, m.hexQueue())
	if isMotion && m.canSkipMotionView(moMsg, prevStrip, prevStatus, prevGrip, prevBorderHover, prevDockHover, prevExDrag, before, beforeStatus, cmd) {
		m.motionSkipView = true
	} else {
		m.motionCachedView = ""
	}
	if m.hover.pendingTick != nil {
		cmd = tea.Batch(cmd, m.hover.pendingTick)
		m.hover.pendingTick = nil
	}
	return next, cmd
}

func (m *Model) canSkipMotionView(mo tea.MouseMotionMsg, prevStrip, prevStatus frame.HitTarget, prevGrip bool, prevBorderHover int, prevDockHover, prevExDrag bool, prevNotice, prevStatusText string, cmd tea.Cmd) bool {
	if m.review.active {
		return false
	}
	if cmd != nil || tea.Mouse(mo).Button != tea.MouseNone || m.drag.active || m.divider.active || m.drag.tab != nil ||
		prevExDrag || m.explorerPanel.Dragging() || m.dock.MouseDragging() || m.dock.Hovered() != prevDockHover ||
		m.notice != prevNotice || m.ed.Status() != prevStatusText {
		return false
	}
	if len(m.overlays) > 0 || m.menu.IsOpen() || m.welcomeVisible() {
		return false
	}
	if len(m.plug.panels.order) > 0 || m.stripHover != prevStrip || m.statusHover != prevStatus || m.gripHover != prevGrip || m.borderHover != prevBorderHover {
		return false
	}
	if m.motionCachedView == "" || m.motionCachedW != m.width || m.motionCachedH != m.height {
		return false
	}
	cx, cy, cok := m.caret()
	if cok != m.motionCachedCaret || (cok && (cx != m.motionCachedCaretX || cy != m.motionCachedCaretY)) {
		return false
	}
	return m.layout().Hit(mo.X, mo.Y).Kind == frame.HitEditorText
}

// emitHostFocusEvent queues a host-window focus event for the plugins that subscribe
// to it and schedules the plugin pump on the next event-loop turn. The focus message
// itself never calls the runtime: the dock state is updated and Update returns, then
// pluginDrainMsg flushes the event through the normal pump budget. Without a runtime
// or a subscriber nothing is queued and no pump is scheduled.
func (m *Model) emitHostFocusEvent(name string) tea.Cmd {
	if m.plug.rt == nil || !m.plug.rt.Wants(name) {
		return nil
	}
	m.plugEmit(name, map[string]any{})
	return m.drainTick()
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
	// Fresh's Remote Indicator menu is a keymap action (the default is the global
	// F6), so a user can rebind it. Resolve it before the plugin key layer, a widget
	// panel, the dock or any focused surface so none of them can swallow it; the
	// chrome focus cycle is the separate built-in F7.
	if k, ok := msg.(tea.KeyPressMsg); ok {
		if cmd, handled := m.remoteMenuKey(tea.Key(k)); handled {
			if cmd.Action == "" {
				// An exact-context unbind: the key is consumed, with no fallback.
				return m, nil
			}
			return m, m.dispatch(cmd)
		}
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
	// Settings refresh messages belong to one page, not to whatever overlay is on top:
	// route them to that page even while a child page or prompt is open, so a warm
	// completion is not swallowed and the next tick never renews after the page closes.
	if cmd, ok := m.routeSettingsMessage(msg); ok {
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
	if hexCmd, taken := m.hexMessage(msg); taken {
		return m, tea.Batch(overlayCmd, hexCmd)
	}
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
	if cmd, ok := m.reviewMessage(msg); ok {
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
		// The cells under the pointer moved with the frame; the next motion names
		// the new target.
		m.stripHover, m.statusHover, m.gripHover, m.borderHover = frame.HitTarget{}, frame.HitTarget{}, false, -1
		m.cancelMouseHover()
		m.syncMenu()
		m.syncViewport()
		m.followSelection()
		m.pluginReady()
	case tea.KeyPressMsg:
		m.cancelMouseHover()
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
		m.cancelMouseHover()
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
		if m.review.active {
			cmd = m.handleReviewClick(msg)
			break
		}
		cmd = m.handleClick(msg)
		m.rebaseFind()
	case tea.MouseMotionMsg:
		if m.review.active {
			if dockCmd, taken := m.dockMouse(msg, msg.X, msg.Y, 1); taken {
				cmd = dockCmd
			} else {
				m.handleReviewMotion(msg.Mouse())
			}
			break
		}
		if m.divider.active {
			// An in-flight Contents divider drag owns the pointer: a floating plugin
			// panel under the pointer must not swallow the motion (leaving divider.active
			// set would resize the section on a later unrelated move).
			cmd = m.handleDrag(msg.Mouse())
		} else if panelCmd, taken := m.pluginPanelMouse(msg); taken {
			cmd = panelCmd
		} else if dockCmd, taken := m.dockMouse(msg, msg.X, msg.Y, 1); taken {
			cmd = dockCmd
		} else {
			cmd = tea.Batch(m.handleDrag(msg.Mouse()), m.explorerMouse(msg))
		}
		m.updateChromeHover(msg.Mouse())
		m.hover.pendingTick = m.armMouseHover(msg.Mouse())
	case mouseHoverTickMsg:
		cmd = m.triggerMouseHover(msg)
	case dock.ResizeMsg:
		m.dockRows = msg.Rows
	case dragScrollMsg:
		cmd = m.dragScroll(msg)
	case tea.MouseReleaseMsg:
		if m.review.active {
			m.handleReviewRelease()
			if dockCmd, taken := m.dockMouse(msg, msg.X, msg.Y, 1); taken {
				cmd = dockCmd
			}
			break
		}
		if m.divider.active {
			// The divider's release ends the drag first, exactly like the motion guard:
			// no plugin panel, dock or explorer action may consume it.
			m.drag = drag{}
			m.releaseDivider()
			break
		}
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
		if dockCmd, taken := m.dockMouse(msg, msg.X, msg.Y, 1); taken {
			cmd = dockCmd
		} else {
			cmd = m.explorerMouse(msg)
		}
	case tea.MouseWheelMsg:
		// Scrolling is navigation, so a message ends with it as it does with a key or a click.
		m.expireStatus()
		if m.review.active {
			cmd = m.handleReviewWheel(msg)
			break
		}
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
	case expanel.MoveMsg:
		cmd = m.askMoveTo(msg.Dir, msg.Paths)
	case prompt.Done:
		cmd = m.answer(msg)
	case mixedExtractSelectionMsg:
		cmd = m.afterMixedExtractSelection(msg.selection)
	case openFileAtMsg:
		cmd = m.openFileCmd(msg.path, false, msg.line, msg.col)
		m.focusEditor()
	case workspaceOpErrMsg:
		m.quietNotice(msg.err.Error())
	case ExtractConflictMsg:
		// The extraction command detected target conflicts BEFORE writing anything.
		// Ask here; the answer retries with replace or cancels untouched.
		m.pushOverlay(prompt.NewConfirmWith(m.styles, extractConflictQuestion(msg), extractConflictAnswer{
			selection: msg.Selection, targetID: msg.TargetID,
		}, prompt.ConfirmOptions{Title: uiText("gvim.prompt.title.extract_tabs", "Extract Tabs")}))
	case fileResultMsg:
		cmd = tea.Batch(m.fileDone(msg), m.gitCmd())
	case urlOpenedMsg:
		m.openURLDone(msg)
	case browser.Open:
		if m.splits.pick != nil {
			m.comparePicked(msg.Path)
			break
		}
		if msg.AskEncoding {
			m.askOpenWithEncoding(msg.Path, msg.Line, msg.Col)
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
	case palette.OpenTerminalMsg:
		// A Live Grep hit in an open dock terminal focuses that tab (showing the dock
		// when hidden) and, when possible, reveals the matched transcript row in the
		// read-only pager. It starts no shell and touches no PTY: the terminal is
		// already open, the transcript line is only where the match was found.
		// Resolve the stable runtime id once; RevealTranscriptLine itself activates
		// the tab, so this path must not SetActive separately (it would queue the
		// dive twice).
		if _, ok := m.dock.IndexForID(msg.ID); !ok {
			m.notice = uiText("gvim.palette.terminal_tab_gone", "Live Grep: the terminal tab is no longer open")
			break
		}
		cmd = m.showDock()
		reveal, located := m.dock.RevealTranscriptLine(dock.TranscriptHit{
			ID: msg.ID, Row: msg.Row, Total: msg.Total,
			Text: msg.Text, Before: msg.Before, After: msg.After,
			BeforeKnown: msg.BeforeKnown, AfterKnown: msg.AfterKnown,
			Snapshot: msg.Snapshot, Valid: msg.Valid,
		})
		if located {
			cmd = tea.Batch(cmd, reveal)
		} else {
			// The tab is there and focused, but a restored read-only tab or a
			// running alternate-screen program has no pager to position.
			m.notice = uiText("gvim.palette.terminal_line_unavailable", "Live Grep: the terminal transcript line cannot be located")
		}
		m.focusDock()
	case palette.GotoLineMsg:
		// The ':' mode jumps at once, as Fresh's does: one Enter reaches the line.
		if msg.Relative {
			m.gotoRelativeLine(msg.Delta, msg.Col)
		} else {
			m.gotoLine(msg.Line, msg.Col)
		}
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
// closes, through quitting. A menu row records usage for the command
// palette's recency history before it runs. Internal callers (the palette's own
// runCommand, macro replay) use runAction or dispatch, so they do not record twice or
// count replay as user usage.
func (m *Model) Run(action string) error {
	record := action
	if _, ok := strings.CutPrefix(action, menubar.CopyThemePrefix); ok {
		record = "copy_with_theme"
	}
	m.rememberCommand(record)
	return m.runAction(action)
}

// runAction executes one action without recording palette usage; Run (menu) and
// runCommand (palette) record their own entry points.
func (m *Model) runAction(action string) error {
	args := map[string]any(nil)
	if name, ok := strings.CutPrefix(action, menubar.CopyThemePrefix); ok {
		action, args = "copy_with_theme", map[string]any{"theme": name}
	}
	defer m.plugSource(srcMenu)()
	m.recordCommand(Command{Action: action, Args: args})
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
	res := m.editorDo(document.Action(action), args)
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
	case "has_buffer":
		return doc != nil
	case "has_text_buffer":
		return doc != nil && !isBinaryDoc(doc) && !isImageDoc(doc)
	case "can_edit", "can_paste":
		return doc != nil && !isDocReadOnly(doc) && !isBinaryDoc(doc) && !isImageDoc(doc)
	case "keymap_default", "keymap_emacs", "keymap_vscode", "keymap_macos_gui":
		return m.util.keymapChecked(name)
	case "inlay_hints":
		return m.lsp.inlay
	case "can_save", "can_revert":
		return doc != nil && hasPath(doc)
	case "can_save_all":
		return slices.ContainsFunc(m.ed.Tabs(), func(t TabInfo) bool { return t.Modified })
	case "read_only":
		return doc != nil && isDocReadOnly(doc)
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
	case "review_diff_mode":
		return m.review.active
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
		Query:      query,
		Owner:      m.workspaceOwner,
		Root:       root,
		Remote:     remote,
		ShowHidden: m.tree != nil && m.tree.ShowHidden(),
		Commands:   translateCommands(m.allCommands()),
		State:      &st,
		History:    append([]string(nil), m.commandHistory...),
		Buffers:    m.paletteBuffers(),
		Files:      m.files,
		Ranker:     m.ranker,
		Preview:    preview,
		Icons:      m.view.icons,
		Theme:      m.theme,
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
	isText := !isBinaryDoc(doc) && !isImageDoc(doc)
	st.TextBuffer = isText
	st.Editable = isText && !isDocReadOnly(doc)
	st.HasPath = hasPath(doc)
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
	m.rememberCommand(action)
	defer m.plugSource(srcPalette)()
	if err := m.runAction(action); err != nil {
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

// rememberCommand moves action to the front of the palette's usage history, keeping
// Fresh's cap of 50 distinct commands. Only a palette/menu command run reaches it.
func (m *Model) rememberCommand(action string) {
	if action == "" {
		return
	}
	next := []string{action}
	for _, prior := range m.commandHistory {
		if prior == action || len(next) >= 50 {
			continue
		}
		next = append(next, prior)
	}
	m.commandHistory = next
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

// gotoRelativeLine jumps delta lines from the current 1-based line, clamped to line 1
// at the top (Fresh's resolve_goto_line_target) and to the end by GotoLine. The quick-
// open relative jump lands in the first column, as Fresh's goto_line_col does.
func (m *Model) gotoRelativeLine(delta, col int) {
	doc := m.ed.Active()
	if doc == nil {
		return
	}
	line := max(doc.Cursor().Line+1+delta, 1)
	if col < 1 {
		col = 1
	}
	m.gotoLine(line, col)
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
		m.ed.Do(document.Action("next_buffer"), nil)
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
	if len(kp.Mods) == 0 && kp.Name == "F7" {
		m.cycleFocus()
		return nil
	}
	if m.dockKeyboard() {
		return m.dockKey(k, kp)
	}
	if m.review.active {
		return m.handleReviewKey(k, kp, text)
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
	if _, on := m.activeHex(); on {
		if cmd, handled := m.hexFallbackKey(kp, text); handled {
			return cmd
		}
	}
	if text != "" {
		if doc := m.ed.Active(); doc != nil && isDocReadOnly(doc) && k.Mod&(tea.ModCtrl|tea.ModAlt|tea.ModSuper) == 0 {
			switch text {
			case "q":
				return m.dispatch(Command{Action: "quit"})
			case " ":
				return m.dispatch(Command{Action: "move_page_down"})
			case "b":
				return m.dispatch(Command{Action: "move_page_up"})
			case "j":
				return m.dispatch(Command{Action: "move_down"})
			case "k":
				return m.dispatch(Command{Action: "move_up"})
			case "d":
				return m.dispatch(Command{Action: "scroll_down"})
			case "u":
				return m.dispatch(Command{Action: "scroll_up"})
			case "/":
				return m.dispatch(Command{Action: "search"})
			case "n":
				return m.dispatch(Command{Action: "find_next"})
			case "N":
				return m.dispatch(Command{Action: "find_previous"})
			case "g":
				return m.dispatch(Command{Action: "move_document_start"})
			case "G":
				return m.dispatch(Command{Action: "move_document_end"})
			}
		}
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
	// Review Diff Mode toggle.
	"toggle_review_diff_mode": true,
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
	if c, ok := m.hexAction(cmd.Action); ok {
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
	res := m.editorDo(document.Action(cmd.Action), cmd.Args)
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
	case "toggle_sticky_scroll":
		m.stickyScroll = !m.stickyScroll
		if m.stickyScroll {
			m.notice = "Sticky scroll enabled"
		} else {
			m.notice = "Sticky scroll disabled"
		}
	case "toggle_review_diff_mode":
		return m.toggleReviewMode(), true
	case "review_diff_mode_refresh":
		if m.review.active {
			return m.reviewRefreshCmd(), true
		}
		return m.enterReviewMode(false), true
	case "review_diff_mode_change_base":
		if !m.review.active {
			return tea.Batch(m.enterReviewMode(false), m.reviewOpenBasePickerCmd()), true
		}
		return m.reviewOpenBasePickerCmd(), true
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
		if m.explorerOpen && m.explorerFocus {
			m.explorerPanel.SearchBackspace(m.explorerGeometry(m.layout()))
		}
	case "markdown_toc_show":
		m.markdownTOCShown()
	case "markdown_toc_toggle":
		m.markdownTOCToggle()
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
	case "detach", "detach_daemon":
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
		if cmd, ok := m.lspAction(action, args); ok {
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
func (m *Model) explorerGeometry(l frame.Layout) expanel.Geometry {
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
	m.blurPluginPanels()
	m.menu = m.menu.Close()
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

// focusEditor gives keyboard focus back to the editor. The panel stays visible. It also hands the widget-panel
// layer back from a focused plugin section; plugin input modes and key captures
// are plugin state and are documented as such (they do not see keys while a native
// chrome region owns the keyboard).
func (m *Model) focusEditor() {
	m.blurPluginPanels()
	m.focusEditorChrome()
}

// focusEditorChrome clears the chrome focus flags without touching a focused plugin
// panel (focusPluginPanel has just set one). The plugin panel layer is separate
// from the plugin's own input modes and key captures.
func (m *Model) focusEditorChrome() {
	m.explorerFocus = false
	m.contentsFocus = false
	m.contents.SetFocus(false)
	m.diag.focus = false
	m.tabFocus = false
	m.dock.SetFocus(false)
}

// The places keyboard focus can be, in F7 chrome-cycle order. The menu is open while it has
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
// editor, and back to the menu. F7 sends it. The menu is skipped while the menu
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
	// The Diagnostics section follows Contents in the F7 order, matching its place
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
	// Every stage is a non-plugin region: hand the keyboard back from a focused
	// plugin section first, so the plugin key layer cannot swallow keys after F7.
	m.blurPluginPanels()
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
	m.diag.focus = false
	m.blurPluginPanels()
	m.menu = m.menu.Close()
	m.divider = dividerState{}
	if m.tree != nil {
		m.explorerPanel = expanel.New(m.tree, m.styles, m.now).WithOptions(m.view.opts.Explorer).CarryCut(m.explorerPanel)
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
func (m *Model) layoutInput() frame.Input {
	split, leaves := m.splitInput()
	in := frame.Input{
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
		Style:           frame.Classic,
		HideStatus:      m.statusHidden(),
		HideLineNumbers: m.hideLineNumbers,
		DockOpen:        m.dock.Open(),
		DockRows:        m.dockRows,
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
	if m.review.active {
		in.ExplorerOpen = m.review.sidebarOpen
		if in.ExplorerOpen {
			in.Sidebar = []sidebar.Spec{{Kind: sidebar.KindExplorer}}
		} else {
			in.Sidebar = nil
		}
		in.HideTabBar = false
		in.Welcome = false
	}
	return in
}

// layout is the geometry of the current frame. Painting and mouse hit testing
// both read it.
func (m *Model) layout() frame.Layout {
	if m.frameLayout != nil {
		return *m.frameLayout
	}
	if c := &m.hoverLayout; c.on {
		key := m.hoverLayoutKey()
		if !c.ok || c.key != key {
			c.l, c.key, c.ok = frame.ComputeLayout(m.width, m.height, m.layoutInput()), key, true
		}
		return c.l
	}
	return frame.ComputeLayout(m.width, m.height, m.layoutInput())
}

// hoverLayoutCache shares one geometry between the layout calls of a hover-only mouse
// motion update, which makes five or more of them (the hover state, the hit map, the caret,
// the plugin sections) for a state that a pointer move does not change. It is on only for
// the duration of that update, and it is rebuilt if the state it is keyed on moves.
type hoverLayoutCache struct {
	on, ok bool
	key    hoverLayoutKey
	l      frame.Layout
}

// hoverLayoutKey is the cheap state of a hover update that the geometry follows: the
// status text, the tabs, the overlays and the panels that open or close.
type hoverLayoutKey struct {
	notice, status                             string
	tabs, active, overlays, panels             int
	explorerWidth                              explorer.Width
	dockRows                                   int
	explorerOpen, dockOpen, menuOpen, findOpen bool
}

func (m *Model) hoverLayoutKey() hoverLayoutKey {
	return hoverLayoutKey{
		notice: m.notice, status: m.ed.Status(),
		tabs: len(m.ed.Tabs()), active: m.activeTab(), overlays: len(m.overlays), panels: len(m.plug.panels.order),
		explorerWidth: m.explorerWidth, dockRows: m.dockRows,
		explorerOpen: m.explorerOpen, dockOpen: m.dock.Open(), menuOpen: m.menu.IsOpen(), findOpen: m.findOpen(),
	}
}

// pointerOnly reports whether mo moves the pointer with no button and no drag in progress,
// so the update changes hover state only.
func (m *Model) pointerOnly(mo tea.MouseMotionMsg) bool {
	return tea.Mouse(mo).Button == tea.MouseNone && !m.drag.active && !m.divider.active && m.drag.tab == nil &&
		!m.explorerPanel.Dragging() && !m.dock.MouseDragging()
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
func (m *Model) statusView() frame.StatusView {
	s := frame.StatusView{Message: m.ed.Status(), EOL: "LF", Encoding: "UTF-8", Language: "Text"}
	if m.notice != "" {
		s.Message = m.notice
	} else if notice := m.hexNotice(); notice != "" {
		s.Message = notice
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
		if isBinaryDoc(doc) {
			if h, ok := m.activeHex(); ok && h.on {
				s.HexView = uiText("gvim.status.binary_summary", "Summary")
			} else {
				s.HexView = uiText("gvim.status.binary_hex", "Hex")
			}
		}
		s.ReadOnly = isDocReadOnly(doc)
		if cl, ok := doc.(editorpane.CaretLister); ok {
			s.Cursors += len(cl.Cursors())
		}
	}
	m.viewStatus(&s)
	s.PaletteHint = m.paletteHint()
	s.Clock = m.now().Format(clockFormat)
	s.Chord = m.statusChord()
	s.Extra = m.statusExtraItems()
	if m.review.active {
		m.reviewStatusInfo(&s)
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
	// The element lists are parsed once per configuration: the row is read by every layout.
	if c := &m.statusRows; c.ok && slices.Equal(c.opts.Left, opts.Left) && slices.Equal(c.opts.Right, opts.Right) && c.opts.Separator == opts.Separator {
		return c.row
	}
	row := m.parseStatusRow(opts)
	m.statusRows = statusRowCache{opts: StatusBarOptions{Left: slices.Clone(opts.Left), Right: slices.Clone(opts.Right), Separator: opts.Separator}, row: row, ok: true}
	return row
}

// statusRowCache is the parsed status row of the options it was parsed from.
type statusRowCache struct {
	opts StatusBarOptions
	row  frame.StatusRow
	ok   bool
}

// parseStatusRow parses the element lists of opts.
func (m *Model) parseStatusRow(opts StatusBarOptions) frame.StatusRow {
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
	if m.motionSkipView && m.width > 0 && m.height > 0 && m.width == m.motionCachedW && m.height == m.motionCachedH {
		v := tea.NewView(m.motionCachedView)
		v.AltScreen = true
		v.ReportFocus = true
		v.MouseMode = tea.MouseModeAllMotion
		if m.motionCachedCaret {
			v.Cursor = tea.NewCursor(m.motionCachedCaretX, m.motionCachedCaretY)
		}
		m.viewTerminal(&v)
		return v
	}
	// The state is not changed while a frame is painted, so the geometry is computed once
	// for the paint, the caret and the terminal placement instead of once per use.
	l := m.layout()
	m.frameLayout = &l
	defer func() { m.frameLayout = nil }()
	content := ""
	var cx, cy int
	var cok, paintedFocus bool
	if m.width > 0 && m.height > 0 {
		m.paintGrid.Resize(m.width, m.height)
		cx, cy, cok, paintedFocus = m.paintIntoWithCaret(&m.paintGrid)
		content = m.paintGrid.String()
	}
	v := tea.NewView(content)
	v.AltScreen = true
	// Focus reporting is always on: terminal children that enable DECSET 1004 need
	// the real outer-window focus merged with the dock's keyboard focus (the model
	// forwards FocusMsg/BlurMsg to the dock), and a remote authority still reconnects
	// when the window is activated.
	v.ReportFocus = true
	v.MouseMode = tea.MouseModeAllMotion
	if !paintedFocus || !m.editorOwnsCaret(l) {
		cx, cy, cok = m.caret()
	}
	if cok {
		v.Cursor = tea.NewCursor(cx, cy)
	}
	if m.width > 0 && m.height > 0 {
		m.motionCachedView = content
		m.motionCachedW, m.motionCachedH = m.width, m.height
		m.motionCachedCaret, m.motionCachedCaretX, m.motionCachedCaretY = cok, cx, cy
	}
	m.viewTerminal(&v)
	return v
}

// imageQueue returns the commands the viewer starts after an update: the
// capability query the first time an image tab is opened, and the read of a
// picture the terminal does not have yet. It runs at the end of Update, before the
// frame: the text summary is painted while the read is in flight, and the
// placeholders are painted only after the upload's Raw message has landed.
//
// Nothing is started while no image tab is open, so a session of text files never
// sends a graphics query.
func (m *Model) imageQueue() tea.Cmd {
	if m.ed == nil {
		return nil
	}
	if m.img.off || m.img.localOnly {
		return nil
	}
	if !m.imageDocShown() {
		// Computing the layout is the cost of the walk below, and every update comes
		// here: a session of text files stops at this cheap look at the pane documents.
		return nil
	}
	type imageTarget struct {
		path string
		doc  Document
	}
	var targets []imageTarget
	seen := make(map[string]bool)
	for _, pane := range m.layout().Panes {
		doc := m.paneDoc(pane)
		if doc == nil {
			continue
		}
		path, ok := m.imagePathOf(doc)
		if !ok || path == "" || seen[path] {
			continue
		}
		seen[path] = true
		targets = append(targets, imageTarget{path: path, doc: doc})
	}
	if len(targets) == 0 {
		return nil
	}
	if !m.img.probed && !m.img.arming {
		// The first image tab asks the terminal once. It is not part of Init, so a
		// session of text files, and a test that counts Init's commands, is unaffected.
		m.img.arming = true
		return func() tea.Msg { return imageProbeArmMsg{} }
	}
	if !m.img.imageEnabled() {
		if !m.img.mosaicEnabled() {
			return nil
		}
		// No graphics support: decode the pictures for the half-block fallback
		// instead. An entry whose stat is current is loaded, loading or failed;
		// a missing or stale one is (re)read.
		var cmds []tea.Cmd
		for _, target := range targets {
			size, mtime := m.imageStatOf(target.doc)
			if e, ok := m.img.mosaic[target.path]; ok && e.size == size && e.mtime == mtime {
				continue
			}
			m.img.mosaicPut(target.path, &mosaicEntry{size: size, mtime: mtime, pending: true})
			cmds = append(cmds, mosaicLoadCmd(target.path, size, mtime))
		}
		return tea.Batch(cmds...)
	}
	if m.img.pending == nil {
		m.img.pending = make(map[int]bool)
	}
	var cmds []tea.Cmd
	for _, target := range targets {
		// imageID allocates the id (and says whether the file changed since it was
		// sent). An upload in flight is not queued twice.
		size, mtime := m.imageStatOf(target.doc)
		id, send := m.img.imageID(target.path, size, mtime)
		if !send || m.img.pending[id] {
			continue
		}
		m.img.pending[id] = true
		cmds = append(cmds, imageSendCmd(target.path, id))
	}
	return tea.Batch(cmds...)
}

// imageDocShown reports whether any pane of the split tree shows a document that has
// a local picture to upload. It reads the split leaves, not the layout.
func (m *Model) imageDocShown() bool {
	for _, n := range m.splits.Root.Leaves() {
		if doc := m.leafDoc(n); doc != nil {
			if _, ok := m.imagePathOf(doc); ok {
				return true
			}
		}
	}
	return false
}

// paintInto draws the frame into g. The theme inspector calls it once with key
// capture enabled on a fresh grid, so the paint path itself is the same in both
// cases; normal frames capture nothing.
func (m *Model) paintInto(g *grid.Grid) {
	m.paintIntoWithCaret(g)
}

func (m *Model) paintIntoWithCaret(g *grid.Grid) (cx, cy int, ok, paintedFocus bool) {
	l := m.layout()
	if l.Gated {
		g.Put(0, 0, g.Width(), frame.GateText, grid.Style{})
		return 0, 0, false, false
	}
	body := style.TaggedCell(m.styles.Editor, "editor", "fg", "editor", "bg")
	g.Clear(body)
	if m.review.active {
		m.paintReviewMode(g, l)
	} else {
		cx, cy, ok, paintedFocus = m.paintEditor(g, l)
		m.paintDropZone(g)
		m.dock.Draw(g, l, m.styles)
		tabstrip.Draw(g, m.styles, l.EditorX, l.StripY, l.Strip, m.stripHover)
		if l.ExplorerOpen {
			m.paintSidebar(g, l)
			if m.gripHover {
				m.paintGripHover(g, l)
			}
		}
	}
	if l.FrameOn {
		m.paintFrameChrome(g, l)
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
	return cx, cy, ok, paintedFocus
}

// paintFrameChrome draws the Style's border outside the workspace cells: the glyphs
// are the layout's Chrome, which the same Style gave the geometry.
func (m *Model) paintFrameChrome(g *grid.Grid, l frame.Layout) {
	st := m.splitCell()
	ch := l.Chrome
	for x := 1; x < l.W-1; x++ {
		g.Set(x, l.FrameTop, ch.Horizontal, st)
		g.Set(x, l.FrameBottom, ch.Horizontal, st)
	}
	for y := l.FrameTop + 1; y < l.FrameBottom; y++ {
		g.Set(0, y, ch.Vertical, st)
		g.Set(l.W-1, y, ch.Vertical, st)
	}
	g.Set(0, l.FrameTop, ch.TopLeft, st)
	g.Set(l.W-1, l.FrameTop, ch.TopRight, st)
	g.Set(0, l.FrameBottom, ch.BottomLeft, st)
	g.Set(l.W-1, l.FrameBottom, ch.BottomRight, st)
	if l.ExplorerOpen {
		x := l.ExX + l.Ex - 1
		if l.ExRight {
			x = l.ExX
		}
		g.Set(x, l.FrameTop, ch.TopJoin, st)
		g.Set(x, l.FrameBottom, ch.BottomJoin, st)
		// The divider between two sidebar sections runs into the side border the
		// column stands against, so the border turns into the column there.
		for _, sec := range l.Sections {
			if sec.Index == 0 {
				continue
			}
			if l.ExX == 0 {
				g.Set(0, sec.HeaderY, ch.LeftJoin, st)
			}
			if l.ExX+l.Ex == l.W {
				g.Set(l.W-1, sec.HeaderY, ch.RightJoin, st)
			}
		}
	}
	for _, border := range l.Borders {
		if border.Vertical && border.Y+border.H == l.FrameBottom {
			g.Set(border.X, l.FrameBottom, ch.BottomJoin, st)
		}
	}
}

// paintEditor paints the editor panes into the body, to the right of the explorer.
// Each pane draws its visible lines, the gutter, the text, the selection, the change
// marks and the scrollbar, and the lines between the panes are drawn in their gaps.
// With no tab open, the welcome screen is painted instead.
func (m *Model) paintEditor(g *grid.Grid, l frame.Layout) (cx, cy int, ok, paintedFocus bool) {
	if m.ed.Active() == nil {
		paintWelcome(g, m.styles, l, m.welcomeSel)
		return 0, 0, false, false
	}
	return m.paintPanes(g, l)
}

// editorOwnsCaret reports whether the focused editor pane owns the terminal caret.
func (m *Model) editorOwnsCaret(l frame.Layout) bool {
	if l.Gated || len(m.overlays) > 0 || m.review.active {
		return false
	}
	if m.explorerFocus || m.contentsFocus || m.diag.focus || m.menu.IsOpen() {
		return false
	}
	if m.plug.panels.focus != 0 || m.dockKeyboard() {
		return false
	}
	doc := m.ed.Active()
	if doc == nil || m.tabFocus {
		return false
	}
	if _, ok := m.activeHex(); ok {
		return false
	}
	return true
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
	if m.explorerFocus || m.contentsFocus || m.diag.focus || m.menu.IsOpen() {
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
	if m.review.active {
		return 0, 0, false
	}
	doc := m.ed.Active()
	if doc == nil || m.tabFocus {
		return 0, 0, false
	}
	if _, ok := m.activeHex(); ok {
		// The hex dump has no text caret: the terminal cursor stays hidden.
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
// that is already inactive is not pushed. An exclusive transient overlay first
// closes the exclusive overlays below it, and an alert keeps the input: the new
// overlay goes under it (see placeOverlay). The stackable layers, such as a dialog
// or a prompt that needs an answer, stay in place.
func (m *Model) pushOverlay(o Overlay) {
	if o != nil && o.Active() {
		if !m.placeOverlay(o) && o.Active() {
			m.overlays = append(m.overlays, o)
		}
	}
}

// routeSettingsMessage routes a Settings page refresh tick/snapshot to the page it names,
// regardless of the top overlay. A page no longer on the stack (or closed) drops the old
// message without scheduling a replacement tick.
func (m *Model) routeSettingsMessage(msg tea.Msg) (tea.Cmd, bool) {
	var page *settingsPage
	switch msg := msg.(type) {
	case settingsRefreshTickMsg:
		page = msg.page
	case settingsSnapshotMsg:
		page = msg.page
	default:
		return nil, false
	}
	if page == nil || !page.Active() {
		return nil, true
	}
	if slices.Contains(m.overlays, Overlay(page)) {
		_, cmd := page.Update(msg)
		return cmd, true
	}
	return nil, true
}

// routeOverlay gives msg to the top overlay and applies the result: the
// overlay it returns takes its place, a nil return pops it, and an overlay that
// is no longer active is popped. Esc pops the top overlay and is not forwarded,
// unless the overlay asks for it (see takesEscape).
func (m *Model) routeOverlay(msg tea.Msg) tea.Cmd {
	if k, ok := msg.(tea.KeyPressMsg); ok && !m.pluginCancelKey(tea.Key(k)) {
		// The overlay contexts consult the keymap before the auto-cancel and the built-in
		// handler; an unbound key falls through to both, while ctrl+c on a plugin overlay
		// always cancels the modal wait (see pluginCancelKey).
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
	return m.deliverOverlay(msg)
}

// popOverlay removes the top overlay. It does nothing on an empty stack.
func (m *Model) popOverlay() {
	n := len(m.overlays)
	if n == 0 {
		return
	}
	// Resume captures Live Grep's controls as the overlay closes: the query, modes and
	// scopes. Hits and the selected row are not kept because Resume searches again. The
	// capture is in memory, so closing starts no search or I/O.
	m.captureLiveResume(m.overlays[n-1])
	m.overlays[n-1] = nil
	m.overlays = m.overlays[:n-1]
}

// updateChromeHover keeps the chrome's pointer feedback in step with the motion:
// the run under the pointer in the tab strip and the status row underlines, and
// the explorer's resize grip takes the accent. The hit map decides all three, so
// the feedback and the click target are always the same cells.
func (m *Model) updateChromeHover(mo tea.Mouse) {
	m.stripHover, m.statusHover, m.gripHover, m.borderHover = frame.HitTarget{}, frame.HitTarget{}, false, -1
	t := m.layout().Hit(mo.X, mo.Y)
	switch t.Kind {
	case frame.HitTab, frame.HitTabClose, frame.HitTabNew, frame.HitTabScrollBefore, frame.HitTabScrollAfter:
		m.stripHover = t
	case frame.HitStatusSegment, frame.HitStatusPalette, frame.HitStatusPlugin:
		m.statusHover = t
	case frame.HitExplorerGrip:
		m.gripHover = true
	case frame.HitSplitBorder:
		m.borderHover = t.Index
	case frame.HitWelcomeRow:
		m.welcomeSel = t.Index
	}
}

// paintGripHover recolours the explorer's resize grip while the pointer is on it.
// The grip is the column's editor-facing wall on every section body, the cells the
// hit map reports; the accent is what says the wall can be dragged, since the wall
// is otherwise a plain border.
func (m *Model) paintGripHover(g *grid.Grid, l frame.Layout) {
	if !l.ExplorerOpen || l.Ex <= 0 {
		return
	}
	x := l.ExX + l.Ex - 1
	if l.ExRight {
		x = l.ExX
	}
	accent := style.Cell(m.styles.Accent).BG
	ink := grid.Style{FG: accent}
	if accent != nil {
		ink.FGKey = style.KeyIDFor("ui", "accent_bg")
	}
	for _, sec := range l.Sections {
		for y := sec.BodyTop; y <= sec.BodyBottom; y++ {
			g.Set(x, y, "│", ink)
		}
	}
}

// paintStatus fills the status row and writes its segments. A hidden status row
// paints nothing.
func (m *Model) paintStatus(g *grid.Grid, l frame.Layout) {
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
	// error foreground over the bar. The hovered element takes Fresh's ERROR hover
	// keys (repainted/underlined, Hover::Swap) instead of only an underline: the
	// same label text, which now names the real foreground program when the poll saw
	// one, in the theme's status_error_indicator_hover_fg on ..._hover_bg.
	restart := bar
	if fg := style.Cell(m.styles.DiagError).FG; fg != nil {
		restart.FG, restart.Bold = fg, true
	} else if fg := style.Cell(m.styles.DiagWarning).FG; fg != nil {
		restart.FG, restart.Bold = fg, true
	}
	g.Fill(0, g.Width(), l.StatusY, bar)
	for _, r := range l.Status {
		st := bar
		// Every clickable run acknowledges the pointer the same way: the text is
		// underlined while the pointer is on it. The restart element adds Fresh's
		// error-hover colours; the others keep the role they already wear.
		hovered := m.statusHover.Kind != frame.HitNone &&
			m.statusHover.Kind == r.Kind && m.statusHover.Index == r.Index
		switch {
		case r.Style == frame.StyleError:
			st = restart
		case r.Style == frame.StyleWarning:
			st = warning
		case r.Style == frame.StyleMuted:
			st = muted
		case r.Kind == frame.HitStatusSegment && r.Index == frame.StatusSegmentTerminalRestart:
			st = restart
			if hovered {
				if c := style.Color(m.theme, "ui", "status_error_indicator_hover_fg"); c != nil {
					st.FG = c
					st.FGKey = style.KeyIDFor("ui", "status_error_indicator_hover_fg")
				}
				if c := style.Color(m.theme, "ui", "status_error_indicator_hover_bg"); c != nil {
					st.BG = c
					st.BGKey = style.KeyIDFor("ui", "status_error_indicator_hover_bg")
				}
			}
		case r.Kind == frame.HitStatusPalette:
			st = palette
		}
		if hovered {
			st.Underline = true
		}
		g.Put(r.X, l.StatusY, g.Width(), r.Text, st)
	}
	for i, text := range l.Notice {
		y := l.NoticeY + i
		g.Fill(l.NoticeX, l.NoticeX+l.NoticeW, y, bar)
		g.Put(l.NoticeX+space.PadX, y, l.NoticeX+l.NoticeW, text, bar)
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
	m.setTabFirst(frame.RevealedFirst(m.stripTabs(), l.EditorX, l.EditorX+l.EditorW, l.Strip.First, i))
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

// isDocReadOnly reports whether doc says it is read-only.
func isDocReadOnly(doc Document) bool {
	if ro, ok := doc.(ReadOnlyDoc); ok {
		return ro.ReadOnly()
	}
	return false
}

// lowerIfLetter returns the lower-case letter for r, or r itself as a string.
func lowerIfLetter(r rune) string {
	if unicode.IsLetter(r) {
		return string(unicode.ToLower(r))
	}
	return string(r)
}

// wheelRows returns the configured mouse wheel scroll lines (default 3).
func (m *Model) wheelRows() int {
	if m.view.opts.MouseWheelScrollLines > 0 {
		return m.view.opts.MouseWheelScrollLines
	}
	return wheelRows
}

// doubleClickWindow returns the configured multi-click detection window (default 500ms).
func (m *Model) doubleClickWindow() time.Duration {
	if m.view.opts.DoubleClickTimeMS > 0 {
		return time.Duration(m.view.opts.DoubleClickTimeMS) * time.Millisecond
	}
	return space.DoubleClickWindow
}

// clearMouseHover clears all chrome hover highlights and in-flight drag state.
func (m *Model) clearMouseHover() {
	m.stripHover, m.statusHover, m.gripHover, m.borderHover = frame.HitTarget{}, frame.HitTarget{}, false, -1
	m.cancelMouseHover()
	m.drag = drag{}
	m.dock.ClearHover()
}

// isAltGr reports Windows AltGr, which its console reports as Ctrl+Alt. It is a
// text-input modifier, not a chord: the caller passes only a printable single
// rune on to the text path. Super/Meta (Command) cannot be part of AltGr.
func isAltGr(mod tea.KeyMod) bool {
	return mod&(tea.ModCtrl|tea.ModAlt) == tea.ModCtrl|tea.ModAlt && mod&(tea.ModSuper|tea.ModMeta) == 0
}
