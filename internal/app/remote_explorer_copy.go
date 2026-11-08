package app

// Remote explorer subtree transfer (task-148): Copy and Move of a remote file or
// directory through the workspace authority. The recursive walk is bounded (entries,
// total bytes, depth), refuses symbolic links and invalid entries, checks cancellation
// between every entry, and writes each destination file atomically; a cancellation or
// failure in the write phase keeps the partial destination and says so (there is no
// rollback). Move/cut copies first and removes the source only after the copy fully
// succeeded. A same-directory copy/duplicate uses Fresh's "name copy[.ext]" naming
// rule; a paste onto an existing destination in another directory is refused (remote
// paste never overwrites). Nothing here touches the local file system: every read and
// write goes through authority.FileSystem/FileMutations, and every operation runs the
// workspace trust gate first.

import (
	"context"
	"fmt"
	"path"
	"sort"
	"strings"
	"time"

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
	rel  string
	kind string
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
// also Duplicate); a copy into another directory keeps the name and is REFUSED when
// that destination already exists (no overwrite, no ErrConflict prompt).
func (o *remoteExplorerOps) Copy(ctx context.Context, src, dstDir string, _ expanel.Conflict) (string, error) {
	if err := o.check(); err != nil {
		return "", err
	}
	if err := o.copyEndpoints(src, dstDir); err != nil {
		return "", err
	}
	opCtx, cancel := o.copyContext(ctx)
	defer cancel()
	if err := o.gateOp(opCtx, "copy", src); err != nil {
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
	name := path.Base(src)
	if path.Clean(path.Dir(src)) == path.Clean(dstDir) {
		name, err = o.uniqueCopyName(opCtx, dstDir, name)
		if err != nil {
			return "", err
		}
	} else {
		if ok, _, err := o.exists(opCtx, path.Join(dstDir, name)); err != nil {
			return "", fmt.Errorf("remote file operation: stat %s: %w", path.Join(dstDir, name), err)
		} else if ok {
			return "", fmt.Errorf("remote paste refused: %s already exists (remote paste never overwrites)", path.Join(dstDir, name))
		}
	}
	dst := path.Join(dstDir, name)
	limits := remoteCopyDefaultLimits
	nodes, err := o.planCopy(opCtx, src, st, limits)
	if err != nil {
		return "", err
	}
	written, err := o.writeCopy(opCtx, src, dst, nodes, limits)
	if err != nil {
		return "", &remoteCopyPartialError{dst: dst, files: written, err: err}
	}
	return dst, nil
}

// Move cuts src into dstDir: it copies the subtree first and removes the source only
// after the copy fully succeeded. A move into the source's own directory is a no-op
// (matching the local explorer; the root then cancels the cut). A destination that
// already exists is refused, never overwritten.
func (o *remoteExplorerOps) Move(ctx context.Context, src, dstDir string) (string, error) {
	if err := o.check(); err != nil {
		return "", err
	}
	if err := o.copyEndpoints(src, dstDir); err != nil {
		return "", err
	}
	if path.Clean(path.Dir(src)) == path.Clean(dstDir) {
		return src, nil
	}
	opCtx, cancel := o.copyContext(ctx)
	defer cancel()
	if err := o.gateOp(opCtx, "move", src); err != nil {
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
		return "", fmt.Errorf("remote file operation: %s is a symbolic link; remote move does not follow links", src)
	}
	dst := path.Join(dstDir, path.Base(src))
	if ok, _, err := o.exists(opCtx, dst); err != nil {
		return "", fmt.Errorf("remote file operation: stat %s: %w", dst, err)
	} else if ok {
		return "", fmt.Errorf("remote paste refused: %s already exists (remote paste never overwrites)", dst)
	}
	limits := remoteCopyDefaultLimits
	nodes, err := o.planCopy(opCtx, src, st, limits)
	if err != nil {
		return "", err
	}
	written, err := o.writeCopy(opCtx, src, dst, nodes, limits)
	if err != nil {
		return "", &remoteCopyPartialError{dst: dst, files: written, err: err}
	}
	// The copy is complete: only now is the source removed.
	if err := o.muts.Remove(opCtx, src, st.Kind == "dir"); err != nil {
		return "", fmt.Errorf("remote move: copied %s to %s but removing the source failed: %w; the copy is kept", src, dst, err)
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
	nodes := []remoteCopyNode{{rel: "", kind: st.Kind}}
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
			count++
			if count > limits.MaxEntries {
				return fmt.Errorf("remote copy refused: the subtree has more than %d entries", limits.MaxEntries)
			}
			switch e.Kind {
			case "dir":
				if depth+1 > limits.MaxDepth {
					return fmt.Errorf("remote copy refused: the subtree is deeper than %d directories", limits.MaxDepth)
				}
				nodes = append(nodes, remoteCopyNode{rel: childRel, kind: "dir"})
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
				nodes = append(nodes, remoteCopyNode{rel: childRel, kind: "file"})
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

// writeCopy creates the planned destination and writes every file atomically. It
// returns how many files were written; on error the caller keeps the partial
// destination and reports it (no rollback).
func (o *remoteExplorerOps) writeCopy(ctx context.Context, src, dst string, nodes []remoteCopyNode, limits remoteCopyLimits) (int, error) {
	limits = limits.bounded()
	written := 0
	bytes := int64(0)
	for _, node := range nodes {
		if err := ctx.Err(); err != nil {
			return written, err
		}
		to := dst
		if node.rel != "" {
			to = path.Join(dst, node.rel)
		}
		if node.kind == "dir" {
			if err := o.muts.Mkdir(ctx, to, false); err != nil {
				return written, fmt.Errorf("create directory %s: %w", to, err)
			}
			continue
		}
		from := src
		if node.rel != "" {
			from = path.Join(src, node.rel)
		}
		data, err := o.files.ReadFile(ctx, from, limits.MaxBytes+1)
		if err != nil {
			return written, fmt.Errorf("read %s: %w", from, err)
		}
		if int64(len(data)) > limits.MaxBytes {
			return written, fmt.Errorf("read %s: larger than the %d-byte copy bound", from, limits.MaxBytes)
		}
		bytes += int64(len(data))
		if bytes > limits.MaxBytes {
			return written, fmt.Errorf("the copy is larger than the %d-byte copy bound", limits.MaxBytes)
		}
		if err := o.files.WriteFile(ctx, to, data, hostos.WriteOptions{Atomic: true, Parents: true}); err != nil {
			return written, fmt.Errorf("write %s: %w", to, err)
		}
		written++
	}
	return written, nil
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
