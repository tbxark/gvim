package editor

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAutoSaveDueFollowsTheInjectedClock(t *testing.T) {
	t.Parallel()
	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	e, _ := openFixture(t, Options{AutoSaveInterval: 5 * time.Second}, "abc\n")
	typeText(t, e, "x")
	tests := []struct {
		at      time.Duration
		wantDue bool
		save    bool // call AutoSave at this time when it is due
	}{
		{0, true, true},                 // no save yet, so due at once
		{4 * time.Second, false, false}, // inside the interval
		{5 * time.Second, true, true},   // the interval has passed
		{9 * time.Second, false, false},
		{10 * time.Second, true, false},
	}
	for _, tt := range tests {
		now := base.Add(tt.at)
		if got := e.AutoSaveDue(now); got != tt.wantDue {
			t.Fatalf("AutoSaveDue(+%v) = %v, want %v", tt.at, got, tt.wantDue)
		}
		if tt.save {
			if err := e.AutoSave(now); err != nil {
				t.Fatalf("AutoSave(+%v): %v", tt.at, err)
			}
		}
	}
}

func TestAutoSaveOffNeverSavesOrDues(t *testing.T) {
	t.Parallel()
	for _, interval := range []time.Duration{0, -time.Second} {
		e, path := openFixture(t, Options{AutoSaveInterval: interval}, "abc\n")
		typeText(t, e, "x")
		now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		if e.AutoSaveDue(now) {
			t.Fatalf("interval %v: AutoSaveDue = true, want false", interval)
		}
		if err := e.AutoSave(now); err != nil {
			t.Fatalf("interval %v: AutoSave: %v", interval, err)
		}
		if got := readFile(t, path); got != "abc\n" {
			t.Fatalf("interval %v: AutoSave wrote %q", interval, got)
		}
		if !e.Active().Modified() {
			t.Fatalf("interval %v: AutoSave cleared modified", interval)
		}
	}
}

func TestAutoSaveSavesOnlyNamedModifiedWritableDocuments(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	write := func(name, text string) string {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	pathA := write("a.txt", "a\n")
	pathB := write("b.txt", "b\n")
	pathC := write("c.txt", "c\n")

	e := New(Options{AutoSaveInterval: time.Second})
	for _, p := range []string{pathA, pathB, pathC} {
		if err := e.OpenFile(p); err != nil {
			t.Fatal(err)
		}
	}
	// Tab 0 is named and modified: it is saved.
	e.ActivateTab(0)
	typeText(t, e, "1")
	// Tab 1 is named, modified and read-only: it is skipped.
	e.ActivateTab(1)
	typeText(t, e, "2")
	run(t, e, "toggle_read_only")
	// Tab 2 is named and clean, and changed on disk behind the editor's back. It
	// is not modified, so AutoSave must not touch it.
	if err := os.WriteFile(pathC, []byte("changed on disk\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Tab 3 is untitled and modified: it has no path, so it is skipped.
	run(t, e, "new")
	typeText(t, e, "3")

	if err := e.AutoSave(time.Now()); err != nil {
		t.Fatalf("AutoSave: %v", err)
	}
	if got := readFile(t, pathA); got != "1a\n" {
		t.Fatalf("a.txt = %q, want the autosaved text", got)
	}
	if got := readFile(t, pathB); got != "b\n" {
		t.Fatalf("read-only b.txt = %q, want the disk text", got)
	}
	if got := readFile(t, pathC); got != "changed on disk\n" {
		t.Fatalf("clean c.txt = %q, want the disk text", got)
	}
	if tabs := e.Tabs(); tabs[3].Path != "" || !tabs[3].Modified {
		t.Fatalf("untitled tab = %+v, want still modified", tabs[3])
	}
}

func TestAutoSaveReportsFailuresAndSavesTheRest(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	pathX := filepath.Join(dir, "x.txt")
	pathY := filepath.Join(dir, "y.txt")
	for _, p := range []string{pathX, pathY} {
		if err := os.WriteFile(p, []byte(filepath.Base(p)+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	e := New(Options{AutoSaveInterval: time.Second})
	if err := e.OpenFile(pathX); err != nil {
		t.Fatal(err)
	}
	typeText(t, e, "1")
	if err := e.OpenFile(pathY); err != nil {
		t.Fatal(err)
	}
	typeText(t, e, "2")
	// x.txt changes on disk after it was opened, so its save is refused.
	if err := os.WriteFile(pathX, []byte("external change\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	err := e.AutoSave(time.Now())
	if !errors.Is(err, ErrChangedOnDisk) {
		t.Fatalf("AutoSave err = %v, want ErrChangedOnDisk", err)
	}
	if got := readFile(t, pathY); got != "2y.txt\n" {
		t.Fatalf("y.txt = %q, want the save to go on past the failure", got)
	}
	if got := readFile(t, pathX); got != "external change\n" {
		t.Fatalf("x.txt = %q, want the outside change kept", got)
	}
	e.ActivateTab(0)
	if !strings.Contains(e.Status(), "changed on disk") {
		t.Fatalf("status = %q, want the failure shown on x.txt", e.Status())
	}
}
