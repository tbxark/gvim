// Package manager runs one language server per language and tracks the documents
// open on each. It is the session layer over the client in package lsp: it starts
// the process, runs the initialize handshake, numbers document versions, and
// replaces a server whose connection has ended. It also holds the built-in
// server catalogue (Spec, Defaults, DefaultFor).
package manager

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/tbxark/gvim/lsp"
)

// ErrNoServer is returned by Manager.Open for a language with no ServerConfig.
var ErrNoServer = errors.New("lsp: no server configured for language")

// ErrNotOpen is returned by Manager.Change for a path that is not open.
var ErrNotOpen = errors.New("lsp: document is not open")

// errServerGone is returned when the server a request was using was shut down,
// or replaced, while the request was running.
var errServerGone = errors.New("lsp: server stopped during the request")

// managerTimeout is the default bound on each step the Manager takes with a
// server: starting it, each handshake request, and each document notification.
// Every step gets its own deadline of this length.
const managerTimeout = 10 * time.Second

// ServerConfig says how to launch the language server for one language. The
// process is Command[0] and its arguments are Command[1:] followed by Args.
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
	// RootMarkers are the names (or globs) of the files that mark a project root. They are
	// for the caller that picks the workspace root of a file; the Manager does not read them.
	RootMarkers []string
}

// Manager runs one language server per language ID and tracks the documents
// open on each. A server starts on the first Open for its language and keeps
// running until Shutdown. Methods are safe for concurrent use. They can block on
// server I/O, so call them from a command, not from a UI update.
//
// Every step with a server has a deadline. A write still blocked at its deadline
// closes that server. A server whose connection has ended, because it exited or
// its pipe broke, is replaced by the next request for its language. The
// documents that server held are forgotten, so the caller must Open them again.
type Manager struct {
	rootURI string
	servers map[string]ServerConfig
	start   func(context.Context, lsp.Config) (*lsp.Client, error)
	// timeout bounds each step. NewManager sets it to managerTimeout.
	timeout time.Duration

	// sendMu orders the calls that change server state: Open, Change and Close. It
	// is held only across the write of each message, so the didOpen, didChange and
	// didClose of one document reach a server in the order their versions were
	// assigned. A server start does not hold it, so a slow start never blocks Change
	// or Close. Shutdown does not take it, so a stalled write never blocks Shutdown.
	sendMu sync.Mutex

	// mu guards the fields below. It is held only to read or update them, never
	// across a write or a server start.
	mu      sync.Mutex
	clients map[string]*lsp.Client  // by language ID
	starts  map[string]*serverStart // by language ID, while a server is starting
	docs    map[string]openDoc      // by file path
	// epoch counts Shutdown calls, so that a server started across one is not kept.
	epoch uint64
}

// openDoc is a document the Manager has sent didOpen for.
type openDoc struct {
	languageID string
	uri        string
	version    int32
}

// serverStart is a start of the server for one language that is still running. A
// caller that needs that server while it starts waits on done and shares the result.
type serverStart struct {
	done chan struct{}
	c    *lsp.Client
	err  error
}

// NewManager returns a Manager for the workspace at root, an absolute directory
// path, or "" for none. It starts servers from servers. Nothing is started until
// Open.
func NewManager(root string, servers map[string]ServerConfig) *Manager {
	rootURI := ""
	if root != "" {
		rootURI = lsp.PathURI(root)
	}
	return &Manager{
		rootURI: rootURI,
		servers: servers,
		start:   lsp.Start,
		timeout: managerTimeout,
		clients: make(map[string]*lsp.Client),
		starts:  make(map[string]*serverStart),
		docs:    make(map[string]openDoc),
	}
}

// Open sends didOpen for the document at path with its full text, starting and
// initializing the server for languageID first if it is not running. It returns
// ErrNoServer when languageID has no ServerConfig. Opening a path that is already
// open closes the old document first.
func (m *Manager) Open(path, languageID, text string) error {
	c, err := m.serverFor(languageID)
	if err != nil {
		return err
	}
	if id := m.servers[languageID].LanguageID; id != "" {
		languageID = id
	}

	m.sendMu.Lock()
	defer m.sendMu.Unlock()
	if err := m.closeDoc(path); err != nil {
		return err
	}
	docURI := lsp.PathURI(path)
	err = m.step(context.Background(), c, func(ctx context.Context) error {
		return c.DidOpen(ctx, docURI, languageID, 1, text)
	})
	if err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.clients[languageID] != c {
		return errServerGone
	}
	m.docs[path] = openDoc{languageID: languageID, uri: docURI, version: 1}
	return nil
}

// Change sends didChange with the full new text of the open document at path. It
// returns ErrNotOpen when path is not open, which includes a path whose server
// has exited.
func (m *Manager) Change(path, text string) error {
	m.sendMu.Lock()
	defer m.sendMu.Unlock()

	c, doc, err := m.advance(path)
	if err != nil {
		return err
	}
	return m.step(context.Background(), c, func(ctx context.Context) error {
		return c.DidChange(ctx, doc.uri, doc.version, text)
	})
}

// Close sends didClose for the document at path and forgets it. Closing a path
// that is not open does nothing. The server keeps running; see Shutdown.
func (m *Manager) Close(path string) error {
	m.sendMu.Lock()
	defer m.sendMu.Unlock()

	return m.closeDoc(path)
}

// ClientFor returns the client whose server holds the open document at path. It
// returns false when path is not open, or when that server has exited.
func (m *Manager) ClientFor(path string) (*lsp.Client, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	doc, ok := m.docs[path]
	if !ok {
		return nil, false
	}
	return m.liveClientLocked(doc.languageID)
}

// Shutdown shuts down, exits and closes every running server, and forgets all
// open documents. A later Open starts a server again. The returned error joins
// the errors from all of the servers. Each request is bounded by ctx and by the
// Manager's timeout, so Shutdown returns even when a server has stopped reading.
func (m *Manager) Shutdown(ctx context.Context) error {
	m.mu.Lock()
	m.epoch++
	clients := m.clients
	m.clients = make(map[string]*lsp.Client)
	m.starts = make(map[string]*serverStart)
	m.docs = make(map[string]openDoc)
	m.mu.Unlock()

	var errs []error
	for _, c := range clients {
		errs = append(errs, m.shutDown(ctx, c))
	}
	return errors.Join(errs...)
}

// shutDown sends shutdown and exit to c, then closes it.
func (m *Manager) shutDown(ctx context.Context, c *lsp.Client) error {
	err := m.step(ctx, c, c.Shutdown)
	exitErr := m.step(ctx, c, c.Exit)
	return errors.Join(err, exitErr, c.Close())
}

// serverFor returns the running server for languageID. If there is none, it starts
// and initializes one, unless a start for languageID is already running: then it
// waits for that start and shares its result. No lock is held across the start, so
// ClientFor, Change, Close and Shutdown are not blocked by it.
func (m *Manager) serverFor(languageID string) (*lsp.Client, error) {
	m.mu.Lock()
	if c, live := m.liveClientLocked(languageID); live {
		m.mu.Unlock()
		return c, nil
	}
	if s, ok := m.starts[languageID]; ok {
		m.mu.Unlock()
		<-s.done
		return s.c, s.err
	}
	sc, ok := m.servers[languageID]
	if !ok {
		m.mu.Unlock()
		return nil, fmt.Errorf("%w: %q", ErrNoServer, languageID)
	}
	s := &serverStart{done: make(chan struct{})}
	m.starts[languageID] = s
	epoch := m.epoch
	m.mu.Unlock()

	c, err := m.startServer(languageID, sc)

	m.mu.Lock()
	if m.starts[languageID] == s {
		delete(m.starts, languageID)
	}
	if err == nil && m.epoch != epoch {
		// Shutdown ran during the start, so the new server is not kept.
		err = errServerGone
	}
	if err == nil {
		m.clients[languageID] = c
	}
	m.mu.Unlock()
	if err != nil {
		if c != nil {
			_ = c.Close()
		}
		c = nil
	}
	s.c, s.err = c, err
	close(s.done)
	return c, err
}

// startServer launches the server for sc and runs the initialize handshake. Each
// step has its own deadline. A server that fails a step is closed. A failure names the
// language and the server: a program that is not installed is a *lsp.NotInstalledError that
// carries the install hint, and a handshake that fails is a *lsp.StartError with the command
// line and what the server wrote to its error output.
func (m *Manager) startServer(lang string, sc ServerConfig) (*lsp.Client, error) {
	command := append(slices.Clone(sc.Command), sc.Args...)
	ctx, cancel := context.WithTimeout(context.Background(), m.timeout)
	c, err := m.start(ctx, lsp.Config{
		Command: command,
		RootURI: m.rootURI,
	})
	cancel()
	if err != nil {
		var ni *lsp.NotInstalledError
		if errors.As(err, &ni) {
			ni.Language, ni.Install = lang, sc.Install
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
		name := sc.Name
		if name == "" && len(sc.Command) > 0 {
			name = sc.Command[0]
		}
		return nil, &lsp.StartError{Language: lang, Name: name, Command: strings.Join(command, " "), Stderr: c.StderrTail(), Err: err}
	}
	return c, nil
}

// advance records the next version of the open document at path. It returns the
// document's server and the document with that version. m.sendMu must be held.
func (m *Manager) advance(path string) (*lsp.Client, openDoc, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	doc, ok := m.docs[path]
	if !ok {
		return nil, openDoc{}, fmt.Errorf("%w: %s", ErrNotOpen, path)
	}
	c, live := m.liveClientLocked(doc.languageID)
	if !live {
		return nil, openDoc{}, fmt.Errorf("%w: %s: its server exited", ErrNotOpen, path)
	}
	doc.version++
	m.docs[path] = doc
	return c, doc, nil
}

// closeDoc forgets the document at path and sends didClose if it was open. A
// server that has exited holds nothing to close, so none is sent. m.sendMu must
// be held.
func (m *Manager) closeDoc(path string) error {
	c, doc, live := m.forget(path)
	if !live {
		return nil
	}
	return m.step(context.Background(), c, func(ctx context.Context) error {
		return c.DidClose(ctx, doc.uri)
	})
}

// forget removes the document at path from the open set and returns the document
// and its server. It reports false when the document was not open or its server
// has exited.
func (m *Manager) forget(path string) (*lsp.Client, openDoc, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	doc, ok := m.docs[path]
	if !ok {
		return nil, openDoc{}, false
	}
	delete(m.docs, path)
	c, live := m.liveClientLocked(doc.languageID)
	return c, doc, live
}

// liveClientLocked returns the running client for languageID. A client whose
// connection has ended is closed in the background and forgotten, together with
// the documents it held, so that the next request starts a new server. m.mu must
// be held.
func (m *Manager) liveClientLocked(languageID string) (*lsp.Client, bool) {
	c, ok := m.clients[languageID]
	if !ok {
		return nil, false
	}
	select {
	case <-c.Done():
	default:
		return c, true
	}
	delete(m.clients, languageID)
	maps.DeleteFunc(m.docs, func(_ string, d openDoc) bool { return d.languageID == languageID })
	go func() { _ = c.Close() }()
	return nil, false
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
