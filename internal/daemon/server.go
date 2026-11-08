package daemon

// Server hosts one editor program over the daemon socket: tui.RunWithIO reads the
// attached clients' input and writes frames through this type. Several clients may be
// attached at once (Fresh's shared-view model): every frame goes to every client, input
// from any client reaches the program, and the program is told the SMALLEST attached
// client's size, so a larger terminal letterboxes instead of cropping the shared view.
// Detaching (or a dropped connection) removes only that client; the program keeps
// running, and the next attach repaints it.
//
// Every write to a client goes through that client's FrameWriter, so frames from the
// program output goroutine, the pong answers, a detach and the exit frame never
// interleave. The frame writers carry a write deadline (frameWriteTimeout), so a slow
// or wedged client delays one bounded write and is then detached: the program output
// path and the shutdown path never block forever on a client.

import (
	"context"
	"errors"
	"io"
	"net"
	"path/filepath"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
)

// frameWriteTimeout bounds one write to an attached client.
const frameWriteTimeout = 2 * time.Second

// deadlineWriter applies a per-write deadline to a connection, so a frame write cannot
// block forever on a slow client. FrameWriter serializes the calls, so the deadline set
// and clear never race with another writer on the same connection.
type deadlineWriter struct {
	conn    net.Conn
	timeout time.Duration
}

func (d deadlineWriter) Write(p []byte) (int, error) {
	_ = d.conn.SetWriteDeadline(time.Now().Add(d.timeout))
	n, err := d.conn.Write(p)
	_ = d.conn.SetWriteDeadline(time.Time{})
	return n, err
}

// clientConn is one attached client: its connection, the serialized frame writer and
// its last known terminal size (0,0 before a valid hello).
type clientConn struct {
	conn net.Conn
	fw   *FrameWriter
	cols int
	rows int
}

// Server is the daemon side of the attach protocol. It implements io.Reader (all
// clients' terminal input) and io.Writer (the shared frame output) for the hosted
// program.
type Server struct {
	ln   net.Listener
	path string

	mu      sync.Mutex
	clients map[*clientConn]struct{}
	prog    *tea.Program
	// openFiles, when set, opens the paths of a client's open_file frame (the legacy
	// callback: bare paths only).
	openFiles func(paths []string)
	// openFilesAt, when set, receives the full open_file request (client cwd, bare
	// Paths for older clients, positional Files for current ones).
	openFilesAt func(req OpenFile)
	// workspace, when set, answers the workspace_* frames (list/select/new/close/
	// rename). The app wires it to the workspace layer; nil means the protocol is not
	// available in this daemon and the client gets a notice.
	workspace func(WorkspaceRequest) WorkspaceResult

	readMu sync.Mutex
	left   []byte
	input  chan []byte
	stop   chan struct{}

	closeOnce sync.Once
	closed    bool
}

// NewServer wraps a bound listener. path is the socket path it was bound at (for the
// startup line and the client names).
func NewServer(ln net.Listener, path string) *Server {
	return &Server{ln: ln, path: path, clients: map[*clientConn]struct{}{}, input: make(chan []byte), stop: make(chan struct{})}
}

// Path is the socket path of the server.
func (s *Server) Path() string { return s.path }

// Name is the socket name of the server: the file name without .sock.
func (s *Server) Name() string { return strings.TrimSuffix(filepath.Base(s.path), ".sock") }

// OnOpenFiles sets the callback that opens a client's requested paths (the legacy
// bare-path callback; OnOpenFilesAt takes precedence when both are set).
func (s *Server) OnOpenFiles(fn func(paths []string)) {
	s.mu.Lock()
	s.openFiles = fn
	s.mu.Unlock()
}

// OnOpenFilesAt sets the callback that receives the whole open_file request: the client's
// cwd and either the positional Files or the legacy Paths. The daemon resolves relative
// paths against req.Cwd.
func (s *Server) OnOpenFilesAt(fn func(req OpenFile)) {
	s.mu.Lock()
	s.openFilesAt = fn
	s.mu.Unlock()
}

// OnWorkspaceRequest sets the callback that answers the workspace frames. Without it a
// workspace frame is answered with a notice (never a disconnect).
func (s *Server) OnWorkspaceRequest(fn func(WorkspaceRequest) WorkspaceResult) {
	s.mu.Lock()
	s.workspace = fn
	s.mu.Unlock()
}

// Serve accepts clients until the server is closed or ctx is done. It returns when
// the listener closes; callers run it in a goroutine before starting the program.
func (s *Server) Serve(ctx context.Context) {
	go func() {
		select {
		case <-ctx.Done():
			_ = s.Close()
		case <-s.stop:
		}
	}()
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			return
		}
		go s.handleConn(conn)
	}
}

// negotiatedSize is the size the program is told: the smallest attached client's size
// (0,0 when no client has sent a valid hello). Tests read it directly.
func (s *Server) negotiatedSize() (cols, rows int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sizeLocked()
}

// sizeLocked is the negotiated size: the smallest size among the attached clients. A
// client that has not sent a valid hello yet does not constrain the shared view.
func (s *Server) sizeLocked() (cols, rows int) {
	first := true
	for c := range s.clients {
		if c.cols <= 0 || c.rows <= 0 {
			continue
		}
		if first || c.cols < cols {
			cols = c.cols
		}
		if first || c.rows < rows {
			rows = c.rows
		}
		first = false
	}
	return cols, rows
}

// sendSizeLocked tells the program the smallest attached size. It must be called with
// s.mu held; it never sends two messages for a no-size state.
func (s *Server) sendSizeLocked() {
	p := s.prog
	if p == nil {
		return
	}
	cols, rows := s.sizeLocked()
	if cols <= 0 || rows <= 0 {
		return
	}
	p.Send(tea.WindowSizeMsg{Width: cols, Height: rows})
}

// OnProgram receives the hosted program, so an attach can resize and repaint it. When
// clients already attached before the program existed, the negotiated size is sent now:
// the daemon would otherwise start painting at no size.
func (s *Server) OnProgram(p *tea.Program) {
	s.mu.Lock()
	s.prog = p
	cols, rows := s.sizeLocked()
	s.mu.Unlock()
	if p != nil && cols > 0 && rows > 0 {
		// Send from a goroutine: Program.Send before Run blocks until the program's
		// loop starts, and OnProgram runs before Run in the daemon host.
		go func() {
			p.Send(tea.WindowSizeMsg{Width: cols, Height: rows})
			p.Send(tea.RequestWindowSize())
		}()
	}
}

// Program returns the hosted program, or nil before RunWithIO started it.
func (s *Server) Program() *tea.Program {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.prog
}

// Detach sends the detached frame to every attached client and drops them. The hosted
// program is untouched; with no clients it keeps running and discards output.
func (s *Server) Detach() {
	s.mu.Lock()
	clients := s.clients
	s.clients = map[*clientConn]struct{}{}
	s.mu.Unlock()
	for c := range clients {
		_ = c.fw.WriteFrame(TypeDetached, nil)
		_ = c.conn.Close()
	}
}

// Close sends the exit frame to every attached client, stops the listener and the
// input reader, and closes the connections. The frame writes are bounded by each
// client's deadline writer, so a stalled client cannot hold shutdown.
func (s *Server) Close() error {
	var err error
	s.closeOnce.Do(func() {
		s.mu.Lock()
		s.closed = true
		clients := s.clients
		s.clients = map[*clientConn]struct{}{}
		s.mu.Unlock()
		close(s.stop)
		for c := range clients {
			_ = c.fw.WriteFrame(TypeExit, nil)
			_ = c.conn.Close()
		}
		err = s.ln.Close()
	})
	if errors.Is(err, net.ErrClosed) {
		return nil
	}
	return err
}

// removeClient drops c (if still attached), renegotiates the size and tells the program
// about the remaining clients' smaller/larger size. It is safe to call twice.
func (s *Server) removeClient(c *clientConn) {
	s.mu.Lock()
	_, ok := s.clients[c]
	if ok {
		delete(s.clients, c)
	}
	s.mu.Unlock()
	if ok {
		s.mu.Lock()
		s.sendSizeLocked()
		s.mu.Unlock()
	}
}

// handleConn runs one attached client: it reads the hello (including the protocol
// version), then input/resize/detach frames until the connection closes.
func (s *Server) handleConn(conn net.Conn) {
	defer func() { _ = conn.Close() }()
	// One writer for this connection: every server frame to it goes through it.
	fw := NewFrameWriter(deadlineWriter{conn: conn, timeout: frameWriteTimeout})
	f, err := ReadFrame(conn)
	if err != nil || f.Type != TypeHello {
		return
	}
	var hello Hello
	if err := DecodeJSON(f, &hello); err != nil {
		return
	}
	// Version handshake before any state changes: an incompatible client is told clearly
	// and dropped; the session and other clients are untouched.
	if hello.Version != ProtocolVersion {
		_ = fw.WriteJSON(TypeVersionRejected, VersionMismatch{Client: hello.Version, Daemon: ProtocolVersion})
		return
	}
	c := &clientConn{conn: conn, fw: fw, cols: hello.Cols, rows: hello.Rows}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.clients[c] = struct{}{}
	s.sendSizeLocked()
	prog := s.prog
	s.mu.Unlock()
	if prog != nil {
		// A fresh frame for the new client (and a shared repaint at the negotiated size).
		prog.Send(tea.RequestWindowSize())
	}
	defer s.removeClient(c)
	for {
		f, err := ReadFrame(conn)
		if err != nil {
			return
		}
		if req, ok, derr := decodeWorkspaceRequest(f); ok {
			if derr != nil {
				_ = fw.WriteNotice(derr.Error())
				continue
			}
			s.mu.Lock()
			hook := s.workspace
			s.mu.Unlock()
			if hook == nil {
				_ = fw.WriteNotice("this daemon has no workspace layer")
				continue
			}
			res := hook(req)
			if res.Notice != "" {
				_ = fw.WriteNotice(res.Notice)
			}
			if res.List != nil {
				if err := fw.WriteJSON(TypeWorkspaces, *res.List); err != nil {
					return
				}
			}
			continue
		}
		switch f.Type {
		case TypeInput:
			if len(f.Payload) == 0 {
				continue
			}
			select {
			case s.input <- f.Payload:
			case <-s.stop:
				return
			}
		case TypeResize:
			var r Resize
			if DecodeJSON(f, &r) != nil || r.Cols <= 0 || r.Rows <= 0 {
				continue
			}
			s.mu.Lock()
			c.cols, c.rows = r.Cols, r.Rows
			s.sendSizeLocked()
			s.mu.Unlock()
		case TypeOpenFile:
			var req OpenFile
			if DecodeJSON(f, &req) != nil || (len(req.Paths) == 0 && len(req.Files) == 0) {
				continue
			}
			s.mu.Lock()
			openAt, open := s.openFilesAt, s.openFiles
			s.mu.Unlock()
			switch {
			case openAt != nil:
				openAt(req)
			case open != nil:
				open(req.Paths)
			}
		case TypeDetach:
			_ = fw.WriteFrame(TypeDetached, nil)
			return
		case TypePing:
			_ = fw.WriteFrame(TypePong, nil)
		default:
			// An unknown client frame: drop this client and keep hosting the others.
			return
		}
	}
}

// Read is the hosted program's keyboard: it blocks until any attached client sends
// input. While no client is attached it keeps blocking, so the program stays alive.
func (s *Server) Read(p []byte) (int, error) {
	s.readMu.Lock()
	if len(s.left) > 0 {
		n := copy(p, s.left)
		s.left = s.left[n:]
		s.readMu.Unlock()
		return n, nil
	}
	s.readMu.Unlock()
	for {
		select {
		case b := <-s.input:
			s.readMu.Lock()
			n := copy(p, b)
			if n < len(b) {
				s.left = append([]byte(nil), b[n:]...)
			}
			s.readMu.Unlock()
			if n == 0 {
				continue
			}
			return n, nil
		case <-s.stop:
			return 0, io.EOF
		}
	}
}

// Write is the hosted program's frame output: every attached client receives it. A
// write error (or the deadline on a stalled client) drops that client instead of
// ending the program.
func (s *Server) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	s.mu.Lock()
	clients := make([]*clientConn, 0, len(s.clients))
	for c := range s.clients {
		clients = append(clients, c)
	}
	s.mu.Unlock()
	for _, c := range clients {
		if err := c.fw.WriteFrame(TypeOutput, p); err != nil {
			s.removeClient(c)
		}
	}
	return len(p), nil
}
