package daemon

// Server hosts one editor program over the daemon socket: tui.RunWithIO reads the
// attached client's input and writes the frame through this type, and an attach or a
// resize becomes a window-size message on the program. Detaching ends only the client;
// the program keeps running and the next attach repaints it.

import (
	"context"
	"errors"
	"io"
	"net"
	"path/filepath"
	"strings"
	"sync"

	tea "charm.land/bubbletea/v2"
)

// Server is the daemon side of the attach protocol. It implements io.Reader (the
// client's terminal input) and io.Writer (the frame output) for the hosted program.
type Server struct {
	ln   net.Listener
	path string

	mu   sync.Mutex
	conn net.Conn
	prog *tea.Program
	// openFiles, when set, opens the paths of a client's open_file frame.
	openFiles func(paths []string)

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
	return &Server{ln: ln, path: path, input: make(chan []byte), stop: make(chan struct{})}
}

// Path is the socket path of the server.
func (s *Server) Path() string { return s.path }

// Name is the socket name of the server: the file name without .sock.
func (s *Server) Name() string { return strings.TrimSuffix(filepath.Base(s.path), ".sock") }

// OnOpenFiles sets the callback that opens a client's requested paths.
func (s *Server) OnOpenFiles(fn func(paths []string)) {
	s.mu.Lock()
	s.openFiles = fn
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

// OnProgram receives the hosted program, so an attach can resize and repaint it.
func (s *Server) OnProgram(p *tea.Program) {
	s.mu.Lock()
	s.prog = p
	s.mu.Unlock()
}

// Program returns the hosted program, or nil before RunWithIO started it.
func (s *Server) Program() *tea.Program {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.prog
}

// Detach sends the detached frame to the attached client (if any) and drops the
// connection. The hosted program is untouched.
func (s *Server) Detach() {
	s.mu.Lock()
	conn := s.conn
	s.conn = nil
	s.mu.Unlock()
	if conn == nil {
		return
	}
	_ = WriteFrame(conn, TypeDetached, nil)
	_ = conn.Close()
}

// Close sends the exit frame to the attached client, stops the listener and the
// input reader, and closes the connection.
func (s *Server) Close() error {
	var err error
	s.closeOnce.Do(func() {
		s.mu.Lock()
		s.closed = true
		conn := s.conn
		s.conn = nil
		s.mu.Unlock()
		close(s.stop)
		if conn != nil {
			_ = WriteFrame(conn, TypeExit, nil)
			_ = conn.Close()
		}
		err = s.ln.Close()
	})
	if errors.Is(err, net.ErrClosed) {
		return nil
	}
	return err
}

// handleConn runs one attached client: it reads the hello, then input/resize/detach
// frames until the connection closes.
func (s *Server) handleConn(conn net.Conn) {
	defer func() { _ = conn.Close() }()
	f, err := ReadFrame(conn)
	if err != nil || f.Type != TypeHello {
		return
	}
	var hello Hello
	if err := DecodeJSON(f, &hello); err != nil {
		return
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	if s.conn != nil && s.conn != conn {
		s.mu.Unlock()
		_ = WriteNotice(conn, "another client is attached to this daemon")
		return
	}
	s.conn = conn
	prog := s.prog
	s.mu.Unlock()
	if prog != nil {
		if hello.Cols > 0 && hello.Rows > 0 {
			prog.Send(tea.WindowSizeMsg{Width: hello.Cols, Height: hello.Rows})
		}
		// A fresh frame: the program repaints at the client's size.
		prog.Send(tea.RequestWindowSize())
	}
	for {
		f, err := ReadFrame(conn)
		if err != nil {
			break
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
			p := s.prog
			s.mu.Unlock()
			if p != nil {
				p.Send(tea.WindowSizeMsg{Width: r.Cols, Height: r.Rows})
			}
		case TypeOpenFile:
			var req OpenFile
			if DecodeJSON(f, &req) != nil || len(req.Paths) == 0 {
				continue
			}
			s.mu.Lock()
			open := s.openFiles
			s.mu.Unlock()
			if open != nil {
				open(req.Paths)
			}
		case TypeDetach:
			_ = WriteFrame(conn, TypeDetached, nil)
			s.detachConn(conn)
			return
		case TypePing:
			_ = WriteFrame(conn, TypePong, nil)
		default:
			// An unknown client frame: drop the connection and keep hosting.
			s.detachConn(conn)
			return
		}
	}
	s.detachConn(conn)
}

// detachConn clears the attached client when it is conn.
func (s *Server) detachConn(conn net.Conn) {
	s.mu.Lock()
	if s.conn == conn {
		s.conn = nil
	}
	s.mu.Unlock()
}

// Read is the hosted program's keyboard: it blocks until the attached client sends
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

// Write is the hosted program's frame output: only an attached client receives it,
// and a client write error detaches that client instead of ending the program.
func (s *Server) Write(p []byte) (int, error) {
	s.mu.Lock()
	conn := s.conn
	s.mu.Unlock()
	if conn == nil || len(p) == 0 {
		return len(p), nil
	}
	if err := WriteFrame(conn, TypeOutput, p); err != nil {
		s.detachConn(conn)
	}
	return len(p), nil
}
