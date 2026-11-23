package lsphost

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go.lsp.dev/protocol"

	"github.com/tbxark/gvim/config"
	"github.com/tbxark/gvim/lsp"
)

type fakeDiagServer struct {
	end     biPipe
	writeMu sync.Mutex

	mu             sync.Mutex
	workspaceItems string
	refreshAck     chan int64
}

func startFakeDiagPort(t testing.TB, root string) (*lspPort, *fakeDiagServer) {
	t.Helper()
	c2sR, c2sW := io.Pipe()
	s2cR, s2cW := io.Pipe()
	clientEnd := biPipe{r: s2cR, w: c2sW}
	serverEnd := biPipe{r: c2sR, w: s2cW}

	srv := &fakeDiagServer{
		end:            serverEnd,
		workspaceItems: "[]",
		refreshAck:     make(chan int64, 8),
	}
	t.Cleanup(func() { _ = serverEnd.Close() })

	go srv.serve()

	port := New(map[string]config.LSPServer{"go": {Command: "fake"}}).(*lspPort)
	port.SetSpawner(func(_ context.Context, cfg lsp.Config) (*lsp.Client, error) {
		return lsp.NewClient(clientEnd, cfg), nil
	})
	port.SetRoot(root)
	t.Cleanup(func() { _ = port.Stop() })
	return port, srv
}

func (s *fakeDiagServer) serve() {
	in := bufio.NewReader(s.end)
	for {
		n := -1
		for {
			line, err := in.ReadString('\n')
			if err != nil {
				return
			}
			line = strings.TrimSpace(line)
			if line == "" {
				break
			}
			if v, ok := strings.CutPrefix(line, "Content-Length: "); ok {
				n, _ = strconv.Atoi(v)
			}
		}
		body := make([]byte, n)
		if _, err := io.ReadFull(in, body); err != nil {
			return
		}
		var msg struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		if json.Unmarshal(body, &msg) != nil {
			continue
		}
		if msg.Method == "" && len(msg.ID) > 0 {
			if id, err := strconv.ParseInt(string(msg.ID), 10, 64); err == nil {
				select {
				case s.refreshAck <- id:
				default:
				}
			}
			continue
		}
		if len(msg.ID) == 0 {
			continue
		}
		result := "null"
		switch msg.Method {
		case "initialize":
			result = `{"capabilities":{"diagnosticProvider":{"interFileDependencies":true,"workspaceDiagnostics":true}}}`
		case "workspace/diagnostic":
			s.mu.Lock()
			items := s.workspaceItems
			s.mu.Unlock()
			result = fmt.Sprintf(`{"items":%s}`, items)
		}
		s.writeFrame(fmt.Sprintf(`{"jsonrpc":"2.0","id":%s,"result":%s}`, msg.ID, result))
	}
}

func (s *fakeDiagServer) writeFrame(payload string) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	_, _ = fmt.Fprintf(s.end, "Content-Length: %d\r\n\r\n%s", len(payload), payload)
}

func (s *fakeDiagServer) publish(filePath, message string, line, startChar, endChar uint32) {
	docURI := lsp.PathURI(filePath)
	msg, _ := json.Marshal(message)
	payload := fmt.Sprintf(
		`{"jsonrpc":"2.0","method":"textDocument/publishDiagnostics","params":{"uri":%q,"diagnostics":[{"range":{"start":{"line":%d,"character":%d},"end":{"line":%d,"character":%d}},"severity":1,"message":%s}]}}`,
		docURI, line, startChar, line, endChar, msg,
	)
	s.writeFrame(payload)
}

func (s *fakeDiagServer) setWorkspaceDiagnostic(filePath, message string, line, startChar, endChar uint32) {
	docURI := lsp.PathURI(filePath)
	msg, _ := json.Marshal(message)
	s.mu.Lock()
	s.workspaceItems = fmt.Sprintf(
		`[{"uri":%q,"kind":"full","items":[{"range":{"start":{"line":%d,"character":%d},"end":{"line":%d,"character":%d}},"severity":2,"message":%s}]}]`,
		docURI, line, startChar, line, endChar, msg,
	)
	s.mu.Unlock()
}

func (s *fakeDiagServer) sendRefresh(t testing.TB, id int64) {
	t.Helper()
	s.writeFrame(fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"method":"workspace/diagnostic/refresh","params":null}`, id))
	select {
	case got := <-s.refreshAck:
		if got != id {
			t.Fatalf("refresh ack id = %d, want %d", got, id)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for workspace/diagnostic/refresh ack %d", id)
	}
}

func waitClientDiag(t testing.TB, c *lsp.Client) {
	t.Helper()
	select {
	case <-c.Diagnostics():
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for client diagnostics event")
	}
}

func TestWorkspaceDiagnosticsCachesLocalUnopenedAndInvalidates(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	port, srv := startFakeDiagPort(t, dir)

	openFile := filepath.Join(dir, "open.go")
	unopenedFile := filepath.Join(dir, "unopened.go")
	missingFile := filepath.Join(dir, "later.go")
	// "a\U0001F600b\n": UTF-16 char 3 is rune col 2; "abcdef\n": UTF-16 char 3 is rune col 3.
	if err := os.WriteFile(unopenedFile, []byte("a\U0001F600b\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(openFile, []byte("abcdef\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Open one document to start the server handle and verify open-doc rec.lines reuse.
	if _, err := port.Open(openFile, "go", "a\U0001F600b\n"); err != nil {
		t.Fatalf("Open: %v", err)
	}
	client := port.managerFor(dir).AllHandles()[0].Client()

	srv.publish(openFile, "open-err", 0, 3, 4)
	waitClientDiag(t, client)
	srv.publish(unopenedFile, "err1", 0, 3, 4)
	waitClientDiag(t, client)

	got1 := port.WorkspaceDiagnostics()
	if len(got1) != 2 {
		t.Fatalf("got %d files, want 2: %+v", len(got1), got1)
	}
	// Open file uses rec.lines (col 2), not disk ("abcdef\n" -> col 3).
	if got1[0].Path != openFile || got1[0].Items[0].Start.Col != 2 {
		t.Fatalf("open file diagnostics = %+v, want Start.Col=2 from rec.lines", got1[0])
	}
	if got1[1].Path != unopenedFile || got1[1].Items[0].Start.Col != 2 || got1[1].Items[0].Message != "err1" {
		t.Fatalf("unopened file diagnostics = %+v, want Start.Col=2 err1", got1[1])
	}

	// Shallow copy protection + skip disk read when stat is unchanged.
	got1[1].Path = "mutated"
	got1[1].Source = "mutated"
	if os.Geteuid() != 0 {
		t.Cleanup(func() { _ = os.Chmod(unopenedFile, 0o644) })
		if err := os.Chmod(unopenedFile, 0o000); err != nil {
			t.Fatal(err)
		}
	}
	got2 := port.WorkspaceDiagnostics()
	if got2[1].Path != unopenedFile || got2[1].Source != "publish" || got2[1].Items[0].Start.Col != 2 {
		t.Fatalf("cached poll = %+v, want unmutated cache hit with Start.Col=2", got2[1])
	}

	// Restore permissions and modify content + mtime on disk: stat check invalidates cache.
	if err := os.Chmod(unopenedFile, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(unopenedFile, []byte("abcdef\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(unopenedFile, future, future); err != nil {
		t.Fatal(err)
	}
	got3 := port.WorkspaceDiagnostics()
	if got3[1].Items[0].Start.Col != 3 {
		t.Fatalf("after disk mtime+size change Start.Col = %d, want 3", got3[1].Items[0].Start.Col)
	}

	// New publishDiagnostics increments client.DiagnosticsRev() and invalidates cache.
	srv.publish(unopenedFile, "err2", 0, 3, 4)
	waitClientDiag(t, client)
	got4 := port.WorkspaceDiagnostics()
	if got4[1].Items[0].Message != "err2" {
		t.Fatalf("after publish Message = %q, want err2", got4[1].Items[0].Message)
	}

	// Change on open doc increments openDocsRev and reuses updated rec.lines.
	if _, err := port.Change(openFile, "uvwxyz\n"); err != nil {
		t.Fatalf("Change: %v", err)
	}
	got5 := port.WorkspaceDiagnostics()
	if got5[0].Items[0].Start.Col != 3 {
		t.Fatalf("after Change open file Start.Col = %d, want 3", got5[0].Items[0].Start.Col)
	}

	// Close on open doc increments openDocsRev; write emoji back to disk so disk != old rec.lines.
	if err := os.WriteFile(openFile, []byte("a\U0001F600b\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := port.Close(openFile); err != nil {
		t.Fatalf("Close: %v", err)
	}
	got6 := port.WorkspaceDiagnostics()
	if got6[0].Items[0].Start.Col != 2 {
		t.Fatalf("after Close open file Start.Col = %d, want 2 from disk", got6[0].Items[0].Start.Col)
	}

	// Workspace pull invalidation + missing->created->deleted file stat transitions.
	srv.setWorkspaceDiagnostic(missingFile, "ws1", 0, 3, 4)
	srv.sendRefresh(t, 100)
	got7 := port.WorkspaceDiagnostics()
	if len(got7) != 3 || got7[0].Path != missingFile || got7[0].Source != "workspace" || got7[0].Items[0].Start.Col != 0 {
		t.Fatalf("missing workspace file = %+v, want Start.Col=0", got7)
	}

	// Creating the file on disk invalidates exists=false -> exists=true.
	if err := os.WriteFile(missingFile, []byte("a\U0001F600b\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got8 := port.WorkspaceDiagnostics()
	if got8[0].Items[0].Start.Col != 2 {
		t.Fatalf("newly created file Start.Col = %d, want 2", got8[0].Items[0].Start.Col)
	}

	// Deleting the file on disk invalidates exists=true -> exists=false.
	if err := os.Remove(missingFile); err != nil {
		t.Fatal(err)
	}
	got9 := port.WorkspaceDiagnostics()
	if got9[0].Items[0].Start.Col != 0 {
		t.Fatalf("deleted file Start.Col = %d, want 0", got9[0].Items[0].Start.Col)
	}
}

func TestWorkspaceDiagnosticsCachesRemoteUnopenedWithoutReReading(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	port, srv := startFakeDiagPort(t, dir)

	remotePublished := "/remote/pub.go"
	remotePulled := "/remote/pull.go"
	var readCalls atomic.Int64
	var remoteTextMu sync.Mutex
	remoteTexts := map[string]string{
		remotePublished: "a\U0001F600b\n",
		remotePulled:    "a\U0001F600b\n",
	}
	port.SetRemoteReadFile(func(_ context.Context, path string) ([]byte, error) {
		readCalls.Add(1)
		remoteTextMu.Lock()
		defer remoteTextMu.Unlock()
		return []byte(remoteTexts[path]), nil
	})
	port.SetRemoteMarkerChecker(func(_ context.Context, _ string, _ []string) (bool, error) {
		return false, nil
	})

	if _, err := port.Open("/remote/open.go", "go", "package open\n"); err != nil {
		t.Fatalf("Open: %v", err)
	}
	client := port.managerFor(dir).AllHandles()[0].Client()

	srv.publish(remotePublished, "remote-pub-1", 0, 3, 4)
	waitClientDiag(t, client)
	srv.setWorkspaceDiagnostic(remotePulled, "remote-ws-1", 0, 3, 4)
	srv.sendRefresh(t, 200)

	got := port.WorkspaceDiagnostics()
	if len(got) != 2 {
		t.Fatalf("got %d files, want 2: %+v", len(got), got)
	}
	if got[0].Path != remotePublished || got[0].Items[0].Start.Col != 2 {
		t.Fatalf("remotePublished = %+v, want Start.Col=2", got[0])
	}
	if got[1].Path != remotePulled || got[1].Items[0].Start.Col != 2 {
		t.Fatalf("remotePulled = %+v, want Start.Col=2", got[1])
	}
	if calls := readCalls.Load(); calls != 2 {
		t.Fatalf("initial remoteReadFile calls = %d, want 2", calls)
	}

	// Repeated polls must trust (clientRevs, pulledRev, openDocsRev) and never re-read over SSH.
	for range 5 {
		again := port.WorkspaceDiagnostics()
		if len(again) != 2 || again[0].Items[0].Message != "remote-pub-1" {
			t.Fatalf("cached remote poll = %+v", again)
		}
	}
	if calls := readCalls.Load(); calls != 2 {
		t.Fatalf("remoteReadFile calls after 5 cached polls = %d, want 2 (no SSH re-reads)", calls)
	}

	// Invalidate via publishDiagnostics (diagRev).
	srv.publish(remotePublished, "remote-pub-2", 0, 3, 4)
	waitClientDiag(t, client)
	gotAfterPub := port.WorkspaceDiagnostics()
	if gotAfterPub[0].Items[0].Message != "remote-pub-2" {
		t.Fatalf("after publish Message = %q, want remote-pub-2", gotAfterPub[0].Items[0].Message)
	}
	if calls := readCalls.Load(); calls != 4 {
		t.Fatalf("remoteReadFile calls after publish invalidation = %d, want 4", calls)
	}

	// Invalidate via workspace pull (pulledRev).
	srv.setWorkspaceDiagnostic(remotePulled, "remote-ws-2", 0, 3, 4)
	srv.sendRefresh(t, 201)
	gotAfterPull := port.WorkspaceDiagnostics()
	if gotAfterPull[1].Items[0].Message != "remote-ws-2" {
		t.Fatalf("after pull Message = %q, want remote-ws-2", gotAfterPull[1].Items[0].Message)
	}

	// Opening remotePublished uses rec.lines instead of calling remoteReadFile for it.
	beforeOpen := readCalls.Load()
	if _, err := port.Open(remotePublished, "go", "abcdef\n"); err != nil {
		t.Fatalf("Open remotePublished: %v", err)
	}
	gotAfterOpen := port.WorkspaceDiagnostics()
	if gotAfterOpen[0].Items[0].Start.Col != 3 {
		t.Fatalf("after Open remotePublished Start.Col = %d, want 3", gotAfterOpen[0].Items[0].Start.Col)
	}
	// Only remotePulled (still unopened) should have called remoteReadFile on invalidation.
	if delta := readCalls.Load() - beforeOpen; delta != 1 {
		t.Fatalf("remoteReadFile calls after opening remotePublished = %d, want 1", delta)
	}
}

func BenchmarkWorkspaceDiagnosticsCachedPoll(b *testing.B) {
	dir := b.TempDir()
	port, srv := startFakeDiagPort(b, dir)

	openFile := filepath.Join(dir, "open.go")
	if err := os.WriteFile(openFile, []byte("package open\nvar x = 1\n"), 0o644); err != nil {
		b.Fatal(err)
	}
	if _, err := port.Open(openFile, "go", "package open\nvar x = 1\n"); err != nil {
		b.Fatal(err)
	}
	client := port.managerFor(dir).AllHandles()[0].Client()
	srv.publish(openFile, "open-err", 1, 4, 5)
	waitClientDiag(b, client)

	for i := range 20 {
		f := filepath.Join(dir, fmt.Sprintf("file_%02d.go", i))
		if err := os.WriteFile(f, []byte("package p\nvar y = 2\n"), 0o644); err != nil {
			b.Fatal(err)
		}
		srv.publish(f, "err", 1, 4, 5)
		waitClientDiag(b, client)
	}

	if got := port.WorkspaceDiagnostics(); len(got) != 21 {
		b.Fatalf("warmup files = %d, want 21", len(got))
	}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		out := port.WorkspaceDiagnostics()
		if len(out) != 21 {
			b.Fatalf("files = %d, want 21", len(out))
		}
	}
}

func BenchmarkCapWorkspaceDiagnostics(b *testing.B) {
	items := make([]protocol.Diagnostic, 0, 500)
	for i := range 500 {
		line := uint32(i % 100)
		items = append(items, protocol.Diagnostic{
			Range: protocol.Range{
				Start: protocol.Position{Line: line, Character: 0},
				End:   protocol.Position{Line: line, Character: 5},
			},
			Severity: protocol.DiagnosticSeverity((i % 4) + 1),
			Message:  protocol.String(fmt.Sprintf("diag %d", i%120)),
		})
	}
	b.ReportAllocs()
	for b.Loop() {
		out := capWorkspaceDiagnostics(items)
		if len(out) != workspaceDiagPerFileCap {
			b.Fatalf("len = %d, want %d", len(out), workspaceDiagPerFileCap)
		}
	}
}

func BenchmarkDecodeSemanticTokens(b *testing.B) {
	legend := &protocol.SemanticTokensLegend{
		TokenTypes:     []string{"keyword", "function", "variable", "string"},
		TokenModifiers: []string{"declaration", "readonly"},
	}
	var sb strings.Builder
	data := make([]uint32, 0, 2000*3*5)
	for line := range 2000 {
		sb.WriteString("func example(arg int) int { return arg }\n")
		dLine := uint32(1)
		if line == 0 {
			dLine = 0
		}
		data = append(data, dLine, 0, 4, 0, 0)
		data = append(data, 0, 5, 7, 1, 1)
		data = append(data, 0, 8, 3, 2, 2)
	}
	lines := newTextLines(sb.String())
	b.ReportAllocs()
	for b.Loop() {
		out := decodeSemanticTokens(data, legend, lines, 100, 140)
		if len(out) == 0 {
			b.Fatal("expected decoded tokens")
		}
	}
}
