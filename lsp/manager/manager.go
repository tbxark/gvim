// Package manager runs the language servers of a workspace. It is the session
// layer over the clients in package lsp: it starts processes, runs the initialize
// handshake, keeps the servers of one language and the universal servers, routes
// a feature request to the servers that can serve it, and tracks the documents
// open on each. It also holds the built-in server catalogue (Spec, Defaults,
// DefaultFor).
//
// # Multi-server routing
//
// A language may have several servers, and universal servers serve every
// language. A running server is a *Handle carrying its name, its feature filter
// (from only_features/except_features) and the capabilities its initialize result
// advertised; a universal handle serves every language. Merged features
// (diagnostics, completion, code actions, symbols) fan out to every eligible
// handle; exclusive features (hover, definition, implementation, references,
// format, rename, signature help, inlay hints, folding ranges, semantic tokens,
// document highlights) go to the first eligible handle in config order, and an
// empty answer from it is final. A handle whose initialize has not finished
// advertises nothing, so no request is routed to a server of unknown
// capabilities.
package manager

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"path"
	"slices"
	"strings"
	"sync"
	"time"

	"go.lsp.dev/protocol"

	"github.com/tbxark/gvim/lsp"
	"github.com/tbxark/gvim/trust"
)

// ErrNoServer is returned by Manager.Open for a language that has no server, and
// by the restart and stop calls for a server that is not configured.
var ErrNoServer = errors.New("lsp: no server configured for language")

// ErrNotOpen is returned by Manager.Change for a path that is not open.
var ErrNotOpen = errors.New("lsp: document is not open")

// ErrStopped is returned by Manager.Open for a language whose servers the user
// stopped or muted: the document is not opened until a manual restart. It is not
// a start failure, so the editor reports it as a notice and keeps its state.
var ErrStopped = errors.New("lsp: language server stopped")

// errServerGone is returned when the server a request was using was shut down,
// or replaced, while the request was running.
var errServerGone = errors.New("lsp: server stopped during the request")

// managerTimeout is the default bound on each step the Manager takes with a
// server: starting it, each handshake request, and each document notification.
// Every step gets its own deadline of this length.
const managerTimeout = 10 * time.Second

// ServerConfig says how to launch one language server. The process is Command[0]
// and its arguments are Command[1:] followed by Args.
type ServerConfig struct {
	Command []string
	Args    []string
	// Name is what messages call the server. Empty means its program.
	Name string
	// Install says how to get the server, for the message of a server that is not on PATH.
	Install string
	// LanguageID is the languageId that didOpen carries, when it differs from the language
	// the server is keyed by (the "tsx" server of TypeScript says "typescriptreact").
	LanguageID string
	// LanguageIDOverrides maps a file extension, without the dot, to the languageId
	// didOpen sends for it (Fresh's language_id_overrides).
	LanguageIDOverrides map[string]string
	// RootMarkers are the names (or globs) of the files that mark a project root. They are
	// for the caller that picks the workspace root of a file; the Manager does not read them.
	RootMarkers []string
	// Env holds extra environment variables for the process, laid over the parent's.
	Env map[string]string
	// InitializationOptions is the JSON handed to initialize as initializationOptions.
	InitializationOptions protocol.LSPAny
	// OnlyFeatures and ExceptFeatures build the server's feature filter. OnlyFeatures
	// wins when both are set. The names are Fresh's LspFeature names.
	OnlyFeatures   []string
	ExceptFeatures []string
	// AutoStart says whether the server starts when a document of its language opens.
	// A manual start (Start/Restart LSP Server) starts it anyway.
	AutoStart bool
	// Limits is the server's process_limits, applied as far as the platform can when
	// it starts; the client records what happened in its limits note.
	Limits lsp.ProcessLimits
}

// CommandLine returns the program and arguments the server runs with.
func (c ServerConfig) CommandLine() []string {
	out := make([]string, 0, len(c.Command)+len(c.Args))
	out = append(out, c.Command...)
	out = append(out, c.Args...)
	return out
}

// DisplayName is what messages and the status popup call the server.
func (c ServerConfig) DisplayName() string {
	if c.Name != "" {
		return c.Name
	}
	if len(c.Command) > 0 {
		return path.Base(c.Command[0])
	}
	return "language server"
}

// LanguageIDFor returns the languageId didOpen sends for the file at path: a
// language_id_overrides entry for its extension wins, then the server's own
// languageId, then the detected language.
func (c ServerConfig) LanguageIDFor(file, language string) string {
	if c.LanguageIDOverrides != nil {
		if ext := strings.TrimPrefix(path.Ext(file), "."); ext != "" {
			if id, ok := c.LanguageIDOverrides[ext]; ok && id != "" {
				return id
			}
		}
	}
	if c.LanguageID != "" {
		return c.LanguageID
	}
	return language
}

// ServerState is where a configured server stands.
type ServerState int

// The states of a configured server.
const (
	ServerStopped  ServerState = iota // not running: never started, stopped, or turned off
	ServerStarting                    // a start is in flight
	ServerRunning                     // the handshake finished and the server is live
	ServerError                       // the last start failed; nothing runs
)

// ServerStatus is one configured server as the editor shows it: the status popup
// lists these to offer a restart or a stop.
type ServerStatus struct {
	// Language is the language key the server is configured under, or "" for a
	// universal server.
	Language  string
	Name      string
	Command   []string
	Install   string
	Universal bool
	AutoStart bool
	State     ServerState
	// Error is why the last start failed, when State is ServerError.
	Error string
	// Limits is what happened to the configured process_limits of the running server:
	// the methods that applied, or why nothing could, from the client's limits note.
	Limits string
}

// Handle is one running language server. It is safe for concurrent use: the
// manager guards the document set and the client is safe by itself.
type Handle struct {
	// key names the configured server: its language (empty for universal) and name.
	key       string
	id        uint64
	language  string
	universal bool
	cfg       ServerConfig
	client    *lsp.Client
	// program is the absolute executable passed to lsp.Start, or "" when a bare
	// command was left to the child PATH. launchEnv is the extra server environment.
	program   string
	launchEnv []string
	filter    FeatureFilter
	// docs holds the paths this server has been sent didOpen for.
	docs map[string]bool
}

// Name is the server's display name.
func (h *Handle) Name() string { return h.cfg.DisplayName() }

// Client returns the connection to the server.
func (h *Handle) Client() *lsp.Client { return h.client }

// Universal reports whether the server serves every language.
func (h *Handle) Universal() bool { return h.universal }

// Language returns the language key the server is configured under; empty for a
// universal server.
func (h *Handle) Language() string { return h.language }

// Can reports whether the server takes feature: its filter allows it and its
// initialize result advertises it. A server whose initialize has not finished
// takes nothing.
func (h *Handle) Can(feature Feature) bool {
	return h.filter.Allows(feature) && Capability(h.client.Capabilities(), feature)
}

// Allows reports whether the server's only/except filter lets feature through.
func (h *Handle) Allows(feature Feature) bool { return h.filter.Allows(feature) }

// Command is the program and arguments the server runs with.
func (h *Handle) Command() []string { return h.cfg.CommandLine() }

// Program is the absolute executable the manager passed to lsp.Start, or "" when the
// server's bare command was resolved by the child's own PATH lookup.
func (h *Handle) Program() string { return h.program }

// LaunchEnv is the extra environment the server was launched with (project overlay plus
// its configured env), for a resolver that must run the same binary/environment.
func (h *Handle) LaunchEnv() []string { return slices.Clone(h.launchEnv) }

// Install is how to install the server, when it is missing.
func (h *Handle) Install() string { return h.cfg.Install }

// Capabilities returns the server's capability snapshot: the initialize result
// plus the providers it registered dynamically.
func (h *Handle) Capabilities() lsp.Capabilities { return h.client.Capabilities() }

// live reports whether the connection has not ended.
func (h *Handle) live() bool {
	select {
	case <-h.client.Done():
		return false
	default:
		return true
	}
}

// configured reports whether a language has any configured server, running or not.
func (m *Manager) configured(language string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.servers[language]) > 0
}

// serverKey names one configured server.
func serverKey(language, name string) string { return language + "\x00" + name }

// openDoc is a document the Manager has sent didOpen for.
type openDoc struct {
	language string
	uri      string
	version  int32
}

// resumeSpec is a server that has run in this manager: enough to start it again after a
// reconnect. It is recorded on a successful start and removed by an explicit user stop or
// a mute.
type resumeSpec struct {
	language  string
	cfg       ServerConfig
	universal bool
}

// serverStart is a start of one server that is still running. A caller that needs
// that server while it starts waits on done and shares the result.
type serverStart struct {
	done chan struct{}
	h    *Handle
	err  error
}

// Manager runs the language servers of one workspace root. A server starts on the
// first Open of a document of its language (or the first document at all, for a
// universal server) and keeps running until Shutdown. Methods are safe for
// concurrent use. They can block on server I/O, so call them from a command, not
// from a UI update.
//
// Every step with a server has a deadline. A write still blocked at its deadline
// closes that server. A server whose connection has ended is replaced the next
// time its language opens a document.
type Manager struct {
	rootURI string
	// rootDir is the workspace directory ("" for none), kept for the spawn gate's cwd.
	rootDir string
	// gate is the workspace trust spawn gate (nil allows); it runs before every server
	// start, with the argv, the workspace root and the child's effective PATH.
	gate func(ctx context.Context, argv []string, cwd, pathValue string) error
	// autoStartGate is the live trust gate for automatic starts (Open/ensure). It is not
	// used by manual StartLanguage/Restart and never stops a running server.
	autoStartGate func() bool
	servers       map[string][]ServerConfig // by language ID
	universal     []ServerConfig
	start         func(context.Context, lsp.Config) (*lsp.Client, error)
	// applyEdit answers a server's workspace/applyEdit request. It gets the
	// language of the requesting server ("" for a universal one). It is read and
	// set under mu.
	applyEdit func(language string, params protocol.ApplyWorkspaceEditParams) bool
	// timeout bounds each step. NewManager sets it to managerTimeout.
	timeout time.Duration
	// spawnEnv, when set, is the active project environment's set-only overlay for
	// server processes (see SetSpawnEnv).
	spawnEnv func() []string

	// sendMu orders the calls that change server state: Open, Change and Close. It
	// is held only across the write of each message, so the didOpen, didChange and
	// didClose of one document reach a server in the order their versions were
	// assigned. A server start does not hold it, so a slow start never blocks Change
	// or Close. Shutdown does not take it, so a stalled write never blocks Shutdown.
	sendMu sync.Mutex

	// mu guards the fields below. It is held only to read or update them, never
	// across a write or a server start.
	mu       sync.Mutex
	handles  []*Handle
	starting map[string]*serverStart
	docs     map[string]*openDoc
	// disabled holds the languages muted with SetLanguageEnabled: no server of
	// theirs starts on its own.
	disabled map[string]bool
	// forced holds the languages started by hand: every configured server starts,
	// whatever auto_start says.
	forced map[string]bool
	// stopped holds the servers the user stopped by hand, so an Open does not start
	// them again until a manual restart.
	stopped map[string]bool
	// resume records the servers that have been started successfully, so a reconnect can
	// restart the ones that were running before a carrier drop. User stops and mutes
	// delete their entries; a crash or a dropped carrier keeps them (that is the case a
	// reconnect recovers).
	resume map[string]resumeSpec
	// failures holds the message of the last failed start, by server key.
	failures map[string]string
	// epoch counts Shutdown calls, so that a server started across one is not kept.
	epoch  uint64
	nextID uint64
}

// NewManager returns a Manager for the workspace at root, an absolute directory
// path, or "" for none. It starts servers from servers, which maps a language ID
// to its servers in config order, and universal to the servers that serve every
// language. Nothing is started until Open or a manual start.
func NewManager(root string, servers map[string][]ServerConfig, universal []ServerConfig) *Manager {
	rootURI := ""
	if root != "" {
		rootURI = lsp.PathURI(root)
	}
	if servers == nil {
		servers = make(map[string][]ServerConfig)
	}
	return &Manager{
		rootURI:   rootURI,
		rootDir:   root,
		servers:   servers,
		universal: slices.Clone(universal),
		start:     lsp.Start,
		timeout:   managerTimeout,
		starting:  make(map[string]*serverStart),
		resume:    make(map[string]resumeSpec),
		docs:      make(map[string]*openDoc),
		disabled:  make(map[string]bool),
		forced:    make(map[string]bool),
		stopped:   make(map[string]bool),
		failures:  make(map[string]string),
	}
}

// SetSpawnEnv installs the active project environment's set-only overlay. It is
// read at every server launch, so an activation applies to the next start.
func (m *Manager) SetSpawnEnv(overlay func() []string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.spawnEnv = overlay
}

// SetSpawner replaces how a server process is started, for a backend that cannot start
// a local exec.Command (a remote authority runs the server over SSH through its
// LongRunning surface; see docs/authority.md). A nil spawner restores lsp.Start. The
// spawn gate still runs before it in launch(), so a remote server is gated like a local
// one. Call it before the servers start; the app wires it once for a remote workspace.
func (m *Manager) SetSpawner(start func(context.Context, lsp.Config) (*lsp.Client, error)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if start == nil {
		start = lsp.Start
	}
	m.start = start
}

// SetGate installs the workspace trust spawn gate. It is read at every server start,
// so a trust decision applies to the next one. A nil gate allows every start.
func (m *Manager) SetGate(gate func(ctx context.Context, argv []string, cwd, pathValue string) error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.gate = gate
}

// SetAutoStartGate installs the live auto-start gate (normally the live workspace trust
// check). While it returns false, Open/ensureServers skips AutoStart servers and universal
// starts; manual StartLanguage/Restart still goes through the spawn gate. It never stops a
// running server.
func (m *Manager) SetAutoStartGate(gate func() bool) {
	m.mu.Lock()
	m.autoStartGate = gate
	m.mu.Unlock()
}

// autoStartAllowed reads the live gate without holding m.mu across it.
func (m *Manager) autoStartAllowed() bool {
	m.mu.Lock()
	gate := m.autoStartGate
	m.mu.Unlock()
	return gate == nil || gate()
}

// gateSpawn runs the trust gate for one server start. The context is the launch's
// bounded start context; a denial becomes an *lsp.StartError so the existing
// failed-to-start surface shows the trust reason.
func (m *Manager) gateSpawn(ctx context.Context, language string, cfg ServerConfig, command []string, pathValue string) error {
	m.mu.Lock()
	gate, rootDir := m.gate, m.rootDir
	m.mu.Unlock()
	if gate == nil {
		return nil
	}
	if err := gate(ctx, command, rootDir, pathValue); err != nil {
		return &lsp.StartError{
			Language: language,
			Name:     cfg.DisplayName(),
			Command:  strings.Join(command, " "),
			Err:      err,
		}
	}
	return nil
}

// PreflightServers checks the workspace spawn gate for every config of language without
// starting anything. ReplaceServers calls it before stopping a running language's servers,
// so a trust revocation (or a project executable) cannot make an explicit restart destroy a
// live server and only then fail the new start. A nil gate allows every start.
func (m *Manager) PreflightServers(language string, configs []ServerConfig) error {
	for _, cfg := range configs {
		if err := m.preflightServer(language, cfg); err != nil {
			return err
		}
	}
	return nil
}

// preflightServer mirrors the gate part of launch: same command/env/PATH and executable
// resolution, no process start and no lock held across the gate.
func (m *Manager) preflightServer(language string, cfg ServerConfig) error {
	command := cfg.CommandLine()
	serverEnv := m.spawnEnvList(envList(cfg.Env))
	pathValue := effectivePath(append(os.Environ(), serverEnv...))
	m.mu.Lock()
	rootDir := m.rootDir
	m.mu.Unlock()
	program := ""
	if len(command) > 0 && !strings.ContainsAny(command[0], "/\\") {
		if resolved, ok := trust.ResolveSpawnPath(rootDir, command[0], pathValue); ok {
			program = resolved
		}
	}
	gateArgv := command
	if program != "" {
		gateArgv = append([]string{program}, command[1:]...)
	}
	ctx, cancel := context.WithTimeout(context.Background(), m.timeout)
	defer cancel()
	return m.gateSpawn(ctx, language, cfg, gateArgv, pathValue)
}

// effectivePath returns the PATH the child resolves bare names through: the first
// PATH entry of its environment (the process's own comes first, then the overlay), as
// the C library and Go runtime read environment variables.
func effectivePath(entries []string) string {
	for _, kv := range entries {
		if k, v, ok := strings.Cut(kv, "="); ok && k == "PATH" {
			return v
		}
	}
	return ""
}

// SetApplyEdit installs the handler for a server's workspace/applyEdit request.
// The manager hands it to every client it starts; the handler answers on the
// client's read goroutine and must only enqueue the edit. Set it before the first
// Open.
func (m *Manager) SetApplyEdit(apply func(language string, params protocol.ApplyWorkspaceEditParams) bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.applyEdit = apply
}

// applyEditHandler returns the handler for a server of language. m.mu must not be
// held.
func (m *Manager) applyEditHandler(language string) func(protocol.ApplyWorkspaceEditParams) bool {
	m.mu.Lock()
	apply := m.applyEdit
	m.mu.Unlock()
	if apply == nil {
		return nil
	}
	return func(params protocol.ApplyWorkspaceEditParams) bool { return apply(language, params) }
}

// Open sends didOpen for the document at path with its full text, starting the
// servers of languageID that should run first. It returns ErrNoServer when
// languageID has no ServerConfig and no universal server serves it. Opening a path
// that is already open closes the old document first.
//
// When several servers serve the language, every one of them is sent didOpen.
// A server that fails to start is reported in the Manager's Servers state and
// left out; Open fails only when no server could open the document.
func (m *Manager) Open(path, languageID, text string) error {
	// The starts run without the send lock: a start can take seconds, and a change
	// or close of a document on another, running server must not wait for it.
	hs, errs := m.ensureServers(languageID, false)
	hs = append(hs, m.ensureUniversal()...)
	if len(hs) == 0 {
		if len(errs) > 0 {
			return errors.Join(errs...)
		}
		if m.configured(languageID) {
			return fmt.Errorf("%w: %s (run Start/Restart LSP Server)", ErrStopped, languageID)
		}
		return fmt.Errorf("%w: %q", ErrNoServer, languageID)
	}

	m.sendMu.Lock()
	defer m.sendMu.Unlock()
	if err := m.closeDoc(path); err != nil {
		return err
	}

	docURI := lsp.PathURI(path)
	var openErrs []error
	opened := 0
	for _, h := range hs {
		id := h.cfg.LanguageIDFor(path, languageID)
		err := m.step(context.Background(), h.client, func(ctx context.Context) error {
			return h.client.DidOpen(ctx, docURI, id, 1, text)
		})
		if err != nil {
			openErrs = append(openErrs, fmt.Errorf("%s: %w", h.Name(), err))
			continue
		}
		m.mu.Lock()
		if h.docs == nil {
			h.docs = make(map[string]bool)
		}
		h.docs[path] = true
		m.mu.Unlock()
		opened++
	}
	if opened == 0 {
		return errors.Join(append(errs, openErrs...)...)
	}

	m.mu.Lock()
	m.docs[path] = &openDoc{language: languageID, uri: docURI, version: 1}
	m.mu.Unlock()
	return nil
}

// Change sends didChange with the full new text of the open document at path to
// every server that has it open. It returns ErrNotOpen when path is not open,
// which includes a path whose servers have all exited.
func (m *Manager) Change(path, text string) error {
	m.sendMu.Lock()
	defer m.sendMu.Unlock()

	m.mu.Lock()
	doc, ok := m.docs[path]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("%w: %s", ErrNotOpen, path)
	}
	hs := m.openHandlesLocked(path)
	if len(hs) == 0 {
		delete(m.docs, path)
		m.mu.Unlock()
		return fmt.Errorf("%w: %s: its servers exited", ErrNotOpen, path)
	}
	doc.version++
	version := doc.version
	m.mu.Unlock()

	var errs []error
	for _, h := range hs {
		err := m.step(context.Background(), h.client, func(ctx context.Context) error {
			return h.client.DidChange(ctx, doc.uri, version, text)
		})
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", h.Name(), err))
		}
	}
	return errors.Join(errs...)
}

// Close sends didClose for the document at path to every server that has it open,
// and forgets it. Closing a path that is not open does nothing. The servers keep
// running; see Shutdown.
func (m *Manager) Close(path string) error {
	m.sendMu.Lock()
	defer m.sendMu.Unlock()
	return m.closeDoc(path)
}

// closeDoc is Close without the send lock: Close and Open call it with sendMu
// already held.
func (m *Manager) closeDoc(path string) error {
	m.mu.Lock()
	doc, ok := m.docs[path]
	if !ok {
		m.mu.Unlock()
		return nil
	}
	delete(m.docs, path)
	hs := m.openHandlesLocked(path)
	m.mu.Unlock()

	var errs []error
	for _, h := range hs {
		err := m.step(context.Background(), h.client, func(ctx context.Context) error {
			return h.client.DidClose(ctx, doc.uri)
		})
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", h.Name(), err))
		}
		m.mu.Lock()
		delete(h.docs, path)
		m.mu.Unlock()
	}
	return errors.Join(errs...)
}

// ClientFor returns a client whose server holds the open document at path. It
// returns false when path is not open, or when no server for it is running.
// It is the single-server view the plugin request uses; the request reaches the
// first server of the document's language, then a universal server.
func (m *Manager) ClientFor(path string) (*lsp.Client, bool) {
	h, ok := m.HandleForPath(path)
	if !ok {
		return nil, false
	}
	return h.client, true
}

// HandleForPath returns the first live handle that has the document at path open:
// a server of its language first, then a universal server.
func (m *Manager) HandleForPath(path string) (*Handle, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	doc, ok := m.docs[path]
	if !ok {
		return nil, false
	}
	if h := m.firstOpenHandleLocked(path, doc.language, false); h != nil {
		return h, true
	}
	if h := m.firstOpenHandleLocked(path, doc.language, true); h != nil {
		return h, true
	}
	return nil, false
}

// DocumentVersion returns the version the Manager last sent for the open document
// at path, and whether the document is open. A TextDocumentEdit carries a version
// the server computed the edit against; the editor compares it with this one to
// reject a stale edit.
func (m *Manager) DocumentVersion(path string) (int32, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	doc, ok := m.docs[path]
	if !ok {
		return 0, false
	}
	return doc.version, true
}

// Servers returns every configured server, language-specific ones in language
// order and universal ones last, with the state it is in. It starts nothing and
// reads no file system, so the status popup can call it from a command cheaply.
func (m *Manager) Servers() []ServerStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pruneLocked()

	var out []ServerStatus
	for _, language := range slices.Sorted(maps.Keys(m.servers)) {
		for _, cfg := range m.servers[language] {
			out = append(out, m.statusLocked(language, cfg))
		}
	}
	for _, cfg := range m.universal {
		out = append(out, m.statusLocked("", cfg))
	}
	return out
}

// statusLocked describes one configured server. m.mu must be held.
func (m *Manager) statusLocked(language string, cfg ServerConfig) ServerStatus {
	key := serverKey(language, cfg.DisplayName())
	st := ServerStatus{
		Language:  language,
		Name:      cfg.DisplayName(),
		Command:   cfg.CommandLine(),
		Install:   cfg.Install,
		Universal: language == "",
		AutoStart: cfg.AutoStart,
	}
	switch {
	case m.handleForKeyLocked(key) != nil:
		st.State = ServerRunning
		if h := m.handleForKeyLocked(key); h != nil && h.client != nil {
			st.Limits = h.client.LimitsNote()
		}
	case m.starting[key] != nil:
		st.State = ServerStarting
	case m.failures[key] != "":
		st.State, st.Error = ServerError, m.failures[key]
	case m.disabled[language] || m.stopped[key]:
		st.State = ServerStopped
	}
	return st
}

// RestartServer stops the named server and starts it again, whatever auto_start
// or the mute says: a manual start overrides the switches, as in Fresh. An empty
// name means the first configured server of the language. A universal server is
// named with an empty language.
func (m *Manager) RestartServer(language, name string) error {
	language = strings.TrimSpace(language)
	cfg, universal, ok := m.findConfig(language, name)
	if !ok {
		return fmt.Errorf("%w: %q", ErrNoServer, language)
	}
	// Preflight the trust gate before stopping the running server: a revoked trust must
	// not turn an explicit restart into a stop-then-fail.
	if err := m.PreflightServers(language, []ServerConfig{cfg}); err != nil {
		return err
	}
	key := serverKey(language, cfg.DisplayName())
	m.stopKey(key)

	m.mu.Lock()
	delete(m.stopped, key)
	delete(m.failures, key)
	if !universal {
		m.forced[language] = true
		delete(m.disabled, language)
	}
	epoch := m.epoch
	m.mu.Unlock()

	_, err := m.startServer(key, language, cfg, universal, epoch, false)
	return err
}

// StopServer stops the named running server. It is not started again on the next
// Open until a manual restart. An empty name means the first configured server of
// the language.
func (m *Manager) StopServer(language, name string) error {
	language = strings.TrimSpace(language)
	cfg, _, ok := m.findConfig(language, name)
	if !ok {
		return fmt.Errorf("%w: %q", ErrNoServer, language)
	}
	key := serverKey(language, cfg.DisplayName())
	m.stopKey(key)
	m.mu.Lock()
	m.stopped[key] = true
	delete(m.resume, key)
	m.mu.Unlock()
	return nil
}

// SetLanguageEnabled mutes or unmutes a language. A muted language starts no
// server of its own, and the servers it has are stopped; a manual restart still
// starts one. Universal servers are not affected. Muting is per workspace root
// and lasts until the Manager is shut down.
func (m *Manager) SetLanguageEnabled(language string, enabled bool) {
	m.mu.Lock()
	if enabled {
		delete(m.disabled, language)
		m.mu.Unlock()
		return
	}
	m.disabled[language] = true
	// Muting overrides an earlier manual start, which would otherwise keep
	// starting the language's servers.
	delete(m.forced, language)
	var keys []string
	for _, h := range m.handles {
		if h.language == language && !h.universal {
			keys = append(keys, h.key)
		}
	}
	m.mu.Unlock()
	for _, key := range keys {
		m.stopKey(key)
		m.mu.Lock()
		delete(m.resume, key)
		m.mu.Unlock()
	}
}

// ClearForced drops every manual-start override. It is used when the live trust level
// changes (a Trusted manual start must not survive a revoke as an automatic=false bypass of
// the auto-start gate). It never stops a running server.
func (m *Manager) ClearForced() {
	m.mu.Lock()
	m.forced = make(map[string]bool)
	m.mu.Unlock()
}

// ClearForcedLanguage drops one manual-start override. It is used when a trust change
// cleared the port's forces while an explicit Start/Restart was between its snapshot and
// its manager call and re-installed the force; only the language that operation owns is
// dropped, and running servers are not stopped.
func (m *Manager) ClearForcedLanguage(language string) {
	m.mu.Lock()
	delete(m.forced, language)
	m.mu.Unlock()
}

// ForceLanguage marks language as started by hand: every configured server of it
// starts at the next Open, whatever auto_start or the mute says. It starts nothing
// now.
func (m *Manager) ForceLanguage(language string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.forced[language] = true
	delete(m.disabled, language)
}

// StartLanguage starts every configured server for language, whatever auto_start
// or the mute says. It is the manual start of a whole language.
func (m *Manager) StartLanguage(language string) error {
	m.ForceLanguage(language)
	// manual=true keeps an explicit start explicit even if a concurrent trust change
	// clears the forced flag before the starts run: the servers still go through the
	// spawn gate, so the trust refusal remains possible but the manual action is not
	// silently reduced to a no-op.
	_, errs := m.ensureServers(language, true)
	return errors.Join(errs...)
}

// Handles returns the live handles that serve language: the servers of the
// language first, then the universal servers, in handle order.
func (m *Manager) Handles(language string) []*Handle {
	return m.handlesForLanguage(language)
}

// AllHandles returns every live handle, whatever language it serves. It is the
// workspace-wide view the diagnostics panel uses.
func (m *Manager) AllHandles() []*Handle {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pruneLocked()
	return slices.Clone(m.handles)
}

// UpdateServerPlan replaces the plan used for future automatic starts without stopping
// running servers or changing explicit user mutes and starts.
func (m *Manager) UpdateServerPlan(servers map[string][]ServerConfig, universal []ServerConfig) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.servers = make(map[string][]ServerConfig, len(servers))
	for lang, configs := range servers {
		m.servers[lang] = slices.Clone(configs)
	}
	m.universal = slices.Clone(universal)
}

// CurrentServers returns a copy of the configured servers for language.
func (m *Manager) CurrentServers(language string) []ServerConfig {
	m.mu.Lock()
	defer m.mu.Unlock()
	return slices.Clone(m.servers[language])
}

// ReplaceServers replaces the configured servers of language and starts them as a manual
// start. It is for an explicit Start/Restart after a completed per-root selection (Deno
// auto-selection): it stops only the running servers of that language (never another
// language or a universal server), waits for any in-flight start of the language so a late
// old-config handle cannot survive, clears old stopped/failure/resume bookkeeping, then
// starts the new configs. It is not called from automatic paths.
func (m *Manager) ReplaceServers(language string, configs []ServerConfig) error {
	language = strings.TrimSpace(language)
	if language == "" {
		return fmt.Errorf("%w: empty language", ErrNoServer)
	}
	configs = slices.Clone(configs)
	if err := m.PreflightServers(language, configs); err != nil {
		return err
	}
	m.mu.Lock()
	m.servers[language] = configs
	m.forced[language] = true
	delete(m.disabled, language)
	keys := make(map[string]bool)
	for _, h := range m.handles {
		if h.language == language && !h.universal {
			keys[h.key] = true
		}
	}
	var waiting []chan struct{}
	for key, s := range m.starting {
		if strings.HasPrefix(key, language+"\x00") {
			keys[key] = true
			waiting = append(waiting, s.done)
		}
	}
	m.mu.Unlock()
	for _, done := range waiting {
		<-done
	}
	m.mu.Lock()
	for key := range keys {
		delete(m.stopped, key)
		delete(m.failures, key)
		delete(m.resume, key)
	}
	m.mu.Unlock()
	for key := range keys {
		m.stopKey(key)
	}
	return m.StartLanguage(language)
}

// findConfig returns the configured server language/name names. An empty name
// takes the first configured server of the language.
func (m *Manager) findConfig(language, name string) (cfg ServerConfig, universal bool, ok bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	configs := m.servers[language]
	found := false
	for _, c := range configs {
		if name == "" || c.DisplayName() == name {
			cfg, found = c, true
			break
		}
	}
	if found {
		return cfg, false, true
	}
	if language == "" {
		for _, c := range m.universal {
			if name == "" || c.DisplayName() == name {
				return c, true, true
			}
		}
	}
	return ServerConfig{}, false, false
}

// ensureServers returns the running handles of language, starting the servers
// that should start now. The second result holds the failures of the starts.
func (m *Manager) ensureServers(language string, manual bool) ([]*Handle, []error) {
	m.mu.Lock()
	configs := slices.Clone(m.servers[language])
	disabled := m.disabled[language]
	m.mu.Unlock()
	auto := m.autoStartAllowed()

	var hs []*Handle
	var errs []error
	for _, cfg := range configs {
		key := serverKey(language, cfg.DisplayName())
		m.mu.Lock()
		if h := m.handleForKeyLocked(key); h != nil {
			m.mu.Unlock()
			hs = append(hs, h)
			continue
		}
		stopped := m.stopped[key]
		forced := m.forced[language]
		epoch := m.epoch
		m.mu.Unlock()
		if stopped {
			continue
		}
		if !manual && !forced && (!auto || disabled || !cfg.AutoStart) {
			continue
		}
		// Forced only overrides the auto_start/disabled switches: an automatic start never
		// bypasses the live auto-start gate. Explicit StartLanguage/Restart passes manual and
		// remains a manual, spawn-gated start.
		h, err := m.startServer(key, language, cfg, false, epoch, !manual)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		hs = append(hs, h)
	}
	return hs, errs
}

// ensureUniversal returns the running universal handles, starting the servers
// that should start now. A universal start failure is reported in the Servers
// state; it does not keep a document of a language with its own server from
// opening.
func (m *Manager) ensureUniversal() []*Handle {
	m.mu.Lock()
	configs := slices.Clone(m.universal)
	epoch := m.epoch
	m.mu.Unlock()
	auto := m.autoStartAllowed()

	var hs []*Handle
	for _, cfg := range configs {
		key := serverKey("", cfg.DisplayName())
		m.mu.Lock()
		if h := m.handleForKeyLocked(key); h != nil {
			m.mu.Unlock()
			hs = append(hs, h)
			continue
		}
		stopped := m.stopped[key]
		m.mu.Unlock()
		if stopped || !auto || !cfg.AutoStart {
			continue
		}
		h, err := m.startServer(key, "", cfg, true, epoch, true)
		if err != nil {
			continue
		}
		hs = append(hs, h)
	}
	return hs
}

// startServer starts one configured server, or returns the running one. A start
// already in flight for the same key is waited for and its result shared.
func (m *Manager) startServer(key, language string, cfg ServerConfig, universal bool, epoch uint64, automatic bool) (*Handle, error) {
	m.mu.Lock()
	if h := m.handleForKeyLocked(key); h != nil {
		m.mu.Unlock()
		return h, nil
	}
	if s, ok := m.starting[key]; ok {
		m.mu.Unlock()
		<-s.done
		return s.h, s.err
	}
	s := &serverStart{done: make(chan struct{})}
	m.starting[key] = s
	m.mu.Unlock()

	h, err := m.launch(key, language, cfg, universal, automatic)

	m.mu.Lock()
	if m.starting[key] == s {
		delete(m.starting, key)
	}
	if err == nil && m.epoch != epoch {
		err = errServerGone
	}
	if err == nil {
		m.nextID++
		h.id = m.nextID
		m.handles = append(m.handles, h)
		m.resume[key] = resumeSpec{language: language, cfg: cfg, universal: universal}
		delete(m.failures, key)
	} else if m.epoch == epoch {
		m.failures[key] = err.Error()
	}
	m.mu.Unlock()
	if err != nil {
		if h != nil && h.client != nil {
			_ = h.client.Close()
		}
		h = nil
	}
	s.h, s.err = h, err
	close(s.done)
	return h, err
}

// spawnEnvList appends the active environment overlay to a server's configured
// environment; a configured variable of the same name wins over the overlay. The
// overlay is set-only (activation unsets are not represented), matching the
// shell, terminal and plugin process paths.
func (m *Manager) spawnEnvList(configured []string) []string {
	m.mu.Lock()
	overlay := m.spawnEnv
	m.mu.Unlock()
	if overlay == nil {
		return configured
	}
	extra := overlay()
	if len(extra) == 0 {
		return configured
	}
	seen := make(map[string]bool, len(configured))
	for _, entry := range configured {
		name, _, _ := strings.Cut(entry, "=")
		seen[name] = true
	}
	out := append([]string(nil), configured...)
	for _, entry := range extra {
		name, _, _ := strings.Cut(entry, "=")
		if !seen[name] {
			out = append(out, entry)
		}
	}
	return out
}

// automaticStartRefused is the StartError for an automatic start whose live trust gate
// closed before the process could start. Manual starts do not use it.
func automaticStartRefused(language string, cfg ServerConfig, command []string) error {
	return &lsp.StartError{
		Language: language,
		Name:     cfg.DisplayName(),
		Command:  strings.Join(command, " "),
		Err:      errors.New("workspace trust: automatic language-server start is disabled"),
	}
}

// launch starts the process for cfg and runs the initialize handshake. A server
// that fails a step is closed.
func (m *Manager) launch(key, language string, cfg ServerConfig, universal bool, automatic bool) (*Handle, error) {
	command := cfg.CommandLine()
	serverEnv := m.spawnEnvList(envList(cfg.Env))
	pathValue := effectivePath(append(os.Environ(), serverEnv...))
	// Resolve a bare program here, through the same cwd and child PATH the gate uses, and
	// hand the absolute path to lsp.Start (Config.Program): the executed binary, the one
	// the gate judges and the one the test surface sees in Command stay consistent.
	m.mu.Lock()
	rootDir := m.rootDir
	start := m.start
	m.mu.Unlock()
	program := ""
	if len(command) > 0 && !strings.ContainsAny(command[0], "/\\") {
		if resolved, ok := trust.ResolveSpawnPath(rootDir, command[0], pathValue); ok {
			program = resolved
		}
	}
	gateArgv := command
	if program != "" {
		gateArgv = append([]string{program}, command[1:]...)
	}
	ctx, cancel := context.WithTimeout(context.Background(), m.timeout)
	// Automatic starts are re-checked live immediately before and after the spawn gate:
	// a trust revoke that lands between the earlier auto-start-gate check and this launch
	// must not let an automatic server start merely because Restricted permits a system
	// binary. The final check and the OS exec are still not atomic (an unavoidable window).
	if automatic {
		if !m.autoStartAllowed() {
			cancel()
			return nil, automaticStartRefused(language, cfg, command)
		}
	}
	if err := m.gateSpawn(ctx, language, cfg, gateArgv, pathValue); err != nil {
		cancel()
		return nil, err
	}
	if automatic && !m.autoStartAllowed() {
		cancel()
		return nil, automaticStartRefused(language, cfg, command)
	}
	c, err := start(ctx, lsp.Config{
		Command:               command,
		Program:               program,
		RootURI:               m.rootURI,
		Env:                   serverEnv,
		InitializationOptions: cfg.InitializationOptions,
		ApplyEdit:             m.applyEditHandler(language),
		Limits:                cfg.Limits,
	})
	cancel()
	if err != nil {
		var ni *lsp.NotInstalledError
		if errors.As(err, &ni) {
			ni.Language, ni.Install = language, cfg.Install
		}
		return nil, err
	}
	err = m.step(context.Background(), c, func(ctx context.Context) error {
		_, err := c.Initialize(ctx)
		return err
	})
	if err == nil {
		err = m.step(context.Background(), c, c.Initialized)
	}
	if err != nil {
		// The process is gone or stopped once it is closed, so what it wrote is all there.
		_ = c.Close()
		name := cfg.DisplayName()
		return nil, &lsp.StartError{Language: language, Name: name, Command: strings.Join(command, " "), Stderr: c.StderrTail(), Err: err}
	}
	return &Handle{
		key:       key,
		language:  language,
		universal: universal,
		cfg:       cfg,
		client:    c,
		program:   program,
		launchEnv: slices.Clone(serverEnv),
		filter:    ParseFeatureFilter(cfg.OnlyFeatures, cfg.ExceptFeatures),
		docs:      make(map[string]bool),
	}, nil
}

// envList turns the extra environment of a server into the KEY=VALUE list a
// process gets. The keys are sorted, so a start is deterministic.
func envList(env map[string]string) []string {
	if len(env) == 0 {
		return nil
	}
	out := make([]string, 0, len(env))
	for _, k := range slices.Sorted(maps.Keys(env)) {
		out = append(out, k+"="+env[k])
	}
	return out
}

// RestartAfterReconnect restarts the servers that were running before a carrier drop,
// for a remote workspace (docs/authority.md). It restarts only the servers that were live
// when the connection died, never servers the user stopped or a muted language, and it
// starts nothing when a live handle or a start in flight already holds the key — so a
// reconnect cannot create a duplicate server. The workspace spawn gate still runs for
// every restart (trust), and ctx is checked between servers: a cancelled context, or one
// that ends while a start is in flight, stops the loop before the next server (the flight
// already begun is the manager's own bounded start, not cancelable here). The count is the
// number of servers restarted.
func (m *Manager) RestartAfterReconnect(ctx context.Context) (int, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	m.mu.Lock()
	m.pruneLocked()
	type target struct {
		key, language string
		cfg           ServerConfig
		universal     bool
	}
	var targets []target
	for key, r := range m.resume {
		if m.stopped[key] || (!r.universal && m.disabled[r.language]) {
			continue
		}
		targets = append(targets, target{key: key, language: r.language, cfg: r.cfg, universal: r.universal})
	}
	epoch := m.epoch
	m.mu.Unlock()

	var errs []error
	started := 0
	for _, t := range targets {
		if err := ctx.Err(); err != nil {
			errs = append(errs, err)
			break
		}
		m.mu.Lock()
		live := m.handleForKeyLocked(t.key) != nil
		_, starting := m.starting[t.key]
		m.mu.Unlock()
		if live || starting {
			continue
		}
		if _, err := m.startServer(t.key, t.language, t.cfg, t.universal, epoch, true); err != nil {
			errs = append(errs, err)
			continue
		}
		started++
	}
	return started, errors.Join(errs...)
}

// stopKey stops the running server named key, if any, and removes it from the
// handle list. The documents it had open are forgotten, so the caller must open
// them again.
func (m *Manager) stopKey(key string) {
	m.mu.Lock()
	idx := slices.IndexFunc(m.handles, func(h *Handle) bool { return h.key == key })
	var h *Handle
	if idx >= 0 {
		h = m.handles[idx]
		m.handles = slices.Delete(m.handles, idx, idx+1)
	}
	m.mu.Unlock()
	if h == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), m.timeout)
	defer cancel()
	_ = m.shutDown(ctx, h.client)
}

// pruneLocked removes the handles whose connection has ended and the documents
// they held. m.mu must be held.
func (m *Manager) pruneLocked() {
	kept := m.handles[:0]
	for _, h := range m.handles {
		if h.live() {
			kept = append(kept, h)
			continue
		}
		_ = h.client.Close()
		for file := range h.docs {
			delete(h.docs, file)
		}
	}
	m.handles = kept
}

// handleForKeyLocked returns the live handle named key. m.mu must be held.
func (m *Manager) handleForKeyLocked(key string) *Handle {
	m.pruneLocked()
	for _, h := range m.handles {
		if h.key == key {
			return h
		}
	}
	return nil
}

// openHandlesLocked returns the live handles that have the document at path open.
// m.mu must be held.
func (m *Manager) openHandlesLocked(file string) []*Handle {
	m.pruneLocked()
	out := make([]*Handle, 0, len(m.handles))
	for _, h := range m.handles {
		if h.docs[file] {
			out = append(out, h)
		}
	}
	return out
}

// firstOpenHandleLocked returns the first live handle that has the document at
// path open, universal ones only when universal is set. m.mu must be held.
func (m *Manager) firstOpenHandleLocked(file, language string, universal bool) *Handle {
	m.pruneLocked()
	for _, h := range m.handles {
		if !h.docs[file] {
			continue
		}
		if universal != h.universal {
			continue
		}
		if !universal && h.language != language {
			continue
		}
		return h
	}
	return nil
}

// FirstFor returns the first live handle that serves feature for the open
// document at path: a server of the document's language first, then a universal
// server. An exclusive feature goes to this server, and an empty answer from it is
// final.
func (m *Manager) FirstFor(file string, feature Feature) (*Handle, bool) {
	m.mu.Lock()
	doc, ok := m.docs[file]
	if !ok {
		m.mu.Unlock()
		return nil, false
	}
	language := doc.language
	m.mu.Unlock()
	hs := m.handlesForLanguage(language)
	for _, h := range hs {
		if !h.universal && h.Can(feature) {
			return h, true
		}
	}
	for _, h := range hs {
		if h.universal && h.Can(feature) {
			return h, true
		}
	}
	return nil, false
}

// AllFor returns every live handle that serves feature for the open document at
// path, language-specific servers first and universal ones after. A merged feature
// asks each of them and combines the answers.
func (m *Manager) AllFor(file string, feature Feature) []*Handle {
	m.mu.Lock()
	doc, ok := m.docs[file]
	if !ok {
		m.mu.Unlock()
		return nil
	}
	language := doc.language
	m.mu.Unlock()
	hs := m.handlesForLanguage(language)
	out := make([]*Handle, 0, len(hs))
	for _, h := range hs {
		if !h.universal && h.Can(feature) {
			out = append(out, h)
		}
	}
	for _, h := range hs {
		if h.universal && h.Can(feature) {
			out = append(out, h)
		}
	}
	return out
}

// handlesForLanguage returns the live handles that serve language, in handle
// order. A universal handle serves every language.
func (m *Manager) handlesForLanguage(language string) []*Handle {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pruneLocked()
	out := make([]*Handle, 0, len(m.handles))
	for _, h := range m.handles {
		if h.universal || h.language == language {
			out = append(out, h)
		}
	}
	return out
}

// Running reports whether any live server serves language.
func (m *Manager) Running(language string) bool {
	return len(m.handlesForLanguage(language)) > 0
}

// Shutdown shuts down, exits and closes every running server, and forgets all
// open documents, the mutes and the manual starts. A later Open starts a server
// again. The returned error joins the errors from all of the servers. Each
// request is bounded by ctx and by the Manager's timeout, so Shutdown returns even
// when a server has stopped reading.
func (m *Manager) Shutdown(ctx context.Context) error {
	m.mu.Lock()
	m.epoch++
	handles := m.handles
	m.handles = nil
	m.starting = make(map[string]*serverStart)
	m.docs = make(map[string]*openDoc)
	m.disabled = make(map[string]bool)
	m.forced = make(map[string]bool)
	m.stopped = make(map[string]bool)
	m.failures = make(map[string]string)
	m.resume = make(map[string]resumeSpec)
	m.mu.Unlock()

	var errs []error
	for _, h := range handles {
		errs = append(errs, m.shutDown(ctx, h.client))
	}
	return errors.Join(errs...)
}

// shutDown sends shutdown and exit to c, then closes it.
func (m *Manager) shutDown(ctx context.Context, c *lsp.Client) error {
	err := m.step(ctx, c, c.Shutdown)
	exitErr := m.step(ctx, c, c.Exit)
	return errors.Join(err, exitErr, c.Close())
}

// step runs fn, one request or notification to c, under a deadline of m.timeout
// that also ends with parent. See call.
func (m *Manager) step(parent context.Context, c *lsp.Client, fn func(ctx context.Context) error) error {
	ctx, cancel := context.WithTimeout(parent, m.timeout)
	defer cancel()
	return call(ctx, c, func() error { return fn(ctx) })
}

// call runs fn, one request or notification to c, and returns its error. The
// transport write inside fn does not observe ctx once it has started, so call
// returns when ctx ends even if fn is still blocked in that write. Giving up
// closes c, which unblocks the write. A frame cut off in the middle cannot be
// resumed, so c is unusable after that.
func call(ctx context.Context, c *lsp.Client, fn func() error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() { done <- fn() }()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
	}
	select {
	case err := <-done:
		return err
	default:
	}
	_ = c.Close()
	return fmt.Errorf("lsp: %w", ctx.Err())
}
