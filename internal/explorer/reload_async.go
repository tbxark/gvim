package explorer

import (
	"errors"
	"path/filepath"
	"strings"
)

// Snapshot names the loaded directories of a tree. T.Snapshot takes it on the
// goroutine that owns the tree, and it reads nothing from disk. Read lists the
// directories and touches no tree, so it may run in a command. Apply then merges the
// listing into the tree on the owner's goroutine.
type Snapshot struct {
	root string
	dirs []string // the loaded directories, the root first
}

// Snapshot returns the loaded directories of t. It reads nothing from disk.
func (t *Tree) Snapshot() Snapshot {
	var dirs []string
	var walk func(n *node)
	walk = func(n *node) {
		if !n.loaded {
			return
		}
		dirs = append(dirs, n.path)
		for _, c := range n.children {
			if c.isDir {
				walk(c)
			}
		}
	}
	walk(t.root)
	return Snapshot{root: t.root.path, dirs: dirs}
}

// SnapshotFor is Snapshot plus the folders on the way to path that are not loaded yet.
// Read lists those as well, and Apply loads them, so RevealLoaded can then reveal path
// with no disk work. An empty path, a path with no unloaded folder on its way, and a
// path outside the tree add nothing, so SnapshotFor("") is Snapshot. It reads nothing
// from disk.
func (t *Tree) SnapshotFor(path string) Snapshot {
	snap := t.Snapshot()
	snap.dirs = append(snap.dirs, t.waypoints(path)...)
	return snap
}

// Listing is the disk state of the directories of a Snapshot, as Read found it.
type Listing struct {
	entries map[string][]Entry // by directory path
	errs    []error            // the directories that could not be read
}

// Read lists the directories of s from disk, with the same options that Reload
// uses. A directory that cannot be read is left out, and its error is kept for
// Apply. Read touches no tree, so it may run on any goroutine.
func (s Snapshot) Read() Listing {
	l := Listing{entries: make(map[string][]Entry, len(s.dirs))}
	for _, dir := range s.dirs {
		entries, err := listDir(dir, ListOptions{ShowHidden: true, ShowIgnored: true, IgnoreRoot: s.root})
		if err != nil {
			l.errs = append(l.errs, err)
			continue
		}
		l.entries[dir] = entries
	}
	return l
}

// Apply merges l into t the way Reload merges the disk: entries that are gone are
// dropped, new entries are added, and the nodes that remain keep their expansion. A
// directory that l does not list keeps its entries as they are. It returns the read
// errors that Read kept, joined. Apply does no disk work, so it must run on the
// goroutine that owns t.
func (t *Tree) Apply(l Listing) error {
	t.applyListing(t.root, l)
	return errors.Join(l.errs...)
}

// applyListing merges the listing of n, when it has one, and then the loaded
// directories below n. An unloaded directory is merged only when the listing has it,
// which is how a SnapshotFor listing loads the folders on the way to a path.
func (t *Tree) applyListing(n *node, l Listing) {
	entries, listed := l.entries[n.path]
	if !n.loaded && !listed {
		return
	}
	if listed {
		fresh := make([]*node, 0, len(entries))
		for _, e := range entries {
			fresh = append(fresh, &node{
				name:    e.Name,
				path:    filepath.Join(n.path, e.Name),
				isDir:   e.IsDir,
				hidden:  e.IsHidden,
				ignored: n.ignored || e.IsIgnored,
				parent:  n,
			})
		}
		old := make(map[string]*node, len(n.children))
		for _, c := range n.children {
			old[c.path] = c
		}
		kept := make([]*node, 0, len(fresh))
		for _, f := range fresh {
			o, ok := old[f.path]
			if !ok || o.isDir != f.isDir {
				kept = append(kept, f)
				continue
			}
			delete(old, f.path)
			o.ignored = f.ignored
			kept = append(kept, o)
		}
		for _, gone := range old {
			t.forget(gone)
		}
		t.adopt(n, kept)
	}
	for _, c := range n.children {
		if c.isDir {
			t.applyListing(c, l)
		}
	}
}

// waypoints returns the folders on the way to path that are not loaded, from the
// first one down to the parent of path, as paths. Reveal expands each folder on the
// way, so a snapshot that lists them lets the reveal run on the applied listing. It
// returns nil when the way is loaded, when path is outside the tree, or when a folder
// on the way is not in the tree or is hidden or ignored, since Reveal refuses those.
func (t *Tree) waypoints(path string) []string {
	if path == "" {
		return nil
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil
	}
	rel, err := filepath.Rel(t.root.path, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return nil
	}
	var parts []string
	for part := range strings.SplitSeq(rel, string(filepath.Separator)) {
		if part != "." && part != "" {
			parts = append(parts, part)
		}
	}
	if len(parts) == 0 {
		return nil
	}
	n := t.root
	for i, part := range parts[:len(parts)-1] {
		next := t.child(n, part)
		if next == nil || !next.isDir || !t.visible(next) {
			return nil
		}
		if !next.loaded {
			dirs := []string{next.path}
			p := next.path
			for _, deeper := range parts[i+1 : len(parts)-1] {
				p = filepath.Join(p, deeper)
				dirs = append(dirs, p)
			}
			return dirs
		}
		n = next
	}
	return nil
}
