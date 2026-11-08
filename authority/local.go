package authority

import (
	"context"
	"strings"

	"github.com/tbxark/gvim/plugin/hostos"
)

// LocalOptions build the local slot over an existing OS implementation (the editor's
// plugin OS port). Shell, when set, is the integrated terminal's local shell probe; Gate
// and Env apply to every launch.
type LocalOptions struct {
	OS    hostos.OS
	Gate  Gate
	Env   EnvProvider
	Shell func() (string, []string)
}

// local is the local implementation: it forwards to the OS port and keeps the gate and the
// env provider at one choke point, exactly like the other backends do.
type local struct {
	os    hostos.OS
	gate  Gate
	env   EnvProvider
	shell func() (string, []string)
}

// Local mounts a local slot. A nil OS refuses everything with the port's unsupported error.
func Local(o LocalOptions) *Authority {
	if o.OS == nil {
		o.OS = hostos.NopOS{}
	}
	l := &local{os: o.OS, gate: o.Gate, env: o.Env, shell: o.Shell}
	return New(Parts{Files: l, OneShot: l, LongRunning: l, Terminal: l})
}

func (l *local) ReadFile(ctx context.Context, path string, max int64) ([]byte, error) {
	return l.os.ReadFile(ctx, path, max)
}
func (l *local) WriteFile(ctx context.Context, path string, data []byte, o hostos.WriteOptions) error {
	return l.os.WriteFile(ctx, path, data, o)
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

// allowed runs the trust gate (nil allows).
func (l *local) allowed(ctx context.Context, argv []string, cwd string) error {
	if l.gate == nil {
		return nil
	}
	return l.gate(ctx, argv, cwd)
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
	if err := l.allowed(ctx, argv, o.Cwd); err != nil {
		return hostos.ProcResult{}, err
	}
	return l.os.Run(ctx, argv, l.envMap(o))
}

func (l *local) Start(ctx context.Context, argv []string, o hostos.ProcOptions) (hostos.Proc, error) {
	if err := l.allowed(ctx, argv, o.Cwd); err != nil {
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
	if err := l.allowed(ctx, nil, o.Dir); err != nil {
		return TerminalLaunch{}, err
	}
	return TerminalLaunch{Shell: shell, Args: args, Dir: o.Dir, Env: env, ManagesCwd: false}, nil
}
