package app

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"sync"
	"time"

	"github.com/tbxark/gvim/authority"
	"github.com/tbxark/gvim/document"
	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/plugin/hostos"
)

// Remote file routing (docs/authority.md): the editor gets a document file access
// backed by the SSH authority so a remote file is read and saved only on the remote
// host. The access is installed before the remote file is opened; the open itself runs
// in the root's first command (tui.Deps.StartupOpen), never on the update loop or in
// startup, and a failed or disconnected open stays retryable (the root retries after a
// reconnect and a failure never becomes an empty buffer).

// remoteOpTimeout bounds one file operation over the SSH carrier. The authority already
// kills the remote helper when the context ends, so a hung host cannot wedge a save or a
// load forever.
const remoteOpTimeout = 30 * time.Second

// setupRemoteEditing installs the authority-backed file access on ed and returns the
// remote path the root should open at startup ("" when there is nothing to open). It runs
// no remote I/O: every call the access makes is made later, inside a command.
func setupRemoteEditing(ed *editor.Editor, slot authority.Slot, spec *authority.RemoteSpec) string {
	if ed == nil || slot == nil || spec == nil {
		return ""
	}
	ed.SetFileAccess(newRemoteFileAccess(slot))
	// The path is used exactly as the spec gave it: a remote file name may legally begin
	// or end with spaces, and trimming it would open (or later save) a different file.
	path := spec.Path
	if path == "" {
		return ""
	}
	return path
}

// remoteEditingNotice is the startup status note for a remote launch: it says exactly
// what works and what does not, and never claims local files were touched.
func remoteEditingNotice(spec *authority.RemoteSpec, startupOpen string) string {
	if spec == nil {
		return ""
	}
	label := "SSH:" + spec.Display()
	if startupOpen != "" {
		return "Remote editing: opening " + startupOpen + " over " + label +
			"; saves, save as, reload and disk checks go to the remote host, language " +
			"servers run on the remote host too (restarted after a reconnect), and file " +
			"changes on the remote host are watched. Remote explorer and crash recovery are " +
			"not available yet; local files are unchanged"
	}
	return "Remote workspace " + label + " at " + spec.Workspace() +
		": opened the SSH terminal; language servers run on the remote host, and file " +
		"changes on the remote host are watched. Remote file browsing is not available " +
		"yet. No local files are opened or changed"
}

// remoteFileAccess implements document.FileAccess over an authority slot. It never falls
// back to the local filesystem: every read, stat and write is a remote operation.
type remoteFileAccess struct {
	slot    authority.Slot
	timeout time.Duration

	mu       sync.Mutex
	home     string
	homeDone bool
}

func newRemoteFileAccess(slot authority.Slot) *remoteFileAccess {
	return &remoteFileAccess{slot: slot, timeout: remoteOpTimeout}
}

// opCtx returns the bounded context of one remote operation.
func (r *remoteFileAccess) opCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), r.timeout)
}

// expand resolves a leading ~ through the remote login shell's $HOME (cached after the
// first success). Another user's ~user is not expanded: the helper command quotes its
// arguments, so the remote shell would not expand it either, and guessing is worse.
func (r *remoteFileAccess) expand(path string) (string, error) {
	if !strings.HasPrefix(path, "~") {
		return path, nil
	}
	if path != "~" && !strings.HasPrefix(path, "~/") {
		return "", fmt.Errorf("remote path %q: expanding another user's home (~user) is not supported yet", path)
	}
	home, err := r.remoteHome()
	if err != nil {
		return "", err
	}
	if path == "~" {
		return home, nil
	}
	return home + path[1:], nil
}

func (r *remoteFileAccess) remoteHome() (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.homeDone {
		return r.home, nil
	}
	ctx, cancel := r.opCtx()
	defer cancel()
	res, err := r.slot.OneShot().Run(ctx, []string{"sh", "-c", `printf %s "$HOME"`}, hostos.ProcOptions{})
	if err != nil {
		return "", fmt.Errorf("remote home: %w", err)
	}
	home := strings.TrimSpace(string(res.Stdout))
	if res.Code != 0 || home == "" {
		return "", errors.New("remote home: the remote shell gave no $HOME")
	}
	r.home, r.homeDone = home, true
	return home, nil
}

// ReadFile implements document.FileAccess.
func (r *remoteFileAccess) ReadFile(path string, max int64) ([]byte, error) {
	p, err := r.expand(path)
	if err != nil {
		return nil, err
	}
	ctx, cancel := r.opCtx()
	defer cancel()
	return r.slot.Files().ReadFile(ctx, p, max)
}

// Stat implements document.FileAccess. A missing path is the zero answer (as the local
// disk state answers a failed stat); a broken symlink or another non-regular kind is
// marked Special so the document refuses to open it instead of creating an empty buffer.
func (r *remoteFileAccess) Stat(path string) (document.AccessStat, error) {
	p, err := r.expand(path)
	if err != nil {
		return document.AccessStat{}, err
	}
	ctx, cancel := r.opCtx()
	defer cancel()
	st, err := r.slot.Files().Stat(ctx, p)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return document.AccessStat{}, nil
		}
		return document.AccessStat{}, err
	}
	special := st.Kind != "file" && st.Kind != "dir"
	return document.AccessStat{
		Exists: true, Special: special, Dir: st.Kind == "dir",
		Size: st.Size, Mtime: st.MTime,
	}, nil
}

// WriteFileAtomic implements document.FileAccess: the remote atomic write (temp file in
// the same directory, then a rename), so a save is never a half-written remote file.
func (r *remoteFileAccess) WriteFileAtomic(path string, data []byte) error {
	p, err := r.expand(path)
	if err != nil {
		return err
	}
	ctx, cancel := r.opCtx()
	defer cancel()
	return r.slot.Files().WriteFile(ctx, p, data, hostos.WriteOptions{Atomic: true})
}
