package tui

import (
	"context"
	"fmt"
	"hash/fnv"
	"path/filepath"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/internal/tui/grid"
	"github.com/tbxark/gvim/internal/tui/list"
	"github.com/tbxark/gvim/internal/tui/prompt"
	"github.com/tbxark/gvim/internal/tui/searchpanel"
	"github.com/tbxark/gvim/syntax"
)

// projectSearchState is the root's project-search state: the last panel, kept after it
// closes so the next and previous match actions can walk its results, and the
// replacement waiting for its confirmation.
type projectSearchState struct {
	panel *searchpanel.Panel
	// replaceCancel cancels an in-flight remote replacement when a new project search
	// opens (the remote replacer checks its context between files).
	replaceCancel context.CancelFunc
}

// projectReplaceTag marks the confirmation of a project-wide replacement.
type projectReplaceTag struct{ req searchpanel.ReplaceRequest }

// projectReplaceDoneMsg carries the plan of a project-wide replacement.
type projectReplaceDoneMsg struct {
	result searchpanel.ReplaceResult
	// remote marks a replacement the remote replacer already wrote on the host: the root
	// applies no local edits and claims no pending Save All.
	remote       bool
	remoteResult searchpanel.RemoteReplaceResult
	remoteTarget string
	err          error
}

// remoteSearchUnsupported refuses the project replacement confirmation in a remote
// workspace: a safe remote replace needs verified remote reads and writes with one
// backup/atomic strategy, which is not wired yet, so the local replacer must never run
// for remote paths. The search itself does run through the authority (see
// docs/architecture.md §8.1).
func (m *Model) remoteSearchUnsupported() bool {
	if m.fileRoot == "" {
		return false
	}
	m.notice = uiText("gvim.project_search.remote_replace_unavailable", "Replacing across project files is not available in a remote workspace yet")
	return true
}

// openProjectSearch opens the project-wide Search and Replace panel, or the Git Grep
// panel when git is true. The panel gets snapshots of the open buffers, so unsaved text
// is searched, and the file index port for the file list. Without the port it shows a
// notice.
func (m *Model) openProjectSearch(git bool) tea.Cmd {
	if git && m.root.ports.GitGrep == nil {
		m.notice = uiText("gvim.project_search.git_grep_unsupported", "Git Grep is not supported in this port")
		return nil
	}
	if !git && m.root.ports.ProjectSearch == nil {
		m.notice = uiText("gvim.project_search.project_search_unsupported", "Project-wide search is not supported in this port")
		return nil
	}
	// A second open while the first panel is still on the stack would leave the first
	// panel's stream running behind the new one: cancel it first. An in-flight remote
	// replacement is cancelled too (it checks its context between remote files).
	if m.proj.replaceCancel != nil {
		m.proj.replaceCancel()
		m.proj.replaceCancel = nil
	}
	if m.proj.panel != nil && m.proj.panel.Active() {
		m.proj.panel.Dismiss()
		m.popOverlay()
	}
	modes, scope := m.projectSearchSettings()
	openFiles := m.projectOpenFiles()
	glob := ""
	var history []string
	if m.proj.panel != nil {
		glob = m.proj.panel.Glob()
		history = m.proj.panel.History()
	}
	var previewer searchpanel.Previewer
	if pv, ok := m.root.ports.ProjectSearch.(searchpanel.Previewer); ok {
		previewer = pv
	}
	// The panel paints the preview's syntax runs from the editor pane's kind styles, so it
	// needs no syntax or editorpane import.
	syntaxStyles := make(map[string]grid.Style, len(m.panes.Syntax))
	for i := range m.panes.Syntax {
		syntaxStyles[syntax.Kind(i).String()] = m.panes.Syntax[i]
	}
	// A remote workspace's panel searches the remote root: its Searcher and Grepper ports
	// are the authority-backed adapters, and the file index holds remote-relative paths.
	root := m.rootDir()
	if m.fileRoot != "" {
		root = m.fileRoot
	}
	query := ""
	if !git {
		query = m.projectSearchPrefill()
	}
	p := searchpanel.New(searchpanel.Options{
		Query:        query,
		Root:         root,
		Owner:        m.workspaceOwner,
		Files:        m.files,
		OpenFiles:    openFiles,
		Current:      m.projectCurrentFile(openFiles),
		Modes:        modes,
		Scope:        scope,
		Glob:         glob,
		History:      history,
		Previewer:    previewer,
		SyntaxStyles: syntaxStyles,
		PreviewWrap:  m.projectPreviewWrap(),
		Git:          git,
		Searcher:     m.root.ports.ProjectSearch,
		Grepper:      m.root.ports.GitGrep,
		Styles:       m.styles,
		Theme:        m.theme,
	})
	m.proj.panel = p
	m.pushOverlay(p)
	return p.Init()
}

// projectPreviewWrap returns the last panel's preview wrap setting, so the next panel
// opens with the same: the setting lives while the session does.
func (m *Model) projectPreviewWrap() bool {
	return m.proj.panel != nil && m.proj.panel.PreviewWrap()
}

// projectSearchPrefill is Fresh's selected-text prefill for the Search and Replace
// panel: a non-empty single-line selection shorter than 200 UTF-8 bytes becomes the
// initial query. A multi-line selection, no selection, or a selection of 200+ bytes
// leaves the query empty. The byte test matches Fresh's primary-selection end-start
// offset check.
func (m *Model) projectSearchPrefill() string {
	doc := m.ed.Active()
	if doc == nil {
		return ""
	}
	anchor, head, ok := doc.Selection()
	if !ok || anchor == head || anchor.Line != head.Line {
		return ""
	}
	text, ok := textBetweenWithin(doc, anchor, head, 800, 1)
	if !ok || text == "" || len(text) >= 200 {
		return ""
	}
	return text
}

// projectSearchSettings returns the match options and scope the next project panel opens
// with: the last panel's, or editor.search's when there is none.
func (m *Model) projectSearchSettings() (searchpanel.MatchOptions, searchpanel.Scope) {
	if m.proj.panel != nil {
		return m.proj.panel.Settings()
	}
	return searchpanel.MatchOptions{
		CaseSensitive: m.view.opts.Search.CaseSensitive,
		WholeWord:     m.view.opts.Search.WholeWord,
		Regex:         m.view.opts.Search.Regex,
	}, searchpanel.AllFiles
}

// projectOpenFiles snapshots the open buffers for a project search, so a search sees
// their unsaved text. It is nil when the editor cannot snapshot its buffers.
func (m *Model) projectOpenFiles() []searchpanel.SourceFile {
	ed, ok := m.ed.(bufferTextsEditor)
	if !ok {
		return nil
	}
	texts, err := ed.BufferTexts()
	if err != nil {
		return nil
	}
	tabs := m.ed.Tabs()
	out := make([]searchpanel.SourceFile, len(texts))
	for i, t := range texts {
		dirty := false
		if i < len(tabs) {
			dirty = tabs[i].Modified
		}
		out[i] = searchpanel.SourceFile{Path: t.Path, Name: t.Name, Text: t.Text, Dirty: dirty}
	}
	return out
}

// projectCurrentFile picks the active buffer out of files, the projectOpenFiles snapshot,
// for the current-file scope.
func (m *Model) projectCurrentFile(files []searchpanel.SourceFile) *searchpanel.SourceFile {
	i := m.activeTab()
	if i < 0 || i >= len(files) {
		return nil
	}
	f := files[i]
	return &f
}

// projectExportCap bounds the quickfix-style list the root builds from a project search:
// the search itself may hold DefaultLimit (10000) matches, but the list overlay gets an
// independent, smaller row budget and a notice naming the omitted count.
const projectExportCap = 2000

// openProjectResultsList opens the last project result set as the filterable list
// overlay: gvim's mapping of a quickfix export (there is no quickfix stack; the results
// panel stays the source of truth). The list's Enter jumps to a match and leaves the
// list open, so the user can walk the result set row by row; Esc closes it.
func (m *Model) openProjectResultsList() tea.Cmd {
	p := m.proj.panel
	if p == nil {
		m.notice = uiText("gvim.project_search.no_results_run_first", "No project search results: run Search and Replace in Project first")
		return nil
	}
	items, omitted := p.ExportItemsCapped(projectExportCap)
	if len(items) == 0 {
		m.notice = uiText("gvim.project_search.no_results_list", "No project search results to list")
		return nil
	}
	if omitted > 0 {
		m.notice = uiTextVars("gvim.project_search.export_truncated", "Project Search export limited to %{cap} matches; %{omitted} more are not listed", map[string]any{"cap": projectExportCap, "omitted": omitted})
	}
	rows := make([]list.Row, 0, len(items)+1)
	for _, it := range items {
		if it.Header {
			rows = append(rows, list.Row{Header: true, Left: it.Name})
			continue
		}
		rows = append(rows, list.Row{
			Left:  it.Text,
			Right: filepath.Base(it.Path),
			Value: projectListValue(it),
		})
	}
	if omitted > 0 {
		rows = append(rows, list.Row{Header: true, Left: fmt.Sprintf("… %d more matches not listed (export cap)", omitted)})
	}
	v := list.New(uiText("gvim.project_search.results_title", "Project Search Results"), rows, m.listChrome(), func(_ *list.View, r list.Row) tea.Cmd {
		t, ok := parseProjectListValue(r.Value)
		if !ok {
			return nil
		}
		return m.projectJump(t)
	})
	m.pushOverlay(v)
	return nil
}

// projectListValue encodes a match as a list row value: path, line and column, tab
// separated (a path cannot hold the tab after the root normalises it).
func projectListValue(it searchpanel.ExportItem) string {
	return it.Path + "	" + strconv.Itoa(it.Line) + "	" + strconv.Itoa(it.Col)
}

// parseProjectListValue decodes projectListValue.
func parseProjectListValue(value string) (searchpanel.OpenMsg, bool) {
	path, rest, ok := strings.Cut(value, "	")
	if !ok {
		return searchpanel.OpenMsg{}, false
	}
	lineText, colText, ok := strings.Cut(rest, "	")
	if !ok {
		return searchpanel.OpenMsg{}, false
	}
	line, err := strconv.Atoi(lineText)
	if err != nil {
		return searchpanel.OpenMsg{}, false
	}
	col, err := strconv.Atoi(colText)
	if err != nil {
		return searchpanel.OpenMsg{}, false
	}
	return searchpanel.OpenMsg{Path: path, Line: line, Col: col}, true
}

// projectJump opens the target of a project result: a file at a position, or the active
// buffer for an unnamed one. The editor takes the focus, as a goto does.
func (m *Model) projectJump(t searchpanel.OpenMsg) tea.Cmd {
	m.focusEditor()
	if t.Path == "" {
		m.gotoLine(t.Line, t.Col)
		return nil
	}
	return m.openFileCmd(t.Path, false, t.Line, t.Col)
}

// projectMatchAction jumps to the next or previous match of the last project search.
func (m *Model) projectMatchAction(forward bool) tea.Cmd {
	p := m.proj.panel
	if p == nil {
		m.notice = uiText("gvim.project_search.no_results_run_first", "No project search results: run Search and Replace in Project first")
		return nil
	}
	target, ok := p.NextMatch(forward)
	if !ok {
		m.notice = uiText("gvim.project_search.no_results_move", "No project search results to move through")
		return nil
	}
	return m.projectJump(target)
}

// askProjectReplace fills the replacement with the open buffers' fresh text and asks
// the user to confirm it.
func (m *Model) askProjectReplace(req searchpanel.ReplaceRequest) tea.Cmd {
	r := m.root.ports.ProjectReplace
	if r == nil {
		m.notice = uiText("gvim.project_search.replace_unsupported", "Replacing across files is not supported in this port")
		return nil
	}
	remote, isRemote := r.(searchpanel.RemoteReplacer)
	if !isRemote && m.remoteSearchUnsupported() {
		return nil
	}
	sources := make(map[string]searchpanel.SourceFile)
	for _, s := range m.projectOpenFiles() {
		sources[s.Path] = s
	}
	for i := range req.Files {
		if !req.Files[i].Dirty {
			continue
		}
		if src, ok := sources[req.Files[i].Path]; ok {
			req.Files[i].Text = src.Text
		}
	}
	target := ""
	if isRemote {
		target = remote.RemoteTarget()
	}
	m.pushOverlay(prompt.NewConfirm(m.styles, projectReplaceQuestion(req, target), projectReplaceTag{req: req}))
	return nil
}

// projectReplaceQuestion is the confirmation text. A remote target is stated in the
// question, so a remote replacement is never mistaken for a local one.
func projectReplaceQuestion(req searchpanel.ReplaceRequest, remoteTarget string) string {
	files := len(req.Files)
	q := fmt.Sprintf("Replace %d %s in %d %s?", req.Matches, pluralWord(req.Matches, "match"), files, pluralWord(files, "file"))
	if remoteTarget != "" {
		q = fmt.Sprintf("Replace %d %s in %d %s on %s?", req.Matches, pluralWord(req.Matches, "match"), files, pluralWord(files, "file"), remoteTarget)
	}
	return q
}

// projectReplaceAnswer runs the confirmed replacement, or reports that it was
// cancelled.
func (m *Model) projectReplaceAnswer(tag projectReplaceTag, d prompt.Done) tea.Cmd {
	if d.Outcome != prompt.Yes {
		m.notice = uiText("gvim.project_search.cancelled", "Replacement cancelled")
		return nil
	}
	r := m.root.ports.ProjectReplace
	if r == nil {
		m.notice = uiText("gvim.project_search.replace_unsupported", "Replacing across files is not supported in this port")
		return nil
	}
	req := tag.req
	if rr, ok := r.(searchpanel.RemoteReplacer); ok {
		if m.proj.replaceCancel != nil {
			m.proj.replaceCancel()
		}
		ctx, cancel := context.WithCancel(context.Background())
		m.proj.replaceCancel = cancel
		target := rr.RemoteTarget()
		return func() tea.Msg {
			res, err := rr.ProjectReplaceRemote(ctx, req)
			return projectReplaceDoneMsg{remote: true, remoteResult: res, remoteTarget: target, err: err}
		}
	}
	return func() tea.Msg {
		res, err := r.ProjectReplace(req)
		return projectReplaceDoneMsg{result: res, err: err}
	}
}

// projectReplaceDone applies a replacement plan through the shared multi-file edit
// helper: the open documents are checked against the text the edits were planned from
// and changed now, and the closed files are read and applied in the helper's command.
// Files whose text changed since the search are skipped, never written at a stale
// offset.
func (m *Model) projectReplaceDone(msg projectReplaceDoneMsg) tea.Cmd {
	if msg.remote {
		return m.remoteProjectReplaceDone(msg)
	}
	if msg.err != nil {
		m.notice = uiTextVars("gvim.project_search.error", "Replace: %{error}", map[string]any{"error": msg.err.Error()})
		return nil
	}
	// The open buffers are keyed by the tab's own path spelling: the finder and the editor
	// can spell one file differently (a canonical /private/var against a /var temp path,
	// or a symlinked workspace), and openTabPath maps the finder's spelling to the tab's.
	sources := map[string]string{}
	for _, s := range m.projectOpenFiles() {
		sources[s.Path] = s.Text
	}
	skipped := 0
	var edits []FileEdit
	for _, fe := range msg.result.Files {
		switch {
		case fe.Err != "" || fe.Stale:
			skipped++
		case len(fe.Edits) == 0:
		default:
			tab, open := m.openTabPath(fe.Path)
			if !open {
				edits = append(edits, FileEdit{Path: fe.Path, Edits: projectLSPEdits(fe.Edits), ExpectHash: fe.Hash})
				continue
			}
			// The open buffer must still hold the text the edits were planned from.
			text, ok := sources[tab]
			if !ok || hashText(text) != fe.Hash {
				skipped++
				continue
			}
			edits = append(edits, FileEdit{Path: tab, Edits: projectLSPEdits(fe.Edits)})
		}
	}
	return m.ApplyFileEdits(edits, func(res FileEditResult) tea.Cmd {
		return m.finishProjectReplace(res.Files, res.Tabs, skipped+len(res.Skipped))
	})
}

// remoteProjectReplaceDone reports a replacement the remote replacer already wrote on the
// host. No local edits are applied and no Save All is claimed: the files are changed on
// the remote disk. A failure keeps the partial count visible (earlier files were
// atomically written; there is no cross-file rollback).
func (m *Model) remoteProjectReplaceDone(msg projectReplaceDoneMsg) tea.Cmd {
	m.proj.replaceCancel = nil
	switch {
	case msg.err != nil:
		if msg.remoteResult.Files > 0 {
			m.notice = fmt.Sprintf("Replace on %s: %v; %d %s already written", msg.remoteTarget, msg.err, msg.remoteResult.Files, pluralWord(msg.remoteResult.Files, "file"))
		} else {
			m.notice = uiTextVars("gvim.project_search.remote_error", "Replace on %{target}: %{error}", map[string]any{"target": msg.remoteTarget, "error": msg.err.Error()})
		}
	case msg.remoteResult.Files == 0 && msg.remoteResult.Skipped == 0:
		m.notice = uiTextVars("gvim.project_search.no_file_matched", "Replace on %{target}: no file matched any more", map[string]any{"target": msg.remoteTarget})
	default:
		m.notice = fmt.Sprintf("%d %s changed on %s", msg.remoteResult.Files, pluralWord(msg.remoteResult.Files, "file"), msg.remoteTarget)
		if msg.remoteResult.Skipped > 0 {
			m.notice += fmt.Sprintf("; %d skipped", msg.remoteResult.Skipped)
		}
	}
	m.lsp.stale = true
	p := m.proj.panel
	if p == nil {
		return nil
	}
	p.SetNotice(m.notice)
	openFiles := m.projectOpenFiles()
	p.SetSources(openFiles, m.projectCurrentFile(openFiles))
	return p.ReSearch()
}

// finishProjectReplace says how many files and tabs a replacement changed, and refreshes
// the panel with the new text.
func (m *Model) finishProjectReplace(files, tabs, skipped int) tea.Cmd {
	m.notice = fmt.Sprintf("%d %s changed in %d %s; Save All to write",
		files, pluralWord(files, "file"), tabs, pluralWord(tabs, "tab"))
	if skipped > 0 {
		m.notice += fmt.Sprintf("; %d skipped", skipped)
	}
	m.lsp.stale = true
	p := m.proj.panel
	if p == nil {
		return nil
	}
	p.SetNotice(m.notice)
	openFiles := m.projectOpenFiles()
	p.SetSources(openFiles, m.projectCurrentFile(openFiles))
	return p.ReSearch()
}

// projectLSPEdits converts a replacement plan's edits to the editor edit type of the
// shared multi-file apply helper.
func projectLSPEdits(edits []searchpanel.ReplaceEdit) []editor.LSPEdit {
	out := make([]editor.LSPEdit, len(edits))
	for i, e := range edits {
		out[i] = editor.LSPEdit{
			From: buffer.Pos{Line: e.From.Line, Col: e.From.Col},
			To:   buffer.Pos{Line: e.To.Line, Col: e.To.Col},
			Text: e.Text,
		}
	}
	return out
}

// hashText is the content token a project replacement plans against: an FNV-64a hash of
// the text, the same token the app port records.
func hashText(text string) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(text))
	return h.Sum64()
}

// pluralWord returns the singular or plural form of a count word.
func pluralWord(n int, word string) string {
	if n == 1 {
		return word
	}
	if word == "match" {
		return "matches"
	}
	return word + "s"
}
