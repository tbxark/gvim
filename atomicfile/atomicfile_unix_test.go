//go:build unix

package atomicfile

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestWriteFileRefusesSpecialFile(t *testing.T) {
	fifo := filepath.Join(t.TempDir(), "pipe")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Skipf("mkfifo unavailable: %v", err)
	}
	if _, err := WriteFile(fifo, []byte("hello")); err == nil {
		t.Fatal("WriteFile replaced a FIFO instead of refusing it")
	}
	fi, err := os.Lstat(fifo)
	if err != nil || fi.Mode()&os.ModeNamedPipe == 0 {
		t.Fatalf("FIFO was altered: fi=%v err=%v", fi, err)
	}
}
