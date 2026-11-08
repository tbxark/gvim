package pluginhost

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/tbxark/gvim/plugin/hostos"
	"github.com/tbxark/gvim/pluginapi"
)

// Processes of the plugin OS (AMENDMENTS C16). Every child runs in its own process group so a
// signal reaches the whole tree it started. Run waits for the child and returns its
// captured output; Start returns a Proc that streams output in batches of at most 64 KiB or
// 20 ms, takes stdin through a bounded queue that never blocks the caller, and ends with an
// exit result.

const (
	defaultMaxOutput = 8 << 20
	outBatch         = 64 << 10
	outBatchDelay    = 20 * time.Millisecond
	readSize         = 32 << 10
	stdinQueue       = 8 << 20
	waitDelay        = 2 * time.Second // after the child exited, how long inherited pipes may stay open
)

// osProc is a running child. It is the Proc of Start and the bookkeeping of Run.
type osProc struct {
	os   *pluginOS
	cmd  *exec.Cmd
	opts hostos.ProcOptions

	smu      sync.Mutex // guards started and cmd.Process against a Shutdown that races launch
	started  bool       // cmd.Start succeeded
	exited   atomic.Bool
	timedOut atomic.Bool
	termOnce sync.Once
	quit     chan struct{} // closed by Terminate: output is discarded from then on
	done     chan struct{} // closed when the child is reaped and its output was read
	res      hostos.ProcResult

	// Start only.
	out     chan osChunk
	omu     sync.Mutex // serialises Output
	pending *osChunk

	wmu      sync.Mutex
	wcond    *sync.Cond
	stdin    io.WriteCloser
	wqueue   [][]byte
	wqueued  int
	wclosing bool
	wbroken  bool
}

type osChunk struct {
	stderr bool
	data   []byte
}

var _ hostos.Proc = (*osProc)(nil)

// command builds the exec.Cmd for argv: no shell, its own process group, the environment
// of the editor plus opts.Env.
func (o *pluginOS) command(argv []string, opts hostos.ProcOptions) *exec.Cmd {
	cmd := exec.Command(argv[0], argv[1:]...)
	if opts.Cwd != "" {
		cmd.Dir = o.abs(opts.Cwd)
	} else {
		cmd.Dir = o.dirs().Cwd
	}
	var overlay []string
	if o.spawnEnv != nil {
		overlay = o.spawnEnv()
	}
	if len(opts.Env) > 0 || len(overlay) > 0 {
		cmd.Env = mergeEnvFrom(overlay, opts.Env)
	}
	cmd.WaitDelay = waitDelay
	setProcAttr(cmd)
	return cmd
}

// mergeEnvFrom is the process environment with the active project environment's
// overlay appended and extra added; extra wins, then base order is kept.
func mergeEnvFrom(overlay []string, extra map[string]string) []string {
	base := append(os.Environ(), overlay...)
	env := make([]string, 0, len(base)+len(extra))
	for _, kv := range base {
		k, _, _ := strings.Cut(kv, "=")
		if _, over := extra[k]; !over {
			env = append(env, kv)
		}
	}
	keys := make([]string, 0, len(extra))
	for k := range extra {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		env = append(env, k+"="+extra[k])
	}
	return env
}

func execErr(argv []string, err error) error {
	return pluginapi.Errf(pluginapi.CodeExec, "%s: %v", argv[0], err)
}

// isClosed reports whether Shutdown ran.
func (o *pluginOS) isClosed() bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.closed
}

// register adds p to the table Shutdown walks; it fails once Shutdown ran.
func (o *pluginOS) register(p *osProc) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.closed {
		return errShutdown()
	}
	o.procs[p] = struct{}{}
	return nil
}

func (o *pluginOS) unregister(p *osProc) {
	o.mu.Lock()
	delete(o.procs, p)
	o.mu.Unlock()
}

// limitBuffer keeps the first max bytes written and counts the rest as truncated. It never
// fails a write: the child must be able to finish.
type limitBuffer struct {
	max       int
	buf       bytes.Buffer
	truncated bool
}

func (l *limitBuffer) Write(p []byte) (int, error) {
	n := len(p)
	if room := l.max - l.buf.Len(); room < n {
		l.truncated = true
		p = p[:max(room, 0)]
	}
	l.buf.Write(p)
	return n, nil // the full length: a short write would end the copy and break the child's pipe
}

// launch starts cmd and registers the process.
func (o *pluginOS) launch(argv []string, opts hostos.ProcOptions, cmd *exec.Cmd) (*osProc, error) {
	p := &osProc{os: o, cmd: cmd, opts: opts, quit: make(chan struct{}), done: make(chan struct{})}
	p.wcond = sync.NewCond(&p.wmu)
	if err := o.register(p); err != nil {
		return nil, err
	}
	p.smu.Lock()
	err := cmd.Start()
	p.started = err == nil
	p.smu.Unlock()
	if err != nil {
		o.unregister(p)
		return nil, execErr(argv, err)
	}
	if o.isClosed() { // Shutdown ran between register and Start and could not signal p
		p.killGroup("KILL")
	}
	if opts.Timeout > 0 {
		t := time.AfterFunc(opts.Timeout, func() {
			if !p.exited.Load() {
				p.timedOut.Store(true)
				p.killGroup("KILL")
			}
		})
		go func() { <-p.done; t.Stop() }()
	}
	return p, nil
}

// finish records the result once the child is reaped.
func (p *osProc) finish(waitErr error) {
	p.exited.Store(true)
	p.os.unregister(p)
	if ps := p.cmd.ProcessState; ps != nil {
		p.res.Code, p.res.Signal = exitInfo(ps)
	} else if waitErr != nil {
		p.res.Code = -1
	}
	p.res.TimedOut = p.timedOut.Load()
	p.wmu.Lock()
	p.wbroken = true
	p.wqueue = nil
	p.wcond.Broadcast()
	if p.stdin != nil {
		_ = p.stdin.Close()
	}
	p.wmu.Unlock()
}

// Run implements hostos.OS.
func (o *pluginOS) Run(ctx context.Context, argv []string, opts hostos.ProcOptions) (hostos.ProcResult, error) {
	if len(argv) == 0 {
		return hostos.ProcResult{}, pluginapi.Errf(pluginapi.CodeInvalid, "empty argv")
	}
	if err := ctxErr(ctx); err != nil {
		return hostos.ProcResult{}, err
	}
	cmd := o.command(argv, opts)
	limit := opts.MaxOutput
	if limit <= 0 {
		limit = defaultMaxOutput
	}
	stdout, stderr := &limitBuffer{max: limit}, &limitBuffer{max: limit}
	cmd.Stdout = stdout
	if opts.MergeStderr {
		cmd.Stderr = stdout
	} else {
		cmd.Stderr = stderr
	}
	if opts.Stdin != nil {
		cmd.Stdin = bytes.NewReader(opts.Stdin)
	}
	p, err := o.launch(argv, opts, cmd)
	if err != nil {
		return hostos.ProcResult{}, err
	}
	waited := make(chan error, 1)
	go func() { waited <- cmd.Wait() }()
	cancelled := false
	var werr error
	select {
	case werr = <-waited:
	case <-ctx.Done():
		cancelled = true
		p.Terminate(killGraceTime)
		werr = <-waited
	}
	p.finish(werr)
	close(p.done)
	if cancelled {
		return hostos.ProcResult{}, mapErr(ctx.Err(), argv[0])
	}
	res := p.res
	res.Stdout, res.Stderr = stdout.buf.Bytes(), stderr.buf.Bytes()
	res.Truncated = stdout.truncated || stderr.truncated
	return res, nil
}

// Start implements hostos.OS.
func (o *pluginOS) Start(ctx context.Context, argv []string, opts hostos.ProcOptions) (hostos.Proc, error) {
	if len(argv) == 0 {
		return nil, pluginapi.Errf(pluginapi.CodeInvalid, "empty argv")
	}
	if err := ctxErr(ctx); err != nil {
		return nil, err
	}
	cmd := o.command(argv, opts)
	outR, outW, err := os.Pipe()
	if err != nil {
		return nil, mapErr(err, "")
	}
	readers := []*os.File{outR}
	var errR *os.File
	closeAll := func() {
		_ = outR.Close()
		_ = outW.Close()
		if errR != nil {
			_ = errR.Close()
		}
	}
	cmd.Stdout = outW
	var errW *os.File
	if opts.MergeStderr {
		cmd.Stderr = outW
	} else {
		if errR, errW, err = os.Pipe(); err != nil {
			closeAll()
			return nil, mapErr(err, "")
		}
		cmd.Stderr = errW
		readers = append(readers, errR)
	}
	var stdinPipe io.WriteCloser
	switch {
	case opts.OpenStdin:
		if stdinPipe, err = cmd.StdinPipe(); err != nil {
			closeAll()
			return nil, mapErr(err, "")
		}
	case opts.Stdin != nil:
		cmd.Stdin = bytes.NewReader(opts.Stdin)
	}
	p, err := o.launch(argv, opts, cmd)
	_ = outW.Close()
	if errW != nil {
		_ = errW.Close()
	}
	if err != nil {
		closeAll()
		return nil, err
	}
	p.out = make(chan osChunk, 16)
	p.stdin = stdinPipe
	if stdinPipe != nil {
		go p.writeLoop()
	}
	var rwg sync.WaitGroup
	for i, r := range readers {
		rwg.Add(1)
		go func() {
			defer rwg.Done()
			p.readLoop(r, i == 1)
		}()
	}
	go func() {
		werr := cmd.Wait() // returns at exit: the pipes are our own files
		rdone := make(chan struct{})
		go func() { rwg.Wait(); close(rdone) }()
		select {
		case <-rdone:
		case <-time.After(waitDelay): // a grandchild still holds the pipe
			for _, r := range readers {
				_ = r.Close()
			}
			<-rdone
		}
		for _, r := range readers {
			_ = r.Close()
		}
		close(p.out)
		p.finish(werr)
		close(p.done)
	}()
	return p, nil
}

// readLoop copies one pipe into the output channel; after Terminate it drains and
// discards, so the child never blocks on a full pipe.
func (p *osProc) readLoop(r io.Reader, stderr bool) {
	buf := make([]byte, readSize)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			select {
			case p.out <- osChunk{stderr: stderr, data: append([]byte(nil), buf[:n]...)}:
			case <-p.quit:
			}
		}
		if err != nil {
			return
		}
	}
}

// PID implements hostos.Proc.
func (p *osProc) PID() int { return p.cmd.Process.Pid }

// Output implements hostos.Proc.
func (p *osProc) Output(ctx context.Context) (hostos.ProcChunk, bool) {
	p.omu.Lock()
	defer p.omu.Unlock()
	var first osChunk
	if p.pending != nil {
		first, p.pending = *p.pending, nil
	} else {
		select {
		case c, ok := <-p.out:
			if !ok {
				return hostos.ProcChunk{}, false
			}
			first = c
		case <-ctx.Done():
			return hostos.ProcChunk{}, false
		}
	}
	data := first.data
	if len(data) < outBatch {
		timer := time.NewTimer(outBatchDelay)
		defer timer.Stop()
	batch:
		for len(data) < outBatch {
			select {
			case c, ok := <-p.out:
				if !ok {
					break batch
				}
				if c.stderr != first.stderr {
					p.pending = &c
					break batch
				}
				data = append(data, c.data...)
			case <-timer.C:
				break batch
			case <-ctx.Done():
				break batch
			}
		}
	}
	return hostos.ProcChunk{Stderr: first.stderr, Data: data}, true
}

// Wait implements hostos.Proc.
func (p *osProc) Wait(ctx context.Context) (hostos.ProcResult, error) {
	select {
	case <-p.done:
		return p.res, nil
	case <-ctx.Done():
		return hostos.ProcResult{}, mapErr(ctx.Err(), "")
	}
}

// Write implements hostos.Proc: it queues data for the writer goroutine and never blocks.
func (p *osProc) Write(data []byte) error {
	p.wmu.Lock()
	defer p.wmu.Unlock()
	switch {
	case p.stdin == nil:
		return pluginapi.Errf(pluginapi.CodeClosed, "stdin is not open")
	case p.wbroken || p.wclosing:
		return pluginapi.Errf(pluginapi.CodeClosed, "stdin is closed")
	case p.wqueued+len(data) > stdinQueue:
		return pluginapi.Errf(pluginapi.CodeBusy, "stdin buffer full")
	}
	p.wqueue = append(p.wqueue, append([]byte(nil), data...))
	p.wqueued += len(data)
	p.wcond.Signal()
	return nil
}

// CloseStdin implements hostos.Proc: the queue is written, then the pipe closes.
func (p *osProc) CloseStdin() {
	p.wmu.Lock()
	p.wclosing = true
	p.wcond.Broadcast()
	p.wmu.Unlock()
}

// writeLoop feeds the queue into the stdin pipe.
func (p *osProc) writeLoop() {
	p.wmu.Lock()
	for {
		for len(p.wqueue) == 0 && !p.wclosing && !p.wbroken {
			p.wcond.Wait()
		}
		if p.wbroken || len(p.wqueue) == 0 {
			break
		}
		chunk := p.wqueue[0]
		p.wqueue = p.wqueue[1:]
		p.wmu.Unlock()
		_, err := p.stdin.Write(chunk)
		p.wmu.Lock()
		p.wqueued -= len(chunk)
		if err != nil {
			p.wbroken = true
			p.wqueue = nil
		}
	}
	p.wmu.Unlock()
	_ = p.stdin.Close()
}

// Kill implements hostos.Proc.
func (p *osProc) Kill(signal string) error {
	if p.exited.Load() {
		return pluginapi.Errf(pluginapi.CodeClosed, "process has exited")
	}
	if err := signalProc(p.cmd.Process, signal); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return mapErr(err, "")
	}
	return nil
}

// killGroup signals the process group; a finished process is not an error.
func (p *osProc) killGroup(signal string) {
	p.smu.Lock()
	defer p.smu.Unlock()
	if p.started && !p.exited.Load() {
		_ = signalProc(p.cmd.Process, signal)
	}
}

// killGroupAfterGrace is the KILL that follows Terminate. The leader may be gone by now while
// a grandchild that ignored TERM lives on in its group, so it signals the group unless the
// group is empty (an empty group's id could belong to an unrelated process by now).
func (p *osProc) killGroupAfterGrace() {
	p.smu.Lock()
	defer p.smu.Unlock()
	if !p.started || (p.exited.Load() && !groupAlive(p.cmd.Process)) {
		return
	}
	_ = signalProc(p.cmd.Process, "KILL")
}

// Terminate implements hostos.Proc.
func (p *osProc) Terminate(grace time.Duration) {
	p.termOnce.Do(func() {
		close(p.quit)
		p.killGroup("TERM")
		time.AfterFunc(grace, p.killGroupAfterGrace)
	})
}
