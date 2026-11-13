package document

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A document with a file access never reads the local disk: FollowDisk must leave
// the remote record alone even when a local file at the same path holds the text.
func TestFollowDiskIgnoresLocalFileOfRemoteDocument(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "a.txt")
	if err := os.WriteFile(path, []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	d := newDocument(path, "hello\n")
	d.access = &fakeRangeAccess{}
	remote := diskState{Exists: true, Size: 6, Mtime: time.Unix(1, 0)}
	d.Disk = remote
	d.FollowDisk(path)
	if !d.Disk.SameAs(remote) {
		t.Errorf("Disk = %+v, want the remote record %+v", d.Disk, remote)
	}
}

func TestFollowDiskLocalDocument(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "a.txt")
	if err := os.WriteFile(path, []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	d := newDocument(path, "hello\n")
	d.Disk = diskState{Exists: true, Size: 6, Mtime: time.Unix(1, 0)}
	d.FollowDisk(path)
	if !d.Disk.SameAs(diskStateOf(path)) {
		t.Errorf("Disk = %+v, want the state of the moved file", d.Disk)
	}
}
