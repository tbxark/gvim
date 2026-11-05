package lsp

import (
	"io"
	"os/exec"
	"sync"
	"time"
)

// killGrace bounds how long Close waits for a launched server process to exit
// after its stdin has been closed. After this, the process is killed.
const killGrace = 2 * time.Second

// guardedRWC is the transport under the JSON-RPC connection. Every outgoing
// frame passes through Write, which holds one mutex, so frames from concurrent
// callers never interleave. Read is not locked: exactly one goroutine (the
// connection's read loop) reads.
//
// Close is idempotent and does not take the write mutex, so it can unblock a
// writer that is parked on a full pipe.
type guardedRWC struct {
	rwc io.ReadWriteCloser

	wmu sync.Mutex

	once     sync.Once
	closeErr error
}

func (g *guardedRWC) Read(p []byte) (int, error) {
	return g.rwc.Read(p)
}

func (g *guardedRWC) Write(p []byte) (int, error) {
	g.wmu.Lock()
	defer g.wmu.Unlock()
	return g.rwc.Write(p)
}

// Close closes the underlying transport once. Later calls return the same error.
func (g *guardedRWC) Close() error {
	g.once.Do(func() {
		g.closeErr = g.rwc.Close()
	})
	return g.closeErr
}

// procPipes joins a server process's stdout (read side) and stdin (write side)
// into one io.ReadWriteCloser.
type procPipes struct {
	r io.ReadCloser
	w io.WriteCloser
}

func (p procPipes) Read(b []byte) (int, error)  { return p.r.Read(b) }
func (p procPipes) Write(b []byte) (int, error) { return p.w.Write(b) }

// Close closes stdin, then stdout. Closing stdout unblocks a read that is
// parked in the read loop.
func (p procPipes) Close() error {
	werr := p.w.Close()
	_ = p.r.Close()
	return werr
}

// waitProcess waits for the launched server to exit. After killGrace it kills
// the process and waits for it, so the wait goroutine always finishes. The
// exit status is not reported: a server that exits after its stdin closes is
// the normal case.
func waitProcess(cmd *exec.Cmd) {
	done := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(done)
	}()

	timer := time.NewTimer(killGrace)
	defer timer.Stop()

	select {
	case <-done:
	case <-timer.C:
		_ = cmd.Process.Kill()
		<-done
	}
}
