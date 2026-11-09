package terminal

import (
	"context"
	"errors"
)

// Backend is a terminal transport: a local PTY, or a remote channel that requested a PTY
// (an SSH exec channel today). It mirrors the internal ptyProc, so a session built over
// one behaves exactly like a local session: input, output, resize, kill and wait.
type Backend interface {
	Read([]byte) (int, error)
	Write([]byte) (int, error)
	Resize(cols, rows int) error
	Kill() error
	Wait() error
	Close() error
}

// StartWithBackend starts a session over b instead of spawning a local shell. The options
// supply the size and the scrollback; Shell, Args and Env are not used (the backend owns
// the remote process), and Dir is cosmetic (OSC 7 from the remote shell reports the real
// one). Close must be called even after the backend has ended.
func StartWithBackend(ctx context.Context, opts Options, b Backend) (*Session, error) {
	if b == nil {
		return nil, errors.New("term: nil backend")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	cols, rows := opts.Cols, opts.Rows
	if cols <= 0 {
		cols = defaultCols
	}
	if rows <= 0 {
		rows = defaultRows
	}
	if err := checkSize(cols, rows); err != nil {
		return nil, err
	}
	return startSession(ctx, backendProc{b: b}, cols, rows, opts.ScrollbackLines), nil
}

// startSession builds the emulator and the goroutines around a transport.
func startSession(ctx context.Context, proc ptyProc, cols, rows, scrollback int) *Session {
	s := newSession(proc, cols, rows, scrollback)
	go s.closeOnCancel(ctx)
	return s
}

// backendProc adapts a Backend to the session's ptyProc.
type backendProc struct{ b Backend }

func (p backendProc) Read(b []byte) (int, error)  { return p.b.Read(b) }
func (p backendProc) Write(b []byte) (int, error) { return p.b.Write(b) }
func (p backendProc) Resize(cols, rows int) error { return p.b.Resize(cols, rows) }
func (p backendProc) Kill() error                 { return p.b.Kill() }
func (p backendProc) Wait() error                 { return p.b.Wait() }
func (p backendProc) Close() error                { return p.b.Close() }

// supportsForegroundProcess reports false for a remote transport: the dock polls
// only a local PTY, and a remote shell's foreground process lives on the far host
// (this is not the remote editing filesystem either).
func (p backendProc) supportsForegroundProcess() bool { return false }

// foregroundProcessName is unavailable on a remote transport.
func (p backendProc) foregroundProcessName() (string, bool) { return "", false }
