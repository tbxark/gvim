package atomicfile

import (
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestWriteFileCreatesWithTheUmask(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no Unix modes")
	}
	path := filepath.Join(t.TempDir(), "new.txt")
	if _, err := WriteFile(path, []byte("x")); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if want := newFileMode &^ umask; fi.Mode().Perm() != want {
		t.Fatalf("new file mode = %v, want %v (0644 under umask %v)", fi.Mode().Perm(), want, umask)
	}
}

func TestWriteFileKeepsTheExistingMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no Unix modes")
	}
	path := filepath.Join(t.TempDir(), "old.txt")
	if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o640); err != nil {
		t.Fatal(err)
	}
	if _, err := WriteFile(path, []byte("new")); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o640 {
		t.Fatalf("mode = %v, want the old file's 0640", fi.Mode().Perm())
	}
	if data, _ := os.ReadFile(path); string(data) != "new" {
		t.Fatalf("content = %q", data)
	}
}

func TestWriteFileWritesThroughASymlink(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real.txt")
	if err := os.WriteFile(real, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.txt")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("no symlinks: %v", err)
	}
	if _, err := WriteFile(link, []byte("new")); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Lstat(link); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("the link was replaced: %v %v", fi, err)
	}
	if data, _ := os.ReadFile(real); string(data) != "new" {
		t.Fatalf("target content = %q, want new", data)
	}
}

// TestWriteFileWithALongFileName: the temporary name is the target's name plus a prefix and
// a suffix, so a name near the 255-byte limit must not make the save fail.
func TestWriteFileWithALongFileName(t *testing.T) {
	path := filepath.Join(t.TempDir(), strings.Repeat("a", 250)+".txt")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Skipf("the filesystem takes no 254-byte file name: %v", err)
	}
	if _, err := WriteFile(path, []byte("new")); err != nil {
		t.Fatalf("WriteFile of a 254-byte name: %v", err)
	}
	if got, err := os.ReadFile(path); err != nil || string(got) != "new" {
		t.Fatalf("content = %q, %v", got, err)
	}
}

func TestTempPrefixIsWhatAWriteCreates(t *testing.T) {
	name := strings.Repeat("n", 250) + ".txt"
	path := filepath.Join(t.TempDir(), name)
	var temp string
	if _, err := WriteFileStream(path, func(io.Writer) error { return nil }, func(info os.FileInfo) { temp = info.Name() }); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(temp, TempPrefix(name)) || len(temp) > 255 {
		t.Fatalf("temporary name %q (%d bytes) does not start with %q", temp, len(temp), TempPrefix(name))
	}
}
