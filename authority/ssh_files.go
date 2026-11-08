package authority

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/fs"
	"path"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/tbxark/gvim/plugin/hostos"
)

// The SSH file backend: the authority.FileSystem over bounded SSH exec helper
// commands. The design decision and its limits are recorded in docs/authority.md
// `6: there is no SFTP module in the offline dependency cache and no network, and
// the dependency policy is pure Go with nothing installed on the remote host, so
// each operation is one `sh -c` helper command over the existing SSH carrier (the
// same RemoteCommand composition the one-shot surface uses). No helper output is
// ever executed here: stdout is parsed as data, and every argument is POSIX-quoted
// by ShellQuote before it reaches the remote shell.
//
// Assumptions recorded for the operator: a POSIX `sh` on the remote and the base
// system tools `head`, `cat`, `chmod`, `mv`, `mkdir`, `dirname`, `readlink`,
// `mktemp`, `find`-free directory walk and `stat` (GNU coreutils or BSD/macOS
// stat; the helper detects which by its options). Nothing is installed or
// required beyond that base system. Outputs are bounded (read cap, directory
// entry cap, polling watch); every call is bounded by its context, and the
// authority kills the remote process when the context ends.
const (
	// sshFilesMaxDirEntries bounds one remote directory listing: the local port
	// returns every entry, but a remote listing is also a network and memory budget.
	sshFilesMaxDirEntries = 5000
	// sshFilesWatchInterval is the poll interval of the remote watcher.
	sshFilesWatchInterval = 500 * time.Millisecond
	// sshFilesMaxLinks bounds the symlink resolution of an atomic save.
	sshFilesMaxLinks = 16
	// sshFilesMaxReadBytes is the transport cap of one whole-file read: a caller's
	// larger max is capped here and an over-cap file is an error (remote large-file
	// mode is a recorded residual), so one read cannot pull an arbitrary amount over
	// the wire.
	sshFilesMaxReadBytes = 64 << 20
	// sshFilesMaxListBytes bounds the transfer of one directory listing; the entry
	// count is bounded by sshFilesMaxDirEntries. An over-cap answer is an error, never
	// a truncation.
	sshFilesMaxListBytes = 4 << 20
	// sshFilesMaxHelperBytes bounds the small stdout of the stat/write helpers.
	sshFilesMaxHelperBytes = 64 << 10
	// sshFilesMaxStderrBytes bounds the stderr kept from a failing helper: a failing
	// command can emit arbitrary text, and only a bounded prefix is ever parsed.
	sshFilesMaxStderrBytes = 64 << 10
)

// sshFiles implements FileSystem for one SSH backend.
type sshFiles struct {
	b *sshBackend
}

func newSSHFiles(b *sshBackend) *sshFiles { return &sshFiles{b: b} }

// helperResult is one helper answer plus the transport-overflow flags, so every caller
// can tell "the answer fit" from "the answer was over the cap" and never acts on a
// silently truncated answer.
type helperResult struct {
	res        hostos.ProcResult
	stdoutOver bool
	stderrOver bool
}

// execScript runs one helper: `sh -c <script> sh <args...>`, collecting stdout and stderr
// through bounded buffers (limits from the caller; the buffers keep draining after the
// limit so the remote process never blocks, and record the overflow instead). A nil
// stdin means the helper takes no input; an empty non-nil stdin is a write of zero
// bytes (the remote cat must see EOF).
func (f *sshFiles) execScript(ctx context.Context, script string, args []string, stdin []byte, stdoutLimit, stderrLimit int64) (helperResult, error) {
	argv := make([]string, 0, 4+len(args))
	argv = append(argv, "sh", "-c", script, "sh")
	argv = append(argv, args...)
	stdout, stderr := newBoundBuffer(stdoutLimit), newBoundBuffer(stderrLimit)
	o := hostos.ProcOptions{}
	if stdin != nil {
		o.Stdin = stdin
	}
	res, err := f.b.runSessionBuffered(ctx, argv, o, stdout, stderr)
	if err != nil {
		return helperResult{}, err
	}
	return helperResult{res: res, stdoutOver: stdout.Overflow(), stderrOver: stderr.Overflow()}, nil
}

// helperFailure maps a failed helper to the file error the port distinguishes, unless
// its stderr was over the transport cap: then only the over-cap error is returned,
// because a truncated message could mislead both the mapping and the user.
func helperFailure(op, path string, hr helperResult) error {
	if hr.stderrOver {
		return fmt.Errorf("remote %s %s: helper error output exceeded %d bytes", op, path, sshFilesMaxStderrBytes)
	}
	return remoteFileErr(op, path, hr.res)
}

// Mkdir implements FileMutations with mkdir, optionally creating ancestors.
func (f *sshFiles) Mkdir(ctx context.Context, path string, parents bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	hr, err := f.execScript(ctx, sshMkdirScript, []string{path, boolArg(parents)}, nil, sshFilesMaxHelperBytes, sshFilesMaxStderrBytes)
	if err != nil {
		return err
	}
	if hr.res.Code != 0 {
		return helperFailure("mkdir", path, hr)
	}
	return nil
}

// Rename implements FileMutations with mv -f (a same-filesystem rename).
func (f *sshFiles) Rename(ctx context.Context, from, to string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	hr, err := f.execScript(ctx, sshRenameScript, []string{from, to}, nil, sshFilesMaxHelperBytes, sshFilesMaxStderrBytes)
	if err != nil {
		return err
	}
	if hr.res.Code != 0 {
		return helperFailure("rename", from, hr)
	}
	return nil
}

// Remove implements FileMutations with rm (recursive when asked). A remote delete is
// permanent: there is no trash on the other side of the carrier.
func (f *sshFiles) Remove(ctx context.Context, path string, recursive bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	hr, err := f.execScript(ctx, sshRemoveScript, []string{path, boolArg(recursive)}, nil, sshFilesMaxHelperBytes, sshFilesMaxStderrBytes)
	if err != nil {
		return err
	}
	if hr.res.Code != 0 {
		return helperFailure("remove", path, hr)
	}
	return nil
}

// ReadFile reads at most max bytes through `head -c max+1`, so a larger file is
// detected from the answer size and reported in the port's too-large shape without
// pulling the whole file.
func (f *sshFiles) ReadFile(ctx context.Context, path string, max int64) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if max < 0 {
		max = 0
	}
	// A remote read is a transfer: cap the caller's max at the transport cap, and read
	// exactly effective+1 bytes so an oversized file is detected without pulling it.
	effective := min(max, int64(sshFilesMaxReadBytes))
	hr, err := f.execScript(ctx, sshReadScript, []string{path, strconv.FormatInt(effective+1, 10)}, nil, effective+1, sshFilesMaxStderrBytes)
	if err != nil {
		return nil, err
	}
	if hr.res.Code != 0 {
		return nil, helperFailure("read", path, hr)
	}
	if hr.stdoutOver || int64(len(hr.res.Stdout)) > effective {
		return nil, fmt.Errorf("%s: larger than %d bytes", path, effective)
	}
	return hr.res.Stdout, nil
}

// sshFilesMaxRangeBytes bounds one windowed remote read: the transport cap of a
// line-range fetch, so a scroll can never pull an arbitrary amount over the wire.
const sshFilesMaxRangeBytes = 1 << 20

// ReadRange reads at most n bytes at off with one tail/head helper.
func (f *sshFiles) ReadRange(ctx context.Context, path string, off, n int64) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if off < 0 || n < 0 {
		return nil, fmt.Errorf("remote range %s: negative offset or length", path)
	}
	n = min(n, int64(sshFilesMaxRangeBytes))
	hr, err := f.execScript(ctx, sshReadRangeScript, []string{path, strconv.FormatInt(off, 10), strconv.FormatInt(n, 10)}, nil, n, sshFilesMaxStderrBytes)
	if err != nil {
		return nil, err
	}
	if hr.res.Code != 0 {
		return nil, helperFailure("read range", path, hr)
	}
	if hr.stdoutOver {
		return nil, fmt.Errorf("remote read range %s: answer exceeded %d bytes", path, n)
	}
	if int64(len(hr.res.Stdout)) < n {
		return hr.res.Stdout, io.EOF
	}
	return hr.res.Stdout, nil
}

// sshReadRangeScript reads bytes [$2, $2+$3) of $1.
const sshReadRangeScript = `p=$1
off=$2
n=$3
if [ ! -e "$p" ] && [ ! -L "$p" ]; then echo "$p: no such file" >&2; exit 1; fi
if [ -d "$p" ]; then echo "$p: is a directory" >&2; exit 2; fi
tail -c +$((off + 1)) "$p" | head -c "$n"
`

// WriteFile writes through the helper. Atomic means a temporary file in the target's
// directory (or its resolved symlink target's directory), the mode applied to the
// temporary file, then one rename over the target: the same-directory rename is the
// atomicity the local port relies on. Existing permissions are preserved when
// WriteOptions.Mode is zero; a symbolic link is written through (resolved, bounded)
// exactly as the local atomic path does. Append and Parents follow the port; a
// non-atomic write truncates in place.
func (f *sshFiles) WriteFile(ctx context.Context, path string, data []byte, o hostos.WriteOptions) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if data == nil {
		data = []byte{}
	}
	mode := ""
	if o.Mode != 0 {
		mode = strconv.FormatUint(uint64(o.Mode.Perm()), 8)
	}
	args := []string{path, mode, boolArg(o.Parents), boolArg(o.Append), boolArg(o.Atomic), strconv.Itoa(sshFilesMaxLinks)}
	hr, err := f.execScript(ctx, sshWriteScript, args, data, sshFilesMaxHelperBytes, sshFilesMaxStderrBytes)
	if err != nil {
		return err
	}
	if hr.res.Code != 0 {
		return helperFailure("write", path, hr)
	}
	if hr.stdoutOver {
		return fmt.Errorf("remote write %s: helper answer exceeded %d bytes", path, sshFilesMaxHelperBytes)
	}
	return nil
}

// Stat reports kind, size, mod time and mode, following a symbolic link like the
// port (a broken link is reported as a symlink, from the no-follow fallback).
func (f *sshFiles) Stat(ctx context.Context, path string) (hostos.FileStat, error) {
	if err := ctx.Err(); err != nil {
		return hostos.FileStat{}, err
	}
	hr, err := f.execScript(ctx, sshStatScript, []string{path}, nil, sshFilesMaxHelperBytes, sshFilesMaxStderrBytes)
	if err != nil {
		return hostos.FileStat{}, err
	}
	if hr.res.Code != 0 {
		return hostos.FileStat{}, helperFailure("stat", path, hr)
	}
	if hr.stdoutOver {
		return hostos.FileStat{}, fmt.Errorf("remote stat %s: answer exceeded %d bytes", path, sshFilesMaxHelperBytes)
	}
	res := hr.res
	fields := strings.Split(strings.TrimSpace(string(res.Stdout)), "|")
	if len(fields) != 4 {
		return hostos.FileStat{}, fmt.Errorf("remote stat %s: unexpected answer %q", path, strings.TrimSpace(string(res.Stdout)))
	}
	size, serr := strconv.ParseInt(fields[1], 10, 64)
	sec, terr := strconv.ParseInt(fields[2], 10, 64)
	perm, perr := strconv.ParseUint(fields[3], 8, 32)
	if serr != nil || terr != nil || perr != nil {
		return hostos.FileStat{}, fmt.Errorf("remote stat %s: unexpected answer %q", path, strings.TrimSpace(string(res.Stdout)))
	}
	kind := remoteKind(fields[0])
	mode := fs.FileMode(perm) & fs.ModePerm
	switch kind {
	case "dir":
		mode |= fs.ModeDir
	case "symlink":
		mode |= fs.ModeSymlink
	}
	return hostos.FileStat{Kind: kind, Size: size, MTime: time.Unix(sec, 0), Mode: mode}, nil
}

// ReadDir lists one directory (hidden filtered like the port), sorted by name, with
// a symlink's target kind. The helper walks the entries itself so the listing is one
// bounded command; a directory with more than sshFilesMaxDirEntries entries is an
// error rather than a silently truncated listing.
func (f *sshFiles) ReadDir(ctx context.Context, path string, hidden bool) ([]hostos.DirEntry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	hr, err := f.execScript(ctx, sshReadDirScript, []string{path}, nil, sshFilesMaxListBytes, sshFilesMaxStderrBytes)
	if err != nil {
		return nil, err
	}
	if hr.res.Code != 0 {
		return nil, helperFailure("list", path, hr)
	}
	if hr.stdoutOver {
		return nil, fmt.Errorf("remote list %s: listing exceeded %d bytes", path, sshFilesMaxListBytes)
	}
	res := hr.res
	parts := bytes.Split(res.Stdout, []byte{0})
	if len(parts) > 0 && len(parts[len(parts)-1]) == 0 {
		parts = parts[:len(parts)-1]
	}
	if len(parts)%3 != 0 {
		return nil, fmt.Errorf("remote list %s: unexpected answer", path)
	}
	if len(parts)/3 > sshFilesMaxDirEntries {
		return nil, fmt.Errorf("remote list %s: more than %d entries", path, sshFilesMaxDirEntries)
	}
	out := make([]hostos.DirEntry, 0, len(parts)/3)
	for i := 0; i < len(parts); i += 3 {
		name := string(parts[i])
		if !hidden && strings.HasPrefix(name, ".") {
			continue
		}
		fields := strings.Split(string(parts[i+1]), "|")
		if len(fields) != 3 {
			return nil, fmt.Errorf("remote list %s: unexpected entry for %q", path, name)
		}
		size, serr := strconv.ParseInt(fields[1], 10, 64)
		sec, terr := strconv.ParseInt(fields[2], 10, 64)
		if serr != nil || terr != nil {
			return nil, fmt.Errorf("remote list %s: unexpected entry for %q", path, name)
		}
		kind := remoteKind(fields[0])
		de := hostos.DirEntry{Name: name, Kind: kind, Size: size, MTime: time.Unix(sec, 0)}
		if kind == "symlink" {
			if target := string(parts[i+2]); target != "" {
				de.TargetKind = remoteKind(target)
			}
		}
		out = append(out, de)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// Watch polls the path because a remote host has no fsnotify channel: a bounded
// stat (or directory snapshot) every sshFilesWatchInterval, coalesced over the
// debounce window. A rename is reported as remove+create, since polling cannot tell
// them apart; that and the poll latency are documented limits.
func (f *sshFiles) Watch(ctx context.Context, path string, o hostos.WatchOptions) (hostos.Watch, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	snap, err := f.snapshot(ctx, path)
	if err != nil {
		return nil, err
	}
	debounce := o.Debounce
	if debounce <= 0 {
		debounce = 50 * time.Millisecond
	}
	w := &sshPollWatch{
		files: f, root: path, dir: snap.dir, debounce: debounce,
		events: make(chan hostos.WatchEvent, 32),
		stop:   make(chan struct{}),
		done:   make(chan struct{}),
		prev:   snap.entries,
	}
	go w.loop(ctx)
	return w, nil
}

// sshSnapshot is one polled state: a directory's entries by name, or one file under
// the empty name.
type sshSnapshot struct {
	dir     bool
	entries map[string]hostos.DirEntry
}

func (f *sshFiles) snapshot(ctx context.Context, p string) (sshSnapshot, error) {
	st, err := f.Stat(ctx, p)
	if err != nil {
		return sshSnapshot{}, err
	}
	if st.Kind == "dir" {
		entries, err := f.ReadDir(ctx, p, true)
		if err != nil {
			return sshSnapshot{}, err
		}
		m := make(map[string]hostos.DirEntry, len(entries))
		for _, e := range entries {
			m[e.Name] = e
		}
		return sshSnapshot{dir: true, entries: m}, nil
	}
	return sshSnapshot{entries: map[string]hostos.DirEntry{"": {Name: "", Kind: st.Kind, Size: st.Size, MTime: st.MTime}}}, nil
}

// sshPollWatch is the polling watcher of one remote path.
type sshPollWatch struct {
	files    *sshFiles
	root     string
	dir      bool
	debounce time.Duration
	events   chan hostos.WatchEvent
	stop     chan struct{}
	done     chan struct{}
	stopOnce sync.Once
	prev     map[string]hostos.DirEntry
}

func (w *sshPollWatch) loop(ctx context.Context) {
	defer close(w.done)
	prev := w.prev
	ticker := time.NewTicker(sshFilesWatchInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-w.stop:
			return
		case <-ticker.C:
		}
		next, err := w.files.snapshot(ctx, w.root)
		if err != nil {
			if !w.emit(ctx, hostos.WatchEvent{Path: w.root, Kind: "error", Err: err.Error()}) {
				return
			}
			continue
		}
		diff := diffSnapshots(w.root, w.dir, prev, next.entries)
		if len(diff) == 0 {
			prev = next.entries
			continue
		}
		// Coalesce a burst: after the debounce window, diff once more against the same
		// baseline so the reader sees one settle, not every intermediate state.
		select {
		case <-ctx.Done():
			return
		case <-w.stop:
			return
		case <-time.After(w.debounce):
		}
		settled, err := w.files.snapshot(ctx, w.root)
		if err == nil {
			next, diff = settled, diffSnapshots(w.root, w.dir, prev, settled.entries)
		}
		for _, ev := range diff {
			if !w.emit(ctx, ev) {
				return
			}
		}
		prev = next.entries
	}
}

func (w *sshPollWatch) emit(ctx context.Context, ev hostos.WatchEvent) bool {
	select {
	case w.events <- ev:
		return true
	case <-ctx.Done():
		return false
	case <-w.stop:
		return false
	}
}

// Next implements hostos.Watch.
func (w *sshPollWatch) Next(ctx context.Context) (hostos.WatchEvent, bool) {
	select {
	case ev := <-w.events:
		return ev, true
	case <-ctx.Done():
		return hostos.WatchEvent{}, false
	case <-w.stop:
		return hostos.WatchEvent{}, false
	}
}

// Stop implements hostos.Watch.
func (w *sshPollWatch) Stop() {
	w.stopOnce.Do(func() { close(w.stop) })
	<-w.done
}

// diffSnapshots reports the changes between two polled states, sorted by path.
func diffSnapshots(root string, dir bool, prev, next map[string]hostos.DirEntry) []hostos.WatchEvent {
	var out []hostos.WatchEvent
	name := func(entry string) string {
		if dir && entry != "" {
			return path.Join(root, entry)
		}
		return root
	}
	for entry, ne := range next {
		pe, ok := prev[entry]
		switch {
		case !ok:
			out = append(out, hostos.WatchEvent{Path: name(entry), Kind: "create"})
		case pe.Kind != ne.Kind || pe.Size != ne.Size || !pe.MTime.Equal(ne.MTime):
			out = append(out, hostos.WatchEvent{Path: name(entry), Kind: "write"})
		}
	}
	for entry := range prev {
		if _, ok := next[entry]; !ok {
			out = append(out, hostos.WatchEvent{Path: name(entry), Kind: "remove"})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Path != out[j].Path {
			return out[i].Path < out[j].Path
		}
		return out[i].Kind < out[j].Kind
	})
	return out
}

// remoteKind maps a GNU/BSD stat type description to the port's kinds.
func remoteKind(desc string) string {
	d := strings.ToLower(strings.TrimSpace(desc))
	switch {
	case strings.Contains(d, "dir"):
		return "dir"
	case strings.Contains(d, "link"):
		return "symlink"
	}
	return "file"
}

// remoteFileErr turns a failed helper into an error that distinguishes the cases the
// port distinguishes: not-found and permission are wrapped (errors.Is), a directory
// used as a file and a non-regular file get the port's wording, and anything else
// carries the remote stderr.
func remoteFileErr(op, path string, res hostos.ProcResult) error {
	msg := strings.TrimSpace(string(res.Stderr))
	lower := strings.ToLower(msg)
	switch {
	case strings.Contains(lower, "no such file"), strings.Contains(lower, "does not exist"), strings.Contains(lower, "cannot access"):
		return fmt.Errorf("%s %s: %w", op, path, fs.ErrNotExist)
	case strings.Contains(lower, "permission denied"), strings.Contains(lower, "operation not permitted"):
		return fmt.Errorf("%s %s: %w", op, path, fs.ErrPermission)
	case strings.Contains(lower, "is a directory"):
		return fmt.Errorf("%s: is a directory", path)
	case strings.Contains(lower, "not a regular file"):
		return fmt.Errorf("%s: not a regular file", path)
	}
	if msg == "" {
		msg = fmt.Sprintf("helper exited %d", res.Code)
	}
	return fmt.Errorf("remote %s %s: %s", op, path, msg)
}

// boolArg is the 0/1 form the helper scripts branch on.
func boolArg(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

// sshMkdirScript creates $1, with ancestors when $2 is 1.
const sshMkdirScript = `p=$1
if [ "$2" = 1 ]; then mkdir -p "$p"; else mkdir "$p"; fi`

// sshRenameScript moves $1 to $2, replacing the target.
const sshRenameScript = `from=$1
to=$2
mv -f "$from" "$to"`

// sshRemoveScript deletes $1, recursively when $2 is 1.
const sshRemoveScript = `p=$1
if [ "$2" = 1 ]; then rm -rf "$p"; else rm -f "$p"; fi`

// sshReadScript reads at most $2 bytes of $1.
const sshReadScript = `p=$1
if [ ! -e "$p" ] && [ ! -L "$p" ]; then
  stat "$p" >/dev/null || true
  exit 1
fi
if [ -d "$p" ]; then echo "$p: is a directory" >&2; exit 2; fi
if [ ! -f "$p" ] && [ ! -L "$p" ]; then echo "$p: not a regular file" >&2; exit 2; fi
head -c "$2" "$p"`

// sshStatScript prints type|size|mtime|mode for $1, GNU first and BSD/macOS stat
// after; when a followed link cannot be read it falls back to the link itself.
const sshStatScript = `p=$1
if stat -c '%s' "$p" >/dev/null 2>&1; then
  out=$(stat -L -c '%F|%s|%Y|%a' "$p" 2>/dev/null) || out=$(stat -c '%F|%s|%Y|%a' "$p" 2>/dev/null)
else
  out=$(stat -L -f '%HT|%z|%m|%Lp' "$p" 2>/dev/null) || out=$(stat -f '%HT|%z|%m|%Lp' "$p" 2>/dev/null)
fi
if [ -z "$out" ]; then
  stat "$p" >/dev/null || true
  exit 1
fi
printf '%s' "$out"`

// sshWriteScript writes stdin to $1. $2 is the octal mode (empty keeps the existing
// permission, 644 for a new file), $3 makes parents, $4 appends, $5 is atomic.
const sshWriteScript = `p=$1
mode=$2
parents=$3
app=$4
atomic=$5
maxlinks=$6
d=$(dirname "$p") || exit 2
if [ "$parents" = 1 ]; then
  mkdir -p "$d" || exit 2
fi
if [ "$atomic" = 1 ]; then
  tgt=$p
  i=0
  while [ -L "$tgt" ]; do
    i=$((i+1))
    if [ "$i" -gt "$maxlinks" ]; then echo "too many symbolic links: $p" >&2; exit 2; fi
    l=$(readlink "$tgt") || exit 2
    case $l in
      /*) tgt=$l ;;
      *) tgt=$(dirname "$tgt")/$l ;;
    esac
  done
  td=$(dirname "$tgt") || exit 2
  if [ -z "$mode" ]; then
    if [ -e "$tgt" ]; then
      mode=$(stat -L -c '%a' "$tgt" 2>/dev/null || stat -L -f '%Lp' "$tgt" 2>/dev/null)
    fi
    if [ -z "$mode" ]; then mode=644; fi
  fi
  t=$(mktemp "$td/.gvim-XXXXXX") || exit 2
  trap 'rm -f "$t"' EXIT HUP INT TERM
  cat > "$t" || { rm -f "$t"; exit 2; }
  chmod "$mode" "$t" || { rm -f "$t"; exit 2; }
  mv -f "$t" "$tgt" || { rm -f "$t"; exit 2; }
  trap - EXIT HUP INT TERM
  exit 0
fi
existed=0
if [ -e "$p" ] || [ -L "$p" ]; then existed=1; fi
if [ "$app" = 1 ]; then
  cat >> "$p" || exit 2
else
  cat > "$p" || exit 2
fi
if [ "$existed" = 0 ] && [ -n "$mode" ]; then chmod "$mode" "$p" || exit 2; fi
exit 0`

// sshReadDirScript prints NUL-separated name, type|size|mtime and followed type for
// every entry of $1, hidden entries included (the caller filters); the outer counter
// bounds the listing. It avoids find -printf so GNU and BSD/macOS both run it.
const sshReadDirScript = `d=$1
if [ ! -e "$d" ] && [ ! -L "$d" ]; then
  stat "$d" >/dev/null || true
  exit 1
fi
if [ ! -d "$d" ]; then echo "not a directory: $d" >&2; exit 2; fi
gnu=0
if stat -c '%s' "$d" >/dev/null 2>&1; then gnu=1; fi
n=0
for p in "$d"/* "$d"/.[!.]* "$d"/..?*; do
  if [ ! -e "$p" ] && [ ! -L "$p" ]; then continue; fi
  n=$((n+1))
  if [ "$n" -gt 5000 ]; then echo "directory has more than 5000 entries" >&2; exit 3; fi
  nm=${p##*/}
  if [ "$gnu" = 1 ]; then
    l=$(stat -c '%F|%s|%Y' "$p" 2>/dev/null) || continue
    fk=$(stat -L -c '%F' "$p" 2>/dev/null) || fk=""
  else
    l=$(stat -f '%HT|%z|%m' "$p" 2>/dev/null) || continue
    fk=$(stat -L -f '%HT' "$p" 2>/dev/null) || fk=""
  fi
  printf '%s\000' "$nm" "$l" "$fk"
done
exit 0`
