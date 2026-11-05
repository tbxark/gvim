// Package editor holds the state of an editing session: the open documents,
// the active tab, the clipboard, and the status message. All changes go
// through Do, which runs one named action (the same names the keymap uses).
package editor

import (
	"errors"
	"fmt"
	"slices"
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
}

// Result is what Do reports back to the caller.
type Result struct {
	// Err is non-nil when the action failed. The message is also in Status.
	Err error
	// Quit is true after the quit action succeeded; the caller should exit.
	Quit bool
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
	e := &Editor{rows: rows, cols: cols}
	if !opts.Empty {
		e.docs = []*Document{newDocument("", "")}
	}
	return e
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
// save. Errors wrap ErrNotUTF8, ErrEmptyPath or the underlying file error. An
// untouched placeholder or untitled buffer is replaced rather than kept.
func (e *Editor) OpenFile(path string) error {
	if path == "" {
		err := fmt.Errorf("open: %w", ErrEmptyPath)
		e.setStatus(err.Error())
		return err
	}
	if i := e.pathIndex(path); i >= 0 {
		e.docs[i].preview = false
		e.switchTo(i)
		e.setStatus("Opened " + e.docs[i].Name())
		return nil
	}
	d, err := readDocument(path)
	if err != nil {
		e.setStatus(err.Error())
		return err
	}
	if e.pristineUntitled() {
		// The untouched startup buffer is replaced, not kept as a tab.
		e.docs[0] = d
		e.switchTo(0)
	} else {
		e.docs = append(e.docs, d)
		e.switchTo(len(e.docs) - 1)
	}
	e.setStatus("Opened " + d.Name())
	return nil
}

// Active returns the document in the active tab, or nil when no tab is open.
func (e *Editor) Active() *Document {
	if len(e.docs) == 0 {
		return nil
	}
	return e.docs[e.active]
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
// SaveAs. Errors wrap the file error and are also in the tab's status. With no
// tab open it returns ErrNoDocument.
func (e *Editor) Save() error {
	d := e.Active()
	if d == nil {
		err := ErrNoDocument
		e.setStatus(err.Error())
		return err
	}
	if err := d.save(); err != nil {
		e.setStatus(err.Error())
		return err
	}
	e.setStatus("Saved")
	return nil
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

	err := h(e, args)
	if d = e.Active(); d != nil && d.preview && d.Modified() {
		// An edit makes a preview tab permanent.
		d.preview = false
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

// switchTo makes tab i active and resets the typing-group state.
func (e *Editor) switchTo(i int) {
	e.active = i
	e.lastKind = ""
}

// ensureVisible scrolls the document so its cursor line is inside the pane. It
// does nothing when d is nil.
func (e *Editor) ensureVisible(d *Document) {
	if d == nil {
		return
	}
	line := d.Cursor.Line
	if line < d.Top {
		d.Top = line
	}
	if line >= d.Top+e.rows {
		d.Top = line - e.rows + 1
	}
	d.Top = max(d.Top, 0)
}

// newUntitled returns an empty buffer named Untitled-N, where N is the next
// number of this session. Numbers start at 1.
func (e *Editor) newUntitled() *Document {
	e.untitledSeq++
	d := newDocument("", "")
	d.untitled = e.untitledSeq
	return d
}

// pristineUntitled reports whether the only open document is an untouched
// buffer with no path: no edits and no text.
func (e *Editor) pristineUntitled() bool {
	if len(e.docs) != 1 {
		return false
	}
	d := e.docs[0]
	return d.Path == "" && !d.Modified() && d.Buffer.Text() == ""
}
