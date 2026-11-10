package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/tbxark/gvim/authority"
	"github.com/tbxark/gvim/internal/tui"
	"github.com/tbxark/gvim/lsp"
	"github.com/tbxark/gvim/plugin/hostos"
	"go.lsp.dev/uri"
)

// LSP over SSH (docs/authority.md §7): a remote workspace starts its language servers on
// the remote host through the authority's LongRunning surface (a separate ssh exec
// channel with piped stdio), instead of a local exec.Command. The manager still runs the
// workspace spawn gate before calling the spawner, so the coarse remote trust rule
// applies; the spawner itself never starts a local process for a remote workspace.

// remoteLSPSpawner returns the manager spawner of one remote workspace. The spawner
// overrides the config the manager built from local data: the remote shell resolves a
// bare command in the remote PATH (so Program, the locally resolved absolute path, is
// cleared), the root URI is the remote workspace path, and the local env overlay is not
// applied (the remote login environment is).
func remoteLSPSpawner(slot authority.Slot, workspace string, spawnEnv ...func() []string) func(context.Context, lsp.Config) (*lsp.Client, error) {
	return func(ctx context.Context, cfg lsp.Config) (*lsp.Client, error) {
		if slot == nil {
			return nil, errors.New("lsp: no remote authority to start the server on")
		}
		if len(cfg.Command) == 0 {
			return nil, lsp.ErrNoCommand
		}
		// The manager passes the root it was created for (a nested root_markers project
		// can differ from the workspace). Use that root for the remote cwd and RootURI so
		// several managers of the same language do not all start in the workspace root.
		root := workspace
		if cfg.RootURI != "" {
			if u := uri.URI(cfg.RootURI); u.IsFile() {
				root = u.FsPath()
			}
		}
		root, err := expandRemotePath(ctx, slot, root)
		if err != nil {
			return nil, fmt.Errorf("lsp: remote workspace %q: %w", root, err)
		}
		var envEntries []string
		if len(spawnEnv) > 0 && spawnEnv[0] != nil {
			// The remote environment's captured delta (env.Remote ran on the remote host;
			// the local overlay never enters it). The server starts in the project
			// environment, the same one the terminal and one-shot commands use.
			envEntries = spawnEnv[0]()
		}
		proc, err := slot.LongRunning().Start(ctx, cfg.Command, hostos.ProcOptions{Cwd: root, Env: remoteEnvMap(envEntries)})
		if err != nil {
			return nil, fmt.Errorf("lsp: start %s on %s: %w", cfg.Command[0], slot.Label(), err)
		}
		conn := newProcConn(proc)
		remote := cfg
		remote.Program = "" // the remote shell resolves the command in the remote PATH
		remote.RootURI = lsp.PathURI(root)
		remote.Env = nil // the remote login environment applies, not the local overlay
		client := lsp.NewClientWithErrorOutput(conn, remote, conn.Stderr())
		if cfg.Limits.Any() {
			client.SetLimitsNote("process_limits: not applied remotely (the limits would apply to the local ssh client, not to the remote host)")
		}
		return client, nil
	}
}

// expandRemotePath resolves a leading ~ through the remote login shell. "~" and "~/..."
// use $HOME; "~user" and "~user/..." use the remote shell's tilde expansion for that user
// (Fresh's remote expanduser does the same). The user name is validated before the fixed
// shell script sees it, so project data is never interpolated into shell code.
func expandRemotePath(ctx context.Context, slot authority.Slot, path string) (string, error) {
	if path == "" || !strings.HasPrefix(path, "~") {
		return path, nil
	}
	if slot == nil {
		return "", errors.New("no remote authority")
	}
	user, rest := "", path[1:]
	if i := strings.IndexByte(rest, '/'); i >= 0 {
		user, rest = rest[:i], rest[i:]
	}
	if user == "" {
		res, err := slot.OneShot().Run(ctx, []string{"sh", "-c", "printf %s \"$HOME\""}, hostos.ProcOptions{})
		if err != nil {
			return "", fmt.Errorf("remote home: %w", err)
		}
		home := strings.TrimSpace(string(res.Stdout))
		if res.Code != 0 || home == "" {
			return "", errors.New("remote home: the remote shell gave no $HOME")
		}
		return home + rest, nil
	}
	// Tilde names are not all usernames: ~- is OLDPWD in POSIX shells and ~-N is a
	// directory-stack entry in bash; ~+ is PWD/dir-stack. Never let those reach the
	// shell as a home path. A leading '-' is not accepted for a remote home name.
	if strings.HasPrefix(user, "-") {
		return "", fmt.Errorf("expanding remote home ~%s: shell-special tilde names are not user names", user)
	}
	for _, r := range user {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '.' || r == '_' || r == '-' {
			continue
		}
		return "", fmt.Errorf("expanding remote home ~%s: invalid user name", user)
	}
	const script = "case $1 in \"\"|[-+]*|*[!A-Za-z0-9._-]*) exit 2;; esac\neval \"printf %s ~$1\""
	res, err := slot.OneShot().Run(ctx, []string{"sh", "-c", script, "sh", user}, hostos.ProcOptions{})
	if err != nil {
		return "", fmt.Errorf("remote home ~%s: %w", user, err)
	}
	home := strings.TrimSpace(string(res.Stdout))
	if res.Code != 0 || home == "" || strings.HasPrefix(home, "~") {
		return "", fmt.Errorf("remote home ~%s: the remote shell could not expand it", user)
	}
	return home + rest, nil
}

// remoteLSMarkerChecker returns the authority-backed root_markers probe of a remote
// workspace. One bounded ReadDir per directory checks every marker (exact names or globs)
// without touching the local filesystem; the caller (lsphost) bounds depth/calls, times
// out and caches the answers.
func remoteLSMarkerChecker(slot authority.Slot) func(ctx context.Context, dir string, markers []string) (bool, error) {
	return func(ctx context.Context, dir string, markers []string) (bool, error) {
		if slot == nil || !slot.Connected() {
			return false, errors.New("remote workspace is disconnected")
		}
		files := slot.Files()
		if files == nil {
			return false, errors.New("remote workspace has no file system")
		}
		entries, err := files.ReadDir(ctx, dir, true)
		if err != nil {
			return false, fmt.Errorf("read remote root_markers directory %s: %w", dir, err)
		}
		for _, marker := range markers {
			for _, entry := range entries {
				if marker == entry.Name {
					return true, nil
				}
				if strings.ContainsAny(marker, "*?[") {
					if ok, _ := path.Match(marker, entry.Name); ok {
						return true, nil
					}
				}
			}
		}
		return false, nil
	}
}

// procConn adapts the authority's chunk-based long-running process to the io.ReadWriteCloser
// lsp.NewClient wants: stdout chunks become the client's reader, stdin writes are fed to
// the process through a pipe (blocking the writer until the authority accepts the bytes,
// so a JSON-RPC message is never dropped), and stderr chunks go to a separate reader for
// the client's error tail. Close terminates the remote child.
type procConn struct {
	proc hostos.Proc
	in   *io.PipeWriter
	inR  *io.PipeReader
	out  *io.PipeReader
	outW *io.PipeWriter
	errR *io.PipeReader
	errW *io.PipeWriter

	closeOnce sync.Once
}

func newProcConn(proc hostos.Proc) *procConn {
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	errR, errW := io.Pipe()
	c := &procConn{proc: proc, in: inW, inR: inR, out: outR, outW: outW, errR: errR, errW: errW}
	go c.pump()
	go c.feed()
	return c
}

// pump copies the process output chunks into the stdout/stderr pipes until the process
// ends (a closed pipe means the client closed the connection first).
func (c *procConn) pump() {
	defer func() {
		_ = c.outW.Close()
		_ = c.errW.Close()
	}()
	for {
		chunk, ok := c.proc.Output(context.Background())
		if !ok {
			return
		}
		var w io.Writer = c.outW
		if chunk.Stderr {
			w = c.errW
		}
		if _, err := w.Write(chunk.Data); err != nil {
			return
		}
	}
}

// feed forwards stdin to the process. A busy stdin buffer is retried (the LSP pipe gives
// backpressure); a dead process ends the loop.
func (c *procConn) feed() {
	defer c.proc.CloseStdin()
	buf := make([]byte, 32<<10)
	for {
		n, err := c.inR.Read(buf)
		if n > 0 {
			for {
				werr := c.proc.Write(buf[:n])
				if werr == nil {
					break
				}
				if closedStdin(werr) {
					return
				}
				time.Sleep(2 * time.Millisecond)
			}
		}
		if err != nil {
			return
		}
	}
}

// closedStdin reports whether a stdin write failed because the process is gone or its
// stdin is closed, rather than because its bounded buffer is momentarily full.
func closedStdin(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "closed") || strings.Contains(msg, "exited") || strings.Contains(msg, "busy")
}

func (c *procConn) Read(p []byte) (int, error) { return c.out.Read(p) }

func (c *procConn) Write(p []byte) (int, error) { return c.in.Write(p) }

// Stderr returns the reader of the server's error output (the client's error tail).
func (c *procConn) Stderr() io.Reader { return c.errR }

// Close ends the client's connection and terminates the remote child. It is idempotent.
func (c *procConn) Close() error {
	c.closeOnce.Do(func() {
		_ = c.in.Close()
		_ = c.out.Close()
		_ = c.errR.Close()
		c.proc.Terminate(time.Second)
	})
	return nil
}

// lspReconnectRestartTimeout bounds the post-reconnect restart work. The manager checks the
// context between servers, so a carrier that drops again stops the loop promptly (a start
// already in flight is the manager's own bounded handshake and cannot be cancelled here).
const lspReconnectRestartTimeout = 30 * time.Second

// restartLSPAfterReconnect is the root's AfterReconnect hook for a remote workspace: it
// restarts the language servers that were running before the carrier drop. It runs inside
// the root's command (off the update goroutine), does nothing when the authority is not
// connected, and the manager's spawn gate still decides before every restart (trust);
// duplicates are prevented by the manager (a live handle or a start in flight is kept).
func restartLSPAfterReconnect(slot authority.Slot, port tui.LSPSession) (int, error) {
	if slot == nil || port == nil || !slot.Connected() {
		return 0, nil
	}
	restarter, ok := port.(interface {
		RestartAfterReconnect(context.Context) (int, error)
	})
	if !ok {
		return 0, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), lspReconnectRestartTimeout)
	defer cancel()
	return restarter.RestartAfterReconnect(ctx)
}
