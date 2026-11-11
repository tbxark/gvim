package fsops

// Local no-replace rename and move. os.Rename replaces an existing destination, so these
// use the platform's rename that refuses one instead (renameat2 RENAME_NOREPLACE on
// Linux, renamex_np RENAME_EXCL on macOS, MoveFileEx without replace on Windows): one
// step, whatever the size of the tree, keeping times, links and attributes. Where that
// is not available (another device, a filesystem or platform without it) they fall back
// to the bounded copy-and-remove shape of the remote authority path: the destination is
// published with the ordinary no-replace copy (O_EXCL files, os.Mkdir directories,
// O_EXCL-like symlinks), and the source is removed only after the whole copy succeeded.
// A target that exists reports a conflict and the source stays.

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

const (
	// localRenameMaxEntries/Bytes/Depth match the remote explorer copy bounds, so a
	// local rename of a tree too large to copy is refused before touching the source.
	localRenameMaxEntries = 10000
	localRenameMaxBytes   = 256 << 20
	localRenameMaxDepth   = 64
)

// RenameNoReplace renames oldPath to newPath without replacing an existing target. A
// rename to a name that differs only in case (the same file on a case-insensitive
// filesystem) is allowed. A failure leaves the source and reports it.
func RenameNoReplace(oldPath, newPath string) error {
	_, err := RenameNoReplaceResult(oldPath, newPath)
	return err
}

// RenameNoReplaceResult is RenameNoReplace plus the copy-publication warning (the
// no-hard-link O_EXCL fallback). Callers that show a status notice use it.
func RenameNoReplaceResult(oldPath, newPath string) (CopyResult, error) {
	oldPath, newPath = cleanPath(oldPath), cleanPath(newPath)
	oldInfo, err := os.Lstat(oldPath)
	if err != nil {
		return CopyResult{}, opError("rename", oldPath, err)
	}
	if oldPath == newPath {
		return CopyResult{}, nil
	}
	if newInfo, err := os.Lstat(newPath); err == nil && os.SameFile(oldInfo, newInfo) {
		// A case-only rename: the "existing target" is the source itself.
		if err := os.Rename(oldPath, newPath); err != nil {
			return CopyResult{}, opError("rename", oldPath, err)
		}
		return CopyResult{}, nil
	}
	return renameOrCopy("rename", oldPath, newPath)
}

// MoveNoReplace moves src into dstDir keeping its name, without replacing an existing
// destination. A move within the source's own directory is a no-op. It renames, or
// copies and removes, as RenameNoReplace does.
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
	return renameOrCopy("move", src, filepath.Join(dstDir, filepath.Base(src)))
}

// errNoNativeRename is renameNative's answer on a platform without a no-replace rename.
var errNoNativeRename = errors.New("no native no-replace rename")

// renameNativeFn is renameNative; a test replaces it to take the copy path.
var renameNativeFn = renameNative

// renameOrCopy renames src to dst with the platform's no-replace rename, and falls back
// to the bounded copy-and-remove where the rename cannot be done in one step.
func renameOrCopy(op, src, dst string) (CopyResult, error) {
	err := renameNativeFn(src, dst)
	switch {
	case err == nil:
		return CopyResult{}, nil
	case errors.Is(err, fs.ErrExist):
		return CopyResult{}, fmt.Errorf("fsops: %s to %s: %w", op, dst, ErrConflict)
	case renameNeedsCopy(err):
		return copyThenRemove(src, dst)
	default:
		return CopyResult{}, opError(op, src, err)
	}
}

// renameNeedsCopy reports whether a failed native rename is one the copy can do: the
// target is on another device, or the platform or filesystem has no such rename.
func renameNeedsCopy(err error) bool {
	if errors.Is(err, errNoNativeRename) || errors.Is(err, errors.ErrUnsupported) {
		return true
	}
	var errno syscall.Errno
	if !errors.As(err, &errno) {
		return false
	}
	return errno == errnoCrossDevice || errno == syscall.EINVAL || errno == syscall.ENOSYS ||
		errno == syscall.ENOTSUP || errno == syscall.EOPNOTSUPP
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
