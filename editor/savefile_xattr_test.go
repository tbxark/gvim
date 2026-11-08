//go:build linux || darwin

package editor

import (
	"os"
	"testing"

	"golang.org/x/sys/unix"

	"github.com/tbxark/gvim/buffer"
)

func TestSaveKeepsExtendedAttributes(t *testing.T) {
	t.Parallel()
	for _, sv := range savers {
		t.Run(sv.Name, func(t *testing.T) {
			t.Parallel()
			path := writeLargeFile(t, "labelled.txt", lineText(150))
			if err := unix.Setxattr(path, "user.gvim-test", []byte("kept"), 0); err != nil {
				t.Skipf("setxattr: %v", err)
			}
			e, d := sv.Open(t, path)
			d.Buffer.Insert(buffer.Pos{}, "A")
			if err := e.Save(); err != nil {
				t.Fatal(err)
			}
			buf := make([]byte, 16)
			n, err := unix.Getxattr(path, "user.gvim-test", buf)
			if err != nil || string(buf[:n]) != "kept" {
				t.Errorf("attribute after the save = %q, %v; want kept", buf[:max(n, 0)], err)
			}
			if _, err := os.Stat(path); err != nil {
				t.Fatal(err)
			}
		})
	}
}
