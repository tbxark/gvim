package explorer

import (
	"errors"
	"path/filepath"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// The file-operation messages. The panel sends them for the explorer keys and
// the File > Explorer menu rows. The root asks for the names and the
// confirmation, runs the operation through its file-operation port, and applies
// the result. The panel never does IO for these.
//
// Dir is the directory an operation targets: the selected directory, the
// parent of the selected file, or the tree root when nothing is selected.
// Path is the selected entry.
type (
	// NewFileMsg asks the root to prompt for a name and create an empty file
	// in Dir (Ctrl+N, File > Explorer > New File).
	NewFileMsg struct{ Dir string }
	// NewFolderMsg asks the root to prompt for a name and create a directory in
	// Dir (Ctrl+Shift+N, New Folder).
	NewFolderMsg struct{ Dir string }
	// RenameMsg asks the root to prompt for a new name for Path (F2).
	RenameMsg struct{ Path string }
	// DeleteMsg asks the root to confirm and move Path to the trash (Delete). With
	// several picked rows it sends Paths instead, every entry in display order.
	DeleteMsg struct {
		Path  string
		Paths []string
	}
	// CopyMsg puts Path on the explorer clipboard as a copy (Ctrl+C). A
	// multi-item copy sends Paths instead, every entry in display order.
	CopyMsg struct {
		Path  string
		Paths []string
	}
	// CutMsg puts Path on the explorer clipboard as a move (Ctrl+X). A multi-item
	// cut sends Paths instead, every entry in display order.
	CutMsg struct {
		Path  string
		Paths []string
	}
	// PasteMsg pastes the explorer clipboard into Dir (Ctrl+V).
	PasteMsg struct{ Dir string }
)

// Conflict is what a copy does when its destination already exists. It mirrors
// fsops.ConflictPolicy, which the ui packages do not import. The app adapter
// maps one to the other.
type Conflict int

const (
	// ConflictCancel stops the copy and reports ErrConflict. It is the first
	// attempt of a paste, so the root can ask before it overwrites.
	ConflictCancel Conflict = iota + 1
	// ConflictOverwrite replaces the destination.
	ConflictOverwrite
	// ConflictSkip leaves the destination alone and reports no error.
	ConflictSkip
)

// ErrConflict reports that a file operation's destination already exists. The
// app adapter wraps the fsops error so that errors.Is finds this sentinel.
var ErrConflict = errors.New("destination already exists")

// selectedMsg returns the command that sends the message make builds from the
// selected entry's path. With no selection it returns nil and nothing is sent. The
// tree root cannot be renamed or deleted, so for it the command is a refusal, and
// verb names the refused operation in the notice.
func (p Panel) selectedMsg(verb string, make func(path string) tea.Msg) tea.Cmd {
	sel, ok := p.tree.Selected()
	if !ok {
		return nil
	}
	if sel.Path == p.tree.Root() {
		return rootRefused(verb)
	}
	return emit(make(sel.Path))
}

// deleteCmd is the DeleteMsg for the targets: the picked rows, or the cursor row when
// none is picked, as Fresh's delete acts on its effective selection. The tree root is
// left out of several, and a delete of only the root is refused.
func (p Panel) deleteCmd() tea.Cmd {
	root := p.tree.Root()
	paths := slices.DeleteFunc(p.targets(), func(q string) bool { return q == root })
	switch len(paths) {
	case 0:
		if len(p.targets()) > 0 {
			return rootRefused("deleted")
		}
		return nil
	case 1:
		return emit(DeleteMsg{Path: paths[0]})
	}
	return emit(DeleteMsg{Paths: paths})
}

// rootRefused returns the command that reports an operation the tree root does not
// allow. The root shows the error as a notice.
func rootRefused(verb string) tea.Cmd {
	return emit(ErrorMsg{Err: errors.New("the explorer root cannot be " + verb)})
}

// clipboardMsg is the CopyMsg, or the CutMsg when cut is set, for paths. One path
// goes in the Path field, which single-item callers use. Several go in Paths, in
// display order.
func clipboardMsg(paths []string, cut bool) tea.Msg {
	var path string
	var many []string
	if len(paths) == 1 {
		path = paths[0]
	} else {
		many = paths
	}
	if cut {
		return CutMsg{Path: path, Paths: many}
	}
	return CopyMsg{Path: path, Paths: many}
}

// Within reports whether path is dir or lies under it. Both are cleaned paths.
func Within(path, dir string) bool {
	if path == dir {
		return true
	}
	sep := string(filepath.Separator)
	return strings.HasPrefix(path, strings.TrimSuffix(dir, sep)+sep)
}

// targetDir is the directory a new entry or a paste goes into: the selected
// directory, the parent of the selected file, or the tree root when no row is
// selected.
func (p Panel) targetDir() string {
	sel, ok := p.tree.Selected()
	switch {
	case !ok:
		return p.tree.Root()
	case sel.IsDir:
		return sel.Path
	default:
		return filepath.Dir(sel.Path)
	}
}
