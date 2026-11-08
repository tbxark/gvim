package app

// Remote explorer write operations (task-124): New File, New Directory, Rename and Delete
// over the workspace authority. The adapter satisfies the root's explorerOps port, but every
// disk change goes through authority.Files()/authority.Mutations(fs) on the remote host:
// nothing here touches the local file system, and there is no local fallback. The
// clipboard operations (move/copy of entries) are not wired for a remote workspace: they
// return a clear error and the explorer's cut/paste actions are refused before they reach
// the adapter (subtree transfer through the authority is a recorded gap).
//
// Safety: the workspace trust gate runs before every mutation, a disconnected authority is
// the explicit "activate the window to reconnect" error, names are validated with the same
// fsops rules as the local adapter and must stay inside the remote workspace root, a create
// or rename never overwrites an existing entry (checked before the mutation; a race between
// the check and the write is a recorded residual because FileMutations has no exclusive
// create), and a remote delete is permanent: Mutations.Remove, no trash.

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"time"

	"github.com/tbxark/gvim/authority"
	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/fsops"
	expanel "github.com/tbxark/gvim/internal/tui/explorer"
	"github.com/tbxark/gvim/plugin/hostos"
)

// remoteExplorerOpTimeout bounds one remote mutation.
const remoteExplorerOpTimeout = 20 * time.Second

// errRemoteExplorerDisconnected is the precise notice of an operation attempted while the
// remote authority is down. The next reconnect keeps the same workspace, so the operation
// can simply be retried once the connection is back.
var errRemoteExplorerDisconnected = errors.New("remote workspace is disconnected; activate the window to reconnect")

// remoteExplorerOps implements the TUI's explorerOps over the remote authority.
type remoteExplorerOps struct {
	slot  authority.Slot
	files authority.FileSystem
	muts  authority.FileMutations
	gate  func(ctx context.Context, argv []string, cwd string) error
	root  string
	label string
	ed    *editor.Editor
}

// newRemoteExplorerOps returns the write adapter of the remote workspace root.
func newRemoteExplorerOps(slot authority.Slot, root, label string, gate func(ctx context.Context, argv []string, cwd string) error, ed *editor.Editor) *remoteExplorerOps {
	o := &remoteExplorerOps{slot: slot, root: root, label: label, gate: gate, ed: ed}
	if slot != nil {
		o.files = slot.Files()
		o.muts = authority.Mutations(o.files)
	}
	return o
}

// RemoteTarget names the backend in the explorer's confirmations.
func (o *remoteExplorerOps) RemoteTarget() string {
	if o == nil {
		return ""
	}
	if o.root == "" {
		return o.label
	}
	return o.label + ":" + o.root
}

// opContext returns the bounded context of one mutation.
func (o *remoteExplorerOps) opContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), remoteExplorerOpTimeout)
}

// check refuses the operation while the authority is down or the backend cannot mutate.
func (o *remoteExplorerOps) check() error {
	if o == nil || o.slot == nil || !o.slot.Connected() {
		return errRemoteExplorerDisconnected
	}
	if o.files == nil {
		return errors.New("remote file operations: the workspace authority mounts no file system")
	}
	if o.muts == nil {
		return errors.New("remote file operations: the backend does not support changing the tree")
	}
	return nil
}

// gateOp runs the workspace trust gate before a mutation.
func (o *remoteExplorerOps) gateOp(ctx context.Context, op, p string) error {
	if o.gate == nil {
		return nil
	}
	if err := o.gate(ctx, []string{"gvim-remote-explorer", op, p}, o.root); err != nil {
		return fmt.Errorf("remote file operation: %w", err)
	}
	return nil
}

// inside reports whether p is the remote root or under it.
func (o *remoteExplorerOps) inside(p string) bool {
	clean, root := path.Clean(p), path.Clean(o.root)
	return clean == root || len(clean) > len(root)+1 && clean[:len(root)+1] == root+"/"
}

// child builds and validates dir/name as one entry of the remote workspace.
func (o *remoteExplorerOps) child(dir, name string) (string, error) {
	if !o.inside(dir) {
		return "", fmt.Errorf("remote file operation: %s is outside the remote workspace", dir)
	}
	if err := fsops.Validate(dir, name); err != nil {
		return "", err
	}
	p := path.Join(dir, name)
	if !o.inside(p) {
		return "", fmt.Errorf("remote file operation: %s is outside the remote workspace", p)
	}
	return p, nil
}

// exists reports whether p exists remotely.
func (o *remoteExplorerOps) exists(ctx context.Context, p string) (bool, hostos.FileStat, error) {
	st, err := o.files.Stat(ctx, p)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return false, hostos.FileStat{}, nil
		}
		return false, hostos.FileStat{}, err
	}
	return true, st, nil
}

// CreateFile creates an empty remote file and returns its path.
func (o *remoteExplorerOps) CreateFile(ctx context.Context, dir, name string) (string, error) {
	if err := o.check(); err != nil {
		return "", err
	}
	p, err := o.child(dir, name)
	if err != nil {
		return "", err
	}
	opCtx, cancel := o.opContext()
	defer cancel()
	if err := o.gateOp(opCtx, "create", p); err != nil {
		return "", err
	}
	if ok, _, err := o.exists(opCtx, p); err != nil {
		return "", fmt.Errorf("remote file operation: stat %s: %w", p, err)
	} else if ok {
		return "", fmt.Errorf("remote file operation: %s already exists", p)
	}
	if err := o.files.WriteFile(opCtx, p, []byte{}, hostos.WriteOptions{Atomic: true}); err != nil {
		return "", fmt.Errorf("remote file operation: create %s: %w", p, err)
	}
	return p, nil
}

// Mkdir creates one remote directory and returns its path.
func (o *remoteExplorerOps) Mkdir(ctx context.Context, dir, name string) (string, error) {
	if err := o.check(); err != nil {
		return "", err
	}
	p, err := o.child(dir, name)
	if err != nil {
		return "", err
	}
	opCtx, cancel := o.opContext()
	defer cancel()
	if err := o.gateOp(opCtx, "mkdir", p); err != nil {
		return "", err
	}
	if ok, _, err := o.exists(opCtx, p); err != nil {
		return "", fmt.Errorf("remote file operation: stat %s: %w", p, err)
	} else if ok {
		return "", fmt.Errorf("remote file operation: %s already exists", p)
	}
	if err := o.muts.Mkdir(opCtx, p, false); err != nil {
		return "", fmt.Errorf("remote file operation: mkdir %s: %w", p, err)
	}
	return p, nil
}

// Rename renames one remote entry inside its directory and returns the new path. A target
// that already exists is refused (no overwrite), matching the local explorer.
func (o *remoteExplorerOps) Rename(ctx context.Context, p, newName string) (string, error) {
	if err := o.check(); err != nil {
		return "", err
	}
	if !o.inside(p) {
		return "", fmt.Errorf("remote file operation: %s is outside the remote workspace", p)
	}
	dir := path.Dir(p)
	np, err := o.child(dir, newName)
	if err != nil {
		return "", err
	}
	if np == p {
		return p, nil
	}
	opCtx, cancel := o.opContext()
	defer cancel()
	if err := o.gateOp(opCtx, "rename", p); err != nil {
		return "", err
	}
	if ok, _, err := o.exists(opCtx, np); err != nil {
		return "", fmt.Errorf("remote file operation: stat %s: %w", np, err)
	} else if ok {
		return "", fmt.Errorf("remote file operation: %s already exists", np)
	}
	if err := o.muts.Rename(opCtx, p, np); err != nil {
		return "", fmt.Errorf("remote file operation: rename %s: %w", p, err)
	}
	return np, nil
}

// Trash deletes one remote entry permanently (there is no remote trash): a directory is
// removed recursively after the confirmation. It returns no path.
func (o *remoteExplorerOps) Trash(ctx context.Context, p string) error {
	if err := o.check(); err != nil {
		return err
	}
	if !o.inside(p) {
		return fmt.Errorf("remote file operation: %s is outside the remote workspace", p)
	}
	opCtx, cancel := o.opContext()
	defer cancel()
	if err := o.gateOp(opCtx, "delete", p); err != nil {
		return err
	}
	ok, st, err := o.exists(opCtx, p)
	if err != nil {
		return fmt.Errorf("remote file operation: stat %s: %w", p, err)
	}
	if !ok {
		return fmt.Errorf("remote file operation: %s does not exist", p)
	}
	if err := o.muts.Remove(opCtx, p, st.Kind == "dir"); err != nil {
		return fmt.Errorf("remote file operation: delete %s: %w", p, err)
	}
	return nil
}

// Move and Copy are not wired remotely: subtree transfer through the authority is a
// recorded gap, and the explorer refuses cut/paste before calling them.
func (o *remoteExplorerOps) Move(context.Context, string, string) (string, error) {
	return "", errors.New("moving remote entries is not available yet")
}

func (o *remoteExplorerOps) Copy(context.Context, string, string, expanel.Conflict) (string, error) {
	return "", errors.New("copying remote entries is not available yet")
}

// CheckRename is the editor's dry run for a rename: it reads the open tabs and changes
// nothing. The root calls it from Update before the rename touches the remote host.
func (o *remoteExplorerOps) CheckRename(oldPath, newPath string) error {
	if o == nil || o.ed == nil {
		return nil
	}
	return o.ed.CheckRename(oldPath, newPath)
}

// FollowRename moves the open tabs of oldPath to newPath after a successful rename.
func (o *remoteExplorerOps) FollowRename(oldPath, newPath string) error {
	if o == nil || o.ed == nil {
		return nil
	}
	return o.ed.RenamePath(oldPath, newPath)
}

// CloseDeleted closes the open tabs of a deleted remote path; a tab with unsaved edits
// stays open and the error says so.
func (o *remoteExplorerOps) CloseDeleted(p string) error {
	if o == nil || o.ed == nil {
		return nil
	}
	return o.ed.CloseByPath(p, false)
}
