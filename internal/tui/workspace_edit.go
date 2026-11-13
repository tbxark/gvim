package tui

import (
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/editor"
)

// FileEdit is one file's text edits for the shared multi-file apply helper. The
// root applies the edits of an open document at once; the edits of a file that is
// not open are applied after the file is read and opened as an unsaved tab, as the
// rename and code-action flows do.
type FileEdit struct {
	Path  string
	Edits []editor.LSPEdit
	// Expect, when set, must be the open document's version at the time the edits
	// were planned, or the edits are refused (editor.ErrStale).
	Expect *editor.Version
	// ExpectHash is the fnv64a hash of the text the edits were planned from, for a
	// file that is not open: the loaded file is skipped when its text differs. Zero
	// means no check.
	ExpectHash uint64
}

// FileEditResult summarises one apply of the shared helper.
type FileEditResult struct {
	// Files is how many files the edits changed.
	Files int
	// Tabs is how many of those are tabs: already open documents and files that
	// were opened by the apply.
	Tabs int
	// Opened is how many files were read and opened as unsaved tabs.
	Opened int
	// Operations is how many file operations (create, rename) a server's workspace
	// edit applied.
	Operations int
	// Skipped names the files left alone, with the reason.
	Skipped []string
}

// ApplyFileEdits applies edits to several files as one logical operation: the
// edits of open documents are applied now, with the version check of each edit,
// and the edits of closed files are read and applied in the returned command. Files
// that were not open are opened as tabs and left unsaved. Every tab is restored to
// the one that was active when the apply began. onDone runs exactly once with the
// summary, and the command it returns is run after the summary.
func (m *Model) ApplyFileEdits(edits []FileEdit, onDone func(FileEditResult) tea.Cmd) tea.Cmd {
	back := ""
	if doc := m.ed.Active(); doc != nil {
		back = docPath(doc)
	}
	res := FileEditResult{}
	var closed []FileEdit
	for _, fe := range edits {
		// The same file can be spelled two ways (a canonical /private/var against a
		// /var temp path, or a symlinked workspace): look up the open tab by its
		// resolved path so the edits reach the document that is already open.
		if open, ok := m.openTabPath(fe.Path); ok {
			fe.Path = open
		}
		err := m.applyLSPEditsExpect(fe.Path, fe.Edits, fe.Expect)
		switch {
		case errors.Is(err, editor.ErrNotOpen):
			closed = append(closed, fe)
		case err != nil:
			res.Skipped = append(res.Skipped, fe.Path+": "+err.Error())
		default:
			res.Files++
			res.Tabs++
		}
	}
	if len(closed) == 0 {
		return finishFileEdits(res, onDone)
	}
	ge, ok := m.ed.(gotoEditor)
	if !ok {
		for _, fe := range closed {
			res.Skipped = append(res.Skipped, fe.Path+": opening files is not supported in this port")
		}
		return finishFileEdits(res, onDone)
	}
	loads := make([]func() (*editor.Document, error), len(closed))
	for i, fe := range closed {
		loads[i] = ge.LoadFunc(fe.Path)
	}
	return func() tea.Msg {
		msg := fileEditsLoadedMsg{edits: closed, files: res.Files, skipped: res.Skipped, back: back, onDone: onDone}
		for _, load := range loads {
			doc, err := load()
			msg.docs = append(msg.docs, doc)
			msg.errs = append(msg.errs, err)
		}
		return msg
	}
}

// fileEditsLoadedMsg is the closed files of a multi-file apply that were read.
type fileEditsLoadedMsg struct {
	edits   []FileEdit
	docs    []*editor.Document
	errs    []error
	files   int
	back    string
	skipped []string
	onDone  func(FileEditResult) tea.Cmd
}

// openTabPath returns the path of the open tab that holds the file spelled path,
// matching by resolved path when the two spellings differ (a symlinked workspace or a
// /var versus /private/var temp directory). ok is false when the file is not open.
func (m *Model) openTabPath(path string) (string, bool) {
	if path == "" {
		return "", false
	}
	for _, t := range m.ed.Tabs() {
		if t.Path == path {
			return t.Path, true
		}
	}
	canon := canonicalFilePath(path)
	for _, t := range m.ed.Tabs() {
		if t.Path != "" && canonicalFilePath(t.Path) == canon {
			return t.Path, true
		}
	}
	return "", false
}

// canonicalFilePath resolves the symlinks of path so two spellings of one file
// compare equal. A path that cannot be resolved is returned cleaned.
func canonicalFilePath(path string) string {
	if path == "" {
		return ""
	}
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved
	}
	return filepath.Clean(path)
}

// fileEditsLoaded opens the files a multi-file apply read and applies their edits.
// The tab that was active when the apply began is restored at the end, so a
// refactor does not move the user.
func (m *Model) fileEditsLoaded(msg fileEditsLoadedMsg) tea.Cmd {
	res := FileEditResult{Files: msg.files, Tabs: msg.files, Skipped: slices.Clone(msg.skipped)}
	ge, ok := m.ed.(gotoEditor)
	if !ok {
		return finishFileEdits(res, msg.onDone)
	}
	for i, fe := range msg.edits {
		if msg.errs[i] != nil {
			res.Skipped = append(res.Skipped, fe.Path+": "+msg.errs[i].Error())
			continue
		}
		doc := msg.docs[i]
		if fe.ExpectHash != 0 && documentHash(doc) != fe.ExpectHash {
			res.Skipped = append(res.Skipped, fe.Path+": the file changed on disk")
			continue
		}
		ge.OpenLoaded(doc)
		if err := m.applyLSPEditsExpect(fe.Path, fe.Edits, nil); err != nil {
			res.Skipped = append(res.Skipped, fe.Path+": "+err.Error())
			continue
		}
		res.Files++
		res.Tabs++
		res.Opened++
	}
	if msg.back != "" {
		m.activateTabPath(msg.back)
	}
	m.lsp.stale = true
	m.syncViewport()
	return finishFileEdits(res, msg.onDone)
}

// finishFileEdits runs onDone with the summary and returns the command it starts.
func finishFileEdits(res FileEditResult, onDone func(FileEditResult) tea.Cmd) tea.Cmd {
	if onDone == nil {
		return nil
	}
	return onDone(res)
}

// documentHash is the fnv64a hash of a loaded document's text.
func documentHash(doc *editor.Document) uint64 {
	if doc == nil || doc.Buffer == nil {
		return hashText("")
	}
	return hashText(doc.Buffer.Text())
}

// activateTabPath makes the tab whose path is path the active one.
func (m *Model) activateTabPath(path string) {
	for i, t := range m.ed.Tabs() {
		if t.Path == path {
			m.activateTab(i)
			return
		}
	}
}

// applyLSPEditsExpect applies edits to the open document at path. With a
// version-checking editor the edits are refused with editor.ErrStale when the
// document no longer has expect; a port without that check refuses an expected
// version rather than applying edits it cannot verify.
func (m *Model) applyLSPEditsExpect(path string, edits []editor.LSPEdit, expect *editor.Version) error {
	if ed, ok := m.ed.(lspExpectEditor); ok {
		return ed.ApplyLSPEditsExpect(path, edits, expect)
	}
	if expect != nil {
		return errors.New("this port cannot check document versions")
	}
	return m.applyLSPEdits(path, edits)
}

// lspExpectEditor is the optional editor part that applies a language server's
// edits only when the document still has the expected version. The app adapter
// implements it.
type lspExpectEditor interface {
	ApplyLSPEditsExpect(path string, edits []editor.LSPEdit, expect *editor.Version) error
}

// lspVersionedEditor is the optional editor part that reports an open document's
// version. The app adapter implements it. The multi-file helper captures the version
// each edit must still see, and a multi-caret completion resolve refuses to start
// without it: the async reply needs the version captured at acceptance.
type lspVersionedEditor interface {
	DocumentVersion(path string) (editor.Version, bool)
}

// workspaceFileOps is the optional editor part that performs the file operations of
// a server's workspace edit. The app adapter implements it. Files are never
// overwritten, and deletes are refused (see docs/lsp.md).
type workspaceFileOps interface {
	CreateFile(path string, ignoreIfExists bool) error
	RenameFile(oldPath, newPath string, ignoreIfExists bool) error
	DeleteFile(path string) error
}

// workspaceEditPlan is the ordered steps of one server's workspace edit. The
// documentChanges run one at a time, in the order the server sent them, so a text
// edit of a file a resource operation just created lands on that file, and a
// refused operation stops the rest. A step that must read a file or touch the disk
// pauses the plan until its command returns.
type workspaceEditPlan struct {
	ops    []WorkspaceOp
	index  int
	back   string
	res    FileEditResult
	report func(FileEditResult) tea.Cmd
}

// workspaceStepLoadedMsg is one closed file of a plan that was read.
type workspaceStepLoadedMsg struct {
	plan  *workspaceEditPlan
	index int
	doc   *editor.Document
	err   error
}

// workspaceStepOpMsg is the result of one file operation of a plan. A non-empty
// reason means the operation was refused or failed and the rest of the plan stops.
type workspaceStepOpMsg struct {
	plan   *workspaceEditPlan
	index  int
	reason string
}

// applyWorkspaceEdit applies a server's workspace edit: its documentChanges in the
// order the server sent them, resource operations and text edits interleaved, a
// text edit of a closed file read and opened as an unsaved tab as the rename and
// code-action flows have always done. A refused or failed resource operation stops
// the remaining changes of the edit and the partial state is reported; text edits
// that fail their checks are skipped without stopping the rest. report summarises
// the apply for the caller.
func (m *Model) applyWorkspaceEdit(edit WorkspaceEdit, report func(FileEditResult) tea.Cmd) tea.Cmd {
	back := ""
	if doc := m.ed.Active(); doc != nil {
		back = docPath(doc)
	}
	plan := &workspaceEditPlan{ops: slices.Clone(edit.Ops), back: back, report: report}
	return m.runWorkspaceEdit(plan)
}

// runWorkspaceEdit runs the plan's steps until one needs a command, then returns
// that command; the message it produces resumes the plan.
func (m *Model) runWorkspaceEdit(plan *workspaceEditPlan) tea.Cmd {
	for plan.index < len(plan.ops) {
		op := plan.ops[plan.index]
		if op.Kind == WorkspaceOpText {
			path, open := m.openTabPath(op.Path)
			if open {
				m.applyWorkspaceTextStep(plan, op, path)
				plan.index++
				continue
			}
			ge, ok := m.ed.(gotoEditor)
			if !ok {
				plan.res.Skipped = append(plan.res.Skipped, op.Path+": opening files is not supported in this port")
				plan.index++
				continue
			}
			index := plan.index
			load := ge.LoadFunc(op.Path)
			return func() tea.Msg {
				doc, err := load()
				return workspaceStepLoadedMsg{plan: plan, index: index, doc: doc, err: err}
			}
		}
		ops, ok := m.ed.(workspaceFileOps)
		if !ok {
			m.notice = "This port cannot apply the file operations of a server edit"
			return nil
		}
		index := plan.index
		return func() tea.Msg {
			return workspaceStepOpMsg{plan: plan, index: index, reason: applyResourceOp(ops, op)}
		}
	}
	return m.finishWorkspaceEdit(plan)
}

// applyWorkspaceTextStep applies one text operation to the open document at path.
// The protocol version and the editor version are read now, when the step runs, so
// several edits of one document in one server edit are each checked against the
// text the earlier steps left, and a step computed against older text is skipped
// without stopping the rest.
func (m *Model) applyWorkspaceTextStep(plan *workspaceEditPlan, op WorkspaceOp, path string) {
	if op.Version != nil {
		if tr := m.lsp.tracks[path]; tr != nil && tr.lspVersionOK && tr.lspVersion != *op.Version {
			plan.res.Skipped = append(plan.res.Skipped, path+": the server's edit is for an older version of the document")
			return
		}
	}
	var expect *editor.Version
	if ve, ok := m.ed.(lspVersionedEditor); ok {
		if v, ok := ve.DocumentVersion(path); ok {
			expect = &v
		}
	}
	if err := m.applyLSPEditsExpect(path, op.Edits, expect); err != nil {
		plan.res.Skipped = append(plan.res.Skipped, path+": "+err.Error())
		return
	}
	plan.res.Files++
	plan.res.Tabs++
}

// workspaceStepLoaded opens the file a plan read and applies its text edits, then
// runs the next step. The file stays unsaved, like every file an LSP edit opens.
func (m *Model) workspaceStepLoaded(msg workspaceStepLoadedMsg) tea.Cmd {
	plan := msg.plan
	if plan == nil || plan.index != msg.index {
		return nil
	}
	op := plan.ops[msg.index]
	plan.index++
	if msg.err != nil {
		plan.res.Skipped = append(plan.res.Skipped, op.Path+": "+msg.err.Error())
		return m.runWorkspaceEdit(plan)
	}
	ge, ok := m.ed.(gotoEditor)
	if !ok {
		plan.res.Skipped = append(plan.res.Skipped, op.Path+": opening files is not supported in this port")
		return m.runWorkspaceEdit(plan)
	}
	ge.OpenLoaded(msg.doc)
	if err := m.applyLSPEditsExpect(op.Path, op.Edits, nil); err != nil {
		plan.res.Skipped = append(plan.res.Skipped, op.Path+": "+err.Error())
		return m.runWorkspaceEdit(plan)
	}
	plan.res.Files++
	plan.res.Tabs++
	plan.res.Opened++
	return m.runWorkspaceEdit(plan)
}

// workspaceStepOp records one file operation's result and runs the next step. A
// refused operation stops the plan: the changes before it stay applied, the
// remaining ones are reported as not applied, and the caller sees the partial
// state rather than an edit that claims to have landed whole.
func (m *Model) workspaceStepOp(msg workspaceStepOpMsg) tea.Cmd {
	plan := msg.plan
	if plan == nil || plan.index != msg.index {
		return nil
	}
	plan.index++
	if msg.reason != "" {
		plan.res.Skipped = append(plan.res.Skipped, msg.reason)
		if later := len(plan.ops) - plan.index; later > 0 {
			plan.res.Skipped = append(plan.res.Skipped, fmt.Sprintf(
				"%d later changes were not applied (the edit was only partly applied)", later))
		}
		plan.index = len(plan.ops)
	} else {
		plan.res.Operations++
	}
	return m.runWorkspaceEdit(plan)
}

// finishWorkspaceEdit restores the tab the edit started in, refreshes the view and
// reports the summary.
func (m *Model) finishWorkspaceEdit(plan *workspaceEditPlan) tea.Cmd {
	if plan.back != "" {
		m.activateTabPath(plan.back)
	}
	m.lsp.stale = true
	m.syncViewport()
	return finishFileEdits(plan.res, plan.report)
}

// applyResourceOp performs one file operation of a workspace edit. It returns the
// reason a refused operation was left alone, or "" when it changed the file.
func applyResourceOp(ops workspaceFileOps, op WorkspaceOp) string {
	switch op.Kind {
	case WorkspaceOpCreate:
		if op.Overwrite {
			return "creating " + baseName(op.Path) + " was refused: the server asked to overwrite it"
		}
		if err := ops.CreateFile(op.Path, op.IgnoreIfExists); err != nil {
			return err.Error()
		}
	case WorkspaceOpRename:
		if op.Overwrite {
			return "renaming onto " + baseName(op.NewPath) + " was refused: the server asked to overwrite it"
		}
		if err := ops.RenameFile(op.Path, op.NewPath, op.IgnoreIfExists); err != nil {
			return err.Error()
		}
	case WorkspaceOpDelete:
		if err := ops.DeleteFile(op.Path); err != nil {
			return err.Error()
		}
	}
	return ""
}

// baseName is the last element of a path, for a message.
func baseName(path string) string {
	if i := strings.LastIndexAny(path, "/\\"); i >= 0 && i+1 < len(path) {
		return path[i+1:]
	}
	return path
}
