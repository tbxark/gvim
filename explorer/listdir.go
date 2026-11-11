package explorer

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/go-git/go-billy/v5/osfs"
	"github.com/go-git/go-git/v5/plumbing/format/gitignore"
)

// GitDirName is the repository metadata directory. No listing of the explorer
// returns it, whatever ListOptions.ShowHidden says: it holds the object store and
// refs, the tree has no use for it, and reading it stalls the explorer. The name
// rule also skips a linked worktree's ".git" pointer file.
const GitDirName = ".git"

// ListOptions selects the entries ListDir returns. The zero value hides dot-
// entries and gitignored entries, as the tree does by default.
type ListOptions struct {
	ShowHidden  bool // include names starting with "."
	ShowIgnored bool // include entries matched by a .gitignore
	// IgnoreRoot is the directory whose .gitignore files apply to the listed
	// directory: the files from IgnoreRoot down to dir, as the tree applies them
	// from its root. Empty means dir is its own root. It applies whether or not
	// ShowIgnored is set, since it decides Entry.IsIgnored.
	IgnoreRoot  string
	Fast        bool // skip per-entry lstat calls for regular files; only stat symlinks
	IgnoreCache map[string]ignoreCacheEntry
}

// Entry is one directory entry returned by ListDir. For a symbolic link, IsDir,
// Size and ModTime describe the target when it exists; a dangling link keeps
// the link's own values.
type Entry struct {
	Name      string
	IsDir     bool
	IsSymlink bool
	IsHidden  bool
	IsIgnored bool // matched by a .gitignore; only listed when ShowIgnored is set
	Size      int64
	ModTime   time.Time
}

// ListDir returns the entries of dir in tree display order: directories first,
// then names in natural, case-insensitive order (see naturalCompare). It is the
// listing the tree uses, without the tree's loading state, so a file browser can
// share it. An entry named GitDirName is never returned, not even with ShowHidden.
func ListDir(dir string, opts ListOptions) ([]Entry, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("explorer: list %q: %w", dir, err)
	}
	dirents, err := os.ReadDir(abs)
	if err != nil {
		return nil, fmt.Errorf("explorer: list %q: %w", dir, err)
	}

	root := abs
	if opts.IgnoreRoot != "" {
		if root, err = filepath.Abs(opts.IgnoreRoot); err != nil {
			return nil, fmt.Errorf("explorer: list %q: %w", dir, err)
		}
	}
	rel, err := relSegments(root, abs)
	if err != nil {
		return nil, fmt.Errorf("explorer: list %q: %w", dir, err)
	}
	m := gitignore.NewMatcher(ignoreRules(root, rel, opts.IgnoreCache))

	out := make([]Entry, 0, len(dirents))
	for _, d := range dirents {
		name := d.Name()
		if name == GitDirName {
			continue
		}
		hidden := strings.HasPrefix(name, ".")
		if hidden && !opts.ShowHidden {
			continue
		}
		var isDir bool
		var isSymlink bool
		var size int64
		var modTime time.Time
		if opts.Fast {
			isDir = d.IsDir()
			isSymlink = d.Type()&os.ModeSymlink != 0
			if isSymlink {
				if target, err := os.Stat(filepath.Join(abs, name)); err == nil {
					isDir = target.IsDir()
				}
			}
		} else {
			info, dir, err := entryInfo(filepath.Join(abs, name), d)
			if err != nil {
				continue // removed since ReadDir
			}
			isDir = dir
			isSymlink = d.Type()&os.ModeSymlink != 0
			size = info.Size()
			modTime = info.ModTime()
		}
		ignored := m.Match(append(slices.Clip(rel), name), isDir)
		if ignored && !opts.ShowIgnored {
			continue
		}
		out = append(out, Entry{
			Name:      name,
			IsDir:     isDir,
			IsSymlink: isSymlink,
			IsHidden:  hidden,
			IsIgnored: ignored,
			Size:      size,
			ModTime:   modTime,
		})
	}
	slices.SortFunc(out, func(a, b Entry) int {
		return entryOrder(a.IsDir, a.Name, b.IsDir, b.Name)
	})
	return out, nil
}

// entryInfo returns the file info ListDir reports for the entry at p: that of
// the target for a symbolic link that resolves, otherwise the entry's own. isDir
// is the directory test the tree uses, which follows links.
func entryInfo(p string, d os.DirEntry) (info os.FileInfo, isDir bool, err error) {
	if d.Type()&os.ModeSymlink != 0 {
		if target, err := os.Stat(p); err == nil {
			return target, target.IsDir(), nil
		}
	}
	info, err = d.Info()
	return info, d.IsDir(), err
}

// relSegments returns the path segments of dir below root. It is nil when dir
// is root itself and an error when dir is outside root.
func relSegments(root, dir string) ([]string, error) {
	rel, err := filepath.Rel(root, dir)
	if err != nil {
		return nil, err
	}
	if rel == "." {
		return nil, nil
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return nil, fmt.Errorf("%q is outside %q", dir, root)
	}
	return strings.Split(filepath.ToSlash(rel), "/"), nil
}

// ignoreRules returns the patterns that apply to the entries of the directory
// rel below root: the .gitignore files at root and at each directory on the way
// down, in that order, so deeper files come later and take precedence.
type ignoreCacheEntry struct {
	modTime time.Time
	exists  bool
	pats    []gitignore.Pattern
}

func ignoreRules(root string, rel []string, cache map[string]ignoreCacheEntry) []gitignore.Pattern {
	fsys := osfs.New(root)
	var rules []gitignore.Pattern
	for i := 0; i <= len(rel); i++ {
		subDir := filepath.Join(append([]string{root}, rel[:i]...)...)
		gitPath := filepath.Join(subDir, gitignoreFile)
		fi, err := os.Stat(gitPath)
		exists := err == nil
		var mtime time.Time
		if exists {
			mtime = fi.ModTime()
		}
		if cache != nil {
			if c, ok := cache[gitPath]; ok && c.exists == exists && c.modTime.Equal(mtime) {
				rules = append(rules, c.pats...)
				continue
			}
		}
		pats := readIgnore(fsys, rel[:i])
		if cache != nil {
			cache[gitPath] = ignoreCacheEntry{
				modTime: mtime,
				exists:  exists,
				pats:    pats,
			}
		}
		rules = append(rules, pats...)
	}
	return rules
}
