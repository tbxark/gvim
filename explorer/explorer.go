// Package explorer models the file-explorer sidebar: a lazily loaded tree of
// directories and files, flattened into the rows the sidebar shows.
//
// Ordering matches Fresh's file tree (crates/fresh-editor/src/view/file_tree):
// directories come before files, and names are compared naturally, so
// "chapter-2" sorts before "chapter-10" and letter case is ignored. Entries
// whose name starts with "." are hidden by default, as in the legacy explorer.
// Entries matched by a .gitignore are hidden by default too
// (refs/fresh/docs/features/file-explorer.md, "Visibility and .gitignore"). Each
// directory's own .gitignore applies to it and below. SetShowHidden and
// SetShowIgnored lift the two rules.
package explorer

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ErrNotDir is returned by Open and Toggle for a file path.
var ErrNotDir = errors.New("explorer: not a directory")

// ErrNotFound is returned when a path is outside the tree or does not exist.
var ErrNotFound = errors.New("explorer: path not in tree")

// Row is one visible line of the sidebar.
type Row struct {
	Name     string
	Path     string
	Depth    int  // 0 is the root; compacted ancestors do not indent the row
	IsDir    bool // directories can be expanded
	Expanded bool // meaningful only when IsDir
	// Parent is the index in Rows of the nearest ancestor that has a row, or -1
	// for the root. Compact mode skips the ancestors it folds into this row, so
	// a parent is the row the sticky band pins, not necessarily the node's
	// immediate parent.
	Parent int
	// Chain is the directory chain compact mode folded into this row, outermost
	// first: the ancestors whose single-child directories share this row. It is
	// nil when compact mode is off or this row starts no chain.
	Chain []ChainSegment
}

// ChainSegment is one folder folded into a compact row: the name it shows and
// the directory it stands for.
type ChainSegment struct {
	Name string
	Path string
}

type node struct {
	name     string
	path     string
	isDir    bool
	hidden   bool // name starts with "."
	ignored  bool // matched by a .gitignore
	parent   *node
	children []*node
	loaded   bool // children have been read from disk
	expanded bool
}

// Tree is the explorer model. It is not safe for concurrent use; the TUI owns
// it from one goroutine.
type Tree struct {
	root        *node
	byPath      map[string]*node
	selected    string // path of the selected row; empty means the first row
	scroll      int    // index in Rows of the first row in view
	showHidden  bool   // rows include dot-entries
	showIgnored bool   // rows include entries matched by .gitignore
	compact     bool   // rows fold single-child directory chains
	cachedRows  []Row
	dirty       bool
	ignoreCache map[string]ignoreCacheEntry
}

// Open returns a tree rooted at dir, expanded, with its first level loaded.
// The root row is always shown; its name is the base name of dir.
func Open(dir string) (*Tree, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("explorer: open %q: %w", dir, err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, fmt.Errorf("explorer: open %q: %w", dir, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("explorer: open %q: %w", dir, ErrNotDir)
	}
	t := &Tree{byPath: make(map[string]*node), ignoreCache: make(map[string]ignoreCacheEntry), dirty: true}
	t.root = &node{name: filepath.Base(abs), path: abs, isDir: true, expanded: true}
	t.byPath[abs] = t.root
	if err := t.load(t.root); err != nil {
		return nil, err
	}
	t.selected = abs
	return t, nil
}

// Root returns the path of the tree root.
func (t *Tree) Root() string { return t.root.path }

// ShowHidden reports whether dot-entries are drawn. The default is false.
func (t *Tree) ShowHidden() bool { return t.showHidden }

// SetShowHidden draws dot-entries when v is true and hides them when false. The
// tree reads every directory it loads with dot-entries included, so the change
// needs no disk access.
func (t *Tree) SetShowHidden(v bool) {
	if t.showHidden != v {
		t.showHidden = v
		t.dirty = true
	}
}

// ShowIgnored reports whether entries matched by .gitignore are drawn. The
// default is false.
func (t *Tree) ShowIgnored() bool { return t.showIgnored }

// SetShowIgnored draws gitignored entries when v is true and hides them when
// false. Like SetShowHidden it needs no disk access.
func (t *Tree) SetShowIgnored(v bool) {
	if t.showIgnored != v {
		t.showIgnored = v
		t.dirty = true
	}
}

// CompactDirectories reports whether compact-directory rendering is on: a folder
// whose expanded, visible content is exactly one folder shares its row with that
// folder, as "src/main/java". The default is off, so a tree built directly keeps
// the plain rows; the app turns it on from file_explorer.compact_directories.
func (t *Tree) CompactDirectories() bool { return t.compact }

// SetCompactDirectories turns compact-directory rendering on or off. It needs no
// disk access: the chains are read off the loaded tree. A selection on a folder
// that compact mode folds into a deeper row moves to that row's anchor.
func (t *Tree) SetCompactDirectories(v bool) {
	if t.compact == v {
		return
	}
	t.compact = v
	t.dirty = true
	if n, ok := t.byPath[t.selected]; ok {
		t.selected = t.anchor(n).path
	}
}

// visible reports whether n is drawn under the current options.
func (t *Tree) visible(n *node) bool {
	return (t.showHidden || !n.hidden) && (t.showIgnored || !n.ignored)
}

// Rows returns the visible rows in display order. The root is row 0.
//
// With compact directories on, a folder that is expanded and whose only visible
// child is another folder has no row of its own: its children are walked as its
// own depth, and the chain of such ancestors is carried into the row of the
// first descendant that does have one (Fresh's projection in
// view/file_tree/view.rs). Row.Depth is the raw depth less the folded ancestors,
// so a chain does not indent.
func (t *Tree) Rows() []Row {
	if !t.dirty && t.cachedRows != nil {
		return t.cachedRows
	}
	rows := make([]Row, 0, 16)
	// depth is the row's depth on screen: a folded ancestor adds a chain segment
	// but no indent, so an absorbed node's children are walked at its own depth.
	var walk func(n *node, depth, parent int, chain []ChainSegment)
	walk = func(n *node, depth, parent int, chain []ChainSegment) {
		if t.isAbsorbed(n) {
			inner := make([]ChainSegment, len(chain), len(chain)+1)
			copy(inner, chain)
			inner = append(inner, ChainSegment{Name: n.name, Path: n.path})
			for _, c := range n.children {
				if t.visible(c) {
					walk(c, depth, parent, inner)
				}
			}
			return
		}
		index := len(rows)
		rows = append(rows, Row{
			Name:     n.name,
			Path:     n.path,
			Depth:    depth,
			IsDir:    n.isDir,
			Expanded: n.expanded,
			Parent:   parent,
			Chain:    chain,
		})
		if !n.expanded {
			return
		}
		for _, c := range n.children {
			if t.visible(c) {
				walk(c, depth+1, index, nil)
			}
		}
	}
	walk(t.root, 0, -1, nil)
	t.cachedRows = rows
	t.dirty = false
	return rows
}

// isAbsorbed reports whether compact mode folds n into a deeper row: n is not
// the root, it is an expanded folder, and its only visible child is a folder.
// Fresh's rule (FileTreeView::is_absorbed); the root is never absorbed.
func (t *Tree) isAbsorbed(n *node) bool {
	if !t.compact || n == nil || n == t.root {
		return false
	}
	return t.singleVisibleDirChild(n) != nil
}

// singleVisibleDirChild returns n's only visible child when that child is a
// folder, and nil when n is not an expanded folder with exactly one visible
// folder child.
func (t *Tree) singleVisibleDirChild(n *node) *node {
	if n == nil || !n.isDir || !n.expanded {
		return nil
	}
	var only *node
	for _, c := range n.children {
		if !t.visible(c) {
			continue
		}
		if only != nil {
			return nil
		}
		only = c
	}
	if only == nil || !only.isDir {
		return nil
	}
	return only
}

// anchor returns the node whose row shows n's place: n itself when it has a row,
// or, for a folder compact mode folds away, the deepest node of its chain. The
// selection is kept on an anchor, so Selected always finds a row (Fresh's
// promote_to_anchor).
func (t *Tree) anchor(n *node) *node {
	for t.isAbsorbed(n) {
		n = t.singleVisibleDirChild(n)
	}
	return n
}

// Selected returns the selected row. ok is false only if the tree is empty of
// rows, which cannot happen for a tree made by Open.
func (t *Tree) Selected() (Row, bool) {
	rows := t.Rows()
	for _, r := range rows {
		if r.Path == t.selected {
			return r, true
		}
	}
	if len(rows) == 0 {
		return Row{}, false
	}
	return rows[0], true
}

// Move changes the selection by delta visible rows, clamped to the first and
// last row. A selection that is no longer visible (its ancestor was collapsed)
// moves from the first row.
func (t *Tree) Move(delta int) {
	rows := t.Rows()
	if len(rows) == 0 {
		return
	}
	idx := 0
	for i, r := range rows {
		if r.Path == t.selected {
			idx = i
			break
		}
	}
	idx = min(max(idx+delta, 0), len(rows)-1)
	t.selected = rows[idx].Path
}

// Select selects the visible row for path. It returns ErrNotFound when the row
// is not visible; use Reveal to expand the way to it first. A folder compact mode
// folds into a deeper row selects that row's anchor, since it has no row of its
// own.
func (t *Tree) Select(path string) error {
	n, err := t.find(path)
	if err != nil {
		return err
	}
	want := t.anchor(n).path
	for _, r := range t.Rows() {
		if r.Path == want {
			t.selected = want
			return nil
		}
	}
	return ErrNotFound
}

// RowAt returns the row drawn at line y of the sidebar body, where y 0 is the
// first body line. The scroll offset is applied. ok is false when y is above
// the body or past the last row.
func (t *Tree) RowAt(y int) (Row, bool) {
	rows := t.Rows()
	i := t.offset(len(rows)) + y
	if y < 0 || i >= len(rows) {
		return Row{}, false
	}
	return rows[i], true
}

// ScrollBy moves the first row in view by n rows; a negative n moves up. The
// offset is clamped to the rows of the tree. The tree does not know the body
// height, so the caller clamps against that if it must.
func (t *Tree) ScrollBy(n int) {
	count := len(t.Rows())
	t.scroll = min(max(t.offset(count)+n, 0), max(count-1, 0))
}

// ScrollOffset returns the index in Rows of the first row in view.
func (t *Tree) ScrollOffset() int { return t.offset(len(t.Rows())) }

// offset is the scroll offset clamped to a tree of count rows.
func (t *Tree) offset(count int) int { return min(t.scroll, max(count-1, 0)) }

// Reveal expands every ancestor of path, loading directories as needed, and
// then selects path. It is how the sidebar follows the active file. It reads the
// disk for a directory that is not loaded, so the UI calls RevealLoaded instead.
func (t *Tree) Reveal(path string) error { return t.reveal(path, true) }

// RevealLoaded is Reveal for the folders that a SnapshotFor listing has loaded. It
// reads nothing from disk, so the UI can call it on the update loop. It returns
// ErrNotFound when a folder on the way to path is not loaded.
func (t *Tree) RevealLoaded(path string) error { return t.reveal(path, false) }

// reveal is Reveal. When load is false it stops at a folder that is not loaded.
func (t *Tree) reveal(path string, load bool) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("explorer: reveal %q: %w", path, err)
	}
	rel, err := filepath.Rel(t.root.path, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return ErrNotFound
	}
	n := t.root
	for part := range strings.SplitSeq(rel, string(filepath.Separator)) {
		if part == "." || part == "" {
			continue
		}
		if !n.isDir {
			return ErrNotFound
		}
		if !load && !n.loaded {
			return ErrNotFound
		}
		if err := t.expand(n); err != nil {
			return err
		}
		next := t.child(n, part)
		if next == nil || !t.visible(next) {
			return ErrNotFound
		}
		n = next
	}
	// A folder compact mode folds into a deeper row has no row of its own: the
	// selection follows the row that shows it.
	t.selected = t.anchor(n).path
	return nil
}

// Expand opens the directory at path, loading its children if needed. Expanding
// an open directory does nothing. It returns ErrNotDir for a file and
// ErrNotFound for a path that is not in the tree.
//
// With compact directories on, expanding also expands the single-child folder
// chain below it (Fresh's expand_with_chain), so the whole chain still renders as
// one row.
func (t *Tree) Expand(path string) error {
	n, err := t.find(path)
	if err != nil {
		return err
	}
	if !n.isDir {
		return ErrNotDir
	}
	return t.expandWithChain(n)
}

// expandWithChain expands n and, with compact directories on, walks down the
// single visible folder children, expanding each, so the chain reveals on one
// row. It stops at a folder with zero or several visible children, or a visible
// child that is not a folder.
func (t *Tree) expandWithChain(n *node) error {
	if err := t.expand(n); err != nil {
		return err
	}
	if !t.compact {
		return nil
	}
	for {
		next := t.singleVisibleDirChild(n)
		if next == nil {
			t.promoteSelection()
			return nil
		}
		if err := t.expand(next); err != nil {
			return err
		}
		n = next
	}
}

// promoteSelection moves the selection onto its row's anchor when compact mode
// folded the selected folder into a deeper row, so Selected always finds a row.
func (t *Tree) promoteSelection() {
	if !t.compact {
		return
	}
	if n, ok := t.byPath[t.selected]; ok {
		t.selected = t.anchor(n).path
	}
}

// Collapse closes the directory at path. Its children stay loaded. Errors are
// those of Expand.
func (t *Tree) Collapse(path string) error {
	n, err := t.find(path)
	if err != nil {
		return err
	}
	if !n.isDir {
		return ErrNotDir
	}
	n.expanded = false
	t.dirty = true
	return nil
}

// Toggle expands a collapsed directory or collapses an expanded one. Expanding
// follows the compact-directory chain like Expand; collapsing closes the row's
// own folder only.
func (t *Tree) Toggle(path string) error {
	n, err := t.find(path)
	if err != nil {
		return err
	}
	if !n.isDir {
		return ErrNotDir
	}
	if n.expanded {
		n.expanded = false
		t.dirty = true
		return nil
	}
	return t.expandWithChain(n)
}

// Reload re-reads every loaded directory from disk, including the .gitignore
// files, so entries created, removed or ignored since the last read show up.
// Expanded directories stay expanded, and the selection is kept while its path
// still exists. Entries that vanished are dropped. Errors from directories that
// cannot be read are joined and returned; the rest of the tree is still reloaded.
func (t *Tree) Reload() error {
	t.ignoreCache = make(map[string]ignoreCacheEntry)
	t.dirty = true
	return t.reload(t.root)
}

// Refresh is Reload. The UI calls it when the watcher reports a change under the
// root.
func (t *Tree) Refresh() error { return t.Reload() }

func (t *Tree) reload(n *node) error {
	if !n.loaded {
		return nil
	}
	fresh, err := t.scan(n)
	if err != nil {
		return err
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
	var errs []error
	for _, c := range kept {
		if err := t.reload(c); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// forget removes n and its loaded descendants from the path index.
func (t *Tree) forget(n *node) {
	delete(t.byPath, n.path)
	for _, c := range n.children {
		t.forget(c)
	}
}

func (t *Tree) find(path string) (*node, error) {
	n, ok := t.byPath[path]
	if !ok || (n.hidden && !t.showHidden) {
		return nil, ErrNotFound
	}
	return n, nil
}

func (t *Tree) child(n *node, name string) *node {
	for _, c := range n.children {
		if c.name == name {
			return c
		}
	}
	return nil
}

func (t *Tree) expand(n *node) error {
	if !n.loaded {
		if err := t.load(n); err != nil {
			return err
		}
	}
	n.expanded = true
	return nil
}

// load reads the children of a directory once and indexes them.
func (t *Tree) load(n *node) error {
	children, err := t.scan(n)
	if err != nil {
		return err
	}
	t.adopt(n, children)
	return nil
}

// adopt makes children the entries of n and indexes them by path.
func (t *Tree) adopt(n *node, children []*node) {
	for _, c := range children {
		t.byPath[c.path] = c
	}
	n.children = children
	n.loaded = true
	t.dirty = true
}

// listDir is the directory read behind every scan and Snapshot.Read. Tests swap it
// to count the reads.
var listDir = ListDir

// scan reads the entries of directory n with ListDir. It returns every entry in
// display order, with hidden and ignored set on each; Rows decides which ones to
// draw. ListDir is given the tree root, so the ignore rules are the .gitignore
// files on disk from the root down to n, read at this scan.
func (t *Tree) scan(n *node) ([]*node, error) {
	entries, err := listDir(n.path, ListOptions{ShowHidden: true, ShowIgnored: true, IgnoreRoot: t.root.path, Fast: true, IgnoreCache: t.ignoreCache})
	if err != nil {
		return nil, err
	}
	children := make([]*node, 0, len(entries))
	for _, e := range entries {
		children = append(children, &node{
			name:   e.Name,
			path:   filepath.Join(n.path, e.Name),
			isDir:  e.IsDir,
			hidden: e.IsHidden,
			// git excludes everything under an excluded directory, so a
			// negation cannot re-include an entry whose parent is ignored.
			ignored: n.ignored || e.IsIgnored,
			parent:  n,
		})
	}
	return children, nil
}

// entryOrder sorts directories before files, then by natural name order.
func entryOrder(aDir bool, aName string, bDir bool, bName string) int {
	switch {
	case aDir && !bDir:
		return -1
	case !aDir && bDir:
		return 1
	}
	return naturalCompare(aName, bName)
}

// naturalCompare orders names the way Fresh's file tree does: runs of ASCII
// digits compare by numeric value, and everything else compares with ASCII
// letters folded to lower case. Among equal values, the shorter spelling sorts
// first ("v1" before "v01"), matching GNU sort -V.
func naturalCompare(a, b string) int {
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		ca, cb := a[i], b[j]
		if isDigit(ca) && isDigit(cb) {
			ei, ej := digitRunEnd(a, i), digitRunEnd(b, j)
			da := strings.TrimLeft(a[i:ei], "0")
			db := strings.TrimLeft(b[j:ej], "0")
			if c := cmpInt(len(da), len(db)); c != 0 {
				return c
			}
			if c := strings.Compare(da, db); c != 0 {
				return c
			}
			if c := cmpInt(ei-i, ej-j); c != 0 {
				return c
			}
			i, j = ei, ej
			continue
		}
		if la, lb := lower(ca), lower(cb); la != lb {
			return cmpInt(int(la), int(lb))
		}
		i++
		j++
	}
	return cmpInt(len(a)-i, len(b)-j)
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

func digitRunEnd(s string, i int) int {
	for i < len(s) && isDigit(s[i]) {
		i++
	}
	return i
}

func lower(c byte) byte {
	if c >= 'A' && c <= 'Z' {
		return c + ('a' - 'A')
	}
	return c
}

func cmpInt(a, b int) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}
