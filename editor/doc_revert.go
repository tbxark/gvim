package editor

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/tbxark/gvim/buffer"
)

// ErrStaleRevert is returned by ApplyRevert when the text changed after PlanRevert. The file
// that RunRevert read is then not the one the document was planned against, so the reload is
// refused and the edit is kept.
var ErrStaleRevert = errors.New("text changed since the revert was planned")

// RevertPlan is a reload of one document from its file. It holds no file state, so making it
// reads nothing. PlanRevert makes a plan for the active document and records its text, which
// ApplyRevert checks is unchanged. A plan made from the path alone, for a clean background
// tab, records no text: ApplyRevert accepts it only while the tab is still clean, so an edit
// made after the plan is kept.
type RevertPlan struct {
	// Path is the file the document reloads from. ApplyRevert finds the document by it, so
	// a tab that is not the active one can be reverted.
	Path string
	// Force is the force argument PlanRevert was given.
	Force bool
	// Modified is true when the document has unsaved edits, which the reload replaces.
	Modified bool
	// Auto marks a reload that the editor started because the file changed on disk. Its
	// status says "Reloaded", not "Reverted", which reads as if the user discarded changes.
	Auto bool

	planned bool        // PlanRevert made the plan, so mark holds the document's content
	eol     string      // the line ending of the document, which the read keeps when it can
	mark    contentMark // the content at the plan, which ApplyRevert checks is unchanged
	// spec is how the read opens a large file. It is set by a plan the editor made, and
	// nil for a plan made from a path alone, which reads the file whole as a small file
	// is read and cannot revert a large document.
	spec *loadSpec
}

// RevertResult is the file that RunRevert read for a RevertPlan. ApplyRevert puts it in place.
type RevertResult struct {
	// Text is the text of the file, decoded as a file is opened. It is empty when Err is set.
	Text string
	// Err is the error of the read. A file that no longer exists wraps fs.ErrNotExist.
	Err error

	// fresh is the document the read made, which records the file on disk. For a large
	// file it is the document that ApplyRevert puts in place of the old one: the text
	// stays in the file.
	fresh *Document
}

// PlanRevert prepares the reload of the active document from its file. It reads no file and
// changes no state, so the UI may call it on its update loop. A missing tab, a read-only
// document and an untitled one are refused with ErrNoDocument and ErrNoPath. A read-only
// document is planned like any other, because a revert changes the buffer from the file and
// not the file; the revert action itself still refuses it. A modified document is refused
// with ErrUnsaved unless force is set, and the plan it returns then has Modified set. The
// refusals are also the tab's status.
func (e *Editor) PlanRevert(force bool) (RevertPlan, error) {
	d := e.Active()
	var err error
	switch {
	case d == nil:
		err = ErrNoDocument
	case d.Path == "":
		err = ErrNoPath
	}
	if err != nil {
		e.setError(err)
		return RevertPlan{}, err
	}
	switch {
	case d.restoring():
		// A swap of the file would drop the stashed edits that wait for the index.
		err := fmt.Errorf("revert %s: %w", d.Name(), ErrRestoring)
		e.setError(err)
		return RevertPlan{}, err
	case d.large != nil && d.Modified():
		// The edits of a large document are an overlay on its file. They are not
		// discarded by a reload, which only swaps the file under an unmodified one.
		err := &largeError{
			msg:  fmt.Sprintf("revert %s: a large file with unsaved changes is not reverted; save it or close it", d.Name()),
			errs: []error{ErrLargeFile},
		}
		e.setError(err)
		return RevertPlan{}, err
	case d.large == nil && e.isLargePath(d.Path):
		// A file that has grown into a large one since the document was opened is not
		// read whole.
		err := fmt.Errorf("revert %s: %w", d.Name(), ErrLargeFile)
		e.setError(err)
		return RevertPlan{}, err
	}
	plan := RevertPlan{
		Path:     d.Path,
		Force:    force,
		Modified: d.Modified(),
		planned:  true,
		eol:      d.eol,
		mark:     d.contentMark(),
	}
	if d.large != nil {
		spec := e.loadSpec()
		plan.spec = &spec
	}
	if plan.Modified && !force {
		err := fmt.Errorf("revert %s: %w", d.Name(), ErrUnsaved)
		e.setError(err)
		return plan, err
	}
	return plan, nil
}

// RunRevert reads the file of plan from disk, decoded as a file is opened. It reads only the
// plan and changes no editor state, so it may run in a command, off the update loop. A file
// that no longer exists is reported in the result as an error that wraps fs.ErrNotExist.
func RunRevert(plan RevertPlan) RevertResult {
	spec := loadSpec{eol: plan.eol}
	if plan.spec != nil {
		spec = *plan.spec
		spec.eol = plan.eol
	} else if info, err := os.Stat(plan.Path); err == nil && info.Size() > maxFullLoad {
		// A plan with no large settings reads the file whole, which a file this big
		// must not be.
		return RevertResult{Err: fmt.Errorf("revert %s: %w", filepath.Base(plan.Path), ErrLargeFile)}
	}
	fresh, err := readDocument(plan.Path, spec)
	if err != nil {
		return RevertResult{Err: fmt.Errorf("revert: %w", err)}
	}
	if !fresh.disk.exists {
		return RevertResult{Err: fmt.Errorf("revert %s: %w", fresh.Name(), fs.ErrNotExist)}
	}
	if fresh.large != nil {
		return RevertResult{fresh: fresh}
	}
	return RevertResult{Text: fresh.Buffer.Text(), fresh: fresh}
}

// PlanRevertPath makes the plan for the reload of the document whose file is path, which
// need not be the active one. It is what auto-revert plans for a tab in the background.
// Like a plan made from the path alone, it is accepted only while the tab is clean, but it
// carries the settings of the editor, so a large file is opened as a large file.
func (e *Editor) PlanRevertPath(path string) RevertPlan {
	spec := e.loadSpec()
	plan := RevertPlan{Path: path, spec: &spec}
	if d := e.docWithPath(path); d != nil {
		plan.eol = d.eol
	}
	return plan
}

// ApplyRevert puts the text that RunRevert read into the document of plan, as one undo step,
// and marks the document saved. The document is found by plan.Path, so a tab that is not
// active can be reverted, and the active tab does not change. Text that is already the file's
// text is not replaced: the undo history, the caret, the selection and the folds stay, and
// only the record of the file and the saved state move on. A read that failed is returned as
// it is. A document that is no longer open and one whose text changed
// since the plan are refused, and so is a file that became binary under a text tab, which
// would show the document as control pictures. A read-only document is reverted: only its
// buffer changes. The document's status says why a revert was refused. It runs on the
// editor's goroutine.
func (e *Editor) ApplyRevert(plan RevertPlan, res RevertResult) error {
	if res.Err != nil {
		return res.Err
	}
	d := e.docWithPath(plan.Path)
	if d == nil {
		e.releaseDoc(res.fresh)
		return fmt.Errorf("revert %s: %w", filepath.Base(plan.Path), ErrNoDocument)
	}
	d.revertAuto = plan.Auto
	defer func() { d.revertAuto = false }()
	freshLarge := res.fresh != nil && res.fresh.large != nil
	var err error
	switch {
	case d.large != nil && (plan.spec == nil || res.fresh == nil), d.large == nil && freshLarge:
		// A plan with no large settings read the file as a small one. A small document
		// is not swapped for a large one either: that is a file that has grown, and the
		// plan refuses it.
		err = ErrLargeFile
	case res.fresh != nil && res.fresh.binary && !d.binary:
		err = fmt.Errorf("file is now %w", ErrBinary)
	case plan.planned && d.contentMark() != plan.mark:
		err = ErrStaleRevert
	case !plan.planned && d.Modified():
		// A path-only plan has no text to compare, so the tab must still be clean.
		err = ErrStaleRevert
	case d.restoring():
		err = ErrRestoring
	case d.large != nil && d.Modified():
		err = ErrLargeFile
	}
	if err != nil {
		e.releaseDoc(res.fresh)
		err = fmt.Errorf("revert %s: %w", d.Name(), err)
		d.fail(err)
		return err
	}
	if d.large != nil {
		if res.fresh.large != nil && !d.dirty() && res.fresh.disk.sameAs(d.disk) {
			// The file is the one the document reads or has just written, as after a
			// save: a swap would only restart the index and drop the selection.
			e.releaseDoc(res.fresh)
			return nil
		}
		// The text of a large document is in its file. The reload opens the file again
		// and puts the new buffer in place of the old one.
		e.swapFile(d, res.fresh)
		return nil
	}
	if res.Text == d.Buffer.Text() {
		// The file already holds this text, as after a save, so nothing is replaced.
		if res.fresh != nil && !d.dirty() && res.fresh.disk.sameAs(d.disk) {
			// It is also the file the document has just read or written, so there is
			// nothing to adopt and nothing to report: the watcher sees every own save,
			// and "Reverted" would replace its "Saved".
			e.releaseDoc(res.fresh)
			return nil
		}
		d.adoptFile(res.Text, res.fresh)
		d.status = d.revertedStatus()
		return nil
	}
	d.Buffer.Group(func() {
		d.Buffer.Delete(buffer.Pos{}, d.Buffer.End())
		d.Buffer.Insert(buffer.Pos{}, res.Text)
	})
	d.adoptFile(res.Text, res.fresh)
	d.Sel = nil
	d.clearCarets()
	d.lastMulti = nil
	d.folded = nil
	d.findRange = nil
	d.clampToText()
	d.want = -1
	d.status = d.revertedStatus()
	return nil
}

// adoptFile records the file that a read found as the saved state of d: the saved text, and
// the line ending, encoding and disk state of the read. A result with no read behind it (fresh
// is nil) changes only the saved text.
func (d *Document) adoptFile(text string, fresh *Document) {
	d.Buffer.MarkSaved()
	d.metaDirty = false
	d.setSaved(text)
	if fresh == nil {
		return
	}
	d.eol = fresh.eol
	d.ends = fresh.ends
	d.disk = fresh.disk
	d.codec = fresh.codec
	d.encoding = fresh.encoding
	// A binary tab whose file became text is text now. It stays read-only, since it was
	// opened that way, and toggle_read_only can make it editable.
	d.binary = fresh.binary
}

// docWithPath returns the open document whose file is path, or nil. An empty path matches
// nothing, so an untitled document is never found this way.
func (e *Editor) docWithPath(path string) *Document {
	if path == "" {
		return nil
	}
	for _, d := range e.docs {
		if d.Path == path {
			return d
		}
	}
	return nil
}

// Revert reloads the active document from its file, as one undo step, and replaces its unsaved
// text. A modified document is refused with ErrUnsaved unless force is set. It runs the plan,
// the read and the apply in turn on this goroutine, so it waits for the file to be read.
func (e *Editor) Revert(force bool) error {
	plan, err := e.PlanRevert(force)
	if err != nil {
		return err
	}
	return e.ApplyRevert(plan, RunRevert(plan))
}

// revertDocument is the revert action. Its force argument is passed on to Revert.
func revertDocument(e *Editor, _ *Document, args map[string]any) error {
	return e.Revert(forced(args))
}

// swapFile puts the buffer of fresh, a document just opened from the file of d, in place
// of the buffer of d, which is released: its file is closed and the scan of its line index
// stops. The caret and the top line stay where they were, as far as the new text has
// them. A large file whose index is still being built has fewer lines than it will, so a
// caret past the end waits for the index (see pending.go). Everything that refers to the
// old text is dropped: the selection, the other carets, the folds, the undo records. The
// file may have become small, or small and large; d takes the form of fresh either way.
func (e *Editor) swapFile(d, fresh *Document) {
	cursor, top := d.Cursor, d.Top
	e.releaseDoc(d)
	d.setBuffer(fresh.Buffer)
	d.large = fresh.large
	d.setSaved(fresh.saved)
	d.eol, d.ends, d.disk = fresh.eol, fresh.ends, fresh.disk
	d.codec, d.encoding = fresh.codec, fresh.encoding
	d.readOnly, d.binary = fresh.readOnly, fresh.binary
	d.metaDirty = false
	d.Sel = nil
	d.block = nil
	d.clearCarets()
	d.folded = nil
	d.want = -1
	d.resetHistoryState()
	d.Cursor = d.Buffer.Clamp(cursor)
	d.Top = max(min(top, d.Buffer.Lines()-1), 0)
	if d.large != nil && cursor != d.Cursor && !d.Index().Complete {
		d.large.pending = &pending{
			kind: pendingGoto, line: cursor.Line + 1, col: cursor.Col + 1, from: jumpEntryOf(d),
		}
	}
	d.status = d.revertedStatus()
}

// revertedStatus is the status after a reload from disk: "Reverted a.txt" for a revert the
// user asked for, and "Reloaded a.txt from disk" for one made because the file changed.
func (d *Document) revertedStatus() string {
	if d.revertAuto {
		return "Reloaded " + d.Name() + " from disk"
	}
	return "Reverted " + d.Name()
}
