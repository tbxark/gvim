package ui

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/explorer"
	"github.com/tbxark/gvim/internal/theme"
	expanel "github.com/tbxark/gvim/internal/ui/explorer"
)

// newRealTreeModel returns a model whose explorer is a real tree over dir, with the
// first level loaded.
func newRealTreeModel(t *testing.T, dir string) (*Model, *explorer.Tree) {
	t.Helper()
	tree, err := explorer.Open(dir)
	must(t, err)
	m := New(Deps{
		Editor:   newFakeEditor("x"),
		Explorer: tree,
		Keys:     testKeys(),
		Theme:    theme.Theme{},
		Clock:    testClock,
	})
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	return m, tree
}

// listingFrom runs cmd and returns the tree listing it sends. It fails the test when
// cmd sends none.
func listingFrom(t *testing.T, cmd tea.Cmd) treeListingMsg {
	t.Helper()
	for _, msg := range collect(cmd) {
		if l, ok := msg.(treeListingMsg); ok {
			return l
		}
	}
	t.Fatal("the command sent no tree listing")
	return treeListingMsg{}
}

// rowNames returns the names of the rows of tree, in display order.
func treeRowNames(tree *explorer.Tree) []string {
	var names []string
	for _, r := range tree.Rows() {
		names = append(names, r.Name)
	}
	return names
}

// TestWatchChangeReadsTheTreeOffTheUpdateLoop checks that a watch change does not
// read the directories inside Update. The tree shows the new entry only after the
// command's listing is applied.
func TestWatchChangeReadsTheTreeOffTheUpdateLoop(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	must(t, os.WriteFile(filepath.Join(dir, "a.txt"), nil, 0o644))
	m, tree := newRealTreeModel(t, dir)
	must(t, os.WriteFile(filepath.Join(dir, "b.txt"), nil, 0o644))

	_, cmd := m.Update(watchMsg{})
	if got := treeRowNames(tree); slices.Contains(got, "b.txt") {
		t.Fatalf("rows after the watch change = %q, want the tree untouched until the listing", got)
	}
	m.Update(listingFrom(t, cmd))
	if got := treeRowNames(tree); !slices.Contains(got, "b.txt") {
		t.Fatalf("rows after the listing = %q, want b.txt", got)
	}
}

// TestWatchBurstWaitsForOneMoreRead checks that a change during a read does not
// start a second read at once. When the first listing arrives, one more read starts,
// so the tree ends with the latest disk state and never with an older one.
func TestWatchBurstWaitsForOneMoreRead(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	must(t, os.WriteFile(filepath.Join(dir, "a.txt"), nil, 0o644))
	m, tree := newRealTreeModel(t, dir)

	must(t, os.WriteFile(filepath.Join(dir, "b.txt"), nil, 0o644))
	_, first := m.Update(watchMsg{})
	firstListing := listingFrom(t, first)

	must(t, os.WriteFile(filepath.Join(dir, "c.txt"), nil, 0o644))
	_, second := m.Update(watchMsg{})
	for _, msg := range collect(second) {
		if _, ok := msg.(treeListingMsg); ok {
			t.Fatal("a change during a read started a second read at once")
		}
	}

	_, next := m.Update(firstListing)
	again := listingFrom(t, next)
	m.Update(again)
	if got := treeRowNames(tree); !slices.Contains(got, "c.txt") {
		t.Fatalf("rows = %q, want the latest state with c.txt", got)
	}
}

// TestTreeListingForAReplacedTreeIsDropped checks that a listing read for a tree
// that is no longer the explorer's is not applied to either tree.
func TestTreeListingForAReplacedTreeIsDropped(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	must(t, os.WriteFile(filepath.Join(dir, "a.txt"), nil, 0o644))
	m, oldTree := newRealTreeModel(t, dir)
	// The replacement is opened before the change, so it can only show b.txt if an
	// old listing was applied to it.
	newTree, err := explorer.Open(dir)
	must(t, err)
	must(t, os.WriteFile(filepath.Join(dir, "b.txt"), nil, 0o644))
	_, cmd := m.Update(watchMsg{})
	listing := listingFrom(t, cmd)

	m.tree = newTree
	m.Update(listing)
	if got := treeRowNames(newTree); slices.Contains(got, "b.txt") {
		t.Fatalf("the replacement tree shows %q after an old listing", got)
	}
	if got := treeRowNames(oldTree); !slices.Contains(got, "a.txt") || slices.Contains(got, "b.txt") {
		t.Fatalf("the replaced tree changed to %q", got)
	}
}

// TestReloadMessageReadsTheTreeOffTheUpdateLoop checks that the explorer's reload does
// not read the directories inside Update. The new entry shows once the command's
// listing is applied.
func TestReloadMessageReadsTheTreeOffTheUpdateLoop(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	must(t, os.WriteFile(filepath.Join(dir, "a.txt"), nil, 0o644))
	m, tree := newRealTreeModel(t, dir)
	must(t, os.WriteFile(filepath.Join(dir, "b.txt"), nil, 0o644))

	_, cmd := m.Update(expanel.ReloadMsg{})
	if got := treeRowNames(tree); slices.Contains(got, "b.txt") {
		t.Fatalf("rows after the reload message = %q, want the tree untouched until the listing", got)
	}
	m.Update(listingFrom(t, cmd))
	if got := treeRowNames(tree); !slices.Contains(got, "b.txt") {
		t.Fatalf("rows after the listing = %q, want b.txt", got)
	}
}

// TestSaveAsReadsTheTreeOffTheUpdateLoop checks that Save As does not read the
// directories inside Update. The saved file shows once the command's listing is applied.
func TestSaveAsReadsTheTreeOffTheUpdateLoop(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	must(t, os.WriteFile(filepath.Join(dir, "a.txt"), nil, 0o644))
	m, tree := newRealTreeModel(t, dir)
	saved := filepath.Join(dir, "b.txt")
	must(t, os.WriteFile(saved, nil, 0o644))

	cmd := m.saveAs(saved, false, flow{})
	if got := treeRowNames(tree); slices.Contains(got, "b.txt") {
		t.Fatalf("rows after Save As = %q, want the tree untouched until the listing", got)
	}
	m.Update(listingFrom(t, cmd))
	if got := treeRowNames(tree); !slices.Contains(got, "b.txt") {
		t.Fatalf("rows after the listing = %q, want b.txt", got)
	}
}

// TestFileOperationRevealsTheNewEntryAfterItsListing checks that a file operation
// reloads the tree off the update loop and reveals the new entry once its listing is
// applied. The listing is what adds the entry, so a reveal before it finds nothing.
func TestFileOperationRevealsTheNewEntryAfterItsListing(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	must(t, os.WriteFile(filepath.Join(dir, "a.txt"), nil, 0o644))
	m, tree := newRealTreeModel(t, dir)
	created := filepath.Join(dir, "b.txt")
	must(t, os.WriteFile(created, nil, 0o644))

	_, cmd := m.Update(fileResultMsg{job: fileJob{kind: jobCreate, dir: dir, name: "b.txt"}, out: created})
	if got := treeRowNames(tree); slices.Contains(got, "b.txt") {
		t.Fatalf("rows after the file operation = %q, want the tree untouched until the listing", got)
	}
	if m.notice != "" {
		t.Fatalf("notice before the listing = %q, want none: the reveal waits for the listing", m.notice)
	}
	m.Update(listingFrom(t, cmd))
	if got := treeRowNames(tree); !slices.Contains(got, "b.txt") {
		t.Fatalf("rows after the listing = %q, want b.txt", got)
	}
	if row, ok := tree.Selected(); !ok || row.Name != "b.txt" {
		t.Fatalf("selected = %+v, want the new entry b.txt", row)
	}
	if m.notice != "" {
		t.Fatalf("notice after the reveal = %q, want none", m.notice)
	}
}

// TestFileOperationRevealWaitsForTheLatestListing checks a file operation that lands
// while a read is running. The running read was taken before the new entry existed, so
// the reveal waits for the read that follows it.
func TestFileOperationRevealWaitsForTheLatestListing(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	must(t, os.WriteFile(filepath.Join(dir, "a.txt"), nil, 0o644))
	m, tree := newRealTreeModel(t, dir)

	_, watchCmd := m.Update(watchMsg{})
	stale := listingFrom(t, watchCmd)
	must(t, os.WriteFile(filepath.Join(dir, "b.txt"), nil, 0o644))
	_, opCmd := m.Update(fileResultMsg{job: fileJob{kind: jobCreate, dir: dir, name: "b.txt"}, out: filepath.Join(dir, "b.txt")})
	if len(collect(opCmd)) != 0 {
		t.Fatal("a file operation during a read started a second read at once")
	}

	_, next := m.Update(stale)
	if row, ok := tree.Selected(); ok && row.Name == "b.txt" {
		t.Fatal("the reveal ran on the stale listing")
	}
	m.Update(listingFrom(t, next))
	if row, ok := tree.Selected(); !ok || row.Name != "b.txt" {
		t.Fatalf("selected = %+v after the latest listing, want b.txt", row)
	}
}
