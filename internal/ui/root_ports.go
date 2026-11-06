package ui

import (
	"time"

	"github.com/tbxark/gvim/internal/explorer/watch"
	"github.com/tbxark/gvim/internal/ui/palette"
)

// The ports in this file are the root features' view of the outside world: the
// project switch, the git status, the live grep, the file watcher and the
// auto-save interval. The app adapters implement them, and they do the disk work.
// The root calls them only from a tea.Cmd, except for the watcher channel, which
// the root reads through a command too.

// Project is what opening a folder as the project root gives back: the explorer
// tree and the file index of the new root, and the recent folder list, newest
// first.
type Project struct {
	Tree    ExplorerTree
	Files   FileIndex
	Recents []string
}

// Projects opens a folder as the project root. Open records the folder in the
// recent list, builds its explorer tree, starts its file index and moves the file
// watcher to it. Open does the disk work, so the root runs it in a tea.Cmd.
type Projects interface {
	Open(path string) (Project, error)
}

// WatchDirs adds a folder to the file watcher. The watcher reports the changes of the
// entries of each folder it watches, and a folder that is added twice is harmless. The
// file watcher of the watch package has it. The root calls it only from a tea.Cmd.
type WatchDirs interface {
	Add(dir string) error
}

// GitStatus reads the git status of the work tree that holds dir. It returns the
// status letter of every changed path, keyed by absolute path: A, M, D, U
// (untracked), R or C. A directory outside a repository gives a nil map and no
// error.
type GitStatus interface {
	Marks(dir string) (map[string]rune, error)
}

// RootPorts are the optional ports of the root features. A nil field disables
// its feature, and the zero value disables all of them.
type RootPorts struct {
	// Projects backs Switch Project and Open Folder. Nil shows a notice.
	Projects Projects
	// Grep searches the text of the workspace files for Live Grep. Nil shows a
	// notice when Live Grep is opened.
	Grep palette.Grepper
	// Git gives the explorer its status letters. Nil shows none.
	Git GitStatus
	// Watch delivers the changes of the tree root. Nil means no watcher, and the
	// tree reloads only on Ctrl+R.
	Watch <-chan watch.Changed
	// WatchDirs adds the folders of the open documents to the watcher, so a change
	// to a document outside the tree root still reaches the root. Nil leaves the
	// watcher to the tree root alone.
	WatchDirs WatchDirs
	// AutoSave is the interval of auto-save. Zero turns it off. The editor
	// saves on its own schedule through the same value (editor.Options).
	AutoSave time.Duration
	// Plugins lists the plugin commands in the command palette and runs the one
	// that is chosen. Nil leaves the plugin commands out.
	Plugins palette.PluginRunner
	// Stashes lists the files whose unsaved text an interrupted save kept, for
	// Review Interrupted Saves. Nil shows a notice when the review is run.
	Stashes InterruptedSaves
}
