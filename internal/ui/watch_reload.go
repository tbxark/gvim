package ui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/explorer"
)

// treeSnapshotter is the background reload surface of the explorer tree. The
// explorer's tree has it. A tree without it reloads on the update loop, as Ctrl+R
// does. SnapshotFor takes the folders on the way to a pending reveal too.
type treeSnapshotter interface {
	SnapshotFor(path string) explorer.Snapshot
	Apply(l explorer.Listing) error
}

// treeRevealer reveals a path in a tree whose folders on the way are loaded, so the
// reveal reads nothing from disk. The explorer's tree has it. A tree without it is
// revealed with its Reveal, which may read the disk.
type treeRevealer interface {
	RevealLoaded(path string) error
}

// treeReload is the state of the background reload. A watch change that arrives while
// a read runs sets again, and one more read starts when the running one is applied.
// So the reads apply in the order they were taken, and the tree ends with the latest
// state.
type treeReload struct {
	running bool
	again   bool
	// reveal is the path a file operation shows once the listing that holds its new
	// entry is applied. It is empty when no operation waits for a listing.
	reveal string
}

// treeListingMsg is the listing that a background reload read. It names the tree it
// was taken from, and it is dropped when the explorer shows another tree by then.
type treeListingMsg struct {
	tree    ExplorerTree
	listing explorer.Listing
}

// reloadTreeAsync reloads the tree after a watch change, a Ctrl+R, a file operation or a
// Save As. The directories are read in a command, and the listing is applied when it
// arrives, so the update loop does no disk work. A tree without a snapshot reloads on
// the update loop. A change during a read is remembered, and it starts one more read
// when that read is applied.
func (m *Model) reloadTreeAsync() tea.Cmd {
	st, ok := m.tree.(treeSnapshotter)
	if !ok {
		m.reloadTree()
		m.revealPending()
		return nil
	}
	if m.root.reload.running {
		m.root.reload.again = true
		return nil
	}
	m.root.reload.running = true
	tree := m.tree
	snap := st.SnapshotFor(m.root.reload.reveal)
	return func() tea.Msg {
		return treeListingMsg{tree: tree, listing: snap.Read()}
	}
}

// treeListed applies a background listing on the update loop. A listing for a tree
// that is no longer the explorer's is dropped. A change that arrived during the read
// starts the next read. Otherwise the listing is the latest state, and a pending reveal
// is shown in it.
func (m *Model) treeListed(msg treeListingMsg) tea.Cmd {
	m.root.reload.running = false
	if st, ok := m.tree.(treeSnapshotter); ok && m.tree == msg.tree {
		if err := st.Apply(msg.listing); err != nil {
			m.notice = err.Error()
		}
	}
	if m.root.reload.again {
		m.root.reload.again = false
		return m.reloadTreeAsync()
	}
	m.revealPending()
	return nil
}

// reloadToReveal reloads the tree after a file operation and reveals path in it, when
// path is not empty. The reveal waits for the listing that holds the new entry, because
// the listing is what adds it, so the update loop does no disk read.
func (m *Model) reloadToReveal(path string) tea.Cmd {
	if path != "" {
		m.root.reload.reveal = path
	}
	return m.reloadTreeAsync()
}

// revealPending reveals the path that a file operation left for the listing, and
// forgets it. It does nothing when no path is pending.
func (m *Model) revealPending() {
	if path := m.root.reload.reveal; path != "" {
		m.root.reload.reveal = ""
		m.reveal(path)
	}
}
