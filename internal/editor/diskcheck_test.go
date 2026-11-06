package editor

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSaveRefusesFileChangedOnDisk(t *testing.T) {
	t.Parallel()
	e, path := openTemp(t, "one\n")
	d := e.Active()
	typeText(t, e, "mine ")
	putFile(t, path, "someone else wrote a longer line\n")

	err := e.Save()
	if !errors.Is(err, ErrChangedOnDisk) {
		t.Fatalf("Save: err = %v, want ErrChangedOnDisk", err)
	}
	if !strings.Contains(err.Error(), "saved.txt changed on disk") {
		t.Errorf("Save: err = %q, want it to name the file", err)
	}
	if got := readFile(t, path); got != "someone else wrote a longer line\n" {
		t.Fatalf("file = %q, the external change was overwritten", got)
	}
	if !d.Modified() {
		t.Fatal("refused save cleared the modified flag")
	}
}

func TestSaveRefusesFileCreatedAfterOpen(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "new.txt")
	e := New(Options{})
	if err := e.OpenFile(path); err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	putFile(t, path, "external\n")
	typeText(t, e, "mine")

	if err := e.Save(); !errors.Is(err, ErrChangedOnDisk) {
		t.Fatalf("Save: err = %v, want ErrChangedOnDisk", err)
	}
	if got := readFile(t, path); got != "external\n" {
		t.Fatalf("file = %q, the external file was overwritten", got)
	}
}

func TestSaveOfUnchangedFileWrites(t *testing.T) {
	t.Parallel()
	e, path := openTemp(t, "one\n")
	typeText(t, e, "mine ")
	if err := e.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if got := readFile(t, path); got != "mine one\n" {
		t.Fatalf("file = %q, want the saved text", got)
	}
	if e.Active().Modified() {
		t.Fatal("successful save left the buffer modified")
	}
}

func TestSecondSaveAfterSaveWorks(t *testing.T) {
	t.Parallel()
	e, path := openTemp(t, "one\n")
	typeText(t, e, "a ")
	if err := e.Save(); err != nil {
		t.Fatalf("first Save: %v", err)
	}
	typeText(t, e, "b ")
	if err := e.Save(); err != nil {
		t.Fatalf("second Save: %v", err)
	}
	if got := readFile(t, path); got != "a b one\n" {
		t.Fatalf("file = %q, want the text of the second save", got)
	}
}

func TestSaveAsKeepsBufferOverChangedFile(t *testing.T) {
	t.Parallel()
	e, path := openTemp(t, "one\n")
	typeText(t, e, "mine ")
	putFile(t, path, "someone else wrote a longer line\n")
	if err := e.Save(); !errors.Is(err, ErrChangedOnDisk) {
		t.Fatalf("Save: err = %v, want ErrChangedOnDisk", err)
	}
	if err := e.SaveAs(path); err != nil {
		t.Fatalf("SaveAs: %v", err)
	}
	if got := readFile(t, path); got != "mine one\n" {
		t.Fatalf("file = %q, want the buffer text", got)
	}
}

func TestSaveThroughSymlinkKeepsTheLink(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	target := filepath.Join(dir, "real.txt")
	link := filepath.Join(dir, "link.txt")
	putFile(t, target, "old\n")
	if err := os.Symlink("real.txt", link); err != nil {
		t.Fatal(err)
	}
	e := New(Options{})
	if err := e.OpenFile(link); err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	typeText(t, e, "new ")
	if err := e.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	info, err := os.Lstat(link)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&fs.ModeSymlink == 0 {
		t.Fatalf("save replaced the link with a regular file (mode %v)", info.Mode())
	}
	if got := readFile(t, target); got != "new old\n" {
		t.Fatalf("target = %q, want the saved text", got)
	}
}

func TestSaveDanglingSymlinkIsRefused(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	link := filepath.Join(dir, "link.txt")
	absent := filepath.Join(dir, "absent.txt")
	if err := os.Symlink("absent.txt", link); err != nil {
		t.Fatal(err)
	}
	e := New(Options{})
	if err := e.OpenFile(link); err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	typeText(t, e, "x")

	err := e.Save()
	if err == nil || !strings.Contains(err.Error(), link) {
		t.Fatalf("Save: err = %v, want an error naming the link %s", err, link)
	}
	info, lerr := os.Lstat(link)
	if lerr != nil || info.Mode()&fs.ModeSymlink == 0 {
		t.Fatalf("save replaced the dangling link: info=%v err=%v", info, lerr)
	}
	if _, err := os.Lstat(absent); err == nil {
		t.Fatal("save created the link target")
	}
	if !e.Active().Modified() {
		t.Fatal("failed save cleared the modified flag")
	}
}

func TestSaveAfterMoveWithChangedModTimeWrites(t *testing.T) {
	t.Parallel()
	e, path := openTemp(t, "one\n")
	typeText(t, e, "mine ")
	moved := filepath.Join(filepath.Dir(path), "moved.txt")
	if err := os.Rename(path, moved); err != nil {
		t.Fatal(err)
	}
	// A move across devices copies the file, so the moved file gets a new
	// modification time. The editor's own move must not read as an outside change.
	later := time.Now().Add(-time.Hour)
	if err := os.Chtimes(moved, later, later); err != nil {
		t.Fatal(err)
	}
	if err := e.RenamePath(path, moved); err != nil {
		t.Fatalf("RenamePath: %v", err)
	}
	if err := e.Save(); err != nil {
		t.Fatalf("Save after move: %v", err)
	}
	if got := readFile(t, moved); got != "mine one\n" {
		t.Fatalf("file = %q, want the buffer text", got)
	}
}

func TestSaveAfterMoveStillRefusesChangedSize(t *testing.T) {
	t.Parallel()
	e, path := openTemp(t, "one\n")
	typeText(t, e, "mine ")
	putFile(t, path, "someone else wrote a longer line\n")
	moved := filepath.Join(filepath.Dir(path), "moved.txt")
	if err := os.Rename(path, moved); err != nil {
		t.Fatal(err)
	}
	if err := e.RenamePath(path, moved); err != nil {
		t.Fatalf("RenamePath: %v", err)
	}
	if err := e.Save(); !errors.Is(err, ErrChangedOnDisk) {
		t.Fatalf("Save: err = %v, want ErrChangedOnDisk", err)
	}
	if got := readFile(t, moved); got != "someone else wrote a longer line\n" {
		t.Fatalf("file = %q, the outside change was overwritten", got)
	}
}

func TestSaveAfterMoveRefusesSameSizeOutsideEdit(t *testing.T) {
	t.Parallel()
	e, path := openTemp(t, "alpha\n")
	// The outside edit keeps the size, so only the bytes tell it apart from a
	// move. The new modification time differs from the one recorded at open.
	putFile(t, path, "bravo\n")
	later := time.Now().Add(-time.Hour)
	if err := os.Chtimes(path, later, later); err != nil {
		t.Fatal(err)
	}
	moved := filepath.Join(filepath.Dir(path), "moved.txt")
	if err := os.Rename(path, moved); err != nil {
		t.Fatal(err)
	}
	if err := e.RenamePath(path, moved); err != nil {
		t.Fatalf("RenamePath: %v", err)
	}
	if err := e.Save(); !errors.Is(err, ErrChangedOnDisk) {
		t.Fatalf("Save: err = %v, want ErrChangedOnDisk", err)
	}
	if got := readFile(t, moved); got != "bravo\n" {
		t.Fatalf("file = %q, the outside edit was overwritten", got)
	}
}

func TestSaveAfterMoveOfCRLFFileWithChangedModTimeWrites(t *testing.T) {
	t.Parallel()
	e, path := openTemp(t, "one\r\ntwo\r\n")
	typeText(t, e, "mine ")
	moved := filepath.Join(filepath.Dir(path), "moved.txt")
	if err := os.Rename(path, moved); err != nil {
		t.Fatal(err)
	}
	// The moved file still holds the saved bytes, with CRLF breaks, under a new
	// modification time. The move must not block the save.
	later := time.Now().Add(-time.Hour)
	if err := os.Chtimes(moved, later, later); err != nil {
		t.Fatal(err)
	}
	if err := e.RenamePath(path, moved); err != nil {
		t.Fatalf("RenamePath: %v", err)
	}
	if err := e.Save(); err != nil {
		t.Fatalf("Save after move: %v", err)
	}
	if got := readFile(t, moved); got != "mine one\r\ntwo\r\n" {
		t.Fatalf("file = %q, want the buffer text with CRLF breaks", got)
	}
}
