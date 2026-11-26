package uiadapter

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

// Projects is the root's Projects port. Opening a folder records it in the
// recent list, builds its explorer tree, starts its file index and moves the
// watcher to it. The index of the folder that was left is stopped. The opener holds
// the current index, so close can stop it when the program ends.
type Projects struct {
	ctx     context.Context
	watcher *filewatch.Watcher // nil when the watcher could not start

	mu    sync.Mutex
	root  string        // the folder the watcher watches
	index *finder.Index // the file index of the current root

	// explorerOpts and finderOpts give the options of the session as they are now, so
	// the tree and index of an opened folder keep the ignore and scan settings of the
	// config. Nil leaves the defaults.
	explorerOpts func() explorer.Options
	finderOpts   func() finder.Options

	// current is the root of the latest Open, or of the first root. It is read without
	// the lock: the plugins ask for the root from any goroutine, and Open can take a while.
	current atomic.Pointer[string]
}

// Open makes path the project root. A failure to record the folder in the recent
// list does not stop the open. A folder that cannot be opened is an error, and
// nothing is changed.
func (p *Projects) Open(path string) (tui.Project, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	r, err := workspace.Switch(path)
	if r.Path() == "" {
		if err == nil {
			err = fmt.Errorf("open workspace %s: no root", path)
		}
		return tui.Project{}, err
	}
	var expOpts explorer.Options
	if p.explorerOpts != nil {
		expOpts = p.explorerOpts()
	}
	tree, err := explorer.OpenWithOptions(r.Path(), expOpts)
	if err != nil {
		return tui.Project{}, err
	}
	var findOpts finder.Options
	if p.finderOpts != nil {
		findOpts = p.finderOpts()
	}
	idx := finder.Start(p.ctx, r.Path(), findOpts)
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

// NewProjects returns the opener of the first root. Its index is files, and
// when there is a watcher, the watcher is pointed at root.
func NewProjects(ctx context.Context, watcher *filewatch.Watcher, root string, files *finder.Index) *Projects {
	p := &Projects{ctx: ctx, watcher: watcher, index: files}
	p.current.Store(&root)
	if watcher != nil {
		_ = watcher.Add(root)
		p.root = root
	}
	return p
}

// SetOptions installs the providers of the live explorer and finder options that Open
// gives the tree and the index of the folder it opens. Either may be nil.
func (p *Projects) SetOptions(explorerOpts func() explorer.Options, finderOpts func() finder.Options) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.explorerOpts, p.finderOpts = explorerOpts, finderOpts
}

// RootDir returns the folder of the current project root.
func (p *Projects) RootDir() string {
	if r := p.current.Load(); r != nil {
		return *r
	}
	return ""
}

// Close stops the file index of the current root.
func (p *Projects) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.index != nil {
		p.index.Close()
	}
}

// Index is the file index of the current root. It may be nil when the session started
// without one, as a test does.
func (p *Projects) Index() *finder.Index {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.index
}
