package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"

	"github.com/tbxark/gvim/internal/buffer"
	"github.com/tbxark/gvim/internal/editor"
	"github.com/tbxark/gvim/internal/explorer"
	"github.com/tbxark/gvim/internal/finder"
	"github.com/tbxark/gvim/internal/fold"
	"github.com/tbxark/gvim/internal/keymap"
	palmodel "github.com/tbxark/gvim/internal/palette"
	"github.com/tbxark/gvim/internal/search"
	"github.com/tbxark/gvim/internal/syntax"
	"github.com/tbxark/gvim/internal/theme"
	"github.com/tbxark/gvim/internal/ui"
	"github.com/tbxark/gvim/internal/ui/dialog"
	"github.com/tbxark/gvim/internal/ui/style"
	"github.com/tbxark/gvim/internal/workspace"
)

// newUIDeps wires the domain packages into the ports of the UI. The command
// table is built for the active keymap, so the palette shows its shortcuts. The
// file ranker is always wired. A nil tree leaves the explorer out of the model,
// and a nil files index leaves the palette's file mode on its loading row. The
// optional ports (LSP and markdown) stay nil until their phases add adapters for
// them here. File operations follow renames and closes in ed.
func newUIDeps(ed *editor.Editor, km *keymap.Map, th theme.Theme, tree *explorer.Tree, files *finder.Index) ui.Deps {
	deps := ui.Deps{
		Editor:   editorAdapter{ed: ed, spans: &spanCache{}, others: map[*editor.Document]*spanCache{}, saves: &autoSaveGate{}},
		Keys:     keymapAdapter{km: km},
		Theme:    th,
		Clock:    time.Now,
		Commands: palmodel.Commands(km),
		Ranker:   rankPort{r: newFileRanker()},
		Dialogs:  dialogPort{},
		Recents:  recentFolders(),
		Ops:      newFileOps(ed),
	}
	// Assign only a non-nil tree or index: a nil concrete pointer in the interface
	// would look like an explorer or a file list that is present.
	if tree != nil {
		deps.Explorer = tree
	}
	if files != nil {
		deps.Files = files
	}
	return deps
}

// recentFolders returns the recent folder list for the welcome screen, newest
// first. A list that cannot be read is empty.
func recentFolders() []string {
	roots, err := workspace.Recent()
	if err != nil {
		return nil
	}
	paths := make([]string, len(roots))
	for i, r := range roots {
		paths[i] = r.Path()
	}
	return paths
}

// rankPort is fileRanker as the root's FileRanker port. The root has its own
// Ranked type, so the rows are copied. Record is forwarded, so the root can
// record the files the picker opens.
type rankPort struct {
	r fileRanker
}

// Rank scores files against query and returns at most limit matches, best first.
func (p rankPort) Rank(query string, files []string, limit int) []ui.Ranked {
	rows := p.r.Rank(query, files, limit)
	out := make([]ui.Ranked, len(rows))
	for i, r := range rows {
		out[i] = ui.Ranked{Path: r.Path, Indexes: r.Indexes}
	}
	return out
}

// Record notes that the file at rel was opened from the picker.
func (p rankPort) Record(rel string) {
	p.r.Record(rel)
}

// editorAdapter is the ui.EditorState view of *editor.Editor.
type editorAdapter struct {
	ed    *editor.Editor
	spans *spanCache
	// others holds the span cache of each tab that is not active, so that a split
	// pane showing such a tab lexes it once per edit. It is nil in a zero adapter,
	// and the other tabs then have no spans.
	others map[*editor.Document]*spanCache
	// saves records whether a planned auto-save is writing. See autosave.go. It is
	// nil in a zero adapter, and then nothing is refused.
	saves *autoSaveGate
}

// SetViewport sizes the editor pane.
func (a editorAdapter) SetViewport(rows, cols int) {
	a.ed.SetViewport(rows, cols)
}

// Do runs one editor action. The UI also sends actions whose feature is not
// built yet (the palette, the menu and the find bar). Those come back as
// ErrUnknownAction without reaching the editor, so the status row keeps the
// editor's own message. Every action may change the text, so the syntax spans
// of the active document are marked stale first. A save is refused while a
// planned write is pending, because its result is not yet recorded.
func (a editorAdapter) Do(act editor.Action, args map[string]any) editor.Result {
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
func (a editorAdapter) PlanSave() (editor.SavePlan, error) {
	if a.writePending() {
		return editor.SavePlan{}, errAutoSaveWriting
	}
	p, err := a.ed.PlanSave()
	a.markWriting(err)
	return p, err
}

// PlanSaveAs prepares the Save As of the active document. It is refused and gated as
// PlanSave is.
func (a editorAdapter) PlanSaveAs(path string, force bool) (editor.SavePlan, error) {
	if a.writePending() {
		return editor.SavePlan{}, errAutoSaveWriting
	}
	p, err := a.ed.PlanSaveAs(path, force)
	a.markWriting(err)
	return p, err
}

// PlanSaveAll prepares the Save All of the modified tabs. It is refused and gated as
// PlanSave is.
func (a editorAdapter) PlanSaveAll() (editor.SavePlan, error) {
	if a.writePending() {
		return editor.SavePlan{}, errAutoSaveWriting
	}
	p, err := a.ed.PlanSaveAll()
	a.markWriting(err)
	return p, err
}

// WriteSave runs the writes of a plan. It reads only the plan, so the root calls it from
// a command, off the update loop.
func (a editorAdapter) WriteSave(p editor.SavePlan) []editor.SaveResult {
	return p.Run()
}

// ApplySavePlan records the results of a planned save on the editor's goroutine, and
// opens the gate again.
func (a editorAdapter) ApplySavePlan(p editor.SavePlan, results []editor.SaveResult) error {
	if a.saves != nil {
		a.saves.writing = false
	}
	return a.ed.ApplySavePlan(p, results)
}

// PlanRevert prepares the reload of the active document, as the editor's PlanRevert does.
// It is refused while a planned write is pending, as a save is, so the file is not read
// half written.
func (a editorAdapter) PlanRevert(force bool) (editor.RevertPlan, error) {
	if a.writePending() {
		return editor.RevertPlan{}, errAutoSaveWriting
	}
	return a.ed.PlanRevert(force)
}

// RunRevert reads the file of plan. It reads only the plan, so the root calls it from a
// command, off the update loop.
func (a editorAdapter) RunRevert(p editor.RevertPlan) editor.RevertResult {
	return editor.RunRevert(p)
}

// ApplyRevert puts the read text into the planned document, on the editor's goroutine. The
// text of a tab that is not active may change too, so every span cache is marked stale, as
// EndShell does.
func (a editorAdapter) ApplyRevert(p editor.RevertPlan, r editor.RevertResult) error {
	a.spans.edited()
	for _, c := range a.others {
		c.edited()
	}
	return a.ed.ApplyRevert(p, r)
}

// HasFormatter reports whether the language of the active buffer has a formatter. The
// menu's when-evaluator asks for it through an optional interface.
func (a editorAdapter) HasFormatter() bool {
	return a.ed.HasFormatter()
}

// writePending reports whether a planned write has not been applied yet. A nil gate is
// never pending.
func (a editorAdapter) writePending() bool {
	return a.saves != nil && a.saves.writing
}

// markWriting marks the gate as writing after a plan that worked. A refused plan writes
// nothing, so it leaves the gate as it is.
func (a editorAdapter) markWriting(err error) {
	if a.saves != nil && err == nil {
		a.saves.writing = true
	}
}

// edited marks the syntax spans stale for an edit to the active document. The
// active document's text may be cached in the shared cache, and it may also be
// cached in its split-pane cache from the time it was not active. Both are marked,
// so a split pane never shows spans from before the edit. A nil cache is ignored.
func (a editorAdapter) edited() {
	a.spans.edited()
	if doc := a.ed.Active(); doc != nil {
		a.others[doc].edited()
	}
}

// Active returns the active document, or nil when no tab is open. The nil is a
// nil interface, so the root's nil check works.
func (a editorAdapter) Active() ui.Document {
	doc := a.ed.Active()
	if doc == nil {
		return nil
	}
	return documentAdapter{doc: doc, spans: a.spans}
}

// DocAt returns the document of tab i as a documentAdapter, or an untyped nil when
// there is no tab i. The active tab uses the shared span cache, and every other tab
// has its own. It implements the root's tabDocs port.
func (a editorAdapter) DocAt(i int) ui.Document {
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
func (a editorAdapter) otherSpans(doc *editor.Document) *spanCache {
	if a.others == nil {
		return nil
	}
	c, ok := a.others[doc]
	if !ok {
		c = &spanCache{}
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
func (a editorAdapter) BeginShell(act editor.Action, args map[string]any) (editor.ShellJob, error) {
	return a.ed.BeginShell(act, args)
}

// EndShell applies the output of a shell job. A replace can change a tab that is
// not active, so every span cache is marked stale, not only the active one's.
func (a editorAdapter) EndShell(job editor.ShellJob, out string, runErr error) editor.Result {
	a.spans.edited()
	for _, c := range a.others {
		c.edited()
	}
	return a.ed.EndShell(job, out, runErr)
}

// FindMatches returns the matches of q in the active document, in document
// order. It does not change the text. With no tab open there are no matches.
func (a editorAdapter) FindMatches(q search.Query) ([]search.Match, error) {
	doc := a.ed.Active()
	if doc == nil {
		return nil, nil
	}
	return search.Find(doc.Buffer, q)
}

// SearchRange is the range that find_in_selection limits the search to, and false when
// the search covers the whole document. The find bar reads it through the ui port.
func (a editorAdapter) SearchRange() (buffer.Selection, bool) { return a.ed.SearchRange() }

// ReplaceMatch replaces the match m of q with repl as one undo step, and returns
// the inserted text. The editor checks the read-only flag. The syntax spans are
// marked stale first, because the text changes.
func (a editorAdapter) ReplaceMatch(q search.Query, repl string, m search.Match) (string, error) {
	a.edited()
	return a.ed.ReplaceMatch(q, repl, m)
}

// ReplaceMatches replaces every match of q with repl as one undo step, and
// returns how many it replaced. The editor checks the read-only flag.
func (a editorAdapter) ReplaceMatches(q search.Query, repl string) (int, error) {
	a.edited()
	return a.ed.ReplaceMatches(q, repl)
}

// Tabs maps the editor's tabs to the tab strip entries. The path is carried too, so the
// root can watch the folders of the open tabs and reload the tabs whose files change.
func (a editorAdapter) Tabs() []ui.TabInfo {
	tabs := a.ed.Tabs()
	out := make([]ui.TabInfo, len(tabs))
	for i, t := range tabs {
		out[i] = ui.TabInfo{Name: t.Name, Path: t.Path, Modified: t.Modified, Preview: t.Preview, Active: t.Active}
	}
	return out
}

// SetRoot names the workspace root of the editor, which copy_relative_file_path measures
// from. The root calls it on its update loop when a project opens.
func (a editorAdapter) SetRoot(root string) {
	a.ed.SetRoot(root)
}

// Status returns the editor's status message.
func (a editorAdapter) Status() string {
	return a.ed.Status()
}

// SetCursor moves the caret, and extend grows the selection from its anchor.
func (a editorAdapter) SetCursor(p buffer.Pos, extend bool) {
	a.ed.SetCursor(p, extend)
}

// ExtendSelection grows the selection to p by the unit of a drag.
func (a editorAdapter) ExtendSelection(p buffer.Pos, u editor.Unit) {
	a.ed.ExtendSelection(p, u)
}

// ScrollBy moves the viewport by rows lines. The caret stays where it is.
func (a editorAdapter) ScrollBy(rows int) {
	a.ed.ScrollBy(rows)
}

// ScrollTo sets the first visible line of the viewport.
func (a editorAdapter) ScrollTo(top int) {
	a.ed.ScrollTo(top)
}

// OpenFile opens path in a permanent tab. The editor reports a failure in its
// status row, and the error is returned too.
func (a editorAdapter) OpenFile(path string) error {
	return a.ed.OpenFile(path)
}

// OpenPreview opens path in the preview tab.
func (a editorAdapter) OpenPreview(path string) error {
	return a.ed.OpenPreview(path)
}

// LoadFunc returns the read of path that runs off the UI goroutine. See
// editor.Editor.LoadFunc.
func (a editorAdapter) LoadFunc(path string) func() (*editor.Document, error) {
	return a.ed.LoadFunc(path)
}

// OpenLoaded opens a document read by LoadFunc in a permanent tab.
func (a editorAdapter) OpenLoaded(d *editor.Document) {
	a.ed.OpenLoaded(d)
}

// GotoLine moves the caret to line and col, both one-based.
func (a editorAdapter) GotoLine(line, col int) error {
	return a.ed.GotoLine(line, col)
}

// ActivateTab makes tab i the active tab. An index outside the tabs is ignored.
func (a editorAdapter) ActivateTab(i int) {
	a.ed.ActivateTab(i)
}

// CloseTab closes tab i. A modified tab is kept with editor.ErrUnsaved unless
// force is set.
func (a editorAdapter) CloseTab(i int, force bool) error {
	return a.ed.CloseTab(i, force)
}

// CloseNeedsConfirm reports whether closing tab i would be refused for unsaved
// edits, so the root can ask first.
func (a editorAdapter) CloseNeedsConfirm(i int) (bool, error) {
	return a.ed.CloseNeedsConfirm(i)
}

// Quit lists the tabs with unsaved edits, in tab order. With none, the editor
// is marked for quit and the list is nil.
func (a editorAdapter) Quit(force bool) []editor.Dirty {
	return a.ed.Quit(force)
}

// spanCache holds the syntax spans of the active document. The lexer runs over
// the whole text, because chroma is a stream lexer, and the result is kept until
// the next edit. The text changes only through editorAdapter.Do, which calls
// edited. A document switch, or a change of its language, starts a new lexer. The
// cache is used on the UI goroutine only.
type spanCache struct {
	gen     uint64           // bumped by every edit
	doc     *editor.Document // the document the lexer belongs to
	lang    string           // the language the lexer was chosen for, as set with Set Language
	plain   bool             // no lexer matched the document: no spans
	hl      *syntax.Highlighter
	spans   [][]syntax.Span
	valid   bool   // spans were lexed
	lexedAt uint64 // gen the spans were lexed at
}

// edited marks the spans stale. It is called before every action. A nil cache
// is a zero adapter with no spans, and it is ignored.
func (c *spanCache) edited() {
	if c != nil {
		c.gen++
	}
}

// lines returns the spans of every line of doc, lexing again only after an
// edit. It returns nil when no lexer matched the document, or when the cache is
// nil.
func (c *spanCache) lines(doc *editor.Document) [][]syntax.Span {
	if c == nil {
		return nil
	}
	if c.doc != doc || c.lang != doc.Language() {
		c.doc = doc
		c.lang = doc.Language()
		c.valid = false
		lexer := documentLexer(doc)
		c.plain = lexer == lexers.Fallback
		c.hl = syntax.NewHighlighter(lexer)
	}
	if c.plain {
		return nil
	}
	if !c.valid || c.lexedAt != c.gen {
		text := make([]string, doc.Buffer.Lines())
		for i := range text {
			text[i] = doc.Buffer.Line(i)
		}
		c.spans = c.hl.Lines(int(c.gen), text)
		c.lexedAt = c.gen
		c.valid = true
	}
	return c.spans
}

// documentLexer returns the lexer of doc. A language set with Set Language names the
// lexer, when the name matches one. Otherwise the file name and the text choose it, as
// syntax.Detect does.
func documentLexer(doc *editor.Document) chroma.Lexer {
	if name := doc.Language(); name != "" {
		if lexer := lexers.Get(name); lexer != nil {
			return lexer
		}
	}
	return syntax.Detect(doc.Path, []byte(doc.Buffer.Text()))
}

// documentAdapter is the read-only ui.Document view of one *editor.Document.
type documentAdapter struct {
	doc   *editor.Document
	spans *spanCache
}

// Encoding reports the saved text's encoding label, computed by the editor when
// the text was opened or saved.
func (d documentAdapter) Encoding() string { return d.doc.Encoding() }

// LineEnding is the ending of the saved text, as the editor reports it.
func (d documentAdapter) LineEnding() string { return d.doc.LineEnding() }

// Name is the file name, or "[No Name]" for an unnamed buffer.
func (d documentAdapter) Name() string {
	return d.doc.Name()
}

// Modified reports unsaved edits.
func (d documentAdapter) Modified() bool {
	return d.doc.Modified()
}

// LineCount is the number of lines in the buffer.
func (d documentAdapter) LineCount() int {
	return d.doc.Buffer.Lines()
}

// Line returns line i of the buffer.
func (d documentAdapter) Line(i int) string {
	return d.doc.Buffer.Line(i)
}

// SavedLines is the text at open or the last save, split into lines. The pane
// diffs it against the buffer for the change marks.
func (d documentAdapter) SavedLines() []string {
	return d.doc.SavedLines()
}

// HasPath reports whether the document has a file on disk. Save and revert need
// it, and an unnamed buffer has none.
func (d documentAdapter) HasPath() bool {
	return d.doc.Path != ""
}

// FilePath is the path the document is saved to, or the empty string for an
// untitled buffer. The Save As prompt is prefilled with it.
func (d documentAdapter) FilePath() string {
	return d.doc.Path
}

// Top is the first visible line of the viewport.
func (d documentAdapter) Top() int {
	return d.doc.Top
}

// Cursor is the caret position.
func (d documentAdapter) Cursor() buffer.Pos {
	return d.doc.Cursor
}

// Selection returns the selection ends, and ok is false when nothing is selected.
func (d documentAdapter) Selection() (anchor, head buffer.Pos, ok bool) {
	if d.doc.Sel == nil {
		return buffer.Pos{}, buffer.Pos{}, false
	}
	return d.doc.Sel.Anchor, d.doc.Sel.Head, true
}

// LineSpans returns the syntax spans of line i, or nil when no lexer matched
// the document or i is out of range. The spans are lexed once per edit.
func (d documentAdapter) LineSpans(i int) []syntax.Span {
	spans := d.spans.lines(d.doc)
	if i < 0 || i >= len(spans) {
		return nil
	}
	return spans[i]
}

// Cursors returns the extra carets of the document, which the pane paints in the
// secondary caret style.
func (d documentAdapter) Cursors() []editor.Caret { return d.doc.Cursors() }

// Folds returns the indentation fold ranges, for a tab of tabWidth columns.
func (d documentAdapter) Folds(tabWidth int) []fold.Range { return d.doc.Folds(tabWidth) }

// Folded reports whether the fold that starts at line is collapsed.
func (d documentAdapter) Folded(line int) bool { return d.doc.Folded(line) }

// ViewSettings is the document's display settings, read by the view commands and the pane.
func (d documentAdapter) ViewSettings() editor.ViewSettings { return d.doc.ViewSettings() }

// Language is the document's language name, shown in the status row.
func (d documentAdapter) Language() string { return d.doc.Language() }

// keymapAdapter is the ui.Keymap view of *keymap.Map.
type keymapAdapter struct {
	km *keymap.Map
}

// Resolve canonicalizes the key press and looks up its binding in the context.
// A chord that does not canonicalize resolves to nothing.
func (k keymapAdapter) Resolve(key ui.KeyPress, ctx ui.KeyContext) (ui.Command, bool) {
	chord, err := keymap.Canonical(key.Name, key.Mods)
	if err != nil {
		return ui.Command{}, false
	}
	b, ok := k.km.Resolve(chord, keymapContext(ctx))
	if !ok {
		return ui.Command{}, false
	}
	return ui.Command{Action: b.Action, Args: b.Args}, true
}

// Bindings lists every binding of the keymap, in file order, with its canonical
// chord and when-clause. It implements the root's BindingLister port.
func (k keymapAdapter) Bindings() []ui.KeyBinding {
	bs := k.km.Bindings()
	out := make([]ui.KeyBinding, len(bs))
	for i, b := range bs {
		out[i] = keyBinding(b)
	}
	return out
}

// Primary returns the binding the UI shows for action, and false when the action has
// no single-chord binding.
func (k keymapAdapter) Primary(action string) (ui.KeyBinding, bool) {
	b, ok := k.km.Primary(action)
	if !ok {
		return ui.KeyBinding{}, false
	}
	return keyBinding(b), true
}

// keyBinding maps a keymap binding to the root's view of it.
func keyBinding(b keymap.Binding) ui.KeyBinding {
	return ui.KeyBinding{Action: b.Action, Chord: b.Chord, Context: string(b.When)}
}

// keymapContext maps a UI key context to its keymap when-clause.
func keymapContext(ctx ui.KeyContext) keymap.Context {
	if ctx == ui.KeyContextFileExplorer {
		return keymap.ContextFileExplorer
	}
	if ctx == ui.KeyContextTerminal {
		return keymap.ContextTerminal
	}
	return keymap.ContextNormal
}

// dialogPort is the root's Dialogs port. It builds the dialogs of
// internal/ui/dialog and answers the Save As prompt's path questions from the
// disk, so the root does no file IO itself.
type dialogPort struct{}

// Unsaved opens the unsaved-changes dialog for the tab called name.
func (dialogPort) Unsaved(name string, tag any, ov style.Overlay) ui.Overlay {
	return ui.WrapDialog(dialog.NewUnsavedChanges(time.Now, name, tag, ov))
}

// SaveAs opens the Save As prompt prefilled with path. A path whose parent
// folder is missing is refused inline, and an existing file needs a confirm.
func (dialogPort) SaveAs(path string, tag any, ov style.Overlay) ui.Overlay {
	checks := dialog.PathChecks{
		Exists: func(p string) bool {
			_, err := os.Stat(p)
			return err == nil
		},
		Validate: func(p string) error {
			dir, err := os.Stat(filepath.Dir(p))
			if err != nil {
				return errors.New("folder does not exist")
			}
			if !dir.IsDir() {
				return errors.New("parent is not a folder")
			}
			return nil
		},
	}
	return ui.WrapDialog(dialog.NewSaveAs(time.Now, path, tag, checks, ov))
}
