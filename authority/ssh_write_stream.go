package authority

// Streamed atomic writes over the SSH helpers: one mktemp in the resolved target
// directory, one bounded `cat >> temp` helper per chunk, then one chmod+mv helper. The
// target is untouched until the commit; Abort removes the temporary file. Memory is one
// chunk (the save's window), never the whole file.

import (
	"context"
	"fmt"
	"io/fs"
	"strconv"
	"strings"
	"time"

	"github.com/tbxark/gvim/plugin/hostos"
)

// WriteStream implements WriteStreamer: the streamed atomic write of a remote file.
func (f *sshFiles) WriteStream(ctx context.Context, path string, o hostos.WriteOptions) (WriteStream, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := validateStreamOptions(o); err != nil {
		return nil, err
	}
	mode := ""
	if o.Mode != 0 {
		mode = strconv.FormatUint(uint64(o.Mode.Perm()), 8)
	}
	hr, err := f.execScript(ctx, sshStreamOpenScript, []string{path, strconv.Itoa(sshFilesMaxLinks)}, nil, sshFilesMaxHelperBytes, sshFilesMaxStderrBytes)
	if err != nil {
		return nil, err
	}
	if hr.res.Code != 0 {
		return nil, helperFailure("stream open", path, hr)
	}
	if hr.stdoutOver {
		return nil, fmt.Errorf("remote stream open %s: helper answer exceeded %d bytes", path, sshFilesMaxHelperBytes)
	}
	fields := strings.Split(string(hr.res.Stdout), "\x00")
	if len(fields) < 2 || fields[0] == "" || fields[1] == "" {
		return nil, fmt.Errorf("remote stream open %s: unexpected answer %q", path, strings.TrimSpace(string(hr.res.Stdout)))
	}
	return &sshWriteStream{files: f, tmp: fields[0], target: fields[1], mode: mode}, nil
}

// sshWriteStream is the SSH streamed write: chunks append to the remote temporary file;
// commit applies the mode and renames it over the target.
type sshWriteStream struct {
	files  *sshFiles
	tmp    string
	target string
	mode   string
	done   bool
}

// Write appends one chunk through one bounded helper.
func (s *sshWriteStream) Write(ctx context.Context, p []byte) error {
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
		return helperFailure("stream write", s.tmp, hr)
	}
	if hr.stdoutOver {
		return fmt.Errorf("remote stream write %s: helper answer exceeded %d bytes", s.tmp, sshFilesMaxHelperBytes)
	}
	return nil
}

// Commit applies the mode and renames the temporary file over the target.
func (s *sshWriteStream) Commit(ctx context.Context) error {
	if s.done {
		return fs.ErrClosed
	}
	if err := ctx.Err(); err != nil {
		s.abortWith(context.Background())
		return err
	}
	hr, err := s.files.execScript(ctx, sshStreamCommitScript, []string{s.tmp, s.target, s.mode}, nil, sshFilesMaxHelperBytes, sshFilesMaxStderrBytes)
	if err != nil {
		s.done = true
		return err
	}
	if hr.res.Code != 0 {
		s.done = true
		return helperFailure("stream commit", s.target, hr)
	}
	if hr.stdoutOver {
		s.done = true
		return fmt.Errorf("remote stream commit %s: helper answer exceeded %d bytes", s.target, sshFilesMaxHelperBytes)
	}
	s.done = true
	return nil
}

// Abort removes the temporary file, never the target.
func (s *sshWriteStream) Abort(ctx context.Context) {
	if s.done {
		return
	}
	s.abortWith(ctx)
}

func (s *sshWriteStream) abortWith(ctx context.Context) {
	s.done = true
	cleanup := ctx
	if cleanup == nil || cleanup.Err() != nil {
		var cancel context.CancelFunc
		cleanup, cancel = context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
	}
	_, _ = s.files.execScript(cleanup, sshStreamAbortScript, []string{s.tmp}, nil, sshFilesMaxHelperBytes, sshFilesMaxStderrBytes)
}

// sshStreamOpenScript creates the temporary file for a streamed atomic write to $1.
// $2 bounds the symbolic-link resolution. It prints the temporary path and the resolved
// target, NUL-separated (neither may contain NUL; a newline is fine).
const sshStreamOpenScript = `p=$1
maxlinks=$2
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
if [ -d "$tgt" ]; then echo "$tgt: is a directory" >&2; exit 2; fi
td=$(dirname "$tgt") || exit 2
tmp=$(mktemp "$td/.$(basename "$tgt" | cut -c1-200).gvim-XXXXXX") || exit 2
printf '%s\0%s\0' "$tmp" "$tgt"
`

// sshStreamWriteScript appends stdin to the temporary file $1.
const sshStreamWriteScript = `cat >> "$1"
`

// sshStreamCommitScript applies $3 (octal mode, empty keeps the target's permission or
// 644) to the temporary file $1 and renames it over the target $2. A failure removes the
// temporary file and leaves the target untouched.
const sshStreamCommitScript = `tmp=$1
tgt=$2
mode=$3
if [ -z "$mode" ]; then
  if [ -e "$tgt" ]; then
    mode=$(stat -L -c '%a' "$tgt" 2>/dev/null || stat -L -f '%Lp' "$tgt" 2>/dev/null)
  fi
  if [ -z "$mode" ]; then mode=644; fi
fi
chmod "$mode" "$tmp" || { rm -f "$tmp"; exit 2; }
mv -f "$tmp" "$tgt" || { rm -f "$tmp"; exit 2; }
`

// sshStreamAbortScript removes the temporary file $1.
const sshStreamAbortScript = `rm -f "$1"
`
