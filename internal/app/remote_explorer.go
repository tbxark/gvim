package app

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/tbxark/gvim/authority"
	"github.com/tbxark/gvim/config"
	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/internal/tui"
	"github.com/tbxark/gvim/plugin/hostos"
	"github.com/tbxark/gvim/pluginapi"
)

// remoteExplorerTimeout bounds one directory listing of the remote explorer. The
// authority bounds its helper command too; this is the outer deadline of one entry in
// a batch.
const remoteExplorerTimeout = 20 * time.Second

// remoteExplorerMaxEntries is the clear-error bound of one remote directory, matching
// the authority's own listing cap. A directory over it is an error, never a silently
// truncated tree.
const remoteExplorerMaxEntries = 5000

// authorityDirLoader implements tui.RemoteDirLoader over the workspace authority: the
// read-only remote explorer lists its directories through authority.Files().ReadDir.
// It never reads the local filesystem. A listing is trust-gated by gate (the workspace
// spawn gate: a remote launch allows commands only on a trusted workspace) and refused
// with tui.ErrRemoteExplorerDisconnected while the authority is disconnected; the TUI
// reloads the tree after a reconnect.
type authorityDirLoader struct {
	slot authority.Slot
	gate func(ctx context.Context, argv []string, cwd string) error
	root string

	// watcher receives each listed directory on the authority-backed remote directory
	// watch bridge (the existing polled Watch fed by remoteWatch); nil means no directory
	// watching. The support probe runs once so an unsupported backend gets one notice
	// instead of repeated silent retries; watching then degrades to manual refresh.
	mu                   sync.Mutex
	watcher              interface{ Add(string) error }
	watchProbed          bool
	watchUnsupported     bool
	notice               string
	respectGitignore     *bool
	customIgnorePatterns []string
}

// newAuthorityDirLoader returns the loader of remoteRoot over slot. A nil gate skips
// the trust check (tests only); the app passes the workspace authority gate.
func newAuthorityDirLoader(slot authority.Slot, gate func(ctx context.Context, argv []string, cwd string) error, remoteRoot string) *authorityDirLoader {
	return &authorityDirLoader{slot: slot, gate: gate, root: remoteRoot}
}

// SetIgnoreOptions updates the .gitignore toggle and custom wildcard ignore patterns
// applied on subsequent remote directory listings.
func (l *authorityDirLoader) SetIgnoreOptions(respectGitignore *bool, customPatterns []string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if respectGitignore != nil {
		v := *respectGitignore
		l.respectGitignore = &v
	} else {
		l.respectGitignore = nil
	}
	l.customIgnorePatterns = slices.Clone(customPatterns)
}

// installRemoteExplorer wires the remote explorer tree and its file-operation adapter
// into deps and returns the loader, so the caller can attach the remote directory watch
// once that bridge exists.
func installRemoteExplorer(deps *tui.Deps, slot authority.Slot, root string, gate func(ctx context.Context, argv []string, cwd string) error, ed *editor.Editor, cfg config.Config, showExplorer bool) *authorityDirLoader {
	loader := newAuthorityDirLoader(slot, gate, root)
	loader.SetIgnoreOptions(cfg.FileExplorer.RespectGitignore, cfg.FileExplorer.CustomIgnorePatterns)
	tree := tui.NewRemoteExplorerTree(root, loader)
	tree.SetShowHidden(cfg.ExplorerShowHidden)
	deps.Explorer = tree
	deps.ShowExplorer = showExplorer
	deps.Ops = newRemoteExplorerOps(slot, root, slot.Label(), gate, ed)
	return loader
}

// setWatcher attaches the remote directory watch bridge. The tree attaches it after the
// bridge is constructed; Add is idempotent per directory.
func (l *authorityDirLoader) setWatcher(w interface{ Add(string) error }) {
	l.mu.Lock()
	l.watcher = w
	l.mu.Unlock()
}

// TakeNotice returns and clears the one-shot load/watch degradation notice.
func (l *authorityDirLoader) TakeNotice() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	notice := l.notice
	l.notice = ""
	return notice
}

// addNotice records the first degradation notice of this load.
func (l *authorityDirLoader) addNotice(text string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.notice == "" {
		l.notice = text
	}
}

// probeWatch checks once whether the backend supports directory watching. A refused
// watch degrades to manual refresh with one notice; a transient failure while
// disconnected is left to the normal disconnected error.
func (l *authorityDirLoader) probeWatch(ctx context.Context, files authority.FileSystem) bool {
	l.mu.Lock()
	if l.watchProbed {
		unsupported := l.watchUnsupported
		l.mu.Unlock()
		return !unsupported
	}
	l.mu.Unlock()
	if l.slot == nil || !l.slot.Connected() || files == nil {
		return false
	}
	probeCtx, cancel := context.WithTimeout(ctx, remoteExplorerTimeout)
	watch, err := files.Watch(probeCtx, l.root, hostos.WatchOptions{Debounce: 150 * time.Millisecond})
	cancel()
	if err != nil {
		if errors.Is(err, authority.ErrUnsupportedFileIO) || errors.Is(err, errors.ErrUnsupported) || pluginapi.CodeOf(err) == pluginapi.CodeUnsupported {
			l.mu.Lock()
			l.watchProbed = true
			l.watchUnsupported = true
			l.mu.Unlock()
			l.addNotice("remote directory watching is not supported by this backend; refresh manually (Ctrl+R)")
		}
		return false
	}
	if watch != nil {
		watch.Stop()
	}
	l.mu.Lock()
	l.watchProbed = true
	l.watchUnsupported = false
	l.mu.Unlock()
	return true
}

// ReadDirs implements tui.RemoteDirLoader. Directories that cannot be listed are left
// out and their errors are joined; a dropped carrier before or during the batch is the
// explicit disconnected error, so the root can tell the user and reload on reconnect.
func (l *authorityDirLoader) ReadDirs(ctx context.Context, dirs []string) (map[string][]tui.RemoteDirEntry, error) {
	if l.slot == nil || !l.slot.Connected() {
		return nil, tui.ErrRemoteExplorerDisconnected
	}
	if l.gate != nil {
		probe := []string{"gvim-remote-explorer", "--list", l.root}
		if err := l.gate(ctx, probe, l.root); err != nil {
			return nil, fmt.Errorf("remote file explorer: %w", err)
		}
	}
	files := l.slot.Files()
	if files == nil {
		return nil, tui.ErrRemoteExplorerDisconnected
	}
	out := make(map[string][]tui.RemoteDirEntry, len(dirs))
	var errs []error
	l.mu.Lock()
	watcher := l.watcher
	respectGitignore := l.respectGitignore == nil || *l.respectGitignore
	customPatterns := slices.Clone(l.customIgnorePatterns)
	l.mu.Unlock()
	ignore := newIgnoreBatchWithOptions(func() authority.FileSystem { return l.slot.Files() }, l.root, respectGitignore, customPatterns)
	watching := watcher != nil && l.probeWatch(ctx, files)
	if watcher == nil {
		l.addNotice("remote directory watching is unavailable; refresh manually (Ctrl+R)")
	}
	for _, dir := range dirs {
		readCtx, cancel := context.WithTimeout(ctx, remoteExplorerTimeout)
		entries, err := files.ReadDir(readCtx, dir, true)
		cancel()
		if err != nil {
			if l.slot == nil || !l.slot.Connected() {
				return nil, tui.ErrRemoteExplorerDisconnected
			}
			errs = append(errs, fmt.Errorf("read remote directory %s: %w", dir, err))
			continue
		}
		if len(entries) > remoteExplorerMaxEntries {
			errs = append(errs, fmt.Errorf("read remote directory %s: more than %d entries; narrow the tree or use the file finder", dir, remoteExplorerMaxEntries))
			continue
		}
		converted := make([]tui.RemoteDirEntry, 0, len(entries))
		for _, e := range entries {
			if e.Name == "" || e.Name == "." || e.Name == ".." || strings.Contains(e.Name, "/") {
				continue
			}
			isDir := e.Kind == "dir" || (e.Kind == "symlink" && e.TargetKind == "dir")
			converted = append(converted, tui.RemoteDirEntry{
				Name:    e.Name,
				IsDir:   isDir,
				Hidden:  strings.HasPrefix(e.Name, "."),
				Ignored: ignore.ignored(ctx, dir, e.Name, isDir),
			})
		}
		out[dir] = converted
		if watching && watcher != nil {
			_ = watcher.Add(dir)
		}
	}
	if notice := ignore.Notice(); notice != "" {
		l.addNotice(notice)
	}
	if len(out) == 0 && len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return out, errors.Join(errs...)
}
