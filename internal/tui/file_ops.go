// File operations driven from the explorer and the palette: opening files, the
// clipboard, create/rename/delete/paste prompts and the background file jobs.

package tui

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/editor"
	expanel "github.com/tbxark/gvim/internal/tui/explorer"
	"github.com/tbxark/gvim/internal/tui/prompt"
)

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
	pasted  int              // how many items of the same paste were already completed
	multi   bool             // this job belongs to a multi-item paste (Fresh's multi dialog)
	drag    bool             // a drag-move job: never read from or written to the clipboard
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
	// remotePasteMultiTag asks the multi-item remote paste conflict dialog (Overwrite,
	// Overwrite All, Skip, Skip All, Cancel).
	remotePasteMultiTag struct{ job fileJob }
	// moveToTag confirms a drag/lifted move before anything moves.
	moveToTag struct {
		dir   string
		paths []string
	}
	gotoTag struct{}
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
		Title:    uiText("gvim.prompt.title.goto_line", "Go to Line"),
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
		what = uiTextVars("gvim.prompt.move_entries", "%{count} entries", map[string]any{"count": len(paths)})
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
	return uiTextVars("gvim.prompt.paste_many", "Paste %{count} %{noun} into %{dir} on %{target}?", map[string]any{"count": count, "noun": noun, "dir": dir, "target": target})
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
	title := uiText("gvim.prompt.title.paste", "Paste")
	switch kind {
	case remoteClipboardCopy:
		title = uiText("gvim.prompt.title.copy", "Copy")
	case remoteClipboardCut:
		title = uiText("gvim.prompt.title.cut", "Cut")
	case remoteClipboardDuplicate:
		title = uiText("gvim.prompt.title.duplicate", "Duplicate")
	case remoteClipboardPaste:
		if m.clip == nil || len(m.clip.paths) == 0 {
			return m.runRemoteClipboard(remoteClipboardTag{kind: kind, paths: paths, dir: dir})
		}
		q = pasteQuestion(len(m.clip.paths), dir, target)
	}
	m.pushOverlay(prompt.NewConfirmWith(m.styles, q, remoteClipboardTag{kind: kind, paths: paths, dir: dir}, prompt.ConfirmOptions{Title: title}))
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

// deleteQuestion is the delete confirmation. A remote delete is permanent: no trash
// exists on the remote host (a trash move was audited and deferred until an atomic
// same-filesystem no-replace move and a private trash directory are available), so the
// question says so and names the target; a local delete keeps the shorter OS-trash form.
func deleteQuestion(paths []string, target string) string {
	q := uiTextVars("gvim.remote.delete_file", "Delete %{name}?", map[string]any{"name": filepath.Base(paths[0])})
	if len(paths) > 1 {
		q = uiTextVars("gvim.remote.delete_items", "Delete %{count} items?", map[string]any{"count": len(paths)})
	}
	if target != "" {
		if len(paths) > 1 {
			q = uiTextVars("gvim.remote.permanent_delete_items", "Permanently delete %{count} items on %{target}? Remote deletes have no trash.", map[string]any{"count": len(paths), "target": target})
		} else {
			q = uiTextVars("gvim.remote.permanent_delete_file", "Permanently delete %{name} on %{target}? Remote deletes have no trash.", map[string]any{"name": filepath.Base(paths[0]), "target": target})
		}
	}
	return q
}

// askName opens the name prompt for a new file or folder in dir.
func (m *Model) askName(dir string, folder bool) {
	if !m.needOps() {
		return
	}
	title := uiText("gvim.prompt.title.new_file", "New File")
	if folder {
		title = uiText("gvim.prompt.title.new_folder", "New Folder")
	}
	m.pushOverlay(prompt.NewLine(m.styles, prompt.LineOptions{
		Title:    title,
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
		Title:    uiText("gvim.prompt.title.rename", "Rename"),
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
	m.pushOverlay(prompt.NewConfirmWith(m.styles, q, deleteTag{paths: paths}, prompt.ConfirmOptions{Title: uiText("gvim.dialog.title.confirm_delete", "Confirm Delete")}))
}

// askOverwrite asks whether a local copy that hit an existing destination may replace
// it. Yes runs the copy with the overwrite policy, and No runs it with skip.
func (m *Model) askOverwrite(job fileJob) {
	q := "Overwrite " + filepath.Base(job.path) + "?"
	m.pushOverlay(prompt.NewConfirmWith(m.styles, q, overwriteTag{job: job}, prompt.ConfirmOptions{Title: uiText("gvim.dialog.title.replace_file", "Replace File")}))
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
		Title: uiText("gvim.prompt.title.paste_conflict", "Paste Conflict"),
		Yes:   "Overwrite",
		No:    "Rename",
	}))
}

// askRemotePasteRename opens the new destination name prompt of a remote conflict.
func (m *Model) askRemotePasteRename(job fileJob) {
	name := job.dstName
	if name == "" {
		name = filepath.Base(job.path)
	}
	m.pushOverlay(prompt.NewLine(m.styles, prompt.LineOptions{
		Title:    uiText("gvim.prompt.title.paste_as", "Paste As"),
		Label:    pasteAsLabel(m.remoteExplorerTarget()),
		Input:    name,
		Tag:      remotePasteRenameTag{job: job},
		Validate: validateName,
	}))
}

// Indices of the remote multi-paste conflict choices (mirrors Fresh's multi dialog).
const (
	remotePasteOverwrite = iota
	remotePasteOverwriteAll
	remotePasteSkip
	remotePasteSkipAll
	remotePasteCancel
)

// remoteMultiPasteOptions are Fresh's multi-entry conflict answers: o/O differ only in
// case (current/all), and so do s/S.
func remoteMultiPasteOptions() []prompt.ChoiceOption {
	return []prompt.ChoiceOption{
		{Label: uiText("gvim.paste.overwrite", "Overwrite"), Key: "o"},
		{Label: uiText("gvim.paste.overwrite_all", "Overwrite All"), Key: "O"},
		{Label: uiText("gvim.paste.skip", "Skip"), Key: "s"},
		{Label: uiText("gvim.paste.skip_all", "Skip All"), Key: "S"},
		{Label: uiText("gvim.paste.cancel", "Cancel"), Key: "c"},
	}
}

// askRemoteMultiPasteConflict asks the multi-item paste conflict dialog.
func (m *Model) askRemoteMultiPasteConflict(job fileJob) {
	name := job.dstName
	if name == "" {
		name = filepath.Base(job.path)
	}
	q := uiTextVars("gvim.notice.remote_destination_exists", "Remote destination exists on %{target}: %{name}", map[string]any{"target": m.remoteExplorerTarget(), "name": name})
	m.pushOverlay(prompt.NewChoice(m.styles, uiText("gvim.prompt.title.paste_conflict", "Paste Conflict"), q, remotePasteMultiTag{job: job}, remoteMultiPasteOptions()))
}

// answerRemoteMultiPaste applies one multi-item conflict answer. Overwrite asks for the
// next conflict; Overwrite All also applies the overwrite policy to the remaining jobs;
// Skip skips only this item and asks again on the next conflict; Skip All stops the
// remaining items (clearing a cut paste's clipboard for the stranded sources, as Fresh's
// finished batch does); Cancel stops the remaining items and keeps the clipboard for a
// retry. Already-completed items are never touched.
func (m *Model) answerRemoteMultiPaste(job fileJob, choice int) tea.Cmd {
	switch choice {
	case remotePasteOverwrite:
		job.policy = expanel.ConflictOverwrite
		return m.runJob(job)
	case remotePasteOverwriteAll:
		job.policy = expanel.ConflictOverwrite
		rest := append([]fileJob(nil), job.rest...)
		for i := range rest {
			if rest[i].kind == jobCopy || rest[i].kind == jobMove {
				rest[i].policy = expanel.ConflictOverwrite
			}
		}
		job.rest = rest
		return m.runJob(job)
	case remotePasteSkip:
		return m.runJobs(job.rest)
	case remotePasteSkipAll:
		m.notice = pasteStopNotice(uiText("gvim.notice.paste_skipped", "Paste skipped"), job.pasted)
		if job.kind == jobMove {
			m.forgetClipboard(job.path)
			for _, next := range job.rest {
				m.forgetClipboard(next.path)
			}
		}
		return nil
	case remotePasteCancel:
		m.notice = pasteStopNotice(uiText("gvim.notice.paste_cancelled", "Paste cancelled"), job.pasted)
		return nil
	}
	return nil
}

// pasteStopNotice names how many items of a stopped paste already completed, so a cancel
// or skip-all after some work is never mistaken for nothing having happened.
func pasteStopNotice(prefix string, completed int) string {
	if completed > 0 {
		return uiTextVars("gvim.notice.paste_stopped", "%{prefix}; %{count} item(s) of this paste were already pasted", map[string]any{"prefix": prefix, "count": completed})
	}
	return prefix
}

// askMoveTo confirms a mouse drag move before any file operation runs. The move is a
// remote-workspace gesture: local explorer rows keep the keyboard Cut/Paste flow, and no
// drag ever overwrites a destination without the conflict dialog's explicit choice. The
// confirmation names the entries (or count) and the destination.
func (m *Model) askMoveTo(dir string, paths []string) tea.Cmd {
	if dir == "" || len(paths) == 0 {
		return nil
	}
	if m.remoteExplorerTarget() == "" {
		m.notice = uiText("gvim.notice.move_drag_remote_only", "Moving by drag is available in a remote workspace; use Cut and Paste for a local move")
		return nil
	}
	var what string
	if len(paths) == 1 {
		what = filepath.Base(paths[0])
	} else {
		what = uiTextVars("gvim.prompt.move_entries", "%{count} entries", map[string]any{"count": len(paths)})
	}
	q := uiTextVars("gvim.prompt.move_remote", "Move %{what} on %{target} to %{dir}?", map[string]any{"what": what, "target": m.remoteExplorerTarget(), "dir": dir})
	m.pushOverlay(prompt.NewConfirmWith(m.styles, q, moveToTag{dir: dir, paths: paths}, prompt.ConfirmOptions{Title: uiText("gvim.prompt.move", "Move"), Yes: uiText("gvim.prompt.move", "Move"), No: uiText("gvim.dialog.button.cancel", "Cancel")}))
	return nil
}

// pasteAsLabel names the conflict rename prompt, including the remote target when there
// is one.
func pasteAsLabel(target string) string {
	if target == "" {
		return uiText("gvim.prompt.paste_as", "Paste as:")
	}
	return uiTextVars("gvim.prompt.paste_as_on_target", "Paste as on %{target}:", map[string]any{"target": target})
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
	case quitDiscardTag:
		return m.quitDiscardAnswer(d)
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
		if d.Outcome != prompt.Yes {
			return nil
		}
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
	case remotePasteMultiTag:
		return m.answerRemoteMultiPaste(tag.job, d.Choice)
	case moveToTag:
		if d.Outcome != prompt.Yes {
			m.notice = uiText("gvim.notice.move_cancelled", "Move cancelled")
			return nil
		}
		jobs := make([]fileJob, 0, len(tag.paths))
		for _, path := range tag.paths {
			jobs = append(jobs, fileJob{kind: jobMove, path: path, dir: tag.dir, policy: expanel.ConflictCancel, multi: len(tag.paths) > 1, drag: true})
		}
		for i := range jobs {
			jobs[i].rest = jobs[i+1:]
		}
		return m.runJobs(jobs)
	case overwriteSaveTag:
		return m.overwriteAnswer(tag, d)
	case fullLoadTag:
		return m.fullLoadAnswer(tag, d)
	case authorityPasswordTag:
		return m.authorityReconnectCmd(d.Text)
	case authorityPassphraseTag:
		return m.authorityPassphraseCmd(d.Text)
	case authorityChallengeTag:
		m.authorityAnswers = append(m.authorityAnswers, d.Text)
		return m.askAuthorityChallenge()
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
		m.notice = uiText("gvim.notice.nothing_to_paste", "Nothing to paste")
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
			jobs = append(jobs, fileJob{kind: jobMove, path: p, dir: dir, multi: len(paths) > 1})
		} else {
			jobs = append(jobs, fileJob{kind: jobCopy, path: p, dir: dir, policy: expanel.ConflictCancel, multi: len(paths) > 1})
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
		// A remote paste conflict offers Fresh's dialogs: the single-item one is
		// overwrite/rename/cancel; a multi-item paste gets overwrite/overwrite all/skip/
		// skip all/cancel. The local adapter keeps its existing overwrite/skip prompt.
		if conflict && m.remoteExplorerTarget() != "" && (r.job.kind == jobCopy || r.job.kind == jobMove) {
			if r.job.multi || len(r.job.rest) > 0 {
				m.askRemoteMultiPasteConflict(r.job)
			} else {
				m.askRemotePasteConflict(r.job)
			}
			return nil
		}
		if r.job.kind == jobCopy && r.job.policy == expanel.ConflictCancel && conflict {
			m.askOverwrite(r.job)
			return nil
		}
		m.notice = r.err.Error()
		// The completed-item count is part of the remote multi-paste conflict flow only:
		// a local paste failure keeps its established error notice unchanged.
		if r.job.pasted > 0 && r.job.multi && m.remoteExplorerTarget() != "" && (r.job.kind == jobCopy || r.job.kind == jobMove) {
			m.notice += fmt.Sprintf(" (%d item(s) of this paste were already pasted)", r.job.pasted)
		}
		return nil
	}
	job := r.job
	switch job.kind {
	case jobRename, jobMove:
		if r.out != job.path {
			if err := m.ops.FollowRename(job.path, r.out); err != nil {
				m.notice = err.Error()
			}
			if !job.drag {
				m.forgetClipboard(job.path)
			}
			m.explorerPanel.DropPicked(job.path)
		} else if job.kind == jobMove && !job.drag {
			// A move into the directory the entry is in moves nothing, but it still
			// cancels a cut, as in Fresh. A rename to the same name keeps the clipboard.
			// A drag-move never touches the clipboard, so its no-op cannot cancel a cut.
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
	if job.kind == jobMove && !job.drag && len(job.rest) > 0 && !m.cutHolds(job.rest[0].path) {
		m.notice = uiText("gvim.notice.paste_cut_cancelled", "Paste stopped: the cut was cancelled")
		return reload
	}
	rest := job.rest
	if len(rest) > 0 {
		rest = append([]fileJob(nil), rest...)
		for i := range rest {
			rest[i].pasted = job.pasted + 1
		}
	}
	if wo, ok := m.ops.(interface{ TakeCopyWarning() string }); ok {
		if w := wo.TakeCopyWarning(); w != "" {
			m.notice = w
		}
	}
	return tea.Batch(reload, m.runJobs(rest))
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
