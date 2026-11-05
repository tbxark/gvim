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

// ListOptions selects the entries ListDir returns. The zero value hides dot-
// entries and gitignored entries, as the tree does by default.
type ListOptions struct {
	ShowHidden  bool // include names starting with "."
	ShowIgnored bool // include entries matched by a .gitignore
	// IgnoreRoot is the directory whose .gitignore files apply to the listed
	// directory: the files from IgnoreRoot down to dir, as the tree applies them
	// from its root. Empty means dir is its own root. Unused when ShowIgnored
	// is true.
	IgnoreRoot string
}

// Entry is one directory entry returned by ListDir. For a symbolic link, IsDir,
// Size and ModTime describe the target when it exists; a dangling link keeps
// the link's own values.
type Entry struct {
	Name      string
	IsDir     bool
	IsSymlink bool
	IsHidden  bool
	Size      int64
	ModTime   time.Time
}

// ListDir returns the entries of dir in tree display order: directories first,
// then names in natural, case-insensitive order (see naturalCompare). It is the
// listing the tree uses, without the tree's loading state, so a file browser can
// share it.
func ListDir(dir string, opts ListOptions) ([]Entry, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("explorer: list %q: %w", dir, err)
	}
	dirents, err := os.ReadDir(abs)
	if err != nil {
		return nil, fmt.Errorf("explorer: list %q: %w", dir, err)
	}

	var m gitignore.Matcher
	var rel []string
	if !opts.ShowIgnored {
		root := abs
		if opts.IgnoreRoot != "" {
			if root, err = filepath.Abs(opts.IgnoreRoot); err != nil {
				return nil, fmt.Errorf("explorer: list %q: %w", dir, err)
			}
		}
		if rel, err = relSegments(root, abs); err != nil {
			return nil, fmt.Errorf("explorer: list %q: %w", dir, err)
		}
		m = gitignore.NewMatcher(ignoreRules(root, rel))
	}

	out := make([]Entry, 0, len(dirents))
	for _, d := range dirents {
		name := d.Name()
		hidden := strings.HasPrefix(name, ".")
		if hidden && !opts.ShowHidden {
			continue
		}
		info, isDir, err := entryInfo(filepath.Join(abs, name), d)
		if err != nil {
			continue // removed since ReadDir
		}
		if !opts.ShowIgnored && m.Match(append(slices.Clip(rel), name), isDir) {
			continue
		}
		out = append(out, Entry{
			Name:      name,
			IsDir:     isDir,
			IsSymlink: d.Type()&os.ModeSymlink != 0,
			IsHidden:  hidden,
			Size:      info.Size(),
			ModTime:   info.ModTime(),
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
func ignoreRules(root string, rel []string) []gitignore.Pattern {
	fsys := osfs.New(root)
	var rules []gitignore.Pattern
	for i := 0; i <= len(rel); i++ {
		rules = append(rules, readIgnore(fsys, rel[:i])...)
	}
	return rules
}
