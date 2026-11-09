package pluginhost

import (
	"context"
	"path"
	"strings"
	"time"

	"github.com/tbxark/gvim/authority"
	"github.com/tbxark/gvim/plugin/hostos"
	"github.com/tbxark/gvim/pluginapi"
)

// authorityOS is the plugin hostos.OS of a remote workspace: gvim.fs writes/reads and
// gvim.proc runs happen on the machine the workspace authority names, not on the local
// machine. It is installed by the app through Options.WrapOS only for a non-local slot;
// a local session keeps the plain plugin OS byte-for-byte.
//
// The authority's own choke point applies the trust gate and the remote working
// directory/environment to every process launch, so this type does not duplicate the
// gate. File mutations are the authority's optional FileMutations surface: a backend
// without it gets a clear unsupported error instead of a local fallback. Walk and Copy
// are bounded re-implementations over the authority's ReadDir/ReadFile/WriteFile (see
// authorityos_walk.go); RealPath has no remote readlink primitive and stays unsupported.
// LookPath, Getenv, Dirs and Now are local-machine APIs (this process), as documented in
// docs/plugins/api-core.md.
type authorityOS struct {
	slot  authority.Slot
	local hostos.OS
	// gate is the workspace trust spawn gate. The authority backend already uses its
	// own gate for the surfaces it mounts; this wrapper applies the plugin host's gate
	// too, so a plugin launch is refused before any remote session is opened even on a
	// backend whose slot was built without one.
	gate SpawnGate
}

var _ hostos.OS = (*authorityOS)(nil)

// NewAuthorityOS returns the plugin OS backed by slot. local provides the local-only
// APIs (LookPath, Getenv, Dirs, Now) and the Shutdown fallback; nil is safe. gate is the
// workspace trust spawn gate consulted before every remote process launch (nil allows).
func NewAuthorityOS(slot authority.Slot, local hostos.OS, gate SpawnGate) hostos.OS {
	if local == nil {
		local = hostos.NopOS{}
	}
	return &authorityOS{slot: slot, local: local, gate: gate}
}

// checkGate applies the workspace trust spawn gate before a launch, with the resolved
// working directory and the launch's own environment entries (the remote environment the
// authority adds is not visible here; the app's remote gate is the coarse trust rule).
func (o *authorityOS) checkGate(ctx context.Context, argv []string, opts hostos.ProcOptions) error {
	if o.gate == nil {
		return nil
	}
	env := make(map[string]string, len(opts.Env))
	for k, v := range opts.Env {
		env[k] = v
	}
	return o.gate(ctx, argv, o.resolveCwd(opts.Cwd), env)
}

// localOS is the fallback for the documented local-only APIs.
func (o *authorityOS) localOS() hostos.OS { return o.local }

// remoteRoot is the workspace root of the slot, or "" for a remote spec that has none.
func (o *authorityOS) remoteRoot() string {
	if o.slot == nil {
		return ""
	}
	if spec := o.slot.State().Remote; spec != nil {
		return spec.Workspace()
	}
	return ""
}

// resolve makes a plugin path absolute on the remote host: a relative path is relative
// to the workspace root, and a path is cleaned POSIX-style (the authority is a POSIX
// helper transport). The remote target itself may be "~" (the login home), which the
// quoted helper cannot expand: relative paths then stay relative, which resolves against
// the remote login home the helper inherits. It never resolves a symlink; that is what
// fs.realpath would need.
func (o *authorityOS) resolve(p string) string {
	if p == "" {
		return p
	}
	if strings.HasPrefix(p, "/") || strings.HasPrefix(p, "~") {
		return path.Clean(p)
	}
	if root := o.remoteRoot(); root != "" && root != "~" {
		return path.Join(root, p)
	}
	return path.Clean(p)
}

// resolveCwd resolves a process working directory; an empty one stays empty (the
// authority's default). A bare remote target ("~") keeps a relative cwd relative to the
// remote login home the authority's command starts in.
func (o *authorityOS) resolveCwd(cwd string) string {
	if cwd == "" {
		return ""
	}
	root := o.remoteRoot()
	if root == "" || root == "~" {
		if strings.HasPrefix(cwd, "/") || strings.HasPrefix(cwd, "~") {
			return path.Clean(cwd)
		}
		return cwd
	}
	return o.resolve(cwd)
}

// files is the slot's file surface (never nil for a mounted slot).
func (o *authorityOS) files() authority.FileSystem {
	if o.slot == nil {
		return authority.UnsupportedFiles{}
	}
	return o.slot.Files()
}

func (o *authorityOS) ReadFile(ctx context.Context, p string, max int64) ([]byte, error) {
	data, err := o.files().ReadFile(ctx, o.resolve(p), max)
	return data, mapErr(err, p)
}

func (o *authorityOS) WriteFile(ctx context.Context, p string, data []byte, opts hostos.WriteOptions) error {
	if !opts.Exclusive {
		return mapErr(o.files().WriteFile(ctx, o.resolve(p), data, opts), p)
	}
	// Exclusive create needs a single claim-and-write operation: the FileCreator alone
	// only creates an empty file, and a separate WriteFile could follow a path another
	// process replaced between the two calls. Backends without the content creator must
	// refuse rather than falling back to that race.
	if opts.Append || opts.Parents {
		return pluginapi.Errf(pluginapi.CodeInvalid, "%s: a create-new cannot append or create parent directories", p)
	}
	creator := authority.ContentCreators(o.files())
	if creator == nil {
		return pluginapi.Errf(pluginapi.CodeUnsupported,
			"%s: the workspace authority does not support exclusive file creation with content", p)
	}
	createOpts := opts
	createOpts.Exclusive = false
	createOpts.Atomic = false
	return mapErr(creator.CreateNewWithContent(ctx, o.resolve(p), data, createOpts), p)
}

func (o *authorityOS) ReadDir(ctx context.Context, p string, hidden bool) ([]hostos.DirEntry, error) {
	entries, err := o.files().ReadDir(ctx, o.resolve(p), hidden)
	return entries, mapErr(err, p)
}

func (o *authorityOS) Stat(ctx context.Context, p string) (hostos.FileStat, error) {
	st, err := o.files().Stat(ctx, o.resolve(p))
	return st, mapErr(err, p)
}

// mutations returns the slot's mutation surface, or the clear unsupported error.
func (o *authorityOS) mutations(p string) (authority.FileMutations, error) {
	if m := authority.Mutations(o.files()); m != nil {
		return m, nil
	}
	return nil, pluginapi.Errf(pluginapi.CodeUnsupported,
		"%s: the workspace authority does not support file changes", p)
}

func (o *authorityOS) Mkdir(ctx context.Context, p string, parents bool) error {
	m, err := o.mutations(p)
	if err != nil {
		return err
	}
	return mapErr(m.Mkdir(ctx, o.resolve(p), parents), p)
}

func (o *authorityOS) Remove(ctx context.Context, p string, recursive bool) error {
	m, err := o.mutations(p)
	if err != nil {
		return err
	}
	return mapErr(m.Remove(ctx, o.resolve(p), recursive), p)
}

func (o *authorityOS) Rename(ctx context.Context, from, to string) error {
	m, err := o.mutations(from)
	if err != nil {
		return err
	}
	return mapErr(m.Rename(ctx, o.resolve(from), o.resolve(to)), from)
}

func (o *authorityOS) Watch(ctx context.Context, p string, opts hostos.WatchOptions) (hostos.Watch, error) {
	w, err := o.files().Watch(ctx, o.resolve(p), opts)
	return w, mapErr(err, p)
}

// Run runs a one-shot process through the slot: the authority's gate, working
// directory and environment apply, and the process runs where the workspace lives.
func (o *authorityOS) Run(ctx context.Context, argv []string, opts hostos.ProcOptions) (hostos.ProcResult, error) {
	if len(argv) == 0 {
		return hostos.ProcResult{}, pluginapi.Errf(pluginapi.CodeInvalid, "empty argv")
	}
	if o.slot == nil {
		return hostos.ProcResult{}, pluginapi.Errf(pluginapi.CodeUnsupported, "proc.run: no workspace authority")
	}
	if err := o.checkGate(ctx, argv, opts); err != nil {
		return hostos.ProcResult{}, mapErr(err, argv[0])
	}
	opts.Cwd = o.resolveCwd(opts.Cwd)
	res, err := o.slot.OneShot().Run(ctx, argv, opts)
	return res, mapErr(err, argv[0])
}

// Start starts a stdio child through the slot. Its output and exit are the authority's
// Proc, so a remote plugin process is bounded by the same transport as LSP servers.
func (o *authorityOS) Start(ctx context.Context, argv []string, opts hostos.ProcOptions) (hostos.Proc, error) {
	if len(argv) == 0 {
		return nil, pluginapi.Errf(pluginapi.CodeInvalid, "empty argv")
	}
	if o.slot == nil {
		return nil, pluginapi.Errf(pluginapi.CodeUnsupported, "proc.spawn: no workspace authority")
	}
	if err := o.checkGate(ctx, argv, opts); err != nil {
		return nil, mapErr(err, argv[0])
	}
	opts.Cwd = o.resolveCwd(opts.Cwd)
	proc, err := o.slot.LongRunning().Start(ctx, argv, opts)
	return proc, mapErr(err, argv[0])
}

// LookPath is a documented local-only API: it searches this machine, not the remote
// host (the remote process is started by the authority, which resolves its own PATH).
func (o *authorityOS) LookPath(name string) (string, bool) { return o.localOS().LookPath(name) }

// Getenv is a documented local-only API.
func (o *authorityOS) Getenv(name string) (string, bool) { return o.localOS().Getenv(name) }

// Dirs are documented local-only: the plugin process's config, home, temp and cwd on
// this machine. Root is the workspace root as the app passes it.
func (o *authorityOS) Dirs() hostos.Dirs { return o.localOS().Dirs() }

// Now is the local clock; time is not a workspace primitive.
func (o *authorityOS) Now() time.Time { return o.localOS().Now() }

// Shutdown ends the local fallback's resources. The authority slot owns the remote
// processes and watchers and is closed by the app; the plugin runtime must not close
// the workspace connection.
func (o *authorityOS) Shutdown() { o.localOS().Shutdown() }
