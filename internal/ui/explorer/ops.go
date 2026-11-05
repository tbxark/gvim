package explorer

import (
	"errors"
	"path/filepath"

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
	// DeleteMsg asks the root to confirm and move Path to the trash (Delete).
	DeleteMsg struct{ Path string }
	// CopyMsg puts Path on the explorer clipboard as a copy (Ctrl+C).
	CopyMsg struct{ Path string }
	// CutMsg puts Path on the explorer clipboard as a move (Ctrl+X).
	CutMsg struct{ Path string }
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
// selected entry's path. With no selection it returns nil and nothing is sent.
func (p Panel) selectedMsg(make func(path string) tea.Msg) tea.Cmd {
	sel, ok := p.tree.Selected()
	if !ok {
		return nil
	}
	return emit(make(sel.Path))
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
