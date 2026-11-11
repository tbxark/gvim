// Package lsphost is the language-server host of the app: it implements the TUI's
// LSPSession over the multi-server manager. It builds the effective server table
// from the built-in catalogue and the config, keeps one manager per workspace
// root, converts the protocol's UTF-16 positions to the editor's rune positions,
// queues server-initiated workspace edits for the root to apply, and lists the
// configured servers for the status popup.
package lsphost

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"

	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/config"
	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/internal/tui"
	"github.com/tbxark/gvim/lsp"
	"github.com/tbxark/gvim/lsp/manager"
	"github.com/tbxark/gvim/pluginapi"
)

// lspRequestTimeout bounds one request to a server. The Manager bounds its own
// steps (start, handshake, document notifications) separately. It is a variable so
// that a test can shorten it.
var lspRequestTimeout = 10 * time.Second

// lspStopTimeout bounds the shutdown of the servers, on lsp_stop and at exit.
const lspStopTimeout = 5 * time.Second

// ExternalLocationRequest describes the server that answered a location request. A
// provider must use Program/Command and Env as the authoritative launch context instead
// of hard-coding a PATH program: the same binary and environment that served the LSP
// request should print synthetic source. Program is the absolute executable the manager
// passed to lsp.Start; it is empty only when the manager left a bare command to the child's
// own PATH lookup (a documented residual for exact-binary identity).
type ExternalLocationRequest struct {
	URI      string
	Language string
	Root     string
	Program  string
	Command  []string
	Env      []string
}

// ExternalLocationOpener resolves a non-file URI (for example slang-synth://core/
// core.builtin) to generated local content. It must parse and validate the URI, use the
// workspace authority/trust gate for any process, bound its own work, and return an
// absolute regular file path. The app installs it; a port without one reports the
// external location instead of guessing a filesystem path.
type ExternalLocationOpener func(ctx context.Context, req ExternalLocationRequest) (path string, err error)

// externalLocationTimeout bounds one external URI resolution (the opener has its own
// smaller process timeout too).
const externalLocationTimeout = 10 * time.Second

// New returns the language-server port for the servers of the config, or
// nil when no server is configured, so that no LSP feature is on. It is the
// explicit-server form used by tests: each server is one that starts on its own.
// NewFor is the config form, with the built-in table and the universal servers.
func New(servers map[string]config.LSPServer) tui.LSPSession {
	if len(servers) == 0 {
		return nil
	}
	plan := lspPlan{
		servers:   make(map[string][]manager.ServerConfig),
		info:      make(map[string]tui.LSPServerInfo),
		auto:      make(map[string]bool),
		startable: make(map[string]bool),
	}
	for lang, s := range servers {
		cfg := serverConfig(s, manager.Spec{})
		cfg.AutoStart = true
		plan.servers[lang] = []manager.ServerConfig{cfg}
		plan.info[lang] = tui.LSPServerInfo{State: tui.LSPServerOn, Language: lang, Command: cfg.Command[0]}
		plan.auto[lang] = true
		plan.startable[lang] = true
	}
	return newPort(plan, true, nil)
}

// newPort builds the port for the plan. lspEnabled is the lsp_enabled master
// switch; it is false when the workspace is untrusted, and then no server starts
// on its own, though a manual start still runs one.
func newPort(plan lspPlan, lspEnabled bool, spawnEnv func() []string) *lspPort {
	p := &lspPort{
		spawnEnv:         spawnEnv,
		servers:          plan.servers,
		universal:        plan.universal,
		info:             plan.info,
		auto:             make(map[string]bool, len(plan.auto)),
		startable:        make(map[string]bool, len(plan.startable)),
		managers:         make(map[string]*manager.Manager),
		rootCache:        make(map[string]remoteRootCacheEntry),
		rootProviders:    make(map[string]func(language string) (manager.ServerConfig, bool)),
		docs:             make(map[string]lspDocRec),
		muted:            make(map[string]bool),
		forced:           make(map[string]bool),
		actionSeq:        make(map[string]uint64),
		sem:              make(map[string]semanticCache),
		pulled:           make(map[string]pulledDiagnostics),
		completionTokens: make(map[int]completionResolve),
	}
	for lang := range plan.auto {
		if lspEnabled {
			p.auto[lang] = true
		}
	}
	for lang := range plan.startable {
		p.startable[lang] = true
	}
	p.status = statusList(plan)
	return p
}

// lspPort is the tui.LSPSession over the language servers. It keeps one
// manager.Manager per workspace root, created on the first use of the root. It also
// keeps the text of each open document, which converts the positions of the
// protocol to rune columns. Its methods may block on a server, so the root calls
// them only from commands.
type lspPort struct {
	// spawnEnv is the active project environment's set-only overlay for servers
	// (nil when none is wired).
	spawnEnv func() []string
	// spawnGate is the workspace trust spawn gate for local server starts (nil when
	// none is wired); it is read at every start.
	spawnGate func(ctx context.Context, argv []string, cwd, pathValue string) error
	// autoStartGate is the live trust gate for automatic (Open) starts. Manual
	// StartLanguage/Restart bypasses it; it never stops a running server.
	autoStartGate func() bool
	baselineWait  func()
	// spawner, when set, starts the server instead of lsp.Start: a remote workspace
	// runs the server on the remote host through the authority's LongRunning surface
	// (docs/authority.md). The manager's spawn gate still runs before it.
	spawner func(context.Context, lsp.Config) (*lsp.Client, error)
	// remoteMarkers, when set, checks root markers through the workspace authority instead
	// of the local filesystem. It is used only for a remote workspace; nil keeps the local
	// serverRoot walk.
	remoteMarkers func(ctx context.Context, dir string, markers []string) (bool, error)
	// externalOpener resolves non-file LSP locations (slang-synth and similar) to
	// generated local content; nil means no provider.
	externalOpener ExternalLocationOpener
	// The server plan: SetConfig replaces it under mu, so a read after construction
	// takes mu too.
	servers   map[string][]manager.ServerConfig // by language ID
	universal []manager.ServerConfig
	info      map[string]tui.LSPServerInfo
	auto      map[string]bool       // languages with a server that starts on its own
	startable map[string]bool       // languages with any configured server
	status    []tui.LSPServerStatus // every configured server, for the popup

	mu sync.Mutex
	// selectionMu serializes per-root provider updates with an explicit Start/Restart
	// applying them, so an async selection cannot change between the snapshot and the
	// manager replacement.
	selectionMu sync.Mutex
	root        string
	managers    map[string]*manager.Manager // by root
	// rootProviders lets the app provide a per-root server override (Deno auto-selection)
	// without rebuilding the port. A provider is consulted only when the manager for its
	// exact root is created; it may return a replacement config for a language.
	rootProviders map[string]func(language string) (manager.ServerConfig, bool)
	// rootChanged is the app's project-root switch callback (Deno auto-selection re-probes
	// the new root and clears the old root's decision). It is called off SetRoot, out of the
	// lock; nil means nobody listens.
	rootChanged func(root string)
	docs        map[string]lspDocRec // by path: the documents the port has open
	// pending holds the workspace edits servers asked for, in arrival order. They
	// are converted to the port's form when the root drains them, because the
	// conversion reads the files the edits touch.
	pending []pendingEdit
	// muted holds the languages the user muted; forced holds the languages
	// started by hand, which start their servers whatever auto_start says.
	muted  map[string]bool
	forced map[string]bool
	// actionSeq and trustSeq version manual-start actions per language against trust
	// clears: ClearForced bumps trustSeq, a newer Start/Restart for a language bumps that
	// language's actionSeq, and an in-flight action only re-applies its forced intent when
	// it still owns the newest action for its own language and the trust version is
	// unchanged. A manual start under non-Trusted trust never persists forced (see
	// finishManual). Keeping this per-language means a newer action for another language
	// (or a root-wide Restart) cannot suppress this language's cleanup.
	actionSeq map[string]uint64
	trustSeq  uint64
	// sem caches the semantic-token data of open documents, and pulled the
	// server-pulled diagnostics. Both are keyed by path and invalidated when the
	// document closes, or on a restart.
	sem    map[string]semanticCache
	pulled map[string]pulledDiagnostics
	// completionTokens maps a completion item's ResolveToken to the request that
	// fills its lazy fields (completionItem/resolve, auto-imports). A token belongs
	// to the most recent completion request: a new request replaces the table.
	completionTokens map[int]completionResolve
	completionSeq    int
	// wsPull caches the workspace/diagnostic answer: the pull is throttled and
	// re-asked when the capabilities change or the set of open documents does.
	wsPull workspacePull
	// wsPulls tracks the workspace/diagnostic pulls in flight so a newer pull, or a
	// stop/restart of the servers, cancels the old ones (their $/cancelRequest goes
	// out through the client) instead of letting them answer into the cache.
	wsPulls pullSet
	// rootCache bounds repeated remote marker ReadDirs; keys are dir+markers and entries
	// expire after remoteRootCacheTTL. SetRoot/SetConfig clear it.
	rootCache      map[string]remoteRootCacheEntry
	rootCacheOrder []string
	// rootGen increments on SetRoot so an in-flight remote scan can abandon and fall
	// back after the workspace changed.
	rootGen uint64
	// notice carries a non-fatal remote root-marker degradation for the TUI status row.
	notice tui.LSPNotice
}

type remoteRootCacheEntry struct {
	ok bool
	at time.Time
}

const (
	remoteRootMaxDepth  = 32
	remoteRootScanTime  = 2 * time.Second
	remoteRootCacheTTL  = 30 * time.Second
	remoteRootCacheSize = 256
)

// completionResolve remembers what completionItem/resolve must send back: the
// document path, the server that offered the item and the item as it arrived.
type completionResolve struct {
	path   string
	handle *manager.Handle
	item   lsp.CompletionItem
}

// pendingEdit is one queued workspace/applyEdit request.
type pendingEdit struct {
	language string
	edit     protocol.WorkspaceEdit
	label    string
}

// lspDocRec is one open document: the root whose manager holds it, its language
// ID, the protocol version it is open at and the text the server was last sent. A
// record is replaced, never changed. lines is text split on newlines, made once
// when the text is set, so that the diagnostics poll and each request do not split
// the whole document again.
type lspDocRec struct {
	root, lang, text string
	version          int32
	lines            textLines
}

// statusList builds the popup's server list from the plan.
func statusList(plan lspPlan) []tui.LSPServerStatus {
	out := make([]tui.LSPServerStatus, 0)
	seen := make(map[string]bool)
	for _, lang := range slices.Sorted(maps.Keys(plan.servers)) {
		for _, cfg := range plan.servers[lang] {
			if !plan.auto[lang] && !plan.startable[lang] {
				continue
			}
			key := lang + "\x00" + cfg.DisplayName()
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, tui.LSPServerStatus{
				Language: lang, Name: cfg.DisplayName(), Command: strings.Join(cfg.CommandLine(), " "),
				Install: cfg.Install, AutoStart: cfg.AutoStart, State: tui.LSPServerStopped,
			})
		}
	}
	for _, cfg := range plan.universal {
		out = append(out, tui.LSPServerStatus{
			Name: cfg.DisplayName(), Command: strings.Join(cfg.CommandLine(), " "),
			Install: cfg.Install, Universal: true, AutoStart: cfg.AutoStart, State: tui.LSPServerStopped,
		})
	}
	return out
}

// SetRoot names the workspace root that new documents are opened under.
func (p *lspPort) SetRoot(root string) {
	p.mu.Lock()
	p.root = root
	p.rootGen++
	p.rootCache = make(map[string]remoteRootCacheEntry)
	p.rootCacheOrder = nil
	changed := p.rootChanged
	p.mu.Unlock()
	if changed != nil {
		changed(root)
	}
}

// SetRootChangeListener installs the app's project-root switch callback. It is called
// outside the port lock when SetRoot names a root, so the listener may probe/cancel without
// deadlocking. The listener is not called for the current root here.
func (p *lspPort) SetRootChangeListener(fn func(root string)) {
	p.mu.Lock()
	p.rootChanged = fn
	p.mu.Unlock()
}

// Supports reports whether a server starts on its own for lang.
func (p *lspPort) Supports(lang string) bool {
	p.mu.Lock()
	auto, gate := p.auto[lang], p.autoStartGate
	p.mu.Unlock()
	return auto && (gate == nil || gate())
}

// SetRemoteMarkerChecker installs the authority-backed marker checker of a remote
// workspace. nil restores the local filesystem serverRoot walk.
func (p *lspPort) SetRemoteMarkerChecker(fn func(ctx context.Context, dir string, markers []string) (bool, error)) {
	p.mu.Lock()
	p.remoteMarkers = fn
	p.rootCache = make(map[string]remoteRootCacheEntry)
	p.rootCacheOrder = nil
	p.mu.Unlock()
}

// TakeLSPNotice returns and clears the pending non-fatal root-marker notice.
func (p *lspPort) TakeLSPNotice() tui.LSPNotice {
	p.mu.Lock()
	defer p.mu.Unlock()
	notice := p.notice
	p.notice = tui.LSPNotice{}
	return notice
}

// resolveServerRoot uses the remote authority marker walk when a checker is installed,
// otherwise the local filesystem walk. The remote walk is bounded and cached; a failed
// or unsupported remote probe degrades to the workspace root with a notice.
func (p *lspPort) resolveServerRoot(filePath, language, workspace string, checker func(context.Context, string, []string) (bool, error)) string {
	markers := p.rootMarkers(language)
	if checker == nil {
		return serverRoot(filePath, markers, workspace)
	}
	if len(markers) == 0 || filePath == "" || workspace == "" {
		return workspace
	}
	ctx, cancel := context.WithTimeout(context.Background(), remoteRootScanTime)
	defer cancel()
	key := strings.Join(markers, "\x00")
	dir := path.Dir(filePath)
	startGen := p.generation()
	for steps := 0; steps < remoteRootMaxDepth; steps++ {
		if p.generation() != startGen {
			p.setNotice(tui.LSPNotice{
				Key: "gvim.notice.lsp.remote_root_changed", English: "Remote LSP root_markers scan abandoned after the workspace changed; using the workspace root",
			})
			return workspace
		}
		if ok, err := p.markerAt(ctx, checker, dir, markers, key); err != nil {
			if ctx.Err() != nil {
				p.setNotice(tui.LSPNotice{
					Key: "gvim.notice.lsp.remote_root_timeout", English: "Remote LSP root_markers scan timed out; using the workspace root",
				})
			} else {
				p.setNotice(tui.LSPNotice{
					Key: "gvim.notice.lsp.remote_root_failed", English: "Remote LSP root_markers scan failed: %{error}; using the workspace root",
					Vars: map[string]any{"error": err.Error()},
				})
			}
			return workspace
		} else if ok {
			return dir
		}
		if dir == workspace || dir == "." || dir == "/" {
			return workspace
		}
		parent := path.Dir(dir)
		if parent == dir {
			return workspace
		}
		dir = parent
	}
	p.setNotice(tui.LSPNotice{
		Key: "gvim.notice.lsp.remote_root_depth", English: "Remote LSP root_markers scan stopped after %{depth} levels; using the workspace root",
		Vars: map[string]any{"depth": remoteRootMaxDepth},
	})
	return workspace
}

func (p *lspPort) generation() uint64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.rootGen
}

func (p *lspPort) setNotice(notice tui.LSPNotice) {
	p.mu.Lock()
	p.notice = notice
	p.mu.Unlock()
}

// markerAt checks one directory, caching the answer for a bounded time.
func (p *lspPort) markerAt(ctx context.Context, checker func(context.Context, string, []string) (bool, error), dir string, markers []string, markerKey string) (bool, error) {
	cacheKey := dir + "\x00" + markerKey
	p.mu.Lock()
	if entry, ok := p.rootCache[cacheKey]; ok && time.Since(entry.at) < remoteRootCacheTTL {
		p.mu.Unlock()
		return entry.ok, nil
	}
	p.mu.Unlock()
	ok, err := checker(ctx, dir, markers)
	if err != nil {
		return false, err
	}
	p.mu.Lock()
	if len(p.rootCacheOrder) >= remoteRootCacheSize {
		old := p.rootCacheOrder[0]
		p.rootCacheOrder = p.rootCacheOrder[1:]
		delete(p.rootCache, old)
	}
	p.rootCache[cacheKey] = remoteRootCacheEntry{ok: ok, at: time.Now()}
	p.rootCacheOrder = append(p.rootCacheOrder, cacheKey)
	p.mu.Unlock()
	return ok, nil
}

// Open sends the text of the document at path, starting its servers if needed. A
// document that fails to open is not kept.
func (p *lspPort) Open(path, lang, text string) (int32, error) {
	p.WaitBaseline()
	p.selectionMu.Lock()
	defer p.selectionMu.Unlock()
	lang = openLanguage(path, lang)
	p.mu.Lock()
	workspace := p.root
	checker := p.remoteMarkers
	p.mu.Unlock()
	root := p.resolveServerRoot(path, lang, workspace, checker)
	mgr := p.managerFor(root)
	p.setDoc(path, lspDocRec{root: root, lang: lang, text: text, lines: newTextLines(text)})
	if err := mgr.Open(path, lang, text); err != nil {
		p.dropDoc(path)
		return 0, unavailable(lang, err)
	}
	version, ok := mgr.DocumentVersion(path)
	if !ok {
		version = 1
	}
	p.updateDocVersion(path, version)
	return version, nil
}

// Change sends the new full text of the open document at path. A document whose
// server has exited is reported as not open, and the root opens it again.
func (p *lspPort) Change(path, text string) (int32, error) {
	rec, mgr, ok := p.lookup(path)
	if !ok || mgr == nil {
		return 0, fmt.Errorf("%w: %s", manager.ErrNotOpen, path)
	}
	p.setDoc(path, lspDocRec{root: rec.root, lang: rec.lang, text: text, lines: newTextLines(text), version: rec.version})
	if err := mgr.Change(path, text); err != nil {
		return 0, err
	}
	version, ok := mgr.DocumentVersion(path)
	if !ok {
		version = rec.version + 1
	}
	p.updateDocVersion(path, version)
	return version, nil
}

// Close sends the close of the document at path and forgets it.
func (p *lspPort) Close(path string) error {
	_, mgr, ok := p.lookup(path)
	p.dropDoc(path)
	if !ok || mgr == nil {
		return nil
	}
	return mgr.Close(path)
}

// Diagnostics returns the latest diagnostics the servers published for the
// document at path, merged over the servers that published for it. ok is false
// when there are none yet, or the document is not open.
func (p *lspPort) Diagnostics(path string) (tui.DiagnosticsEvent, bool) {
	rec, mgr, ok := p.lookup(path)
	if !ok || mgr == nil {
		return tui.DiagnosticsEvent{}, false
	}
	docURI := lsp.PathURI(path)
	var (
		items     []tui.Diagnostic
		version   int32
		versioned = true
		seen      bool
	)
	for _, h := range mgr.Handles(rec.lang) {
		ev, ok := h.Client().DiagnosticsFor(docURI)
		if !ok {
			continue
		}
		seen = true
		items = append(items, uiDiagnostics(rec.lines, ev.Diagnostics)...)
		if !ev.Versioned {
			versioned = false
			continue
		}
		// Keep the least advanced version: the merged set is only as fresh as the
		// stalest server that contributed to it.
		if version == 0 || ev.Version < version {
			version = ev.Version
		}
	}
	if !seen {
		// No server publishes for this document: ask the servers that serve
		// diagnostics by pull instead (docs/lsp.md).
		return p.pullDiagnostics(path, rec, mgr)
	}
	return tui.DiagnosticsEvent{Path: path, Items: items, Version: version, Versioned: versioned}, true
}

// Definition returns the definitions of the symbol at at.
func (p *lspPort) Definition(path string, at buffer.Pos) ([]tui.LSPLocation, error) {
	return p.locations(path, at, manager.FeatureDefinition, func(ctx context.Context, c *lsp.Client, u string, pos protocol.Position) ([]protocol.Location, error) {
		return c.Definition(ctx, u, pos)
	})
}

// Implementation returns the implementations of the symbol at at.
func (p *lspPort) Implementation(path string, at buffer.Pos) ([]tui.LSPLocation, error) {
	return p.locations(path, at, manager.FeatureImplementation, func(ctx context.Context, c *lsp.Client, u string, pos protocol.Position) ([]protocol.Location, error) {
		return c.Implementation(ctx, u, pos)
	})
}

// References returns the references to the symbol at at, with its declaration.
func (p *lspPort) References(path string, at buffer.Pos) ([]tui.LSPLocation, error) {
	return p.locations(path, at, manager.FeatureReferences, func(ctx context.Context, c *lsp.Client, u string, pos protocol.Position) ([]protocol.Location, error) {
		return c.References(ctx, u, pos, true)
	})
}

// Hover returns the hover text at at.
func (p *lspPort) Hover(path string, at buffer.Pos) (string, error) {
	var text string
	err := p.call(path, at, manager.FeatureHover, func(ctx context.Context, c *lsp.Client, u string, pos protocol.Position) error {
		var err error
		text, err = c.Hover(ctx, u, pos)
		return err
	})
	return text, err
}

// CompletionTriggerCharacters returns the union of the completion trigger
// characters the running servers that serve completion for path advertise (static
// initialize capabilities plus dynamic registrations). It reads the manager's
// handles without opening the document or starting a server, so the root can call
// it on the update loop; nil means nothing advertises a trigger character.
func (p *lspPort) CompletionTriggerCharacters(path string) []string {
	rec, ok := p.doc(path)
	if !ok {
		return nil
	}
	mgr := p.managerFor(rec.root)
	handles := mgr.AllFor(path, manager.FeatureCompletion)
	if len(handles) == 0 {
		return nil
	}
	seen := make(map[string]bool, 4)
	var out []string
	for _, h := range handles {
		for _, ch := range h.Capabilities().CompletionTriggerCharacters {
			if ch == "" || seen[ch] {
				continue
			}
			seen[ch] = true
			out = append(out, ch)
		}
	}
	return out
}

// Completion returns the completion items at at, merged over the servers that
// serve completion for the language. Every range is in the rune columns of the
// document, snippet text is expanded with its final caret offset, and an item whose
// additional edits arrive lazily gets a resolve token.
func (p *lspPort) Completion(path string, at buffer.Pos) ([]tui.CompletionItem, error) {
	hs, rec, err := p.handles(path, manager.FeatureCompletion)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), lspRequestTimeout)
	defer cancel()
	p.mu.Lock()
	p.completionTokens = make(map[int]completionResolve)
	p.completionSeq = 0
	p.mu.Unlock()
	docURI := lsp.PathURI(path)
	pos := rec.lines.toLSP(at)
	var out []tui.CompletionItem
	var errs []error
	for _, h := range hs {
		items, err := h.Client().Completion(ctx, docURI, pos)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", h.Name(), err))
			continue
		}
		caps := h.Capabilities()
		for _, it := range items {
			out = append(out, p.uiCompletion(path, rec, h, caps, it, true))
		}
	}
	if len(out) == 0 && len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return out, nil
}

// ResolveCompletion fills the lazy fields of a completion item on the server that
// offered it (completionItem/resolve: the additional edits of an auto-import). It is
// the optional CompletionResolver port and runs in a command.
func (p *lspPort) ResolveCompletion(path string, token int) (tui.CompletionItem, error) {
	p.mu.Lock()
	entry, ok := p.completionTokens[token]
	p.mu.Unlock()
	if !ok || entry.path != path {
		return tui.CompletionItem{}, errors.New("lsp: the completion item is no longer available")
	}
	rec, ok := p.doc(path)
	if !ok {
		return tui.CompletionItem{}, fmt.Errorf("%w: %s", manager.ErrNotOpen, path)
	}
	ctx, cancel := context.WithTimeout(context.Background(), lspRequestTimeout)
	defer cancel()
	resolved, err := entry.handle.Client().CompletionResolve(ctx, entry.item)
	if err != nil {
		return tui.CompletionItem{}, err
	}
	out := p.uiCompletion(path, rec, entry.handle, entry.handle.Capabilities(), resolved, false)
	out.ResolveToken = 0
	return out, nil
}

// uiCompletion converts one server item to the root's form: rune ranges, expanded
// snippet text and its final caret offset, the additional edits (auto-imports), and
// a resolve token when the server fills the item in lazily.
func (p *lspPort) uiCompletion(path string, rec lspDocRec, h *manager.Handle, caps lsp.Capabilities, it lsp.CompletionItem, allowResolve bool) tui.CompletionItem {
	insert := it.InsertText
	cursor := -1
	if it.Snippet {
		expanded := lsp.ExpandSnippet(insert)
		insert, cursor = expanded.Text, expanded.CursorOffset
	}
	out := tui.CompletionItem{
		Label: it.Label, Detail: it.Detail, Insert: insert,
		Range: rec.lines.rangeOf(it.EditRange), CursorOffset: cursor, Snippet: it.Snippet,
	}
	for _, te := range it.AdditionalTextEdits {
		if r := rec.lines.rangeOf(&te.Range); r != nil {
			out.Additional = append(out.Additional, tui.CompletionEdit{Range: *r, Text: te.NewText})
		}
	}
	if allowResolve && len(out.Additional) == 0 && it.HasData && caps.CompletionResolve {
		p.mu.Lock()
		p.completionSeq++
		token := p.completionSeq
		p.completionTokens[token] = completionResolve{path: path, handle: h, item: it}
		p.mu.Unlock()
		out.ResolveToken = token
	}
	return out
}

// Rename renames the symbol at at to name and returns the workspace edit to
// apply: the text edits per file and any file create, rename or delete.
func (p *lspPort) Rename(path string, at buffer.Pos, name string) (tui.WorkspaceEdit, error) {
	var edit *protocol.WorkspaceEdit
	err := p.call(path, at, manager.FeatureRename, func(ctx context.Context, c *lsp.Client, u string, pos protocol.Position) error {
		var err error
		edit, err = c.Rename(ctx, u, pos, name)
		return err
	})
	if err != nil {
		return tui.WorkspaceEdit{}, err
	}
	return p.workspaceEdit(edit, "")
}

// CodeActions returns the code actions at at, merged over the servers that serve
// them for the language. The stored diagnostics that overlap the position are
// sent as context, as servers that offer quick fixes need them.
func (p *lspPort) CodeActions(path string, at buffer.Pos) ([]tui.CodeAction, error) {
	hs, rec, err := p.handles(path, manager.FeatureCodeAction)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), lspRequestTimeout)
	defer cancel()
	docURI := lsp.PathURI(path)
	pos := rec.lines.toLSP(at)
	rng := protocol.Range{Start: pos, End: pos}
	var out []tui.CodeAction
	var errs []error
	for _, h := range hs {
		actions, err := h.Client().CodeActions(ctx, docURI, rng, p.diagnosticsFor(h, docURI, rng))
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", h.Name(), err))
			continue
		}
		for _, a := range actions {
			out = append(out, tui.CodeAction{
				Title:        a.Title,
				Kind:         a.Kind,
				Source:       h.Name(),
				Preferred:    a.Preferred,
				Edit:         editOrNil(p, a.Edit, a.Title),
				Command:      commandOf(a.Command),
				NeedsResolve: a.HasData && a.Edit == nil && a.Command == nil,
				Data:         actionData{language: rec.lang, server: h.Name(), action: a},
			})
		}
	}
	if len(out) == 0 && len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return out, nil
}

// ResolveCodeAction completes an action that was offered with only its data.
func (p *lspPort) ResolveCodeAction(path string, action tui.CodeAction) (tui.CodeAction, error) {
	data, ok := action.Data.(actionData)
	if !ok {
		return action, nil
	}
	h, ok := p.handleNamed(path, data.language, data.server)
	if !ok {
		return tui.CodeAction{}, fmt.Errorf("lsp: the server %s of %s is not running", data.server, action.Title)
	}
	ctx, cancel := context.WithTimeout(context.Background(), lspRequestTimeout)
	defer cancel()
	resolved, err := h.Client().ResolveCodeAction(ctx, data.action)
	if err != nil {
		return tui.CodeAction{}, err
	}
	out := action
	out.Edit = editOrNil(p, resolved.Edit, resolved.Title)
	out.Command = commandOf(resolved.Command)
	out.NeedsResolve = false
	out.Data = actionData{language: data.language, server: data.server, action: resolved}
	return out, nil
}

// ExecuteCommand runs a command of the server that offered it.
func (p *lspPort) ExecuteCommand(path string, command tui.LSPCommand) error {
	rec, ok := p.doc(path)
	if !ok {
		return fmt.Errorf("%w: %s", manager.ErrNotOpen, path)
	}
	source := command.Source
	if source == "" {
		if data, ok := command.Data.(actionData); ok && data.server != "" {
			source = data.server
		}
	}
	h, ok := p.handleNamed(path, rec.lang, source)
	if !ok {
		hs := p.handlesFor(rec.lang)
		if len(hs) == 0 {
			return fmt.Errorf("%w: %s", manager.ErrNotOpen, path)
		}
		h = hs[0]
	}
	ctx, cancel := context.WithTimeout(context.Background(), lspRequestTimeout)
	defer cancel()
	return h.Client().ExecuteCommand(ctx, protocol.Command{Title: command.Title, Command: command.Command, Arguments: lspArguments(command.Arguments)})
}

// WorkspaceEdits drains the edits servers asked to apply through
// workspace/applyEdit, in arrival order.
func (p *lspPort) WorkspaceEdits() []tui.WorkspaceEdit {
	p.mu.Lock()
	pending := p.pending
	p.pending = nil
	p.mu.Unlock()
	out := make([]tui.WorkspaceEdit, 0, len(pending))
	for _, pe := range pending {
		edit, err := p.workspaceEdit(&pe.edit, pe.label)
		if err != nil {
			continue
		}
		out = append(out, edit)
	}
	return out
}

// actionData is the port's opaque payload of a code action: which server offered
// it and the action as the protocol sent it.
type actionData struct {
	language string
	server   string
	action   lsp.CodeAction
}

// editOrNil converts a server's workspace edit.
func editOrNil(p *lspPort, edit *protocol.WorkspaceEdit, label string) *tui.WorkspaceEdit {
	if edit == nil {
		return nil
	}
	converted, err := p.workspaceEdit(edit, label)
	if err != nil {
		return nil
	}
	return &converted
}

// workspaceEdit converts a protocol workspace edit to the port's form: text ops
// with rune ranges and resource operations, in the order the server sent them.
func (p *lspPort) workspaceEdit(edit *protocol.WorkspaceEdit, label string) (tui.WorkspaceEdit, error) {
	out := tui.WorkspaceEdit{Label: label}
	if edit == nil {
		return out, nil
	}
	appendText := func(docURI string, version *int32, els []protocol.TextDocumentEditElement) error {
		file := uri.URI(docURI).FsPath()
		lines := newTextLines(p.textOf(file))
		op := tui.WorkspaceOp{Kind: tui.WorkspaceOpText, Path: file, Version: version}
		for _, el := range els {
			te, err := plainTextEdit(el)
			if err != nil {
				return err
			}
			op.Edits = append(op.Edits, editor.LSPEdit{
				From: lines.fromLSP(te.Range.Start),
				To:   lines.fromLSP(te.Range.End),
				Text: te.NewText,
			})
		}
		out.Ops = append(out.Ops, op)
		return nil
	}
	if len(edit.DocumentChanges) > 0 {
		for _, dc := range edit.DocumentChanges {
			switch change := dc.(type) {
			case *protocol.TextDocumentEdit:
				if err := appendText(string(change.TextDocument.URI), change.TextDocument.Version, change.Edits); err != nil {
					return tui.WorkspaceEdit{}, err
				}
			case *protocol.CreateFile:
				op := tui.WorkspaceOp{Kind: tui.WorkspaceOpCreate, Path: uri.URI(change.URI).FsPath()}
				if change.Options != nil {
					op.Overwrite = boolValue(change.Options.Overwrite)
					op.IgnoreIfExists = boolValue(change.Options.IgnoreIfExists)
				}
				out.Ops = append(out.Ops, op)
			case *protocol.RenameFile:
				op := tui.WorkspaceOp{Kind: tui.WorkspaceOpRename, Path: uri.URI(change.OldURI).FsPath(), NewPath: uri.URI(change.NewURI).FsPath()}
				if change.Options != nil {
					op.Overwrite = boolValue(change.Options.Overwrite)
					op.IgnoreIfExists = boolValue(change.Options.IgnoreIfExists)
				}
				out.Ops = append(out.Ops, op)
			case *protocol.DeleteFile:
				out.Ops = append(out.Ops, tui.WorkspaceOp{Kind: tui.WorkspaceOpDelete, Path: uri.URI(change.URI).FsPath()})
			default:
				return tui.WorkspaceEdit{}, fmt.Errorf("lsp: unsupported document change %T", dc)
			}
		}
		return out, nil
	}
	for _, u := range slices.Sorted(maps.Keys(edit.Changes)) {
		if err := appendText(string(u), nil, textEdits(edit.Changes[u])); err != nil {
			return tui.WorkspaceEdit{}, err
		}
	}
	return out, nil
}

// textEdits wraps the plain text edits of a changes map.
func textEdits(edits []protocol.TextEdit) []protocol.TextDocumentEditElement {
	out := make([]protocol.TextDocumentEditElement, len(edits))
	for i := range edits {
		out[i] = &edits[i]
	}
	return out
}

// boolValue reads a protocol bool option.
func boolValue(v *bool) bool { return v != nil && *v }

// commandOf converts a server command, or returns nil.
func commandOf(cmd *protocol.Command) *tui.LSPCommand {
	if cmd == nil || cmd.Command == "" {
		return nil
	}
	out := &tui.LSPCommand{Title: cmd.Title, Command: cmd.Command}
	if len(cmd.Arguments) > 0 {
		out.Arguments = slices.Clone(cmd.Arguments)
	}
	return out
}

// lspArguments converts the opaque argument value back to the protocol's list.
func lspArguments(v any) []protocol.LSPAny {
	if args, ok := v.([]protocol.LSPAny); ok {
		return args
	}
	return nil
}

// diagnosticsFor returns the stored diagnostics of h that overlap rng, the code
// action context many servers need before offering a quick fix.
func (p *lspPort) diagnosticsFor(h *manager.Handle, docURI string, rng protocol.Range) []protocol.Diagnostic {
	ev, ok := h.Client().DiagnosticsFor(docURI)
	if !ok {
		return nil
	}
	out := make([]protocol.Diagnostic, 0, len(ev.Diagnostics))
	for _, d := range ev.Diagnostics {
		if rangesOverlap(d.Range, rng) {
			out = append(out, d)
		}
	}
	return out
}

// rangesOverlap reports whether two protocol ranges overlap; an empty range
// overlaps a range that contains its position.
func rangesOverlap(a, b protocol.Range) bool {
	if positionAfter(b.Start, a.End) || positionAfter(a.Start, b.End) {
		return false
	}
	return true
}

// positionAfter reports whether a is after b.
func positionAfter(a, b protocol.Position) bool {
	return a.Line > b.Line || (a.Line == b.Line && a.Character > b.Character)
}

// maxRequestResult bounds the raw JSON result of Request that is decoded: a plugin must
// not be able to make the editor build a value of any size.
const maxRequestResult = 4 << 20

// Request sends one request of the protocol to a server that has the document at path
// open, and returns the decoded JSON result. The caller bounds nothing: the call is
// limited by lspRequestTimeout, and the result by maxRequestResult.
func (p *lspPort) Request(path, method string, params any) (any, error) {
	rec, ok := p.doc(path)
	if !ok {
		return nil, pluginapi.Errf(pluginapi.CodeNotFound, "%v: %s", manager.ErrNotOpen, path)
	}
	mgr := p.managerFor(rec.root)
	h, ok := mgr.HandleForPath(path)
	if !ok {
		if err := mgr.Open(path, rec.lang, rec.text); err != nil {
			if errors.Is(err, manager.ErrNotOpen) {
				return nil, pluginapi.Errf(pluginapi.CodeNotFound, "%v", err)
			}
			return nil, err
		}
		if h, ok = mgr.HandleForPath(path); !ok {
			return nil, pluginapi.Errf(pluginapi.CodeNotFound, "%v: %s", manager.ErrNotOpen, path)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), lspRequestTimeout)
	defer cancel()
	raw, err := h.Client().Request(ctx, method, params)
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return nil, pluginapi.Errf(pluginapi.CodeTimeout, "%s did not answer in %v", method, lspRequestTimeout)
	case err != nil:
		return nil, err
	case len(raw) > maxRequestResult:
		return nil, pluginapi.Errf(pluginapi.CodeTooLarge, "the result of %s is over %d bytes", method, maxRequestResult)
	}
	var out any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("lsp: %s: bad result: %w", method, err)
	}
	return out, nil
}

// Format asks the server of the document at path to format it and returns the
// edits in rune columns of text. When the server holds older text than text, it is
// sent first, so the positions fit. A server with no formatting answers with no
// edits.
func (p *lspPort) Format(path, text string, tabSize int, insertSpaces bool) ([]editor.LSPEdit, error) {
	rec, ok := p.doc(path)
	if !ok {
		return nil, errors.New("the document is not open on a language server")
	}
	if rec.text != text {
		if _, err := p.Change(path, text); err != nil {
			return nil, err
		}
	}
	hs, _, err := p.handles(path, manager.FeatureFormat)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), lspRequestTimeout)
	defer cancel()
	edits, err := hs[0].Client().Formatting(ctx, lsp.PathURI(path), tabSize, insertSpaces)
	if err != nil {
		return nil, err
	}
	lines := newTextLines(text)
	out := make([]editor.LSPEdit, 0, len(edits))
	for _, e := range edits {
		out = append(out, editor.LSPEdit{From: lines.fromLSP(e.Range.Start), To: lines.fromLSP(e.Range.End), Text: e.NewText})
	}
	return out, nil
}

// StartLanguage starts every configured server of language, a manual start that
// overrides auto_start, lsp_enabled and the mute. It waits on the starts, so the
// root and the settings service call it from a command.
func (p *lspPort) StartLanguage(language string) error {
	p.selectionMu.Lock()
	defer p.selectionMu.Unlock()
	type managerDesired struct {
		root    string
		mgr     *manager.Manager
		desired []manager.ServerConfig
	}
	p.mu.Lock()
	p.actionSeq[language]++
	startSeq := p.actionSeq[language]
	trustSeq := p.trustSeq
	// A manual start under Restricted/Blocked may start the server now (explicit user
	// action) but must not leave a persistent forced bypass of the live auto-start gate.
	persist := p.autoStartGate == nil || p.autoStartGate()
	if persist {
		p.forced[language] = true
	} else {
		delete(p.forced, language)
	}
	delete(p.muted, language)
	list := make([]managerDesired, 0, len(p.managers))
	for root, mgr := range p.managers {
		list = append(list, managerDesired{root: root, mgr: mgr, desired: slices.Clone(p.serverConfigsFor(root)[language])})
	}
	p.mu.Unlock()

	var errs []error
	for _, item := range list {
		if !reflect.DeepEqual(item.mgr.CurrentServers(language), item.desired) {
			// The completed per-root selection differs from this existing manager's config
			// (for example Deno was confirmed after the manager was created, or a config
			// reload removed/disabled the override). Applying it is explicit: Start/Restart
			// only, and only this language's servers are stopped by ReplaceServers.
			err := item.mgr.ReplaceServers(language, item.desired)
			errs = append(errs, err)
			if err == nil {
				errs = append(errs, p.reopenDocs(item.root, language)...)
			}
			continue
		}
		errs = append(errs, item.mgr.StartLanguage(language))
	}
	p.finishManual(language, startSeq, trustSeq, persist)
	return errors.Join(errs...)
}

// Restart shuts down the servers of every root. The root reopens its documents
// after it. A language named here counts as started by hand, so its servers start
// at the next open whatever auto_start says.
func (p *lspPort) Restart(language string) error {
	// A workspace/diagnostic pull in flight asks a server that is about to go: cancel
	// it on the wire instead of letting it answer into a cache the restart clears.
	p.wsPulls.cancelAll()
	p.mu.Lock()
	var startSeq, trustSeq uint64
	var persist bool
	if language != "" {
		// Root-wide Restart (language == "") must not bump any per-language action token:
		// an in-flight manual Start for a language must still finish its own cleanup.
		p.actionSeq[language]++
		startSeq = p.actionSeq[language]
		trustSeq = p.trustSeq
		persist = p.autoStartGate == nil || p.autoStartGate()
		if persist {
			p.forced[language] = true
		} else {
			delete(p.forced, language)
		}
		delete(p.muted, language)
	} else {
		// Root-wide Restart clears the port-level manual intent: it must not be
		// inherited by managers created for the rebuilt roots. It does not bump any
		// per-language action token or trustSeq, so an in-flight per-language Start's
		// finishManual still owns and clears its own language.
		p.forced = make(map[string]bool)
	}
	mgrs := slices.Collect(maps.Values(p.managers))
	p.managers = make(map[string]*manager.Manager)
	p.sem = make(map[string]semanticCache)
	p.pulled = make(map[string]pulledDiagnostics)
	p.wsPull = workspacePull{}
	p.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), lspStopTimeout)
	defer cancel()
	var errs []error
	for _, mgr := range mgrs {
		errs = append(errs, mgr.Shutdown(ctx))
	}
	if language != "" {
		p.finishManual(language, startSeq, trustSeq, persist)
	}
	return errors.Join(errs...)
}

// Stop shuts down every server of every root and forgets every document.
func (p *lspPort) Stop() error {
	p.wsPulls.cancelAll()
	p.mu.Lock()
	mgrs := slices.Collect(maps.Values(p.managers))
	p.managers = make(map[string]*manager.Manager)
	p.docs = make(map[string]lspDocRec)
	p.sem = make(map[string]semanticCache)
	p.pulled = make(map[string]pulledDiagnostics)
	p.wsPull = workspacePull{}
	p.pending = nil
	p.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), lspStopTimeout)
	defer cancel()
	var errs []error
	for _, mgr := range mgrs {
		errs = append(errs, mgr.Shutdown(ctx))
	}
	return errors.Join(errs...)
}

// Servers lists every configured server with its state, for the status popup.
func (p *lspPort) Servers() []tui.LSPServerStatus {
	p.mu.Lock()
	mgrs := slices.Collect(maps.Values(p.managers))
	muted := maps.Clone(p.muted)
	status := p.status
	p.mu.Unlock()
	running := make(map[string]manager.ServerStatus)
	for _, mgr := range mgrs {
		for _, st := range mgr.Servers() {
			running[st.Language+"\x00"+st.Name] = st
		}
	}
	out := make([]tui.LSPServerStatus, 0, len(status))
	for _, st := range status {
		cur, ok := running[st.Language+"\x00"+st.Name]
		if ok {
			st.State = runState(cur.State)
			st.Error = cur.Error
			st.Limits = cur.Limits
		} else if muted[st.Language] {
			st.State = tui.LSPServerStopped
		}
		out = append(out, st)
	}
	return out
}

// runState converts the manager's state to the port's.
func runState(s manager.ServerState) tui.LSPServerRunState {
	switch s {
	case manager.ServerRunning:
		return tui.LSPServerRunning
	case manager.ServerStarting:
		return tui.LSPServerStarting
	case manager.ServerError:
		return tui.LSPServerFailed
	}
	return tui.LSPServerStopped
}

// RestartServer stops the named server and starts it again, whatever auto_start or
// the mute says. With no active-file path this targets the current workspace root (not an
// arbitrary map-order manager).
func (p *lspPort) RestartServer(language, name string) error {
	return p.restartServerAt(p.serverActionRoot("", language), language, name)
}

// RestartServerFor targets the root of path: the status popup's Restart Server uses the
// active document so a language/name pair present in several roots restarts the manager
// the user is looking at.
func (p *lspPort) RestartServerFor(path, language, name string) error {
	return p.restartServerAt(p.serverActionRoot(path, language), language, name)
}

func (p *lspPort) restartServerAt(root, language, name string) error {
	// The pull may be asking this very server: cancel it before the restart.
	p.wsPulls.cancelAll()
	p.selectionMu.Lock()
	defer p.selectionMu.Unlock()
	p.mu.Lock()
	p.actionSeq[language]++
	startSeq := p.actionSeq[language]
	trustSeq := p.trustSeq
	persist := p.autoStartGate == nil || p.autoStartGate()
	delete(p.muted, language)
	mgr := p.managers[root]
	if mgr == nil {
		mgr = p.newManagerLocked(root)
		p.managers[root] = mgr
	}
	desired := slices.Clone(p.serverConfigsFor(root)[language])
	p.mu.Unlock()
	var err error
	if !reflect.DeepEqual(mgr.CurrentServers(language), desired) {
		// Explicit Restart Server applies a completed per-root selection to the existing
		// manager (only this language's servers are stopped by ReplaceServers).
		err = mgr.ReplaceServers(language, desired)
		if err == nil {
			err = errors.Join(p.reopenDocs(root, language)...)
		}
	} else {
		err = mgr.RestartServer(language, name)
	}
	p.finishManual(language, startSeq, trustSeq, persist)
	return err
}

// serverActionRoot returns the root a server action should target. An open document in
// the same language supplies its recorded manager root; otherwise the current workspace
// root is used, never random map iteration.
func (p *lspPort) serverActionRoot(path, language string) string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if path != "" {
		if rec, ok := p.docs[path]; ok && rec.lang == language && rec.root != "" {
			return rec.root
		}
	}
	return p.root
}

// StopServer stops the named server of the current workspace root.
func (p *lspPort) StopServer(language, name string) error {
	return p.stopServerAt(p.serverActionRoot("", language), language, name)
}

// StopServerFor stops the named server in the root of path.
func (p *lspPort) StopServerFor(path, language, name string) error {
	return p.stopServerAt(p.serverActionRoot(path, language), language, name)
}

func (p *lspPort) stopServerAt(root, language, name string) error {
	p.wsPulls.cancelAll()
	p.mu.Lock()
	mgr := p.managers[root]
	p.mu.Unlock()
	if mgr == nil {
		return nil
	}
	return mgr.StopServer(language, name)
}

// SetLanguageEnabled mutes or unmutes a language.
func (p *lspPort) SetLanguageEnabled(language string, enabled bool) error {
	// Muting a language stops its servers; a pull of theirs in flight is cancelled.
	p.wsPulls.cancelAll()
	p.mu.Lock()
	if enabled {
		delete(p.muted, language)
	} else {
		p.muted[language] = true
		delete(p.forced, language)
	}
	mgrs := slices.Collect(maps.Values(p.managers))
	p.mu.Unlock()
	for _, mgr := range mgrs {
		mgr.SetLanguageEnabled(language, enabled)
	}
	return nil
}

// reopenDocs re-sends didOpen for the open documents of one root/language after an explicit
// Start/Restart replaced that language's server config. The manager cannot do this itself:
// it does not keep the document text. Errors are reported alongside the start result.
func (p *lspPort) reopenDocs(root, language string) []error {
	type sample struct{ path, lang, text string }
	p.mu.Lock()
	var docs []sample
	for path, rec := range p.docs {
		if rec.root == root && rec.lang == language {
			docs = append(docs, sample{path: path, lang: rec.lang, text: rec.text})
		}
	}
	p.mu.Unlock()
	var errs []error
	for _, d := range docs {
		if _, err := p.Open(d.path, d.lang, d.text); err != nil {
			errs = append(errs, err)
		}
	}
	return errs
}

// locationFile resolves one protocol location URI to a local path. A file URI is
// converted directly; any other scheme goes through the app's external-location opener,
// which owns validation, trust/authority checks and generation. The returned readOnly
// marks generated content that the TUI must open non-editable.
func (p *lspPort) locationFile(ctx context.Context, raw string, target ExternalLocationRequest) (file string, readOnly bool, err error) {
	u, err := uri.ParseStrict(raw)
	if err != nil {
		return "", false, fmt.Errorf("lsp location %q: %w", raw, err)
	}
	if u.IsFile() || u.Scheme() == "" {
		return u.FsPath(), false, nil
	}
	p.mu.Lock()
	opener := p.externalOpener
	p.mu.Unlock()
	if opener == nil {
		return "", false, fmt.Errorf("external location %q: no URI provider", raw)
	}
	ctx, cancel := context.WithTimeout(ctx, externalLocationTimeout)
	defer cancel()
	target.URI = raw
	path, err := opener(ctx, target)
	if err != nil {
		return "", false, fmt.Errorf("external location %q: %w", raw, err)
	}
	if path == "" || !filepath.IsAbs(path) {
		return "", false, fmt.Errorf("external location %q: provider returned a non-absolute path %q", raw, path)
	}
	st, err := os.Stat(path)
	if err != nil {
		return "", false, fmt.Errorf("external location %q: %w", raw, err)
	}
	if !st.Mode().IsRegular() {
		return "", false, fmt.Errorf("external location %q: %q is not a regular file", raw, path)
	}
	return path, true, nil
}

// locations runs a request that returns locations, and converts the result to rune
// positions of each file.
func (p *lspPort) locations(path string, at buffer.Pos, feature manager.Feature, ask func(context.Context, *lsp.Client, string, protocol.Position) ([]protocol.Location, error)) ([]tui.LSPLocation, error) {
	hs, rec, err := p.handles(path, feature)
	if err != nil {
		return nil, err
	}
	if len(hs) == 0 {
		return nil, fmt.Errorf("lsp: no server serves %s for %s", feature, rec.lang)
	}
	h := hs[0]
	ctx, cancel := context.WithTimeout(context.Background(), lspRequestTimeout)
	defer cancel()
	var locs []protocol.Location
	locs, askErr := ask(ctx, h.Client(), lsp.PathURI(path), rec.lines.toLSP(at))
	if askErr != nil {
		return nil, askErr
	}
	target := ExternalLocationRequest{
		Language: rec.lang,
		Root:     rec.root,
		Program:  h.Program(),
		Command:  h.Command(),
		Env:      h.LaunchEnv(),
	}
	texts := make(map[string]textLines)
	out := make([]tui.LSPLocation, 0, len(locs))
	for _, l := range locs {
		file, readOnly, err := p.locationFile(context.Background(), string(l.URI), target)
		if err != nil {
			return nil, err
		}
		lines, ok := texts[file]
		if !ok {
			lines = newTextLines(p.textOf(file))
			texts[file] = lines
		}
		out = append(out, tui.LSPLocation{Path: file, Start: lines.fromLSP(l.Range.Start), End: lines.fromLSP(l.Range.End), ReadOnly: readOnly})
	}
	return out, nil
}

// call runs one exclusive request for the document at path. fn gets a context
// bounded by lspRequestTimeout, the client, the document's URI and the protocol
// position of at.
func (p *lspPort) call(path string, at buffer.Pos, feature manager.Feature, fn func(context.Context, *lsp.Client, string, protocol.Position) error) error {
	hs, rec, err := p.handles(path, feature)
	if err != nil {
		return err
	}
	if len(hs) == 0 {
		return fmt.Errorf("lsp: no server serves %s for %s", feature, rec.lang)
	}
	ctx, cancel := context.WithTimeout(context.Background(), lspRequestTimeout)
	defer cancel()
	return fn(ctx, hs[0].Client(), lsp.PathURI(path), rec.lines.toLSP(at))
}

// handles returns the servers that serve feature for the document at path. When the
// document's servers have exited, it opens the document again first, which starts
// them.
func (p *lspPort) handles(path string, feature manager.Feature) ([]*manager.Handle, lspDocRec, error) {
	rec, ok := p.doc(path)
	if !ok {
		return nil, lspDocRec{}, fmt.Errorf("%w: %s", manager.ErrNotOpen, path)
	}
	mgr := p.managerFor(rec.root)
	var hs []*manager.Handle
	if feature.Merged() {
		hs = mgr.AllFor(path, feature)
	} else if h, ok := mgr.FirstFor(path, feature); ok {
		hs = []*manager.Handle{h}
	}
	if len(hs) == 0 {
		if err := mgr.Open(path, rec.lang, rec.text); err != nil {
			return nil, rec, err
		}
		if feature.Merged() {
			hs = mgr.AllFor(path, feature)
		} else if h, ok := mgr.FirstFor(path, feature); ok {
			hs = []*manager.Handle{h}
		}
	}
	if len(hs) == 0 {
		return nil, rec, fmt.Errorf("lsp: no running server serves %s for %s", feature, rec.lang)
	}
	return hs, rec, nil
}

// handleNamed returns the handle of language the server name names.
func (p *lspPort) handleNamed(path, language, name string) (*manager.Handle, bool) {
	rec, ok := p.doc(path)
	if !ok {
		return nil, false
	}
	mgr := p.managerFor(rec.root)
	for _, h := range mgr.Handles(rec.lang) {
		if h.Name() == name {
			return h, true
		}
	}
	return nil, false
}

// handlesFor returns every live handle that serves language.
func (p *lspPort) handlesFor(language string) []*manager.Handle {
	p.mu.Lock()
	mgrs := slices.Collect(maps.Values(p.managers))
	p.mu.Unlock()
	var out []*manager.Handle
	for _, mgr := range mgrs {
		out = append(out, mgr.Handles(language)...)
	}
	return out
}

// textOf returns the text of the document at path: the text the server holds when
// it is open, else the file on disk. A file that cannot be read has no text.
func (p *lspPort) textOf(path string) string {
	if rec, ok := p.doc(path); ok {
		return rec.text
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(data)
}

// rootMarkers returns the root markers a server uses for language, from the first
// configured server of it.
func (p *lspPort) rootMarkers(language string) []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if configs := p.servers[language]; len(configs) > 0 {
		return configs[0].RootMarkers
	}
	return nil
}

// SetConfig reapplies the server plan of a settings reload: future managers and explicit
// Start/Restart use the new baseline, while existing managers keep their current config until
// an explicit Start/Restart applies a difference. It does not touch running servers or the
// per-root Deno providers (they re-read the live config too).
func (p *lspPort) SetConfig(cfg config.Config) {
	plan := lspPlanFor(cfg)
	p.selectionMu.Lock()
	defer p.selectionMu.Unlock()
	p.mu.Lock()
	p.servers = plan.servers
	p.universal = plan.universal
	p.info = plan.info
	p.auto = plan.auto
	p.startable = plan.startable
	p.status = statusList(plan)
	p.rootCache = make(map[string]remoteRootCacheEntry)
	p.rootCacheOrder = nil
	p.mu.Unlock()
}

// RefreshServerPlans applies the current baseline to existing managers without stopping
// running servers. It is used before a workspace becomes trusted.
func (p *lspPort) RefreshServerPlans() {
	p.selectionMu.Lock()
	defer p.selectionMu.Unlock()
	p.mu.Lock()
	defer p.mu.Unlock()
	for root, mgr := range p.managers {
		mgr.UpdateServerPlan(p.serverConfigsFor(root), p.universal)
	}
}

// SetBaselineWait installs the app's asynchronous config barrier.
func (p *lspPort) SetBaselineWait(wait func()) {
	p.mu.Lock()
	p.baselineWait = wait
	p.mu.Unlock()
}

// WaitBaseline waits for pending config changes without holding the port lock.
func (p *lspPort) WaitBaseline() {
	p.mu.Lock()
	wait := p.baselineWait
	p.mu.Unlock()
	if wait != nil {
		wait()
	}
}

// SetExternalLocationOpener installs the app's resolver for non-file LSP locations.
// The caller validates the URI/trust/authority and returns generated local content;
// nil disables external locations.
func (p *lspPort) SetExternalLocationOpener(fn ExternalLocationOpener) {
	p.mu.Lock()
	p.externalOpener = fn
	p.mu.Unlock()
}

// SetRootServerProvider installs a per-root server replacement resolver. The provider is
// consulted when the manager for exactly root is created (never for another root); it may
// return a replacement config for a language. A nil provider clears it. Existing managers
// are not rebuilt: a running or already-created server keeps its config (Deno auto-selection
// therefore lands on future roots/managers only).
func (p *lspPort) SetRootServerProvider(root string, provider func(language string) (manager.ServerConfig, bool)) {
	p.selectionMu.Lock()
	defer p.selectionMu.Unlock()
	p.mu.Lock()
	if p.rootProviders == nil {
		p.rootProviders = make(map[string]func(language string) (manager.ServerConfig, bool))
	}
	if provider == nil {
		delete(p.rootProviders, root)
	} else {
		p.rootProviders[root] = provider
	}
	p.mu.Unlock()
}

// ClearRootServerProvider removes the per-root provider for root.
func (p *lspPort) ClearRootServerProvider(root string) {
	p.selectionMu.Lock()
	defer p.selectionMu.Unlock()
	p.mu.Lock()
	delete(p.rootProviders, root)
	p.mu.Unlock()
}

// serverConfigsFor returns the language configs a manager of root starts with: the
// port's plan, with the root provider's replacement applied to each language it answers
// for. p.mu must be held.
func (p *lspPort) serverConfigsFor(root string) map[string][]manager.ServerConfig {
	if p.rootProviders == nil {
		return p.servers
	}
	provider := p.rootProviders[root]
	if provider == nil {
		return p.servers
	}
	servers := make(map[string][]manager.ServerConfig, len(p.servers))
	for lang, list := range p.servers {
		servers[lang] = list
		if cfg, ok := provider(lang); ok {
			servers[lang] = []manager.ServerConfig{cfg}
		}
	}
	return servers
}

// managerFor returns the manager of root, creating it on first use.
func (p *lspPort) managerFor(root string) *manager.Manager {
	p.mu.Lock()
	defer p.mu.Unlock()
	if mgr, ok := p.managers[root]; ok {
		return mgr
	}
	mgr := p.newManagerLocked(root)
	p.managers[root] = mgr
	return mgr
}

// newManagerLocked builds the manager of root and applies the manual starts. p.mu
// must be held.
func (p *lspPort) newManagerLocked(root string) *manager.Manager {
	mgr := manager.NewManager(root, p.serverConfigsFor(root), p.universal)
	mgr.SetApplyEdit(p.enqueueEdit)
	mgr.SetSpawnEnv(p.spawnEnv)
	mgr.SetGate(p.spawnGate)
	mgr.SetAutoStartGate(p.autoStartGate)
	mgr.SetSpawner(p.spawner)
	for lang := range p.forced {
		mgr.ForceLanguage(lang)
	}
	return mgr
}

// enqueueEdit is the workspace/applyEdit handler: it queues the edit and answers
// the server at once. It runs on a server's read goroutine, so it does no IO and
// waits for nothing; the root drains the queue and applies the edits.
func (p *lspPort) enqueueEdit(language string, params protocol.ApplyWorkspaceEditParams) bool {
	label := ""
	if params.Label != nil {
		label = *params.Label
	}
	p.mu.Lock()
	p.pending = append(p.pending, pendingEdit{language: language, edit: params.Edit, label: label})
	p.mu.Unlock()
	return true
}

// SetSpawnEnv installs the active project environment's set-only overlay for
// servers this port starts from now on. It is read at every server launch.
func (p *lspPort) SetSpawnEnv(overlay func() []string) {
	p.mu.Lock()
	p.spawnEnv = overlay
	p.mu.Unlock()
}

// SetSpawnGate installs the workspace trust spawn gate for the servers this port starts
// from now on. It is read at every start, so a trust decision applies to the next one.
func (p *lspPort) SetSpawnGate(gate func(ctx context.Context, argv []string, cwd, pathValue string) error) {
	p.mu.Lock()
	p.spawnGate = gate
	managers := make([]*manager.Manager, 0, len(p.managers))
	for _, mgr := range p.managers {
		managers = append(managers, mgr)
	}
	p.mu.Unlock()
	for _, mgr := range managers {
		mgr.SetGate(gate)
	}
}

// finishManual reconciles a manual start with the live trust version. When the action is
// still the newest (not superseded by another manual action) and it did not persist (manual
// start while not Trusted) or trust changed while it ran, it clears the forced state it may
// have installed, so a concurrent ClearForced cannot be overwritten. It never stops the
// servers the explicit action started.
func (p *lspPort) finishManual(language string, startSeq, trustSeq uint64, persist bool) {
	p.mu.Lock()
	currentStart, currentTrust := p.actionSeq[language], p.trustSeq
	if currentStart != startSeq {
		p.mu.Unlock()
		return // a newer manual action owns the forced state
	}
	if persist && currentTrust == trustSeq {
		p.mu.Unlock()
		return
	}
	delete(p.forced, language)
	mgrs := slices.Collect(maps.Values(p.managers))
	p.mu.Unlock()
	for _, mgr := range mgrs {
		mgr.ClearForcedLanguage(language)
	}
}

// ClearForced drops the manual-start overrides of the port and all of its managers. A
// trust change calls it so a language manually started while Trusted cannot keep an
// automatic=false bypass of the auto-start gate after a revoke; running processes are not
// stopped. It takes no selectionMu (the trust dialog must not wait on an LSP start).
func (p *lspPort) ClearForced() {
	p.mu.Lock()
	p.trustSeq++
	p.forced = make(map[string]bool)
	mgrs := slices.Collect(maps.Values(p.managers))
	p.mu.Unlock()
	for _, mgr := range mgrs {
		mgr.ClearForced()
	}
}

// SetAutoStartGate installs the live trust gate for automatic starts. It is applied to
// existing managers (without stopping anything) and to managers created later; manual
// StartLanguage/Restart still uses the spawn gate.
func (p *lspPort) SetAutoStartGate(gate func() bool) {
	p.mu.Lock()
	p.autoStartGate = gate
	mgrs := slices.Collect(maps.Values(p.managers))
	p.mu.Unlock()
	for _, mgr := range mgrs {
		mgr.SetAutoStartGate(gate)
	}
}

// SetSpawner installs how the servers this port starts are launched. It is for a
// remote workspace, whose servers run on the remote host through the authority's
// LongRunning surface; nil restores the local lsp.Start. The spawn gate still runs
// before every start. It applies to the managers of every root, now and later.
func (p *lspPort) SetSpawner(start func(context.Context, lsp.Config) (*lsp.Client, error)) {
	p.mu.Lock()
	p.spawner = start
	managers := make([]*manager.Manager, 0, len(p.managers))
	for _, mgr := range p.managers {
		managers = append(managers, mgr)
	}
	p.mu.Unlock()
	for _, mgr := range managers {
		mgr.SetSpawner(start)
	}
}

// RestartAfterReconnect restarts the servers that were running before an authority
// reconnect, for a remote workspace: each manager restarts only its previously running
// servers, never a user-stopped server or a muted language, and never a duplicate of a
// live one (see manager.RestartAfterReconnect). ctx bounds the work and is checked
// between servers.
func (p *lspPort) RestartAfterReconnect(ctx context.Context) (int, error) {
	p.mu.Lock()
	managers := make([]*manager.Manager, 0, len(p.managers))
	for _, mgr := range p.managers {
		managers = append(managers, mgr)
	}
	p.mu.Unlock()
	total := 0
	var errs []error
	for _, mgr := range managers {
		n, err := mgr.RestartAfterReconnect(ctx)
		total += n
		if err != nil {
			errs = append(errs, err)
		}
	}
	return total, errors.Join(errs...)
}

// doc returns the record of the open document at path.
func (p *lspPort) doc(path string) (lspDocRec, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	rec, ok := p.docs[path]
	return rec, ok
}

// lookup returns the record of the open document at path and the manager of its
// root. mgr is nil when the root has no manager, which happens after a restart.
func (p *lspPort) lookup(path string) (rec lspDocRec, mgr *manager.Manager, ok bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	rec, ok = p.docs[path]
	return rec, p.managers[rec.root], ok
}

// setDoc records the document at path.
func (p *lspPort) setDoc(path string, rec lspDocRec) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.docs[path] = rec
}

// updateDocVersion records the protocol version the document is open at.
func (p *lspPort) updateDocVersion(path string, version int32) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if rec, ok := p.docs[path]; ok {
		rec.version = version
		p.docs[path] = rec
	}
}

// dropDoc forgets the document at path and the data cached for it: semantic tokens
// and pulled diagnostics describe a text and a server connection that are gone.
func (p *lspPort) dropDoc(path string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.docs, path)
	delete(p.sem, path)
	delete(p.pulled, path)
}

// textLines is the lines of a document, for the conversion between the rune columns
// of the editor and the UTF-16 positions of the protocol. A line past the end is
// empty.
type textLines []string

// newTextLines splits text on newlines, as the buffer does.
func newTextLines(text string) textLines {
	return strings.Split(text, "\n")
}

// line returns line n, or "" past the end.
func (l textLines) line(n int) string {
	if n < 0 || n >= len(l) {
		return ""
	}
	return l[n]
}

// fromLSP converts a protocol position to a rune position of the editor.
func (l textLines) fromLSP(p protocol.Position) buffer.Pos {
	n := int(p.Line)
	return buffer.Pos{Line: n, Col: lsp.UTF16ToRuneCol(l.line(n), p.Character)}
}

// toLSP converts a rune position of the editor to a protocol position.
func (l textLines) toLSP(p buffer.Pos) protocol.Position {
	return protocol.Position{Line: uint32(p.Line), Character: lsp.RuneColToUTF16(l.line(p.Line), p.Col)}
}

// rangeOf converts a protocol range, or returns nil for a nil range.
func (l textLines) rangeOf(r *protocol.Range) *tui.LSPRange {
	if r == nil {
		return nil
	}
	return &tui.LSPRange{Start: l.fromLSP(r.Start), End: l.fromLSP(r.End)}
}

// uiDiagnostics converts the diagnostics of one document to rune ranges.
func uiDiagnostics(lines textLines, ds []protocol.Diagnostic) []tui.Diagnostic {
	out := make([]tui.Diagnostic, 0, len(ds))
	for _, d := range ds {
		source, _ := d.Source.Get()
		out = append(out, tui.Diagnostic{
			Start:    lines.fromLSP(d.Range.Start),
			End:      lines.fromLSP(d.Range.End),
			Severity: int(d.Severity),
			Message:  diagnosticText(d.Message),
			Source:   source,
			Code:     diagnosticCode(d.Code),
		})
	}
	return out
}

// diagnosticCode returns a diagnostic's code as text. The protocol allows a
// number or a string; either way it is shown as it stands.
func diagnosticCode(code protocol.ProgressToken) string {
	switch v := code.(type) {
	case protocol.String:
		return string(v)
	case protocol.Integer:
		return strconv.FormatInt(int64(v), 10)
	}
	return ""
}

// diagnosticText returns the message of a diagnostic as plain text. The protocol
// allows a string or a markup value, and the markup is shown as its text.
func diagnosticText(m protocol.InlayHintTooltip) string {
	switch v := m.(type) {
	case protocol.String:
		return string(v)
	case *protocol.MarkupContent:
		if v != nil {
			return v.Value
		}
	}
	return ""
}

// plainTextEdit returns the text edit inside a document change element. A snippet
// edit is an error: inserting its text raw would leave the $ placeholders in the
// document.
func plainTextEdit(el protocol.TextDocumentEditElement) (protocol.TextEdit, error) {
	switch e := el.(type) {
	case *protocol.TextEdit:
		if e != nil {
			return *e, nil
		}
	case *protocol.AnnotatedTextEdit:
		if e != nil {
			return e.TextEdit, nil
		}
	}
	return protocol.TextEdit{}, fmt.Errorf("unsupported text edit %T", el)
}

// unavailable turns the failure to start the server of lang into the error the root model
// remembers for the language: a program that is not installed, or a server that failed its
// handshake. Any other error is returned as it is.
func unavailable(lang string, err error) error {
	if errors.Is(err, manager.ErrStopped) {
		return err
	}
	var ni *lsp.NotInstalledError
	var se *lsp.StartError
	switch {
	case errors.As(err, &ni):
		return &tui.LSPUnavailableError{Language: lang, Command: ni.Command, Install: ni.Install, Missing: true, Err: err}
	case errors.As(err, &se):
		return &tui.LSPUnavailableError{Language: lang, Command: se.Command, Err: err}
	}
	return err
}
