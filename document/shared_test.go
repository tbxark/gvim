package document

import (
	"testing"

	"github.com/tbxark/gvim/buffer"
)

func TestNewMirrorDocumentCarriesModifiedState(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		edit bool
	}{
		{"unmodified owner", false},
		{"modified owner", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			owner := NewDocument("a.txt", "one\ntwo\n")
			if tt.edit {
				owner.Buffer.Insert(buffer.Pos{}, "X")
			}
			mirror, err := NewMirrorDocument(owner)
			if err != nil {
				t.Fatal(err)
			}
			if got := mirror.Buffer.Modified(); got != tt.edit {
				t.Fatalf("mirror Modified = %v, want %v", got, tt.edit)
			}
			if got, want := mirror.Buffer.Text(), owner.Buffer.Text(); got != want {
				t.Fatalf("mirror text = %q, want %q", got, want)
			}
		})
	}
}

func TestSyncMirrorFromReplacesTheBuffer(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		edit bool
	}{
		{"unmodified owner", false},
		{"modified owner", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			owner := NewDocument("a.txt", "one\ntwo\n")
			mirror, err := NewMirrorDocument(owner)
			if err != nil {
				t.Fatal(err)
			}
			mirror.Cursor = buffer.Pos{Line: 2}
			oldVersion := mirror.Buffer.Version()
			owner.Buffer.Delete(buffer.Pos{Line: 1}, buffer.Pos{Line: 2})
			if !tt.edit {
				owner.Buffer.MarkSaved()
			}
			if err := mirror.SyncMirrorFrom(owner); err != nil {
				t.Fatal(err)
			}
			if got, want := mirror.Buffer.Text(), owner.Buffer.Text(); got != want {
				t.Fatalf("mirror text = %q, want %q", got, want)
			}
			if _, ok := mirror.Buffer.Undo(); ok {
				t.Error("the sync left an undo step on the read-only mirror")
			}
			if got := mirror.Buffer.Modified(); got != tt.edit {
				t.Errorf("mirror Modified = %v, want %v", got, tt.edit)
			}
			if mirror.Buffer.Version() == oldVersion {
				t.Error("the replacement buffer reused a version of the old one")
			}
			if want := mirror.Buffer.Clamp(mirror.Cursor); mirror.Cursor != want {
				t.Errorf("cursor %v is outside the text", mirror.Cursor)
			}
			// The next owner edit applies incrementally: the recorded versions are in step.
			var got buffer.Change
			owner.Buffer.SetObserver(func(c buffer.Change) { got = c })
			owner.Buffer.Insert(buffer.Pos{}, "Z")
			if err := mirror.ApplyOwnerChange(owner, got); err != nil {
				t.Fatal(err)
			}
			if g, w := mirror.Buffer.Text(), owner.Buffer.Text(); g != w {
				t.Fatalf("mirror text after an owner edit = %q, want %q", g, w)
			}
		})
	}
}

func TestApplyOwnerChangeClampsCaretAndSelection(t *testing.T) {
	t.Parallel()
	owner := NewDocument("a.txt", "one\ntwo\nthree\n")
	mirror, err := NewMirrorDocument(owner)
	if err != nil {
		t.Fatal(err)
	}
	mirror.Cursor = buffer.Pos{Line: 2, Col: 4}
	mirror.Sel = &buffer.Selection{Anchor: buffer.Pos{Line: 1, Col: 1}, Head: mirror.Cursor}
	var change buffer.Change
	owner.Buffer.SetObserver(func(c buffer.Change) { change = c })
	owner.Buffer.Delete(buffer.Pos{Line: 0, Col: 3}, buffer.Pos{Line: 2, Col: 5})
	if err := mirror.ApplyOwnerChange(owner, change); err != nil {
		t.Fatal(err)
	}
	if got, want := mirror.Buffer.Text(), owner.Buffer.Text(); got != want {
		t.Fatalf("mirror text = %q, want %q", got, want)
	}
	if want := mirror.Buffer.Clamp(mirror.Cursor); mirror.Cursor != want {
		t.Errorf("cursor %v is outside the text", mirror.Cursor)
	}
	if mirror.Sel != nil {
		if a := mirror.Buffer.Clamp(mirror.Sel.Anchor); a != mirror.Sel.Anchor {
			t.Errorf("selection anchor %v is outside the text", mirror.Sel.Anchor)
		}
	}
}
