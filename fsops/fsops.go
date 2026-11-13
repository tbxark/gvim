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

// cleanPath cleans path so that a trailing separator does not make Lstat follow
// a symbolic link. An empty path stays empty, because Clean would turn it into
// "." and name the working directory.
func cleanPath(path string) string {
	if path == "" {
		return ""
	}
	return filepath.Clean(path)
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
