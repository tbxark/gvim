package tui

import (
	"context"
	"errors"
	"fmt"
	"path"
	"sort"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/explorer"
)

// Remote explorer browsing (docs/architecture.md): the read-only file tree of a
// remote workspace. The tree keeps the loaded directories in memory and lists a
// directory only when the user expands it. The listing itself is remote I/O, so it
// runs in a tea.Cmd, never on the update loop; the command's message carries the
// entries and a generation, and the root applies it only while the tree and the
// request are current. There is no local filesystem fallback: every entry comes
// from the authority through a RemoteDirLoader, and a loader error is shown as a
// notice (a disconnected authority says so and the tree is reloaded on reconnect).
//
// The tree is read-only: the panel refuses rename/delete/create/cut/copy/paste with
// one explicit notice, and the app clears the local file-operation port. Fresh's
// remote explorer supports the full file operations over the authority; those are
// recorded raw gaps, not hidden local operations.

// DefaultRemoteExplorerTimeout bounds one remote directory read. The authority
// bounds its helper commands too; this is the outer deadline of the command.
const DefaultRemoteExplorerTimeout = 20 * time.Second

// ErrRemoteExplorerDisconnected is the notice text of a listing attempted while
// the workspace authority is disconnected. The tree is reloaded after a reconnect.
var ErrRemoteExplorerDisconnected = errors.New("remote workspace is disconnected; activate the window to reconnect")

// RemoteDirEntry is one entry a RemoteDirLoader listed. Hidden entries are the
// ones whose name starts with ".".
type RemoteDirEntry struct {
	Name   string
	IsDir  bool
	Hidden bool
	// Ignored is set when a remote .gitignore matches the entry; Rows hides it unless
	// the tree's show-ignored setting is on.
	Ignored bool
}

// RemoteDirLoader lists remote directories. The app implements it over
// authority.Files().ReadDir and the workspace trust gate. ReadDirs is called only
// from a tea.Cmd, and it must not read the local filesystem.
type RemoteDirLoader interface {
	// ReadDirs lists the directories named in dirs. A directory that cannot be
	// listed is left out of the map, and err describes why. It reports
	// ErrRemoteExplorerDisconnected (wrapped) when the authority is not connected.
	ReadDirs(ctx context.Context, dirs []string) (map[string][]RemoteDirEntry, error)
}

// remoteExplorerReloader is the async surface the root uses for a tree whose
// directory reads run in commands. The local explorer.Tree does not implement it.
type remoteExplorerReloader interface {
	ReloadCmd(reveal string) tea.Cmd
	ApplyReload(msg remoteTreeLoadedMsg) error
}

// remoteNode is one cached directory of the remote tree.
type remoteNode struct {
	path     string
	name     string
	isDir    bool
	hidden   bool
	ignored  bool
	parent   string
	children []string
	loaded   bool
	expanded bool
	loading  bool
	err      error
}

// RemoteExplorerTree is the read-only explorer tree of a remote workspace. It
// implements the root's ExplorerTree interface. Rows, selection and scrolling read
// only the cached nodes; ToggleCmd and ReloadCmd return the commands that list
// remote directories.
type RemoteExplorerTree struct {
	root    string
	loader  RemoteDirLoader
	timeout time.Duration

	mu          sync.Mutex
	nodes       map[string]*remoteNode
	selected    string
	scroll      int
	showHidden  bool
	showIgnored bool
	gen         uint64
	// reloadGen orders the whole-tree reloads apart from the folder toggles, so a toggle
	// during a reload does not make the reload's result look stale.
	reloadGen uint64
	pending   map[string]uint64
}

// NewRemoteExplorerTree returns the tree of root, whose directories come from
// loader. It reads nothing: the root row is shown until the first listing.
func NewRemoteExplorerTree(root string, loader RemoteDirLoader) *RemoteExplorerTree {
	name := path.Base(root)
	if root == "/" || root == "~" || name == "." || name == "/" {
		name = root
	}
	t := &RemoteExplorerTree{
		root: root, loader: loader, timeout: DefaultRemoteExplorerTimeout,
		nodes:   map[string]*remoteNode{},
		pending: map[string]uint64{},
	}
	t.nodes[root] = &remoteNode{path: root, name: name, isDir: true, expanded: true}
	return t
}

// Root returns the remote workspace directory.
func (t *RemoteExplorerTree) Root() string { return t.root }

// ReadOnly reports false: New File, New Directory, Rename, Delete and the clipboard
// operations (cut/copy/paste/duplicate of remote subtrees) are available on the remote
// tree through the app's remote file-operation adapter.
func (t *RemoteExplorerTree) ReadOnly() bool { return false }

// ClipboardDisabled reports false: remote subtrees can be cut, copied, pasted and
// duplicated through the authority (bounded, no local fallback). It remains for the
// panel's capability interface so another tree shape can still refuse the clipboard.
func (t *RemoteExplorerTree) ClipboardDisabled() bool { return false }

// TakeLoadNotice returns and clears a one-shot notice from the loader: degraded remote
// .gitignore filtering or a backend without directory watching (manual refresh remains).
func (t *RemoteExplorerTree) TakeLoadNotice() string {
	if noticer, ok := t.loader.(interface{ TakeNotice() string }); ok {
		return noticer.TakeNotice()
	}
	return ""
}

// ShowHidden reports whether dot entries are listed.
func (t *RemoteExplorerTree) ShowHidden() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.showHidden
}

// SetShowHidden includes or hides dot entries. It reads nothing: the loader always
// returns the hidden entries too, and rows filter them.
func (t *RemoteExplorerTree) SetShowHidden(v bool) {
	t.mu.Lock()
	t.showHidden = v
	t.mu.Unlock()
}

// ShowIgnored reports whether entries matched by a remote .gitignore are listed.
func (t *RemoteExplorerTree) ShowIgnored() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.showIgnored
}

// SetShowIgnored includes or hides remote entries matched by a .gitignore. It reads
// nothing: the loader always returns the ignore flag, and rows filter them.
func (t *RemoteExplorerTree) SetShowIgnored(v bool) {
	t.mu.Lock()
	t.showIgnored = v
	t.mu.Unlock()
}

// Rows returns the visible rows: the expanded directories' entries, directories
// first in natural order, as the local tree shows them. It reads only the cache.
func (t *RemoteExplorerTree) Rows() []explorer.Row {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.rowsLocked()
}

// rowsLocked flattens the expanded nodes. The caller holds t.mu.
func (t *RemoteExplorerTree) rowsLocked() []explorer.Row {
	rows := make([]explorer.Row, 0, 64)
	parents := map[string]int{}
	var add func(p string, depth int)
	add = func(p string, depth int) {
		n, ok := t.nodes[p]
		if !ok {
			return
		}
		parent := -1
		if depth > 0 {
			if i, ok := parents[p]; ok {
				parent = i
			}
		}
		row := explorer.Row{Name: n.name, Path: n.path, Depth: depth, IsDir: n.isDir, Expanded: n.expanded, Parent: parent}
		rows = append(rows, row)
		if !n.isDir || !n.expanded || !n.loaded {
			return
		}
		for _, child := range t.sortedChildrenLocked(n) {
			if !t.showHidden && strings.HasPrefix(child.name, ".") {
				continue
			}
			if !t.showIgnored && child.ignored {
				continue
			}
			parents[child.path] = len(rows) - 1
			add(child.path, depth+1)
		}
	}
	add(t.root, 0)
	return rows
}

// sortedChildrenLocked returns the visible children of n in display order.
func (t *RemoteExplorerTree) sortedChildrenLocked(n *remoteNode) []*remoteNode {
	out := make([]*remoteNode, 0, len(n.children))
	for _, p := range n.children {
		if c, ok := t.nodes[p]; ok {
			out = append(out, c)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].isDir != out[j].isDir {
			return out[i].isDir
		}
		return naturalLessRemote(out[i].name, out[j].name)
	})
	return out
}

// naturalLessRemote compares names like the local tree: case-insensitive, with
// digit runs compared numerically, so chapter-2 comes before chapter-10.
func naturalLessRemote(a, b string) bool {
	la, lb := strings.ToLower(a), strings.ToLower(b)
	i, j := 0, 0
	for i < len(la) && j < len(lb) {
		if isDigitByte(la[i]) && isDigitByte(lb[j]) {
			si, sj := i, j
			for i < len(la) && isDigitByte(la[i]) {
				i++
			}
			for j < len(lb) && isDigitByte(lb[j]) {
				j++
			}
			na, nb := strings.TrimLeft(la[si:i], "0"), strings.TrimLeft(lb[sj:j], "0")
			if len(na) != len(nb) {
				return len(na) < len(nb)
			}
			if na != nb {
				return na < nb
			}
			continue
		}
		if la[i] != lb[j] {
			return la[i] < lb[j]
		}
		i++
		j++
	}
	return len(la)-i < len(lb)-j
}

// isDigitByte reports whether b is an ASCII digit.
func isDigitByte(b byte) bool { return b >= '0' && b <= '9' }

// Selected returns the selected row, or the first row when none is selected.
func (t *RemoteExplorerTree) Selected() (explorer.Row, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	rows := t.rowsLocked()
	if len(rows) == 0 {
		return explorer.Row{}, false
	}
	for _, r := range rows {
		if r.Path == t.selected {
			return r, true
		}
	}
	return rows[0], true
}

// Move moves the selection by delta rows.
func (t *RemoteExplorerTree) Move(delta int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	rows := t.rowsLocked()
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

// ScrollBy scrolls the rows by n.
func (t *RemoteExplorerTree) ScrollBy(n int) {
	t.mu.Lock()
	t.scroll += n
	t.mu.Unlock()
}

// ScrollOffset returns the first row in view.
func (t *RemoteExplorerTree) ScrollOffset() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return min(t.scroll, max(len(t.rowsLocked())-1, 0))
}

// Expand marks a directory expanded. The root calls it for the tree root at
// startup, before the first listing: the intent applies when the listing lands.
func (t *RemoteExplorerTree) Expand(p string) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if n, ok := t.nodes[p]; ok && n.isDir {
		n.expanded = true
	}
	return nil
}

// RevealLoaded selects path when its directory chain is already loaded. A path in
// an unloaded directory is an error; the root then reloads the tree with the path
// pending and applies the reveal to the new listing.
func (t *RemoteExplorerTree) RevealLoaded(p string) error {
	return t.selectLoaded(p)
}

// Reveal is RevealLoaded for a tree that reads its directories in commands, and the
// root tries RevealLoaded first; it is here to satisfy ExplorerTree.
func (t *RemoteExplorerTree) Reveal(p string) error { return t.selectLoaded(p) }

// selectLoaded selects p when it is a loaded row.
func (t *RemoteExplorerTree) selectLoaded(p string) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.selectLoadedLocked(p)
}

// selectLoadedLocked is selectLoaded with t.mu held.
func (t *RemoteExplorerTree) selectLoadedLocked(p string) error {
	for _, r := range t.rowsLocked() {
		if r.Path == p {
			t.selected = p
			return nil
		}
	}
	return explorer.ErrNotFound
}

// Reload satisfies ExplorerTree. The root reloads a remote tree only through
// ReloadCmd (its directories are read in a command), so this synchronous form does
// nothing and reads nothing.
func (t *RemoteExplorerTree) Reload() error { return nil }

// Toggle flips a loaded directory. The panel uses ToggleCmd for this tree, so this
// synchronous form is the fallback for a directory that is already loaded.
func (t *RemoteExplorerTree) Toggle(p string) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	n, ok := t.nodes[p]
	if !ok {
		return explorer.ErrNotFound
	}
	if !n.isDir {
		return explorer.ErrNotDir
	}
	if !n.loaded {
		return errors.New("remote directory is not loaded yet")
	}
	n.expanded = !n.expanded
	t.selected = p
	return nil
}

// ToggleCmd returns the command that lists p and opens it, or nil when toggling is
// immediate (an already loaded directory). It never reads a remote directory; the
// command it returns does, and its message is applied by the root.
func (t *RemoteExplorerTree) ToggleCmd(p string) tea.Cmd {
	t.mu.Lock()
	n, ok := t.nodes[p]
	if !ok || !n.isDir {
		t.mu.Unlock()
		return nil
	}
	if n.loaded {
		n.expanded = !n.expanded
		t.selected = p
		t.mu.Unlock()
		return nil
	}
	if n.loading {
		t.mu.Unlock()
		return nil
	}
	n.loading = true
	t.gen++
	gen := t.gen
	t.pending[p] = gen
	loader := t.loader
	timeout := t.timeout
	t.mu.Unlock()
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		listings, err := loader.ReadDirs(ctx, []string{p})
		return remoteTreeToggleMsg{tree: t, path: p, gen: gen, entries: listings[p], err: err}
	}
}

// ReloadCmd returns the command that lists every loaded directory and the folders
// on the way to reveal (when reveal is set). The local reload path uses it for a
// remote tree; the command's result is applied by the root.
func (t *RemoteExplorerTree) ReloadCmd(reveal string) tea.Cmd {
	t.mu.Lock()
	t.reloadGen++
	gen := t.reloadGen
	dirs := t.loadedDirsLocked()
	dirs = append(dirs, t.waypointsLocked(reveal)...)
	dirs = dedupeStrings(dirs)
	loader := t.loader
	timeout := t.timeout
	t.mu.Unlock()
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		listings, err := loader.ReadDirs(ctx, dirs)
		return remoteTreeLoadedMsg{tree: t, gen: gen, listings: listings, reveal: reveal, err: err}
	}
}

// loadedDirsLocked returns the paths of the loaded directories, the root first.
func (t *RemoteExplorerTree) loadedDirsLocked() []string {
	dirs := make([]string, 0, len(t.nodes)+1)
	// The root is always listed: it is the first level the explorer shows.
	dirs = append(dirs, t.root)
	for p, n := range t.nodes {
		if p == t.root || !n.loaded {
			continue
		}
		dirs = append(dirs, p)
	}
	sort.Strings(dirs)
	return dirs
}

// waypointsLocked returns the directories on the way from the root to reveal that
// are not loaded yet (the parent directories of reveal, outermost first).
func (t *RemoteExplorerTree) waypointsLocked(reveal string) []string {
	if reveal == "" || t.root == "" {
		return nil
	}
	rel, ok := strings.CutPrefix(reveal, strings.TrimSuffix(t.root, "/")+"/")
	if !ok {
		return nil
	}
	parts := strings.Split(rel, "/")
	if len(parts) <= 1 {
		return nil
	}
	dir := strings.TrimSuffix(t.root, "/")
	var out []string
	for _, part := range parts[:len(parts)-1] {
		dir = path.Join(dir, part)
		if n, ok := t.nodes[dir]; ok && n.loaded {
			continue
		}
		out = append(out, dir)
	}
	return out
}

// ApplyReload applies a reload listing on the update loop. A stale generation (a
// newer reload started meanwhile) is dropped. It returns the read error, if any,
// so the root can show it.
func (t *RemoteExplorerTree) ApplyReload(msg remoteTreeLoadedMsg) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if msg.gen != t.reloadGen {
		return nil
	}
	if msg.err != nil {
		return msg.err
	}
	// Merge parents before their children, so a directory listing whose parent is
	// listed too finds the parent node (a map iteration is unordered).
	dirs := make([]string, 0, len(msg.listings))
	for dir := range msg.listings {
		dirs = append(dirs, dir)
	}
	sort.Slice(dirs, func(i, j int) bool {
		di, dj := strings.Count(dirs[i], "/"), strings.Count(dirs[j], "/")
		if di != dj {
			return di < dj
		}
		return dirs[i] < dirs[j]
	})
	for _, dir := range dirs {
		t.mergeLocked(dir, msg.listings[dir])
	}
	if root, ok := t.nodes[t.root]; ok && root.loaded {
		root.expanded = true
	}
	if msg.reveal != "" {
		reveal := strings.TrimSpace(msg.reveal)
		t.expandWaypointsLocked(reveal)
		_ = t.selectLoadedLocked(reveal)
	}
	return nil
}

// expandWaypointsLocked expands the directories on the way from the root to reveal,
// so the newly loaded path is visible in the rows after a reveal reload.
func (t *RemoteExplorerTree) expandWaypointsLocked(reveal string) {
	if reveal == "" || t.root == "" {
		return
	}
	rel, ok := strings.CutPrefix(reveal, strings.TrimSuffix(t.root, "/")+"/")
	if !ok {
		return
	}
	parts := strings.Split(rel, "/")
	if len(parts) <= 1 {
		return
	}
	dir := strings.TrimSuffix(t.root, "/")
	for _, part := range parts[:len(parts)-1] {
		dir = path.Join(dir, part)
		if n, ok := t.nodes[dir]; ok && n.isDir {
			n.expanded = true
		}
	}
}

// mergeLocked replaces the children of dir with entries, keeping the nodes that
// remain (and so their loaded subtrees and expansion).
func (t *RemoteExplorerTree) mergeLocked(dir string, entries []RemoteDirEntry) {
	n, ok := t.nodes[dir]
	if !ok || !n.isDir {
		return
	}
	fresh := make([]string, 0, len(entries))
	seen := make(map[string]bool, len(entries))
	for _, e := range entries {
		if e.Name == "" || e.Name == "." || e.Name == ".." {
			continue
		}
		if e.Name == explorer.GitDirName {
			// The local listing drops it (explorer.GitDirName), so the remote tree
			// never caches it and it has no row to expand.
			continue
		}
		cp := path.Join(dir, e.Name)
		if seen[cp] {
			continue
		}
		seen[cp] = true
		child, ok := t.nodes[cp]
		if !ok || child.isDir != e.IsDir {
			if ok {
				t.forgetLocked(cp)
			}
			child = &remoteNode{path: cp, name: e.Name, isDir: e.IsDir, hidden: strings.HasPrefix(e.Name, "."), parent: dir}
			t.nodes[cp] = child
		}
		child.hidden = strings.HasPrefix(e.Name, ".")
		child.ignored = e.Ignored
		fresh = append(fresh, cp)
	}
	for _, old := range n.children {
		if !seen[old] {
			t.forgetLocked(old)
		}
	}
	n.children = fresh
	n.loaded = true
	n.loading = false
	n.err = nil
	if dir == t.root {
		n.expanded = true
	}
}

// forgetLocked removes p and its subtree from the cache.
func (t *RemoteExplorerTree) forgetLocked(p string) {
	n, ok := t.nodes[p]
	if !ok {
		return
	}
	for _, c := range n.children {
		t.forgetLocked(c)
	}
	delete(t.nodes, p)
	if t.selected == p {
		t.selected = ""
	}
}

// applyToggle applies a toggle command's listing. A stale generation is dropped;
// the node keeps its old state and the error is returned to the root once.
func (t *RemoteExplorerTree) applyToggle(msg remoteTreeToggleMsg) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	n, ok := t.nodes[msg.path]
	if !ok || t.pending[msg.path] != msg.gen {
		return nil
	}
	delete(t.pending, msg.path)
	if n != nil {
		n.loading = false
	}
	if msg.err != nil {
		if n != nil {
			n.err = msg.err
		}
		return msg.err
	}
	t.mergeLocked(msg.path, msg.entries)
	if n, ok := t.nodes[msg.path]; ok {
		n.expanded = true
	}
	t.selected = msg.path
	return nil
}

// remoteTreeToggleMsg is the result of expanding a remote directory.
type remoteTreeToggleMsg struct {
	tree    *RemoteExplorerTree
	path    string
	gen     uint64
	entries []RemoteDirEntry
	err     error
}

// remoteTreeLoadedMsg is the result of a remote tree reload.
type remoteTreeLoadedMsg struct {
	tree     *RemoteExplorerTree
	gen      uint64
	listings map[string][]RemoteDirEntry
	reveal   string
	err      error
}

// dedupeStrings removes duplicates while keeping order.
func dedupeStrings(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := in[:0]
	for _, s := range in {
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

// remoteTreeToggleDone applies a remote toggle result on the update loop.
func (m *Model) remoteTreeToggleDone(msg remoteTreeToggleMsg) tea.Cmd {
	if msg.tree == nil || m.tree != ExplorerTree(msg.tree) {
		return nil
	}
	if err := msg.tree.applyToggle(msg); err != nil {
		m.notice = noticeRemoteExplorerError(err)
		return nil
	}
	m.followSelection()
	return nil
}

// remoteTreeLoadDone applies a remote reload result on the update loop. The reload
// bookkeeping is the same as the local background listing: a change during the read
// starts one more read, and a pending reveal is applied to the new rows.
func (m *Model) remoteTreeLoadDone(msg remoteTreeLoadedMsg) tea.Cmd {
	m.root.reload.running = false
	if msg.tree != nil && m.tree == ExplorerTree(msg.tree) {
		if err := msg.tree.ApplyReload(msg); err != nil {
			m.notice = noticeRemoteExplorerError(err)
		}
		if noticer, ok := m.tree.(interface{ TakeLoadNotice() string }); ok {
			if notice := noticer.TakeLoadNotice(); notice != "" {
				m.notice = notice
			}
		}
	}
	if m.root.reload.again {
		m.root.reload.again = false
		return m.reloadTreeAsync()
	}
	m.revealPending()
	return nil
}

// remoteExplorerInitialCmd loads the remote tree on start. A local tree returns nil
// (its first level is loaded synchronously by its opener).
func (m *Model) remoteExplorerInitialCmd() tea.Cmd {
	if _, ok := m.tree.(remoteExplorerReloader); !ok {
		return nil
	}
	return m.reloadTreeAsync()
}

// remoteExplorerReconnectCmd reloads the remote tree after the authority
// reconnected, clearing the disconnected error the last listing showed. A local
// tree returns nil.
func (m *Model) remoteExplorerReconnectCmd() tea.Cmd {
	if _, ok := m.tree.(remoteExplorerReloader); !ok {
		return nil
	}
	return m.reloadTreeAsync()
}

// noticeRemoteExplorerError formats a loader error for the status row, naming the
// workspace when the authority is disconnected.
func noticeRemoteExplorerError(err error) string {
	if errors.Is(err, ErrRemoteExplorerDisconnected) {
		return ErrRemoteExplorerDisconnected.Error()
	}
	if err != nil && strings.Contains(err.Error(), "remote workspace is disconnected") {
		return err.Error()
	}
	return fmt.Sprint(err)
}
