package editor

import (
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"sync"

	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/document"
)

// Cross-workspace shared buffers (daemon #19b bounded slice). A daemon's materialized
// workspaces share one SharedBuffers registry. A file is owned by one workspace at a
// time; opening it in another workspace gives that workspace a read-only mirror and,
// when it is the focused workspace, asks for ownership. Ownership moves the canonical
// Document (text, undo, unsaved state) to the new editor and leaves a mirror in the old
// one. Owner edits reach mirrors through an explicit change path with the document
// stale-version guard. Large files, virtual buffers and remote workspaces stay
// independent (document.NewMirrorDocument refuses them, so OpenFile falls back to the
// ordinary independent open).

// SharedBuffers is the per-daemon registry of cross-workspace shared buffers. Nil in a
// direct launch; every method is safe for concurrent use, but editor mutation happens
// only on the daemon's UI goroutine (OpenFile, workspace switch, workspace close).
type SharedBuffers struct {
	mu      sync.Mutex
	entries map[string]*sharedEntry
	// active is the focused workspace editor; the one allowed to take ownership.
	active *Editor
	// Label names an editor's workspace for status text; nil uses "another workspace".
	Label func(*Editor) string
	// Deny is a test hook that can refuse an ownership transfer; nil grants it.
	Deny func(owner, requester *Editor) error
	// pending maps an editor to documents opened through OpenLoaded for a path another
	// workspace owns. Startup restore opens the file first and applies its stash text
	// after OpenLoaded, so adoption is deferred to the editor's focus: only then is the
	// restored unsaved text final and a real conflict can be told from a plain mirror.
	pending map[*Editor][]*document.Document
	// reserved holds the in-flight Save As claims, by canonical target path. A Save As
	// plan acquires its reservation on the UI goroutine before the async write starts and
	// releases it on apply; other workspaces are refused that target meanwhile, so the
	// plan's ownership check cannot be beaten by a write that lands later.
	reserved map[string]*sharedSaveReservation
}

// sharedSaveReservation is the revocable exclusive claim one Save As holds on its target
// canonical path until the plan is applied (successfully, with an error, or cancelled).
// It is unexported: callers carry it inside the SavePlan and release it through
// SavePlan.Release (ApplySavePlan does that automatically).
type sharedSaveReservation struct {
	shared    *SharedBuffers
	editor    *Editor
	doc       *document.Document
	canonical string
}

// Release ends the reservation. It is idempotent and safe to call from the UI goroutine
// that owns the editor; ApplySavePlan calls it for every plan, and a caller that drops a
// plan without applying it must call SavePlan.Release itself.
func (r *sharedSaveReservation) Release() {
	if r == nil || r.shared == nil {
		return
	}
	r.shared.releaseSaveReservation(r)
}

type sharedEntry struct {
	canonical string
	owner     *Editor
	// doc is the canonical document, open in owner (the only editor with write access).
	doc *document.Document
	// mirrors maps each other editor to its read-only snapshot.
	mirrors map[*Editor]*document.Document
	// watchRemove/watchKey are the owner's change listener and TrackChanges key.
	watchRemove func()
	watchKey    string
	watchEditor *Editor
}

// NewSharedBuffers returns an empty cross-workspace registry.
func NewSharedBuffers() *SharedBuffers {
	return &SharedBuffers{
		entries:  make(map[string]*sharedEntry),
		pending:  make(map[*Editor][]*document.Document),
		reserved: make(map[string]*sharedSaveReservation),
	}
}

// sharedCanonical canonicalizes an open path for the registry: absolute and, when it
// resolves, symlink-free (the same identity the daemon's open-path routing uses).
func sharedCanonical(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		return resolved
	}
	return abs
}

func (s *SharedBuffers) label(e *Editor) string {
	if s.Label != nil {
		if name := s.Label(e); name != "" {
			return name
		}
	}
	return "another workspace"
}

// Open is the editor's OpenFile hook. It reports handled=false for a path no other
// workspace owns (the normal load then registers it); handled=true when it opened a
// mirror, possibly taking ownership. err is the conflict/refusal the caller shows.
func (s *SharedBuffers) Open(e *Editor, path string) (handled bool, err error) {
	if s == nil || e == nil || path == "" {
		return false, nil
	}
	canonical := sharedCanonical(path)
	s.mu.Lock()
	defer s.mu.Unlock()
	if r := s.reserved[canonical]; r != nil {
		return true, fmt.Errorf("open: %s is being saved by %s; try again when it finishes",
			filepath.Base(canonical), s.label(r.editor))
	}
	entry := s.entries[canonical]
	if entry == nil || entry.owner == e {
		return false, nil
	}
	if entry.doc == nil || entry.owner == nil {
		return false, nil
	}
	if s.active != nil && s.active != e {
		// A workspace that is being materialized or restored (not the focused one) may
		// have session stash text applied to the document AFTER OpenFile returns. Let it
		// load normally; OpenLoaded records the deferred adoption and Focus resolves it
		// when the workspace is shown, with the restored unsaved text final.
		return false, nil
	}
	mirror, err := document.NewMirrorDocument(entry.doc)
	if err != nil {
		// Not shareable in this slice (large/virtual/over the size bound): keep the
		// ordinary independent open rather than failing the file.
		return false, nil
	}
	fromLabel := s.label(entry.owner)
	mirror.SetSharedMirrorStatus("Mirror of " + fromLabel + ": read-only until focused")
	s.openMirrorLocked(e, mirror)
	entry.mirrors[e] = mirror
	if s.active == nil {
		s.active = e
	}
	if s.active == e {
		if terr := s.transferLocked(entry, entry.owner, e); terr != nil {
			mirror.SetSharedMirrorStatus("Cross-workspace share conflict: " + terr.Error() + "; the mirror stays read-only")
			e.setStatus(mirror.Status)
			return true, nil
		}
		e.setStatus("Opened " + filepath.Base(canonical) + " shared with " + fromLabel)
		return true, nil
	}
	e.setStatus(mirror.Status)
	return true, nil
}

// openMirrorLocked adds the mirror as the active tab, replacing an untouched startup
// placeholder. e.docs ownership rules match OpenLoaded.
func (s *SharedBuffers) openMirrorLocked(e *Editor, mirror *document.Document) {
	e.pushJump()
	if e.pristineUntitled() {
		e.releaseDoc(e.docs[0])
		e.setDoc(0, mirror)
		e.switchTo(0)
		return
	}
	e.addDoc(mirror)
	e.switchTo(len(e.docs) - 1)
}

// AdoptLoaded is the OpenLoaded hook for startup restore and other pre-loaded
// documents. With no shared entry the caller keeps d and the ordinary registration
// follows (handled false). When another workspace owns the canonical path, an unmodified
// restored copy is replaced by a read-only mirror of the owner (handled true, open is the
// mirror) and a restored UNSAVED copy whose text differs is kept as an extra untitled
// conflict tab instead of being discarded or allowed to overwrite the owner later: the
// original document is renamed away from the file, labelled and returned as conflict.
// When this workspace already owns the entry, its loaded copy is replaced by the
// canonical document under the same rule.
func (s *SharedBuffers) AdoptLoaded(e *Editor, d *document.Document) (open, conflict *document.Document, handled bool) {
	if s == nil || e == nil || d == nil || d.Path == "" || d.SharedMirror() || d.Large() || d.Virtual() || d.Binary() {
		return d, nil, false
	}
	canonical := sharedCanonical(d.Path)
	s.mu.Lock()
	defer s.mu.Unlock()
	entry := s.entries[canonical]
	if entry == nil || entry.doc == nil || entry.owner == nil {
		return d, nil, false
	}
	if entry.owner == e {
		if entry.doc == d {
			return d, nil, true
		}
		conflict = s.conflictCopyLocked(e, d, entry)
		e.replaceRecent(d, entry.doc)
		return entry.doc, conflict, true
	}
	// Defer the adoption to the editor's focus: the restore pipeline applies the stash
	// text after OpenLoaded, so a decision now could miss (or wrongly keep) the restored
	// unsaved text. The loaded document stays a normal tab until Focus adopts it.
	s.pending[e] = append(s.pending[e], d)
	return d, nil, false
}

// conflictCopyLocked turns a restored unsaved document whose text differs from the
// owner's into an untitled conflict buffer. It returns nil when the restored text is not
// unsaved or already matches the owner, and never changes the owner. s.mu is held.
func (s *SharedBuffers) conflictCopyLocked(e *Editor, d *document.Document, entry *sharedEntry) *document.Document {
	if !d.Modified() || d.Large() || d.Virtual() || entry.doc == nil {
		return nil
	}
	restored, ok := sharedDocumentText(d)
	if !ok {
		return nil
	}
	ownerText, ok := sharedDocumentText(entry.doc)
	if !ok || restored == ownerText {
		return nil
	}
	name := filepath.Base(d.Path)
	d.Rename("")
	d.SetLabel("[shared conflict] " + name)
	d.SetPreview(false)
	d.Status = "Restored unsaved copy of " + name + " from this workspace's session; " +
		s.label(entry.owner) + " owns the file. Save As to keep this copy."
	return d
}

// sharedDocumentText reads a document's text for the share/conflict decisions, bounded.
func sharedDocumentText(d *document.Document) (string, bool) {
	if d == nil || d.Buffer == nil {
		return "", false
	}
	text, err := d.Buffer.TextChecked()
	if err != nil || len(text) > document.MaxSharedDocumentBytes {
		return "", false
	}
	return text, true
}

// Register is the OpenLoaded hook: the newly loaded document becomes the owner of its
// canonical path when no other workspace owns it. A path already owned elsewhere is
// left to the ordinary open (sharing starts on the OpenFile path).
func (s *SharedBuffers) Register(e *Editor, d *document.Document) {
	if s == nil || e == nil || d == nil || d.Path == "" || d.SharedMirror() || d.Large() || d.Virtual() || d.Binary() {
		return
	}
	canonical := sharedCanonical(d.Path)
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.entries[canonical]; ok {
		return
	}
	if r := s.reserved[canonical]; r != nil {
		// A Save As in flight owns the path until it applies; do not register a second
		// owner for it (the Open path already refuses the file, this guards the
		// OpenLoaded/plugin path).
		return
	}
	entry := &sharedEntry{canonical: canonical, owner: e, doc: d, mirrors: map[*Editor]*document.Document{}}
	s.entries[canonical] = entry
	s.watchLocked(entry, e)
}

// watchLocked installs the owner editor's change listener for entry. It is called with
// s.mu held; AddChangeListener/TrackChanges touch only the editor.
func (s *SharedBuffers) watchLocked(entry *sharedEntry, owner *Editor) {
	entry.watchKey = "workspace-shared:" + entry.canonical
	entry.watchEditor = owner
	entry.watchRemove = owner.AddChangeListener(entry.watchKey, func(_ DocID, c buffer.Change) {
		s.onOwnerChange(entry, c)
	})
	owner.TrackChanges(entry.watchKey, true)
}

// unwatchLocked removes the owner listener.
func (s *SharedBuffers) unwatchLocked(entry *sharedEntry) {
	if entry.watchEditor != nil && entry.watchRemove != nil {
		entry.watchRemove()
		entry.watchEditor.TrackChanges(entry.watchKey, false)
	}
	entry.watchRemove, entry.watchEditor, entry.watchKey = nil, nil, ""
}

// onOwnerChange propagates an owner change to every mirror through the document
// stale-version guard; a mirror that cannot apply it reloads the whole text.
func (s *SharedBuffers) onOwnerChange(entry *sharedEntry, c buffer.Change) {
	if s == nil || entry == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	owner := entry.owner
	if owner == nil || entry.doc == nil {
		return
	}
	for _, mirror := range entry.mirrors {
		if err := mirror.ApplyOwnerChange(entry.doc, c); err != nil {
			if serr := mirror.SyncMirrorFrom(entry.doc); serr != nil {
				mirror.SetSharedMirrorStatus("Cross-workspace mirror: " + serr.Error())
			}
		}
	}
}

// checkTransferLocked is the part of transferLocked that can refuse, and changes
// nothing: it returns the tab slots of the move, the mirror that takes the canonical
// document and the snapshot the old owner keeps.
func (s *SharedBuffers) checkTransferLocked(entry *sharedEntry, from, to *Editor) (fromIdx, toIdx int, toMirror, snapshot *document.Document, err error) {
	if entry.owner != from || entry.doc == nil || to == nil || from == nil {
		return 0, 0, nil, nil, errors.New("the ownership changed while taking it")
	}
	if s.Deny != nil {
		if err := s.Deny(from, to); err != nil {
			return 0, 0, nil, nil, err
		}
	}
	toMirror = entry.mirrors[to]
	if toMirror == nil {
		return 0, 0, nil, nil, errors.New("the mirror is no longer open")
	}
	fromIdx = slices.Index(from.docs, entry.doc)
	if fromIdx < 0 {
		return 0, 0, nil, nil, errors.New("the owner's tab is no longer open")
	}
	toIdx = slices.Index(to.docs, toMirror)
	if toIdx < 0 {
		return 0, 0, nil, nil, errors.New("the mirror's tab is no longer open")
	}
	// The mirror text must still be the owner's version before the move.
	snapshot, err = document.NewMirrorDocument(entry.doc)
	if err != nil {
		return 0, 0, nil, nil, err
	}
	return fromIdx, toIdx, toMirror, snapshot, nil
}

// transferLocked moves the canonical document from from to to. The requester must have a
// mirror of entry (to's slot); from gets a fresh read-only snapshot in the same tab, to
// adopts the canonical Document with its undo and unsaved state. The stale-version guard
// is checked before the move: the owner's recorded version must still be the mirror's.
func (s *SharedBuffers) transferLocked(entry *sharedEntry, from, to *Editor) error {
	fromIdx, toIdx, toMirror, snapshot, err := s.checkTransferLocked(entry, from, to)
	if err != nil {
		return err
	}
	s.unwatchLocked(entry)
	canonical := entry.doc
	canonical.Seam().Obs = nil
	canonical.Seam().ID = 0
	canonical.Buffer.SetObserver(nil)
	snapshot.SetSharedMirrorStatus("Mirror of " + s.label(to) + ": read-only until focused")
	from.docs[fromIdx] = snapshot
	from.adopt(snapshot)
	from.replaceRecent(canonical, snapshot)
	to.docs[toIdx] = canonical
	canonical.MakeSharedOwner()
	to.adopt(canonical)
	to.replaceRecent(toMirror, canonical)
	entry.owner = to
	entry.doc = canonical
	entry.mirrors[from] = snapshot
	delete(entry.mirrors, to)
	s.watchLocked(entry, to)
	return nil
}

// adoptPendingLocked resolves the documents OpenLoaded deferred for e. It runs on the
// editor's focus, after startup restore has applied the stash text. A path whose owner
// disappeared becomes e's owner; a path still owned elsewhere becomes a read-only mirror,
// and a modified restored copy whose text differs from the live owner is kept as an
// untitled conflict tab instead of being discarded or overwriting the owner later.
func (s *SharedBuffers) adoptPendingLocked(e *Editor) {
	docs := s.pending[e]
	if len(docs) == 0 {
		return
	}
	delete(s.pending, e)
	for _, d := range docs {
		idx := slices.Index(e.docs, d)
		if idx < 0 || d.Path == "" {
			continue
		}
		canonical := sharedCanonical(d.Path)
		entry := s.entries[canonical]
		if entry == nil || entry.doc == nil || entry.owner == nil {
			// The owner went away while this workspace was dormant: the restored
			// document becomes the owner, with its restored unsaved text.
			entry = &sharedEntry{canonical: canonical, owner: e, doc: d, mirrors: map[*Editor]*document.Document{}}
			s.entries[canonical] = entry
			s.watchLocked(entry, e)
			d.MakeSharedOwner()
			continue
		}
		if entry.owner == e {
			if entry.doc != d {
				// A duplicate loaded copy of a path this editor owns: keep the canonical
				// one and keep a conflicting unsaved copy under the conflict rule.
				conflict := s.conflictCopyLocked(e, d, entry)
				e.docs[idx] = entry.doc
				e.replaceRecent(d, entry.doc)
				entry.doc.MakeSharedOwner()
				if conflict == nil {
					e.releaseDoc(d)
				} else {
					e.addDoc(conflict)
				}
			}
			continue
		}
		mirror, err := document.NewMirrorDocument(entry.doc)
		if err != nil {
			// Large/virtual/over-bound: leave the restored document independent.
			continue
		}
		conflict := s.conflictCopyLocked(e, d, entry)
		mirror.SetSharedMirrorStatus("Mirror of " + s.label(entry.owner) + ": read-only until focused")
		e.docs[idx] = mirror
		e.adopt(mirror)
		e.replaceRecent(d, mirror)
		entry.mirrors[e] = mirror
		if conflict == nil {
			e.releaseDoc(d)
		} else {
			e.addDoc(conflict)
		}
	}
}

// Focus makes e the focused workspace: it requests ownership of every entry it mirrors,
// and refreshes its owned entries' mirrors (a save updates the saved state without a
// buffer change, so a focus syncs it). A refused transfer leaves a conflict in the
// mirror's status; the mirror stays read-only.
func (s *SharedBuffers) Focus(e *Editor) {
	if s == nil || e == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.active = e
	s.adoptPendingLocked(e)
	for _, entry := range s.entries {
		if mirror, ok := entry.mirrors[e]; ok && entry.owner != e && entry.doc != nil {
			if err := s.transferLocked(entry, entry.owner, e); err != nil {
				mirror.SetSharedMirrorStatus("Cross-workspace share conflict: " + err.Error() + "; the mirror stays read-only")
			}
			continue
		}
		if entry.owner == e && entry.doc != nil {
			for _, mirror := range entry.mirrors {
				if err := mirror.SyncMirrorFrom(entry.doc); err != nil {
					mirror.SetSharedMirrorStatus("Cross-workspace mirror: " + err.Error())
				}
			}
		}
	}
}

// closeEntryLocked finds the shared entry of d in e, or nil.
func (s *SharedBuffers) closeEntryLocked(e *Editor, d *document.Document) *sharedEntry {
	if s == nil || e == nil || d == nil || d.Path == "" {
		return nil
	}
	canonical := sharedCanonical(d.Path)
	entry := s.entries[canonical]
	if entry == nil {
		return nil
	}
	if entry.mirrors[e] == d || (entry.owner == e && entry.doc == d) {
		return entry
	}
	return nil
}

// ClosePrompt reports whether closing d in e must ask about unsaved text. A mirror
// never does (the owner keeps the text), and neither does an owner whose buffer a
// mirror in another workspace keeps alive: the close promotes that mirror instead of
// dropping the text. The final view of an unshared or sole owner keeps the ordinary
// modified prompt.
func (s *SharedBuffers) ClosePrompt(e *Editor, d *document.Document) bool {
	if s == nil {
		return true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entry := s.closeEntryLocked(e, d)
	if entry == nil {
		return true
	}
	if entry.mirrors[e] == d {
		return false
	}
	return len(entry.mirrors) == 0
}

// BeforeClose detaches d from its shared entry before its tab is removed. A mirror is
// dropped from the registry (its text belongs to the owner). An owner with a mirror
// promotes the canonical document to that mirror editor, so a tab close keeps the text,
// undo history and unsaved state alive in the other workspace; the closing editor gets a
// throw-away snapshot in the same tab slot, which the close then releases. A sole owner
// simply leaves the registry. The error is a refused ownership transfer: the caller must
// keep the tab open then, never close over unsaved text.
func (s *SharedBuffers) BeforeClose(e *Editor, d *document.Document) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entry := s.closeEntryLocked(e, d)
	if entry == nil {
		return nil
	}
	if entry.mirrors[e] == d {
		delete(entry.mirrors, e)
		return nil
	}
	var target *Editor
	for ed := range entry.mirrors {
		target = ed
		break
	}
	if target == nil {
		s.unwatchLocked(entry)
		delete(s.entries, entry.canonical)
		return nil
	}
	if err := s.transferLocked(entry, e, target); err != nil {
		return err
	}
	// The closing editor's slot now holds a snapshot; it is released with the tab.
	delete(entry.mirrors, e)
	return nil
}

// CheckClose reports the error BeforeClose(e, d) would return, and changes nothing, so a
// caller closing several tabs can refuse the whole close before it detaches the first.
func (s *SharedBuffers) CheckClose(e *Editor, d *document.Document) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entry := s.closeEntryLocked(e, d)
	if entry == nil || entry.mirrors[e] == d {
		return nil
	}
	for target := range entry.mirrors {
		_, _, _, _, err := s.checkTransferLocked(entry, e, target)
		return err
	}
	return nil
}

// DropEditor removes an editor that is being closed or evicted. When it owned a buffer,
// ownership moves to a mirror in another workspace, so closing one workspace keeps the
// file and its unsaved state in the other; with no mirror the entry is dropped.
func (s *SharedBuffers) DropEditor(e *Editor) {
	if s == nil || e == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.pending, e)
	for canonical, r := range s.reserved {
		if r.editor == e {
			delete(s.reserved, canonical)
		}
	}
	for canonical, entry := range s.entries {
		delete(entry.mirrors, e)
		if entry.owner != e {
			continue
		}
		var target *Editor
		for ed := range entry.mirrors {
			target = ed
			break
		}
		if target == nil {
			s.unwatchLocked(entry)
			delete(s.entries, canonical)
			continue
		}
		if err := s.transferLocked(entry, e, target); err != nil {
			// The close cannot be undone here; keep the canonical text in the target by
			// replacing its mirror with a full sync, then drop the entry.
			if mirror := entry.mirrors[target]; mirror != nil && entry.doc != nil {
				_ = mirror.SyncMirrorFrom(entry.doc)
			}
			s.unwatchLocked(entry)
			delete(s.entries, canonical)
			continue
		}
		// The closing editor now holds a snapshot; it is being discarded with the
		// runtime, so the registry does not need its mirror.
		delete(entry.mirrors, e)
	}
}

// saveAsRefusalLocked returns why e may not Save As d to canonical, or nil. s.mu is held.
func (s *SharedBuffers) saveAsRefusalLocked(e *Editor, d *document.Document, canonical string) error {
	if r := s.reserved[canonical]; r != nil {
		if r.editor == e {
			return fmt.Errorf("save as: a save to %s is already in progress", filepath.Base(canonical))
		}
		return fmt.Errorf("save as: %s is being saved by %s; try again when it finishes",
			filepath.Base(canonical), s.label(r.editor))
	}
	entry := s.entries[canonical]
	if entry == nil {
		return nil
	}
	if entry.owner == e && entry.doc == d {
		return nil
	}
	return fmt.Errorf("save as: %s is open in %s; save under another name or close it there first",
		filepath.Base(canonical), s.label(entry.owner))
}

// CheckSaveAs refuses a Save As target that another workspace owns (or that this
// workspace owns with a different document) or that another Save As is currently writing.
// A save-as must never write a file that is live as another workspace's canonical
// document: the write would silently diverge from the owner's in-memory text, and a later
// owner save would overwrite the saved copy. A read-only mirror cannot be saved-as either:
// it is a view of another workspace's buffer, and a copy would fork it without the
// registry knowing.
func (s *SharedBuffers) CheckSaveAs(e *Editor, d *document.Document, path string) error {
	if s == nil || e == nil || d == nil || path == "" {
		return nil
	}
	if d.SharedMirror() {
		return errors.New("save as: the tab is a read-only mirror of another workspace's buffer; focus its workspace to take ownership first")
	}
	canonical := sharedCanonical(path)
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveAsRefusalLocked(e, d, canonical)
}

// ReserveSaveAs claims the Save As target for e before its asynchronous write starts. The
// claim is checked against the registry's owner and against another in-flight reservation;
// it is revoked by the returned reservation's Release (SavePlan.Release), which
// ApplySavePlan does automatically on success, failure and cancellation. A caller without
// a shared registry (a direct launch) gets a nil reservation.
func (s *SharedBuffers) ReserveSaveAs(e *Editor, d *document.Document, path string) (*sharedSaveReservation, error) {
	if s == nil || e == nil || d == nil || path == "" {
		return nil, nil
	}
	if d.SharedMirror() {
		return nil, errors.New("save as: the tab is a read-only mirror of another workspace's buffer; focus its workspace to take ownership first")
	}
	canonical := sharedCanonical(path)
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.saveAsRefusalLocked(e, d, canonical); err != nil {
		return nil, err
	}
	if s.reserved == nil {
		s.reserved = make(map[string]*sharedSaveReservation)
	}
	r := &sharedSaveReservation{shared: s, editor: e, doc: d, canonical: canonical}
	s.reserved[canonical] = r
	return r, nil
}

// releaseSaveReservation drops r if it is still the current claim for its path.
func (s *SharedBuffers) releaseSaveReservation(r *sharedSaveReservation) {
	if s == nil || r == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.reserved[r.canonical] == r {
		delete(s.reserved, r.canonical)
	}
}

// AfterSaveAs updates the registry after a document actually took newPath (Save As
// renamed it). A shared OWNER keeps its buffer identity: the entry is rekeyed to the new
// path, its mirrors follow (their view shows the renamed buffer) and the old path stops
// being shared, so opening it later loads the untouched old file independently. A
// conflict/untitled copy that becomes a real file is registered as the owner of its new
// path, so another workspace opening it shares instead of opening an independent copy
// that could overwrite it. oldPath is the document's path before the save ("" for an
// untitled buffer); the caller stays within the registry's UI goroutine.
func (s *SharedBuffers) AfterSaveAs(e *Editor, d *document.Document, oldPath, newPath string) {
	if s == nil || e == nil || d == nil || newPath == "" || d.SharedMirror() {
		return
	}
	newCanonical := sharedCanonical(newPath)
	oldCanonical := ""
	if oldPath != "" {
		oldCanonical = sharedCanonical(oldPath)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if oldCanonical != "" && oldCanonical != newCanonical {
		if entry := s.entries[oldCanonical]; entry != nil && entry.owner == e && entry.doc == d {
			// The buffer was renamed: keep one canonical document and one entry, now
			// keyed by the new path. The views in other workspaces follow it, matching
			// Fresh's "view state is per split, buffer identity is shared".
			delete(s.entries, oldCanonical)
			s.unwatchLocked(entry)
			entry.canonical = newCanonical
			for _, mirror := range entry.mirrors {
				mirror.Rename(d.Path)
				if err := mirror.SyncMirrorFrom(d); err != nil {
					mirror.SetSharedMirrorStatus("Cross-workspace mirror: " + err.Error())
				} else {
					mirror.SetSharedMirrorStatus("Mirror of " + s.label(e) + ": read-only until focused")
				}
			}
			s.entries[newCanonical] = entry
			s.watchLocked(entry, e)
			return
		}
	}
	if _, exists := s.entries[newCanonical]; exists {
		// Another workspace claimed the path between the plan and the apply (the write
		// already happened): do not replace its entry; keep this document independent
		// and say so instead of silently sharing two different texts.
		if entry := s.entries[newCanonical]; entry != nil && (entry.owner != e || entry.doc != d) {
			d.Status = "Saved, but " + filepath.Base(newCanonical) + " is open in " + s.label(entry.owner) + "; the copy stays independent"
		}
		return
	}
	entry := &sharedEntry{canonical: newCanonical, owner: e, doc: d, mirrors: map[*Editor]*document.Document{}}
	s.entries[newCanonical] = entry
	s.watchLocked(entry, e)
}

// CanonicalPath returns the registry key of path (exposed for the app's open-path
// routing to compare with its own canonical form).
func (s *SharedBuffers) CanonicalPath(path string) string { return sharedCanonical(path) }

// Has reports whether a canonical path is shared.
func (s *SharedBuffers) Has(canonical string) bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.entries[canonical]
	return ok
}

// OwnerLabel names the workspace that owns canonical, for notices.
func (s *SharedBuffers) OwnerLabel(canonical string) string {
	if s == nil {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if entry := s.entries[canonical]; entry != nil {
		return s.label(entry.owner)
	}
	return ""
}

// String describes the registry for tests/status.
func (s *SharedBuffers) String() string {
	if s == nil {
		return "shared buffers: off"
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return fmt.Sprintf("shared buffers: %d entries", len(s.entries))
}
