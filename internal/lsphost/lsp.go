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
		spawnEnv:  spawnEnv,
		servers:   plan.servers,
		universal: plan.universal,
		info:      plan.info,
		auto:      make(map[string]bool, len(plan.auto)),
		startable: make(map[string]bool, len(plan.startable)),
		managers:  make(map[string]*manager.Manager),
		docs:      make(map[string]lspDocRec),
		muted:     make(map[string]bool),
		forced:    make(map[string]bool),
		sem:       make(map[string]semanticCache),
		pulled:    make(map[string]pulledDiagnostics),
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
	// spawner, when set, starts the server instead of lsp.Start: a remote workspace
	// runs the server on the remote host through the authority's LongRunning surface
	// (docs/authority.md). The manager's spawn gate still runs before it.
	spawner   func(context.Context, lsp.Config) (*lsp.Client, error)
	servers   map[string][]manager.ServerConfig // by language ID; read only after construction
	universal []manager.ServerConfig
	info      map[string]tui.LSPServerInfo // read only after construction
	auto      map[string]bool              // languages with a server that starts on its own
	startable map[string]bool              // languages with any configured server
	status    []tui.LSPServerStatus        // every configured server, for the popup

	mu       sync.Mutex
	root     string
	managers map[string]*manager.Manager // by root
	docs     map[string]lspDocRec        // by path: the documents the port has open
	// pending holds the workspace edits servers asked for, in arrival order. They
	// are converted to the port's form when the root drains them, because the
	// conversion reads the files the edits touch.
	pending []pendingEdit
	// muted holds the languages the user muted; forced holds the languages
	// started by hand, which start their servers whatever auto_start says.
	muted  map[string]bool
	forced map[string]bool
	// sem caches the semantic-token data of open documents, and pulled the
	// server-pulled diagnostics. Both are keyed by path and invalidated when the
	// document closes, or on a restart.
	sem    map[string]semanticCache
	pulled map[string]pulledDiagnostics
	// wsPull caches the workspace/diagnostic answer: the pull is throttled and
	// re-asked when the capabilities change or the set of open documents does.
	wsPull workspacePull
	// wsPulls tracks the workspace/diagnostic pulls in flight so a newer pull, or a
	// stop/restart of the servers, cancels the old ones (their $/cancelRequest goes
	// out through the client) instead of letting them answer into the cache.
	wsPulls pullSet
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
	defer p.mu.Unlock()
	p.root = root
}

// Supports reports whether a server starts on its own for lang.
func (p *lspPort) Supports(lang string) bool {
	return p.auto[lang]
}

// Open sends the text of the document at path, starting its servers if needed. A
// document that fails to open is not kept.
func (p *lspPort) Open(path, lang, text string) (int32, error) {
	lang = openLanguage(path, lang)
	p.mu.Lock()
	root := p.root
	p.mu.Unlock()
	root = serverRoot(path, p.rootMarkers(lang), root)
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

// Completion returns the completion items at at, merged over the servers that
// serve completion for the language. A range the server gave is in the rune
// columns of the document, as the editor needs it.
func (p *lspPort) Completion(path string, at buffer.Pos) ([]tui.CompletionItem, error) {
	hs, rec, err := p.handles(path, manager.FeatureCompletion)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), lspRequestTimeout)
	defer cancel()
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
		for _, it := range items {
			out = append(out, tui.CompletionItem{Label: it.Label, Detail: it.Detail, Insert: it.InsertText, Range: rec.lines.rangeOf(it.EditRange)})
		}
	}
	if len(out) == 0 && len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return out, nil
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
	p.mu.Lock()
	p.forced[language] = true
	delete(p.muted, language)
	mgrs := slices.Collect(maps.Values(p.managers))
	p.mu.Unlock()
	var errs []error
	for _, mgr := range mgrs {
		errs = append(errs, mgr.StartLanguage(language))
	}
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
	if language != "" {
		p.forced[language] = true
		delete(p.muted, language)
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
	p.mu.Unlock()
	running := make(map[string]manager.ServerStatus)
	for _, mgr := range mgrs {
		for _, st := range mgr.Servers() {
			running[st.Language+"\x00"+st.Name] = st
		}
	}
	out := make([]tui.LSPServerStatus, 0, len(p.status))
	for _, st := range p.status {
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
// the mute says.
func (p *lspPort) RestartServer(language, name string) error {
	// The pull may be asking this very server: cancel it before the restart.
	p.wsPulls.cancelAll()
	mgr := p.managerForServer(language, name)
	p.mu.Lock()
	delete(p.muted, language)
	p.mu.Unlock()
	return mgr.RestartServer(language, name)
}

// StopServer stops the named server.
func (p *lspPort) StopServer(language, name string) error {
	p.wsPulls.cancelAll()
	return p.managerForServer(language, name).StopServer(language, name)
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

// managerForServer returns the manager that has the named server, or the one of the
// current root when none does.
func (p *lspPort) managerForServer(language, name string) *manager.Manager {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, mgr := range p.managers {
		for _, st := range mgr.Servers() {
			if st.Language == language && (name == "" || st.Name == name) {
				return mgr
			}
		}
	}
	root := p.root
	if mgr, ok := p.managers[root]; ok {
		return mgr
	}
	mgr := p.newManagerLocked(root)
	p.managers[root] = mgr
	return mgr
}

// locations runs a request that returns locations, and converts the result to rune
// positions of each file.
func (p *lspPort) locations(path string, at buffer.Pos, feature manager.Feature, ask func(context.Context, *lsp.Client, string, protocol.Position) ([]protocol.Location, error)) ([]tui.LSPLocation, error) {
	var locs []protocol.Location
	err := p.call(path, at, feature, func(ctx context.Context, c *lsp.Client, u string, pos protocol.Position) error {
		var err error
		locs, err = ask(ctx, c, u, pos)
		return err
	})
	if err != nil {
		return nil, err
	}
	texts := make(map[string]textLines)
	out := make([]tui.LSPLocation, 0, len(locs))
	for _, l := range locs {
		file := uri.URI(l.URI).FsPath()
		lines, ok := texts[file]
		if !ok {
			lines = newTextLines(p.textOf(file))
			texts[file] = lines
		}
		out = append(out, tui.LSPLocation{Path: file, Start: lines.fromLSP(l.Range.Start), End: lines.fromLSP(l.Range.End)})
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
	if configs := p.servers[language]; len(configs) > 0 {
		return configs[0].RootMarkers
	}
	return nil
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
	mgr := manager.NewManager(root, p.servers, p.universal)
	mgr.SetApplyEdit(p.enqueueEdit)
	mgr.SetSpawnEnv(p.spawnEnv)
	mgr.SetGate(p.spawnGate)
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
