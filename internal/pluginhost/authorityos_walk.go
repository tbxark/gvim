package pluginhost

// Remote Walk and Copy over the authority's bounded file surface. The authority exposes
// ReadDir/Stat/ReadFile/WriteFile and (optionally) the FileMutations surface, but no
// readlink/streaming primitive, so these are deliberately bounded re-implementations:
//   - Walk is a depth-first, lexical-order walk with the local semantics for hidden
//     entries, skipped directories, MaxDepth and MaxEntries; symlinks are reported but
//     never followed; unreadable subdirectories are skipped; cancellation is checked
//     throughout. It reads the remote directory entries, never the local filesystem.
//   - Copy copies a regular file, or a directory tree, entirely remote-to-remote. A
//     file is read whole and capped (remoteCopyFileMax); symlinks in a tree are refused
//     (there is no remote readlink primitive, so copying them as links is impossible and
//     silently dropping them would be wrong), and the tree is bounded by copyTreeMax
//     entries. RealPath remains unsupported: without remote readlink a cleaned path
//     would be a wrong answer for a symlink.
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
	// remoteCopyFileMax is the largest single file Copy moves through the authority's
	// value-based ReadFile/WriteFile. Larger files are a clear too_large error (the
	// local OS streams instead; a remote streaming copy is a recorded follow-up).
	remoteCopyFileMax = 16 << 20
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
func (o *authorityOS) Copy(ctx context.Context, from, to string) error {
	if err := ctx.Err(); err != nil {
		return mapErr(err, "")
	}
	src, dst := o.resolve(from), o.resolve(to)
	if src == dst || src == "" || dst == "" {
		return pluginapi.Errf(pluginapi.CodeInvalid, "%s: source and destination are the same path", src)
	}
	st, err := o.files().Stat(ctx, src)
	if err != nil {
		return mapErr(err, from)
	}
	switch st.Kind {
	case "dir":
		return o.copyTree(ctx, src, dst, from)
	case "file":
	default:
		return pluginapi.Errf(pluginapi.CodeInvalid, "%s: not a regular file (remote copy supports files and directory trees)", from)
	}
	if d, err := o.files().Stat(ctx, dst); err == nil && d.Kind == "dir" {
		return pluginapi.Errf(pluginapi.CodeConflict, "%s: the destination is a directory", to)
	}
	return o.copyFile(ctx, src, dst, from, st.Size, st.Mode)
}

func (o *authorityOS) copyTree(ctx context.Context, src, dst, displayFrom string) error {
	if strings.HasPrefix(dst, src+"/") || dst == src {
		return pluginapi.Errf(pluginapi.CodeInvalid, "cannot copy %s into itself", displayFrom)
	}
	if _, err := o.files().Stat(ctx, dst); err == nil {
		return pluginapi.Errf(pluginapi.CodeConflict, "%s: the destination already exists", dst)
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
	if creator := authority.DirectoryCreators(o.files()); creator != nil {
		// Create missing ancestors (a benign operation), then claim the destination root
		// with one exclusive mkdir. The old path used mkdir -p on the destination itself:
		// a directory created concurrently between the Stat check and that mkdir was
		// silently merged into (and its files could be overwritten). MkdirNew fails
		// atomically with fs.ErrExist, so the copy refuses a destination that appeared
		// meanwhile.
		if parent := path.Dir(dst); parent != "." && parent != "/" {
			if err := mapErr(m.Mkdir(ctx, parent, true), parent); err != nil {
				return err
			}
		}
		if err := creator.MkdirNew(ctx, dst); err != nil {
			if errors.Is(err, fs.ErrExist) {
				return pluginapi.Errf(pluginapi.CodeConflict, "%s: the destination already exists (atomic no-overwrite claim; a concurrent creator won)", dst)
			}
			return mapErr(err, dst)
		}
	} else {
		// The backend cannot guarantee an atomic claim: keep the previous check-then-mkdir
		// behaviour instead of failing a copy that used to work, and record the residual
		// race (the Stat check above, and Mkdir with parents, can merge into a concurrent
		// creator). Current authority backends implement DirectoryCreator.
		if err := mapErr(m.Mkdir(ctx, dst, true), dst); err != nil {
			return err
		}
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

// copyFile copies one remote file. When the backend has the authority's streamed-write
// surface, it copies in bounded ReadRange windows into an atomic WriteStream: memory is
// one chunk, the destination is untouched until Commit, and a failure/cancellation Aborts
// the temporary file (so an existing destination survives). Without that surface it keeps
// the previous whole-file fallback, whose 16 MiB cap is deliberately conservative because
// a whole-file ReadFile/WriteFile pair is neither streaming nor atomic on every backend.
func (o *authorityOS) copyFile(ctx context.Context, src, dst, display string, size int64, mode fs.FileMode) error {
	if streamer := authority.WriteStreams(o.files()); streamer != nil {
		return o.copyStreamedFile(ctx, streamer, src, dst, display, size, mode)
	}
	if size > remoteCopyFileMax {
		return pluginapi.Errf(pluginapi.CodeTooLarge, "%s: larger than %d bytes (remote copy is bounded on this backend: it has no streamed-write surface)", display, remoteCopyFileMax)
	}
	data, err := o.files().ReadFile(ctx, src, remoteCopyFileMax)
	if err != nil {
		return mapErr(err, src)
	}
	if int64(len(data)) > remoteCopyFileMax {
		return pluginapi.Errf(pluginapi.CodeTooLarge, "%s: larger than %d bytes (remote copy is bounded on this backend: it has no streamed-write surface)", display, remoteCopyFileMax)
	}
	opts := hostos.WriteOptions{}
	if mode != 0 {
		opts.Mode = mode
	}
	return mapErr(o.files().WriteFile(ctx, dst, data, opts), dst)
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
