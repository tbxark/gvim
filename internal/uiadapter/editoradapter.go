package uiadapter

import (
	"fmt"
	"slices"
	"unicode/utf8"

	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/internal/highlight"
	"github.com/tbxark/gvim/internal/tui"
	"github.com/tbxark/gvim/search"
)

// Editor is the tui.EditorState view of *editor.Editor.
type Editor struct {
	ed    *editor.Editor
	spans *highlight.Cache
	// others holds the span cache of each tab that is not active, so that a split
	// pane showing such a tab lexes it once per edit. It is nil in a zero adapter,
	// and the other tabs then have no spans.
	others map[*editor.Document]*highlight.Cache
	// saves records whether a planned auto-save is writing. See autosave.go. It is
	// nil in a zero adapter, and then nothing is refused.
	saves *autoSaveGate
}

// SetViewport sizes the editor pane.
func (a *Editor) SetViewport(rows, cols int) {
	a.ed.SetViewport(rows, cols)
}

// Do runs one editor action. The UI also sends actions whose feature is not
// built yet (the palette, the menu and the find bar). Those come back as
// ErrUnknownAction without reaching the editor, so the status row keeps the
// editor's own message. Every action may change the text, so the syntax spans
// of the active document are marked stale first. A save is refused while a
// planned write is pending, because its result is not yet recorded.
func (a *Editor) Do(act editor.Action, args map[string]any) editor.Result {
	a.edited()
	if !slices.Contains(editor.Actions(), act) {
		return editor.Result{Err: fmt.Errorf("%w: %s", editor.ErrUnknownAction, act)}
	}
	if a.saves != nil && a.saves.writing && (act == "save" || act == "save_as" || act == "save_all") {
		return editor.Result{Err: errAutoSaveWriting}
	}
	return a.ed.Do(act, args)
}

// PlanSave prepares the explicit save of the active document, as the editor's PlanSave
// does. The plan is refused while a planned write is pending, as Do refuses a save. A
// plan that is made marks the gate writing until ApplySavePlan records its result.
func (a *Editor) PlanSave() (editor.SavePlan, error) {
	if a.writePending() {
		return editor.SavePlan{}, errAutoSaveWriting
	}
	p, err := a.ed.PlanSave()
	a.markWriting(err)
	return p, err
}

// PlanSaveAs prepares the Save As of the active document. It is refused and gated as
// PlanSave is.
func (a *Editor) PlanSaveAs(path string, force bool) (editor.SavePlan, error) {
	if a.writePending() {
		return editor.SavePlan{}, errAutoSaveWriting
	}
	p, err := a.ed.PlanSaveAs(path, force)
	a.markWriting(err)
	return p, err
}

// PlanSaveAll prepares the Save All of the modified tabs. It is refused and gated as
// PlanSave is.
func (a *Editor) PlanSaveAll() (editor.SavePlan, error) {
	if a.writePending() {
		return editor.SavePlan{}, errAutoSaveWriting
	}
	p, err := a.ed.PlanSaveAll()
	a.markWriting(err)
	return p, err
}

// WriteSave runs the writes of a plan. It reads only the plan, so the root calls it from
// a command, off the update loop.
func (a *Editor) WriteSave(p editor.SavePlan) []editor.SaveResult {
	return p.Run()
}

// ApplySavePlan records the results of a planned save on the editor's goroutine, and
// opens the gate again.
func (a *Editor) ApplySavePlan(p editor.SavePlan, results []editor.SaveResult) error {
	if a.saves != nil {
		a.saves.writing = false
	}
	return a.ed.ApplySavePlan(p, results)
}

// PlanRevert prepares the reload of the active document, as the editor's PlanRevert does.
// It is refused while a planned write is pending, as a save is, so the file is not read
// half written.
func (a *Editor) PlanRevert(force bool) (editor.RevertPlan, error) {
	if a.writePending() {
		return editor.RevertPlan{}, errAutoSaveWriting
	}
	return a.ed.PlanRevert(force)
}

// PlanRevertPath prepares the reload of the tab whose file is path, which need not be the
// active one, as the editor's PlanRevertPath does. It reads no file.
func (a *Editor) PlanRevertPath(path string) editor.RevertPlan {
	return a.ed.PlanRevertPath(path)
}

// RunRevert reads the file of plan. It reads only the plan, so the root calls it from a
// command, off the update loop.
func (a *Editor) RunRevert(p editor.RevertPlan) editor.RevertResult {
	return editor.RunRevert(p)
}

// ApplyRevert puts the read text into the planned document, on the editor's goroutine. The
// text of a tab that is not active may change too, so every span cache is marked stale, as
// EndShell does.
func (a *Editor) ApplyRevert(p editor.RevertPlan, r editor.RevertResult) error {
	a.spans.Edited()
	for _, c := range a.others {
		c.Edited()
	}
	return a.ed.ApplyRevert(p, r)
}

// HasFormatter reports whether the language of the active buffer has a formatter. The
// menu's when-evaluator asks for it through an optional interface.
func (a *Editor) HasFormatter() bool {
	return a.ed.HasFormatter()
}

// writePending reports whether a planned write has not been applied yet. A nil gate is
// never pending.
func (a *Editor) writePending() bool {
	return a.saves != nil && a.saves.writing
}

// markWriting marks the gate as writing after a plan that worked. A refused plan writes
// nothing, so it leaves the gate as it is.
func (a *Editor) markWriting(err error) {
	if a.saves != nil && err == nil {
		a.saves.writing = true
	}
}

// edited marks the syntax spans stale for an edit to the active document. The
// active document's text may be cached in the shared cache, and it may also be
// cached in its split-pane cache from the time it was not active. Both are marked,
// so a split pane never shows spans from before the edit. A nil cache is ignored.
func (a *Editor) edited() {
	a.spans.Edited()
	if doc := a.ed.Active(); doc != nil {
		a.others[doc].Edited()
	}
}

// MarkEdited marks the syntax spans of d stale for an edit made through the plugin port.
// The document need not be the active tab: a split pane can show a tab that is not, and
// that pane's cache is marked too. The shared cache belongs to the active document, so it
// is only marked when d is that one.
func (a *Editor) MarkEdited(d *editor.Document) {
	if d == nil {
		return
	}
	if d == a.ed.Active() {
		a.spans.Edited()
	}
	a.others[d].Edited()
}

// Active returns the active document, or nil when no tab is open. The nil is a
// nil interface, so the root's nil check works.
func (a *Editor) Active() tui.Document {
	doc := a.ed.Active()
	if doc == nil {
		return nil
	}
	return documentAdapter{doc: doc, spans: a.spans}
}

// DocAt returns the document of tab i as a documentAdapter, or an untyped nil when
// there is no tab i. The active tab uses the shared span cache, and every other tab
// has its own. It implements the root's tabDocs port.
func (a *Editor) DocAt(i int) tui.Document {
	doc := a.ed.DocAt(i)
	if doc == nil {
		return nil
	}
	if doc == a.ed.Active() {
		return documentAdapter{doc: doc, spans: a.spans}
	}
	return documentAdapter{doc: doc, spans: a.otherSpans(doc)}
}

// otherSpans returns the span cache of a tab that is not active, and makes it on
// first use. When there are more caches than tabs, the caches of closed tabs are
// dropped here, so a closed document is not kept alive.
func (a *Editor) otherSpans(doc *editor.Document) *highlight.Cache {
	if a.others == nil {
		return nil
	}
	c, ok := a.others[doc]
	if !ok {
		c = a.spans.Fresh()
		a.others[doc] = c
	}
	if n := len(a.ed.Tabs()); len(a.others) > n {
		open := make(map[*editor.Document]bool, n)
		for j := range n {
			open[a.ed.DocAt(j)] = true
		}
		for d := range a.others {
			if !open[d] {
				delete(a.others, d)
			}
		}
	}
	return c
}

// BeginShell prepares a shell command for the root, which runs it off the update
// loop. See editor.BeginShell.
func (a *Editor) BeginShell(act editor.Action, args map[string]any) (editor.ShellJob, error) {
	return a.ed.BeginShell(act, args)
}

// EndShell applies the output of a shell job. A replace can change a tab that is
// not active, so every span cache is marked stale, not only the active one's.
func (a *Editor) EndShell(job editor.ShellJob, out string, runErr error) editor.Result {
	a.spans.Edited()
	for _, c := range a.others {
		c.Edited()
	}
	return a.ed.EndShell(job, out, runErr)
}

// FindMatches returns the matches of q in the active document, in document
// order. It does not change the text. With no tab open there are no matches.
func (a *Editor) FindMatches(q search.Query) ([]search.Match, error) {
	doc := a.ed.Active()
	if doc == nil {
		return nil, nil
	}
	return search.Find(doc.Buffer, q)
}

// SearchRange is the range that find_in_selection limits the search to, and false when
// the search covers the whole document. The find bar reads it through the ui port.
func (a *Editor) SearchRange() (buffer.Selection, bool) { return a.ed.SearchRange() }

// ReplaceMatch replaces the match m of q with repl as one undo step, and returns
// the inserted text. The editor checks the read-only flag. The syntax spans are
// marked stale first, because the text changes.
func (a *Editor) ReplaceMatch(q search.Query, repl string, m search.Match) (string, error) {
	a.edited()
	return a.ed.ReplaceMatch(q, repl, m)
}

// ReplaceMatches replaces every match of q with repl as one undo step, and
// returns how many it replaced. The editor checks the read-only flag.
func (a *Editor) ReplaceMatches(q search.Query, repl string) (int, error) {
	a.edited()
	return a.ed.ReplaceMatches(q, repl)
}

// Tabs maps the editor's tabs to the tab strip entries. The path is carried too, so the
// root can watch the folders of the open tabs and reload the tabs whose files change.
// ApplyCompletionAtCarets accepts one completion at every caret of the document at path
// as one atomic edit. See editor.Editor.ApplyCompletionAtCarets; the TUI's multi-cursor
// completion accept path uses it.
func (a *Editor) ApplyCompletionAtCarets(path, text, wordChars string, expect *editor.Version) (int, error) {
	return a.ed.ApplyCompletionAtCarets(path, text, wordChars, expect)
}

// RecentTabIndices returns the editor's open tabs in MRU order (the active tab first),
// as editor.Editor.RecentTabIndices does. The TUI's multi-buffer completion reads it;
// without it completion falls back to tab order.
func (a *Editor) RecentTabIndices() []int {
	return a.ed.RecentTabIndices()
}

func (a *Editor) Tabs() []tui.TabInfo {
	tabs := a.ed.Tabs()
	out := make([]tui.TabInfo, len(tabs))
	for i, t := range tabs {
		out[i] = tui.TabInfo{Name: t.Name, Path: t.Path, Modified: t.Modified, Preview: t.Preview, Active: t.Active}
	}
	return out
}

// BufferTexts snapshots the open buffers for Live Grep's Buffers scope: each tab's
// text, in tab order, at most tui.BufferTextBytes of it. The sheet that searches them
// is modal, so one snapshot serves the whole session. The port is optional: the root
// type-asserts it on EditorState.
func (a *Editor) BufferTexts() ([]tui.BufferText, error) {
	tabs := a.ed.Tabs()
	out := make([]tui.BufferText, len(tabs))
	for i, t := range tabs {
		var text string
		partial := false
		if d := a.ed.DocAt(i); d != nil {
			text = d.Buffer.TextPrefix(tui.BufferTextBytes + 1)
			if len(text) > tui.BufferTextBytes {
				text, partial = text[:tui.BufferTextBytes], true
				// The cut may fall inside a rune; the grep works on text, so drop the
				// broken tail rather than search half a character.
				for len(text) > 0 && !utf8.ValidString(text) {
					text = text[:len(text)-1]
				}
			}
		}
		out[i] = tui.BufferText{Name: t.Name, Path: t.Path, Text: text, Partial: partial}
	}
	return out, nil
}

// SetRoot names the workspace root of the editor, which copy_relative_file_path measures
// from. The root calls it on its update loop when a project opens.
func (a *Editor) SetRoot(root string) {
	a.ed.SetRoot(root)
}

// Status returns the editor's status message.
func (a *Editor) Status() string {
	return a.ed.Status()
}

// StatusIsError reports whether the status message is a failure, so the warnings log
// keeps it and leaves out notes such as "Saved".
func (a *Editor) StatusIsError() bool {
	return a.ed.StatusIsError()
}

// ClearStatus drops the status message of the active tab. The root calls it when the
// user presses a key or clicks, so a stale message does not stay on the status row.
func (a *Editor) ClearStatus() {
	a.ed.ClearStatus()
}

// SetCursor moves the caret, and extend grows the selection from its anchor.
func (a *Editor) SetCursor(p buffer.Pos, extend bool) {
	a.ed.SetCursor(p, extend)
}

// VirtualSpaceEnabled reports whether the active document may put its caret past
// the end of a line (editor.virtual_space, the per-buffer switch, wrap off).
func (a *Editor) VirtualSpaceEnabled() bool { return a.ed.VirtualSpaceEnabled() }

// SetVirtualColumns parks the caret n display columns past the end of its line.
func (a *Editor) SetVirtualColumns(n int) { a.ed.SetVirtualColumns(n) }

// SetVirtualLines parks the caret on a virtual line below the last line, at
// display column col, and reports whether the editor allowed it.
func (a *Editor) SetVirtualLines(lines, col int) bool { return a.ed.SetVirtualLines(lines, col) }

// ExtendSelection grows the selection to p by the unit of a drag.
func (a *Editor) ExtendSelection(p buffer.Pos, u editor.Unit) {
	a.ed.ExtendSelection(p, u)
}

// ScrollBy moves the viewport by rows lines. The caret stays where it is.
func (a *Editor) ScrollBy(rows int) {
	a.ed.ScrollBy(rows)
}

// ScrollTo sets the first visible line of the viewport.
func (a *Editor) ScrollTo(top int) {
	a.ed.ScrollTo(top)
}

// OpenFile opens path in a permanent tab. The editor reports a failure in its
// status row, and the error is returned too.
func (a *Editor) OpenFile(path string) error {
	return a.ed.OpenFile(path)
}

// OpenPreview opens path in the preview tab.
func (a *Editor) OpenPreview(path string) error {
	return a.ed.OpenPreview(path)
}

// LoadFunc returns the read of path that runs off the UI goroutine. See
// editor.Editor.LoadFunc.
func (a *Editor) LoadFunc(path string) func() (*editor.Document, error) {
	return a.ed.LoadFunc(path)
}

// IsLargePath reports whether path would open in large file mode. See
// editor.Editor.IsLargePath.
func (a *Editor) IsLargePath(path string) bool {
	return a.ed.IsLargePath(path)
}

// OpenLoaded opens a document read by LoadFunc in a permanent tab.
func (a *Editor) OpenLoaded(d *editor.Document) {
	a.ed.OpenLoaded(d)
}

// GotoLine moves the caret to line and col, both one-based.
func (a *Editor) GotoLine(line, col int) error {
	return a.ed.GotoLine(line, col)
}

// GotoLineSelecting is GotoLine for the go-to-line prompt: in mark mode it extends the
// selection to the line.
func (a *Editor) GotoLineSelecting(line, col int) error {
	return a.ed.GotoLineSelecting(line, col)
}

// ActivateTab makes tab i the active tab. An index outside the tabs is ignored.
func (a *Editor) ActivateTab(i int) {
	a.ed.ActivateTab(i)
}

// CloseTab closes tab i. A modified tab is kept with editor.ErrUnsaved unless
// force is set.
func (a *Editor) CloseTab(i int, force bool) error {
	return a.ed.CloseTab(i, force)
}

// MoveTabTo moves tab from to index to, for a drag of a tab along the strip. The active
// tab stays on its document.
func (a *Editor) MoveTabTo(from, to int) error {
	ids := a.ed.DocIDs()
	if from < 0 || from >= len(ids) || to < 0 || to >= len(ids) {
		return fmt.Errorf("move tab: no tab at %d or %d", from, to)
	}
	return a.ed.MoveTab(ids[from], to-from)
}

// CloseNeedsConfirm reports whether closing tab i would be refused for unsaved
// edits, so the root can ask first.
func (a *Editor) CloseNeedsConfirm(i int) (bool, error) {
	return a.ed.CloseNeedsConfirm(i)
}

// Quit lists the tabs with unsaved edits, in tab order. With none, the editor
// is marked for quit and the list is nil.
func (a *Editor) Quit(force bool) []editor.Dirty {
	return a.ed.Quit(force)
}

// SetContext sets how many bytes of lines above the lines shown the lexer is given
// (highlight_context_bytes).
func (a *Editor) SetContext(n int) {
	a.spans.SetContext(n)
}
