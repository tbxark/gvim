package editor

import (
	"errors"
	"strings"
	"testing"

	"github.com/tbxark/gvim/internal/buffer"
	"github.com/tbxark/gvim/internal/session"
)

var errTestOpen = errors.New("cannot open")

// restoredText opens the file at the path of d again, puts st back and returns the text
// of the new document once the index is complete.
func restoredText(t *testing.T, opts Options, d *Document, st session.OverlayStash) string {
	t.Helper()
	fresh, nd := sameFileOpened(t, opts, d.Path)
	if err := fresh.RestoreOverlay(nd, st); err != nil {
		t.Fatalf("RestoreOverlay: %v", err)
	}
	indexAll(t, fresh)
	if nd.restoring() {
		t.Fatal("the restore did not finish")
	}
	return nd.Buffer.Text()
}

func TestStashAfterSaveWithEditsDuringTheWrite(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		// during edits the buffer between the snapshot and the apply.
		during func(d *Document)
		// before edits the buffer before the save is planned.
		before func(d *Document)
	}{
		{
			name:   "edit on another line",
			before: func(d *Document) { d.Buffer.Insert(buffer.Pos{}, "A") },
			during: func(d *Document) { d.Buffer.Insert(buffer.Pos{Line: 1}, "B") },
		},
		{
			name:   "edit on the line that was saved",
			before: func(d *Document) { d.Buffer.Insert(buffer.Pos{Line: 3}, "A") },
			during: func(d *Document) { d.Buffer.Insert(buffer.Pos{Line: 3}, "B") },
		},
		{
			name:   "lines added and removed after the saved ones",
			before: func(d *Document) { d.Buffer.Insert(buffer.Pos{Line: 10}, "x\ny\nz") },
			during: func(d *Document) {
				d.Buffer.Delete(buffer.Pos{Line: 20}, buffer.Pos{Line: 25})
				d.Buffer.Insert(buffer.Pos{Line: 140}, "tail\n")
			},
		},
		{
			name:   "line deleted after the saved edit",
			before: func(d *Document) { d.Buffer.Insert(buffer.Pos{Line: 5}, "A") },
			during: func(d *Document) { d.Buffer.Delete(buffer.Pos{Line: 5}, buffer.Pos{Line: 6}) },
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			opts := largeOptions()
			e, d := openIndexed(t, opts, lineText(150))
			tt.before(d)
			plan, err := e.PlanSave()
			if err != nil {
				t.Fatal(err)
			}
			results := plan.Run()
			tt.during(d)
			if err := e.ApplySavePlan(plan, results); err != nil {
				t.Fatal(err)
			}
			if !d.Modified() {
				t.Fatal("the document was edited during the save and is not modified")
			}
			want := d.Buffer.Text()
			st := stashOf(t, d)
			if got := restoredText(t, opts, d, st); got != want {
				t.Errorf("the restored text differs:\n got %q\nwant %q", got, want)
			}
		})
	}
}

func TestStashAfterSaveWithEditsDuringTheWriteThenEditsAgain(t *testing.T) {
	t.Parallel()
	opts := largeOptions()
	e, d := openIndexed(t, opts, lineText(150))
	d.Buffer.Insert(buffer.Pos{}, "A")
	plan, _ := e.PlanSave()
	results := plan.Run()
	d.Buffer.Insert(buffer.Pos{Line: 1}, "B")
	if err := e.ApplySavePlan(plan, results); err != nil {
		t.Fatal(err)
	}
	// A second save writes the newer text, and the document is then clean.
	d.Buffer.Insert(buffer.Pos{Line: 2}, "C")
	if err := e.Save(); err != nil {
		t.Fatal(err)
	}
	d.Buffer.Insert(buffer.Pos{Line: 3}, "D")
	want := d.Buffer.Text()
	if !strings.Contains(want, "Dline 0003") {
		t.Fatalf("setup: %q", want[:40])
	}
	if got := restoredText(t, opts, d, stashOf(t, d)); got != want {
		t.Errorf("the restored text differs:\n got %q\nwant %q", got, want)
	}
}

func TestStashAfterSaveWhoseRebaseFailed(t *testing.T) {
	t.Parallel()
	opts := largeOptions()
	fail := false
	opts.openSrc = &sourceOpener{open: func(path string) (buffer.Source, error) {
		if fail {
			return nil, errTestOpen
		}
		return buffer.OpenFileSource(path)
	}}
	e, d := openIndexed(t, opts, lineText(150))
	d.Buffer.Insert(buffer.Pos{}, "A")
	fail = true
	if err := e.Save(); err != nil {
		t.Fatal(err)
	}
	fail = false
	if !d.large.rebaseFailed {
		t.Fatal("setup: the rebase did not fail")
	}
	d.Buffer.Insert(buffer.Pos{Line: 7}, "B")
	want := d.Buffer.Text()
	st := stashOf(t, d)
	if got := restoredText(t, opts, d, st); got != want {
		t.Errorf("the restored text differs:\n got %q\nwant %q", got, want)
	}
}
