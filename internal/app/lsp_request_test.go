package app

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tbxark/gvim/internal/config"
	"github.com/tbxark/gvim/internal/pluginapi"
	"github.com/tbxark/gvim/internal/ui"
)

// fakeLSPEnv makes the test binary act as a language server: TestFakeLSPServerProcess
// serves the protocol on stdin and stdout when it is set. lspPort starts the server the
// way it starts gopls, with os/exec, so the production Request path runs against a
// process that speaks the wire protocol.
const fakeLSPEnv = "GVIM_TEST_FAKE_LSP_SERVER"

// bigResultBytes is over maxRequestResult.
const bigResultBytes = maxRequestResult + 1<<20

func TestFakeLSPServerProcess(t *testing.T) {
	if os.Getenv(fakeLSPEnv) == "" {
		t.Skip("runs only as the server process of the lspPort tests")
	}
	if os.Getenv(fakeLSPEnv) == "die" {
		fmt.Fprintln(os.Stderr, "fake server: cannot start: bad configuration")
		os.Exit(3)
	}
	in := bufio.NewReader(os.Stdin)
	for {
		n := -1
		for {
			line, err := in.ReadString('\n')
			if err != nil {
				os.Exit(0)
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
			os.Exit(0)
		}
		var msg struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		if json.Unmarshal(body, &msg) != nil {
			os.Exit(1)
		}
		if msg.Method == "exit" {
			os.Exit(0)
		}
		if len(msg.ID) == 0 {
			continue // a notification
		}
		var result string
		switch msg.Method {
		case "initialize":
			result = `{"capabilities":{}}`
		case "textDocument/hover":
			result = `{"contents":"doc","list":[1,null,3],"none":[]}`
		case "textDocument/references":
			// Answers after the test's timeout, from a goroutine so that the server keeps
			// reading (the shutdown at the end of the test must not wait for it).
			go func(id json.RawMessage) {
				time.Sleep(time.Second)
				send(id, "null")
			}(msg.ID)
			continue
		case "textDocument/formatting":
			result = `[{"range":{"start":{"line":1,"character":4},"end":{"line":1,"character":6}},"newText":" "}]`
		case "textDocument/inlayHint":
			result = strings.Repeat("[", 12000) + strings.Repeat("]", 12000)
		case "textDocument/documentSymbol":
			result = `"` + strings.Repeat("x", bigResultBytes) + `"`
		default:
			result = "null"
		}
		send(msg.ID, result)
	}
}

var fakeLSPWrite sync.Mutex

// send writes the response to request id.
func send(id json.RawMessage, result string) {
	reply := fmt.Sprintf(`{"jsonrpc":"2.0","id":%s,"result":%s}`, id, result)
	fakeLSPWrite.Lock()
	defer fakeLSPWrite.Unlock()
	fmt.Printf("Content-Length: %d\r\n\r\n%s", len(reply), reply)
}

// startFakeLSPPort returns a port whose "go" server is the test binary, with one open
// document, and its path.
func startFakeLSPPort(t *testing.T) (ui.LSPSession, string) {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(fakeLSPEnv, "1")
	port := newLSPPort(map[string]config.LSPServer{"go": {Command: exe, Args: []string{"-test.run=^TestFakeLSPServerProcess$"}}})
	dir := t.TempDir()
	port.SetRoot(dir)
	path := filepath.Join(dir, "a.go")
	if err := port.Open(path, "go", "package a\n"); err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = port.Stop() })
	return port, path
}

func TestLSPPortRequestAgainstAServer(t *testing.T) {
	port, path := startFakeLSPPort(t)

	t.Run("decodes the raw result", func(t *testing.T) {
		got, err := port.Request(path, "textDocument/hover", map[string]any{"position": map[string]any{"line": 0, "character": 0}})
		if err != nil {
			t.Fatal(err)
		}
		want := map[string]any{"contents": "doc", "list": []any{1.0, nil, 3.0}, "none": []any{}}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("result = %#v, want %#v", got, want)
		}
	})
	t.Run("a null result is nil", func(t *testing.T) {
		got, err := port.Request(path, "textDocument/definition", nil)
		if err != nil || got != nil {
			t.Fatalf("result = %#v, %v; want nil, nil", got, err)
		}
	})
	t.Run("a result over the cap is too_large", func(t *testing.T) {
		got, err := port.Request(path, "textDocument/documentSymbol", nil)
		if pluginapi.CodeOf(err) != pluginapi.CodeTooLarge || got != nil {
			t.Fatalf("result = %#v, err = %v; want a too_large error", got, err)
		}
	})
	t.Run("a result that does not decode is an error", func(t *testing.T) {
		// Valid JSON nested deeper than encoding/json takes.
		got, err := port.Request(path, "textDocument/inlayHint", nil)
		if err == nil || !strings.Contains(err.Error(), "bad result") || got != nil {
			t.Fatalf("result = %#v, err = %v; want a bad result error", got, err)
		}
	})
	t.Run("a slow server is a timeout", func(t *testing.T) {
		old := lspRequestTimeout
		lspRequestTimeout = 200 * time.Millisecond
		t.Cleanup(func() { lspRequestTimeout = old })
		start := time.Now()
		_, err := port.Request(path, "textDocument/references", nil)
		if pluginapi.CodeOf(err) != pluginapi.CodeTimeout {
			t.Fatalf("err = %v, want a timeout error", err)
		}
		if d := time.Since(start); d > 5*time.Second {
			t.Fatalf("the request took %v, want about the timeout", d)
		}
	})
}
