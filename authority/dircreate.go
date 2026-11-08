package authority

// Atomic directory creation: the optional capability for callers that must claim a new
// directory without ever merging into one created concurrently (the remote plugin copy).
// It exists apart from FileMutations.Mkdir so a backend that cannot make the create
// atomic does not claim it: the caller then keeps its older check-then-create behaviour
// (and reports that residual) instead of trusting a check that races.

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"strings"
)

// DirectoryCreator atomically creates one new directory. It never replaces or merges
// into an existing entry: a path that already exists (file, directory or symlink) fails
// with an error wrapping fs.ErrExist, and missing ancestors fail with fs.ErrNotExist
// (this surface creates one level only, like os.Mkdir/mkdir(1)).
type DirectoryCreator interface {
	MkdirNew(ctx context.Context, path string) error
}

// DirectoryCreators returns the atomic-create surface of fs, or nil when the backend
// does not guarantee one.
func DirectoryCreators(fs FileSystem) DirectoryCreator {
	c, _ := fs.(DirectoryCreator)
	return c
}

// MkdirNew implements DirectoryCreator on the local backend with os.Mkdir (O_EXCL-like
// at the directory-entry level: mkdir fails atomically when the name exists).
func (l *local) MkdirNew(ctx context.Context, path string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if path == "" {
		return fmt.Errorf("mkdir: empty path: %w", fs.ErrInvalid)
	}
	return os.Mkdir(path, 0o755)
}

// MkdirNew implements DirectoryCreator over SSH with one plain mkdir helper (mkdir -p is
// deliberately not used: that would merge into an existing destination).
func (f *sshFiles) MkdirNew(ctx context.Context, path string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if path == "" {
		return fmt.Errorf("mkdir: empty path: %w", fs.ErrInvalid)
	}
	hr, err := f.execScript(ctx, sshMkdirNewScript, []string{path}, nil, sshFilesMaxHelperBytes, sshFilesMaxStderrBytes)
	if err != nil {
		return err
	}
	if hr.res.Code != 0 {
		lower := strings.ToLower(string(hr.res.Stderr))
		if strings.Contains(lower, "file exists") || strings.Contains(lower, "already exists") {
			return fmt.Errorf("mkdir %s: %w", path, fs.ErrExist)
		}
		return helperFailure("mkdir", path, hr)
	}
	if hr.stdoutOver {
		return fmt.Errorf("remote mkdir %s: answer exceeded %d bytes", path, sshFilesMaxHelperBytes)
	}
	return nil
}

var (
	_ DirectoryCreator = (*local)(nil)
	_ DirectoryCreator = (*sshFiles)(nil)
)

// sshMkdirNewScript creates one directory, never merging existing components.
const sshMkdirNewScript = `mkdir "$1"`
