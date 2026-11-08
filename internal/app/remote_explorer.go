package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/tbxark/gvim/authority"
	"github.com/tbxark/gvim/internal/tui"
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
}

// newAuthorityDirLoader returns the loader of remoteRoot over slot. A nil gate skips
// the trust check (tests only); the app passes the workspace authority gate.
func newAuthorityDirLoader(slot authority.Slot, gate func(ctx context.Context, argv []string, cwd string) error, remoteRoot string) *authorityDirLoader {
	return &authorityDirLoader{slot: slot, gate: gate, root: remoteRoot}
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
	out := make(map[string][]tui.RemoteDirEntry, len(dirs))
	var errs []error
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
				Name:   e.Name,
				IsDir:  isDir,
				Hidden: strings.HasPrefix(e.Name, "."),
			})
		}
		out[dir] = converted
	}
	if len(out) == 0 && len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return out, errors.Join(errs...)
}
