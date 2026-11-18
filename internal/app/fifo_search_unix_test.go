//go:build !windows

package app

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestPreviewAndSearchSkipFIFOWithoutBlocking(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	fifo := filepath.Join(dir, "pipe")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link-to-pipe")
	if err := os.Symlink(fifo, link); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		var p previewReader
		if _, _, err := p.ReadHead(fifo, 64); err == nil {
			t.Error("ReadHead accepted a FIFO")
		}
		if _, _, _, err := p.ReadWindow(context.Background(), link, 1, 64); err == nil {
			t.Error("ReadWindow accepted a symlink to FIFO")
		}
		if _, reason := readSearchableFile(fifo, 0); reason != "unreadable file" {
			t.Errorf("readSearchableFile(fifo) = %q, want unreadable file", reason)
		}
		if _, reason := readSearchableFile(link, 0); reason != "unreadable file" {
			t.Errorf("readSearchableFile(symlink->fifo) = %q, want unreadable file", reason)
		}
	}()
	<-done
}
