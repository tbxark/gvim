package fsops

// Local no-replace rename and move. There is no portable atomic rename-new on every
// supported local filesystem (os.Rename replaces an existing destination), so these
// operations use the same bounded copy-and-remove shape as the remote authority path:
// the destination is published with the ordinary no-replace copy (O_EXCL files,
// os.Mkdir directories, O_EXCL-like symlinks), and the source is removed only after the
// whole copy succeeded. A target that exists reports a conflict and the source stays.

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const (
	// localRenameMaxEntries/Bytes/Depth match the remote explorer copy bounds, so a
	// local rename of a tree too large to copy is refused before touching the source.
	localRenameMaxEntries = 10000
	localRenameMaxBytes   = 256 << 20
	localRenameMaxDepth   = 64
)

// RenameNoReplace renames oldPath to newPath without replacing an existing target. It
// copies first (bounded) and removes the source only after the copy succeeded; a failure
// leaves the source and reports it. The destination is published with no-replace creates,
// so a concurrent creator gets a conflict instead of being overwritten.
func RenameNoReplace(oldPath, newPath string) error {
	_, err := RenameNoReplaceResult(oldPath, newPath)
	return err
}

// RenameNoReplaceResult is RenameNoReplace plus the copy-publication warning (the
// no-hard-link O_EXCL fallback). Callers that show a status notice use it.
func RenameNoReplaceResult(oldPath, newPath string) (CopyResult, error) {
	oldPath, newPath = cleanPath(oldPath), cleanPath(newPath)
	if oldPath == newPath {
		if _, err := os.Lstat(oldPath); err != nil {
			return CopyResult{}, opError("rename", oldPath, err)
		}
		return CopyResult{}, nil
	}
	return copyThenRemove(oldPath, newPath)
}

// MoveNoReplace moves src into dstDir keeping its name, without replacing an existing
// destination. A move within the source's own directory is a no-op. It uses the same
// bounded copy-and-remove path as RenameNoReplace.
func MoveNoReplace(src, dstDir string) error {
	_, err := MoveNoReplaceResult(src, dstDir)
	return err
}

// MoveNoReplaceResult is MoveNoReplace plus the copy-publication warning.
func MoveNoReplaceResult(src, dstDir string) (CopyResult, error) {
	src, dstDir = cleanPath(src), cleanPath(dstDir)
	if _, err := os.Lstat(src); err != nil {
		return CopyResult{}, opError("move", src, err)
	}
	info, err := os.Stat(dstDir)
	if err != nil {
		return CopyResult{}, opError("move", dstDir, err)
	}
	if !info.IsDir() {
		return CopyResult{}, fmt.Errorf("fsops: move to %s: not a directory: %w", dstDir, ErrInvalidDest)
	}
	if filepath.Clean(filepath.Dir(src)) == dstDir {
		return CopyResult{}, nil
	}
	if contains(src, dstDir) {
		return CopyResult{}, fmt.Errorf("fsops: move %s into %s: %w", src, dstDir, ErrInvalidDest)
	}
	return copyThenRemove(src, filepath.Join(dstDir, filepath.Base(src)))
}

// copyThenRemove is the shared bounded publish-then-remove path.
func copyThenRemove(src, dst string) (CopyResult, error) {
	if err := checkLocalCopyBounds(src); err != nil {
		return CopyResult{}, err
	}
	result, err := CopyWithResult(src, dst, Cancel)
	if err != nil {
		return result, err
	}
	if err := os.RemoveAll(src); err != nil {
		return result, fmt.Errorf("fsops: copy-then-remove %s: copied to %s but could not remove the source: %w", src, dst, err)
	}
	return result, nil
}

// checkLocalCopyBounds walks the source without following links and refuses a tree over
// the local copy bounds before any destination is created.
func checkLocalCopyBounds(root string) error {
	info, err := os.Lstat(root)
	if err != nil {
		return opError("copy", root, err)
	}
	if !info.IsDir() {
		if info.Mode().IsRegular() && info.Size() > localRenameMaxBytes {
			return fmt.Errorf("fsops: copy %s: larger than the %d-byte copy bound", root, localRenameMaxBytes)
		}
		return nil
	}
	entries, bytes := 0, int64(0)
	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return opError("copy", p, err)
		}
		if p == root {
			return nil
		}
		rel, rerr := filepath.Rel(root, p)
		if rerr != nil {
			return opError("copy", p, rerr)
		}
		depth := strings.Count(rel, string(filepath.Separator)) + 1
		entries++
		if entries > localRenameMaxEntries {
			return fmt.Errorf("fsops: copy %s: the subtree has more than %d entries", root, localRenameMaxEntries)
		}
		if depth > localRenameMaxDepth {
			return fmt.Errorf("fsops: copy %s: the subtree is deeper than %d directories", root, localRenameMaxDepth)
		}
		if d.Type().IsRegular() {
			info, ierr := d.Info()
			if ierr != nil {
				return opError("copy", p, ierr)
			}
			bytes += info.Size()
			if bytes > localRenameMaxBytes {
				return fmt.Errorf("fsops: copy %s: the subtree is larger than the %d-byte copy bound", root, localRenameMaxBytes)
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	return nil
}
