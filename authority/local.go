package authority

import (
	"context"
	"os"
	"strings"

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
