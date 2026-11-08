package uiadapter

import (
	"path/filepath"

	"github.com/tbxark/gvim/gitstate"
)

// GitStatus is the root's GitStatus port over internal/gitstate. It finds the work tree
// that holds the folder and returns the status letter of each changed path, keyed
// by absolute path.
type GitStatus struct{}

// Marks returns the status letters of the work tree that holds dir. A folder
// outside a repository has none.
func (GitStatus) Marks(dir string) (map[string]rune, error) {
	root, ok := gitstate.FindRoot(dir)
	if !ok {
		return nil, nil
	}
	st, err := gitstate.Status(root)
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
func gitMark(s gitstate.FileStatus) rune {
	switch s {
	case gitstate.Added:
		return 'A'
	case gitstate.Modified:
		return 'M'
	case gitstate.Deleted:
		return 'D'
	case gitstate.Untracked:
		return 'U'
	case gitstate.Renamed:
		return 'R'
	case gitstate.Conflicted:
		return 'C'
	}
	return '?'
}
