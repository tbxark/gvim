package explorer

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/go-git/go-billy/v5"
	"github.com/go-git/go-billy/v5/osfs"
	"github.com/go-git/go-git/v5/plumbing/format/gitignore"
)

// WalkOptions controls WalkFiles.
type WalkOptions struct {
	ShowHidden  bool // visit names starting with "."
	ShowIgnored bool // visit entries matched by a .gitignore
	// SkipDirs names directories that are never entered, such as ".git" or
	// "node_modules". The match is on the directory's own name at any depth.
	SkipDirs []string
}

// WalkFiles calls fn with the slash-separated path, relative to root, of every
// file under root, in lexical order by directory listing. A gitignored directory
// is not entered, as git does not enter an excluded directory. Symbolic links
// to directories are not followed, so a cycle cannot loop. Unreadable
// subdirectories are skipped. fn returns false to stop the walk, which then
// returns nil. An error is returned only when root itself cannot be read or
// ctx is done.
func WalkFiles(ctx context.Context, root string, opts WalkOptions, fn func(rel string) bool) error {
	abs, err := filepath.Abs(root)
	if err != nil {
		return fmt.Errorf("explorer: walk %q: %w", root, err)
	}
	skip := make(map[string]bool, len(opts.SkipDirs))
	for _, s := range opts.SkipDirs {
		skip[s] = true
	}
	w := walker{ctx: ctx, root: abs, fsys: osfs.New(abs), opts: opts, skip: skip, fn: fn}
	if err := w.dir(nil, nil, true); err != nil {
		return err
	}
	return nil
}

type walker struct {
	ctx  context.Context
	root string
	fsys billy.Filesystem // rooted at root; reads .gitignore files
	opts WalkOptions
	skip map[string]bool // opts.SkipDirs as a set
	fn   func(rel string) bool
	stop bool // fn returned false
}

// dir visits the directory rel below root. rules are the patterns inherited
// from its ancestors. It returns an error only for the top directory, or for a
// done context.
func (w *walker) dir(rel []string, rules []gitignore.Pattern, top bool) error {
	if err := w.ctx.Err(); err != nil {
		return err
	}
	abs := filepath.Join(w.root, filepath.Join(rel...))
	entries, err := os.ReadDir(abs)
	if err != nil {
		if top {
			return fmt.Errorf("explorer: walk %q: %w", abs, err)
		}
		return nil
	}
	var m gitignore.Matcher
	if !w.opts.ShowIgnored {
		rules = slices.Concat(rules, readIgnore(w.fsys, rel))
		m = gitignore.NewMatcher(rules)
	}
	for _, e := range entries {
		name := e.Name()
		if !w.opts.ShowHidden && strings.HasPrefix(name, ".") {
			continue
		}
		childRel := append(slices.Clip(rel), name)
		isDir := e.IsDir()
		if e.Type()&os.ModeSymlink != 0 {
			// A link to a directory is skipped: it is neither listed nor entered.
			info, err := os.Stat(filepath.Join(abs, name))
			isDir = err == nil && info.IsDir()
			if isDir {
				continue
			}
		}
		if !w.opts.ShowIgnored && m.Match(childRel, isDir) {
			continue
		}
		if isDir {
			if w.skip[name] {
				continue
			}
			if err := w.dir(childRel, rules, false); err != nil {
				return err
			}
		} else if !w.fn(strings.Join(childRel, "/")) {
			w.stop = true
		}
		if w.stop {
			return nil
		}
	}
	return nil
}
