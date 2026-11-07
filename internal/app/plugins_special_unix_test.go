//go:build unix

package app

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestPluginBufOpenRefusesAFIFO opens a FIFO through the real host. The open used to read
// the file on the UI goroutine, and a FIFO without a writer froze the editor.
func TestPluginBufOpenRefusesAFIFO(t *testing.T) {
	isolateConfig(t)
	root := t.TempDir()
	fifo := filepath.Join(root, "pipe")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Skipf("mkfifo: %v", err)
	}
	// If the open blocks, a writer end frees it so the test fails instead of hanging.
	unblock := time.AfterFunc(20*time.Second, func() {
		if f, err := os.OpenFile(fifo, os.O_RDWR, 0); err == nil {
			_ = f.Close()
		}
	})
	defer unblock.Stop()
	result := filepath.Join(t.TempDir(), "result.txt")
	writeUserFile(t, "plugins/fifo.lua", `
local gvim = require "gvim"
gvim.command.register{ name = "fifo", title = "Open Fifo", run = function()
  local b, err = gvim.buf.open("`+fifo+`")
  gvim.fs.write("`+result+`", tostring(b) .. "|" .. tostring(err))
end }
`)
	s := newSession(t, Options{Root: root})
	s.waitStatus("fifo", "loaded")
	s.runCommand("Open Fifo")
	s.until("the result file", func() bool { _, err := os.Stat(result); return err == nil })
	s.pump(30 * time.Millisecond)
	data, err := os.ReadFile(result)
	must(t, err)
	if got := string(data); !strings.HasPrefix(got, "nil|") || !strings.Contains(got, "not a regular file") {
		t.Fatalf("buf.open(fifo) = %q, want nil and a not-a-regular-file error", got)
	}
}
