package authority

// Exclusive create over the SSH helper channel: one POSIX noclobber redirection
// (`set -C; : > "$p"`) creates the file atomically and fails if anything already exists.
// The shell implements the noclobber path with O_EXCL semantics, so a concurrent creator
// loses cleanly and a symbolic link at the path is not followed or truncated. This is a
// deliberate improvement over Fresh's remote authority, whose agent protocol has no
// exclusive create and therefore returns Unsupported for create_new_file.

import (
	"context"
	"fmt"
	"io/fs"
	"strconv"

	"github.com/tbxark/gvim/plugin/hostos"
)

// CreateNew implements FileCreator: an atomic create-new helper. Anything already at path
// (file, directory or symbolic link) reports fs.ErrExist and is untouched; only an
// explicit mode is chmod'ed, otherwise the remote umask decides like a shell redirect.
func (f *sshFiles) CreateNew(ctx context.Context, path string, o hostos.WriteOptions) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validateCreateOptions(o); err != nil {
		return err
	}
	mode := ""
	if o.Mode != 0 {
		mode = strconv.FormatUint(uint64(o.Mode.Perm()), 8)
	}
	hr, err := f.execScript(ctx, sshCreateNewScript, []string{path, mode}, nil, sshFilesMaxHelperBytes, sshFilesMaxStderrBytes)
	if err != nil {
		return err
	}
	if hr.res.Code != 0 {
		if hr.stderrOver {
			return fmt.Errorf("remote create %s: helper error output exceeded %d bytes", path, sshFilesMaxStderrBytes)
		}
		if hr.res.Code == 3 {
			return fmt.Errorf("remote create %s: %w", path, fs.ErrExist)
		}
		return helperFailure("create", path, hr)
	}
	if hr.stdoutOver {
		return fmt.Errorf("remote create %s: helper answer exceeded %d bytes", path, sshFilesMaxHelperBytes)
	}
	return nil
}

// CreateNewWithContent implements FileContentCreator with one helper: a noclobber
// redirection opens the target once (O_EXCL-like), and stdin is written through that open
// descriptor, never by reopening the path. Anything already at path (file, directory or
// dangling symlink) exits 3 and is untouched; a write/chmod failure exits 2 and leaves the
// claimed file rather than deleting a path another process may have replaced.
func (f *sshFiles) CreateNewWithContent(ctx context.Context, path string, data []byte, o hostos.WriteOptions) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validateCreateOptions(o); err != nil {
		return err
	}
	if data == nil {
		data = []byte{}
	}
	mode := ""
	if o.Mode != 0 {
		mode = strconv.FormatUint(uint64(o.Mode.Perm()), 8)
	}
	hr, err := f.execScript(ctx, sshCreateNewContentScript, []string{path, mode}, data, sshFilesMaxHelperBytes, sshFilesMaxStderrBytes)
	if err != nil {
		return err
	}
	if hr.res.Code != 0 {
		if hr.stderrOver {
			return fmt.Errorf("remote create %s: helper error output exceeded %d bytes", path, sshFilesMaxStderrBytes)
		}
		if hr.res.Code == 3 {
			return fmt.Errorf("remote create %s: %w", path, fs.ErrExist)
		}
		return helperFailure("create", path, hr)
	}
	if hr.stdoutOver {
		return fmt.Errorf("remote create %s: helper answer exceeded %d bytes", path, sshFilesMaxHelperBytes)
	}
	return nil
}

// sshCreateNewScript creates the empty file $1 exclusively. $2 is an optional octal mode
// (empty leaves it to the remote umask). Exit 3 means the path already exists (the caller
// maps it to fs.ErrExist); any other failure is the ordinary helper error. A chmod failure
// does NOT remove the path: between the exclusive create and the chmod another process
// could have replaced it, and a helper that deletes on its own failure could delete that
// process's file. The caller gets an error and the empty file this helper created stays.
const sshCreateNewScript = `p=$1
mode=$2
if (set -C; : > "$p") 2>/dev/null; then :; else
  if [ -e "$p" ] || [ -L "$p" ]; then echo "create $p: already exists" >&2; exit 3; fi
  echo "create $p: cannot create" >&2; exit 2
fi
if [ -n "$mode" ]; then chmod "$mode" "$p" || exit 2; fi
`

// sshCreateNewContentScript creates $1 exclusively and writes stdin through the same
// noclobber descriptor: set -C makes exec 3> open O_EXCL-like, then cat >&3 uses fd 3, so
// the path is never reopened after the claim. $2 is an optional octal mode (empty leaves
// it to the remote umask). Exit 3 means something already exists at the path (the caller
// maps it to fs.ErrExist); a write or chmod failure exits 2 and leaves this caller's
// claimed file rather than deleting a path another process may have replaced.
const sshCreateNewContentScript = `p=$1
mode=$2
set -C
if { exec 3>"$p"; } 2>/dev/null; then
  if ! cat >&3; then exec 3>&-; echo "create $p: write failed" >&2; exit 2; fi
  exec 3>&-
else
  if [ -e "$p" ] || [ -L "$p" ]; then echo "create $p: already exists" >&2; exit 3; fi
  echo "create $p: cannot create" >&2; exit 2
fi
if [ -n "$mode" ]; then chmod "$mode" "$p" || exit 2; fi
`
