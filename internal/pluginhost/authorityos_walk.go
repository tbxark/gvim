package pluginhost

// Remote Walk and Copy over the authority's bounded file surface. The authority exposes
// ReadDir/Stat/ReadFile/WriteFile and (optionally) the FileMutations surface, but no
// readlink/streaming primitive, so these are deliberately bounded re-implementations:
//   - Walk is a depth-first, lexical-order walk with the local semantics for hidden
//     entries, skipped directories, MaxDepth and MaxEntries; symlinks are reported but
//     never followed; unreadable subdirectories are skipped; cancellation is checked
//     throughout. It reads the remote directory entries, never the local filesystem.
//   - Copy copies a regular file, or a directory tree, entirely remote-to-remote. Every
//     file is copied through the authority's atomic streamed-write surface; a backend
//     without it gets a clear unsupported error before reading/creating/writing the
//     destination (there is no same-handle exclusive create+write fallback). Symlinks in
//     a tree are refused (there is no remote readlink primitive, so copying them as links
//     is impossible and silently dropping them would be wrong), and the tree is bounded
//     by copyTreeMax entries. Copy also requires RealPather to prove source/destination
//     safety and DirectoryCreator for a tree; a backend without those capabilities gets a
//     clear unsupported error instead of guessing or falling back locally.
//
// A cancelled context stops the walk/copy at the next check and is mapped to the plugin
// API's cancelled code; no operation ever falls back to the local machine.

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"path"
	"sort"
	"strings"

	"github.com/tbxark/gvim/authority"
	"github.com/tbxark/gvim/plugin/hostos"
	"github.com/tbxark/gvim/pluginapi"
)

const (
	// remoteWalkDefaultEntries matches the local Walk's default when MaxEntries is zero.
	remoteWalkDefaultEntries = 50_000
	// remoteCopyChunk is the bounded ReadRange window of a streamed remote copy, matching
	// the authority's windowed-read cap.
	remoteCopyChunk = 1 << 20
)

// RealPath resolves symbolic links on the workspace machine through the authority's
// optional RealPather capability (local: EvalSymlinks; SSH: one realpath/readlink -f
// helper). A backend without the capability gets a clear unsupported error; the local
// machine's OS is never consulted and the returned path is a remote absolute path.
func (o *authorityOS) RealPath(ctx context.Context, p string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", mapErr(err, "")
	}
	rp, ok := authority.RealPathResolver(o.files())
	if !ok {
		return "", pluginapi.Errf(pluginapi.CodeUnsupported,
			"%s: the workspace authority has no realpath capability", p)
	}
	real, err := rp.RealPath(ctx, o.resolve(p))
	if err != nil {
		return "", mapErr(err, p)
	}
	if real == "" {
		return "", pluginapi.Errf(pluginapi.CodeIO, "%s: the remote realpath returned no path", p)
	}
	return real, nil
}

// Walk implements hostos.OS over the authority's ReadDir.
func (o *authorityOS) Walk(ctx context.Context, root string, opts hostos.WalkOptions) (hostos.WalkResult, error) {
	if err := ctx.Err(); err != nil {
		return hostos.WalkResult{}, mapErr(err, "")
	}
	base := o.resolve(root)
	if base == "" {
		return hostos.WalkResult{}, pluginapi.Errf(pluginapi.CodeInvalid, "walk: empty root")
	}
	var res hostos.WalkResult
	if _, err := o.walkInto(ctx, base, "", opts, 1, &res, true); err != nil {
		return hostos.WalkResult{}, mapErr(err, root)
	}
	return res, nil
}

// walkInto appends the entries below dir (depth 1 = a direct child of the root) to res.
// It returns stop=true when MaxEntries was reached (res.Truncated is set). A subdirectory
// that cannot be listed is skipped, as the local Walk does; a root that cannot be listed
// is an error.
func (o *authorityOS) walkInto(ctx context.Context, dir, relBase string, opts hostos.WalkOptions, depth int, res *hostos.WalkResult, isRoot bool) (bool, error) {
	entries, err := o.files().ReadDir(ctx, dir, true)
	if err != nil {
		if isRoot || ctx.Err() != nil {
			return false, err
		}
		return false, nil
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	limit := opts.MaxEntries
	if limit <= 0 {
		limit = remoteWalkDefaultEntries
	}
	skip := make(map[string]bool, len(opts.SkipDirs))
	for _, s := range opts.SkipDirs {
		skip[s] = true
	}
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		if !opts.Hidden && strings.HasPrefix(entry.Name, ".") {
			continue
		}
		if entry.Kind == "dir" && skip[entry.Name] {
			continue
		}
		if opts.MaxDepth > 0 && depth > opts.MaxDepth {
			continue
		}
		if len(res.Entries) >= limit {
			res.Truncated = true
			return true, nil
		}
		rel := entry.Name
		if relBase != "" {
			rel = path.Join(relBase, entry.Name)
		}
		res.Entries = append(res.Entries, hostos.WalkEntry{
			Path: path.Join(dir, entry.Name), Rel: rel, Kind: entry.Kind,
			Size: entry.Size, MTime: entry.MTime,
		})
		if entry.Kind == "dir" && (opts.MaxDepth <= 0 || depth < opts.MaxDepth) {
			stop, err := o.walkInto(ctx, path.Join(dir, entry.Name), rel, opts, depth+1, res, false)
			if err != nil {
				return false, err
			}
			if stop {
				return true, nil
			}
		}
	}
	return false, nil
}

// Copy implements hostos.OS: one regular file, or a directory tree, copied entirely
// through the authority. Symbolic links are refused for trees (no remote readlink); the
// destination directory must not exist; a directory copy into itself is refused.
//
// A copy is a data-safety operation, so the authority must be able to prove where the
// write lands: both paths are canonicalized through the RealPather capability before any
// write, and a backend without that capability (or whose realpath answer cannot be
// trusted absolute) returns unsupported. A directory tree additionally needs the
// DirectoryCreator atomic destination claim; a backend without it returns unsupported
// instead of falling back to a check-then-mkdir merge. The tree entry cap is never used
// as a substitute for this safety check.
func (o *authorityOS) Copy(ctx context.Context, from, to string) error {
	if err := ctx.Err(); err != nil {
		return mapErr(err, "")
	}
	src, dst := o.resolve(from), o.resolve(to)
	if src == "" || dst == "" {
		return pluginapi.Errf(pluginapi.CodeInvalid, "copy: empty path")
	}
	if src == dst {
		return pluginapi.Errf(pluginapi.CodeInvalid, "%s: source and destination are the same path", src)
	}
	st, err := o.files().Stat(ctx, src)
	if err != nil {
		return mapErr(err, from)
	}
	switch st.Kind {
	case "dir", "file":
	default:
		return pluginapi.Errf(pluginapi.CodeInvalid, "%s: not a regular file (remote copy supports files and directory trees)", from)
	}
	srcReal, dstReal, err := o.copyCanonicalPaths(ctx, src, dst)
	if err != nil {
		return err
	}
	if srcReal == dstReal {
		return pluginapi.Errf(pluginapi.CodeInvalid, "%s: source and destination are the same path", from)
	}
	if st.Kind == "dir" {
		// The filesystem root is a prefix of every absolute destination; the generic
		// "src + /" test would miss it, so refuse a root source explicitly.
		if srcReal == "/" || strings.HasPrefix(dstReal, srcReal+"/") {
			return pluginapi.Errf(pluginapi.CodeInvalid, "cannot copy %s into itself", from)
		}
		return o.copyTree(ctx, srcReal, dstReal, from, to)
	}
	if d, err := o.files().Stat(ctx, dstReal); err == nil && d.Kind == "dir" {
		return pluginapi.Errf(pluginapi.CodeConflict, "%s: the destination is a directory", to)
	}
	return o.copyFile(ctx, srcReal, dstReal, from, st.Size, st.Mode)
}

// copyCanonicalPaths proves where the copy would read and write. RealPather absence or a
// failed/relative resolution refuses safely (unsupported/invalid/io) before any write:
// path normalization alone cannot prove that a symlink or alias does not point into the
// source or elsewhere. A missing destination is canonicalized through its deepest
// existing ancestor; a dangling symlink component is refused instead of guessed.
func (o *authorityOS) copyCanonicalPaths(ctx context.Context, src, dst string) (string, string, error) {
	rp, ok := authority.RealPathResolver(o.files())
	if !ok {
		return "", "", pluginapi.Errf(pluginapi.CodeUnsupported,
			"copy: the workspace authority has no realpath capability to prove source/destination safety")
	}
	srcReal, err := rp.RealPath(ctx, src)
	if err != nil {
		return "", "", mapErr(err, src)
	}
	if !path.IsAbs(srcReal) {
		return "", "", pluginapi.Errf(pluginapi.CodeUnsupported, "%s: copy: realpath returned a non-absolute source path %q", src, srcReal)
	}
	dstReal, err := o.copyDestinationRealPath(ctx, rp, dst)
	if err != nil {
		return "", "", err
	}
	return path.Clean(srcReal), path.Clean(dstReal), nil
}

// copyDestinationRealPath resolves the existing part of a destination through realpath
// and appends the missing suffix lexically. Every symlink component must resolve; a
// dangling/looping/permission-denied link or an unprovable answer is a safe refusal.
func (o *authorityOS) copyDestinationRealPath(ctx context.Context, rp authority.RealPather, dst string) (string, error) {
	var suffix []string
	cur := dst
	for {
		st, err := o.files().Stat(ctx, cur)
		if err == nil {
			real, rerr := rp.RealPath(ctx, cur)
			if rerr != nil {
				if st.Kind == "symlink" {
					return "", pluginapi.Errf(pluginapi.CodeInvalid, "%s: destination symbolic link cannot be resolved safely", dst)
				}
				return "", mapErr(rerr, cur)
			}
			if !path.IsAbs(real) {
				return "", pluginapi.Errf(pluginapi.CodeUnsupported, "%s: copy: realpath returned a non-absolute destination %q", dst, real)
			}
			for i := len(suffix) - 1; i >= 0; i-- {
				real = path.Join(real, suffix[i])
			}
			if real == "" {
				return "", pluginapi.Errf(pluginapi.CodeIO, "%s: destination realpath returned no path", dst)
			}
			return path.Clean(real), nil
		}
		if pluginapi.CodeOf(err) != pluginapi.CodeNotFound {
			return "", mapErr(err, cur)
		}
		parent := path.Dir(cur)
		if parent == cur || parent == "." {
			return "", pluginapi.Errf(pluginapi.CodeInvalid, "%s: cannot prove the copy destination is safe", dst)
		}
		suffix = append(suffix, path.Base(cur))
		cur = parent
	}
}

// copyTree copies a canonicalized source directory to a canonicalized, not-yet-existing
// destination. The atomic destination claim is required: without DirectoryCreator a
// check-then-mkdir fallback could merge into a destination created concurrently and
// overwrite its files, so that backend gets a clear unsupported error before any write.
func (o *authorityOS) copyTree(ctx context.Context, src, dst, displayFrom, displayTo string) error {
	if strings.HasPrefix(dst, src+"/") || dst == src {
		return pluginapi.Errf(pluginapi.CodeInvalid, "cannot copy %s into itself", displayFrom)
	}
	if _, err := o.files().Stat(ctx, dst); err == nil {
		return pluginapi.Errf(pluginapi.CodeConflict, "%s: the destination already exists", displayTo)
	}
	creator := authority.DirectoryCreators(o.files())
	if creator == nil {
		return pluginapi.Errf(pluginapi.CodeUnsupported, "%s: this backend cannot atomically claim a copy destination; refusing an unsafe directory copy", displayTo)
	}
	// A tree copy writes files below the claimed destination. Without the atomic streamed
	// write surface a fallback would create/write a target through a separate (non-exclusive)
	// call; refuse before the claim/walk instead of leaving a partial or racy destination.
	if authority.WriteStreams(o.files()) == nil {
		return pluginapi.Errf(pluginapi.CodeUnsupported, "%s: this backend has no atomic streamed-write surface; refusing a copy that could race another writer", displayTo)
	}
	// The tree is listed before any write, so a refused symlink cannot leave a partial
	// copy behind and the entry cap is checked before doing work.
	res, err := o.Walk(ctx, src, hostos.WalkOptions{Hidden: true, MaxEntries: copyTreeMax})
	if err != nil {
		return err
	}
	if res.Truncated {
		return pluginapi.Errf(pluginapi.CodeLimit, "more than %d entries to copy", copyTreeMax)
	}
	for _, entry := range res.Entries {
		if entry.Kind == "symlink" {
			return pluginapi.Errf(pluginapi.CodeUnsupported, "%s: remote copy cannot recreate the symbolic link %s (no remote readlink)", displayFrom, entry.Rel)
		}
	}
	m, err := o.mutations(dst)
	if err != nil {
		return err
	}
	if err := claimCtx(ctx); err != nil {
		return err
	}
	// Create missing ancestors (a benign operation), then claim the destination root with
	// one exclusive mkdir. MkdirNew fails atomically with fs.ErrExist, so a destination
	// created concurrently after the pre-check is refused instead of being merged into.
	if parent := path.Dir(dst); parent != "." && parent != "/" {
		if err := mapErr(m.Mkdir(ctx, parent, true), parent); err != nil {
			return err
		}
	}
	if err := creator.MkdirNew(ctx, dst); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return pluginapi.Errf(pluginapi.CodeConflict, "%s: the destination already exists (atomic no-overwrite claim; a concurrent creator won)", displayTo)
		}
		return mapErr(err, dst)
	}
	for _, entry := range res.Entries {
		if err := claimCtx(ctx); err != nil {
			return err
		}
		target := path.Join(dst, entry.Rel)
		switch entry.Kind {
		case "dir":
			if err := mapErr(m.Mkdir(ctx, target, true), target); err != nil {
				return err
			}
		case "file":
			if err := o.copyFile(ctx, entry.Path, target, entry.Path, entry.Size, 0); err != nil {
				return err
			}
		default:
			// Sockets, devices and the like are not copied, as locally.
		}
	}
	return nil
}

// copyFile copies one remote file through the authority's atomic streamed-write surface.
// The destination is untouched until Commit and a failure/cancellation aborts the temp file.
// A backend without that surface is refused before reading or writing: FileCreator creates
// an empty file but gives no exclusive handle for the subsequent data write, so a
// create-then-WriteFile fallback would let another writer replace the path and then be
// overwritten by the copy. There is no provable same-handle create+write primitive here, so
// the only safe behavior is unsupported with no target created or written.
func (o *authorityOS) copyFile(ctx context.Context, src, dst, display string, size int64, mode fs.FileMode) error {
	streamer := authority.WriteStreams(o.files())
	if streamer == nil {
		return pluginapi.Errf(pluginapi.CodeUnsupported, "%s: this backend has no atomic streamed-write surface; refusing a copy that could race another writer", display)
	}
	return o.copyStreamedFile(ctx, streamer, src, dst, display, size, mode)
}

// copyStreamedFile copies exactly the source size observed before the copy. A source that
// shrank (short ReadRange/EOF) fails, aborting the target temp; growth beyond the initial
// size is not copied (recorded residual). Cancellation is checked between chunks and the
// abort uses a background context so the temp file is still removed.
func (o *authorityOS) copyStreamedFile(ctx context.Context, streamer authority.WriteStreamer, src, dst, display string, size int64, mode fs.FileMode) error {
	opts := hostos.WriteOptions{Atomic: true}
	if mode != 0 {
		opts.Mode = mode
	}
	ws, err := streamer.WriteStream(ctx, dst, opts)
	if err != nil {
		return mapErr(err, dst)
	}
	committed := false
	defer func() {
		if !committed {
			ws.Abort(context.Background())
		}
	}()
	files := o.files()
	var off int64
	for off < size {
		if err := ctx.Err(); err != nil {
			return mapErr(err, "")
		}
		want := min(remoteCopyChunk, size-off)
		chunk, rerr := files.ReadRange(ctx, src, off, want)
		if len(chunk) > 0 {
			if werr := ws.Write(ctx, chunk); werr != nil {
				return mapErr(werr, dst)
			}
			off += int64(len(chunk))
		}
		if off >= size {
			break
		}
		if rerr != nil {
			if errors.Is(rerr, io.EOF) {
				return pluginapi.Errf(pluginapi.CodeIO, "%s: the source changed during copy (read %d of %d bytes); the destination was not replaced", display, off, size)
			}
			return mapErr(rerr, src)
		}
		if len(chunk) == 0 {
			return pluginapi.Errf(pluginapi.CodeIO, "%s: the source changed during copy (no data at %d of %d bytes); the destination was not replaced", display, off, size)
		}
	}
	if err := ws.Commit(ctx); err != nil {
		return mapErr(err, dst)
	}
	committed = true
	return nil
}

func claimCtx(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return mapErr(err, "")
	}
	return nil
}
