//go:build unix

package editor

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/tbxark/gvim/atomicfile"
	"github.com/tbxark/gvim/buffer"
)

func statOf(t *testing.T, path string) *syscall.Stat_t {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.Sys().(*syscall.Stat_t)
}

// otherGroup returns a group of the user that is not gid, or skips the test.
func otherGroup(t *testing.T, gid int) int {
	t.Helper()
	groups, _ := os.Getgroups()
	for _, g := range groups {
		if g != gid {
			return g
		}
	}
	t.Skip("the user has no second group")
	return 0
}

// savers are the ways a document is saved: a small one and a large one.
var savers = []struct {
	name string
	open func(t *testing.T, path string) (*Editor, *Document)
}{
	{"small", func(t *testing.T, path string) (*Editor, *Document) {
		t.Helper()
		e := New(Options{})
		if err := e.OpenFile(path); err != nil {
			t.Fatal(err)
		}
		return e, e.Active()
	}},
	{"large", func(t *testing.T, path string) (*Editor, *Document) {
		t.Helper()
		e := New(largeOptions())
		if err := e.OpenFile(path); err != nil {
			t.Fatal(err)
		}
		indexAll(t, e)
		return e, e.Active()
	}},
}

func TestSaveKeepsTheGroupOfTheFile(t *testing.T) {
	t.Parallel()
	for _, sv := range savers {
		t.Run(sv.name, func(t *testing.T) {
			t.Parallel()
			path := writeLargeFile(t, "shared.txt", lineText(150))
			group := otherGroup(t, int(statOf(t, path).Gid))
			if err := os.Chown(path, -1, group); err != nil {
				t.Skipf("chgrp: %v", err)
			}
			e, d := sv.open(t, path)
			d.Buffer.Insert(buffer.Pos{}, "A")
			if err := e.Save(); err != nil {
				t.Fatal(err)
			}
			if got := int(statOf(t, path).Gid); got != group {
				t.Errorf("gid = %d after the save, want %d", got, group)
			}
			if !strings.HasPrefix(d.status, "Saved") || strings.Contains(d.status, "could not") {
				t.Errorf("status = %q, want a plain Saved", d.status)
			}
		})
	}
}

func TestSaveKeepsTheSetGroupBit(t *testing.T) {
	t.Parallel()
	for _, sv := range savers {
		t.Run(sv.name, func(t *testing.T) {
			t.Parallel()
			path := writeLargeFile(t, "shared.txt", lineText(150))
			if err := os.Chmod(path, 0o640|os.ModeSetgid); err != nil {
				t.Skipf("chmod: %v", err)
			}
			if info, _ := os.Stat(path); info.Mode()&os.ModeSetgid == 0 {
				t.Skip("this system does not keep the set-group bit")
			}
			e, d := sv.open(t, path)
			d.Buffer.Insert(buffer.Pos{}, "A")
			if err := e.Save(); err != nil {
				t.Fatal(err)
			}
			info, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			if info.Mode()&os.ModeSetgid == 0 || info.Mode().Perm() != 0o640 {
				t.Errorf("mode = %v after the save, want 0640 and setgid", info.Mode())
			}
		})
	}
}

func TestSaveSaysWhenALinkIsBroken(t *testing.T) {
	t.Parallel()
	for _, sv := range savers {
		t.Run(sv.name, func(t *testing.T) {
			t.Parallel()
			path := writeLargeFile(t, "shared.txt", lineText(150))
			other := filepath.Join(filepath.Dir(path), "other-name.txt")
			if err := os.Link(path, other); err != nil {
				t.Skipf("link: %v", err)
			}
			e, d := sv.open(t, path)
			d.Buffer.Insert(buffer.Pos{}, "A")
			if err := e.Save(); err != nil {
				t.Fatal(err)
			}
			if !strings.HasPrefix(readFile(t, path), "Aline 0000") {
				t.Fatal("the file was not saved")
			}
			if readFile(t, other) != lineText(150) {
				t.Fatal("setup: the other name changed, so the link was not broken")
			}
			if !strings.Contains(d.status, "link") {
				t.Errorf("status = %q: it does not say that the link was broken", d.status)
			}
			// The next save has a file with one name and says nothing.
			d.Buffer.Insert(buffer.Pos{}, "B")
			if err := e.Save(); err != nil {
				t.Fatal(err)
			}
			if !strings.HasPrefix(d.status, "Saved") || strings.Contains(d.status, "link") {
				t.Errorf("status = %q after the second save, want a plain Saved", d.status)
			}
		})
	}
}

func TestSaveSaysWhenTheOwnerCannotBeKept(t *testing.T) {
	// Not parallel: it replaces atomicfile.FchownFn.
	orig := atomicfile.FchownFn
	atomicfile.FchownFn = func(*os.File, int, int) error { return syscall.EPERM }
	t.Cleanup(func() { atomicfile.FchownFn = orig })
	path := writeLargeFile(t, "owned.txt", lineText(150))
	group := otherGroup(t, int(statOf(t, path).Gid))
	if err := os.Chown(path, -1, group); err != nil {
		t.Skipf("chgrp: %v", err)
	}
	for _, sv := range savers {
		t.Run(sv.name, func(t *testing.T) {
			e, d := sv.open(t, path)
			d.Buffer.Insert(buffer.Pos{}, "A")
			if err := e.Save(); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(d.status, "group") {
				t.Errorf("status = %q: it does not say that the group changed", d.status)
			}
			// The file is back in the saver's group by the first save; give it back
			// for the next case.
			if err := os.Chown(path, -1, group); err != nil {
				t.Fatal(err)
			}
		})
	}
}
