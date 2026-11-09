package authority

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/tbxark/gvim/plugin/hostos"
	"github.com/tbxark/gvim/trust"
)

// LocalOptions build the local slot over an existing OS implementation (the editor's
// plugin OS port). Shell, when set, is the integrated terminal's local shell probe; Gate
// and Env apply to every launch.
type LocalOptions struct {
	OS hostos.OS
	// Gate is the trust check without the child environment; kept for callers that
	// build a local slot without PATH resolution (and for tests).
	Gate Gate
	// GateEnv, when set, is preferred over Gate: it receives the launch's effective
	// environment so a bare command name can be resolved against the child's PATH.
	GateEnv GateEnv
	Env     EnvProvider
	Shell   func() (string, []string)
}

// local is the local implementation: it forwards to the OS port and keeps the gate and the
// env provider at one choke point, exactly like the other backends do.
type local struct {
	os      hostos.OS
	gate    Gate
	gateEnv GateEnv
	env     EnvProvider
	shell   func() (string, []string)
}

// Local mounts a local slot. A nil OS refuses everything with the port's unsupported error.
func Local(o LocalOptions) *Authority {
	if o.OS == nil {
		o.OS = hostos.NopOS{}
	}
	l := &local{os: o.OS, gate: o.Gate, gateEnv: o.GateEnv, env: o.Env, shell: o.Shell}
	return New(Parts{Files: l, OneShot: l, LongRunning: l, Terminal: l})
}

func (l *local) ReadFile(ctx context.Context, path string, max int64) ([]byte, error) {
	return l.os.ReadFile(ctx, path, max)
}

// localReadRangeCap bounds one local windowed read, matching the SSH transport cap.
const localReadRangeCap = 1 << 20

func (l *local) ReadRange(ctx context.Context, path string, off, n int64) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if off < 0 || n < 0 {
		return nil, fmt.Errorf("read range %s: negative offset or length", path)
	}
	n = min(n, localReadRangeCap)
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	buf := make([]byte, n)
	got, err := f.ReadAt(buf, off)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	if got < len(buf) {
		return buf[:got], io.EOF
	}
	return buf, nil
}

// Mkdir implements FileMutations: the local tree through the OS port.
func (l *local) Mkdir(ctx context.Context, path string, parents bool) error {
	return l.os.Mkdir(ctx, path, parents)
}

// Rename implements FileMutations.
func (l *local) Rename(ctx context.Context, from, to string) error {
	return l.os.Rename(ctx, from, to)
}

// SetMetadata implements FileMetadata: chmod and chtimes on the local path. A zero mode
// or zero mtime leaves that field unchanged.
func (l *local) SetMetadata(ctx context.Context, path string, mode fs.FileMode, mtime time.Time) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if mode != 0 {
		if err := os.Chmod(path, mode.Perm()); err != nil {
			return err
		}
	}
	if !mtime.IsZero() {
		if err := os.Chtimes(path, mtime, mtime); err != nil {
			return err
		}
	}
	return nil
}

// Remove implements FileMutations.
func (l *local) Remove(ctx context.Context, path string, recursive bool) error {
	return l.os.Remove(ctx, path, recursive)
}

func (l *local) WriteFile(ctx context.Context, path string, data []byte, o hostos.WriteOptions) error {
	return l.os.WriteFile(ctx, path, data, o)
}

// CreateNew implements FileCreator: an exclusive create (O_WRONLY|O_CREATE|O_EXCL), so
// nothing that already exists at path (file, directory or symbolic link) is replaced. The
// default permission is 0644 through the process umask, like fsops.CreateFile.
func (l *local) CreateNew(ctx context.Context, path string, o hostos.WriteOptions) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validateCreateOptions(o); err != nil {
		return err
	}
	perm := fs.FileMode(0o644)
	if o.Mode != 0 {
		perm = o.Mode.Perm()
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
	if err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return nil
}

// CreateNewWithContent implements FileContentCreator: one O_EXCL open, then the data is
// written through that same descriptor. The path is never opened a second time, so a
// concurrent replacement cannot redirect the write. A write/close failure returns the
// error and leaves the claimed file (deleting by path could remove a file another process
// has since replaced).
func (l *local) CreateNewWithContent(ctx context.Context, path string, data []byte, o hostos.WriteOptions) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validateCreateOptions(o); err != nil {
		return err
	}
	if data == nil {
		data = []byte{}
	}
	perm := fs.FileMode(0o644)
	if o.Mode != 0 {
		perm = o.Mode.Perm()
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
	if err != nil {
		return err
	}
	_, werr := f.Write(data)
	cerr := f.Close()
	return errors.Join(werr, cerr)
}

// WriteStream implements WriteStreamer: a temporary file in the target's directory,
// chunked writes into it, then the mode and one rename over the target. The target is
// untouched until Commit; Abort removes the temporary file. It follows a symbolic link to
// its target like the local atomic WriteFile path, and preserves an existing target's
// permission when WriteOptions.Mode is zero.
func (l *local) WriteStream(ctx context.Context, path string, o hostos.WriteOptions) (WriteStream, error) {
	return l.writeStream(ctx, path, o, true)
}

// WriteStreamNoFollow implements NoFollowWriteStreamer: the same atomic stream, but a
// symbolic link at the final destination is not resolved. Commit renames over the link
// itself, so a copy whose canonical destination was proven does not accidentally replace
// the link's target if another writer swaps a link in.
func (l *local) WriteStreamNoFollow(ctx context.Context, path string, o hostos.WriteOptions) (WriteStream, error) {
	return l.writeStream(ctx, path, o, false)
}

func (l *local) writeStream(ctx context.Context, path string, o hostos.WriteOptions, follow bool) (WriteStream, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := validateStreamOptions(o); err != nil {
		return nil, err
	}
	target := path
	if follow {
		if fi, err := os.Lstat(path); err == nil && fi.Mode()&os.ModeSymlink != 0 {
			resolved, rerr := filepath.EvalSymlinks(path)
			if rerr != nil {
				return nil, rerr
			}
			target = resolved
		}
	}
	mode := fs.FileMode(0o644)
	if o.Mode != 0 {
		mode = o.Mode.Perm()
	} else if fi, err := os.Stat(target); err == nil {
		mode = fi.Mode().Perm()
	}
	f, err := os.CreateTemp(filepath.Dir(target), "."+filepath.Base(target)+".gvim-*")
	if err != nil {
		return nil, err
	}
	return &localWriteStream{file: f, tmp: f.Name(), target: target, mode: mode}, nil
}

// localWriteStream is a local file being streamed into a temporary file and renamed at
// Commit. Every method is called from the save command.
type localWriteStream struct {
	file   *os.File
	tmp    string
	target string
	mode   fs.FileMode
	done   bool
}

func (s *localWriteStream) Write(ctx context.Context, p []byte) error {
	if s.done {
		return fs.ErrClosed
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	_, err := s.file.Write(p)
	return err
}

func (s *localWriteStream) Commit(ctx context.Context) error {
	if s.done {
		return fs.ErrClosed
	}
	if err := ctx.Err(); err != nil {
		s.abort()
		return err
	}
	if err := s.file.Chmod(s.mode); err != nil {
		s.abort()
		return err
	}
	if err := s.file.Close(); err != nil {
		s.abort()
		return err
	}
	if err := os.Rename(s.tmp, s.target); err != nil {
		s.abort()
		return err
	}
	s.done = true
	return nil
}

func (s *localWriteStream) Abort(context.Context) { s.abort() }

func (s *localWriteStream) abort() {
	if s.done {
		return
	}
	s.done = true
	_ = s.file.Close()
	_ = os.Remove(s.tmp)
}

func (l *local) Stat(ctx context.Context, path string) (hostos.FileStat, error) {
	return l.os.Stat(ctx, path)
}
func (l *local) ReadDir(ctx context.Context, path string, hidden bool) ([]hostos.DirEntry, error) {
	return l.os.ReadDir(ctx, path, hidden)
}
func (l *local) Watch(ctx context.Context, path string, o hostos.WatchOptions) (hostos.Watch, error) {
	return l.os.Watch(ctx, path, o)
}

// allowed runs the trust gate (nil allows): the environment-aware gate when one is
// installed, else the plain one.
func (l *local) allowed(ctx context.Context, argv []string, cwd string, env map[string]string) error {
	if l.gateEnv != nil {
		return l.gateEnv(ctx, argv, cwd, env)
	}
	if l.gate == nil {
		return nil
	}
	return l.gate(ctx, argv, cwd)
}

// effectiveEnv is the environment the child will inherit: the parent environment, the
// active project overlay, then the launch's entries through envMap — the same merge the
// OS port receives, so the gate's PATH is the child's PATH.
func (l *local) effectiveEnv(o hostos.ProcOptions) map[string]string {
	env := make(map[string]string, len(o.Env)+16)
	for _, kv := range os.Environ() {
		if k, v, ok := strings.Cut(kv, "="); ok {
			env[k] = v
		}
	}
	if l.env != nil {
		for _, kv := range l.env() {
			if k, v, ok := strings.Cut(kv, "="); ok {
				env[k] = v
			}
		}
	}
	for k, v := range l.envMap(o).Env {
		env[k] = v
	}
	return env
}

// envMap merges the provider's entries into o.Env.
func (l *local) envMap(o hostos.ProcOptions) hostos.ProcOptions {
	if l.env == nil {
		return o
	}
	extra := l.env()
	if len(extra) == 0 {
		return o
	}
	m := make(map[string]string, len(o.Env)+len(extra))
	for k, v := range o.Env {
		m[k] = v
	}
	for _, entry := range extra {
		if k, v, ok := strings.Cut(entry, "="); ok {
			m[k] = v
		}
	}
	o.Env = m
	return o
}

func (l *local) Run(ctx context.Context, argv []string, o hostos.ProcOptions) (hostos.ProcResult, error) {
	if err := l.allowed(ctx, argv, o.Cwd, l.effectiveEnv(o)); err != nil {
		return hostos.ProcResult{}, err
	}
	return l.os.Run(ctx, argv, l.envMap(o))
}

func (l *local) Start(ctx context.Context, argv []string, o hostos.ProcOptions) (hostos.Proc, error) {
	if err := l.allowed(ctx, argv, o.Cwd, l.effectiveEnv(o)); err != nil {
		return nil, err
	}
	return l.os.Start(ctx, argv, l.envMap(o))
}

// Launch describes the local terminal: the caller starts a local PTY as before.
func (l *local) Launch(ctx context.Context, o TerminalOptions) (TerminalLaunch, error) {
	shell, args := o.Shell, o.Args
	if shell == "" && l.shell != nil {
		shell, args = l.shell()
	}
	env := append([]string(nil), o.Env...)
	if l.env != nil {
		env = append(env, l.env()...)
	}
	// The terminal's shell is supplied separately from the argv shape; gate it by name so
	// a project-contained shell override is refused under Restricted and a Blocked
	// workspace refuses the terminal itself.
	termEnv := make(map[string]string, 16)
	for _, kv := range os.Environ() {
		if k, v, ok := strings.Cut(kv, "="); ok {
			termEnv[k] = v
		}
	}
	for _, kv := range env {
		if k, v, ok := strings.Cut(kv, "="); ok {
			termEnv[k] = v
		}
	}
	shellArgv := []string(nil)
	if shell != "" {
		// Resolve a bare shell with the same child PATH the gate uses, and hand the
		// resolved absolute path to the caller: the PTY must start the program the
		// gate allowed, not whatever the parent's LookPath would find later.
		if !strings.ContainsAny(shell, "/\\") {
			if resolved, ok := trust.ResolveSpawnPath(o.Dir, shell, termEnv["PATH"]); ok {
				shell = resolved
			}
		}
		shellArgv = []string{shell}
	}
	if err := l.allowed(ctx, shellArgv, o.Dir, termEnv); err != nil {
		return TerminalLaunch{}, err
	}
	return TerminalLaunch{Shell: shell, Args: args, Dir: o.Dir, Env: env, ManagesCwd: false}, nil
}
