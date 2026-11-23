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
	// RespectGitignore controls whether .gitignore files are read and applied. Default true.
	RespectGitignore *bool
	// CustomIgnorePatterns are additional wildcard patterns (e.g. *.log, build/).
	CustomIgnorePatterns []string
}

// RespectGitignoreEnabled reports whether gitignore is respected (default true).
func (o WalkOptions) RespectGitignoreEnabled() bool {
	return o.RespectGitignore == nil || *o.RespectGitignore
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
	var customRules []gitignore.Pattern
	var initMatcher gitignore.Matcher
	if !opts.ShowIgnored {
		for _, pat := range opts.CustomIgnorePatterns {
			pat = strings.TrimSpace(pat)
			if pat == "" || strings.HasPrefix(pat, "#") {
				continue
			}
			customRules = append(customRules, gitignore.ParsePattern(pat, nil))
		}
		if len(customRules) > 0 {
			initMatcher = gitignore.NewMatcher(customRules)
		}
	}
	w := walker{
		ctx:         ctx,
		root:        abs,
		fsys:        osfs.New(abs),
		opts:        opts,
		skip:        skip,
		customRules: customRules,
		fn:          fn,
	}
	if err := w.dir(nil, nil, initMatcher, true); err != nil {
		return err
	}
	return nil
}

type walker struct {
	ctx         context.Context
	root        string
	fsys        billy.Filesystem // rooted at root; reads .gitignore files
	opts        WalkOptions
	skip        map[string]bool // opts.SkipDirs as a set
	customRules []gitignore.Pattern
	fn          func(rel string) bool
	stop        bool // fn returned false
}

// dir visits the directory rel below root. rules and m are the patterns and
// matcher inherited from its ancestors. It returns an error only for the top
// directory, or for a done context.
func (w *walker) dir(rel []string, rules []gitignore.Pattern, m gitignore.Matcher, top bool) error {
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
	if !w.opts.ShowIgnored && w.opts.RespectGitignoreEnabled() && hasGitignoreEntry(entries) {
		if local := readIgnore(w.fsys, rel); len(local) > 0 {
			nextRules := make([]gitignore.Pattern, len(rules)+len(local))
			copy(nextRules, rules)
			copy(nextRules[len(rules):], local)
			rules = nextRules

			if len(w.customRules) == 0 {
				m = gitignore.NewMatcher(rules)
			} else {
				allRules := make([]gitignore.Pattern, len(rules)+len(w.customRules))
				copy(allRules, rules)
				copy(allRules[len(rules):], w.customRules)
				m = gitignore.NewMatcher(allRules)
			}
		}
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
		if !w.opts.ShowIgnored && m != nil && m.Match(childRel, isDir) {
			continue
		}
		if isDir {
			if w.skip[name] {
				continue
			}
			if err := w.dir(childRel, rules, m, false); err != nil {
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

func hasGitignoreEntry(entries []os.DirEntry) bool {
	for _, e := range entries {
		if e.Name() == gitignoreFile {
			return true
		}
	}
	return false
}
