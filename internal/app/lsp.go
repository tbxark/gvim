package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"slices"
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
	"github.com/tbxark/gvim/pluginapi"
)

// lspRequestTimeout bounds one request to a server. The Manager bounds its own
// steps (start, handshake, document notifications) separately. It is a variable so
// that a test can shorten it.
var lspRequestTimeout = 10 * time.Second

// lspStopTimeout bounds the shutdown of the servers, on lsp_stop and at exit.
const lspStopTimeout = 5 * time.Second

// newLSPPort returns the language-server port for the servers of the config, or
// nil when no server is configured, so that no LSP feature is on. Nothing starts
// here: a server starts when its first document opens.
func newLSPPort(servers map[string]config.LSPServer) tui.LSPSession {
	if len(servers) == 0 {
		return nil
	}
	cfg := make(map[string]lsp.ServerConfig, len(servers))
	for lang, s := range servers {
		cfg[lang] = lsp.ServerConfig{Command: []string{s.Command}, Args: slices.Clone(s.Args)}
	}
	return &lspPort{servers: cfg, managers: make(map[string]*lsp.Manager), docs: make(map[string]lspDocRec)}
}

// lspPort is the tui.LSPSession over the language servers. It keeps one lsp.Manager
// per workspace root, created on the first use of the root. It also keeps the
// text of each open document, which converts the positions of the protocol to rune
// columns. Its methods may block on a server, so the root calls them only from
// commands.
type lspPort struct {
	servers map[string]lsp.ServerConfig // by language ID: the servers that start on their own; read only after construction
	// startable is every server that can run, the ones in servers and the ones that only
	// start by hand. Nil in a port built from servers alone.
	startable map[string]lsp.ServerConfig
	info      map[string]tui.LSPServerInfo // what the config does with each language; read only too

	mu       sync.Mutex
	root     string
	managers map[string]*lsp.Manager // by root
	docs     map[string]lspDocRec    // by path: the documents the port has open
}

// lspDocRec is one open document: the root whose manager holds it, its language
// ID and the text the server was last sent. A record is replaced, never changed. lines is
// text split on newlines, made once when the text is set, so that the diagnostics poll and
// each request do not split the whole document again.
type lspDocRec struct {
	root, lang, text string
	lines            textLines
}

// SetRoot names the workspace root that new documents are opened under.
func (p *lspPort) SetRoot(root string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.root = root
}

// Supports reports whether a server starts on its own for lang.
func (p *lspPort) Supports(lang string) bool {
	_, ok := p.servers[lang]
	return ok
}

// runnable is the servers the managers can start: all of startable, or the servers that start
// on their own when the port has no other.
func (p *lspPort) runnable() map[string]lsp.ServerConfig {
	if p.startable != nil {
		return p.startable
	}
	return p.servers
}

// Open sends the text of the document at path, starting its server if needed. A
// document that fails to open is not kept.
func (p *lspPort) Open(path, lang, text string) error {
	lang = openLanguage(path, lang)
	p.mu.Lock()
	root := p.root
	p.mu.Unlock()
	root = serverRoot(path, p.runnable()[lang].RootMarkers, root)
	mgr := p.managerFor(root)
	p.setDoc(path, lspDocRec{root: root, lang: lang, text: text, lines: newTextLines(text)})
	if err := mgr.Open(path, lang, text); err != nil {
		p.dropDoc(path)
		return unavailable(lang, err)
	}
	return nil
}

// Change sends the new full text of the open document at path. A document whose
// server has exited is reported as not open, and the root opens it again.
func (p *lspPort) Change(path, text string) error {
	rec, mgr, ok := p.lookup(path)
	if !ok || mgr == nil {
		return fmt.Errorf("%w: %s", lsp.ErrNotOpen, path)
	}
	p.setDoc(path, lspDocRec{root: rec.root, lang: rec.lang, text: text, lines: newTextLines(text)})
	return mgr.Change(path, text)
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

// Diagnostics returns the latest diagnostics the server published for the document
// at path. ok is false when there are none yet, or the document is not open.
func (p *lspPort) Diagnostics(path string) (tui.DiagnosticsEvent, bool) {
	rec, mgr, ok := p.lookup(path)
	if !ok || mgr == nil {
		return tui.DiagnosticsEvent{}, false
	}
	c, ok := mgr.ClientFor(path)
	if !ok {
		return tui.DiagnosticsEvent{}, false
	}
	ev, ok := c.DiagnosticsFor(lsp.PathURI(path))
	if !ok {
		return tui.DiagnosticsEvent{}, false
	}
	return tui.DiagnosticsEvent{Path: path, Items: uiDiagnostics(rec.lines, ev.Diagnostics)}, true
}

// Definition returns the definitions of the symbol at at.
func (p *lspPort) Definition(path string, at buffer.Pos) ([]tui.LSPLocation, error) {
	return p.locations(path, at, func(ctx context.Context, c *lsp.Client, u string, pos protocol.Position) ([]protocol.Location, error) {
		return c.Definition(ctx, u, pos)
	})
}

// Implementation returns the implementations of the symbol at at.
func (p *lspPort) Implementation(path string, at buffer.Pos) ([]tui.LSPLocation, error) {
	return p.locations(path, at, func(ctx context.Context, c *lsp.Client, u string, pos protocol.Position) ([]protocol.Location, error) {
		return c.Implementation(ctx, u, pos)
	})
}

// References returns the references to the symbol at at, with its declaration.
func (p *lspPort) References(path string, at buffer.Pos) ([]tui.LSPLocation, error) {
	return p.locations(path, at, func(ctx context.Context, c *lsp.Client, u string, pos protocol.Position) ([]protocol.Location, error) {
		return c.References(ctx, u, pos, true)
	})
}

// Hover returns the hover text at at.
func (p *lspPort) Hover(path string, at buffer.Pos) (string, error) {
	var text string
	err := p.call(path, at, func(ctx context.Context, c *lsp.Client, u string, pos protocol.Position) error {
		var err error
		text, err = c.Hover(ctx, u, pos)
		return err
	})
	return text, err
}

// Completion returns the completion items at at. A range the server gave is in the
// rune columns of the document, as the editor needs it.
func (p *lspPort) Completion(path string, at buffer.Pos) ([]tui.CompletionItem, error) {
	var items []lsp.CompletionItem
	err := p.call(path, at, func(ctx context.Context, c *lsp.Client, u string, pos protocol.Position) error {
		var err error
		items, err = c.Completion(ctx, u, pos)
		return err
	})
	if err != nil {
		return nil, err
	}
	lines := newTextLines(p.textOf(path))
	out := make([]tui.CompletionItem, len(items))
	for i, it := range items {
		out[i] = tui.CompletionItem{Label: it.Label, Detail: it.Detail, Insert: it.InsertText, Range: lines.rangeOf(it.EditRange)}
	}
	return out, nil
}

// Rename renames the symbol at at to name. It returns the edits per file, in order
// of path.
func (p *lspPort) Rename(path string, at buffer.Pos, name string) ([]tui.FileEdit, error) {
	var edits map[string][]protocol.TextEdit
	err := p.call(path, at, func(ctx context.Context, c *lsp.Client, u string, pos protocol.Position) error {
		var err error
		edits, err = c.Rename(ctx, u, pos, name)
		return err
	})
	if err != nil {
		return nil, err
	}
	return fileEdits(edits, p.textOf), nil
}

// CodeActions returns the titles of the code actions at at.
func (p *lspPort) CodeActions(path string, at buffer.Pos) ([]string, error) {
	var actions []lsp.CodeAction
	err := p.call(path, at, func(ctx context.Context, c *lsp.Client, u string, pos protocol.Position) error {
		var err error
		actions, err = c.CodeActions(ctx, u, protocol.Range{Start: pos, End: pos})
		return err
	})
	if err != nil {
		return nil, err
	}
	titles := make([]string, len(actions))
	for i, a := range actions {
		titles[i] = a.Title
	}
	return titles, nil
}

// maxRequestResult bounds the raw JSON result of Request that is decoded: a plugin must
// not be able to make the editor build a value of any size.
const maxRequestResult = 4 << 20

// Request sends one request of the protocol to the server that has the document at path
// open, and returns the decoded JSON result. The caller bounds nothing: the call is
// limited by lspRequestTimeout, and the result by maxRequestResult.
func (p *lspPort) Request(path, method string, params any) (any, error) {
	c, _, err := p.client(path)
	if err != nil {
		if errors.Is(err, lsp.ErrNotOpen) {
			return nil, pluginapi.Errf(pluginapi.CodeNotFound, "%v", err)
		}
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), lspRequestTimeout)
	defer cancel()
	raw, err := c.Request(ctx, method, params)
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

// Restart shuts down the servers of the current root. The Manager forgets their
// documents, and the root opens them again on its next sync.
func (p *lspPort) Restart() error {
	p.mu.Lock()
	mgrs := slices.Collect(maps.Values(p.managers))
	p.managers = make(map[string]*lsp.Manager)
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
	p.mu.Lock()
	mgrs := slices.Collect(maps.Values(p.managers))
	p.managers = make(map[string]*lsp.Manager)
	p.docs = make(map[string]lspDocRec)
	p.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), lspStopTimeout)
	defer cancel()
	var errs []error
	for _, mgr := range mgrs {
		errs = append(errs, mgr.Shutdown(ctx))
	}
	return errors.Join(errs...)
}

// locations runs a request that returns locations, and converts the result to rune
// positions of each file.
func (p *lspPort) locations(path string, at buffer.Pos, ask func(context.Context, *lsp.Client, string, protocol.Position) ([]protocol.Location, error)) ([]tui.LSPLocation, error) {
	var locs []protocol.Location
	err := p.call(path, at, func(ctx context.Context, c *lsp.Client, u string, pos protocol.Position) error {
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

// call runs one request for the document at path. fn gets a context bounded by
// lspRequestTimeout, the client, the document's URI and the protocol position of at.
func (p *lspPort) call(path string, at buffer.Pos, fn func(context.Context, *lsp.Client, string, protocol.Position) error) error {
	c, rec, err := p.client(path)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), lspRequestTimeout)
	defer cancel()
	return fn(ctx, c, lsp.PathURI(path), rec.lines.toLSP(at))
}

// client returns the client of the document at path. When its server has exited,
// the document is opened again, which starts the server.
func (p *lspPort) client(path string) (*lsp.Client, lspDocRec, error) {
	rec, ok := p.doc(path)
	if !ok {
		return nil, lspDocRec{}, fmt.Errorf("%w: %s", lsp.ErrNotOpen, path)
	}
	mgr := p.managerFor(rec.root)
	if c, ok := mgr.ClientFor(path); ok {
		return c, rec, nil
	}
	if err := mgr.Open(path, rec.lang, rec.text); err != nil {
		return nil, lspDocRec{}, err
	}
	c, ok := mgr.ClientFor(path)
	if !ok {
		return nil, lspDocRec{}, fmt.Errorf("lsp: server for %s is not running", path)
	}
	return c, rec, nil
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

// managerFor returns the manager of root, creating it on first use.
func (p *lspPort) managerFor(root string) *lsp.Manager {
	p.mu.Lock()
	defer p.mu.Unlock()
	mgr, ok := p.managers[root]
	if !ok {
		mgr = lsp.NewManager(root, p.runnable())
		p.managers[root] = mgr
	}
	return mgr
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
func (p *lspPort) lookup(path string) (rec lspDocRec, mgr *lsp.Manager, ok bool) {
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

// dropDoc forgets the document at path.
func (p *lspPort) dropDoc(path string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.docs, path)
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
		})
	}
	return out
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

// fileEdits converts the edits of a rename, keyed by document URI, to the edits of
// each file. text gives the text of a path, which the positions are converted with.
// The files are in order of their paths.
func fileEdits(edits map[string][]protocol.TextEdit, text func(path string) string) []tui.FileEdit {
	keys := slices.Collect(maps.Keys(edits))
	slices.Sort(keys)
	out := make([]tui.FileEdit, 0, len(keys))
	for _, k := range keys {
		file := uri.URI(k).FsPath()
		lines := newTextLines(text(file))
		fe := tui.FileEdit{Path: file}
		for _, e := range edits[k] {
			fe.Edits = append(fe.Edits, editor.LSPEdit{
				From: lines.fromLSP(e.Range.Start),
				To:   lines.fromLSP(e.Range.End),
				Text: e.NewText,
			})
		}
		out = append(out, fe)
	}
	return out
}
