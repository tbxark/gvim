package editor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tbxark/gvim/buffer"
)

// writeSharedFile writes a file for the cross-workspace sharing tests.
func writeSharedFile(t *testing.T, text string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "shared.txt")
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestSharedBuffersOpenInTwoWorkspaces: the second workspace opens a mirror and, being
// focused, takes ownership; the first becomes a read-only mirror; edits propagate; undo
// and unsaved state travel with the canonical document.
func TestSharedBuffersOpenInTwoWorkspaces(t *testing.T) {
	path := writeSharedFile(t, "one\ntwo\n")
	shared := NewSharedBuffers()
	a := New(Options{Empty: true, Shared: shared})
	b := New(Options{Empty: true, Shared: shared})
	shared.Focus(a)
	if err := a.OpenFile(path); err != nil {
		t.Fatalf("open in first workspace: %v", err)
	}
	shared.Focus(b)
	if err := b.OpenFile(path); err != nil {
		t.Fatalf("open in second workspace: %v", err)
	}
	if b.Active().SharedMirror() || b.Active().ReadOnly() {
		t.Fatal("the focused second workspace did not take ownership")
	}
	if !a.Active().SharedMirror() || !a.Active().ReadOnly() {
		t.Fatal("the first workspace is not a read-only mirror after the ownership move")
	}
	// An owner edit reaches the mirror through the explicit change path.
	b.Active().Buffer.Insert(buffer.Pos{Line: 0, Col: 0}, "X")
	if got := a.Active().Buffer.Text(); got != "Xone\ntwo\n" {
		t.Fatalf("mirror text after the owner edit = %q", got)
	}
	if !b.Active().Modified() || !a.Active().Modified() {
		t.Fatal("the unsaved state did not reach the mirror")
	}
	// Ownership moves back on focus, keeping the text and the unsaved state; the old
	// owner becomes the mirror.
	shared.Focus(a)
	if a.Active().SharedMirror() || a.Active().ReadOnly() {
		t.Fatal("the focused mirror did not become the owner")
	}
	if !b.Active().SharedMirror() || !b.Active().ReadOnly() {
		t.Fatal("the old owner did not become a read-only mirror")
	}
	a.Active().Buffer.Insert(buffer.Pos{Line: 1, Col: 0}, "Y")
	if got := b.Active().Buffer.Text(); got != "Xone\nYtwo\n" {
		t.Fatalf("mirror text after the second ownership edit = %q", got)
	}
	// Undo travels with the canonical document: the edit just made in a is undoable
	// there after the handoff and the mirror follows the undo.
	if _, ok := a.Active().Buffer.Undo(); !ok {
		t.Fatal("the canonical buffer lost its undo history on the handoff")
	}
	if got, want := b.Active().Buffer.Text(), a.Active().Buffer.Text(); got != want {
		t.Fatalf("mirror text after undo = %q, owner = %q", got, want)
	}
}

// TestSharedBuffersConflictNotice: a refused ownership request leaves a read-only
// mirror with the conflict in its status, and no edit is lost.
func TestSharedBuffersConflictNotice(t *testing.T) {
	path := writeSharedFile(t, "alpha\n")
	shared := NewSharedBuffers()
	a := New(Options{Empty: true, Shared: shared})
	b := New(Options{Empty: true, Shared: shared})
	shared.Focus(a)
	if err := a.OpenFile(path); err != nil {
		t.Fatal(err)
	}
	shared.Focus(b)
	if err := b.OpenFile(path); err != nil {
		t.Fatal(err)
	}
	// Now a is the mirror. Refuse the next ownership request.
	shared.Deny = func(owner, requester *Editor) error {
		return errTestShareConflict
	}
	shared.Focus(a)
	if !a.Active().SharedMirror() || !a.Active().ReadOnly() {
		t.Fatal("a refused ownership request did not leave the mirror read-only")
	}
	if !strings.Contains(a.Active().Status, "conflict") {
		t.Fatalf("conflict status = %q", a.Active().Status)
	}
	// The mirror is read-only, and the owner did not change on the refused request.
	if !b.Active().SharedMirror() == false {
		t.Fatal("the owner changed on a refused request")
	}
	shared.Deny = nil
	shared.Focus(a)
	if a.Active().SharedMirror() {
		t.Fatal("ownership was not taken after the refusal was cleared")
	}
}

// TestSharedBuffersCloseKeepsTheOther: closing the owning workspace promotes a mirror
// instead of dropping the text; closing a mirror leaves the owner untouched.
func TestSharedBuffersCloseKeepsTheOther(t *testing.T) {
	path := writeSharedFile(t, "keep\n")
	shared := NewSharedBuffers()
	a := New(Options{Empty: true, Shared: shared})
	b := New(Options{Empty: true, Shared: shared})
	shared.Focus(a)
	if err := a.OpenFile(path); err != nil {
		t.Fatal(err)
	}
	shared.Focus(b)
	if err := b.OpenFile(path); err != nil {
		t.Fatal(err)
	}
	// a mirrors b now. Close the mirror: the owner is unaffected.
	shared.DropEditor(a)
	if b.Active().SharedMirror() || b.Active().ReadOnly() {
		t.Fatal("closing the mirror disturbed the owner")
	}
	// a is gone from the registry; b still owns.
	if !shared.Has(shared.CanonicalPath(path)) {
		t.Fatal("closing the only mirror dropped the shared entry")
	}
	// Close the owner with its mirror: make a a fresh mirror again, then drop the owner.
	shared.Focus(a)
	if err := a.OpenFile(path); err != nil {
		t.Fatal(err)
	}
	shared.Focus(a)
	if a.Active().SharedMirror() {
		t.Fatal("the re-opened editor is not the owner")
	}
	shared.DropEditor(b)
	if a.Active().SharedMirror() || a.Active().ReadOnly() {
		t.Fatal("closing the owner lost the buffer instead of promoting the mirror")
	}
	if got := a.Active().Buffer.Text(); got != "keep\n" {
		t.Fatalf("promoted text = %q", got)
	}
}

// errTestShareConflict is the injected ownership refusal.
var errTestShareConflict = &shareConflictError{}

type shareConflictError struct{}

func (*shareConflictError) Error() string { return "the owner is busy" }

// TestSharedBuffersMirrorReloadsOnOutOfBandChange: a mirror whose text was changed behind
// the registry's back cannot apply the next owner change by range; the stale-version guard
// reloads the whole text from the owner instead.
func TestSharedBuffersMirrorReloadsOnOutOfBandChange(t *testing.T) {
	path := writeSharedFile(t, "base\n")
	shared := NewSharedBuffers()
	a := New(Options{Empty: true, Shared: shared})
	b := New(Options{Empty: true, Shared: shared})
	shared.Focus(a)
	if err := a.OpenFile(path); err != nil {
		t.Fatal(err)
	}
	shared.Focus(b)
	if err := b.OpenFile(path); err != nil {
		t.Fatal(err)
	}
	// a is the read-only mirror of b. Change its buffer out of band; the next owner
	// change must not be applied onto the diverged mirror.
	a.Active().Buffer.Insert(buffer.Pos{Line: 0, Col: 0}, "OUT-OF-BAND ")
	b.Active().Buffer.Insert(buffer.Pos{Line: 0, Col: 0}, "owner ")
	want := b.Active().Buffer.Text()
	if got := a.Active().Buffer.Text(); got != want {
		t.Fatalf("mirror text after reload = %q, want the owner text %q", got, want)
	}
	if !strings.Contains(a.Active().Status, "reloaded") {
		t.Fatalf("mirror status = %q, want the reload notice", a.Active().Status)
	}
}
