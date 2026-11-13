package finder

// Remote enumeration for a remote workspace: the authority file system is the only
// source, the local disk is never consulted. It streams into the same Index the local
// finder uses, so the palette's poll-while-loading and stale-query handling are shared.

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// RemoteEntry is one directory entry of the remote file system: its name and kind
// ("file", "dir", "symlink"; an empty kind is treated as a file).
type RemoteEntry struct {
	Name string
	Kind string
}

// RemoteFS is the part of the remote file system the finder enumerates. The app adapts
// authority.Files() to it; the finder never imports the authority package.
type RemoteFS interface {
	ReadDir(ctx context.Context, path string, hidden bool) ([]RemoteEntry, error)
}

// Bounds of the remote walk: directory visits and nesting depth, so a huge or hostile
// remote tree cannot run away. Files are bounded by MaxFiles, the shared cap.
const (
	MaxRemoteDirs  = 20000
	MaxRemoteDepth = 64
)

// ErrRemoteTooManyDirs is the error of a remote walk that visited more directories than
// MaxRemoteDirs. The files found so far stay in the index.
var ErrRemoteTooManyDirs = fmt.Errorf("finder: remote enumeration stopped after %d directories", MaxRemoteDirs)

// remoteDir is one pending directory of the walk.
type remoteDir struct {
	abs   string
	rel   string // slash-separated, empty for the root
	depth int
}

// EnumerateRemote calls emit with each remote file under root as a slash-separated path
// relative to root, until emit returns false, ctx is done, or the walk bounds are hit.
// The root read failure is the error; an unreadable subdirectory is skipped so one bad
// branch does not drop the rest. It never touches the local file system: fs is the only
// source.
func EnumerateRemote(ctx context.Context, fs RemoteFS, root string, opts Options, emit func(rel string) bool) error {
	if fs == nil {
		return errors.New("finder: remote enumeration without a file system")
	}
	stack := []remoteDir{{abs: root}}
	visited := 0
	for len(stack) > 0 {
		if err := ctx.Err(); err != nil {
			return err
		}
		d := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if d.depth > MaxRemoteDepth {
			continue
		}
		visited++
		if visited > MaxRemoteDirs {
			return ErrRemoteTooManyDirs
		}
		entries, err := fs.ReadDir(ctx, d.abs, opts.ShowHidden)
		if err != nil {
			if d.rel == "" {
				return err
			}
			continue
		}
		sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
		for _, e := range entries {
			if !safeRemoteName(e.Name) {
				continue
			}
			if !opts.ShowHidden && strings.HasPrefix(e.Name, ".") {
				continue
			}
			rel := e.Name
			if d.rel != "" {
				rel = d.rel + "/" + e.Name
			}
			abs := joinRemotePath(d.abs, e.Name)
			if e.Kind == "dir" {
				if skipDirName(e.Name) {
					continue
				}
				stack = append(stack, remoteDir{abs: abs, rel: rel, depth: d.depth + 1})
				continue
			}
			if !emit(rel) {
				return nil
			}
		}
	}
	return nil
}

// safeRemoteName accepts one directory-entry name of the remote file system: a single
// path element with no separators, parent/self references or NUL, so a remote system
// cannot hand the walk a name that escapes the root. Symlinks are never traversed (only
// entries the remote reports as directories are), so a link cannot widen the walk either.
func safeRemoteName(name string) bool {
	if name == "" || name == "." || name == ".." {
		return false
	}
	return !strings.ContainsAny(name, "/\\\x00")
}

// joinRemotePath appends one safe name to a remote directory (slash-separated), keeping
// the root's leading slash.
func joinRemotePath(dir, name string) string {
	if dir == "" || dir == "/" {
		return "/" + name
	}
	return strings.TrimSuffix(dir, "/") + "/" + name
}

// skipDirName reports whether the local walk's skipDirs list excludes a directory.
func skipDirName(name string) bool {
	for _, skip := range skipDirs {
		if name == skip {
			return true
		}
	}
	return false
}

// Idle returns an index that is finished, has no files and is safe to read: a remote
// launch uses it before its authority slot is connected, so no local enumeration ever
// starts in the meantime.
func Idle() *Index {
	x := &Index{cancel: func() {}, done: make(chan struct{}), idle: true}
	close(x.done)
	return x
}
