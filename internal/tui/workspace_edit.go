package tui

import (
	"errors"
	"hash/fnv"
	"path/filepath"
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
	canon := canonicalFilePath(path)
	if canon == "" {
		return "", false
	}
	for _, t := range m.ed.Tabs() {
		if t.Path != "" && t.Path == path {
			return t.Path, true
		}
	}
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
	res := FileEditResult{Files: msg.files, Tabs: msg.files, Skipped: cloneStrings(msg.skipped)}
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

// cloneStrings returns a copy of s, or nil for an empty slice.
func cloneStrings(s []string) []string {
	if len(s) == 0 {
		return nil
	}
	out := make([]string, len(s))
	copy(out, s)
	return out
}

// documentHash is the fnv64a hash of a loaded document's text.
func documentHash(doc *editor.Document) uint64 {
	h := fnv.New64a()
	if doc != nil && doc.Buffer != nil {
		_, _ = h.Write([]byte(doc.Buffer.Text()))
	}
	return h.Sum64()
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
// version. The app adapter implements it, and the multi-file helper captures the
// version each edit must still see.
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

// applyWorkspaceEdit applies a server's workspace edit: the file operations in a
// command first, then the text edits through the shared multi-file helper. The
// text edits of an open document whose protocol version has moved past the one the
// edit was computed against are refused, and so is a whole edit that touches such
// a document. report summarises the apply for the caller.
func (m *Model) applyWorkspaceEdit(edit WorkspaceEdit, report func(FileEditResult) tea.Cmd) tea.Cmd {
	var files []FileEdit
	var resourceOps []WorkspaceOp
	var stale []string
	for _, op := range edit.Ops {
		if op.Kind != WorkspaceOpText {
			resourceOps = append(resourceOps, op)
			continue
		}
		if op.Version != nil {
			if tr := m.lsp.tracks[op.Path]; tr != nil && tr.lspVersionOK && tr.lspVersion != *op.Version {
				stale = append(stale, op.Path)
				continue
			}
		}
		var expect *editor.Version
		if ve, ok := m.ed.(lspVersionedEditor); ok {
			if v, ok := ve.DocumentVersion(op.Path); ok {
				expect = &v
			}
		}
		files = append(files, FileEdit{Path: op.Path, Edits: op.Edits, Expect: expect})
	}
	if len(stale) > 0 {
		m.notice = "The server's edit is for an older version of " + baseName(stale[0]) + ": not applied"
		return nil
	}
	if len(resourceOps) == 0 {
		return m.ApplyFileEdits(files, report)
	}
	ops, ok := m.ed.(workspaceFileOps)
	if !ok {
		m.notice = "This port cannot apply the file operations of a server edit"
		return nil
	}
	return func() tea.Msg {
		msg := workspaceOpsMsg{files: files, report: report}
		for _, op := range resourceOps {
			if reason := applyResourceOp(ops, op); reason != "" {
				msg.skipped = append(msg.skipped, reason)
			}
		}
		return msg
	}
}

// workspaceOpsMsg is the result of the file operations of a workspace edit.
type workspaceOpsMsg struct {
	files   []FileEdit
	skipped []string
	report  func(FileEditResult) tea.Cmd
}

// workspaceOpsDone applies the text edits after the file operations.
func (m *Model) workspaceOpsDone(msg workspaceOpsMsg) tea.Cmd {
	m.lsp.stale = true
	return m.ApplyFileEdits(msg.files, func(res FileEditResult) tea.Cmd {
		res.Skipped = append(cloneStrings(msg.skipped), res.Skipped...)
		if msg.report == nil {
			return nil
		}
		return msg.report(res)
	})
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
		return ops.DeleteFile(op.Path).Error()
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
