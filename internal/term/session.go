package term

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sync"

	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/vt"
)

var errClosed = errors.New("term: session closed")

// Session is a program running in a pseudo-terminal. Its methods are safe for
// concurrent use.
//
// Goroutines per session:
//   - readOutput reads the PTY and feeds the emulator.
//   - relayReplies moves the emulator's replies into the inbox.
//   - writeInput is the only writer to the PTY.
//   - reap waits for the child.
//   - closeOnCancel closes the session when the Start context is cancelled.
type Session struct {
	ptmx *os.File
	cmd  *exec.Cmd

	mu        sync.Mutex // guards emu, closed and appCursor
	emu       *vt.Emulator
	closed    bool // set by teardown; emu is not written after it
	appCursor bool // application cursor keys (DECCKM) is on, see noteMode

	in *inbox // bytes for the child: user input and emulator replies

	updates   chan struct{} // one pending signal per screen change, see Updates
	done      chan struct{} // closed once the child has been reaped
	waitErr   error         // written before done is closed
	relayDone chan struct{} // closed when relayReplies returns

	closeOnce sync.Once
	closeErr  error // written inside closeOnce
}

func newSession(ptmx *os.File, cmd *exec.Cmd, cols, rows int) *Session {
	s := &Session{
		ptmx:      ptmx,
		cmd:       cmd,
		emu:       vt.NewEmulator(cols, rows),
		in:        newInbox(),
		updates:   make(chan struct{}, 1),
		done:      make(chan struct{}),
		relayDone: make(chan struct{}),
	}
	s.emu.SetCallbacks(vt.Callbacks{
		EnableMode:  func(mode ansi.Mode) { s.noteMode(mode, true) },
		DisableMode: func(mode ansi.Mode) { s.noteMode(mode, false) },
	})
	// relayReplies starts first: the emulator blocks on replies until they are read.
	go s.relayReplies()
	go s.writeInput()
	go s.readOutput()
	go s.reap()
	return s
}

// Write queues p for the child's input and returns len(p). It does not wait
// for the child to read the bytes, so it is safe to call from a UI update. It
// fails once the session is closed or the child's input has gone away.
func (s *Session) Write(p []byte) (int, error) {
	if !s.in.push(p) {
		return 0, errClosed
	}
	return len(p), nil
}

// Paste sends text to the child as a paste. The emulator wraps the text in the
// bracketed-paste markers when the child has turned that mode on, and sends it
// unchanged otherwise. Like Write, it does not wait for the child to read the
// bytes, so it is safe to call from a UI update.
func (s *Session) Paste(text string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errClosed
	}
	s.emu.Paste(text)
	return nil
}

// Updates receives after the screen changes. Signals coalesce: a receiver that is
// slow sees the latest screen, not every change, so it reads Screen after each
// signal. The channel is closed when the output ends, which is when the child and
// its PTY are gone or the session is closed. Done is the signal that the child has
// exited.
func (s *Session) Updates() <-chan struct{} {
	return s.updates
}

// Resize changes the size of the PTY and of the emulated screen. The child is
// told about the new size through SIGWINCH.
func (s *Session) Resize(cols, rows int) error {
	if err := checkSize(cols, rows); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errClosed
	}
	if err := setSize(s.ptmx, cols, rows); err != nil {
		return fmt.Errorf("term: resize: %w", err)
	}
	s.emu.Resize(cols, rows)
	return nil
}

// Screen returns a copy of the screen as it is now. The copy does not change
// when the child prints more.
func (s *Session) Screen() Screen {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, cols := s.emu.Height(), s.emu.Width()
	pos := s.emu.CursorPosition()
	sc := Screen{
		Rows:   rows,
		Cols:   cols,
		Cursor: Cursor{Row: pos.Y, Col: pos.X, Visible: !s.emu.CursorHidden()},
		cells:  make([]Cell, rows*cols),
	}
	for r := range rows {
		for c := range cols {
			sc.cells[r*cols+c] = cellOf(s.emu.CellAt(c, r))
		}
	}
	return sc
}

// AppCursorKeys reports whether the child has turned on application cursor keys
// (DECCKM). While it is on, the unmodified arrow keys, Home and End are sent in the
// SS3 form, ESC O followed by the letter.
func (s *Session) AppCursorKeys() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.appCursor
}

// noteMode records the modes the emulator turns on and off. The emulator calls it while
// it parses child output, which feed does under mu.
func (s *Session) noteMode(mode ansi.Mode, on bool) {
	if mode == ansi.ModeCursorKeys {
		s.appCursor = on
	}
}

// Done returns a channel that is closed when the child exits. The screen can
// still lag the child's last output when Done closes.
func (s *Session) Done() <-chan struct{} {
	return s.done
}

// Wait blocks until the child exits. It returns nil for exit status 0, and
// otherwise the error from os/exec, such as *exec.ExitError.
func (s *Session) Wait() error {
	<-s.done
	return s.waitErr
}

// Close ends the session. It closes the PTY, kills the child if it is still
// running, and returns once the child has been reaped and Done is closed.
// Close is idempotent. The screen stays readable after Close.
func (s *Session) Close() error {
	s.closeOnce.Do(s.teardown)
	<-s.done
	return s.closeErr
}

// teardown stops the session. Every write to the emulator's input pipe happens
// under mu, and closed is set under mu first. So after this point the only
// pipe write is the wake-up below. The emulator is never closed: its Read
// would race with emu.Close on the closed flag.
func (s *Session) teardown() {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()

	s.in.close()
	s.closeErr = s.ptmx.Close()
	_ = s.cmd.Process.Kill()

	// relayReplies is blocked in emu.Read. An empty write gives it a zero-byte
	// read, which no emulator reply can produce, and that is its stop signal.
	_, _ = s.emu.InputPipe().Write(nil)
	<-s.relayDone
}

// closeOnCancel closes the session when ctx is cancelled.
func (s *Session) closeOnCancel(ctx context.Context) {
	select {
	case <-ctx.Done():
		_ = s.Close()
	case <-s.done:
	}
}

// readOutput feeds child output to the emulator. It returns when the PTY read
// fails. That happens when every process holding the PTY has exited, or when
// Close closes the PTY.
func (s *Session) readOutput() {
	defer close(s.updates)
	buf := make([]byte, 32<<10)
	for {
		n, err := s.ptmx.Read(buf)
		if n > 0 && s.feed(buf[:n]) {
			s.signal()
		}
		if err != nil {
			return
		}
	}
}

// feed gives child output to the emulator. It reports false, and writes nothing,
// once the session is closed.
func (s *Session) feed(p []byte) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return false
	}
	_, _ = s.emu.Write(p)
	return true
}

// signal tells Updates receivers that the screen changed. It never blocks: a
// signal that is already pending covers this change too.
func (s *Session) signal() {
	select {
	case s.updates <- struct{}{}:
	default:
	}
}

// relayReplies moves the emulator's replies (cursor position, device
// attributes and so on) into the inbox. The emulator writes a reply while it
// parses output, and that write blocks until this goroutine reads it. So this
// goroutine must never wait on the child.
func (s *Session) relayReplies() {
	defer close(s.relayDone)
	buf := make([]byte, 256)
	for {
		n, err := s.emu.Read(buf)
		if err != nil || n == 0 {
			return
		}
		s.in.push(buf[:n])
	}
}

// writeInput drains the inbox into the PTY. It is the only writer of the PTY,
// so user input and replies never interleave.
func (s *Session) writeInput() {
	for {
		b, ok := s.in.take()
		if !ok {
			return
		}
		if _, err := s.ptmx.Write(b); err != nil {
			s.in.close()
			return
		}
	}
}

// reap waits for the child and then announces that it has exited.
func (s *Session) reap() {
	s.waitErr = s.cmd.Wait()
	close(s.done)
}

// inbox is an unbounded FIFO of bytes for the child. push never blocks, so
// neither a UI update calling Write nor the emulator's reply path can stall
// behind a child that is not reading its input.
type inbox struct {
	mu     sync.Mutex
	avail  *sync.Cond // signalled when data is pushed, and on close
	buf    []byte
	closed bool
}

func newInbox() *inbox {
	q := &inbox{}
	q.avail = sync.NewCond(&q.mu)
	return q
}

// push appends p. It reports false once the inbox is closed.
func (q *inbox) push(p []byte) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return false
	}
	q.buf = append(q.buf, p...)
	q.avail.Signal()
	return true
}

// take blocks until there is data, then returns all of it. It reports false
// once the inbox is closed.
func (q *inbox) take() ([]byte, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	for len(q.buf) == 0 && !q.closed {
		q.avail.Wait()
	}
	if q.closed {
		return nil, false
	}
	b := q.buf
	q.buf = nil
	return b, true
}

// close discards queued data and wakes any blocked take.
func (q *inbox) close() {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.closed = true
	q.buf = nil
	q.avail.Broadcast()
}
