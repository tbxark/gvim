package app

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/tbxark/gvim/fsops"
	"github.com/tbxark/gvim/internal/tui"
	expl "github.com/tbxark/gvim/internal/tui/explorer"
)

// fileOps is the explorer's file-operation adapter over internal/fsops. The
// root runs the disk operations in a tea.Cmd, so the methods that touch the
// disk never change the editor. Editor follow-ups are separate methods, and the
// root calls them from Update, the goroutine that owns the editor, after the disk
// operation has finished. CheckRename is the one editor read that comes before a
// rename or move; the root calls it from Update too, so the command never reads
// the open tabs.
//
// Names are validated with fsops.Validate, so a bad name fails before the disk
// is touched. A destination that exists fails with an error that matches
// expl.ErrConflict.
type fileOps struct {
	ed pathEditor
}

// pathEditor is the part of *editor.Editor the file operations use. Tests pass a
// fake.
type pathEditor interface {
	CheckRename(oldPath, newPath string) error
	RenamePath(oldPath, newPath string) error
	CloseByPath(path string, force bool) error
}

// newFileOps returns the adapter over ed. ed is used by CheckRename, FollowRename
// and CloseDeleted.
func newFileOps(ed pathEditor) fileOps {
	return fileOps{ed: ed}
}

// The adapter satisfies the root's FileOps port. The root also asserts the
// wider set on it (see tui.Deps.Ops), so it must keep those methods too.
var _ tui.FileOps = fileOps{}

// CreateFile creates an empty file name inside dir and returns its path.
func (fileOps) CreateFile(_ context.Context, dir, name string) (string, error) {
	if err := fsops.Validate(dir, name); err != nil {
		return "", err
	}
	p := filepath.Join(dir, name)
	if err := fsops.CreateFile(p); err != nil {
		return "", classify(err)
	}
	return p, nil
}

// Mkdir creates a directory name inside dir and returns its path.
func (fileOps) Mkdir(_ context.Context, dir, name string) (string, error) {
	if err := fsops.Validate(dir, name); err != nil {
		return "", err
	}
	p := filepath.Join(dir, name)
	if err := fsops.Mkdir(p); err != nil {
		return "", classify(err)
	}
	return p, nil
}

// CheckRename is the editor's dry run for a rename or move from oldPath to newPath.
// It reads the open tabs and changes nothing. The root calls it from Update before
// it starts the disk operation.
func (f fileOps) CheckRename(oldPath, newPath string) error {
	return f.ed.CheckRename(oldPath, newPath)
}

// Rename gives path the name newName in its own directory and returns the new
// path. Renaming to the same name succeeds without a change. Rename does not read
// the open tabs. The root calls CheckRename from Update before it runs Rename, so
// a rename onto the path of an open tab is refused before the disk is touched.
func (f fileOps) Rename(_ context.Context, path, newName string) (string, error) {
	if err := fsops.Validate(filepath.Dir(path), newName); err != nil {
		return "", err
	}
	np := filepath.Join(filepath.Dir(path), newName)
	if err := fsops.Rename(path, np); err != nil {
		return "", classify(err)
	}
	return np, nil
}

// Move moves src into dstDir, keeping its name, and returns the new path.
func (f fileOps) Move(_ context.Context, src, dstDir string) (string, error) {
	dst := filepath.Join(dstDir, filepath.Base(src))
	if err := fsops.Move(src, dstDir); err != nil {
		return "", classify(err)
	}
	return dst, nil
}

// Copy copies src into dstDir and returns the new path. When src already lives
// in dstDir, the copy gets a free name such as "a copy.txt" (fsops.CopyName),
// so a paste into the same directory never overwrites the source. Otherwise the
// copy keeps the name and c decides a conflict.
func (fileOps) Copy(_ context.Context, src, dstDir string, c expl.Conflict) (string, error) {
	policy, err := toPolicy(c)
	if err != nil {
		return "", err
	}
	name := filepath.Base(src)
	if filepath.Clean(filepath.Dir(src)) == filepath.Clean(dstDir) {
		name, err = fsops.CopyName(dstDir, name)
		if err != nil {
			return "", classify(err)
		}
	}
	dst := filepath.Join(dstDir, name)
	if err := fsops.Copy(src, dst, policy); err != nil {
		return "", classify(err)
	}
	return dst, nil
}

// Trash moves path to the system trash.
func (fileOps) Trash(_ context.Context, path string) error {
	return classify(fsops.Trash(path))
}

// FollowRename moves the open tabs of oldPath, or of anything under it, to
// newPath. It does not touch the disk. The root calls it after a rename or a
// move, from Update.
func (f fileOps) FollowRename(oldPath, newPath string) error {
	return f.ed.RenamePath(oldPath, newPath)
}

// CloseDeleted closes the open tabs of path, or of anything under it, after the
// file was trashed. A tab with unsaved edits stays open and the error says so.
// The root calls it after a trash, from Update.
func (f fileOps) CloseDeleted(path string) error {
	return f.ed.CloseByPath(path, false)
}

// toPolicy maps the explorer's conflict choice to the fsops policy.
func toPolicy(c expl.Conflict) (fsops.ConflictPolicy, error) {
	switch c {
	case expl.ConflictCancel:
		return fsops.Cancel, nil
	case expl.ConflictOverwrite:
		return fsops.Overwrite, nil
	case expl.ConflictSkip:
		return fsops.Skip, nil
	}
	return 0, fmt.Errorf("explorer: unknown conflict choice %d", int(c))
}

// conflictError is an fsops conflict that also matches expl.ErrConflict. Its
// text is the fsops text.
type conflictError struct{ err error }

func (e conflictError) Error() string        { return e.err.Error() }
func (e conflictError) Unwrap() error        { return e.err }
func (e conflictError) Is(target error) bool { return target == expl.ErrConflict }

// classify marks an fsops conflict so the root can offer an overwrite, and
// passes every other error through.
func classify(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, fsops.ErrConflict) {
		return conflictError{err: err}
	}
	return err
}
