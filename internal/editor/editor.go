// Package editor holds the state of an editing session: the open documents,
// the active tab, the clipboard, and the status message. All changes go
// through Do, which runs one named action (the same names the keymap uses).
package editor

import (
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/tbxark/gvim/internal/buffer"
)

// Action is the name of an editor command, such as "move_left" or "save".
// Names match the action vocabulary in Fresh's keymaps.
type Action string

// ErrUnknownAction is returned by Do for an action name the editor does not implement.
var ErrUnknownAction = errors.New("unknown action")

// ErrUnsaved is returned by close_tab, CloseTab and quit when a buffer has
// unsaved edits. Pass the argument force=true to discard them.
var ErrUnsaved = errors.New("unsaved changes")

// ErrNoDocument is returned by Do and by the document methods when no tab is
// open (Empty reports true). The status message says the same.
var ErrNoDocument = errors.New("no open document")

// Options configures a new Editor.
type Options struct {
	// Rows and Cols give the size of the editor pane. Zero values use 24 and 80.
	Rows, Cols int
	// Empty starts the editor with no open document, which is the state of
	// gvim started without files. The UI then shows its welcome screen.
	Empty bool
	// UseTabs makes Tab insert a literal tab. When it is false, Tab inserts
	// TabSize spaces, and Shift+Tab removes one indent unit. The zero value is false, so a zero Options inserts
	// spaces. The app should set UseTabs to the inverse of insert_spaces.
	UseTabs bool
	// TabSize is the columns per tab stop, and the width of one indent unit
	// when UseTabs is false. Zero means 4.
	TabSize int
	// AutoIndent makes insert_newline indent the new line like the caret line, one
	// level deeper after a line that ends in { [ ( or :, and makes a typed closing
	// bracket re-indent its line. The zero value keeps the line start empty.
	AutoIndent bool
	// TrimTrailingWhitespace strips trailing spaces and tabs from every line on
	// save. The buffer is changed to match, as one undo step.
	TrimTrailingWhitespace bool
	// EnsureFinalNewline ends the saved text with a line break, and the buffer
	// gets it too. An empty text stays empty.
	EnsureFinalNewline bool
	// DefaultLineEnding is "LF" or "CRLF" for a document that has no file to
	// read the ending from. Empty means "LF".
	DefaultLineEnding string
	// AutoSaveInterval is how often AutoSave saves the modified documents. Zero
	// or a negative value turns auto-save off. See AutoSaveDue.
	AutoSaveInterval time.Duration
	// Root is the workspace root that copy_relative_file_path measures from. Empty
	// means no root is known, and the relative copy then gives the absolute path.
	Root string
	// SessionDir is the directory of the saved session, which InterruptedSaves reads.
	// Empty means there is no session, and InterruptedSaves lists nothing.
	SessionDir string

	// The options below come from the Fresh editor.* config options. The zero value of
	// each is "off" or 0, so a plain Options{} switches them off; the app sets them from
	// the config, whose defaults are Fresh's. Except AutoClose, AutoSurround and the large file
	// options, none of them changes behaviour yet: see the contract for the consumer of each. The two that are not plain values, the
	// language resolver and the ephemeral patterns, are set with SetLanguageSettings and
	// SetEphemeralFilePatterns so that Options stays comparable.

	// AutoClose (editor.auto_close, Fresh default true) makes typing an opener insert its
	// closer. AutoSurround (editor.auto_surround, default true) makes it wrap a
	// selection. A language can switch either off; SettingsFor gives the result.
	AutoClose    bool
	AutoSurround bool
	// VirtualSpace is editor.virtual_space: VirtualSpaceOff (default), VirtualSpaceBlock or
	// VirtualSpaceOn. Empty reads as off.
	VirtualSpace string
	// ScrollOffset is editor.scroll_offset (default 3): the lines kept visible above and
	// below the caret. Zero keeps none.
	ScrollOffset int
	// AutoReadOnly is editor.auto_read_only (default true): open files that are not
	// writable, or that sit in a library folder, as read-only.
	AutoReadOnly bool
	// SnapshotInterval is editor.snapshot_interval (default 100): the edits between undo
	// snapshots.
	SnapshotInterval int
	// ConfirmQuit is editor.confirm_quit (default false): quit asks even when no buffer
	// has unsaved edits.
	ConfirmQuit bool
	// AutoCreateEmptyBufferOnLastBufferClose is
	// editor.auto_create_empty_buffer_on_last_buffer_close (default true): closing the
	// last tab opens an empty buffer instead of the welcome screen.
	AutoCreateEmptyBufferOnLastBufferClose bool
	// RecoveryEnabled is editor.recovery_enabled (default true), and
	// AutoRecoverySaveIntervalSecs is editor.auto_recovery_save_interval_secs
	// (default 2): the crash-recovery copies of modified buffers.
	RecoveryEnabled              bool
	AutoRecoverySaveIntervalSecs int
	// AutoRevertPollIntervalMS is editor.auto_revert_poll_interval_ms (default 2000): how
	// often an open file is checked for changes on disk when no watcher event arrives.
	AutoRevertPollIntervalMS int
	// LargeFileThresholdBytes is editor.large_file_threshold_bytes (default 10485760):
	// a file of at least this many bytes opens in large file mode, where the text stays
	// on disk and only an index of its lines is kept in memory (see large.go). Zero
	// turns large file mode off. Fresh differs here: its zero makes every file large.
	// EstimatedLineLength is editor.estimated_line_length (default 80), the line
	// length that the line count estimate assumes before enough lines are known.
	LargeFileThresholdBytes int
	EstimatedLineLength     int
	// LargeFileMode enables large file mode. It is not a config key: it is the
	// switch that the app always sets (editorOptions), and that the editor tests
	// leave off unless they test large file mode. Large file mode needs it and a
	// positive LargeFileThresholdBytes; a file of at least that size opens large.
	LargeFileMode bool

	// largeGeom, openSrc, fullLoadMax and indexBatch are for tests. A zero largeGeom
	// means the defaults of buffer.LargeOptions, a nil openSrc buffer.OpenFileSource,
	// a zero fullLoadMax maxFullLoad and a zero indexBatch indexBatchBytes. openSrc is
	// a pointer so that Options stays comparable.
	largeGeom   buffer.LargeOptions
	openSrc     *sourceOpener
	fullLoadMax int64
	indexBatch  int64
}

// Result is what Do reports back to the caller.
type Result struct {
	// Err is non-nil when the action failed. The message is also in Status.
	Err error
	// Quit is true after the quit action succeeded; the caller should exit.
	Quit bool
	// Text is the text the action hands back, or "". copy_file_path and
	// copy_relative_file_path set it to the path they copied, which the caller may
	// also write to the terminal clipboard.
	Text string
}

// Tab describes one open document for the tab strip.
type Tab struct {
	// Name is the label for the tab strip. A preview tab carries the suffix
	// " (preview)" after its file name.
	Name     string
	Path     string
	Modified bool
	Active   bool
	// Preview is true for a tab opened with OpenPreview.
	Preview bool
	// Language is the lexer name set with SetLanguage, or "".
	Language string
}

// Dirty describes a tab with unsaved edits, as Quit lists it.
type Dirty struct {
	// Index is the tab index, for ActivateTab and CloseTab.
	Index int
	Name  string
	Path  string
	// Untitled is true for a buffer with no file name. It must go through Save As.
	Untitled bool
}

// Editor is one editing session. Use New to create one; the zero value is not usable.
type Editor struct {
	docs      []*Document
	active    int
	clipboard string
	rows      int
	cols      int
	quit      bool
	lastKind  string
	// untitledSeq is the last N handed out as Untitled-N in this session.
	untitledSeq int
	// emptyStatus is the status message while no tab is open.
	emptyStatus string
	// opts is the configuration New was given, with TabSize defaulted.
	opts Options
	// rowMap reports where a line wraps (SetRowMapper).
	rowMap RowMapper
	// languages resolves the per-language settings (SetLanguageSettings).
	languages SettingsResolver
	// inString tells whether a position is inside a string literal (SetStringContext).
	inString StringContext
	// nonCode masks comments and strings for the indent rules (SetNonCodeContext).
	nonCode NonCodeContext
	// ephemeral holds editor.ephemeral_file_patterns (SetEphemeralFilePatterns).
	ephemeral []string
	// extras is the state of the marks, navigation and dabbrev actions (marks.go).
	extras extraState
	// autoSaved is the time of the last AutoSave. It is zero before the first.
	autoSaved time.Time
	// prevDoc is the document that was active before the current one, for
	// switch_to_previous_tab. It is a pointer, so a closed tab is not found.
	prevDoc *Document
	// formatters maps a language name or file extension to its formatter command. See
	// SetFormatters.
	formatters map[string]string
	// resultText is the text the running action hands back in Result.Text. Do clears
	// it before the action runs.
	resultText string
}

// New returns an Editor. Unless opts.Empty is set, it holds one empty
// placeholder buffer named "[No Name]", which is replaced by the first New or
// OpenFile. With opts.Empty it holds no document at all (see Empty).
func New(opts Options) *Editor {
	rows, cols := opts.Rows, opts.Cols
	if rows <= 0 {
		rows = 24
	}
	if cols <= 0 {
		cols = 80
	}
	if opts.TabSize <= 0 {
		opts.TabSize = 4
	}
	e := &Editor{rows: rows, cols: cols, opts: opts}
	if !opts.Empty {
		d := newDocument("", "")
		d.eol = e.defaultEOL()
		e.docs = []*Document{d}
	}
	return e
}

// defaultEOL is the line ending of a document with no file to read it from:
// CRLF when Options.DefaultLineEnding says so, LF otherwise.
func (e *Editor) defaultEOL() string {
	if e.opts.DefaultLineEnding == eolCRLF {
		return eolCRLF
	}
	return eolLF
}

// Empty reports whether no tab is open. Active returns nil then, Tabs returns
// nothing, and the actions that need a document return ErrNoDocument.
func (e *Editor) Empty() bool {
	return len(e.docs) == 0
}

// SetViewport records the size of the editor pane and keeps the cursor visible.
func (e *Editor) SetViewport(rows, cols int) {
	e.rows = max(rows, 1)
	e.cols = max(cols, 1)
	e.ensureVisible(e.Active())
}

// OpenFile opens path in a new tab and makes it active. If path is already open
// in a tab, that tab is activated instead and its text is kept, not reloaded.
// A missing file opens as an empty buffer bound to path, which is created on
// save. A file that is not valid UTF-8 is decoded in its detected encoding, and
// errors wrap ErrNotDecodable, ErrEmptyPath or the underlying file error. An
// untouched placeholder or untitled buffer is replaced rather than kept. The
// document's path is the absolute form of path. A file of at
// least Options.LargeFileThresholdBytes opens in large file mode (see large.go) when
// Options.LargeFileMode is set. If it is not UTF-8, the error is an
// ErrLargeNeedsFullLoad, and the caller may load it with LoadFullFunc.
func (e *Editor) OpenFile(path string) error {
	if path == "" {
		err := fmt.Errorf("open: %w", ErrEmptyPath)
		e.setStatus(err.Error())
		return err
	}
	path, err := absPath(path)
	if err != nil {
		e.setStatus(err.Error())
		return err
	}
	if i := e.pathIndex(path); i >= 0 {
		e.docs[i].preview = false
		if i != e.active {
			e.pushJump()
		}
		e.switchTo(i)
		e.setStatus("Opened " + e.docs[i].Name())
		return nil
	}
	d, err := loadDocument(path, e.loadSpec())
	if err != nil {
		e.setStatus(err.Error())
		return err
	}
	e.OpenLoaded(d)
	return nil
}

// Active returns the document in the active tab, or nil when no tab is open.
func (e *Editor) Active() *Document {
	if len(e.docs) == 0 {
		return nil
	}
	return e.docs[e.active]
}

// DocAt returns the document of tab i, or nil when there is no tab i. Unlike
// Active it does not change the active tab, so a split pane can show a tab that is
// not active.
func (e *Editor) DocAt(i int) *Document {
	if i < 0 || i >= len(e.docs) {
		return nil
	}
	return e.docs[i]
}

// Tabs returns the open documents in tab order.
func (e *Editor) Tabs() []Tab {
	tabs := make([]Tab, len(e.docs))
	for i, d := range e.docs {
		name := d.Name()
		if d.preview {
			name += previewSuffix
		}
		tabs[i] = Tab{
			Name:     name,
			Path:     d.Path,
			Modified: d.Modified(),
			Active:   i == e.active,
			Preview:  d.preview,
			Language: d.language,
		}
	}
	return tabs
}

// Status returns the message for the active tab. Each tab keeps its own message,
// so a switch of tabs shows that tab's message, or "" if it has none. With no
// tab open it is the message of the empty editor.
func (e *Editor) Status() string {
	if d := e.Active(); d != nil {
		return d.status
	}
	return e.emptyStatus
}

// setStatus sets the message of the active tab, or of the empty editor.
func (e *Editor) setStatus(msg string) {
	if d := e.Active(); d != nil {
		d.status = msg
		return
	}
	e.emptyStatus = msg
}

// SetLanguage records the lexer name of the active tab. The UI resolves the
// name (internal/syntax) and sets it, because the lexer depends on the file
// name. An empty name clears it. Renaming the tab clears it. It does nothing
// when no tab is open.
func (e *Editor) SetLanguage(name string) {
	if d := e.Active(); d != nil {
		d.language = name
	}
}

// Save writes the active document to its path and marks it saved. An untitled
// buffer returns ErrNoPath, and the caller should run Save As, which calls
// SaveAs. Errors wrap the file error and are also in the tab's status. A read-only
// document returns ErrReadOnly, because a save would change its file. With no tab
// open it returns ErrNoDocument.
func (e *Editor) Save() error {
	p, err := e.PlanSave()
	if err != nil {
		return err
	}
	return e.ApplySavePlan(p, p.Run())
}

// Quit reports the tabs with unsaved edits, in tab order. If there are none, or
// force is true, the editor is marked for quit: Quitting reports true, and the
// caller should exit. If a list is returned, nothing changes. The caller runs
// one unsaved-changes dialog per tab (ActivateTab, then Save or CloseTab with
// force), then calls Quit again. Cancel stops the quit.
func (e *Editor) Quit(force bool) []Dirty {
	if !force {
		if dirty := e.dirtyTabs(); len(dirty) > 0 {
			return dirty
		}
	}
	e.quit = true
	return nil
}

// Quitting reports whether a quit has been granted by Quit or the quit action.
func (e *Editor) Quitting() bool {
	return e.quit
}

// dirtyTabs lists the tabs with unsaved edits, in tab order.
func (e *Editor) dirtyTabs() []Dirty {
	var dirty []Dirty
	for i, d := range e.docs {
		if d.Modified() {
			dirty = append(dirty, Dirty{Index: i, Name: d.Name(), Path: d.Path, Untitled: d.Untitled()})
		}
	}
	return dirty
}

// Actions returns every action name Do understands, sorted.
func Actions() []Action {
	names := make([]Action, 0, len(handlers))
	for a := range handlers {
		names = append(names, a)
	}
	slices.Sort(names)
	return names
}

// Do runs action a. args carries action parameters: insert_char takes "text",
// and close_tab and quit take "force" (bool). Do never panics on bad arguments;
// it reports them through Result.Err and Status.
func (e *Editor) Do(a Action, args map[string]any) Result {
	h, ok := handlers[a]
	if !ok {
		err := fmt.Errorf("%w: %s", ErrUnknownAction, a)
		e.setStatus(err.Error())
		return Result{Err: err}
	}
	d := e.Active()
	if d == nil && !worksWithoutDocument[a] {
		err := ErrNoDocument
		e.setStatus(err.Error())
		return Result{Err: err}
	}
	kind := typingKind(a)
	if d != nil && (kind == "" || kind != e.lastKind) {
		d.Buffer.Break()
	}
	e.lastKind = kind

	e.resultText = ""
	var mark jumpMark
	if a != "navigate_back" && a != "navigate_forward" {
		mark = e.markJump()
	}
	// A refusal belongs to the action that caused it, so none from before is left over.
	// A refused action in a large document puts the carets back (see takeRefusal).
	var carets *cursorSnapshot
	if d != nil && d.large != nil {
		_ = d.takeRefusal()
		carets = d.snapCursors()
	}
	err := h(e, args)
	if carets != nil && e.Active() == d {
		if refused := d.takeRefusal(); refused != nil {
			d.restoreCursors(carets)
			if err == nil {
				err = refused
			}
		}
	}
	r := e.finish(err)
	e.recordJump(mark)
	r.Text = e.resultText
	return r
}

// finish ends an action that ran with err. An edited preview tab becomes
// permanent, err goes to the status, and the caret stays visible. Do and EndShell
// both end this way.
func (e *Editor) finish(err error) Result {
	if d := e.Active(); d != nil && d.preview && d.Modified() {
		// An edit makes a preview tab permanent.
		d.preview = false
	}
	if d := e.Active(); d != nil && d.large != nil {
		// A large document refuses an edit or a read it cannot do. The reason is the
		// action's error unless the action has one already.
		if refused := d.takeRefusal(); err == nil {
			err = refused
		}
	}
	if err != nil {
		e.setStatus(err.Error())
	}
	e.ensureVisible(e.Active())
	return Result{Err: err, Quit: e.quit}
}

// worksWithoutDocument lists the actions that run with no tab open. Every other
// action needs a document, and Do refuses it with ErrNoDocument.
var worksWithoutDocument = map[Action]bool{
	"new":                      true,
	"noop":                     true,
	"quit":                     true,
	"save":                     true,
	"save_as":                  true,
	"close_tab":                true,
	"next_buffer":              true,
	"prev_buffer":              true,
	"goto_line":                true,
	"remove_secondary_cursors": true,
}

// typingKind groups consecutive edits that should share one undo step.
// Any other action returns "" and always ends the current step.
func typingKind(a Action) string {
	switch a {
	case "insert_char", "insert_newline", "insert_tab":
		return "insert"
	case "delete_backward":
		return "backspace"
	case "delete_forward":
		return "delete"
	}
	return ""
}

// switchTo makes tab i active and resets the typing-group state. The tab that was
// active becomes the previous tab, for switch_to_previous_tab.
func (e *Editor) switchTo(i int) {
	// closeAt can switch away from a tab that was the last one, so e.active may be
	// past the end here.
	inRange := func(j int) bool { return j >= 0 && j < len(e.docs) }
	if inRange(e.active) && inRange(i) && e.docs[i] != e.docs[e.active] {
		e.prevDoc = e.docs[e.active]
	}
	e.active = i
	e.lastKind = ""
}

// ensureVisible scrolls the document so its cursor line is inside the pane,
// keeping the scroll-off margin from the pane edge once the caret has moved. It
// does nothing when d is nil.
func (e *Editor) ensureVisible(d *Document) {
	if d == nil {
		return
	}
	line := d.Cursor.Line
	margin := 0
	if d.Cursor != d.seen {
		margin = e.scrollMargin()
	}
	d.seen = d.Cursor
	if line < d.Top+margin {
		d.Top = line - margin
	}
	if line >= d.Top+e.rows-margin {
		top := line - e.rows + 1 + margin
		if margin > 0 {
			// Do not scroll past the end just to keep the margin, and never scroll
			// up when the view already shows more than the end.
			top = max(min(top, d.Buffer.Lines()-e.rows), line-e.rows+1, d.Top)
		}
		d.Top = top
	}
	d.Top = max(d.Top, 0)
}

// newUntitled returns an empty buffer named Untitled-N, where N is the next
// number of this session. Numbers start at 1.
func (e *Editor) newUntitled() *Document {
	e.untitledSeq++
	d := newDocument("", "")
	d.untitled = e.untitledSeq
	d.eol = e.defaultEOL()
	return d
}

// pristineUntitled reports whether the only open document is an untouched
// buffer with no path: no edits and no text.
func (e *Editor) pristineUntitled() bool {
	if len(e.docs) != 1 {
		return false
	}
	d := e.docs[0]
	return d.Path == "" && !d.Modified() && d.Buffer.Empty()
}
