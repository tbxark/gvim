package fsops

import (
	"context"
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

// CopyResult carries a non-fatal warning of a successful copy. The only current
// warning is the atomic-publish fallback: a filesystem without hard links falls back to
// direct O_EXCL creation, which can leave a partial target if the write fails afterwards.
type CopyResult struct {
	Warning string
}

// Copy copies src to dst. A file is copied with its permission bits. A
// directory is copied recursively. A symbolic link is recreated as a link with
// the same target, not followed.
//
// If dst does not exist, the copy is made. If dst exists, policy decides; see
// ConflictPolicy. Only the top-level destination is checked: entries created
// inside a new copy never conflict. A free destination is created in place
// (O_EXCL files, so a concurrent creator is never overwritten); a failed
// directory copy removes what it created. With Overwrite, the copy is built in a
// hidden staging directory beside dst, the old dst is renamed to a backup beside
// it, the copy is renamed in, and the backup is removed afterwards, so a failure
// while copying leaves dst unchanged. An error names the backup when it holds the
// old contents: dst is then the new copy (removing the backup failed), or missing
// (the old contents could not be renamed back).
//
// Copy fails with ErrInvalidDest if dst is src, lies inside src, or contains
// src. It also fails with ErrInvalidDest if src is a symbolic link whose target
// is dst, lies inside dst, or is the same file as dst, because an overwrite
// would remove the target. The parent of dst must already exist. src is cleaned
// first, so a trailing separator does not make Copy follow a link.
//
// Copy returns an error only; use CopyWithResult when the caller needs a non-fatal
// publication warning.
func Copy(src, dst string, policy ConflictPolicy) error {
	_, err := CopyWithResult(src, dst, policy)
	return err
}

// CopyWithResult is Copy plus the warning of a non-fatal fallback.
func CopyWithResult(src, dst string, policy ConflictPolicy) (CopyResult, error) {
	return CopyWithResultContext(context.Background(), src, dst, policy)
}

// CopyWithResultContext is CopyWithResult with a cancellable context. The copy checks
// ctx between entries and during file chunks; a cancelled copy removes its temporary
// files and any destination directory it created (cleanup errors are joined).
func CopyWithResultContext(ctx context.Context, src, dst string, policy ConflictPolicy) (CopyResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	report := &copyReport{}
	err := copyWithReport(ctx, src, dst, policy, report)
	return CopyResult{Warning: report.warning}, err
}

func copyWithReport(ctx context.Context, src, dst string, policy ConflictPolicy, report *copyReport) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if policy < Overwrite || policy > Cancel {
		return fmt.Errorf("fsops: copy: unknown conflict policy %d", int(policy))
	}
	src = cleanPath(src)
	srcInfo, err := os.Lstat(src)
	if err != nil {
		return opError("copy", src, err)
	}
	if contains(src, dst) || contains(dst, src) {
		return fmt.Errorf("fsops: copy %s to %s: %w", src, dst, ErrInvalidDest)
	}
	if srcInfo.Mode()&fs.ModeSymlink != 0 {
		onTarget, err := linkTargetIsDest(src, dst)
		if err != nil {
			return opError("copy", src, err)
		}
		if onTarget {
			return fmt.Errorf("fsops: copy link %s onto its target %s: %w", src, dst, ErrInvalidDest)
		}
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
	return place(ctx, src, srcInfo, dst, replace, report)
}

// linkTargetIsDest reports whether the symbolic link src points at dst, at a
// path inside dst, or at the same file as dst. An overwriting Copy removes the
// old dst, which would remove the target too.
//
// The directory holding the link is resolved through symbolic links before the
// relative target is joined to it, and the final target comes from
// EvalSymlinks(src), so the kernel's rules decide where the link leads. A path
// that names dst's entry, or an alias of it, counts. A link that does not
// resolve holds no content, so only a link that names dst is reported then.
func linkTargetIsDest(src, dst string) (bool, error) {
	link, err := os.Readlink(src)
	if err != nil {
		return false, err
	}
	dstPath, err := physicalPath(dst)
	if err != nil {
		// The parent of dst is missing, so the copy fails later with a clearer error.
		return false, nil
	}
	srcDir, err := filepath.EvalSymlinks(filepath.Dir(src))
	if err != nil {
		return false, err
	}
	named := link
	if !filepath.IsAbs(named) {
		named = filepath.Join(srcDir, named)
	}
	if p, err := physicalPath(named); err == nil && p == dstPath {
		return true, nil
	}
	realTarget, err := filepath.EvalSymlinks(src)
	if err != nil {
		return false, nil
	}
	if contains(dstPath, realTarget) {
		return true, nil
	}
	targetInfo, err := os.Stat(realTarget)
	if err != nil {
		return false, nil
	}
	dstInfo, err := os.Lstat(dst)
	return err == nil && os.SameFile(targetInfo, dstInfo), nil
}

// physicalPath returns the absolute form of path with its parent directory
// resolved through symbolic links. The base name is kept, so a symbolic link at
// path itself is not followed.
func physicalPath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	dir, err := filepath.EvalSymlinks(filepath.Dir(abs))
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, filepath.Base(abs)), nil
}

// renamePath renames the entries that an overwriting Copy moves. Tests replace
// it to force a failure.
var renamePath = os.Rename

// place builds a copy of src at dst. A free destination is created in place with
// O_EXCL/O_Mkdir/OSymlink, so a concurrent creator cannot be overwritten (a failed
// copy can leave a partial destination, which the caller reports). An explicit
// overwrite still uses a staging directory and swapIn.
func place(ctx context.Context, src string, srcInfo fs.FileInfo, dst string, replace bool, report *copyReport) error {
	if !replace {
		return copyTree(ctx, src, srcInfo, dst, report)
	}
	stage, err := os.MkdirTemp(filepath.Dir(dst), ".fsops-copy-*")
	if err != nil {
		return opError("copy", dst, err)
	}
	defer func() { _ = os.RemoveAll(stage) }()

	staged := filepath.Join(stage, filepath.Base(dst))
	if err := copyTree(ctx, src, srcInfo, staged, report); err != nil {
		return err
	}
	return swapIn(staged, dst)
}

// swapIn puts the staged copy in place of the existing dst. The existing dst is
// first renamed into a fresh backup directory beside it, so it is not deleted
// before the new copy is in place. If the new copy cannot be renamed in, the
// old dst is renamed back. Once the new copy is in place the backup is removed;
// if that removal fails, the error names the backup path.
func swapIn(staged, dst string) error {
	backupDir, err := os.MkdirTemp(filepath.Dir(dst), ".fsops-backup-*")
	if err != nil {
		return opError("copy", dst, err)
	}
	backup := filepath.Join(backupDir, filepath.Base(dst))
	if err := renamePath(dst, backup); err != nil {
		_ = os.Remove(backupDir)
		return opError("copy", dst, err)
	}
	if err := renamePath(staged, dst); err != nil {
		if restoreErr := renamePath(backup, dst); restoreErr != nil {
			return fmt.Errorf("fsops: copy to %s: %w; the old destination is kept at %s: %w", dst, err, backup, restoreErr)
		}
		_ = os.Remove(backupDir)
		return opError("copy", dst, err)
	}
	if err := os.RemoveAll(backupDir); err != nil {
		return fmt.Errorf("fsops: copy to %s: the new copy is in place, but the old destination could not be removed from %s: %w", dst, backup, err)
	}
	return nil
}

// copyTree copies the entry at src, described by info, to dst, which must not
// exist.
func copyTree(ctx context.Context, src string, info fs.FileInfo, dst string, report *copyReport) error {
	if err := ctx.Err(); err != nil {
		return err
	}
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
		cleanup := func(err error) error {
			if rerr := os.RemoveAll(dst); rerr != nil {
				return errors.Join(err, fmt.Errorf("fsops: copy to %s: remove partial destination: %w", dst, rerr))
			}
			return err
		}
		entries, err := os.ReadDir(src)
		if err != nil {
			return cleanup(opError("copy", src, err))
		}
		for _, e := range entries {
			childInfo, err := e.Info()
			if err != nil {
				return cleanup(opError("copy", filepath.Join(src, e.Name()), err))
			}
			if err := copyTree(ctx, filepath.Join(src, e.Name()), childInfo, filepath.Join(dst, e.Name()), report); err != nil {
				return cleanup(err)
			}
		}
		if err := os.Chmod(dst, mode.Perm()); err != nil {
			return cleanup(opError("copy", dst, err))
		}
		return nil
	case mode.IsRegular():
		return copyFileAtomic(ctx, src, dst, mode.Perm(), report)
	default:
		return fmt.Errorf("fsops: copy %s: unsupported file type %s", src, mode.Type())
	}
}

// copyContextReader makes a chunked copy observe cancellation between reads.
type copyContextReader struct {
	ctx context.Context
	r   io.Reader
}

// copyReadHook, when set by a test, observes each file chunk after it is read.
var copyReadHook func() error

func (c *copyContextReader) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := c.r.Read(p)
	if hook := copyReadHook; hook != nil {
		if herr := hook(); herr != nil {
			return n, herr
		}
	}
	return n, err
}

// copyReport collects non-fatal copy observations.
type copyReport struct {
	warning string
}

func (r *copyReport) fallback(dst string, linkErr error) {
	if r.warning == "" {
		r.warning = fmt.Sprintf("file copy used the O_EXCL fallback at %s (hard-link publish failed: %v); a failed write can leave a partial target", dst, linkErr)
	}
}

// linkCopyFile publishes a completed temporary file. Tests replace it to exercise the
// no-hard-link fallback.
var linkCopyFile = os.Link

// copyFileWriteHook, when set by a test, fails a copy after the temporary file is
// complete but before it is published.
var copyFileWriteHook func() error

// copyFileAtomic writes src into a unique 0600 temporary file in dst's directory, applies
// perm, then publishes it with a no-clobber hard link. A failed write removes the
// temporary file and leaves no target, so a retry is not a conflict. If the filesystem
// cannot hard-link, it records the fallback and uses the older direct O_EXCL write (whose
// partial-target risk is the recorded warning).
func copyFileAtomic(ctx context.Context, src, dst string, perm fs.FileMode, report *copyReport) (err error) {
	in, err := os.Open(src)
	if err != nil {
		return opError("copy", src, err)
	}
	defer func() { _ = in.Close() }()

	temp, err := os.CreateTemp(filepath.Dir(dst), ".fsops-copy-*")
	if err != nil {
		return opError("copy", dst, err)
	}
	tempName := temp.Name()
	cleanup := func() error {
		if rerr := os.Remove(tempName); rerr != nil && !errors.Is(rerr, os.ErrNotExist) {
			return fmt.Errorf("fsops: copy to %s: remove temporary file %s: %w", dst, tempName, rerr)
		}
		return nil
	}
	defer func() {
		if cerr := cleanup(); cerr != nil {
			err = errors.Join(err, cerr)
		}
	}()

	if _, err = io.Copy(temp, &copyContextReader{ctx: ctx, r: in}); err != nil {
		_ = temp.Close()
		return opError("copy", src, err)
	}
	if err = temp.Chmod(perm); err != nil {
		_ = temp.Close()
		return opError("copy", dst, err)
	}
	if err = temp.Close(); err != nil {
		return opError("copy", dst, err)
	}
	if hook := copyFileWriteHook; hook != nil {
		if err = hook(); err != nil {
			return err
		}
	}
	linkErr := linkCopyFile(tempName, dst)
	if linkErr == nil {
		return nil
	}
	if errors.Is(linkErr, fs.ErrExist) || os.IsExist(linkErr) {
		return opError("copy", dst, linkErr)
	}
	// No usable hard link in this directory/filesystem: publish the bytes with the
	// direct O_EXCL write. The report names the fallback so the UI can show its risk.
	report.fallback(dst, linkErr)
	if err = cleanup(); err != nil {
		return err
	}
	if err = copyFileDirect(ctx, src, dst, perm); err != nil {
		return err
	}
	return nil
}

// copyFileDirect is the fallback: it creates dst with O_EXCL and writes into that target
// directly. A failure can leave a partial target; only the no-hard-link fallback uses it.
func copyFileDirect(ctx context.Context, src, dst string, perm fs.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return opError("copy", src, err)
	}
	defer func() { _ = in.Close() }()

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
	if err != nil {
		return opError("copy", dst, err)
	}
	if _, err := io.Copy(out, &copyContextReader{ctx: ctx, r: in}); err != nil {
		_ = out.Close()
		return opError("copy", src, err)
	}
	if err := out.Close(); err != nil {
		return opError("copy", dst, err)
	}
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
