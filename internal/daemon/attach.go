package daemon

// AttachClientTargets connects the current terminal to a daemon: the terminal goes into raw
// mode, stdin bytes become input frames, output frames are written to stdout, resize
// signals become resize frames, and a detached/exit frame restores the terminal and
// returns. It runs in the client process, not in the daemon.

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/charmbracelet/x/term"
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
	if term.IsTerminal(os.Stdin.Fd()) {
		state, err := term.MakeRaw(os.Stdin.Fd())
		if err != nil {
			return fmt.Errorf("daemon: put the terminal into raw mode: %w", err)
		}
		defer func() { _ = term.Restore(os.Stdin.Fd(), state) }()
	}
	stopResize := watchResize(fw)
	defer stopResize()
	// Keep the terminal readable: a close or any read error ends the proxy.
	done := make(chan error, 1)
	go func() {
		buf := make([]byte, 32*1024)
		for {
			n, err := os.Stdin.Read(buf)
			if n > 0 {
				if werr := fw.WriteFrame(TypeInput, buf[:n]); werr != nil {
					done <- werr
					return
				}
			}
			if err != nil {
				if err == io.EOF {
					done <- nil
					return
				}
				done <- err
				return
			}
		}
	}()
	go func() {
		for {
			f, err := ReadFrame(conn)
			if err != nil {
				done <- nil
				return
			}
			switch f.Type {
			case TypeOutput:
				if _, err := os.Stdout.Write(f.Payload); err != nil {
					done <- err
					return
				}
			case TypeNotice:
				_, _ = fmt.Fprintln(os.Stderr, "gvim:", string(f.Payload))
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
