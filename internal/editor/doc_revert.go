package editor

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"

	"github.com/tbxark/gvim/internal/buffer"
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

	planned bool   // PlanRevert made the plan, so text holds the document's text
	eol     string // the line ending of the document, which the read keeps when it can
	text    string // the text at the plan, which ApplyRevert checks is unchanged
}

// RevertResult is the file that RunRevert read for a RevertPlan. ApplyRevert puts it in place.
type RevertResult struct {
	// Text is the text of the file, decoded as a file is opened. It is empty when Err is set.
	Text string
	// Err is the error of the read. A file that no longer exists wraps fs.ErrNotExist.
	Err error

	// fresh is the document the read made, which records the file on disk.
	fresh *Document
}

// PlanRevert prepares the reload of the active document from its file. It reads no file and
// changes no state, so the UI may call it on its update loop. A missing tab, a read-only
// document and an untitled one are refused with ErrNoDocument, ErrReadOnly and ErrNoPath. A
// modified document is refused with ErrUnsaved unless force is set, and the plan it returns
// then has Modified set. The refusals are also the tab's status.
func (e *Editor) PlanRevert(force bool) (RevertPlan, error) {
	d := e.Active()
	var err error
	switch {
	case d == nil:
		err = ErrNoDocument
	case d.readOnly:
		err = ErrReadOnly
	case d.Path == "":
		err = ErrNoPath
	}
	if err != nil {
		e.setStatus(err.Error())
		return RevertPlan{}, err
	}
	plan := RevertPlan{
		Path:     d.Path,
		Force:    force,
		Modified: d.Modified(),
		planned:  true,
		eol:      d.eol,
		text:     d.Buffer.Text(),
	}
	if plan.Modified && !force {
		err := fmt.Errorf("revert %s: %w", d.Name(), ErrUnsaved)
		e.setStatus(err.Error())
		return plan, err
	}
	return plan, nil
}

// RunRevert reads the file of plan from disk, decoded as a file is opened. It reads only the
// plan and changes no editor state, so it may run in a command, off the update loop. A file
// that no longer exists is reported in the result as an error that wraps fs.ErrNotExist.
func RunRevert(plan RevertPlan) RevertResult {
	fresh, err := readDocument(plan.Path, plan.eol)
	if err != nil {
		return RevertResult{Err: fmt.Errorf("revert: %w", err)}
	}
	if !fresh.disk.exists {
		return RevertResult{Err: fmt.Errorf("revert %s: %w", fresh.Name(), fs.ErrNotExist)}
	}
	return RevertResult{Text: fresh.Buffer.Text(), fresh: fresh}
}

// ApplyRevert puts the text that RunRevert read into the document of plan, as one undo step,
// and marks the document saved. The document is found by plan.Path, so a tab that is not
// active can be reverted, and the active tab does not change. Text that is already the file's
// text is not replaced: the undo history, the caret, the selection and the folds stay, and
// only the record of the file and the saved state move on. A read that failed is returned as
// it is. A document that is no longer open, one that became read-only, and one whose text
// changed since the plan are refused, and the document's status says why. It runs on the
// editor's goroutine.
func (e *Editor) ApplyRevert(plan RevertPlan, res RevertResult) error {
	if res.Err != nil {
		return res.Err
	}
	d := e.docWithPath(plan.Path)
	if d == nil {
		return fmt.Errorf("revert %s: %w", filepath.Base(plan.Path), ErrNoDocument)
	}
	var err error
	switch {
	case d.readOnly:
		err = ErrReadOnly
	case plan.planned && d.Buffer.Text() != plan.text:
		err = ErrStaleRevert
	case !plan.planned && d.Modified():
		// A path-only plan has no text to compare, so the tab must still be clean.
		err = ErrStaleRevert
	}
	if err != nil {
		err = fmt.Errorf("revert %s: %w", d.Name(), err)
		d.status = err.Error()
		return err
	}
	if res.Text == d.Buffer.Text() {
		// The file already holds this text, as after a save, so nothing is replaced.
		d.adoptFile(res.Text, res.fresh)
		d.status = "Reverted " + d.Name()
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
	d.status = "Reverted " + d.Name()
	return nil
}

// adoptFile records the file that a read found as the saved state of d: the saved text, and
// the line ending, encoding and disk state of the read. A result with no read behind it (fresh
// is nil) changes only the saved text.
func (d *Document) adoptFile(text string, fresh *Document) {
	d.Buffer.MarkSaved()
	d.saved = text
	if fresh == nil {
		return
	}
	d.eol = fresh.eol
	d.disk = fresh.disk
	d.codec = fresh.codec
	d.encoding = fresh.encoding
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
