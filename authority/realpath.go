package authority

// RealPath is the authority's optional canonical-path capability: resolve every
// symbolic link and return the canonical absolute path on the machine the workspace
// lives on. It is deliberately not part of FileSystem so backends that cannot readlink
// (and third-party fakes) keep compiling; callers use the realpath capability check and
// return a clear unsupported error rather than falling back to the local machine.
//
// The local backend uses os.EvalSymlinks. The SSH backend runs one bounded helper over
// the existing carrier (realpath, or readlink -f; the remote must have one of them) and
// requires an absolute answer. Missing paths, symlink loops, a host without either tool,
// cancellation and over-cap answers are all clear errors; no path is guessed.

import (
	"context"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
)

// RealPather is a FileSystem that can resolve symbolic links on its own machine.
type RealPather interface {
	// RealPath returns the canonical absolute path of path, resolving symlinks.
	RealPath(ctx context.Context, path string) (string, error)
}

// RealPathResolver returns the realpath capability of files, or false when the backend
// does not provide one.
func RealPathResolver(files FileSystem) (RealPather, bool) {
	r, ok := files.(RealPather)
	return r, ok
}

// RealPath implements RealPather on the local backend.
func (l *local) RealPath(ctx context.Context, path string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if path == "" {
		return "", fmt.Errorf("realpath: empty path: %w", fs.ErrInvalid)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return filepath.Clean(real), nil
}

// RealPath implements RealPather over SSH: one bounded helper. realpath is preferred,
// readlink -f is the fallback; both resolve every component including the final one.
func (f *sshFiles) RealPath(ctx context.Context, path string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if path == "" {
		// A whitespace-only path is a legal file name: only the empty string (and a NUL,
		// which no filesystem path can contain) is invalid.
		return "", fmt.Errorf("remote realpath: empty path: %w", fs.ErrInvalid)
	}
	if strings.ContainsRune(path, 0) {
		return "", fmt.Errorf("remote realpath: path contains NUL: %w", fs.ErrInvalid)
	}
	hr, err := f.execScript(ctx, sshRealPathScript, []string{path}, nil, sshFilesMaxHelperBytes, sshFilesMaxStderrBytes)
	if err != nil {
		return "", err
	}
	if hr.res.Code != 0 {
		return "", helperFailure("realpath", path, hr)
	}
	if hr.stdoutOver {
		return "", fmt.Errorf("remote realpath %s: answer exceeded %d bytes", path, sshFilesMaxHelperBytes)
	}
	// The helper prints exactly <path>\n and the shell has already removed the tool's
	// one newline: strip exactly one trailing LF and no CR. A file name may legally end
	// with newlines or a carriage return, so trimming more would silently return a wrong
	// path; the helper also rejects a missing final path instead of relying on
	// readlink -f, which can report a path that does not exist.
	raw := string(hr.res.Stdout)
	if !strings.HasSuffix(raw, "\n") {
		return "", fmt.Errorf("remote realpath %s: helper answer did not end with a newline", path)
	}
	out := raw[:len(raw)-1]
	if out == "" {
		return "", fmt.Errorf("remote realpath %s: empty answer", path)
	}
	if !strings.HasPrefix(out, "/") || strings.ContainsRune(out, 0) {
		return "", fmt.Errorf("remote realpath %s: unexpected answer %q", path, out)
	}
	return out, nil
}

// sshRealPathScript resolves $1 to a canonical absolute path. It prefers realpath and
// falls back to readlink -f, both of which resolve the final component too; a relative
// path is anchored at the helper's physical working directory. Missing paths fail with
// the tool's error (mapped to not-found), and a host with neither tool is a clear error.
const sshRealPathScript = `p=$1
case $p in
  "") echo "realpath: empty path" >&2; exit 2 ;;
  /*) ;;
  *) p=$(pwd -P)/$p || exit 2 ;;
esac
# Capture with an appended sentinel: $(...) strips trailing newlines, which would eat a
# file name that ends with one. The sentinel keeps every newline of the tool's answer,
# then the last character (the sentinel) is removed and exactly one tool newline is
# stripped. The result is printed with one newline and Go strips exactly that one.
if command -v realpath >/dev/null 2>&1; then
  raw=$(realpath "$p" && printf x) || exit 1
elif command -v readlink >/dev/null 2>&1 && readlink -f / >/dev/null 2>&1; then
  raw=$(readlink -f "$p" && printf x) || exit 1
else
  echo "realpath: the remote host has neither realpath nor readlink -f" >&2
  exit 3
fi
raw=${raw%x}
case $raw in
  *"
") out=${raw%?} ;;
  *) echo "realpath: helper answer did not end with a newline" >&2; exit 3 ;;
esac
if [ -z "$out" ]; then echo "realpath $p: empty answer" >&2; exit 1; fi
# A missing final component (and a broken symlink) must fail like the local
# EvalSymlinks, even under readlink -f which can report a nonexistent path.
if [ ! -e "$out" ]; then echo "realpath $p: no such file" >&2; exit 1; fi
printf '%s\n' "$out"`
