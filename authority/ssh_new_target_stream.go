package authority

// New-target-only streamed copy support over the SSH helpers. The destination is never
// opened, followed, replaced or removed: chunks go into a private mktemp file in the
// destination directory, Commit publishes the complete file with the POSIX link utility
// (exact target: it never treats a directory operand as a place to create a name inside),
// and an existing file, directory, symlink or dangling symlink
// makes the link fail with EEXIST. Abort removes only the temporary file. The capability
// is proven in the destination directory by a private probe before any source data is
// read; a remote filesystem/toolchain that cannot prove the no-clobber hardlink commit is
// refused as unsupported.

import (
	"context"
	"fmt"
	"io/fs"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/tbxark/gvim/plugin/hostos"
)

var _ NewTargetWriteStreamer = (*sshFiles)(nil)
var _ NewTargetProver = (*sshFiles)(nil)

// WriteStreamNewTarget opens a new-target-only streamed copy. The destination directory
// must prove same-directory hard-link/no-clobber support first; otherwise the call returns
// ErrUnsupportedFileIO before the source data is read or a target can be touched.
func (f *sshFiles) WriteStreamNewTarget(ctx context.Context, target string, o hostos.WriteOptions) (WriteStream, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := validateStreamOptions(o); err != nil {
		return nil, err
	}
	dir := path.Dir(target)
	if dir == "" || dir == "." {
		return nil, fmt.Errorf("%w: new-target copy needs an absolute destination directory", ErrUnsupportedFileIO)
	}
	if err := f.proveNewTargetCommit(ctx, dir); err != nil {
		return nil, err
	}
	mode := ""
	if o.Mode != 0 {
		mode = strconv.FormatUint(uint64(o.Mode.Perm()), 8)
	}
	hr, err := f.execScript(ctx, sshNewTargetOpenScript, []string{target}, nil, sshFilesMaxHelperBytes, sshFilesMaxStderrBytes)
	if err != nil {
		return nil, err
	}
	if hr.res.Code != 0 {
		return nil, helperFailure("new-target open", target, hr)
	}
	if hr.stdoutOver {
		return nil, fmt.Errorf("remote new-target open %s: helper answer exceeded %d bytes", target, sshFilesMaxHelperBytes)
	}
	fields := strings.Split(string(hr.res.Stdout), "\x00")
	if len(fields) < 2 || fields[0] == "" || fields[1] == "" {
		return nil, fmt.Errorf("remote new-target open %s: unexpected answer %q", target, strings.TrimSpace(string(hr.res.Stdout)))
	}
	return &sshNewTargetStream{files: f, tmp: fields[0], target: fields[1], mode: mode}, nil
}

// ProveNewTarget proves the no-clobber hardlink commit in dir without creating a target.
func (f *sshFiles) ProveNewTarget(ctx context.Context, dir string) error {
	return f.proveNewTargetCommit(ctx, dir)
}

// proveNewTargetCommit caches the proof per destination directory. A failed proof is
// cached as unsupported; a cancelled probe is not cached so a retry can run.
func (f *sshFiles) proveNewTargetCommit(ctx context.Context, dir string) error {
	f.newTargetMu.Lock()
	if f.newTargetOK != nil && f.newTargetOK[dir] {
		f.newTargetMu.Unlock()
		return nil
	}
	if f.newTargetErr != nil {
		if err, ok := f.newTargetErr[dir]; ok {
			f.newTargetMu.Unlock()
			return err
		}
	}
	f.newTargetMu.Unlock()
	hr, err := f.execScript(ctx, sshNewTargetProbeScript, []string{dir}, nil, sshFilesMaxHelperBytes, sshFilesMaxStderrBytes)
	if err != nil {
		return err
	}
	if hr.res.Code != 0 {
		err := fmt.Errorf("%w: cannot prove hard-link no-clobber commit in %s", ErrUnsupportedFileIO, dir)
		if ctx.Err() == nil {
			f.newTargetMu.Lock()
			if f.newTargetOK == nil {
				f.newTargetOK = map[string]bool{}
			}
			if f.newTargetErr == nil {
				f.newTargetErr = map[string]error{}
			}
			f.newTargetErr[dir] = err
			f.newTargetMu.Unlock()
		}
		return err
	}
	f.newTargetMu.Lock()
	if f.newTargetOK == nil {
		f.newTargetOK = map[string]bool{}
	}
	if f.newTargetErr == nil {
		f.newTargetErr = map[string]error{}
	}
	f.newTargetOK[dir] = true
	delete(f.newTargetErr, dir)
	f.newTargetMu.Unlock()
	return nil
}

type sshNewTargetStream struct {
	files  *sshFiles
	tmp    string
	target string
	mode   string
	done   bool
}

func (s *sshNewTargetStream) Write(ctx context.Context, p []byte) error {
	if s.done {
		return fs.ErrClosed
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	hr, err := s.files.execScript(ctx, sshStreamWriteScript, []string{s.tmp}, p, sshFilesMaxHelperBytes, sshFilesMaxStderrBytes)
	if err != nil {
		return err
	}
	if hr.res.Code != 0 {
		return helperFailure("new-target write", s.tmp, hr)
	}
	if hr.stdoutOver {
		return fmt.Errorf("remote new-target write %s: helper answer exceeded %d bytes", s.tmp, sshFilesMaxHelperBytes)
	}
	return nil
}

func (s *sshNewTargetStream) Commit(ctx context.Context) error {
	if s.done {
		return fs.ErrClosed
	}
	if err := ctx.Err(); err != nil {
		s.abortWith(context.Background())
		return err
	}
	hr, err := s.files.execScript(ctx, sshNewTargetCommitScript, []string{s.tmp, s.target, s.mode}, nil, sshFilesMaxHelperBytes, sshFilesMaxStderrBytes)
	if err != nil {
		s.done = true
		return err
	}
	switch hr.res.Code {
	case 0:
		s.done = true
		return nil
	case 3:
		s.done = true
		return fmt.Errorf("%s: %w: destination already exists", s.target, fs.ErrExist)
	default:
		s.done = true
		return helperFailure("new-target commit", s.target, hr)
	}
}

func (s *sshNewTargetStream) Abort(ctx context.Context) {
	if s.done {
		return
	}
	s.done = true
	s.abortWith(ctx)
}

func (s *sshNewTargetStream) abortWith(ctx context.Context) {
	cleanup := ctx
	if cleanup == nil || cleanup.Err() != nil {
		var cancel context.CancelFunc
		cleanup, cancel = context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
	}
	_, _ = s.files.execScript(cleanup, sshStreamAbortScript, []string{s.tmp}, nil, sshFilesMaxHelperBytes, sshFilesMaxStderrBytes)
}

// sshNewTargetOpenScript creates the private temp in the destination directory; it never
// resolves or opens the target. Prints temp and target NUL-separated.
const sshNewTargetOpenScript = `
p=$1
td=$(dirname "$p") || exit 2
tmp=$(mktemp "$td/.$(basename "$p").gvim-XXXXXX") || exit 2
printf '%s\0%s\0' "$tmp" "$p"
`

// sshNewTargetProbeScript proves, using only private temp names, that this directory's
// filesystem supports a no-clobber exact-target hard link and does not follow a symlink or
// directory target. The POSIX link utility is used instead of ln: ln treats an existing
// directory target as a directory to create a name in and would not fail EEXIST. It never
// touches the requested destination.
const sshNewTargetProbeScript = `
d=$1
a=$(mktemp "$d/.gvim-lnprobe-XXXXXX") || exit 2
b=$(mktemp "$d/.gvim-lnprobe-XXXXXX") || { rm -f "$a"; exit 2; }
rm -f "$b"
if ! link "$a" "$b" 2>/dev/null; then rm -f "$a" "$b"; exit 1; fi
if link "$a" "$b" 2>/dev/null; then rm -f "$a" "$b"; exit 1; fi
d="$a.dir"
mkdir "$d" || { rm -f "$a" "$b"; exit 1; }
if link "$a" "$d" 2>/dev/null; then rm -f "$a" "$b"; rm -rf "$d"; exit 1; fi
rmdir "$d" || { rm -f "$a" "$b"; exit 1; }
c="$a.link"
ln -s "$a" "$c" 2>/dev/null || { rm -f "$a" "$b" "$c"; exit 1; }
if link "$a" "$c" 2>/dev/null; then rm -f "$a" "$b" "$c"; exit 1; fi
rm -f "$a" "$b" "$c"
exit 0
`

// sshNewTargetCommitScript applies the mode to the private temp and publishes it with one
// no-clobber exact-target hard link (POSIX link utility, not ln: ln would put a file inside
// an existing directory instead of failing EEXIST). It never removes, follows or replaces
// the destination: an existing file/directory/symlink/dangling symlink is reported as exit
// 3 and only the temporary file is removed. Exit 2 is an ordinary failure with the
// temporary file removed.
const sshNewTargetCommitScript = `
tmp=$1
tgt=$2
mode=$3
if [ -z "$mode" ]; then mode=644; fi
chmod "$mode" "$tmp" || { rm -f "$tmp"; exit 2; }
if link "$tmp" "$tgt" 2>/dev/null; then
  rm -f "$tmp"
  exit 0
fi
if [ -e "$tgt" ] || [ -L "$tgt" ]; then
  rm -f "$tmp"
  echo "destination exists" >&2
  exit 3
fi
rm -f "$tmp"
exit 2
`
