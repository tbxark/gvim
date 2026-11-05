package ui

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/buffer"
	"github.com/tbxark/gvim/internal/editor"
	"github.com/tbxark/gvim/internal/explorer"
	palmodel "github.com/tbxark/gvim/internal/palette"
	"github.com/tbxark/gvim/internal/syntax"
	"github.com/tbxark/gvim/internal/theme"
	"github.com/tbxark/gvim/internal/ui/browser"
	"github.com/tbxark/gvim/internal/ui/dialog"
	"github.com/tbxark/gvim/internal/ui/editorpane"
	expanel "github.com/tbxark/gvim/internal/ui/explorer"
	"github.com/tbxark/gvim/internal/ui/find"
	"github.com/tbxark/gvim/internal/ui/grid"
	"github.com/tbxark/gvim/internal/ui/menubar"
	"github.com/tbxark/gvim/internal/ui/palette"
	"github.com/tbxark/gvim/internal/ui/prompt"
	"github.com/tbxark/gvim/internal/ui/style"
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

	// The palette's sources. The commands always have a table, and files and
	// ranker are nil when Deps leaves them out.
	commands []palmodel.Entry
	files    palette.Files
	ranker   palette.Ranker
	recorder fileRecorder // the ranker's frecency record, when it has one

	// findBar is the last find bar. It stays after the bar closes, so find_next and
	// find_previous can run on its search text. The bar is on the overlay stack
	// only while findOpen reports it.
	findBar *find.Bar

	width, height int
	explorerOpen  bool    // the panel is shown
	explorerFocus bool    // the panel has keyboard focus
	tabFocus      bool    // the tab strip has keyboard focus (F6 cycle)
	dialogs       Dialogs // the file dialogs; nil disables them
	// explorerOverride is the panel width set by a grip drag. Zero means the
	// automatic width. The grip drag itself comes in the explorer phase.
	explorerOverride int

	overlays []Overlay // the stack; the last one is on top and gets the input first

	menu          menubar.Model // the menu bar and its open dropdown
	explorerPanel expanel.Panel // paints the explorer and answers its key actions
	ops           explorerOps   // file operations; nil when Deps.Ops lacks them
	clip          *clipboard    // the explorer clipboard; nil when empty
	quitting      bool          // set by a menu row that quit the editor
	pending       tea.Cmd       // commands a menu row started, returned by routeMenu
	notice        string        // replaces the editor status until the next press

	clicks    clickCounter
	lastPress int // the count of the last editor press, see pressEditor
	drag      drag
	// langs memoizes languageFor by file name.
	langs map[string]string

	// tabFirst is the tab strip's scroll position, the index of its first tab.
	// placeStrip clamps it; setTabFirst keeps the clamped value here.
	tabFirst int
	// recents are the recent folders the welcome screen lists.
	recents []string
	// welcomeSel is the highlighted row of the welcome screen.
	welcomeSel int
}

// drag is the pointer gesture in progress: a text selection, or a grab of the
// scrollbar thumb. The zero value is no gesture.
type drag struct {
	active bool
	thumb  bool        // the scrollbar thumb: motion moves the viewport
	unit   editor.Unit // the unit a text drag grows the selection by
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
	commands := deps.Commands
	if commands == nil {
		commands = palmodel.Commands(nil)
	}
	m := &Model{
		ed:       deps.Editor,
		tree:     deps.Explorer,
		keys:     deps.Keys,
		styles:   st,
		panes:    editorpane.NewStyles(deps.Theme),
		now:      now,
		theme:    deps.Theme,
		commands: commands,
		recents:  deps.Recents,
	}
	// Assign only a non-nil source: a nil concrete value in an interface would
	// look like a file index or ranker that is present.
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
	m.menu = menubar.New(m, st)
	if deps.Explorer != nil {
		m.explorerPanel = expanel.New(deps.Explorer, st, now)
	}
	m.dialogs = deps.Dialogs
	if deps.ShowExplorer && deps.Explorer != nil {
		m.explorerOpen = true
		m.expandRoot()
	}
	if ops, ok := deps.Ops.(explorerOps); ok {
		m.ops = ops
	}
	return m
}

// Run runs m in the alternate screen until it quits or ctx is canceled. A
// cancel is reported as the context's error.
func Run(ctx context.Context, m *Model) error {
	_, err := tea.NewProgram(m, tea.WithContext(ctx)).Run()
	if err != nil && ctx.Err() != nil {
		return ctx.Err()
	}
	return err
}

// Init implements tea.Model. The model starts no commands.
func (m *Model) Init() tea.Cmd {
	return nil
}

// Update implements tea.Model. It runs update, then scrolls the tab strip to the
// active tab when the active tab or the number of tabs changed, and when the
// frame size changes (the first size shows the active tab too). Scrolling by the
// wheel or a marker changes neither, so it is not undone here.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	active, count := m.activeTab(), len(m.ed.Tabs())
	_, resized := msg.(tea.WindowSizeMsg)
	next, cmd := m.update(msg)
	if resized || m.activeTab() != active || len(m.ed.Tabs()) != count {
		m.revealActiveTab()
	}
	return next, cmd
}

// update handles one message. While an overlay is open, key and mouse messages
// go to the top overlay (see Overlay). Otherwise key presses go through the
// keymap in the focused context, and text that no binding claims is inserted
// when the editor has focus. Mouse presses, drags and wheel notches go through
// the layout's hit map.
func (m *Model) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.overlayInput(msg) {
		cmd := m.routeOverlay(msg)
		// An overlay that opened or closed the find bar changes the editor body.
		m.syncViewport()
		if _, isKey := msg.(tea.KeyPressMsg); isKey && m.findOpen() {
			m.revealCaret()
		}
		return m, cmd
	}
	// A message that is not input still goes to the top overlay, because an overlay
	// may have started the work that sends it (the palette's file ranking). The
	// root handles the message too.
	var overlayCmd tea.Cmd
	if len(m.overlays) > 0 {
		overlayCmd = m.routeOverlay(msg)
	}
	if taken, cmd := m.routeMenu(msg); taken {
		return m, tea.Batch(overlayCmd, cmd)
	}
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.syncMenu()
		m.syncViewport()
		m.followSelection()
	case tea.KeyPressMsg:
		m.notice = ""
		return m, m.handleKey(tea.Key(msg))
	case tea.MouseClickMsg:
		m.notice = ""
		cmd = m.handleClick(msg)
	case tea.MouseMotionMsg:
		m.handleDrag(msg.Mouse())
		cmd = m.explorerMouse(msg)
	case tea.MouseReleaseMsg:
		m.drag = drag{}
		cmd = m.explorerMouse(msg)
	case tea.MouseWheelMsg:
		cmd = m.handleWheel(msg)
	case dialog.Result:
		cmd = m.dialogDone(msg)
	case menubar.Failed:
		m.notice = failedNotice(msg)
	case expanel.OpenFileMsg:
		m.openFile(msg.Path, msg.Preview)
		if !msg.Preview {
			m.focusEditor()
		}
	case expanel.ReloadMsg:
		m.reloadTree()
	case expanel.CloseMsg:
		m.closeExplorer()
	case expanel.FocusMsg:
		m.focusExplorer()
	case expanel.ResizeMsg:
		m.explorerOverride = msg.Width
	case expanel.ErrorMsg:
		m.notice = msg.Err.Error()
	case expanel.NewFileMsg:
		m.askName(msg.Dir, false)
	case expanel.NewFolderMsg:
		m.askName(msg.Dir, true)
	case expanel.RenameMsg:
		m.askRename(msg.Path)
	case expanel.DeleteMsg:
		m.askDelete(msg.Path)
	case expanel.CopyMsg:
		m.clip = &clipboard{path: msg.Path}
	case expanel.CutMsg:
		m.clip = &clipboard{path: msg.Path, cut: true}
	case expanel.PasteMsg:
		cmd = m.paste(msg.Dir)
	case prompt.Done:
		cmd = m.answer(msg)
	case fileResultMsg:
		m.fileDone(msg)
	case browser.Open:
		m.openFileAt(msg.Path, msg.Line, msg.Col)
	case browser.Choose:
		m.notice = "Switch Project is not supported in this port yet"
	case palette.RunCommandMsg:
		cmd = m.runCommand(msg.Action)
	case palette.OpenFileMsg:
		if m.recorder != nil {
			m.recorder.Record(msg.Rel)
		}
		m.openFile(msg.Path, false)
		m.focusEditor()
	case palette.GotoLineMsg:
		// The ':' mode opens the go-to-line prompt with the chosen position, so the
		// user confirms it there.
		m.openGoto(gotoInput(msg.Line, msg.Col))
	case palette.ActivateBufferMsg:
		m.activateTab(msg.Index)
	}
	return m, tea.Batch(overlayCmd, cmd)
}

// routeMenu gives msg to the menu bar when the bar takes it. A key the open
// dropdown answers, a left press on a label, and a press on the open dropdown
// are taken. A press outside an open dropdown closes it and is not taken, so it
// still reaches the frame. It returns the quit command when a menu row quit the
// editor.
func (m *Model) routeMenu(msg tea.Msg) (bool, tea.Cmd) {
	m.syncMenu()
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
		return true, tea.Batch(cmd, tea.Quit)
	}
	// A message that is not taken only closes the dropdown. Its command is nil,
	// unless a row ran an explorer action that started one.
	return taken, cmd
}

// isLabelPress reports whether msg is a left press on a label of the menu bar.
func isLabelPress(bar menubar.Model, msg tea.Msg) bool {
	mo, ok := msg.(tea.MouseClickMsg)
	if !ok || mo.Button != tea.MouseLeft {
		return false
	}
	return bar.Hit(mo.X, mo.Y).Kind == menubar.TargetLabel
}

// syncMenu gives the menu bar the frame size and the explorer focus of the
// current state. Closing the dropdown when its label leaves the bar is part of
// WithExplorerFocus.
func (m *Model) syncMenu() {
	m.menu = m.menu.SetSize(m.width, m.height).WithExplorerFocus(m.explorerFocus)
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
	if cmd, ok := m.uiAction(action); ok {
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
	res := m.ed.Do(editor.Action(action), nil)
	m.syncViewport()
	if errors.Is(res.Err, editor.ErrUnknownAction) {
		return menubar.ErrUnsupported
	}
	if res.Quit {
		m.quitting = true
	}
	return nil
}

// Holds implements menubar.Ports. It evaluates the legacy context keys that the
// menu rows name, against the current state. A key the port does not model does
// not hold, so its rows stay disabled.
func (m *Model) Holds(name string) bool {
	doc := m.ed.Active()
	switch name {
	case "has_buffer", "has_text_buffer":
		return doc != nil
	case "can_edit", "can_paste", "line_numbers", "vertical_scrollbar",
		"horizontal_scrollbar", "mouse_capture", "keymap_default":
		return true
	case "can_save", "can_revert":
		return doc != nil && hasPath(doc)
	case "can_save_all":
		return slices.ContainsFunc(m.ed.Tabs(), func(t TabInfo) bool { return t.Modified })
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
	}
	return false
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

// openFile opens path as a preview tab or a permanent one. The editor reports
// a failure in its status row.
func (m *Model) openFile(path string, preview bool) {
	f, ok := m.ed.(fileEditor)
	if !ok {
		m.notice = "Opening files is not supported in this port"
		return
	}
	if preview {
		_ = f.OpenPreview(path)
	} else {
		_ = f.OpenFile(path)
	}
	m.syncViewport()
}

// openFileAt opens path and moves the caret to line and col when line is set.
func (m *Model) openFileAt(path string, line, col int) {
	m.openFile(path, false)
	if f, ok := m.ed.(fileEditor); ok && line > 0 {
		if err := f.GotoLine(line, col); err != nil {
			m.notice = err.Error()
		}
	}
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
}

// clipboard is the explorer clipboard: one path, copied or cut.
type clipboard struct {
	path string
	cut  bool
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
// the root can run it again after a conflict.
type fileJob struct {
	kind   jobKind
	path   string           // the entry: rename, move, copy source, trash
	dir    string           // the directory: create, mkdir, move and copy destination
	name   string           // the new name: create, mkdir, rename
	policy expanel.Conflict // copy only
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
	deleteTag    struct{ path string }
	overwriteTag struct{ job fileJob }
	gotoTag      struct{}
)

// fileRecorder is the ranker's frecency record. A ranker that has one gets the
// path of each file the picker opens.
type fileRecorder interface {
	Record(rel string)
}

// openGoto shows the go-to-line prompt with input as its text. The prompt takes
// "N" or "N:COL", and Enter moves the caret there.
func (m *Model) openGoto(input string) {
	m.pushOverlay(lineOverlay{l: prompt.NewLine(m.styles, prompt.LineOptions{
		Label:    "Go to line:",
		Input:    input,
		Tag:      gotoTag{},
		Validate: prompt.ValidateLineCol,
	})})
}

// gotoInput is the text the go-to-line prompt starts with for a position picked
// from the palette. The column is shown only when it is not the first.
func gotoInput(line, col int) string {
	s := strconv.Itoa(line)
	if col > 1 {
		s += ":" + strconv.Itoa(col)
	}
	return s
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

// lineOverlay adapts *prompt.Line to Overlay. The prompt closes itself when it
// submits or is closed, and the adapter reports that to the stack.
type lineOverlay struct{ l *prompt.Line }

// Update forwards the message to the prompt. It returns nil once the prompt is
// closed, so the stack pops it.
func (o lineOverlay) Update(msg tea.Msg) (Overlay, tea.Cmd) {
	l, cmd := o.l.Update(msg)
	if !l.Active() {
		return nil, cmd
	}
	return lineOverlay{l: l}, cmd
}

// Draw paints the prompt on the bottom row.
func (o lineOverlay) Draw(g *grid.Grid) { o.l.Draw(g) }

// Active reports whether the prompt is still open.
func (o lineOverlay) Active() bool { return o.l.Active() }

// confirmOverlay adapts *prompt.Confirm to Overlay, as lineOverlay does for
// text prompts.
type confirmOverlay struct{ c *prompt.Confirm }

// Update forwards the message to the confirm. It returns nil once the confirm is
// closed, so the stack pops it.
func (o confirmOverlay) Update(msg tea.Msg) (Overlay, tea.Cmd) {
	c, cmd := o.c.Update(msg)
	if !c.Active() {
		return nil, cmd
	}
	return confirmOverlay{c: c}, cmd
}

// Draw paints the question on the bottom row.
func (o confirmOverlay) Draw(g *grid.Grid) { o.c.Draw(g) }

// Active reports whether the confirm is still open.
func (o confirmOverlay) Active() bool { return o.c.Active() }

// needOps reports whether the file operations are wired. When they are not, the
// notice row says so and the operation does nothing.
func (m *Model) needOps() bool {
	if m.ops == nil {
		m.notice = "File operations are not supported in this port"
	}
	return m.ops != nil
}

// askName opens the name prompt for a new file or folder in dir.
func (m *Model) askName(dir string, folder bool) {
	if !m.needOps() {
		return
	}
	label := "New file:"
	if folder {
		label = "New folder:"
	}
	m.pushOverlay(lineOverlay{l: prompt.NewLine(m.styles, prompt.LineOptions{
		Label:    label,
		Tag:      newNameTag{dir: dir, folder: folder},
		Validate: validateName,
	})})
}

// askRename opens the rename prompt for path, with its current name filled in.
func (m *Model) askRename(path string) {
	if !m.needOps() {
		return
	}
	m.pushOverlay(lineOverlay{l: prompt.NewLine(m.styles, prompt.LineOptions{
		Label:    "Rename to:",
		Input:    filepath.Base(path),
		Tag:      renameTag{path: path},
		Validate: validateName,
	})})
}

// askDelete opens the Yes/No confirm for trashing path. The focus starts on No.
func (m *Model) askDelete(path string) {
	if !m.needOps() {
		return
	}
	q := "Delete " + filepath.Base(path) + "?"
	m.pushOverlay(confirmOverlay{c: prompt.NewConfirm(m.styles, q, deleteTag{path: path})})
}

// askOverwrite asks whether a copy that hit an existing destination may replace
// it. Yes runs the copy with the overwrite policy, and No runs it with skip.
func (m *Model) askOverwrite(job fileJob) {
	q := "Overwrite " + filepath.Base(job.path) + "?"
	m.pushOverlay(confirmOverlay{c: prompt.NewConfirm(m.styles, q, overwriteTag{job: job})})
}

// validateName is the name check of the explorer prompts. It keeps the prompt
// open for a name that cannot be a single path element. The app adapter checks
// the rest, against the disk, through fsops.Validate.
func validateName(text string) error {
	switch {
	case strings.TrimSpace(text) == "":
		return errors.New("name is empty")
	case text == "." || text == "..":
		return errors.New("reserved name")
	case strings.ContainsAny(text, "/\x00"):
		return errors.New("name cannot contain / or NUL")
	}
	return nil
}

// answer applies a prompt's Done. A text prompt submits a name, and a confirm
// answers Yes or No. It returns the disk operation the answer starts, if any.
func (m *Model) answer(d prompt.Done) tea.Cmd {
	switch tag := d.Tag.(type) {
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
			return m.runJob(fileJob{kind: jobTrash, path: tag.path})
		}
	case overwriteTag:
		job := tag.job
		job.policy = expanel.ConflictSkip
		if d.Outcome == prompt.Yes {
			job.policy = expanel.ConflictOverwrite
		}
		return m.runJob(job)
	case gotoTag:
		if d.Outcome == prompt.Submitted {
			m.gotoText(d.Text)
		}
	}
	return nil
}

// paste starts the paste of the clipboard into dir. A cut moves the entry. A
// copy runs first with the cancel policy, so a conflict can be asked about.
func (m *Model) paste(dir string) tea.Cmd {
	if m.clip == nil {
		m.notice = "Nothing to paste"
		return nil
	}
	c := *m.clip
	if c.cut {
		return m.runJob(fileJob{kind: jobMove, path: c.path, dir: dir})
	}
	return m.runJob(fileJob{kind: jobCopy, path: c.path, dir: dir, policy: expanel.ConflictCancel})
}

// runJob returns the command that runs job on the disk. The command does not
// touch the editor or the tree. Its result comes back as a fileResultMsg.
func (m *Model) runJob(job fileJob) tea.Cmd {
	if !m.needOps() {
		return nil
	}
	ops := m.ops
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
			out, err = ops.Move(ctx, job.path, job.dir)
		case jobCopy:
			out, err = ops.Copy(ctx, job.path, job.dir, job.policy)
		case jobTrash:
			err = ops.Trash(ctx, job.path)
		}
		return fileResultMsg{job: job, out: out, err: err}
	}
}

// fileDone applies the result of a disk operation, in Update. A copy that hit an
// existing destination asks before it overwrites. Any other failure goes to the
// notice row. A success moves the open tabs of a renamed or moved entry, closes
// the tabs of a trashed one, reloads the tree and reveals the new entry.
func (m *Model) fileDone(r fileResultMsg) {
	if r.err != nil {
		if r.job.kind == jobCopy && r.job.policy == expanel.ConflictCancel && errors.Is(r.err, expanel.ErrConflict) {
			m.askOverwrite(r.job)
			return
		}
		m.notice = r.err.Error()
		return
	}
	job := r.job
	switch job.kind {
	case jobRename, jobMove:
		if r.out != job.path {
			if err := m.ops.FollowRename(job.path, r.out); err != nil {
				m.notice = err.Error()
			}
		}
		if job.kind == jobMove && m.clip != nil && m.clip.cut && m.clip.path == job.path {
			m.clip = nil
		}
	case jobTrash:
		if err := m.ops.CloseDeleted(job.path); err != nil {
			m.notice = err.Error()
		}
	}
	m.reloadTree()
	if job.kind != jobTrash {
		m.reveal(r.out)
	}
}

// reveal selects the entry at path in the tree, expanding its parents. A failure
// goes to the notice row.
func (m *Model) reveal(path string) {
	if m.tree == nil || path == "" {
		return
	}
	if err := m.tree.Reveal(path); err != nil {
		m.notice = err.Error()
	}
}

// openBrowser shows the file browser over the frame. folderOnly is the Switch
// Project mode: the list has folders only, and Enter chooses one.
func (m *Model) openBrowser(folderOnly bool) {
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
	m.pushOverlay(browserOverlay{b: b})
}

// browserOverlay adapts *browser.Browser to Overlay. The browser cannot name
// Overlay, because that would import this package. The browser closes itself
// when it sends a choice, and the adapter reports that to the stack.
type browserOverlay struct {
	b *browser.Browser
}

// Update forwards the message to the browser. It returns nil once the browser is
// closed, so the stack pops it.
func (o browserOverlay) Update(msg tea.Msg) (Overlay, tea.Cmd) {
	b, cmd := o.b.Update(msg)
	if !b.Active() {
		return nil, cmd
	}
	return o, cmd
}

// Draw paints the browser over the frame.
func (o browserOverlay) Draw(g *grid.Grid) { o.b.Draw(g) }

// Active reports whether the browser is still open.
func (o browserOverlay) Active() bool { return o.b.Active() }

// openPalette shows the command palette with query as its input and returns the
// command that ranks its files, if the query is a file query.
func (m *Model) openPalette(query string) tea.Cmd {
	p := palette.New(palette.Options{
		Query:    query,
		Root:     m.rootDir(),
		Commands: m.commands,
		Buffers:  m.paletteBuffers(),
		Files:    m.files,
		Ranker:   m.ranker,
		Theme:    m.theme,
	})
	m.pushOverlay(paletteOverlay{p: p})
	return p.Init()
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

// paletteOverlay adapts *palette.Palette to Overlay. The palette cannot name
// Overlay, because that would import this package. The palette closes itself
// when a row runs, and the adapter reports that to the stack.
type paletteOverlay struct {
	p *palette.Palette
}

// Update forwards the message to the palette. It returns nil once the palette is
// closed, so the stack pops it.
func (o paletteOverlay) Update(msg tea.Msg) (Overlay, tea.Cmd) {
	cmd := o.p.Update(msg)
	if !o.p.Active() {
		return nil, cmd
	}
	return o, cmd
}

// Draw paints the palette over the frame.
func (o paletteOverlay) Draw(g *grid.Grid) { o.p.Draw(g) }

// Active reports whether the palette is still open.
func (o paletteOverlay) Active() bool { return o.p.Active() }

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
	if err := m.Run(action); err != nil {
		m.notice = failedNotice(menubar.Failed{Label: action, Err: err})
	}
	cmd := m.pending
	m.pending = nil
	if m.quitting {
		m.quitting = false
		return tea.Batch(cmd, tea.Quit)
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
	if err := f.GotoLine(line, col); err != nil {
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
	kp, text := translateKey(k)
	if len(kp.Mods) == 0 && kp.Name == "F6" {
		m.cycleFocus()
		return nil
	}
	if m.tabFocus && len(kp.Mods) == 0 {
		return m.tabKey(kp)
	}
	if len(kp.Mods) == 0 && !m.explorerFocus && m.welcomeVisible() {
		if cmd, ok := m.welcomeKey(kp.Name); ok {
			return cmd
		}
	}
	ctx := KeyContextNormal
	if m.explorerFocus {
		ctx = KeyContextFileExplorer
	}
	if m.keys != nil {
		if cmd, ok := m.keys.Resolve(kp, ctx); ok {
			return m.dispatch(cmd)
		}
	}
	if text != "" && !m.explorerFocus {
		return m.dispatch(Command{Action: "insert_char", Args: map[string]any{"text": text}})
	}
	return nil
}

// clipboardActions are the editor actions the explorer takes over while it has
// focus: copy, cut and paste work on the selected entry, not on the text.
var clipboardActions = map[string]bool{"copy": true, "cut": true, "paste": true}

// dispatch runs one command. Focus and explorer actions are handled here. The
// editor runs everything else.
func (m *Model) dispatch(cmd Command) tea.Cmd {
	// A forced close is the Discard of the unsaved dialog, so it goes to the
	// editor. Any other close asks first.
	if forced, _ := cmd.Args["force"].(bool); cmd.Action != "close_tab" || !forced {
		if c, ok := m.uiAction(cmd.Action); ok {
			return c
		}
	}
	if c, ok := m.explorerAction(cmd.Action); ok {
		return c
	}
	if m.explorerFocus && clipboardActions[cmd.Action] {
		return nil
	}
	res := m.ed.Do(editor.Action(cmd.Action), cmd.Args)
	m.syncViewport()
	if res.Quit {
		return tea.Quit
	}
	return nil
}

// uiAction runs the command when the root handles it. It reports whether it did,
// and the command the action starts, if any. The file browser opens for "open"
// (Ctrl+O and File > Open File) and, in folder mode, for "switch_project". The
// palette opens for "quick_open" (Ctrl+P) and "command_palette" in command mode,
// for "quick_open_files" in file mode and for "quick_open_buffers" in buffer
// mode. "menu_activate" (F10) opens the menu bar.
func (m *Model) uiAction(action string) (tea.Cmd, bool) {
	switch action {
	case "toggle_file_explorer":
		m.toggleExplorer()
	case "focus_file_explorer":
		m.focusExplorer()
	case "focus_editor":
		m.focusEditor()
	case "menu_activate":
		m.syncMenu()
		m.menu = m.menu.Activate()
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
		m.save()
	case "save_as":
		m.askSaveAs(flow{})
	case "quit":
		return m.quitLoop(), true
	default:
		return nil, false
	}
	return nil, true
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
	return expanel.Geometry{Width: l.ex, Bottom: l.exBottom, Focused: m.explorerFocus}
}

// handleClick routes a left press to the part of the frame under it, and a
// middle press on a tab closes the tab. A press on the explorer goes to the
// panel, which answers with its own messages. Presses that have no effect yet
// are dropped here.
func (m *Model) handleClick(msg tea.MouseClickMsg) tea.Cmd {
	mo := tea.Mouse(msg)
	t := m.layout().hit(mo.X, mo.Y)
	if mo.Button == tea.MouseMiddle {
		if t.Kind == hitTab || t.Kind == hitTabClose {
			return m.requestClose(t.Index)
		}
		return nil
	}
	if mo.Button != tea.MouseLeft {
		return nil
	}
	count := m.clicks.press(m.now(), mo.X, mo.Y)
	m.drag = drag{}
	switch t.Kind {
	case hitTab:
		m.focusEditor()
		m.activateTab(t.Index)
	case hitTabClose:
		return m.requestClose(t.Index)
	case hitTabNew:
		return m.dispatch(Command{Action: "new"})
	case hitTabScrollBefore:
		m.scrollTabs(-1)
	case hitTabScrollAfter:
		m.scrollTabs(1)
	case hitWelcomeRow:
		m.focusEditor()
		return m.runWelcome(t.Index)
	case hitExplorerRow, hitExplorerBody:
		m.focusExplorer()
		return m.explorerMouse(msg)
	case hitExplorerTitleClose, hitExplorerTitleReload, hitExplorerGrip:
		return m.explorerMouse(msg)
	case hitStatusPalette:
		return m.openPalette(">")
	case hitEditorText, hitGutter, hitScrollbarTrack:
		m.pressEditor(mo.X, mo.Y, count)
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

// pressEditor is a press in the editor body. The pane maps the cell to what it
// shows: a text or gutter cell places the caret, a count of 2 selects the word
// and a count of 3 the line. A press on the scrollbar track moves the viewport
// to the press, and a press on the thumb grabs it for the drag.
func (m *Model) pressEditor(x, y, count int) {
	m.lastPress = count
	m.focusEditor()
	p, hit, ok := m.paneHit(x, y)
	if !ok {
		return
	}
	switch hit.Kind {
	case editorpane.HitScrollbarThumb:
		m.drag = drag{active: true, thumb: true}
	case editorpane.HitScrollbarTrack:
		m.ed.ScrollTo(p.ScrollTopFor(m.bodyRows(), hit.Row))
		m.drag = drag{active: true, thumb: true}
	case editorpane.HitText, editorpane.HitGutter:
		m.drag = m.placeCaret(hit.Pos, count)
	}
}

// placeCaret moves the caret to pos, or selects the word or the line under it
// for counts 2 and 3. It returns the drag that follows the press, which grows
// the selection by the same unit.
func (m *Model) placeCaret(pos buffer.Pos, count int) drag {
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
	return drag{active: true, unit: editor.UnitChar}
}

// handleDrag moves the grab of a text selection or of the scrollbar thumb. The
// pointer may leave the pane: the thumb follows the row, and the text end
// follows the nearest cell.
func (m *Model) handleDrag(mo tea.Mouse) {
	if !m.drag.active || mo.Button != tea.MouseLeft {
		return
	}
	doc := m.ed.Active()
	if doc == nil {
		return
	}
	l := m.layout()
	w, h := m.paneSize(l)
	if h <= 0 || w <= 0 {
		return
	}
	x := min(max(mo.X-l.editorX, 0), w-1)
	y := min(max(mo.Y-l.bodyTop, 0), h-1)
	p := m.pane(doc)
	if m.drag.thumb {
		m.ed.ScrollTo(p.ScrollTopFor(h, y))
		return
	}
	m.ed.ExtendSelection(p.PosAt(w, h, x, y), m.drag.unit)
}

// handleWheel scrolls the panel's tree over the explorer, and the editor
// viewport over the editor body, by wheelRows per notch. The caret stays where
// it is in both cases.
func (m *Model) handleWheel(msg tea.MouseWheelMsg) tea.Cmd {
	mo := tea.Mouse(msg)
	l := m.layout()
	if mo.Y == 1 && mo.X >= l.editorX {
		// The tab row scrolls the strip one tab per notch.
		switch mo.Button {
		case tea.MouseWheelUp:
			m.scrollTabs(-1)
		case tea.MouseWheelDown:
			m.scrollTabs(1)
		}
		return nil
	}
	switch l.hit(mo.X, mo.Y).Kind {
	case hitExplorerRow, hitExplorerBody, hitExplorerTitleClose, hitExplorerTitleReload, hitExplorerGrip:
		return m.explorerMouse(msg)
	case hitEditorText, hitGutter, hitScrollbarTrack:
		delta := 0
		switch mo.Button {
		case tea.MouseWheelUp:
			delta = -wheelRows
		case tea.MouseWheelDown:
			delta = wheelRows
		}
		if delta != 0 {
			m.ed.ScrollBy(delta)
		}
	}
	return nil
}

// toggleExplorer hides the panel when it has focus or is open, and shows and
// focuses it otherwise. With no explorer, it does nothing.
func (m *Model) toggleExplorer() {
	switch {
	case m.tree == nil:
	case m.explorerFocus:
		m.focusEditor()
	case m.explorerOpen:
		m.explorerOpen = false
		m.syncViewport()
	default:
		m.focusExplorer()
	}
}

// focusExplorer shows the panel when it is hidden and gives it focus. The
// legacy TUI also reveals the active file here. Reveal needs the active path,
// which the Document view does not carry yet.
func (m *Model) focusExplorer() {
	if m.tree == nil {
		return
	}
	if !m.explorerOpen {
		m.explorerOpen = true
		m.syncViewport()
	}
	m.tabFocus = false
	m.explorerFocus = true
	m.followSelection()
}

// focusEditor gives keyboard focus back to the editor. The panel stays visible.
func (m *Model) focusEditor() {
	m.explorerFocus = false
	m.tabFocus = false
}

// The places keyboard focus can be, in F6 order. The menu is open while it has
// focus, and the explorer is skipped while it is hidden.
type focusStage int

const (
	stageEditor focusStage = iota
	stageMenu
	stageTabs
	stageExplorer
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
	}
	return stageEditor
}

// cycleFocus moves keyboard focus to the next stage: menu, tabs, explorer,
// editor, and back to the menu. F6 sends it.
func (m *Model) cycleFocus() {
	order := []focusStage{stageMenu, stageTabs, stageExplorer, stageEditor}
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
	default:
		m.focusEditor()
	}
}

// tabKey handles a key while the tab strip has focus: Left and Right step
// through the tabs, and Enter, Down or Escape returns focus to the editor. The
// other keys without a modifier are ignored, so they do not type into the text.
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
// a grip or scrollbar gesture in progress ends with it.
func (m *Model) closeExplorer() {
	m.explorerOpen = false
	m.explorerFocus = false
	if m.tree != nil {
		m.explorerPanel = expanel.New(m.tree, m.styles, m.now)
	}
	m.syncViewport()
}

// followSelection scrolls the tree so the selected row is inside the panel body.
// The tree does not know the body height, so the model moves the offset by the
// difference, and the tree clamps it.
func (m *Model) followSelection() {
	if m.tree == nil || !m.explorerOpen {
		return
	}
	sel, ok := m.tree.Selected()
	if !ok {
		return
	}
	idx := slices.IndexFunc(m.tree.Rows(), func(r explorer.Row) bool { return r.Path == sel.Path })
	if idx < 0 {
		return
	}
	off, body := m.tree.ScrollOffset(), m.bodyRows()
	switch {
	case idx < off:
		m.tree.ScrollBy(idx - off)
	case idx >= off+body:
		m.tree.ScrollBy(idx - off - body + 1)
	}
}

// bodyRows is the number of editor body rows in the current frame.
func (m *Model) bodyRows() int {
	l := m.layout()
	return max(l.bodyBottom-l.bodyTop+1, 0)
}

// syncViewport tells the editor how big its pane is: the body rows, and the
// columns right of the gutter.
func (m *Model) syncViewport() {
	if m.width == 0 {
		return
	}
	l := m.layout()
	m.ed.SetViewport(l.bodyBottom-l.bodyTop+1, l.textW)
}

// layoutInput gathers the state the geometry depends on.
func (m *Model) layoutInput() layoutInput {
	in := layoutInput{
		explorerFocus:    m.explorerFocus,
		explorerOverride: m.explorerOverride,
		tabs:             m.ed.Tabs(),
		tabFirst:         m.tabFirst,
		welcome:          m.welcomeVisible(),
		recents:          m.recents,
		status:           m.statusView(),
	}
	if m.findOpen() {
		in.findRows = m.findBar.Rows()
	}
	if doc := m.ed.Active(); doc != nil {
		in.lines = doc.LineCount()
	}
	if m.explorerOpen && m.tree != nil {
		in.explorerOpen = true
		in.explorerRows = len(m.tree.Rows()) - m.tree.ScrollOffset()
	}
	return in
}

// layout is the geometry of the current frame. Painting and mouse hit testing
// both read it.
func (m *Model) layout() layout {
	return computeLayout(m.width, m.height, m.layoutInput())
}

// statusView gathers the status row contents for the active document. It runs
// on every frame, so it does no lexer or text work: the encoding comes from the
// document and the language is memoized by file name.
func (m *Model) statusView() statusView {
	s := statusView{message: m.ed.Status(), eol: "LF", encoding: "UTF-8", language: "Text"}
	if m.notice != "" {
		s.message = m.notice
	}
	if doc := m.ed.Active(); doc != nil {
		c := doc.Cursor()
		s.position = "Ln " + strconv.Itoa(c.Line+1) + ", Col " + strconv.Itoa(c.Col+1)
		s.lines = doc.LineCount()
		s.encoding = doc.Encoding()
		s.language = m.languageFor(doc.Name())
	}
	return s
}

// noDocumentNotice is the status text for an action that needs an open tab.
const noDocumentNotice = "No open document. Press Ctrl+N to create one."

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
	v.MouseMode = tea.MouseModeAllMotion
	if x, y, ok := m.caret(); ok {
		v.Cursor = tea.NewCursor(x, y)
	}
	return v
}

// paint draws the frame into a new grid. The editor pane fills the body to the
// right of the explorer. Each other band is painted from the layout runs, so the
// hit map and the paint cannot drift apart. The open overlays are drawn last.
func (m *Model) paint() *grid.Grid {
	g := grid.New(m.width, m.height)
	l := m.layout()
	if l.gated {
		g.Put(0, 0, g.Width(), gateText, grid.Style{})
		return g
	}
	body := style.Cell(m.styles.Editor)
	for y := range g.Height() {
		g.Fill(0, g.Width(), y, body)
	}
	m.paintEditor(g, l)
	drawTabStrip(g, m.styles, l.editorX, l.strip)
	if l.explorerOpen {
		m.explorerPanel.Draw(g, m.explorerGeometry(l))
	}
	m.paintStatus(g, l)
	// The menu bar comes last among the frame bands, so an open dropdown covers
	// the rows under it. The overlays are drawn over the menu.
	m.menu.Paint(g)
	for _, o := range m.overlays {
		o.Draw(g)
	}
	return g
}

// paneSize is the size of the editor pane in cells: the columns right of the
// explorer, and the body rows.
func (m *Model) paneSize(l layout) (w, h int) {
	return max(l.w-l.editorX, 0), m.bodyRows()
}

// pane is the editor pane for doc in the current frame.
func (m *Model) pane(doc Document) editorpane.Pane {
	return editorpane.Pane{Doc: doc, Top: doc.Top(), Styles: m.panes}
}

// paneHit maps a frame cell to what the editor pane shows there. ok is false
// when there is no document.
func (m *Model) paneHit(x, y int) (p editorpane.Pane, hit editorpane.Hit, ok bool) {
	doc := m.ed.Active()
	if doc == nil {
		return p, hit, false
	}
	l := m.layout()
	w, h := m.paneSize(l)
	p = m.pane(doc)
	return p, p.Hit(w, h, x-l.editorX, y-l.bodyTop), true
}

// paintEditor paints the editor pane into the body, to the right of the
// explorer. The pane draws the visible lines, the gutter, the text, the
// selection, the change marks and the scrollbar. With no tab open, the welcome
// screen is painted instead.
func (m *Model) paintEditor(g *grid.Grid, l layout) {
	doc := m.ed.Active()
	if doc == nil {
		paintWelcome(g, m.styles, l, m.welcomeSel)
		return
	}
	w, h := m.paneSize(l)
	if w <= 0 || h <= 0 {
		return
	}
	editorpane.Blit(g, l.editorX, l.bodyTop, m.pane(doc).Render(w, h))
}

// caret is the terminal cursor position in frame cells. ok is false when the
// editor does not have the focus, an overlay is open, or the caret is outside
// the body.
func (m *Model) caret() (x, y int, ok bool) {
	if m.explorerFocus {
		return 0, 0, false
	}
	// The find bar, when it is the top overlay, shows the caret in its search row.
	if n := len(m.overlays); n > 0 {
		if fo, isFind := m.overlays[n-1].(findOverlay); isFind {
			return fo.b.Cursor(m.width, m.height)
		}
		// A dialog with a text field reports its caret the same way.
		if c, ok := m.overlays[n-1].(interface {
			Cursor(w, h int) (int, int, bool)
		}); ok {
			return c.Cursor(m.width, m.height)
		}
		return 0, 0, false
	}
	doc := m.ed.Active()
	if doc == nil || m.tabFocus {
		return 0, 0, false
	}
	l := m.layout()
	if l.gated {
		return 0, 0, false
	}
	w, h := m.paneSize(l)
	cx, cy, ok := m.pane(doc).Caret(w, h)
	if !ok {
		return 0, 0, false
	}
	return l.editorX + cx, l.bodyTop + cy, true
}

// overlayInput reports whether msg is input that the top overlay takes: a key
// or a mouse message, while an overlay is open.
func (m *Model) overlayInput(msg tea.Msg) bool {
	if len(m.overlays) == 0 {
		return false
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
// is no longer active is popped. Esc pops the top overlay and is not forwarded.
func (m *Model) routeOverlay(msg tea.Msg) tea.Cmd {
	if k, ok := msg.(tea.KeyPressMsg); ok && tea.Key(k).Code == tea.KeyEscape {
		m.popOverlay()
		return nil
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

// paintStatus fills the status row and writes its segments.
func (m *Model) paintStatus(g *grid.Grid, l layout) {
	bar := style.Cell(m.styles.Status)
	palette := style.Cell(m.styles.StatusPalette)
	g.Fill(0, g.Width(), l.statusY, bar)
	for _, r := range l.status {
		st := bar
		if r.kind == hitStatusPalette {
			st = palette
		}
		g.Put(r.x, l.statusY, g.Width(), r.text, st)
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
	m.setTabFirst(m.layout().strip.first + delta)
}

// setTabFirst sets the scroll position of the tab strip. The stored value is the
// clamped one, so a scroll back starts from the end the strip shows.
func (m *Model) setTabFirst(first int) {
	m.tabFirst = first
	m.tabFirst = m.layout().strip.first
}

// revealActiveTab scrolls the tab strip only as far as needed to show the active
// tab in full, or from its start when it is wider than the strip.
func (m *Model) revealActiveTab() {
	i := m.activeTab()
	if i < 0 {
		return
	}
	l := m.layout()
	m.setTabFirst(revealedFirst(m.ed.Tabs(), l.editorX, l.w, l.strip.first, i))
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
		if err := t.CloseTab(index, false); err != nil {
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
		return tea.Quit
	}
	dirty := t.Quit(false)
	if len(dirty) == 0 {
		return tea.Quit
	}
	d := dirty[0]
	t.ActivateTab(d.Index)
	m.syncViewport()
	m.askUnsaved(flow{then: nextQuit, index: d.Index}, d.Name)
	return nil
}

// save runs the save action. An untitled buffer has no path, so the Save As
// prompt opens instead.
func (m *Model) save() {
	res := m.ed.Do("save", nil)
	if errors.Is(res.Err, editor.ErrNoPath) {
		m.askSaveAs(flow{})
	}
}

// askUnsaved opens the unsaved-changes dialog for the tab called name.
func (m *Model) askUnsaved(f flow, name string) {
	if m.dialogs == nil {
		m.notice = "Unsaved changes in " + name
		return
	}
	m.pushOverlay(m.dialogs.Unsaved(name, unsavedTag{f: f}))
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
	m.pushOverlay(m.dialogs.SaveAs(prefill, saveAsTag{f: f}))
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
		if res := m.ed.Do("save", nil); res.Err != nil {
			return nil
		}
		return m.afterSave(f)
	case dialog.ChoiceDiscard:
		if err := t.CloseTab(f.index, true); err != nil {
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
			if err := t.CloseTab(f.index, false); err != nil {
				m.notice = err.Error()
			}
			m.syncViewport()
		}
	case nextQuit:
		return m.quitLoop()
	}
	return nil
}

// saveAs writes the active document to path. The editor refuses a path that
// another tab has open, and an existing file when force is false. The Save As
// dialog asks about an existing file before it sends Replace, so the refusal
// only shows up when the dialog could not check the disk. A success refreshes
// the explorer and goes on with the flow.
func (m *Model) saveAs(path string, force bool, f flow) tea.Cmd {
	args := map[string]any{"path": path, "force": force}
	res := m.ed.Do("save_as", args)
	if res.Err != nil {
		m.notice = res.Err.Error()
		return nil
	}
	m.reloadTree()
	return m.afterSave(f)
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
	// Shift stays on named keys and shifted letters. A shifted symbol is bound by its own character.
	if shift && (named || unicode.IsLetter(k.Code)) {
		kp.Mods = append(kp.Mods, "shift")
	}
	var text string
	if k.Text != "" && k.Mod&(tea.ModCtrl|tea.ModAlt) == 0 {
		text = k.Text
	}
	return kp, text
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
