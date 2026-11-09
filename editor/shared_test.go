package editor

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/document"
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

// TestSharedBuffersCloseOwnerPromotesMirror: closing the owner's tab while another
// workspace mirrors the buffer does not ask the unsaved question; it promotes the mirror
// and leaves the text, undo history and unsaved state in the surviving workspace.
func TestSharedBuffersCloseOwnerPromotesMirror(t *testing.T) {
	path := writeSharedFile(t, "one\n")
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
	// b owns; a mirrors. Make an unsaved edit and an undoable history in the owner.
	b.Active().Buffer.Insert(buffer.Pos{Line: 0, Col: 0}, "unsaved ")
	if index, ok := b.DocumentIndexByPath(path); !ok {
		t.Fatal("the owner has no tab for the shared path")
	} else {
		confirm, err := b.CloseNeedsConfirm(index)
		if err != nil || confirm {
			t.Fatalf("closing the owner tab with a mirror asks for confirmation: %v %v", confirm, err)
		}
		if err := b.CloseTab(index, false); err != nil {
			t.Fatalf("closing the owner tab with a mirror: %v", err)
		}
	}
	if index, ok := b.DocumentIndexByPath(path); ok {
		t.Fatalf("the owner tab is still open at %d", index)
	}
	if a.Active().SharedMirror() || a.Active().ReadOnly() {
		t.Fatal("the mirror was not promoted to owner")
	}
	if got := a.Active().Buffer.Text(); got != "unsaved one\n" {
		t.Fatalf("promoted text = %q, want the owner's unsaved text", got)
	}
	if !a.Active().Modified() {
		t.Fatal("the promoted buffer lost its unsaved state")
	}
	if _, ok := a.Active().Buffer.Undo(); !ok {
		t.Fatal("the promoted buffer lost its undo history")
	}
	if got := a.Active().Buffer.Text(); got != "one\n" {
		t.Fatalf("undo on the promoted buffer = %q", got)
	}
}

// TestSharedBuffersCloseMirrorNeedsNoPrompt: a read-only mirror closes without a save
// question and without disturbing the owner.
func TestSharedBuffersCloseMirrorNeedsNoPrompt(t *testing.T) {
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
	b.Active().Buffer.Insert(buffer.Pos{Line: 0, Col: 0}, "owner ")
	// a mirrors b and is modified too, but closing it loses nothing.
	if index, ok := a.DocumentIndexByPath(path); !ok {
		t.Fatal("the mirror has no tab")
	} else if confirm, err := a.CloseNeedsConfirm(index); err != nil || confirm {
		t.Fatalf("closing the mirror asks for confirmation: %v %v", confirm, err)
	} else if err := a.CloseTab(index, false); err != nil {
		t.Fatalf("closing the mirror: %v", err)
	}
	if !shared.Has(shared.CanonicalPath(path)) {
		t.Fatal("closing the mirror dropped the shared entry")
	}
	if b.Active().SharedMirror() || b.Active().ReadOnly() {
		t.Fatal("closing the mirror disturbed the owner")
	}
	if got := b.Active().Buffer.Text(); got != "owner alpha\n" {
		t.Fatalf("owner text = %q", got)
	}
}

// TestSharedBuffersCloseLastViewPrompts: with no other view left, the owner tab keeps the
// ordinary unsaved prompt; forcing the close is the only way to drop it, and it removes
// the registry entry.
func TestSharedBuffersCloseLastViewPrompts(t *testing.T) {
	path := writeSharedFile(t, "last\n")
	shared := NewSharedBuffers()
	a := New(Options{Empty: true, Shared: shared})
	shared.Focus(a)
	if err := a.OpenFile(path); err != nil {
		t.Fatal(err)
	}
	a.Active().Buffer.Insert(buffer.Pos{Line: 0, Col: 0}, "edit ")
	index, ok := a.DocumentIndexByPath(path)
	if !ok {
		t.Fatal("no tab")
	}
	if confirm, err := a.CloseNeedsConfirm(index); err != nil || !confirm {
		t.Fatalf("the last view did not ask for confirmation: %v %v", confirm, err)
	}
	if err := a.CloseTab(index, false); err == nil {
		t.Fatal("closing the last modified view without force succeeded")
	}
	if _, ok := a.DocumentIndexByPath(path); !ok {
		t.Fatal("the refused close dropped the tab")
	}
	if err := a.CloseTab(index, true); err != nil {
		t.Fatalf("forcing the last view closed: %v", err)
	}
	if shared.Has(shared.CanonicalPath(path)) {
		t.Fatal("closing the last view left a shared entry")
	}
}

// TestSharedBuffersCloseByPathPromotes: a bulk path close (a deleted file/folder) takes
// the same close path: the owner promotes a mirror instead of dropping unsaved text.
func TestSharedBuffersCloseByPathPromotes(t *testing.T) {
	path := writeSharedFile(t, "bulk\n")
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
	b.Active().Buffer.Insert(buffer.Pos{Line: 0, Col: 0}, "keep ")
	if err := b.CloseByPath(path, false); err != nil {
		t.Fatalf("CloseByPath on a shared owner: %v", err)
	}
	if _, ok := b.DocumentIndexByPath(path); ok {
		t.Fatal("the closed path is still open in the owner workspace")
	}
	if a.Active().SharedMirror() || a.Active().ReadOnly() {
		t.Fatal("CloseByPath did not promote the mirror")
	}
	if got := a.Active().Buffer.Text(); got != "keep bulk\n" {
		t.Fatalf("promoted text after CloseByPath = %q", got)
	}
}

// TestSharedBuffersAdoptLoadedStartupRestore: a workspace restored at startup with the
// same file another workspace owns becomes a read-only mirror of the owner's live text
// (not an independent copy), and a restored UNSAVED copy whose text differs is kept as
// an untitled conflict tab instead of overwriting the owner.
func TestSharedBuffersAdoptLoadedStartupRestore(t *testing.T) {
	path := writeSharedFile(t, "base\n")
	shared := NewSharedBuffers()
	a := New(Options{Empty: true, Shared: shared})
	b := New(Options{Empty: true, Shared: shared})
	shared.Focus(a)
	if err := a.OpenFile(path); err != nil {
		t.Fatal(err)
	}
	a.Active().Buffer.Insert(buffer.Pos{Line: 0, Col: 0}, "owner ")
	ownerText := a.Active().Buffer.Text()
	// A restored session of the second workspace: same path, different unsaved text.
	restored := document.NewStdinDocument("stale unsaved\n")
	restored.Rename(path)
	restored.Buffer.Insert(buffer.Pos{Line: 0, Col: 0}, "stale ")
	shared.Focus(b)
	b.OpenLoaded(restored)
	// The adoption is deferred until the restored workspace is focused: the restore
	// pipeline applies the stash after OpenLoaded. Focus then adopts the owner's live
	// text, keeps the conflicting unsaved copy in an untitled tab and moves ownership.
	shared.Focus(b)
	if b.Active().SharedMirror() || b.Active().ReadOnly() {
		t.Fatal("the focused restored workspace did not take ownership")
	}
	var conflict *document.Document
	for i := 0; i < len(b.Tabs()); i++ {
		if d := b.DocAt(i); d != nil && d.Path == "" {
			conflict = d
			break
		}
	}
	if conflict == nil {
		t.Fatal("the restored conflicting unsaved copy was discarded")
	}
	if got := conflict.Buffer.Text(); got != "stale stale unsaved\n" {
		t.Fatalf("conflict copy text = %q", got)
	}
	if !strings.Contains(conflict.Status, "Restored unsaved copy") {
		t.Fatalf("conflict status = %q", conflict.Status)
	}
	// The switch moved the canonical document (owner text, undo, unsaved state) to the
	// restored workspace; the old workspace keeps a mirror.
	if got := b.Active().Buffer.Text(); got != ownerText {
		t.Fatalf("restored workspace owner text = %q, want the live owner text %q", got, ownerText)
	}
	if !a.Active().SharedMirror() || !a.Active().ReadOnly() {
		t.Fatal("the original workspace is not a read-only mirror after the switch")
	}
	if got, err := os.ReadFile(path); err != nil || string(got) != "base\n" {
		t.Fatalf("the restored conflict overwrote the disk file: %q, %v", got, err)
	}
}

// TestSharedBuffersAdoptLoadedUnmodified: an unmodified restored copy of a shared file is
// simply replaced by the owner's mirror (no conflict tab, no independent copy).
func TestSharedBuffersAdoptLoadedUnmodified(t *testing.T) {
	path := writeSharedFile(t, "plain\n")
	shared := NewSharedBuffers()
	a := New(Options{Empty: true, Shared: shared})
	b := New(Options{Empty: true, Shared: shared})
	shared.Focus(a)
	if err := a.OpenFile(path); err != nil {
		t.Fatal(err)
	}
	restored := document.NewStdinDocument("plain\n")
	restored.Rename(path)
	b.OpenLoaded(restored)
	shared.Focus(b)
	if len(b.Tabs()) != 1 {
		t.Fatalf("the unmodified restore opened %d tabs, want only the shared buffer", len(b.Tabs()))
	}
	if b.Active().SharedMirror() || b.Active().ReadOnly() {
		t.Fatal("the focused unmodified restore did not take ownership")
	}
	if got := b.Active().Buffer.Text(); got != "plain\n" {
		t.Fatalf("restored text = %q", got)
	}
	if shared.OwnerLabel(shared.CanonicalPath(path)) == "" {
		t.Fatal("the shared entry lost its owner label")
	}
}

// sharedConflictEditor sets up two workspaces sharing path: a owns it with live unsaved
// text, b restores an unsaved stale copy of the same path and takes ownership once, then
// a takes ownership back. b therefore holds a mirror tab and an untitled conflict copy.
func sharedConflictEditor(t *testing.T, shared *SharedBuffers) (a, b *Editor, path, conflictText string) {
	t.Helper()
	path = writeSharedFile(t, "base\n")
	a = New(Options{Empty: true, Shared: shared})
	b = New(Options{Empty: true, Shared: shared})
	shared.Focus(a)
	if err := a.OpenFile(path); err != nil {
		t.Fatal(err)
	}
	a.Active().Buffer.Insert(buffer.Pos{}, "owner ")
	restored := document.NewStdinDocument("stale\n")
	restored.Rename(path)
	restored.Buffer.Insert(buffer.Pos{}, "stale ")
	b.OpenLoaded(restored)
	shared.Focus(b)
	shared.Focus(a)
	for i := 0; i < len(b.Tabs()); i++ {
		if d := b.DocAt(i); d != nil && d.Path == "" {
			conflictText = d.Buffer.Text()
		}
	}
	if conflictText == "" {
		t.Fatal("the conflict tab is missing")
	}
	return a, b, path, conflictText
}

// TestSharedBuffersConflictSaveAsCannotOverwriteOwner: Save As (even forced) to the path
// the other workspace owns is refused, so a restored conflict copy can never overwrite
// the owner's live file; the disk and the owner's text stay untouched.
func TestSharedBuffersConflictSaveAsCannotOverwriteOwner(t *testing.T) {
	shared := NewSharedBuffers()
	shared.Label = func(ed *Editor) string {
		switch ed {
		case nil:
			return ""
		}
		return "A"
	}
	a, b, path, _ := sharedConflictEditor(t, shared)
	// Close the mirror tab: only the conflict copy is left in b, so the ordinary open-tab
	// check cannot catch the target; the shared registry must.
	for i := 0; i < len(b.Tabs()); i++ {
		if d := b.DocAt(i); d != nil && d.Path == path {
			if err := b.CloseTab(i, true); err != nil {
				t.Fatalf("close mirror: %v", err)
			}
			break
		}
	}
	var conflictIdx = -1
	for i := 0; i < len(b.Tabs()); i++ {
		if d := b.DocAt(i); d != nil && d.Path == "" && d.Modified() {
			conflictIdx = i
			break
		}
	}
	if conflictIdx < 0 {
		t.Fatal("no conflict tab")
	}
	b.ActivateTab(conflictIdx)
	if _, err := b.PlanSaveAs(path, true); err == nil || !strings.Contains(err.Error(), "open in A") {
		t.Fatalf("forced save-as over the owner = %v, want a shared-ownership refusal", err)
	}
	if err := b.SaveAs(path); err == nil {
		t.Fatal("save-as over the owner's path succeeded")
	}
	if raw, err := os.ReadFile(path); err != nil || string(raw) != "base\n" {
		t.Fatalf("the refused save-as wrote the disk: %q, %v", raw, err)
	}
	if got := a.Active().Buffer.Text(); got != "owner base\n" {
		t.Fatalf("the owner's text changed: %q", got)
	}
	if b.Active() == nil || !strings.Contains(b.Active().Buffer.Text(), "stale") {
		t.Fatalf("the conflict text was lost: %+v", b.Active())
	}
}

// TestSharedBuffersConflictSaveAsNewPathBecomesShared: saving a restored conflict copy
// under a new name makes that file the workspace's canonical shared buffer, so another
// workspace opening it shares it instead of opening an independent copy that could
// overwrite it later.
func TestSharedBuffersConflictSaveAsNewPathBecomesShared(t *testing.T) {
	shared := NewSharedBuffers()
	labels := map[*Editor]string{}
	shared.Label = func(ed *Editor) string { return labels[ed] }
	a, b, _, conflictText := sharedConflictEditor(t, shared)
	labels[a], labels[b] = "A", "B"
	newPath := filepath.Join(t.TempDir(), "kept.txt")
	var conflictIdx = -1
	for i := 0; i < len(b.Tabs()); i++ {
		if d := b.DocAt(i); d != nil && d.Path == "" && d.Modified() {
			conflictIdx = i
			break
		}
	}
	b.ActivateTab(conflictIdx)
	if err := b.SaveAs(newPath); err != nil {
		t.Fatalf("save the conflict copy: %v", err)
	}
	if raw, err := os.ReadFile(newPath); err != nil || string(raw) != conflictText {
		t.Fatalf("new file = %q, %v; want the conflict text", raw, err)
	}
	if got := shared.OwnerLabel(shared.CanonicalPath(newPath)); got != "B" {
		t.Fatalf("new file owner = %q, want B (the save-as must register the path)", got)
	}
	// A third workspace opening the new path sees the shared buffer: it takes ownership
	// as the focused workspace and b keeps a mirror of the same text, not an independent
	// duplicate.
	c := New(Options{Empty: true, Shared: shared})
	labels[c] = "C"
	shared.Focus(c)
	if err := c.OpenFile(newPath); err != nil {
		t.Fatal(err)
	}
	if c.Active().SharedMirror() {
		t.Fatal("the focused workspace did not take ownership of the shared new file")
	}
	if got := c.Active().Buffer.Text(); got != conflictText {
		t.Fatalf("new workspace text = %q, want the saved conflict text", got)
	}
	if !b.Active().SharedMirror() && b.Active().Path == newPath {
		t.Fatal("b kept the canonical document instead of becoming a mirror")
	}
}

// TestSharedBuffersOwnerSaveAsRekeysViews: Save As on a shared owner keeps one buffer
// identity: the old path stops being shared and its untouched disk file can be opened
// independently, while the mirrors follow the renamed buffer to the new path.
func TestSharedBuffersOwnerSaveAsRekeysViews(t *testing.T) {
	shared := NewSharedBuffers()
	labels := map[*Editor]string{}
	shared.Label = func(ed *Editor) string { return labels[ed] }
	dir := t.TempDir()
	path1 := filepath.Join(dir, "one.txt")
	path2 := filepath.Join(dir, "two.txt")
	if err := os.WriteFile(path1, []byte("one\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	a := New(Options{Empty: true, Shared: shared})
	b := New(Options{Empty: true, Shared: shared})
	labels[a], labels[b] = "A", "B"
	shared.Focus(a)
	if err := a.OpenFile(path1); err != nil {
		t.Fatal(err)
	}
	if err := b.OpenFile(path1); err != nil {
		t.Fatal(err)
	}
	shared.Focus(b)
	shared.Focus(a)
	if !b.Active().SharedMirror() {
		t.Fatal("b did not start as the mirror")
	}
	if err := a.SaveAs(path2); err != nil {
		t.Fatalf("owner save-as: %v", err)
	}
	if got := shared.OwnerLabel(shared.CanonicalPath(path1)); got != "" {
		t.Fatalf("the old path is still shared by %q", got)
	}
	if got := shared.OwnerLabel(shared.CanonicalPath(path2)); got != "A" {
		t.Fatalf("the new path owner = %q, want A", got)
	}
	if b.Active() == nil || b.Active().Path != path2 || !b.Active().SharedMirror() {
		t.Fatalf("the mirror view did not follow the renamed buffer: %+v", b.Active())
	}
	if raw, err := os.ReadFile(path1); err != nil || string(raw) != "one\n" {
		t.Fatalf("save-as changed the old file: %q, %v", raw, err)
	}
	// The old path is no longer shared: opening it elsewhere loads the untouched file as
	// an ordinary independent buffer.
	c := New(Options{Empty: true, Shared: shared})
	labels[c] = "C"
	shared.Focus(c)
	if err := c.OpenFile(path1); err != nil {
		t.Fatal(err)
	}
	if c.Active().SharedMirror() {
		t.Fatal("the old path reopened as a mirror")
	}
	for i := 0; i < len(b.Tabs()); i++ {
		if d := b.DocAt(i); d != nil && d.Path == path1 {
			t.Fatal("b still has a view of the old path")
		}
	}
}

// TestSharedBuffersMirrorSaveAsRefused: a read-only mirror is a view of another
// workspace's buffer; Save As must not fork it into an independent file under the
// registry's nose.
func TestSharedBuffersMirrorSaveAsRefused(t *testing.T) {
	shared := NewSharedBuffers()
	a := New(Options{Empty: true, Shared: shared})
	b := New(Options{Empty: true, Shared: shared})
	shared.Focus(a)
	path := writeSharedFile(t, "plain\n")
	if err := a.OpenFile(path); err != nil {
		t.Fatal(err)
	}
	if err := b.OpenFile(path); err != nil {
		t.Fatal(err)
	}
	shared.Focus(b)
	shared.Focus(a)
	if !b.Active().SharedMirror() {
		t.Fatal("b is not a mirror")
	}
	if err := b.SaveAs(filepath.Join(t.TempDir(), "copy.txt")); err == nil || !strings.Contains(err.Error(), "read-only mirror") {
		t.Fatalf("save-as on a mirror = %v, want a refusal", err)
	}
}

// saveAsReservationSetUp returns two workspaces and a, the focused one.
func saveAsReservationSetUp(t *testing.T) (shared *SharedBuffers, a, b *Editor) {
	t.Helper()
	shared = NewSharedBuffers()
	labels := map[*Editor]string{}
	shared.Label = func(ed *Editor) string { return labels[ed] }
	a = New(Options{Empty: true, Shared: shared})
	b = New(Options{Empty: true, Shared: shared})
	labels[a], labels[b] = "A", "B"
	shared.Focus(a)
	a.Do("new", nil)
	a.Active().Buffer.Insert(buffer.Pos{}, "A\n")
	b.Do("new", nil)
	b.Active().Buffer.Insert(buffer.Pos{}, "B\n")
	return shared, a, b
}

// TestSharedBuffersSaveAsReservationBlocksOtherWorkspace: a Save As plan reserves its
// target before the asynchronous write starts; another workspace cannot open it (no
// independent copy that could overwrite the pending write) or plan its own Save As to it,
// the same editor cannot plan twice, and the reservation is gone after ApplySavePlan.
func TestSharedBuffersSaveAsReservationBlocksOtherWorkspace(t *testing.T) {
	_, a, b := saveAsReservationSetUp(t)
	target := filepath.Join(t.TempDir(), "new.txt")
	plan, err := a.PlanSaveAs(target, false)
	if err != nil {
		t.Fatalf("PlanSaveAs: %v", err)
	}
	if err := b.OpenFile(target); err == nil || !strings.Contains(err.Error(), "being saved by A") {
		t.Fatalf("opening a reserved target = %v, want the reservation refusal", err)
	}
	if len(b.Tabs()) != 1 || b.Active().Path != "" {
		t.Fatalf("the refused open changed the tabs: %+v", b.Tabs())
	}
	if _, err := b.PlanSaveAs(target, false); err == nil || !strings.Contains(err.Error(), "being saved by A") {
		t.Fatalf("second Save As plan = %v, want the reservation refusal", err)
	}
	if _, err := a.PlanSaveAs(target, false); err == nil || !strings.Contains(err.Error(), "already in progress") {
		t.Fatalf("same-editor second plan = %v, want the in-progress refusal", err)
	}
	if err := a.ApplySavePlan(plan, plan.Run()); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if raw, err := os.ReadFile(target); err != nil || string(raw) == "" {
		t.Fatalf("target after apply = %q, %v", raw, err)
	}
	// The target is now owned by a: the refusal changed from "being saved" to ownership.
	if _, err := b.PlanSaveAs(target, false); err == nil || !strings.Contains(err.Error(), "open in A") {
		t.Fatalf("plan after apply = %v, want the ownership refusal", err)
	}
}

// TestSharedBuffersSaveAsReservationReleasedOnFailure: a failed write (an existing file
// without force) releases the reservation in ApplySavePlan, so another workspace can save
// that path afterwards.
func TestSharedBuffersSaveAsReservationReleasedOnFailure(t *testing.T) {
	_, a, b := saveAsReservationSetUp(t)
	target := filepath.Join(t.TempDir(), "exists.txt")
	if err := os.WriteFile(target, []byte("theirs\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	plan, err := a.PlanSaveAs(target, false)
	if err != nil {
		t.Fatalf("PlanSaveAs: %v", err)
	}
	results := plan.Run()
	if err := a.ApplySavePlan(plan, results); err == nil {
		t.Fatal("the write over an existing file without force succeeded")
	}
	if raw, err := os.ReadFile(target); err != nil || string(raw) != "theirs\n" {
		t.Fatalf("the failed write changed the file: %q, %v", raw, err)
	}
	next, err := b.PlanSaveAs(target, true)
	if err != nil || next.reservation == nil {
		t.Fatalf("the reservation leaked after a failed apply: %v", err)
	}
	next.Release()
}

// TestSharedBuffersSaveAsReservationReleasedOnCancel: a cancelled write releases the
// reservation in ApplySavePlan (the caller still applies the cancelled results).
func TestSharedBuffersSaveAsReservationReleasedOnCancel(t *testing.T) {
	_, a, b := saveAsReservationSetUp(t)
	target := filepath.Join(t.TempDir(), "cancelled.txt")
	plan, err := a.PlanSaveAs(target, false)
	if err != nil {
		t.Fatalf("PlanSaveAs: %v", err)
	}
	// A caller that cancels before the write starts has no results to apply; applying
	// the plan anyway is the same release path (ApplySavePlan defers it).
	if err := a.ApplySavePlan(plan, nil); err != nil {
		t.Fatalf("apply of a cancelled plan: %v", err)
	}
	if _, err := b.PlanSaveAs(target, false); err != nil {
		t.Fatalf("the reservation leaked after a cancelled apply: %v", err)
	}
}

// TestSharedBuffersSaveAsReservationExplicitRelease: a caller that drops a plan without
// applying it can release the claim explicitly; then another workspace may plan it.
func TestSharedBuffersSaveAsReservationExplicitRelease(t *testing.T) {
	_, a, b := saveAsReservationSetUp(t)
	target := filepath.Join(t.TempDir(), "dropped.txt")
	plan, err := a.PlanSaveAs(target, false)
	if err != nil {
		t.Fatalf("PlanSaveAs: %v", err)
	}
	plan.Release()
	next, err := b.PlanSaveAs(target, false)
	if err != nil {
		t.Fatalf("plan after Release: %v", err)
	}
	next.Release()
}

// TestSharedBuffersSaveAsReservationDroppedEditor: closing/evicting the workspace that
// holds a plan releases its reservations, so the path cannot stay claimed by a gone editor.
func TestSharedBuffersSaveAsReservationDroppedEditor(t *testing.T) {
	shared, a, b := saveAsReservationSetUp(t)
	target := filepath.Join(t.TempDir(), "evicted.txt")
	plan, err := a.PlanSaveAs(target, false)
	if err != nil {
		t.Fatalf("PlanSaveAs: %v", err)
	}
	_ = plan // never applied: the workspace goes away with the plan in flight
	shared.DropEditor(a)
	next, err := b.PlanSaveAs(target, false)
	if err != nil {
		t.Fatalf("plan after DropEditor: %v", err)
	}
	next.Release()
}

// TestSharedBuffersSaveAsReservationConcurrentSingleWinner: concurrent plans for one path
// (the controllable race window) produce exactly one claim; the losers get the
// in-progress refusal and nothing leaks after the winner releases.
func TestSharedBuffersSaveAsReservationConcurrentSingleWinner(t *testing.T) {
	shared, a, _ := saveAsReservationSetUp(t)
	target := filepath.Join(t.TempDir(), "race.txt")
	doc := a.Active()
	const n = 16
	start := make(chan struct{})
	var wg sync.WaitGroup
	results := make(chan *sharedSaveReservation, n)
	errsCh := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			r, err := shared.ReserveSaveAs(a, doc, target)
			if err != nil {
				errsCh <- err
				return
			}
			results <- r
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	close(errsCh)
	var winners []*sharedSaveReservation
	for r := range results {
		winners = append(winners, r)
	}
	if len(winners) != 1 {
		t.Fatalf("concurrent reservations produced %d winners, want exactly one", len(winners))
	}
	for err := range errsCh {
		if !strings.Contains(err.Error(), "already in progress") {
			t.Fatalf("loser error = %v, want the in-progress refusal", err)
		}
	}
	winners[0].Release()
	if r, err := shared.ReserveSaveAs(a, doc, target); err != nil {
		t.Fatalf("reserve after release: %v", err)
	} else {
		r.Release()
	}
}

// TestSharedBuffersSaveAsAsyncWindowNoIndependentCopy: the exact plan→write→apply window:
// while the write command runs, another workspace's open is refused; after apply the file
// is the plan's text and no independent copy has been created to overwrite it.
func TestSharedBuffersSaveAsAsyncWindowNoIndependentCopy(t *testing.T) {
	_, a, b := saveAsReservationSetUp(t)
	target := filepath.Join(t.TempDir(), "async.txt")
	plan, err := a.PlanSaveAs(target, false)
	if err != nil {
		t.Fatalf("PlanSaveAs: %v", err)
	}
	written := make(chan []SaveResult, 1)
	go func() { written <- plan.Run() }()
	// The command is writing off the UI goroutine. A competing open during this window
	// must not load an independent copy of the file.
	if err := b.OpenFile(target); err == nil || !strings.Contains(err.Error(), "being saved by A") {
		t.Fatalf("competing open during the write = %v, want the reservation refusal", err)
	}
	results := <-written
	if err := a.ApplySavePlan(plan, results); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if len(b.Tabs()) != 1 || b.Active().Path != "" {
		t.Fatalf("the competing open changed the tabs: %+v", b.Tabs())
	}
	if raw, err := os.ReadFile(target); err != nil || string(raw) != "A\n" {
		t.Fatalf("target = %q, %v, want the planned text", raw, err)
	}
}
