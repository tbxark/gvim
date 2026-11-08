package app

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/tbxark/gvim/explorer"
	"github.com/tbxark/gvim/finder"
	"github.com/tbxark/gvim/internal/filewatch"
	"github.com/tbxark/gvim/internal/tui"
	"github.com/tbxark/gvim/workspace"
)

// projectOpener is the root's Projects port. Opening a folder records it in the
// recent list, builds its explorer tree, starts its file index and moves the
// watcher to it. The index of the folder that was left is stopped. The opener holds
// the current index, so close can stop it when the program ends.
type projectOpener struct {
	ctx     context.Context
	watcher *filewatch.Watcher // nil when the watcher could not start

	mu    sync.Mutex
	root  string        // the folder the watcher watches
	index *finder.Index // the file index of the current root

	// current is the root of the latest Open, or of the first root. It is read without
	// the lock: the plugins ask for the root from any goroutine, and Open can take a while.
	current atomic.Pointer[string]
}

// Open makes path the project root. A failure to record the folder in the recent
// list does not stop the open. A folder that cannot be opened is an error, and
// nothing is changed.
func (p *projectOpener) Open(path string) (tui.Project, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	r, err := workspace.Switch(path)
	if r.Path() == "" {
		if err == nil {
			err = fmt.Errorf("open workspace %s: no root", path)
		}
		return tui.Project{}, err
	}
	tree, err := explorer.Open(r.Path())
	if err != nil {
		return tui.Project{}, err
	}
	idx := finder.Start(p.ctx, r.Path())
	if p.watcher != nil {
		if p.root != "" {
			_ = p.watcher.Remove(p.root)
		}
		_ = p.watcher.Add(r.Path())
	}
	p.root = r.Path()
	root := r.Path()
	p.current.Store(&root)
	old := p.index
	p.index = idx
	if old != nil {
		old.Close()
	}
	return tui.Project{Tree: tree, Files: idx, Recents: recentFolders()}, nil
}

// newProjectOpener returns the opener of the first root. Its index is files, and
// when there is a watcher, the watcher is pointed at root.
func newProjectOpener(ctx context.Context, watcher *filewatch.Watcher, root string, files *finder.Index) *projectOpener {
	p := &projectOpener{ctx: ctx, watcher: watcher, index: files}
	p.current.Store(&root)
	if watcher != nil {
		_ = watcher.Add(root)
		p.root = root
	}
	return p
}

// rootDir returns the folder of the current project root.
func (p *projectOpener) rootDir() string {
	if r := p.current.Load(); r != nil {
		return *r
	}
	return ""
}

// close stops the file index of the current root.
func (p *projectOpener) close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.index != nil {
		p.index.Close()
	}
}
