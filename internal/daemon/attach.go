package daemon

// AttachClientTargets connects the current terminal to a daemon: the terminal goes into raw
// mode, stdin bytes become input frames, output frames are written to stdout, resize
// signals become resize frames, and a detached/exit frame restores the terminal and
// returns. It runs in the client process, not in the daemon.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"sync"

	"github.com/charmbracelet/x/term"
)

const (
	// attachEnterSeq puts an attaching client terminal into the modes the hosted Bubble Tea
	// program expects (alt screen, bracketed paste, focus reporting, all-motion SGR mouse,
	// modifyOtherKeys, and kitty keyboard protocol).
	attachEnterSeq = "\x1b[?1049h\x1b[?2004h\x1b[?1004h\x1b[?1003h\x1b[?1006h\x1b[>4;2m\x1b[>1u"
	// attachExitSeq restores mouse, paste, focus, keyboard protocol, cursor visibility, and
	// alternate screen on detach before term.Restore runs.
	attachExitSeq = "\x1b[?1000l\x1b[?1002l\x1b[?1003l\x1b[?1006l\x1b[?2004l\x1b[?1004l\x1b[>4;0m\x1b[<u\x1b[0 q\x1b[?25h\x1b[?1049l"

	maxBufferedNotices = 64
)

// AttachClientTargets attaches to the daemon called name and forwards files (which the
// daemon opens in its editor) after the hello. Each target's path is sent as given
// (relative paths are resolved by the daemon against cwd, the client's working directory)
// with its 1-based Line/Col. It blocks until the daemon detaches this client, exits, the
// connection drops, or ctx is canceled.
func AttachClientTargets(ctx context.Context, name, cwd string, files []OpenFileTarget) error {
	conn, err := Attach(name)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()
	cols, rows := 80, 24
	if w, h, err := term.GetSize(os.Stdout.Fd()); err == nil && w > 0 && h > 0 {
		cols, rows = w, h
	}
	// One writer for this connection: the input goroutine and the SIGWINCH goroutine
	// both write frames, and they must never interleave.
	fw := NewFrameWriter(deadlineWriter{conn: conn, timeout: frameWriteTimeout})
	if err := fw.WriteJSON(TypeHello, Hello{Version: ProtocolVersion, Cols: cols, Rows: rows}); err != nil {
		return err
	}
	if len(files) > 0 {
		if err := fw.WriteJSON(TypeOpenFile, OpenFile{Cwd: cwd, Files: files}); err != nil {
			return err
		}
	}
	var restore func()
	isTTY := term.IsTerminal(os.Stdin.Fd())
	if isTTY {
		state, err := term.MakeRaw(os.Stdin.Fd())
		if err != nil {
			return fmt.Errorf("daemon: put the terminal into raw mode: %w", err)
		}
		restore = func() { _ = term.Restore(os.Stdin.Fd(), state) }
	}
	stopResize := watchResize(fw)
	defer stopResize()
	return runAttachSession(ctx, conn, fw, os.Stdin, os.Stdout, os.Stderr, isTTY, restore)
}

func runAttachSession(
	ctx context.Context,
	conn net.Conn,
	fw *FrameWriter,
	in io.Reader,
	out, errOut io.Writer,
	emitVT bool,
	restore func(),
) error {
	var noticesMu sync.Mutex
	var notices []string
	readDone := make(chan struct{})
	// Teardown order on exit: close conn and wait for the reader goroutine so no
	// trailing TypeOutput races with VT teardown, emit attachExitSeq, restore term
	// state, and finally print buffered notices in cooked normal screen mode.
	defer func() {
		_ = conn.Close()
		<-readDone
		if emitVT {
			_, _ = io.WriteString(out, attachExitSeq)
		}
		if restore != nil {
			restore()
		}
		noticesMu.Lock()
		pending := notices
		notices = nil
		noticesMu.Unlock()
		for _, n := range pending {
			_, _ = fmt.Fprintf(errOut, "gvim: %s\n", n)
		}
	}()
	if emitVT {
		_, _ = io.WriteString(out, attachEnterSeq)
	}

	// Keep the terminal readable: a close or any read error ends the proxy.
	done := make(chan error, 2)
	go func() {
		buf := make([]byte, 32*1024)
		for {
			n, err := in.Read(buf)
			if n > 0 {
				if werr := fw.WriteFrame(TypeInput, buf[:n]); werr != nil {
					done <- werr
					return
				}
			}
			if err != nil {
				if errors.Is(err, io.EOF) {
					done <- nil
					return
				}
				done <- err
				return
			}
		}
	}()
	go func() {
		defer close(readDone)
		for {
			f, err := ReadFrame(conn)
			if err != nil {
				if errors.Is(err, io.EOF) {
					done <- nil
					return
				}
				done <- err
				return
			}
			switch f.Type {
			case TypeOutput:
				if _, err := out.Write(f.Payload); err != nil {
					done <- err
					return
				}
			case TypeNotice:
				if msg := formatAttachNotice(f.Payload); msg != "" {
					noticesMu.Lock()
					if len(notices) < maxBufferedNotices {
						notices = append(notices, msg)
					}
					noticesMu.Unlock()
				}
			case TypeVersionRejected:
				var m VersionMismatch
				_ = DecodeJSON(f, &m)
				done <- fmt.Errorf("daemon protocol version %d is incompatible with this client (version %d); update gvim on both sides", m.Daemon, m.Client)
				return
			case TypeDetached, TypeExit:
				done <- nil
				return
			case TypePong:
			default:
				// Unknown server frame: ignore it; the protocol is versioned by the
				// shared binary.
			}
		}
	}()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-done:
		return err
	}
}

func formatAttachNotice(payload []byte) string {
	var b strings.Builder
	b.Grow(len(payload))
	for _, r := range string(payload) {
		switch {
		case r == '\r' || r == '\n' || r == '\t':
			b.WriteByte(' ')
		case r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f):
			continue
		default:
			b.WriteRune(r)
		}
	}
	return strings.TrimSpace(b.String())
}
