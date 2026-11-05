package fsops

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// ConflictPolicy decides what Copy does when the destination already exists.
// The zero value is not a valid policy.
type ConflictPolicy int

const (
	// Overwrite replaces the existing destination with the copy.
	Overwrite ConflictPolicy = iota + 1
	// Skip leaves the existing destination untouched and returns nil.
	Skip
	// Cancel leaves the destination untouched and returns an error that
	// wraps both ErrCanceled and ErrConflict.
	Cancel
)

// String returns the policy name.
func (p ConflictPolicy) String() string {
	switch p {
	case Overwrite:
		return "Overwrite"
	case Skip:
		return "Skip"
	case Cancel:
		return "Cancel"
	default:
		return fmt.Sprintf("ConflictPolicy(%d)", int(p))
	}
}

// Copy copies src to dst. A file is copied with its permission bits. A
// directory is copied recursively. A symbolic link is recreated as a link with
// the same target, not followed.
//
// If dst does not exist, the copy is made. If dst exists, policy decides; see
// ConflictPolicy. Only the top-level destination is checked: entries created
// inside a new copy never conflict. The copy is built in a hidden staging
// directory beside dst and renamed into place, so a failure partway through
// leaves dst unchanged.
//
// Copy fails with ErrInvalidDest if dst is src, lies inside src, or contains
// src. The parent of dst must already exist.
func Copy(src, dst string, policy ConflictPolicy) error {
	if policy < Overwrite || policy > Cancel {
		return fmt.Errorf("fsops: copy: unknown conflict policy %d", int(policy))
	}
	srcInfo, err := os.Lstat(src)
	if err != nil {
		return opError("copy", src, err)
	}
	if contains(src, dst) || contains(dst, src) {
		return fmt.Errorf("fsops: copy %s to %s: %w", src, dst, ErrInvalidDest)
	}
	replace := false
	dstInfo, err := os.Lstat(dst)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		// Free destination: copy below.
	case err != nil:
		return opError("copy", dst, err)
	default:
		if os.SameFile(srcInfo, dstInfo) {
			return fmt.Errorf("fsops: copy %s onto itself: %w", src, ErrInvalidDest)
		}
		switch policy {
		case Overwrite:
			replace = true
		case Skip:
			return nil
		default:
			return fmt.Errorf("fsops: copy to %s: %w: %w", dst, ErrCanceled, ErrConflict)
		}
	}
	return place(src, srcInfo, dst, replace)
}

// place builds a copy of src in a staging directory beside dst, then renames
// it onto dst. When replace is true, the existing dst is removed just before
// the rename, after the copy has succeeded.
func place(src string, srcInfo fs.FileInfo, dst string, replace bool) error {
	stage, err := os.MkdirTemp(filepath.Dir(dst), ".fsops-copy-*")
	if err != nil {
		return opError("copy", dst, err)
	}
	defer func() { _ = os.RemoveAll(stage) }()

	staged := filepath.Join(stage, filepath.Base(dst))
	if err := copyTree(src, srcInfo, staged); err != nil {
		return err
	}
	if replace {
		if err := os.RemoveAll(dst); err != nil {
			return opError("copy", dst, err)
		}
	}
	if err := os.Rename(staged, dst); err != nil {
		return opError("copy", dst, err)
	}
	return nil
}

// copyTree copies the entry at src, described by info, to dst, which must not
// exist.
func copyTree(src string, info fs.FileInfo, dst string) error {
	mode := info.Mode()
	switch {
	case mode&fs.ModeSymlink != 0:
		target, err := os.Readlink(src)
		if err != nil {
			return opError("copy", src, err)
		}
		if err := os.Symlink(target, dst); err != nil {
			return opError("copy", dst, err)
		}
		return nil
	case mode.IsDir():
		// Create with owner access so children can be written even when the
		// source directory is read-only; the real mode is applied last.
		if err := os.Mkdir(dst, 0o700); err != nil {
			return opError("copy", dst, err)
		}
		entries, err := os.ReadDir(src)
		if err != nil {
			return opError("copy", src, err)
		}
		for _, e := range entries {
			childInfo, err := e.Info()
			if err != nil {
				return opError("copy", filepath.Join(src, e.Name()), err)
			}
			if err := copyTree(filepath.Join(src, e.Name()), childInfo, filepath.Join(dst, e.Name())); err != nil {
				return err
			}
		}
		if err := os.Chmod(dst, mode.Perm()); err != nil {
			return opError("copy", dst, err)
		}
		return nil
	case mode.IsRegular():
		return copyFile(src, dst, mode.Perm())
	default:
		return fmt.Errorf("fsops: copy %s: unsupported file type %s", src, mode.Type())
	}
}

// copyFile copies the contents of the regular file src to a new file dst with
// the given permission bits.
func copyFile(src, dst string, perm fs.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return opError("copy", src, err)
	}
	defer func() { _ = in.Close() }()

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
	if err != nil {
		return opError("copy", dst, err)
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return opError("copy", src, err)
	}
	if err := out.Close(); err != nil {
		return opError("copy", dst, err)
	}
	// The umask may have stripped bits from the mode passed to OpenFile.
	if err := os.Chmod(dst, perm); err != nil {
		return opError("copy", dst, err)
	}
	return nil
}

// maxCopyAttempts bounds the number of " copy N" candidates CopyName tries.
const maxCopyAttempts = 1000

// CopyName returns the first free duplicate name for name inside dir. The
// candidates are "<stem> copy.<ext>", then "<stem> copy 2.<ext>", and so on.
// Names without an extension use "<stem> copy" and "<stem> copy 2". The
// extension is the text after the last dot, unless that dot is the first byte,
// so ".bashrc" has no extension. The returned name is not created.
//
// CopyName returns ErrInvalidName if name is not a valid name for dir, and
// ErrConflict if no free candidate was found.
func CopyName(dir, name string) (string, error) {
	if err := Validate(dir, name); err != nil {
		return "", err
	}
	stem, ext := splitStemExt(name)
	for n := 1; n <= maxCopyAttempts; n++ {
		candidate := copyCandidate(stem, ext, n)
		_, err := os.Lstat(filepath.Join(dir, candidate))
		if errors.Is(err, fs.ErrNotExist) {
			return candidate, nil
		}
		if err != nil {
			return "", opError("copy name", dir, err)
		}
	}
	return "", fmt.Errorf("fsops: no free copy name for %s in %s: %w", name, dir, ErrConflict)
}

// copyCandidate builds the n-th duplicate name. n = 1 gives no number.
func copyCandidate(stem, ext string, n int) string {
	suffix := " copy"
	if n > 1 {
		suffix = fmt.Sprintf(" copy %d", n)
	}
	if ext == "" {
		return stem + suffix
	}
	return stem + suffix + "." + ext
}

// splitStemExt splits name at its last dot. A leading dot does not start an
// extension.
func splitStemExt(name string) (stem, ext string) {
	if i := strings.LastIndexByte(name, '.'); i > 0 {
		return name[:i], name[i+1:]
	}
	return name, ""
}
