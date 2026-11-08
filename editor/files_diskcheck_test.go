package editor

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPlanDiskCheckReportsFilesChangedOutsideTheEditor(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	write := func(name, content string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	a, b := write("a.txt", "a\n"), write("b.txt", "b\n")
	e := New(Options{})
	for _, p := range []string{a, b} {
		if err := e.OpenFile(p); err != nil {
			t.Fatal(err)
		}
	}
	check := func() map[string]DiskChange {
		out := map[string]DiskChange{}
		for _, c := range e.PlanDiskCheck()() {
			out[filepath.Base(c.Path)] = c
		}
		return out
	}
	for name, c := range check() {
		if c.Changed {
			t.Fatalf("%s reported changed right after it was opened", name)
		}
	}

	// An outside edit of a.txt, which also changes its size.
	write("a.txt", "a changed\n")
	later := time.Now().Add(time.Minute)
	if err := os.Chtimes(a, later, later); err != nil {
		t.Fatal(err)
	}
	got := check()
	if !got["a.txt"].Changed || got["b.txt"].Changed {
		t.Fatalf("changed = a:%v b:%v, want a only", got["a.txt"].Changed, got["b.txt"].Changed)
	}
	first := got["a.txt"].Stamp
	if again := check()["a.txt"]; again.Stamp != first {
		t.Fatalf("stamp changed from %q to %q without a new edit", first, again.Stamp)
	}

	// The editor's own save records the new state, so it is no longer a change.
	if r := e.Do("insert_char", map[string]any{"text": "x"}); r.Err != nil {
		t.Fatal(r.Err)
	}
	// The active document is b.txt, whose file did not change.
	if err := e.Save(); err != nil {
		t.Fatal(err)
	}
	if c := check()["b.txt"]; c.Changed {
		t.Fatal("the editor's own save of b.txt counts as an outside change")
	}

	// A deleted file is not a change to reload.
	if err := os.Remove(b); err != nil {
		t.Fatal(err)
	}
	if c := check()["b.txt"]; c.Changed {
		t.Fatal("a deleted file counts as a change")
	}
}

func TestPlanDiskCheckSkipsDocumentsWithoutAFile(t *testing.T) {
	t.Parallel()
	e := New(Options{})
	run(t, e, "new")
	if got := e.PlanDiskCheck()(); len(got) != 0 {
		t.Fatalf("DiskCheck of untitled buffers = %+v, want none", got)
	}
}
