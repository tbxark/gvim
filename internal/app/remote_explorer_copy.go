package app

// Remote explorer subtree transfer (task-148): Copy and Move of a remote file or
// directory through the workspace authority. The recursive walk is bounded (entries,
// total bytes, depth), refuses symbolic links and invalid entries, checks cancellation
// between every entry, and writes each destination file atomically; a cancellation or
// failure in the write phase keeps the partial destination and says so (there is no
// rollback). Move/cut copies first and removes the source only after the copy fully
// succeeded. A same-directory copy/duplicate uses Fresh's "name copy[.ext]" naming
// rule. A destination that exists follows the explicit conflict choice: cancel reports
// ErrConflict, skip leaves it, and overwrite (the only path that replaces) removes the
// old target first; the exclusive no-replace path is used for every other choice. Nothing here touches the local file system: every read and
// write goes through authority.FileSystem/FileMutations, and every operation runs the
// workspace trust gate first.

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/tbxark/gvim/authority"
	"github.com/tbxark/gvim/fsops"
	expanel "github.com/tbxark/gvim/internal/tui/explorer"
	"github.com/tbxark/gvim/plugin/hostos"
)

const (
	// remoteCopyTimeout bounds one subtree transfer; a large bounded copy over SSH is
	// slower than a single mutation.
	remoteCopyTimeout = 2 * time.Minute
	// Bounds of one remote copy: a subtree with more entries, bytes or depth is refused
	// before the destination is created.
	remoteCopyMaxEntries = 10000
	remoteCopyMaxBytes   = 256 << 20
	remoteCopyMaxDepth   = 64
	// remoteCopyNameAttempts bounds Fresh's "name copy N" search.
	remoteCopyNameAttempts = 1000
)

// remoteCopyLimits is one copy's bounds; the zero value fields are replaced by the
// package defaults, and tests pass smaller values.
type remoteCopyLimits struct {
	MaxEntries int
	MaxBytes   int64
	MaxDepth   int
}

// remoteCopyDefaultLimits are the production bounds.
var remoteCopyDefaultLimits = remoteCopyLimits{
	MaxEntries: remoteCopyMaxEntries,
	MaxBytes:   remoteCopyMaxBytes,
	MaxDepth:   remoteCopyMaxDepth,
}

// bounded fills in zero fields from the defaults.
func (l remoteCopyLimits) bounded() remoteCopyLimits {
	if l.MaxEntries <= 0 {
		l.MaxEntries = remoteCopyDefaultLimits.MaxEntries
	}
	if l.MaxBytes <= 0 {
		l.MaxBytes = remoteCopyDefaultLimits.MaxBytes
	}
	if l.MaxDepth <= 0 {
		l.MaxDepth = remoteCopyDefaultLimits.MaxDepth
	}
	return l
}

// remoteCopyNode is one planned entry of a subtree copy: rel is "" for the source
// itself, otherwise its slash-relative path under the source.
type remoteCopyNode struct {
	rel   string
	kind  string
	mode  fs.FileMode // source permission bits, preserved best-effort
	mtime time.Time   // source modification time, preserved best-effort
}

// remoteCopyPartialError reports a failure after the destination may hold part of the
// copy. It is deliberately explicit: gvim keeps the partial destination (it never
// deletes remote data on a failed copy) and the notice says where it is.
type remoteCopyPartialError struct {
	dst   string
	files int
	err   error
}

func (e *remoteCopyPartialError) Error() string {
	return fmt.Sprintf("remote copy to %s failed after %d files: %v; the partial destination is kept", e.dst, e.files, e.err)
}

func (e *remoteCopyPartialError) Unwrap() error { return e.err }

// copyContext bounds a subtree transfer; the caller's context still cancels it.
func (o *remoteExplorerOps) copyContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, remoteCopyTimeout)
}

// Copy copies the remote file/dir src into dstDir and returns the new path. A copy
// within the source's own directory gets Fresh's free "name copy[.ext]" name (this is
// also Duplicate). A paste into another directory keeps the name; an existing destination
// follows the explorer's conflict choice: cancel reports ErrConflict (the root then offers
// overwrite/rename), skip leaves it alone and returns its path, overwrite removes it first
// (there is no rollback after that removal; that explicit overwrite is non-atomic, and a
// concurrent re-creation can be replaced).
func (o *remoteExplorerOps) Copy(ctx context.Context, src, dstDir string, c expanel.Conflict) (string, error) {
	return o.transfer(ctx, src, dstDir, "", false, c, "copy")
}

// CopyAs copies src into dstDir under name, for a conflict prompt's rename choice. It
// validates name as one path element and uses the same conflict policy as Copy.
func (o *remoteExplorerOps) CopyAs(ctx context.Context, src, dstDir, name string, c expanel.Conflict) (string, error) {
	if name == "" {
		return "", fmt.Errorf("remote copy: empty destination name")
	}
	return o.transfer(ctx, src, dstDir, name, false, c, "copy")
}

// Move cuts src into dstDir: it copies the subtree first and removes the source only
// after the copy fully succeeded. A move into the source's own directory is a no-op
// (matching the local explorer; the root then cancels the cut). A destination that exists
// follows the conflict choice: cancel reports ErrConflict, skip leaves both alone and
// returns the source (the root cancels the cut), overwrite removes the destination first
// (no rollback; documented race).
func (o *remoteExplorerOps) Move(ctx context.Context, src, dstDir string) (string, error) {
	return o.transfer(ctx, src, dstDir, "", true, expanel.ConflictCancel, "move")
}

// MoveAs is Move with a conflict policy and an optional destination name: an empty name
// keeps the source's name (the overwrite choice) and a name is the conflict prompt's rename
// choice.
func (o *remoteExplorerOps) MoveAs(ctx context.Context, src, dstDir, name string, c expanel.Conflict) (string, error) {
	return o.transfer(ctx, src, dstDir, name, true, c, "move")
}

// remotePasteConflictError is a paste destination that already exists. It matches
// explorer.ErrConflict, so the root can offer overwrite/rename/cancel instead of failing.
type remotePasteConflictError struct{ dst string }

func (e *remotePasteConflictError) Error() string {
	return "remote destination " + e.dst + " already exists"
}
func (e *remotePasteConflictError) Is(target error) bool { return target == expanel.ErrConflict }

// errRemoteExplorerNoExclusiveCopy refuses a paste when the backend has no atomic
// no-replace destination surface: a Stat + copy/write fallback would reopen the race.
var errRemoteExplorerNoExclusiveCopy = errors.New("this backend cannot create a copy destination exclusively; refusing to race an existing target")

// transfer is the shared Copy/CopyAs/Move/MoveAs implementation.
func (o *remoteExplorerOps) transfer(ctx context.Context, src, dstDir, name string, move bool, c expanel.Conflict, gateOp string) (string, error) {
	if err := o.check(); err != nil {
		return "", err
	}
	if err := o.copyEndpoints(src, dstDir); err != nil {
		return "", err
	}
	sameDir := path.Clean(path.Dir(src)) == path.Clean(dstDir)
	if move && sameDir && name == "" {
		return src, nil
	}
	opCtx, cancel := o.copyContext(ctx)
	defer cancel()
	if err := o.gateOp(opCtx, gateOp, src); err != nil {
		return "", err
	}
	ok, st, err := o.exists(opCtx, src)
	if err != nil {
		return "", fmt.Errorf("remote file operation: stat %s: %w", src, err)
	}
	if !ok {
		return "", fmt.Errorf("remote file operation: %s does not exist", src)
	}
	if sym, err := o.symlink(opCtx, src, st); err != nil {
		return "", err
	} else if sym {
		return "", fmt.Errorf("remote file operation: %s is a symbolic link; remote copy does not follow links", src)
	}
	if name != "" {
		if err := fsops.Validate(dstDir, name); err != nil {
			return "", fmt.Errorf("remote copy: %w", err)
		}
	}
	if name == "" && sameDir && !move {
		name, err = o.uniqueCopyName(opCtx, dstDir, path.Base(src))
		if err != nil {
			return "", err
		}
	} else {
		if name == "" {
			name = path.Base(src)
		}
		dst := path.Join(dstDir, name)
		if path.Clean(dst) == path.Clean(src) {
			return "", fmt.Errorf("remote copy: source and destination are the same: %s", src)
		}
		dstOK, dstStat, err := o.exists(opCtx, dst)
		if err != nil {
			return "", fmt.Errorf("remote file operation: stat %s: %w", dst, err)
		}
		if dstOK {
			switch c {
			case expanel.ConflictCancel:
				return "", &remotePasteConflictError{dst: dst}
			case expanel.ConflictSkip:
				if move {
					return src, nil
				}
				return dst, nil
			case expanel.ConflictOverwrite:
				if err := o.muts.Remove(opCtx, dst, dstStat.Kind == "dir"); err != nil {
					return "", fmt.Errorf("remote paste overwrite: remove %s: %w", dst, err)
				}
			default:
				return "", fmt.Errorf("remote paste: unknown conflict choice %d", int(c))
			}
		}
	}
	dst := path.Join(dstDir, name)
	limits := remoteCopyDefaultLimits
	nodes, err := o.planCopy(opCtx, src, st, limits)
	if err != nil {
		return "", err
	}
	if c != expanel.ConflictOverwrite {
		if err := o.requireExclusiveCopy(dst, nodes); err != nil {
			return "", err
		}
	}
	written, started, err := o.writeCopy(opCtx, src, dst, nodes, limits, c != expanel.ConflictOverwrite)
	if err != nil {
		if errors.Is(err, expanel.ErrConflict) {
			return "", err
		}
		if !started {
			return "", err
		}
		return "", &remoteCopyPartialError{dst: dst, files: written, err: err}
	}
	if move {
		if err := o.muts.Remove(opCtx, src, st.Kind == "dir"); err != nil {
			return "", fmt.Errorf("remote move: copied %s to %s but removing the source failed: %w; the copy is kept", src, dst, err)
		}
	}
	return dst, nil
}

// symlink reports whether path is a symbolic link. Stat follows a link, so the
// no-follow answer comes from the parent listing (the authority has no Lstat); a broken
// link is already reported as a symlink by Stat.
func (o *remoteExplorerOps) symlink(ctx context.Context, p string, st hostos.FileStat) (bool, error) {
	if st.Kind == "symlink" {
		return true, nil
	}
	entries, err := o.files.ReadDir(ctx, path.Dir(p), true)
	if err != nil {
		return false, fmt.Errorf("remote file operation: list %s: %w", path.Dir(p), err)
	}
	name := path.Base(p)
	for _, e := range entries {
		if e.Name == name {
			return e.Kind == "symlink", nil
		}
	}
	// Not found in the parent listing (a race or a backend that hides it): Stat's
	// answer stands.
	return false, nil
}

// copyEndpoints validates the source and destination of a copy/move: both inside the
// remote workspace, the workspace root itself not a source, and the destination not
// inside the source (a subtree cannot be pasted into itself).
func (o *remoteExplorerOps) copyEndpoints(src, dstDir string) error {
	if !o.inside(src) {
		return fmt.Errorf("remote file operation: %s is outside the remote workspace", src)
	}
	if !o.inside(dstDir) {
		return fmt.Errorf("remote file operation: %s is outside the remote workspace", dstDir)
	}
	if src == path.Clean(o.root) {
		return fmt.Errorf("remote file operation: the workspace root cannot be copied or moved")
	}
	if dstDir == src || strings.HasPrefix(dstDir, src+"/") {
		return fmt.Errorf("remote paste refused: the destination %s is inside the source %s", dstDir, src)
	}
	return nil
}

// planCopy walks the source subtree and validates the bounds, symlinks and entry names
// BEFORE anything is created at the destination. The returned plan is parent-first.
func (o *remoteExplorerOps) planCopy(ctx context.Context, src string, st hostos.FileStat, limits remoteCopyLimits) ([]remoteCopyNode, error) {
	limits = limits.bounded()
	if st.Kind == "symlink" {
		return nil, fmt.Errorf("remote copy refused: %s is a symbolic link", src)
	}
	nodes := []remoteCopyNode{{rel: "", kind: st.Kind, mode: st.Mode, mtime: st.MTime}}
	count, bytes := 1, int64(0)
	if st.Kind == "file" {
		if st.Size > limits.MaxBytes {
			return nil, fmt.Errorf("remote copy refused: %s is larger than the %d-byte copy bound", src, limits.MaxBytes)
		}
		return nodes, nil
	}
	if st.Kind != "dir" {
		return nil, fmt.Errorf("remote copy refused: %s is not a file or directory", src)
	}
	var walk func(dir, rel string, depth int) error
	walk = func(dir, rel string, depth int) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		entries, err := o.files.ReadDir(ctx, dir, true)
		if err != nil {
			return fmt.Errorf("remote copy: list %s: %w", dir, err)
		}
		list := append([]hostos.DirEntry(nil), entries...)
		sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })
		for _, e := range list {
			if err := ctx.Err(); err != nil {
				return err
			}
			if e.Name == "" || e.Name == "." || e.Name == ".." || strings.ContainsAny(e.Name, "/\x00") {
				return fmt.Errorf("remote copy refused: invalid entry name %q in %s", e.Name, dir)
			}
			childRel := path.Join(rel, e.Name)
			if e.Kind == "symlink" {
				return fmt.Errorf("remote copy refused: %s contains a symbolic link (%s); remote copy does not follow links", src, e.Name)
			}
			childMode, childTime := fs.FileMode(0), e.MTime
			if cs, serr := o.files.Stat(ctx, path.Join(dir, e.Name)); serr == nil {
				// Best effort: metadata is a nice-to-have, and a backend that cannot
				// stat an entry still copies its bytes.
				childMode, childTime = cs.Mode, cs.MTime
			}
			count++
			if count > limits.MaxEntries {
				return fmt.Errorf("remote copy refused: the subtree has more than %d entries", limits.MaxEntries)
			}
			switch e.Kind {
			case "dir":
				if depth+1 > limits.MaxDepth {
					return fmt.Errorf("remote copy refused: the subtree is deeper than %d directories", limits.MaxDepth)
				}
				nodes = append(nodes, remoteCopyNode{rel: childRel, kind: "dir", mode: childMode, mtime: childTime})
				if err := walk(path.Join(dir, e.Name), childRel, depth+1); err != nil {
					return err
				}
			case "file":
				if e.Size > limits.MaxBytes {
					return fmt.Errorf("remote copy refused: %s is larger than the %d-byte copy bound", path.Join(dir, e.Name), limits.MaxBytes)
				}
				bytes += e.Size
				if bytes > limits.MaxBytes {
					return fmt.Errorf("remote copy refused: the subtree is larger than the %d-byte copy bound", limits.MaxBytes)
				}
				nodes = append(nodes, remoteCopyNode{rel: childRel, kind: "file", mode: childMode, mtime: childTime})
			default:
				return fmt.Errorf("remote copy refused: %s is not a file or directory", path.Join(dir, e.Name))
			}
		}
		return nil
	}
	if err := walk(src, "", 0); err != nil {
		return nil, err
	}
	return nodes, nil
}

// requireExclusiveCopy checks the optional no-replace surfaces before anything is
// created. A backend without them refuses the paste instead of falling back to the
// check-to-create race.
func (o *remoteExplorerOps) requireExclusiveCopy(dst string, nodes []remoteCopyNode) error {
	content := authority.ContentCreators(o.files)
	dirs := authority.DirectoryCreators(o.files)
	for _, node := range nodes {
		switch node.kind {
		case "dir":
			if dirs == nil {
				return fmt.Errorf("remote copy: %w: %s", errRemoteExplorerNoExclusiveCopy, dst)
			}
		case "file":
			if content == nil {
				return fmt.Errorf("remote copy: %w: %s", errRemoteExplorerNoExclusiveCopy, dst)
			}
		}
	}
	return nil
}

// writeCopy publishes the planned destination. exclusive uses DirectoryCreator.MkdirNew
// and FileContentCreator.CreateNewWithContent, so the top-level destination has one
// atomic winner and a concurrent creator gets the conflict flow; the explicit-overwrite
// path removes the old destination first and uses Mutations.Mkdir/WriteFile(Atomic: true)
// (non-atomic by user choice). It returns how many files were written, whether the
// top-level destination was started, and the error; on a started error the caller keeps
// the partial destination and reports it (no rollback).
func (o *remoteExplorerOps) writeCopy(ctx context.Context, src, dst string, nodes []remoteCopyNode, limits remoteCopyLimits, exclusive bool) (int, bool, error) {
	limits = limits.bounded()
	written := 0
	started := false
	bytes := int64(0)
	dirs := authority.DirectoryCreators(o.files)
	content := authority.ContentCreators(o.files)
	for _, node := range nodes {
		if err := ctx.Err(); err != nil {
			return written, started, err
		}
		to := dst
		if node.rel != "" {
			to = path.Join(dst, node.rel)
		}
		if node.kind == "dir" {
			var err error
			if exclusive {
				err = dirs.MkdirNew(ctx, to)
			} else {
				err = o.muts.Mkdir(ctx, to, false)
			}
			if err != nil {
				if exclusive && node.rel == "" && errors.Is(err, fs.ErrExist) {
					return written, started, &remotePasteConflictError{dst: to}
				}
				if errors.Is(err, authority.ErrUnsupportedFileIO) {
					return written, started, fmt.Errorf("remote copy: %w: %s", errRemoteExplorerNoExclusiveCopy, to)
				}
				return written, started, fmt.Errorf("create directory %s: %w", to, err)
			}
			if node.rel == "" {
				started = true
			}
			continue
		}
		from := src
		if node.rel != "" {
			from = path.Join(src, node.rel)
		}
		data, err := o.files.ReadFile(ctx, from, limits.MaxBytes+1)
		if err != nil {
			return written, started, fmt.Errorf("read %s: %w", from, err)
		}
		if int64(len(data)) > limits.MaxBytes {
			return written, started, fmt.Errorf("read %s: larger than the %d-byte copy bound", from, limits.MaxBytes)
		}
		bytes += int64(len(data))
		if bytes > limits.MaxBytes {
			return written, started, fmt.Errorf("the copy is larger than the %d-byte copy bound", limits.MaxBytes)
		}
		if exclusive {
			err = content.CreateNewWithContent(ctx, to, data, hostos.WriteOptions{Atomic: true, Mode: node.mode})
		} else {
			err = o.files.WriteFile(ctx, to, data, hostos.WriteOptions{Atomic: true, Parents: true, Mode: node.mode})
		}
		if err != nil {
			if exclusive && node.rel == "" && errors.Is(err, fs.ErrExist) {
				return written, started, &remotePasteConflictError{dst: to}
			}
			if errors.Is(err, authority.ErrUnsupportedFileIO) {
				return written, started, fmt.Errorf("remote copy: %w: %s", errRemoteExplorerNoExclusiveCopy, to)
			}
			return written, started, fmt.Errorf("write %s: %w", to, err)
		}
		if node.rel == "" {
			started = true
		}
		written++
	}
	o.applyCopyMetadata(ctx, dst, nodes)
	return written, started, nil
}

// applyCopyMetadata restores the planned permission bits and mtimes after the bytes are
// complete, best effort: files first, then directories in reverse (a parent's mtime must
// be the last thing set). A backend without authority.FileMetadata keeps the bytes-only
// copy and is not claimed to preserve metadata. Any failure is collected into the
// non-fatal copy warning the TUI surfaces; it never turns a completed copy into an error.
func (o *remoteExplorerOps) applyCopyMetadata(ctx context.Context, dst string, nodes []remoteCopyNode) {
	meta := authority.Metadata(o.files)
	if meta == nil {
		return
	}
	failed, total := 0, 0
	var firstErr error
	for i := len(nodes) - 1; i >= 0; i-- {
		node := nodes[i]
		if node.mode == 0 && node.mtime.IsZero() {
			continue
		}
		to := dst
		if node.rel != "" {
			to = path.Join(dst, node.rel)
		}
		total++
		if err := meta.SetMetadata(ctx, to, node.mode, node.mtime); err != nil {
			failed++
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	if failed > 0 {
		o.setCopyWarning(appTextVars("gvim.notice.copy_metadata_warning", "copy completed, but permissions/mtime were not preserved on %{failed} of %{total} entries: %{error}", map[string]any{"failed": failed, "total": total, "error": firstErr}))
	}
}

// uniqueCopyName returns Fresh's free duplicate name for name in dir: "name copy.ext",
// then "name copy 2.ext", and so on, checked against the remote directory.
func (o *remoteExplorerOps) uniqueCopyName(ctx context.Context, dir, name string) (string, error) {
	if err := fsops.Validate(dir, name); err != nil {
		return "", err
	}
	stem, ext := splitCopyStemExt(name)
	for n := 1; n <= remoteCopyNameAttempts; n++ {
		candidate := copyNameCandidate(stem, ext, n)
		ok, _, err := o.exists(ctx, path.Join(dir, candidate))
		if err != nil {
			return "", fmt.Errorf("remote copy: stat %s: %w", path.Join(dir, candidate), err)
		}
		if !ok {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("remote copy: no free copy name for %s in %s after %d attempts", name, dir, remoteCopyNameAttempts)
}

// copyNameCandidate builds the n-th duplicate name (n = 1 has no number).
func copyNameCandidate(stem, ext string, n int) string {
	suffix := " copy"
	if n > 1 {
		suffix = fmt.Sprintf(" copy %d", n)
	}
	if ext == "" {
		return stem + suffix
	}
	return stem + suffix + "." + ext
}

// splitCopyStemExt splits at the last dot; a leading dot does not start an extension
// (the same rule as fsops.CopyName).
func splitCopyStemExt(name string) (stem, ext string) {
	if i := strings.LastIndexByte(name, '.'); i > 0 {
		return name[:i], name[i+1:]
	}
	return name, ""
}
