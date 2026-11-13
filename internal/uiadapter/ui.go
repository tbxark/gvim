package uiadapter

import (
	"time"

	commands "github.com/tbxark/gvim/commands"
	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/explorer"
	"github.com/tbxark/gvim/finder"
	"github.com/tbxark/gvim/internal/highlight"
	"github.com/tbxark/gvim/internal/keybindings"
	"github.com/tbxark/gvim/internal/tui"
	"github.com/tbxark/gvim/keymap"
	"github.com/tbxark/gvim/theme"
	"github.com/tbxark/gvim/workspace"
)

// NewDeps wires the domain packages into the ports of the UI. The command
// table is built for the active keymap, so the palette shows its shortcuts. The
// file ranker is always wired. A nil tree leaves the explorer out of the model,
// and a nil files index leaves the palette's file mode on its loading row. The
// optional ports (LSP, plugins, terminal and the rest) stay nil: internal/app sets them
// on the result. File operations follow renames and closes in ed.
func NewDeps(ed *editor.Editor, km *keymap.Map, th theme.Theme, tree *explorer.Tree, files *finder.Index) tui.Deps {
	deps := tui.Deps{
		Editor:   &Editor{ed: ed, spans: highlight.New(), others: map[*editor.Document]*highlight.Cache{}, saves: &autoSaveGate{}},
		Keys:     keybindings.NewKeymap(km),
		Theme:    th,
		Clock:    time.Now,
		Commands: commands.Commands(km),
		Ranker:   rankPort{r: newFileRanker()},
		Dialogs:  dialogPort{},
		Recents:  recentFolders(),
		Ops:      newFileOps(ed),
	}
	// Assign only a non-nil tree or index: a nil concrete pointer in the interface
	// would look like an explorer or a file list that is present.
	if tree != nil {
		deps.Explorer = tree
	}
	if files != nil {
		deps.Files = files
	}
	return deps
}

// recentFolders returns the recent folder list for the welcome screen, newest
// first. A list that cannot be read is empty.
func recentFolders() []string {
	roots, err := workspace.Recent()
	if err != nil {
		return nil
	}
	paths := make([]string, len(roots))
	for i, r := range roots {
		paths[i] = r.Path()
	}
	return paths
}

// rankPort is fileRanker as the root's FileRanker port. The root has its own
// Ranked type, so the rows are copied. Record is forwarded, so the root can
// record the files the picker opens.
type rankPort struct {
	r fileRanker
}

// Rank scores files against query and returns at most limit matches, best first.
func (p rankPort) Rank(query string, files []string, limit int) []tui.Ranked {
	rows := p.r.Rank(query, files, limit)
	out := make([]tui.Ranked, len(rows))
	for i, r := range rows {
		out[i] = tui.Ranked{Path: r.Path, Indexes: r.Indexes}
	}
	return out
}

// Record notes that the file at rel was opened from the picker.
func (p rankPort) Record(rel string) {
	p.r.Record(rel)
}
