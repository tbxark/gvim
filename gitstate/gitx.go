// Package gitx reads the state of a git work tree with go-git: the work tree
// root, the checked-out branch, and the status of each changed file.
package gitstate

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/format/index"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// ErrNotRepo is returned when a directory is not inside a git repository.
var ErrNotRepo = errors.New("gitx: not a git repository")

// FileStatus is the state of one changed path in a work tree. The zero value
// is not a valid status.
type FileStatus uint8

const (
	Untracked FileStatus = iota + 1
	Added
	Modified
	Deleted
	Renamed
	Conflicted
)

const shortHashLen = 7

// emptyBlob is the blob every empty file shares. Identical empty files are not
// paired as renames, because any empty file would match any other.
var emptyBlob = plumbing.ComputeHash(plumbing.BlobObject, nil)

func (s FileStatus) String() string {
	switch s {
	case Untracked:
		return "untracked"
	case Added:
		return "added"
	case Modified:
		return "modified"
	case Deleted:
		return "deleted"
	case Renamed:
		return "renamed"
	case Conflicted:
		return "conflicted"
	}
	return fmt.Sprintf("FileStatus(%d)", uint8(s))
}

// FindRoot walks upward from dir and returns the first directory that holds a
// .git entry, as an absolute path. A .git file (linked worktrees and
// submodules) counts. ok is false when no ancestor has one.
func FindRoot(dir string) (root string, ok bool) {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return "", false
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

// Status returns the status of every changed path under root, keyed by its
// slash-separated path relative to root. Clean paths are absent. Ignored
// paths are absent too. root must be the work tree root, as returned by
// FindRoot; any other directory returns ErrNotRepo.
//
// A path with an unmerged index entry is Conflicted, whatever else it shows.
// A staged deletion and a staged addition with identical content are reported
// as one Renamed path, keyed by the new name. Renames whose content also
// changed are reported as Deleted and Added.
func Status(root string) (map[string]FileStatus, error) {
	repo, err := open(root)
	if err != nil {
		return nil, err
	}
	wt, err := repo.Worktree()
	if err != nil {
		return nil, fmt.Errorf("gitx: worktree: %w", err)
	}
	st, err := wt.Status()
	if err != nil {
		return nil, fmt.Errorf("gitx: status: %w", err)
	}
	idx, err := repo.Storer.Index()
	if err != nil {
		return nil, fmt.Errorf("gitx: index: %w", err)
	}

	out := make(map[string]FileStatus, len(st))
	for path, fs := range st {
		if s, ok := classify(fs); ok {
			out[path] = s
		}
	}
	for _, e := range idx.Entries {
		// Stage 0 is a merged entry. Stages 1 to 3 are the base, ours and
		// theirs sides of an unmerged path. go-git's index.Merged constant is 1
		// and must not be used for this comparison.
		if e.Stage != 0 {
			out[e.Name] = Conflicted
		}
	}
	if err := pairRenames(repo, idx, st, out); err != nil {
		return nil, err
	}
	return out, nil
}

// Branch returns the short name of the checked-out branch under root. An
// unborn branch reports its name. A detached HEAD reports the first seven hex
// digits of its commit. root must be the work tree root, as with Status.
func Branch(root string) (string, error) {
	repo, err := open(root)
	if err != nil {
		return "", err
	}
	head, err := repo.Reference(plumbing.HEAD, false)
	if err != nil {
		return "", fmt.Errorf("gitx: read HEAD: %w", err)
	}
	if head.Type() == plumbing.SymbolicReference {
		return head.Target().Short(), nil
	}
	return head.Hash().String()[:shortHashLen], nil
}

// open opens the repository whose work tree is root. It does not search
// parent directories.
func open(root string) (*git.Repository, error) {
	repo, err := git.PlainOpenWithOptions(root, &git.PlainOpenOptions{})
	if errors.Is(err, git.ErrRepositoryNotExists) {
		return nil, ErrNotRepo
	}
	if err != nil {
		return nil, fmt.Errorf("gitx: open %s: %w", root, err)
	}
	return repo, nil
}

// classify maps the go-git status of one path to a FileStatus. It reports
// false for a clean path. A path that is deleted on disk shows as Deleted even
// when it is also staged as added.
func classify(fs *git.FileStatus) (FileStatus, bool) {
	switch {
	case fs.Staging == git.Unmodified && fs.Worktree == git.Unmodified:
		return 0, false
	case fs.Worktree == git.Untracked:
		return Untracked, true
	case fs.Staging == git.Deleted || fs.Worktree == git.Deleted:
		return Deleted, true
	case fs.Staging == git.Added:
		return Added, true
	default:
		return Modified, true
	}
}

// pairRenames turns each staged deletion whose HEAD blob equals the blob of a
// staged addition into one Renamed entry at the new path. Each deletion pairs
// at most once. Paths are visited in sorted order, so the pairing is stable.
func pairRenames(repo *git.Repository, idx *index.Index, st git.Status, out map[string]FileStatus) error {
	head, err := repo.Head()
	if errors.Is(err, plumbing.ErrReferenceNotFound) {
		return nil // no commit yet, so nothing can be a staged deletion
	}
	if err != nil {
		return fmt.Errorf("gitx: head: %w", err)
	}
	commit, err := repo.CommitObject(head.Hash())
	if err != nil {
		return fmt.Errorf("gitx: commit: %w", err)
	}
	tree, err := commit.Tree()
	if err != nil {
		return fmt.Errorf("gitx: tree: %w", err)
	}

	deleted := make(map[plumbing.Hash][]string)
	for _, path := range slices.Sorted(maps.Keys(st)) {
		if out[path] != Deleted || st[path].Staging != git.Deleted {
			continue
		}
		entry, err := tree.FindEntry(path)
		if errors.Is(err, object.ErrEntryNotFound) {
			continue
		}
		if err != nil {
			return fmt.Errorf("gitx: tree entry %s: %w", path, err)
		}
		deleted[entry.Hash] = append(deleted[entry.Hash], path)
	}
	if len(deleted) == 0 {
		return nil
	}

	blobs := make(map[string]plumbing.Hash, len(idx.Entries))
	for _, e := range idx.Entries {
		if e.Stage == 0 {
			blobs[e.Name] = e.Hash
		}
	}
	for _, path := range slices.Sorted(maps.Keys(st)) {
		if out[path] != Added {
			continue
		}
		hash, ok := blobs[path]
		if !ok || hash == emptyBlob {
			continue
		}
		olds := deleted[hash]
		if len(olds) == 0 {
			continue
		}
		out[path] = Renamed
		delete(out, olds[0])
		deleted[hash] = olds[1:]
	}
	return nil
}
