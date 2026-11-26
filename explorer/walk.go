package explorer

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
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
	// ScanExclusions are gitignore-style patterns relative to root
	// (file_scan_exclusions): a match is never visited, files and directories
	// alike.
	ScanExclusions []string
	// ScanInclusions are gitignore-style patterns relative to root
	// (file_scan_inclusions): a match is visited even when a .gitignore matches
	// it, and an ignored directory is descended so included files under it are
	// found. Exclusions win over inclusions.
	ScanInclusions []string
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
	var customRules []IgnorePattern
	var initMatcher *IgnoreMatcher
	if !opts.ShowIgnored {
		customRules = ParsePatterns(opts.CustomIgnorePatterns)
		if len(customRules) > 0 {
			initMatcher = NewIgnoreMatcher(customRules)
		}
	}
	w := walker{
		ctx:         ctx,
		root:        abs,
		opts:        opts,
		skip:        skip,
		customRules: customRules,
		scan:        scanMatchersFor(opts.ScanExclusions, opts.ScanInclusions),
		fn:          fn,
	}
	if err := w.dir(nil, nil, initMatcher, true, false); err != nil {
		return err
	}
	return nil
}

type walker struct {
	ctx         context.Context
	root        string
	opts        WalkOptions
	skip        map[string]bool // opts.SkipDirs as a set
	customRules []IgnorePattern
	scan        scanMatchers
	fn          func(rel string) bool
	stop        bool // fn returned false
}

// dir visits the directory rel below root. rules and m are the patterns and
// matcher inherited from its ancestors, and parentIgnored says whether the
// directory itself is ignored (the walk descends an ignored directory only to
// reach files a scan inclusion names). It returns an error only for the top
// directory, or for a done context.
func (w *walker) dir(rel []string, rules []IgnorePattern, m *IgnoreMatcher, top, parentIgnored bool) error {
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
		if local := readIgnore(w.root, rel); len(local) > 0 {
			nextRules := make([]IgnorePattern, len(rules)+len(local))
			copy(nextRules, rules)
			copy(nextRules[len(rules):], local)
			rules = nextRules

			if len(w.customRules) == 0 {
				m = NewIgnoreMatcher(rules)
			} else {
				allRules := make([]IgnorePattern, len(rules)+len(w.customRules))
				copy(allRules, rules)
				copy(allRules[len(rules):], w.customRules)
				m = NewIgnoreMatcher(allRules)
			}
		}
	}
	for _, e := range entries {
		name := e.Name()
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
		if w.scan.excluded(childRel, isDir) {
			continue
		}
		// A scan inclusion also rescues a hidden name (".env" is hidden *and*
		// ignored): it is what puts the file in the finder/grep scope.
		included := w.scan.included(childRel, isDir)
		if !w.opts.ShowHidden && strings.HasPrefix(name, ".") && !included {
			continue
		}
		ignored := parentIgnored || (!w.opts.ShowIgnored && m != nil && m.Match(childRel, isDir))
		if ignored && included {
			ignored = false
		}
		if isDir {
			if w.skip[name] {
				continue
			}
			if ignored && !w.opts.ShowIgnored && !w.scan.hasInclusions() {
				continue
			}
			if err := w.dir(childRel, rules, m, false, ignored); err != nil {
				return err
			}
		} else {
			if ignored && !w.opts.ShowIgnored {
				continue
			}
			if !w.fn(strings.Join(childRel, "/")) {
				w.stop = true
			}
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
