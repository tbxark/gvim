// Package workspace holds the project root that gvim works in and the list of
// roots used recently. A root is a directory: the file explorer shows it, and
// the finder and open-file paths resolve against it.
//
// The recent list is JSON at gvim/recent.json under os.UserConfigDir. Only the
// standard library is used.
package workspace

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
)

// ErrNotDirectory is returned by Open and Switch when the path is not a
// directory, including when it does not exist. Callers test it with errors.Is.
var ErrNotDirectory = errors.New("not a directory")

// MaxRecent is the most roots kept in the recent list.
const MaxRecent = 10

// errCorrupt marks a recent list that exists but cannot be parsed.
var errCorrupt = errors.New("workspace: recent list is corrupt")

// Root is a project directory. The zero value has no path.
type Root struct {
	path string
}

// Path returns the absolute path of the root with symlinks resolved.
func (r Root) Path() string {
	return r.path
}

// Open checks that path is a directory and returns it as a Root. A relative
// path is resolved against the current directory, and symlinks are followed.
// Open does not change the recent list.
//
// A path that is not a directory, or does not exist, returns an error that
// wraps ErrNotDirectory. The message is "not a directory: <absolute path>".
func Open(path string) (Root, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return Root{}, fmt.Errorf("workspace: open %q: %w", path, err)
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR) {
			return Root{}, fmt.Errorf("%w: %s", ErrNotDirectory, abs)
		}
		return Root{}, fmt.Errorf("workspace: open %q: %w", path, err)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return Root{}, fmt.Errorf("workspace: open %q: %w", path, err)
	}
	if !info.IsDir() {
		return Root{}, fmt.Errorf("%w: %s", ErrNotDirectory, abs)
	}
	return Root{path: resolved}, nil
}

// Switch opens path with Open and puts it at the front of the recent list,
// removing any earlier entry for the same root.
//
// If the path is not a directory, Switch returns the zero Root and the Open
// error, and the recent list is unchanged. If the root is valid but the recent
// list cannot be saved, Switch returns the valid Root together with an error.
// Callers should check Root.Path before treating the call as failed, so they
// can still re-root while reporting the save problem.
func Switch(path string) (Root, error) {
	r, err := Open(path)
	if err != nil {
		return Root{}, err
	}
	if err := remember(r); err != nil {
		return r, fmt.Errorf("workspace: record recent root: %w", err)
	}
	return r, nil
}

// Recent returns the recent roots, most recent first, with at most MaxRecent
// entries. Entries that are no longer directories are left out. A missing
// recent list gives an empty result and no error. A list that cannot be
// parsed gives an error; the next Switch replaces it.
func Recent() ([]Root, error) {
	paths, err := load()
	if err != nil {
		return nil, err
	}
	var roots []Root
	for _, p := range paths {
		if info, err := os.Stat(p); err == nil && info.IsDir() {
			roots = append(roots, Root{path: p})
		}
	}
	return roots, nil
}

// recentFile is the on-disk form of the recent list.
type recentFile struct {
	Roots []string `json:"roots"`
}

// recentPath returns the location of the recent list in the user config dir.
func recentPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("workspace: config dir: %w", err)
	}
	return filepath.Join(dir, "gvim", "recent.json"), nil
}

// load reads the stored root paths. A missing file is an empty list.
func load() ([]string, error) {
	path, err := recentPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("workspace: read recent list: %w", err)
	}
	var f recentFile
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("%w: %s: %v", errCorrupt, path, err)
	}
	return f.Roots, nil
}

// remember writes r at the front of the recent list. A corrupt list is
// replaced; any other read error is returned without writing.
func remember(r Root) error {
	path, err := recentPath()
	if err != nil {
		return err
	}
	existing, err := load()
	if err != nil && !errors.Is(err, errCorrupt) {
		return err
	}
	roots := []string{r.path}
	for _, p := range existing {
		if len(roots) == MaxRecent {
			break
		}
		if p != r.path {
			roots = append(roots, p)
		}
	}
	return write(path, roots)
}

// write replaces the file at path with roots. It writes a temporary file in
// the same directory and renames it, so a crash cannot leave a partial list.
func write(path string, roots []string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(recentFile{Roots: roots}, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp, err := os.CreateTemp(dir, ".recent-*.json")
	if err != nil {
		return err
	}
	_, werr := tmp.Write(data)
	cerr := tmp.Close()
	if err := errors.Join(werr, cerr); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	return nil
}
