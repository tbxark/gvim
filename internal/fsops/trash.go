package fsops

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	trash "github.com/hymkor/trash-go"
)

// trashBackend moves the given paths to the system trash. Tests replace it.
//
// The production backend is github.com/hymkor/trash-go (v0.3.0, pure Go; the
// purity gate passes). Tests replace it.
var trashBackend = trash.Throw

// Trash moves path to the system trash. It fails with an error wrapping
// fs.ErrNotExist if path does not exist, without calling the backend. Backend
// errors are wrapped with %w.
//
// Trash also refuses, without calling the backend, a path that is a symbolic
// link, a directory tree that holds a symbolic link or any entry that is not a
// regular file or directory, and a tree that cannot be read in full. The error
// names the entry that blocked the move. The backend follows links while it
// moves a tree across filesystems, so it could delete the target's files.
//
// Trash cleans path first, so a trailing separator cannot make a link look like
// the directory it names. If the backend returns nil but path still exists,
// Trash returns an error that names path.
func Trash(path string) error {
	path = cleanPath(path)
	if _, err := os.Lstat(path); err != nil {
		return opError("trash", path, err)
	}
	if err := checkTrashable(path); err != nil {
		return fmt.Errorf("fsops: trash %s: not moved: %w; handle that entry outside gvim", path, err)
	}
	if err := trashBackend(path); err != nil {
		return fmt.Errorf("fsops: trash %s: %w", path, err)
	}
	return checkRemoved(path)
}

// checkRemoved returns an error if path still exists after the backend returned
// nil. trash-go copies an entry to the trash when it cannot move it, then
// ignores the failed removal of the original.
func checkRemoved(path string) error {
	_, err := os.Lstat(path)
	switch {
	case err == nil:
		return fmt.Errorf("fsops: trash %s: copied to the trash, but the original is still there at %s; check the trash before retrying", path, path)
	case errors.Is(err, fs.ErrNotExist):
		return nil
	default:
		return fmt.Errorf("fsops: trash %s: could not confirm the original was removed: %w", path, err)
	}
}

// checkTrashable walks path without following links. It returns an error
// naming the first entry that is a symbolic link or another entry that is not a
// regular file or directory, or the first entry the walk cannot read.
func checkTrashable(path string) error {
	return filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("cannot read %s: %w", p, err)
		}
		if d.IsDir() || d.Type().IsRegular() {
			return nil
		}
		if d.Type()&fs.ModeSymlink != 0 {
			return fmt.Errorf("%s is a symbolic link", p)
		}
		return fmt.Errorf("%s is not a regular file or directory", p)
	})
}
