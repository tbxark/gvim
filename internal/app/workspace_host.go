package app

// The daemon workspace layer (#19a): one tea program over a tui.WorkspaceHost that swaps
// the active workspace model, and a manager of materialized workspace sessions over the
// durable registry.
//
// Lifecycle: the launch root's workspace is materialized eagerly at daemon start. Other
// workspaces stay inert until a switch. A switch captures the current workspace
// (layout/stashes/terminal record into its per-workspace session directory) and then
// builds the target runtime OFF the UI goroutine: buildRuntime loads files and starts
// processes, so blocking Update would freeze input and quit. The switch result carries a
// sequence number; a newer request cancels the build (context) and a stale result is
// closed instead of switching. Warm runtimes are kept (LSP, plugins, dock shells), so
// switching back is a model swap.
//
// Exit saves every materialized workspace (active first) and closes it; detach only drops
// the client, so the program keeps running and nothing is saved. The workspace registry
// v1 layout and the per-workspace session directories are documented in
// docs/architecture.md §8.1.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/tbxark/gvim/internal/daemon"
	"github.com/tbxark/gvim/internal/tui"
	"github.com/tbxark/gvim/session"
)

// errWorkspaceStale is the outcome of a switch a newer request superseded.
var errWorkspaceStale = errors.New("workspace: switch superseded")

// workspaceSession is one materialized workspace: its registry entry and runtime.
type workspaceSession struct {
	ws daemon.Workspace
	rt *runtime
}

// workspaceManager owns the registry and the materialized runtimes of a daemon. Editor
// state is only read on the program's UI goroutine (capture, label changes, host.Show);
// building a dormant runtime happens off it. The mutex protects the maps and the request
// sequence, which the socket goroutine also touches.
type workspaceManager struct {
	ctx   context.Context
	opts  Options
	reg   *daemon.Registry
	build func(context.Context, Options) (*runtime, error)

	mu       sync.Mutex
	sessions map[string]*workspaceSession
	active   string
	host     *tui.WorkspaceHost
	// seq is the switch request sequence; cancel cancels the build of the previous
	// request so a newer switch does not wait for stale work.
	seq uint64
	// pendingID is the target of the newest switch request (the one the build goroutine
	// commits when it finishes and still owns the sequence).
	pendingID string
	cancel    context.CancelFunc
	// writes are the outstanding asynchronous capture writes, in order.
	writes []*saveJob
	// launches are the imported sessions' terminal launches waiting for their
	// workspace to be materialized and shown (keyed by stable id).
	launches map[string]tui.WorkspaceLaunch
	// writers are the per-workspace ordered capture write queues.
	writers map[string]*captureWriter
	// writeSnapshotFn is a test hook replacing writeSnapshot.
	writeSnapshotFn func(*workspaceSession, *captureSnapshot) error
	// captureWriterGate is a test hook called by runWriter before it takes the per-workspace
	// write lock; it lets a test hold a writer BEFORE it commits (the delayed-writer
	// regression for the exit commit token). Nil in production.
	captureWriterGate func()
	// beginSelectFn is a test hook replacing beginSelect for the extract path, so a
	// regression can inject a synchronous refusal and a queued-switch failure.
	beginSelectFn func(string) (func() error, error)
	// lastNotice is the last failure reported to the user (test/observability).
	lastNotice string
	// lastReport is the last successful workspace action report (a close that stopped
	// running terminals, an extraction), carried to the socket protocol's close answer.
	lastReport string
	now        func() time.Time
}

// newWorkspaceManager opens the registry, discovers the launch root as the foreground
// workspace and materializes it eagerly (this runs before the program exists).
func newWorkspaceManager(ctx context.Context, opts Options) (*workspaceManager, error) {
	reg, err := daemon.OpenRegistry("")
	if err != nil {
		return nil, err
	}
	root := opts.Root
	if root == "" {
		wd, err := os.Getwd()
		if err != nil {
			return nil, err
		}
		root = wd
	}
	specText := ""
	if opts.Remote != nil {
		specText, err = authoritySpecText(opts.Remote)
		if err != nil {
			return nil, fmt.Errorf("daemon: remote authority: %w", err)
		}
	}
	var disc daemon.Discovery
	if specText != "" {
		// The daemon was started with a remote target (jump box): the foreground
		// workspace is bound to that authority spec, and a restart with the same target
		// reuses the same stable id/session directory.
		disc, err = reg.DiscoverWithAuthority(opts.Remote.Workspace(), specText)
	} else {
		disc, err = reg.Discover(root)
	}
	if err != nil {
		return nil, err
	}
	m := &workspaceManager{
		ctx: ctx, opts: opts, reg: reg, now: time.Now,
		sessions: map[string]*workspaceSession{},
		launches: map[string]tui.WorkspaceLaunch{},
		writers:  map[string]*captureWriter{},
		build: func(ctx context.Context, opts Options) (*runtime, error) {
			return buildRuntime(ctx, opts, true)
		},
	}
	sess, err := m.buildWorkspace(m.ctx, disc.Foreground.File.StableID)
	if err != nil {
		return nil, err
	}
	m.sessions[sess.ws.File.StableID] = sess
	m.active = sess.ws.File.StableID
	// Bind the daemon's shared plugin runtime/host to the eagerly materialized foreground
	// workspace before anything can start it (runDaemonHost's explicit call is idempotent).
	m.activateForegroundPlugins()
	return m, nil
}

// ensureForegroundAuthority connects a daemon-started remote foreground workspace on the
// daemon's own terminal before the socket starts hosting: the Fresh jump-box daemon answers
// password/passphrase/2FA prompts there once, then attaches need no SSH setup of their own.
// A non-interactive daemon fails with an actionable error instead of prompting on a stdin
// nobody answers.
func (m *workspaceManager) ensureForegroundAuthority(ctx context.Context) error {
	m.mu.Lock()
	sess := m.sessions[m.active]
	m.mu.Unlock()
	if sess == nil || sess.ws.File.AuthoritySpec == "" {
		return nil
	}
	if sess.rt.auth == nil || sess.rt.auth.State().Connected {
		return nil
	}
	prompter, err := daemonPrompterFactory()
	if err != nil {
		return err
	}
	if err := connectDaemonAuthority(ctx, sess.rt.auth, prompter); err != nil {
		return err
	}
	return nil
}

// setHost gives the manager the workspace host that swaps program models.
func (m *workspaceManager) setHost(h *tui.WorkspaceHost) {
	m.mu.Lock()
	m.host = h
	m.mu.Unlock()
}

// ForegroundModel is the model the daemon's program starts on.
func (m *workspaceManager) ForegroundModel() *tui.Model {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s := m.sessions[m.active]; s != nil {
		return s.rt.model
	}
	return nil
}

// runOnUI runs fn on the program's UI goroutine and waits. Without a host (tests) it runs
// inline. It reports false when the program is not ready or does not answer.
func (m *workspaceManager) runOnUI(fn func()) bool {
	m.mu.Lock()
	h := m.host
	m.mu.Unlock()
	if h == nil || !h.Ready() {
		// No program yet: no concurrent Update loop, so the call runs inline (the
		// startup materialization path). Once the program exists it marshals.
		fn()
		return true
	}
	return h.Do(fn)
}

// notify shows a failure on the active workspace's status row.
func (m *workspaceManager) notify(err error) {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, errWorkspaceStale) {
		return
	}
	m.mu.Lock()
	m.lastNotice = err.Error()
	h := m.host
	m.mu.Unlock()
	if h != nil {
		h.Notify(err.Error())
	}
}

// workspaceOptions is the build options of one workspace: its root, no launch files, and
// its own session directory (layout, stashes, terminal record).
func (m *workspaceManager) workspaceOptions(ws daemon.Workspace) (Options, error) {
	dir, err := m.reg.SessionDir(ws.File.StableID)
	if err != nil {
		return Options{}, err
	}
	opts := m.opts
	opts.Root = ws.File.Root
	opts.Files = nil
	opts.Remote = nil
	if ws.File.AuthoritySpec != "" {
		spec, err := parseAuthoritySpec(ws.File.AuthoritySpec)
		if err != nil {
			return Options{}, fmt.Errorf("workspace %q: remote authority %q: %w", ws.File.Label, ws.File.AuthoritySpec, err)
		}
		opts.Remote = spec
		// The remote root is not a local directory: buildRuntime resolves the local
		// process root itself for a remote launch (remoteLocalRoot) and uses the remote
		// workspace path from the spec.
		opts.Root = ""
	}
	opts.Diff = false
	opts.Restore = false
	opts.NoRestore = false
	opts.Simple = false
	opts.ReadStdin = false
	opts.Stdin = nil
	opts.StdinName = ""
	opts.SessionDir = dir
	if opts.Daemon != nil {
		d := *opts.Daemon
		opts.Daemon = &d
	}
	return opts, nil
}

// buildWorkspace builds a dormant workspace's runtime. It runs off the UI goroutine: file
// loads, plugin setup and process start belong to the new runtime, not to the active one.
func (m *workspaceManager) buildWorkspace(ctx context.Context, id string) (*workspaceSession, error) {
	ws, ok, err := m.reg.Get(id)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("workspace: no workspace %q", id)
	}
	opts, err := m.workspaceOptions(ws)
	if err != nil {
		return nil, err
	}
	rt, err := m.build(ctx, opts)
	if err != nil {
		return nil, err
	}
	rt.model.SetWorkspacePort(m)
	rt.model.SetWorkspaceLabel(ws.File.Label)
	rt.model.AddWorkspaceCommands()
	return &workspaceSession{ws: ws, rt: rt}, nil
}

// entriesLocked is the workspace list in registry order. Called under m.mu.
func (m *workspaceManager) entriesLocked() ([]tui.WorkspaceEntry, []daemon.WorkspaceInfo) {
	list, _ := m.reg.List()
	entries := make([]tui.WorkspaceEntry, 0, len(list))
	infos := make([]daemon.WorkspaceInfo, 0, len(list))
	for _, w := range list {
		_, materialized := m.sessions[w.File.StableID]
		active := w.File.StableID == m.active
		entries = append(entries, tui.WorkspaceEntry{
			ID: w.File.StableID, Label: w.File.Label, Root: w.File.Root,
			Materialized: materialized, Active: active,
		})
		infos = append(infos, daemon.WorkspaceInfo{
			ID: w.File.StableID, Label: w.File.Label, Root: w.File.Root,
			Materialized: materialized, Active: active,
		})
	}
	return entries, infos
}

// listResultLocked is the protocol answer of the current workspace list.
func (m *workspaceManager) listResultLocked() daemon.WorkspaceResult {
	_, infos := m.entriesLocked()
	return daemon.WorkspaceResult{List: &daemon.WorkspaceList{Active: m.active, Entries: infos}}
}

// applyCommit makes a built workspace the active model. Must run on the UI goroutine.
func (m *workspaceManager) applyCommit(seq uint64) {
	m.mu.Lock()
	if seq != m.seq {
		m.mu.Unlock()
		return
	}
	sess := m.sessions[m.active]
	_ = sess
	targetID := m.pendingID
	target := m.sessions[targetID]
	if target == nil {
		m.mu.Unlock()
		return
	}
	launch, hasLaunch := m.launches[targetID]
	if hasLaunch {
		delete(m.launches, targetID)
	}
	m.active = targetID
	host := m.host
	m.mu.Unlock()
	target.rt.model.SetWorkspaceLabel(target.ws.File.Label)
	_, _ = m.reg.Touch(targetID, m.now())
	// The daemon's one plugin runtime is rebound to the entering workspace before its
	// model is shown: OS/gate/env and the gvim.state namespace follow the active
	// workspace, and the editor-wide registrations are installed into its model.
	m.activatePlugins(target)
	if host != nil {
		host.Show(target.rt.model)
		if hasLaunch {
			host.Queue(target.rt.model.StartWorkspaceLaunch(launch))
		}
	}
}

// activatePlugins rebinds the daemon's shared plugin runtime and host to sess: its
// per-workspace OS/gate/env become the active plugin OS, its gvim.state namespace becomes
// the runtime's store, and the shared host mux points at the model (with the editor-wide
// registrations installed). Remote/authority workspaces keep their own runtime and are
// skipped (pluginWS nil) - sharing them is a recorded residual. It must run on the UI
// goroutine, before the model is shown.
func (m *workspaceManager) activatePlugins(sess *workspaceSession) {
	if sess == nil || sess.rt == nil {
		return
	}
	if m.opts.PluginDaemon != nil && sess.rt.pluginWS != nil {
		m.opts.PluginDaemon.Activate(sess.rt.pluginWS)
	}
	if m.opts.PluginShared != nil && sess.rt.pluginWS != nil {
		m.opts.PluginShared.Bind(sess.rt.model)
	}
}

// activateForegroundPlugins binds the eagerly materialized foreground workspace before the
// program starts (no UI goroutine yet; the call runs inline).
func (m *workspaceManager) activateForegroundPlugins() {
	m.mu.Lock()
	sess := m.sessions[m.active]
	m.mu.Unlock()
	m.activatePlugins(sess)
}

// beginSelect starts one switch. It runs on the UI goroutine: the current workspace is
// captured here (reading the editor state), then a dormant target is built in a goroutine.
// It returns a wait function (nil for a warm target, which switched immediately), or an
// error. A new request cancels the previous build and makes its result stale.
func (m *workspaceManager) beginSelect(id string) (func() error, error) {
	m.mu.Lock()
	if id == m.active {
		m.mu.Unlock()
		return nil, nil
	}
	_, warm := m.sessions[id]
	cur := m.sessions[m.active]
	m.mu.Unlock()
	if cur != nil {
		m.enqueueCapture(cur)
	}
	m.mu.Lock()
	if _, ok, err := m.reg.Get(id); err != nil || !ok {
		m.mu.Unlock()
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("workspace: no workspace %q", id)
	}
	m.seq++
	seq := m.seq
	m.pendingID = id
	// Cancel only an in-flight build. A committed runtime keeps its own lifetime
	// context (buildRuntime's TermStarter/plugin/LSP resources use it), so canceling it
	// here would kill the warm workspace's shells; a successful build clears m.cancel
	// below as soon as it commits.
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}
	if warm {
		m.mu.Unlock()
		m.applyCommit(seq)
		return nil, nil
	}
	ctx, cancel := context.WithCancel(m.ctx)
	m.cancel = cancel
	m.mu.Unlock()
	ch := make(chan error, 1)
	go func() {
		sess, err := m.buildWorkspace(ctx, id)
		if err == nil {
			m.mu.Lock()
			stale := seq != m.seq || ctx.Err() != nil
			if !stale {
				m.sessions[id] = sess
			}
			m.mu.Unlock()
			if stale {
				sess.rt.exit()
				err = errWorkspaceStale
			} else if !m.runOnUI(func() { m.applyCommit(seq) }) {
				m.mu.Lock()
				delete(m.sessions, id)
				m.mu.Unlock()
				sess.rt.exit()
				err = errors.New("workspace: the daemon is not ready")
			}
		}
		// The build no longer owns a cancel hook once it committed (or failed/staled):
		// a later switch must not cancel the committed runtime's context.
		m.mu.Lock()
		if m.seq == seq {
			// No newer switch started: this request owns the cancel hook.
			m.cancel = nil
		}
		m.mu.Unlock()
		ch <- err
	}()
	return func() error {
		err := <-ch
		if err != nil && !errors.Is(err, errWorkspaceStale) && !errors.Is(err, context.Canceled) {
			m.notify(err)
		}
		return err
	}, nil
}

// Entries implements tui.WorkspacePort.
func (m *workspaceManager) Entries() []tui.WorkspaceEntry {
	m.mu.Lock()
	defer m.mu.Unlock()
	entries, _ := m.entriesLocked()
	return entries
}

// Select implements tui.WorkspacePort.
func (m *workspaceManager) Select(id string) (tui.WorkspaceCmd, error) {
	var wait func() error
	var err error
	if !m.runOnUI(func() { wait, err = m.beginSelect(id) }) {
		return nil, errors.New("workspace: the daemon is not ready")
	}
	return tui.WorkspaceWaitCmd(wait), err
}

// Create implements tui.WorkspacePort: create (or reuse) the workspace for root, then
// switch to it. The registry write and the materialization run off the UI goroutine.
func (m *workspaceManager) Create(root, label string) (tui.WorkspaceCmd, error) {
	if root == "" {
		return nil, errors.New("workspace: the root path is empty")
	}
	return tui.WorkspaceCmdOf(func() {
		ws, _, err := m.reg.Create(root, label)
		if err != nil {
			m.notify(err)
			return
		}
		var wait func() error
		var serr error
		if !m.runOnUI(func() { wait, serr = m.beginSelect(ws.File.StableID) }) {
			m.notify(errors.New("workspace: the daemon is not ready"))
			return
		}
		if serr == nil && wait != nil {
			serr = wait()
		}
		m.notify(serr)
	}), nil
}

// startPendingLaunch starts a remembered launch when its workspace is already shown (a
// repeated import of the same root: each import gets its own attach tab in the one
// workspace of that root).
func (m *workspaceManager) startPendingLaunch(id string) {
	m.mu.Lock()
	launch, ok := m.launches[id]
	host := m.host
	sess := m.sessions[id]
	if ok {
		delete(m.launches, id)
	}
	m.mu.Unlock()
	if !ok || host == nil || sess == nil {
		return
	}
	host.Queue(sess.rt.model.StartWorkspaceLaunch(launch))
}

// CreateWithLaunch implements the discovery picker's Import: create (or reuse) the
// workspace for root and start launch in its dock once the workspace is materialized and
// shown. Same-root imports reuse the one workspace of that canonical root and each starts
// its own attach terminal tab there (the selected session is preserved as a terminal, not
// as a second workspace; the registry v1 has no Source field).
func (m *workspaceManager) CreateWithLaunch(root, label string, launch tui.WorkspaceLaunch) (tui.WorkspaceCmd, error) {
	if root == "" {
		return nil, errors.New("workspace: the root path is empty")
	}
	return tui.WorkspaceCmdOf(func() {
		ws, _, err := m.reg.Create(root, label)
		if err != nil {
			m.notify(err)
			return
		}
		id := ws.File.StableID
		var wait func() error
		var serr error
		var wasActive bool
		if !m.runOnUI(func() {
			m.mu.Lock()
			m.launches[id] = launch
			wasActive = m.active == id
			m.mu.Unlock()
			wait, serr = m.beginSelect(id)
		}) {
			m.notify(errors.New("workspace: the daemon is not ready"))
			return
		}
		if serr != nil {
			m.notify(serr)
			return
		}
		if wait != nil {
			if serr = wait(); serr != nil {
				m.notify(serr)
				return
			}
		}
		if wasActive {
			m.runOnUI(func() { m.startPendingLaunch(id) })
		}
	}), nil
}

// Close implements tui.WorkspacePort: switch away when the workspace is active, then save
// and close its runtime and drop its registry file. The session directory is kept.
func (m *workspaceManager) Close(id string) (tui.WorkspaceCmd, error) {
	return tui.WorkspaceCmdOf(func() {
		if err := m.closeWorkspace(id); err != nil {
			m.notify(err)
		}
	}), nil
}

// closeWorkspace is Close's body. It is safe to call from a tea command or from the
// daemon's connection goroutine (each UI step is marshalled to the program goroutine).
//
// Ordering: the active workspace is switched away first (which queues that workspace's
// capture), the workspace's own snapshot is taken on the UI goroutine and queued on its
// ordered writer, and the runtime is closed only after that write completed - so the
// asynchronous capture can never race the synchronous close save. If the write fails, the
// error is reported and the synchronous save in rt.exit is still attempted; the unsaved
// text is never silently dropped without a report.
func (m *workspaceManager) closeWorkspace(id string) error {
	var wait func() error
	var job *saveJob
	var prepareErr error
	var sess *workspaceSession
	// closeLabel and running are read on the UI goroutine, before anything is closed, so
	// the close report names exactly what its processes were.
	var closeLabel string
	var running []tui.WorkspaceRunningTerminal
	var wasMaterialized bool
	if !m.runOnUI(func() {
		m.mu.Lock()
		active := m.active
		_, materialized := m.sessions[id]
		sess = m.sessions[id]
		var next string
		if id == active {
			entries, _ := m.entriesLocked()
			for _, e := range entries {
				if e.ID != id {
					next = e.ID
					break
				}
			}
		}
		// Closing a workspace that is the target of an in-flight materialization
		// cancels and invalidates that switch: the build must not commit (recreate the
		// workspace) after the close removed it.
		if m.pendingID == id {
			m.seq++
			if m.cancel != nil {
				m.cancel()
				m.cancel = nil
			}
			m.pendingID = ""
		}
		m.mu.Unlock()
		if id == active && next == "" {
			prepareErr = errors.New("workspace: cannot close the last workspace")
			return
		}
		if id == active {
			wait, prepareErr = m.beginSelect(next)
			if prepareErr != nil {
				return
			}
		}
		if materialized {
			wasMaterialized = true
			closeLabel = sess.ws.File.Label
			running = sess.rt.model.RunningTerminals()
			// Snapshot on the UI goroutine (it reads the editor state) and queue it: the
			// write is awaited below, before rt.exit's synchronous save.
			job = m.enqueueCapture(sess)
		}
	}) {
		return errors.New("workspace: the daemon is not ready")
	}
	if prepareErr != nil {
		return prepareErr
	}
	if wait != nil {
		if err := wait(); err != nil {
			return err
		}
	}
	m.mu.Lock()
	sess = m.sessions[id]
	m.mu.Unlock()
	var persistErr error
	if job != nil {
		<-job.done
		if job.err != nil {
			label := id
			if sess != nil {
				label = sess.ws.File.Label
			}
			// The asynchronous capture failed: confirm the data is durable with the
			// synchronous fallback save BEFORE closing anything. If that cannot be
			// confirmed, keep the workspace, the runtime and the registry file so no
			// unsaved state is silently lost, and return the failure.
			if sess != nil {
				if ferr := sess.rt.save(); ferr != nil {
					cerr := fmt.Errorf("close workspace %q: the capture write failed (%v) and the fallback save failed: %w", label, job.err, ferr)
					m.notify(cerr)
					return cerr
				}
			}
			persistErr = fmt.Errorf("close workspace %q: the capture write failed (%w); the fallback save kept the session", label, job.err)
			m.notify(persistErr)
		}
	}
	if sess != nil {
		sess.rt.exit()
	}
	m.mu.Lock()
	delete(m.sessions, id)
	m.mu.Unlock()
	if err := m.reg.Remove(id); err != nil {
		return err
	}
	switch {
	case len(running) > 0:
		m.report(fmt.Sprintf("Closed workspace %q; stopped %s", closeLabel, runningTerminalSummary(running)))
	case !wasMaterialized:
		// A dormant workspace has no runtime and no processes: report the cheap close
		// so the action is not silent.
		m.report("Closed a dormant workspace")
	}
	return persistErr
}

// Rename implements tui.WorkspacePort.
func (m *workspaceManager) Rename(id, label string) error {
	if label == "" {
		return errors.New("workspace: the label is empty")
	}
	ws, err := m.reg.Rename(id, label)
	if err != nil {
		return err
	}
	m.mu.Lock()
	sess := m.sessions[id]
	m.mu.Unlock()
	if sess != nil {
		m.runOnUI(func() {
			m.mu.Lock()
			sess.ws = ws
			m.mu.Unlock()
			sess.rt.model.SetWorkspaceLabel(ws.File.Label)
		})
	}
	m.notifyText(fmt.Sprintf("Renamed workspace to %q", ws.File.Label))
	return nil
}

// Next implements tui.WorkspacePort.
func (m *workspaceManager) Next() (tui.WorkspaceCmd, error) {
	var wait func() error
	var err error
	if !m.runOnUI(func() {
		m.mu.Lock()
		entries, _ := m.entriesLocked()
		active := m.active
		m.mu.Unlock()
		if len(entries) < 2 {
			return
		}
		idx := 0
		for i, e := range entries {
			if e.ID == active {
				idx = i
				break
			}
		}
		wait, err = m.beginSelect(entries[(idx+1)%len(entries)].ID)
	}) {
		return nil, errors.New("workspace: the daemon is not ready")
	}
	if err != nil {
		return nil, err
	}
	return tui.WorkspaceWaitCmd(wait), nil
}

// handleRequest answers one protocol workspace request. It may wait for an asynchronous
// switch, but never blocks the UI goroutine: the work runs off it and commits through it.
func (m *workspaceManager) handleRequest(req daemon.WorkspaceRequest) daemon.WorkspaceResult {
	switch {
	case req.List != nil:
		return m.listResult()
	case req.Select != nil:
		if _, err := m.selectSync(req.Select.ID); err != nil {
			res := m.listResult()
			res.Notice = err.Error()
			return res
		}
	case req.New != nil:
		if _, _, err := m.reg.Create(req.New.Root, req.New.Label); err != nil {
			res := m.listResult()
			res.Notice = err.Error()
			return res
		}
		list, _ := m.reg.List()
		// Switch to the workspace of that root (the freshest list entry).
		want := ""
		if resolved, err := daemon.CanonicalRoot(req.New.Root); err == nil {
			for _, w := range list {
				if w.File.Root == resolved {
					want = w.File.StableID
				}
			}
		}
		if want != "" {
			if _, err := m.selectSync(want); err != nil {
				res := m.listResult()
				res.Notice = err.Error()
				return res
			}
		}
	case req.Close != nil:
		if err := m.closeSync(req.Close.ID); err != nil {
			res := m.listResult()
			res.Notice = err.Error()
			return res
		}
		res := m.listResult()
		// The close reports exactly what it stopped through the same protocol answer.
		res.Notice = m.takeReport()
		return res
	case req.Rename != nil:
		if err := m.Rename(req.Rename.ID, req.Rename.Label); err != nil {
			res := m.listResult()
			res.Notice = err.Error()
			return res
		}
	default:
		return daemon.WorkspaceResult{Notice: "unknown workspace request"}
	}
	return m.listResult()
}

// listResult is the protocol answer of the current list.
func (m *workspaceManager) listResult() daemon.WorkspaceResult {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.listResultLocked()
}

// selectSync starts a switch and waits for the build; it is called from the daemon's
// connection goroutine (never the UI goroutine).
func (m *workspaceManager) selectSync(id string) (func() error, error) {
	var wait func() error
	var err error
	if !m.runOnUI(func() { wait, err = m.beginSelect(id) }) {
		return nil, errors.New("workspace: the daemon is not ready")
	}
	if err != nil {
		return nil, err
	}
	if wait != nil {
		if err := wait(); err != nil && !errors.Is(err, errWorkspaceStale) {
			return nil, err
		}
	}
	return nil, nil
}

// closeSync closes a workspace from the connection goroutine.
func (m *workspaceManager) closeSync(id string) error {
	return m.closeWorkspace(id)
}

// workspaceOpenTarget is one resolved client open-file request: an absolute (or
// client-cwd-resolved) path and its optional 1-based position.
type workspaceOpenTarget struct {
	path      string
	line, col int
}

// workspacePosition is a position saved in a workspace's layout.
type workspacePosition struct{ line, col int }

// openFiles opens a client's open-file request: each path is routed to the workspace that
// owns it. A materialized owner is switched to; a dormant owner is detected from its saved
// layout and materialized/switched asynchronously (the single-writer rule applies to
// dormant workspaces too). A path owned nowhere opens in the active workspace. Positions
// from the request survive to the editor; a dormant owner's saved position is used when
// the request has none and the file has to be opened fresh after materialization.
func (m *workspaceManager) openFiles(req daemon.OpenFile) {
	for _, target := range openTargetsFromRequest(req) {
		m.openTarget(target)
	}
}

// openTargetsFromRequest resolves the request's paths against the client's cwd (a client
// may attach from another directory) and merges the legacy Paths with the positional
// Files field. Files wins when both are present; legacy Paths still work unchanged.
func openTargetsFromRequest(req daemon.OpenFile) []workspaceOpenTarget {
	base := req.Cwd
	if base == "" {
		base, _ = os.Getwd()
	}
	var out []workspaceOpenTarget
	resolve := func(path string, line, col int) {
		if path == "" {
			return
		}
		if !filepath.IsAbs(path) {
			path = filepath.Join(base, path)
		}
		out = append(out, workspaceOpenTarget{path: filepath.Clean(path), line: line, col: col})
	}
	if len(req.Files) > 0 {
		for _, f := range req.Files {
			resolve(f.Path, f.Line, f.Col)
		}
		return out
	}
	for _, path := range req.Paths {
		resolve(path, 0, 0)
	}
	return out
}

// openTarget routes one resolved path to its owning workspace and posts the open at the
// requested position.
func (m *workspaceManager) openTarget(target workspaceOpenTarget) {
	canonical := canonicalWorkspaceFile(target.path)
	m.mu.Lock()
	ownerID := ""
	for id, s := range m.sessions {
		if sessionHasPath(s.rt, canonical) {
			ownerID = id
			break
		}
	}
	active := m.active
	m.mu.Unlock()
	saved := workspacePosition{}
	if ownerID == "" {
		if id, pos, ok := m.dormantOwner(canonical); ok {
			ownerID, saved = id, pos
		}
	}
	if ownerID != "" && ownerID != active {
		if _, err := m.selectSync(ownerID); err != nil {
			// The owner was detected but could not be materialized. Report and keep
			// the current workspace: opening the path in another workspace would
			// violate the single-writer rule and could conflict with the dormant
			// workspace's unsaved stash.
			m.notify(fmt.Errorf("open %s in its workspace: %w; the file was not opened", canonical, err))
			return
		}
	}
	line, col := target.line, target.col
	if line == 0 && ownerID != "" {
		// No request position: a dormant owner's restored caret is its saved one; if
		// the materialization could not restore the file, open it at the saved position.
		line, col = saved.line, saved.col
	}
	m.mu.Lock()
	host := m.host
	_, ok := m.sessions[m.active]
	m.mu.Unlock()
	if ok && host != nil {
		host.PostOpenFileAt(canonical, line, col)
	}
}

// dormantOwner finds the workspace that has canonical open according to its saved layout.
// It reads the layout from the registry file first and falls back to the per-workspace
// session file. A layout entry is ignored when the file is gone and it has no unsaved
// stash/overlay (stale layout), so a deleted file does not materialize a workspace for
// nothing.
func (m *workspaceManager) dormantOwner(canonical string) (string, workspacePosition, bool) {
	entries, _ := m.reg.List()
	for _, e := range entries {
		m.mu.Lock()
		_, materialized := m.sessions[e.File.StableID]
		m.mu.Unlock()
		if materialized {
			continue
		}
		layout := e.File.Layout
		if layout == nil {
			if dir, derr := m.reg.SessionDir(e.File.StableID); derr == nil {
				if sess, ok, err := session.Load(dir); err == nil && ok {
					layout = &sess
				}
			}
		}
		if layout == nil {
			continue
		}
		for _, f := range layout.Files {
			if f.Path == "" || canonicalWorkspaceFile(f.Path) != canonical {
				continue
			}
			if f.StashFile == "" && f.OverlayFile == "" {
				if _, err := os.Stat(canonical); err != nil {
					continue // stale layout entry: the file is gone and has no unsaved text
				}
			}
			return e.File.StableID, workspacePosition{line: f.Line, col: f.Col}, true
		}
	}
	return "", workspacePosition{}, false
}

// canonicalWorkspaceFile makes an absolute clean path for the open-path comparison.
func canonicalWorkspaceFile(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		return resolved
	}
	return abs
}

// sessionHasPath reports whether the runtime has the canonical path open in a tab.
func sessionHasPath(rt *runtime, canonical string) bool {
	if rt == nil || rt.ed == nil {
		return false
	}
	for _, tab := range rt.ed.Tabs() {
		if tab.Path == "" {
			continue
		}
		if canonicalWorkspaceFile(tab.Path) == canonical {
			return true
		}
	}
	return false
}

// closeAll saves and closes every materialized workspace, active first. It runs after the
// program ended; no UI callbacks are possible, so snapshots are taken here and the writes
// run in workers. It waits for every write before closing the runtimes, so a quit cannot
// drop the unsaved text of any workspace.
// closeAll is the compatibility form of the checked exit save: tests and deferred
// cleanup call it without an outcome. The daemon's exit path uses closeAllChecked, which
// returns the durability error; this wrapper only reports it on stderr. The one overall
// deadline of the checked path supersedes waitWrites' per-job timeout for the exit.
func (m *workspaceManager) closeAll() {
	if err := m.closeAllChecked(workspaceExitTimeout); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "gvim: warning: workspace save:", err)
	}
}

// runDaemonHost serves the daemon with the workspace layer (#19a): one tea program over
// the host model, the registry's foreground workspace materialized eagerly, and the
// workspace protocol/open-file requests routed through the manager.
func runDaemonHost(ctx context.Context, srv *daemon.Server, opts Options) (err error) {
	ws, err := newWorkspaceManager(ctx, opts)
	if err != nil {
		return err
	}
	if opts.PluginDaemon != nil {
		// Registered before the closeAll defer, so it runs AFTER the workspaces are closed:
		// the one shared plugin runtime is stopped once, at daemon exit (workspace models
		// skip pluginStop in shared mode).
		defer opts.PluginDaemon.Stop()
	}
	defer func() {
		// The exit save is part of the outcome: a capture write that failed, timed out or
		// could not be rescued makes the daemon exit non-zero instead of reporting a clean
		// quit after losing the captured workspace state.
		saveErr := ws.closeAllChecked(workspaceExitTimeout)
		if saveErr != nil {
			_, _ = fmt.Fprintln(os.Stderr, "gvim: error: workspace save:", saveErr)
		}
		err = errors.Join(err, saveErr)
	}()
	// The foreground remote workspace connects before the socket hosts, so the first
	// attach never waits on SSH and interactive prompts happen on the daemon terminal.
	if err := ws.ensureForegroundAuthority(ctx); err != nil {
		return err
	}
	srv.OnWorkspaceRequest(ws.handleRequest)
	srv.OnOpenFilesAt(ws.openFiles)
	// Bind the shared plugin host/runtime to the eagerly materialized foreground workspace
	// before the program starts (the runtime's loader reads its plugin sources through the
	// active workspace's OS on the first Init).
	ws.activateForegroundPlugins()
	host := tui.NewWorkspaceHost(ws.ForegroundModel())
	ws.setHost(host)
	go srv.Serve(ctx)
	runErr := tui.RunWorkspaceHost(ctx, host, srv, srv, srv.OnProgram)
	if runErr != nil {
		_, _ = fmt.Fprintln(os.Stderr, "gvim:", runErr)
	}
	return runErr
}
