package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tbxark/gvim/authority"
	"github.com/tbxark/gvim/authority/authoritytest"
	"github.com/tbxark/gvim/lsp"
	"github.com/tbxark/gvim/plugin/hostos"
)

// fakeLSPProc is an authority long-running child that answers one LSP initialize request
// on its piped stdio, so the spawner test exercises the real transport adapter and the
// real lsp client handshake without a local server process.
type fakeLSPProc struct {
	mu       sync.Mutex
	in       bytes.Buffer
	out      chan hostos.ProcChunk
	done     chan struct{}
	doneOnce sync.Once
	replied  bool
}

func newFakeLSPProc() *fakeLSPProc {
	return &fakeLSPProc{out: make(chan hostos.ProcChunk, 8), done: make(chan struct{})}
}

func (p *fakeLSPProc) PID() int { return 4242 }

func (p *fakeLSPProc) Write(data []byte) error {
	p.mu.Lock()
	p.in.Write(data)
	buffered := p.in.Bytes()
	replied := p.replied
	p.mu.Unlock()
	if replied || !bytes.Contains(buffered, []byte(`"method":"initialize"`)) {
		return nil
	}
	head := bytes.Index(buffered, []byte("\r\n\r\n"))
	if head < 0 {
		return nil
	}
	body := buffered[head+4:]
	var req struct {
		ID     int    `json:"id"`
		Method string `json:"method"`
	}
	if err := json.Unmarshal(body, &req); err != nil || req.Method != "initialize" {
		return nil
	}
	result, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": map[string]any{"capabilities": map[string]any{}}})
	frame := fmt.Sprintf("Content-Length: %d\r\n\r\n%s", len(result), result)
	p.mu.Lock()
	p.replied = true
	p.mu.Unlock()
	p.out <- hostos.ProcChunk{Data: []byte(frame)}
	return nil
}

func (p *fakeLSPProc) CloseStdin() {}

func (p *fakeLSPProc) Kill(string) error { p.Terminate(0); return nil }

func (p *fakeLSPProc) Terminate(time.Duration) { p.doneOnce.Do(func() { close(p.done) }) }

func (p *fakeLSPProc) Output(ctx context.Context) (hostos.ProcChunk, bool) {
	select {
	case chunk := <-p.out:
		return chunk, true
	case <-p.done:
		return hostos.ProcChunk{}, false
	case <-ctx.Done():
		return hostos.ProcChunk{}, false
	}
}

func (p *fakeLSPProc) Wait(ctx context.Context) (hostos.ProcResult, error) {
	select {
	case <-p.done:
		return hostos.ProcResult{}, nil
	case <-ctx.Done():
		return hostos.ProcResult{}, ctx.Err()
	}
}

// fakeLongRunner records the start of a remote child.
type fakeLongRunner struct {
	proc hostos.Proc
	argv []string
	opts hostos.ProcOptions
}

func (f *fakeLongRunner) Start(_ context.Context, argv []string, o hostos.ProcOptions) (hostos.Proc, error) {
	f.argv = append([]string(nil), argv...)
	f.opts = o
	return f.proc, nil
}

var _ authority.LongRunning = (*fakeLongRunner)(nil)

// TestRemoteLSPSpawnerRunsOnTheAuthority checks LSP over SSH at the seam: the server
// starts on the remote authority (workspace cwd, the configured command verbatim, the
// locally resolved Program ignored), its stdio completes a real initialize handshake, and
// process limits are recorded as not applicable remotely.
func TestRemoteLSPSpawnerRunsOnTheAuthority(t *testing.T) {
	proc := newFakeLSPProc()
	runner := &fakeLongRunner{proc: proc}
	slot := authoritytest.New()
	slot.LongRunner = runner
	spawn := remoteLSPSpawner(slot, "/remote/proj")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	mem := uint32(50)
	client, err := spawn(ctx, lsp.Config{
		Command: []string{"gopls", "serve"},
		Program: "/local/resolved/gopls",
		RootURI: "file:///local/proj",
		Limits:  lsp.ProcessLimits{Enabled: true, MaxMemoryPercent: &mem},
	})
	if err != nil {
		t.Fatalf("spawner: %v", err)
	}
	defer func() { _ = client.Close() }()
	if len(runner.argv) != 2 || runner.argv[0] != "gopls" || runner.argv[1] != "serve" {
		t.Fatalf("remote argv = %v, want the configured command verbatim", runner.argv)
	}
	if runner.opts.Cwd != "/remote/proj" {
		t.Fatalf("remote cwd = %q, want the remote workspace", runner.opts.Cwd)
	}
	res, err := client.Initialize(ctx)
	if err != nil {
		t.Fatalf("initialize over the authority transport: %v", err)
	}
	if res == nil {
		t.Fatal("no initialize result")
	}
	if note := client.LimitsNote(); !strings.Contains(note, "not applied remotely") {
		t.Fatalf("LimitsNote = %q, want the remote limits note", note)
	}
}
