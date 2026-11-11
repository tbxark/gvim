package app

// Remote .gitignore filtering (task-136). The local explorer and file walker match
// go-git's gitignore patterns read from each directory's .gitignore file. A remote
// workspace has no local files to read, so this index reads the ignore files through
// authority.Files() instead (never the local disk) and applies the same parser and
// matcher: root and nested .gitignore files, negation and anchored patterns behave as
// locally.
//
// The reads are bounded: at most remoteIgnoreMaxDepth directory levels are consulted,
// one .gitignore is at most remoteIgnoreFileMaxBytes and the accumulated ruleset is
// capped at remoteIgnoreMaxRules. A missing file is normal; an oversized or unreadable
// one, or a depth/rule overflow, degrades to "rules not applied" and records one
// notice, so the listing still works (with ignore filtering incomplete) rather than
// failing or silently pretending nothing is ignored.
//
// One ignoreBatch lives for one listing/reload (or one remote finder walk): each
// .gitignore is read once per batch, so an edit that the directory watch/reload sees is
// applied on the next pass. The remote finder keeps its batch for one index lifetime;
// .gitignore edits are picked up when the index restarts (reconnect/manual restart) — a
// recorded residual.

import (
	"context"
	"errors"
	"io/fs"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/go-git/go-git/v5/plumbing/format/gitignore"

	"github.com/tbxark/gvim/authority"
	"github.com/tbxark/gvim/explorer"
)

const (
	// remoteIgnoreMaxDepth bounds the directory levels whose .gitignore files are read.
	remoteIgnoreMaxDepth = 64
	// remoteIgnoreFileMaxBytes bounds one remote .gitignore read.
	remoteIgnoreFileMaxBytes = 64 << 10
	// remoteIgnoreMaxRules bounds the accumulated patterns of one directory.
	remoteIgnoreMaxRules = 4096
	// remoteIgnoreReadTimeout bounds one remote .gitignore read.
	remoteIgnoreReadTimeout = 5 * time.Second
)

// ignoreBatch reads and caches the remote ignore files of one listing/walk, and the
// matcher of each directory listed.
type ignoreBatch struct {
	files func() authority.FileSystem
	root  string

	mu       sync.Mutex
	cache    map[string][]gitignore.Pattern
	matchers map[string]gitignore.Matcher
	notice   string
}

// newIgnoreBatch returns a batch for the remote workspace root over files. files is a
// function so a reconnected authority resolves the current carrier; a nil result is
// treated as "no ignore files" (filtering is not applicable), never a local fallback.
func newIgnoreBatch(files func() authority.FileSystem, root string) *ignoreBatch {
	return &ignoreBatch{files: files, root: root, cache: map[string][]gitignore.Pattern{}, matchers: map[string]gitignore.Matcher{}}
}

// Notice returns the one-shot degradation notice, if any.
func (b *ignoreBatch) Notice() string {
	if b == nil {
		return ""
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.notice
}

// setNotice records the first degradation notice.
func (b *ignoreBatch) setNotice(text string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.notice == "" {
		b.notice = text
	}
}

// relParts returns dir's path segments relative to the workspace root (nil for the root
// itself, and nil for a directory outside the root).
func (b *ignoreBatch) relParts(dir string) []string {
	root := path.Clean(b.root)
	clean := path.Clean(dir)
	if clean == root {
		return nil
	}
	prefix := root + "/"
	if !strings.HasPrefix(clean, prefix) {
		return nil
	}
	return strings.Split(strings.TrimPrefix(clean, prefix), "/")
}

// rulesFor returns the patterns that apply to dir: the .gitignore of every directory on
// the way from the workspace root down to dir, scoped to its directory.
func (b *ignoreBatch) rulesFor(ctx context.Context, dir string) []gitignore.Pattern {
	parts := b.relParts(dir)
	if len(parts) > remoteIgnoreMaxDepth {
		b.setNotice("remote .gitignore rules are not applied below 64 directory levels; ignore filtering may be incomplete")
		return nil
	}
	var out []gitignore.Pattern
	rules := 0
	for i := 0; i <= len(parts); i++ {
		dirPath := b.root
		if i > 0 {
			dirPath = path.Join(append([]string{b.root}, parts[:i]...)...)
		}
		patterns := b.ignoreFile(ctx, dirPath, parts[:i])
		if len(patterns) == 0 {
			continue
		}
		room := remoteIgnoreMaxRules - rules
		if len(patterns) > room {
			patterns = patterns[:room]
			b.setNotice("remote .gitignore rule limit reached; some rules were not applied")
		}
		rules += len(patterns)
		out = append(out, patterns...)
		if rules >= remoteIgnoreMaxRules {
			break
		}
	}
	return out
}

// ignoreFile reads and parses dir/.gitignore once per batch. It returns nil for a
// missing file and records a notice for an unreadable/oversized one.
func (b *ignoreBatch) ignoreFile(ctx context.Context, dir string, dirParts []string) []gitignore.Pattern {
	file := path.Join(dir, ".gitignore")
	b.mu.Lock()
	if cached, ok := b.cache[file]; ok {
		b.mu.Unlock()
		return cached
	}
	b.mu.Unlock()
	patterns := make([]gitignore.Pattern, 0, 8)
	if files := b.files(); files != nil {
		readCtx, cancel := context.WithTimeout(ctx, remoteIgnoreReadTimeout)
		data, err := files.ReadFile(readCtx, file, remoteIgnoreFileMaxBytes)
		cancel()
		switch {
		case err == nil:
			patterns = explorer.ParseIgnore(data, dirParts)
		case errors.Is(err, fs.ErrNotExist):
			// A directory without a .gitignore is normal.
		default:
			b.setNotice("some remote .gitignore files could not be read (size or permissions); ignore filtering may be incomplete")
		}
	}
	b.mu.Lock()
	b.cache[file] = patterns
	b.mu.Unlock()
	return patterns
}

// matcher returns the ignore matcher that applies to dir, built once per batch.
func (b *ignoreBatch) matcher(ctx context.Context, dir string) gitignore.Matcher {
	b.mu.Lock()
	m, ok := b.matchers[dir]
	b.mu.Unlock()
	if ok {
		return m
	}
	m = gitignore.NewMatcher(b.rulesFor(ctx, dir))
	b.mu.Lock()
	b.matchers[dir] = m
	b.mu.Unlock()
	return m
}

// ignored reports whether the entry name of dir is matched by the ignore rules that apply
// to dir. The path handed to the matcher is relative to the workspace root, as go-git
// expects.
func (b *ignoreBatch) ignored(ctx context.Context, dir, name string, isDir bool) bool {
	rel := append(append([]string(nil), b.relParts(dir)...), name)
	return b.matcher(ctx, dir).Match(rel, isDir)
}
