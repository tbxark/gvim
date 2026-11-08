package tui

import (
	"time"

	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/internal/filewatch"
	"github.com/tbxark/gvim/internal/tui/palette"
	"github.com/tbxark/gvim/pluginapi"
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
	// Grep searches the text of the workspace files and open buffers for Live Grep.
	// Nil shows a notice when Live Grep is opened.
	Grep palette.Grepper
	// Preview reads the start of a file for the palette's file preview. Nil paints no
	// preview column.
	Preview palette.Previewer
	// Git gives the explorer its status letters. Nil shows none.
	Git GitStatus
	// Watch delivers the changes of the tree root. Nil means no watcher, and the
	// tree reloads only on Ctrl+R.
	Watch <-chan filewatch.Changed
	// WatchDirs adds the folders of the open documents to the watcher, so a change
	// to a document outside the tree root still reaches the root. Nil leaves the
	// watcher to the tree root alone.
	WatchDirs WatchDirs
	// AutoSave is the interval of auto-save. Zero turns it off. The editor
	// saves on its own schedule through the same value (editor.Options).
	AutoSave time.Duration
	// AutoRevertPoll is how often the open files are checked for changes the watcher
	// does not report (editor.auto_revert_poll_interval_ms). Zero turns the poll off.
	AutoRevertPoll time.Duration
	// Recovery writes the crash-recovery copy of the unsaved text every RecoveryEvery.
	// Nil or a zero interval turns it off.
	Recovery      RecoveryPort
	RecoveryEvery time.Duration
	// FullLoadPrompts are the files named at startup that are large and not UTF-8, so
	// they were not opened. The root asks about each once it runs, as it does for a file
	// opened from a menu, instead of decoding it whole before the first frame.
	FullLoadPrompts []editor.ErrLargeNeedsFullLoad
	// Keybindings reads and writes the user's custom keybindings for the keybinding
	// editor. Nil makes the editor read only.
	Keybindings KeybindingStore
	// Plugins is the plugin runtime. Nil turns plugins off: the palette lists
	// only the built-in commands.
	Plugins pluginapi.Runtime
	// PluginEditor is the editor as the plugins see it. Nil makes the buffer calls of a
	// plugin answer unsupported.
	PluginEditor PluginEditor
	// PluginKeys builds the keymap that holds the plugins' key bindings: the preset, then
	// the bindings, then the user's own. preset names the built-in keymap to start from, or
	// is "" for the one the session started with. The root uses it when the bindings
	// change, and with the new preset name when the keymap is switched, so a switch keeps
	// them. A keymap can come back with an error that names what was left out. Nil leaves
	// the keymap as it is.
	PluginKeys func(preset string, b []pluginapi.Binding) (Keymap, error)
	// Stashes lists the files whose unsaved text an interrupted save kept, for
	// Review Interrupted Saves. Nil shows a notice when the review is run.
	Stashes InterruptedSaves
}
