package uiadapter

import (
	"errors"
	"github.com/tbxark/gvim/document"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/tbxark/gvim/fsops"
)

// ApplyLSPEditsExpect applies the edits of a language server to the open document
// at path only when the document is still at expect. It is the version-checked
// form the root's multi-file edit helper uses for a workspace edit.
func (a *Editor) ApplyLSPEditsExpect(path string, edits []document.LSPEdit, expect *document.Version) error {
	return a.ed.ApplyLSPEditsExpect(path, edits, expect)
}

// DocumentVersion returns the version of the open document at path. ok is false
// when no open document has that path.
func (a *Editor) DocumentVersion(path string) (document.Version, bool) {
	return a.ed.OpenDocumentVersion(path)
}

// CreateFile creates an empty file for a server's workspace edit. It never
// overwrites: an existing file is an error, or nothing when ignoreIfExists is
// set. It is the root's workspaceFileOps port.
func (a *Editor) CreateFile(path string, ignoreIfExists bool) error {
	if a != nil && a.remote {
		return errors.New("creating " + path + " was refused: remote LSP file creation is not supported")
	}
	if _, err := os.Lstat(path); err == nil {
		if ignoreIfExists {
			return nil
		}
		return errors.New(path + " already exists")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		if ignoreIfExists && errors.Is(err, fs.ErrExist) {
			return nil
		}
		return err
	}
	return f.Close()
}

// RenameFile renames a file for a server's workspace edit and updates the open
// tabs whose path moved. It never overwrites: an existing target is an error, or
// nothing when ignoreIfExists is set. The rename is refused whole when an open tab
// would collide.
func (a *Editor) RenameFile(oldPath, newPath string, ignoreIfExists bool) error {
	if a != nil && a.remote {
		return errors.New("renaming " + oldPath + " was refused: remote LSP file rename is not supported")
	}
	if _, err := os.Lstat(newPath); err == nil {
		if ignoreIfExists {
			return nil
		}
		return errors.New(newPath + " already exists")
	}
	if err := a.CheckRename(oldPath, newPath); err != nil {
		return err
	}
	moved, err := a.MoveFile(oldPath, newPath, ignoreIfExists)
	if err != nil || !moved {
		return err
	}
	return a.FollowRename(oldPath, newPath)
}

// CheckRename reports the error FollowRename would return, before the disk is touched.
// It reads editor state, so it runs on the UI goroutine.
func (a *Editor) CheckRename(oldPath, newPath string) error {
	return a.ed.CheckRename(oldPath, newPath)
}

// MoveFile renames the file on disk and never overwrites. It touches only the disk, so
// it may run in a tea.Cmd. moved is false when nothing changed: the target existed and
// ignoreIfExists was set.
func (a *Editor) MoveFile(oldPath, newPath string, ignoreIfExists bool) (moved bool, err error) {
	if a != nil && a.remote {
		return false, errors.New("renaming " + oldPath + " was refused: remote LSP file rename is not supported")
	}
	if _, err := os.Lstat(newPath); err == nil {
		if ignoreIfExists {
			return false, nil
		}
		return false, errors.New(newPath + " already exists")
	}
	if err := os.MkdirAll(filepath.Dir(newPath), 0o755); err != nil {
		return false, err
	}
	if _, err := fsops.RenameNoReplaceResult(oldPath, newPath); err != nil {
		if ignoreIfExists && errors.Is(err, fsops.ErrConflict) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// FollowRename moves the open tabs of oldPath to newPath after a successful MoveFile.
// It changes editor state, so it runs on the UI goroutine.
func (a *Editor) FollowRename(oldPath, newPath string) error {
	return a.ed.RenamePath(oldPath, newPath)
}

// DeleteFile is refused: a server-initiated workspace edit does not delete files.
// Fresh refuses these too, because the request carries no user confirmation and
// leaves no trash to recover from. The root reports the refusal.
func (a *Editor) DeleteFile(path string) error {
	return errors.New("deleting " + path + " was refused: a language server's edit does not delete files")
}
