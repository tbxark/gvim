package app

import (
	"path/filepath"

	"github.com/tbxark/gvim/internal/gitx"
)

// gitStatus is the root's GitStatus port over internal/gitx. It finds the work tree
// that holds the folder and returns the status letter of each changed path, keyed
// by absolute path.
type gitStatus struct{}

// Marks returns the status letters of the work tree that holds dir. A folder
// outside a repository has none.
func (gitStatus) Marks(dir string) (map[string]rune, error) {
	root, ok := gitx.FindRoot(dir)
	if !ok {
		return nil, nil
	}
	st, err := gitx.Status(root)
	if err != nil {
		return nil, err
	}
	out := make(map[string]rune, len(st))
	for rel, s := range st {
		out[filepath.Join(root, filepath.FromSlash(rel))] = gitMark(s)
	}
	return out, nil
}

// gitMark is the letter the explorer shows for a changed path.
func gitMark(s gitx.FileStatus) rune {
	switch s {
	case gitx.Added:
		return 'A'
	case gitx.Modified:
		return 'M'
	case gitx.Deleted:
		return 'D'
	case gitx.Untracked:
		return 'U'
	case gitx.Renamed:
		return 'R'
	case gitx.Conflicted:
		return 'C'
	}
	return '?'
}
