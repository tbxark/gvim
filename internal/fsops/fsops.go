// Package fsops implements the file operations behind the explorer: create,
// mkdir, rename, move, recursive copy with a conflict policy, trash, and name
// validation.
//
// Paths are used as given; callers resolve them. Symbolic links are never
// followed by Copy or Move: the link itself is the entry. Failures wrap the
// sentinel errors in this package so callers can branch with errors.Is.
package fsops

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

var (
	// ErrConflict reports that a destination already exists.
	ErrConflict = errors.New("destination already exists")
	// ErrInvalidName reports a name that is empty, reserved, contains a path
	// separator or NUL, or would resolve outside the root.
	ErrInvalidName = errors.New("invalid name")
	// ErrCanceled reports that a Copy was stopped by the Cancel policy.
	ErrCanceled = errors.New("operation canceled")
	// ErrInvalidDest reports a destination that is the source itself, lies
	// inside the source, contains the source, or is not a directory where one
	// is required.
	ErrInvalidDest = errors.New("invalid destination")
	// ErrTrashUnavailable reports that no system trash backend is linked.
	ErrTrashUnavailable = errors.New("trash backend not available")
)

// CreateFile creates an empty regular file at path. It fails with an error
// wrapping ErrConflict if path already exists. The parent directory must exist.
func CreateFile(path string) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return opError("create", path, err)
	}
	if err := f.Close(); err != nil {
		return opError("create", path, err)
	}
	return nil
}

// Mkdir creates a single directory at path. It fails with an error wrapping
// ErrConflict if anything already exists there. The parent must exist.
func Mkdir(path string) error {
	if err := os.Mkdir(path, 0o755); err != nil {
		return opError("mkdir", path, err)
	}
	return nil
}

// Rename renames oldPath to newPath. Both are full paths. Renaming an entry
// to its own name is a no-op. If newPath exists and is a different entry,
// Rename fails with an error wrapping ErrConflict and nothing is changed.
// Case-only renames on case-insensitive filesystems are allowed.
func Rename(oldPath, newPath string) error {
	oldInfo, err := os.Lstat(oldPath)
	if err != nil {
		return opError("rename", oldPath, err)
	}
	newInfo, err := os.Lstat(newPath)
	if err == nil {
		if !os.SameFile(oldInfo, newInfo) {
			return fmt.Errorf("fsops: rename to %s: %w", newPath, ErrConflict)
		}
		if oldPath == newPath {
			return nil
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return opError("rename", newPath, err)
	}
	if err := os.Rename(oldPath, newPath); err != nil {
		return opError("rename", oldPath, err)
	}
	return nil
}

// Move moves src into the directory dstDir, keeping its base name. If src
// already lives in dstDir the call is a no-op, matching Fresh's same-directory
// cut. It fails with ErrConflict if dstDir already holds an entry with that
// name, and with ErrInvalidDest if dstDir is not a directory or lies inside
// src. Across filesystems it copies and then removes src.
func Move(src, dstDir string) error {
	if _, err := os.Lstat(src); err != nil {
		return opError("move", src, err)
	}
	dirInfo, err := os.Stat(dstDir)
	if err != nil {
		return opError("move", dstDir, err)
	}
	if !dirInfo.IsDir() {
		return fmt.Errorf("fsops: move to %s: not a directory: %w", dstDir, ErrInvalidDest)
	}
	if filepath.Clean(filepath.Dir(src)) == filepath.Clean(dstDir) {
		return nil
	}
	if contains(src, dstDir) {
		return fmt.Errorf("fsops: move %s into %s: %w", src, dstDir, ErrInvalidDest)
	}
	dst := filepath.Join(dstDir, filepath.Base(src))
	if _, err := os.Lstat(dst); err == nil {
		return fmt.Errorf("fsops: move to %s: %w", dst, ErrConflict)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return opError("move", dst, err)
	}
	renameErr := os.Rename(src, dst)
	if renameErr == nil {
		return nil
	}
	if !errors.Is(renameErr, syscall.EXDEV) {
		return opError("move", src, renameErr)
	}
	if err := Copy(src, dst, Cancel); err != nil {
		return err
	}
	if err := os.RemoveAll(src); err != nil {
		return fmt.Errorf("fsops: move %s: copied to %s but could not remove source: %w", src, dst, err)
	}
	return nil
}

// opError wraps err with the operation and path. Existence failures also wrap
// ErrConflict so callers can branch on it.
func opError(op, path string, err error) error {
	if errors.Is(err, fs.ErrExist) {
		return fmt.Errorf("fsops: %s %s: %w: %w", op, path, ErrConflict, err)
	}
	return fmt.Errorf("fsops: %s %s: %w", op, path, err)
}

// contains reports whether child is parent itself or lies inside it, after
// making both paths absolute. It is lexical and does not resolve symlinks.
func contains(parent, child string) bool {
	p, err := filepath.Abs(parent)
	if err != nil {
		return false
	}
	c, err := filepath.Abs(child)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(p, c)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
