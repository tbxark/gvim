package editor

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// TestPlanRevertReadsNothing checks that PlanRevert reads no file. The file is deleted
// after the open, and the plan still works and reports the unsaved edit.
func TestPlanRevertReadsNothing(t *testing.T) {
	t.Parallel()
	e, path := openRevertable(t, "a\n")
	typeText(t, e, "x")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	plan, err := e.PlanRevert(true)
	if err != nil {
		t.Fatalf("PlanRevert: %v", err)
	}
	if plan.Path != path {
		t.Fatalf("plan.Path = %q, want %q", plan.Path, path)
	}
	if !plan.Modified {
		t.Fatal("plan.Modified = false, want true for an edited buffer")
	}
	if got := e.Active().Buffer.Text(); got != "xa\n" {
		t.Fatalf("text = %q, want the edit kept", got)
	}
}

// TestPlanRevertRefusals checks the refusals that PlanRevert makes before any read.
func TestPlanRevertRefusals(t *testing.T) {
	t.Parallel()
	tests := []struct {
		Name    string
		setup   func(t *testing.T) *Editor
		wantErr error
	}{
		{
			Name:    "no tab is open",
			setup:   func(t *testing.T) *Editor { return New(Options{Empty: true}) },
			wantErr: ErrNoDocument,
		},
		{
			Name:    "an untitled buffer has no file",
			setup:   func(t *testing.T) *Editor { return New(Options{}) },
			wantErr: ErrNoPath,
		},
		{
			Name: "a modified document is refused without force",
			setup: func(t *testing.T) *Editor {
				e, _ := openRevertable(t, "a\n")
				typeText(t, e, "x")
				return e
			},
			wantErr: ErrUnsaved,
		},
	}
	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()
			e := tt.setup(t)
			if _, err := e.PlanRevert(false); !errors.Is(err, tt.wantErr) {
				t.Fatalf("PlanRevert err = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

// TestRunRevertReadsTheDiskAndChangesNoState checks the read on its own. RunRevert returns
// the file as it is when it runs, and the editor keeps its text until ApplyRevert.
func TestRunRevertReadsTheDiskAndChangesNoState(t *testing.T) {
	t.Parallel()
	e, path := openRevertable(t, "a\n")
	plan, err := e.PlanRevert(false)
	if err != nil {
		t.Fatalf("PlanRevert: %v", err)
	}
	putFile(t, path, "b\n")
	res := RunRevert(plan)
	if res.Err != nil {
		t.Fatalf("RunRevert: %v", res.Err)
	}
	if res.Text != "b\n" {
		t.Fatalf("result text = %q, want the file text %q", res.Text, "b\n")
	}
	if got := e.Active().Buffer.Text(); got != "a\n" {
		t.Fatalf("after RunRevert text = %q, want it unchanged", got)
	}
	if err := e.ApplyRevert(plan, res); err != nil {
		t.Fatalf("ApplyRevert: %v", err)
	}
	if got := e.Active().Buffer.Text(); got != "b\n" {
		t.Fatalf("after ApplyRevert text = %q, want %q", got, "b\n")
	}
	if e.Active().Modified() {
		t.Fatal("document is modified after ApplyRevert, want it saved")
	}
}

func TestRunRevertOfDeletedFileFails(t *testing.T) {
	t.Parallel()
	e, path := openRevertable(t, "a\n")
	plan, err := e.PlanRevert(false)
	if err != nil {
		t.Fatalf("PlanRevert: %v", err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	res := RunRevert(plan)
	if !errors.Is(res.Err, fs.ErrNotExist) {
		t.Fatalf("RunRevert err = %v, want fs.ErrNotExist", res.Err)
	}
	if err := e.ApplyRevert(plan, res); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("ApplyRevert err = %v, want fs.ErrNotExist", err)
	}
	if got := e.Active().Buffer.Text(); got != "a\n" {
		t.Fatalf("text = %q, want it unchanged", got)
	}
}

// TestApplyRevertRefusesAChangedText checks that a text changed after the plan is not
// replaced. The plan was made when the text was "a", so the edit typed later is kept.
func TestApplyRevertRefusesAChangedText(t *testing.T) {
	t.Parallel()
	e, path := openRevertable(t, "a\n")
	plan, err := e.PlanRevert(false)
	if err != nil {
		t.Fatalf("PlanRevert: %v", err)
	}
	putFile(t, path, "b\n")
	res := RunRevert(plan)
	if res.Err != nil {
		t.Fatalf("RunRevert: %v", res.Err)
	}
	typeText(t, e, "x")
	if err := e.ApplyRevert(plan, res); !errors.Is(err, ErrStaleRevert) {
		t.Fatalf("ApplyRevert err = %v, want ErrStaleRevert", err)
	}
	if got := e.Active().Buffer.Text(); got != "xa\n" {
		t.Fatalf("text = %q, want the edit kept", got)
	}
}

func TestApplyRevertOfAClosedTabFails(t *testing.T) {
	t.Parallel()
	e, path := openRevertable(t, "a\n")
	plan, err := e.PlanRevert(false)
	if err != nil {
		t.Fatalf("PlanRevert: %v", err)
	}
	putFile(t, path, "b\n")
	res := RunRevert(plan)
	if res.Err != nil {
		t.Fatalf("RunRevert: %v", res.Err)
	}
	if err := e.CloseTab(0, false); err != nil {
		t.Fatalf("CloseTab: %v", err)
	}
	if err := e.ApplyRevert(plan, res); !errors.Is(err, ErrNoDocument) {
		t.Fatalf("ApplyRevert err = %v, want ErrNoDocument", err)
	}
}

// TestApplyRevertAcceptsAPathOnlyPlanForACleanTab checks the plan that the UI makes for a
// clean background tab, which has the path and no text. The tab reloads from its file, found
// by the path, and the active tab does not change.
func TestApplyRevertAcceptsAPathOnlyPlanForACleanTab(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	pathA := filepath.Join(dir, "a.txt")
	pathB := filepath.Join(dir, "b.txt")
	putFile(t, pathA, "a\n")
	putFile(t, pathB, "b\n")
	e := New(Options{})
	if err := e.OpenFile(pathA); err != nil {
		t.Fatal(err)
	}
	if err := e.OpenFile(pathB); err != nil {
		t.Fatal(err)
	}
	putFile(t, pathA, "c\n")
	plan := RevertPlan{Path: pathA}
	res := RunRevert(plan)
	if res.Err != nil {
		t.Fatalf("RunRevert: %v", res.Err)
	}
	if err := e.ApplyRevert(plan, res); err != nil {
		t.Fatalf("ApplyRevert: %v", err)
	}
	if got := e.DocAt(0).Buffer.Text(); got != "c\n" {
		t.Fatalf("background tab text = %q, want %q", got, "c\n")
	}
	if got := e.Active().Path; got != pathB {
		t.Fatalf("active tab = %q, want %q", got, pathB)
	}
}

// TestApplyRevertRefusesAPathOnlyPlanForAnEditedTab checks that a background tab edited after
// its path-only plan keeps the edit. The plan assumed a clean tab, so replacing the text would
// lose the edit.
func TestApplyRevertRefusesAPathOnlyPlanForAnEditedTab(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	pathA := filepath.Join(dir, "a.txt")
	pathB := filepath.Join(dir, "b.txt")
	putFile(t, pathA, "a\n")
	putFile(t, pathB, "b\n")
	e := New(Options{})
	if err := e.OpenFile(pathA); err != nil {
		t.Fatal(err)
	}
	if err := e.OpenFile(pathB); err != nil {
		t.Fatal(err)
	}
	plan := RevertPlan{Path: pathA}
	putFile(t, pathA, "c\n")
	e.ActivateTab(0)
	typeText(t, e, "x")
	e.ActivateTab(1)
	res := RunRevert(plan)
	if res.Err != nil {
		t.Fatalf("RunRevert: %v", res.Err)
	}
	if err := e.ApplyRevert(plan, res); !errors.Is(err, ErrStaleRevert) {
		t.Fatalf("ApplyRevert err = %v, want ErrStaleRevert", err)
	}
	if got := e.DocAt(0).Buffer.Text(); got != "xa\n" {
		t.Fatalf("background tab text = %q, want the edit kept", got)
	}
}

// TestApplyRevertReloadsThePlannedTabOnly checks that a revert of a tab that is not the
// active one leaves the active tab alone.
func TestApplyRevertReloadsThePlannedTabOnly(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	pathA := filepath.Join(dir, "a.txt")
	pathB := filepath.Join(dir, "b.txt")
	putFile(t, pathA, "a\n")
	putFile(t, pathB, "b\n")
	e := New(Options{})
	if err := e.OpenFile(pathA); err != nil {
		t.Fatal(err)
	}
	if err := e.OpenFile(pathB); err != nil {
		t.Fatal(err)
	}
	e.ActivateTab(0)
	plan, err := e.PlanRevert(false)
	if err != nil {
		t.Fatalf("PlanRevert: %v", err)
	}
	putFile(t, pathA, "c\n")
	res := RunRevert(plan)
	if res.Err != nil {
		t.Fatalf("RunRevert: %v", res.Err)
	}
	e.ActivateTab(1)
	typeText(t, e, "y")
	if err := e.ApplyRevert(plan, res); err != nil {
		t.Fatalf("ApplyRevert: %v", err)
	}
	if got := e.DocAt(0).Buffer.Text(); got != "c\n" {
		t.Fatalf("planned tab text = %q, want %q", got, "c\n")
	}
	if got := e.Active().Buffer.Text(); got != "yb\n" {
		t.Fatalf("active tab text = %q, want the edit kept", got)
	}
}

// TestApplyRevertIsOneUndoStep checks the split path with a modified document: the
// forced revert is one undo step, so undo brings the edit back.
func TestApplyRevertIsOneUndoStep(t *testing.T) {
	t.Parallel()
	e, _ := openRevertable(t, "a\n")
	typeText(t, e, "x")
	plan, err := e.PlanRevert(true)
	if err != nil {
		t.Fatalf("PlanRevert: %v", err)
	}
	res := RunRevert(plan)
	if res.Err != nil {
		t.Fatalf("RunRevert: %v", res.Err)
	}
	if err := e.ApplyRevert(plan, res); err != nil {
		t.Fatalf("ApplyRevert: %v", err)
	}
	if got := e.Active().Buffer.Text(); got != "a\n" {
		t.Fatalf("after ApplyRevert text = %q, want %q", got, "a\n")
	}
	run(t, e, "undo")
	if got := e.Active().Buffer.Text(); got != "xa\n" {
		t.Fatalf("after undo text = %q, want the edit back", got)
	}
}

// TestApplyRevertKeepsTheUndoHistoryWhenTheFileHoldsTheText checks that a revert whose file
// already holds the buffer's text, as after a save, replaces nothing: the edit stays one undo
// step, and the document is saved.
func TestApplyRevertKeepsTheUndoHistoryWhenTheFileHoldsTheText(t *testing.T) {
	t.Parallel()
	e, _ := openRevertable(t, "a\n")
	typeText(t, e, "x")
	run(t, e, "save")
	plan, err := e.PlanRevert(false)
	if err != nil {
		t.Fatalf("PlanRevert: %v", err)
	}
	res := RunRevert(plan)
	if res.Err != nil {
		t.Fatalf("RunRevert: %v", res.Err)
	}
	if err := e.ApplyRevert(plan, res); err != nil {
		t.Fatalf("ApplyRevert: %v", err)
	}
	if got := e.Active().Buffer.Text(); got != "xa\n" {
		t.Fatalf("after ApplyRevert text = %q, want %q", got, "xa\n")
	}
	if e.Active().Modified() {
		t.Fatal("document is modified after ApplyRevert, want it saved")
	}
	run(t, e, "undo")
	if got := e.Active().Buffer.Text(); got != "a\n" {
		t.Fatalf("after undo text = %q, want %q: the undo history was replaced", got, "a\n")
	}
}

// TestRevertTakesAForceFlag checks the synchronous Revert the revert action runs. Without
// force a modified document is refused and keeps its edit. With force it is reloaded.
func TestRevertTakesAForceFlag(t *testing.T) {
	t.Parallel()
	tests := []struct {
		Name         string
		Edit         bool
		force        bool
		wantErr      error
		wantText     string
		wantModified bool
	}{
		{Name: "an unmodified document reloads", wantText: "b\n"},
		{Name: "a modified document is refused without force", Edit: true, wantErr: ErrUnsaved, wantText: "xa\n", wantModified: true},
		{Name: "a modified document reloads with force", Edit: true, force: true, wantText: "b\n"},
	}
	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()
			e, path := openRevertable(t, "a\n")
			if tt.Edit {
				typeText(t, e, "x")
			}
			putFile(t, path, "b\n")
			if err := e.Revert(tt.force); !errors.Is(err, tt.wantErr) {
				t.Fatalf("Revert err = %v, want %v", err, tt.wantErr)
			}
			d := e.Active()
			if got := d.Buffer.Text(); got != tt.wantText {
				t.Fatalf("text = %q, want %q", got, tt.wantText)
			}
			if d.Modified() != tt.wantModified {
				t.Fatalf("modified = %t, want %t", d.Modified(), tt.wantModified)
			}
		})
	}
}

// TestPlanRevertRefusesAnUnsavedDocumentWithoutForce checks that a modified document is refused
// without force, and that the refused plan still reports the unsaved edit, so the caller can ask.
// Nothing is read or replaced.
func TestPlanRevertRefusesAnUnsavedDocumentWithoutForce(t *testing.T) {
	t.Parallel()
	e, path := openRevertable(t, "a\n")
	typeText(t, e, "x")
	putFile(t, path, "b\n")
	plan, err := e.PlanRevert(false)
	if !errors.Is(err, ErrUnsaved) {
		t.Fatalf("PlanRevert err = %v, want ErrUnsaved", err)
	}
	if !plan.Modified {
		t.Fatal("plan.Modified = false, want true for an edited buffer")
	}
	if got := e.Active().Buffer.Text(); got != "xa\n" {
		t.Fatalf("text = %q, want the edit kept", got)
	}
}

// TestApplyRevertOfIdenticalTextChangesNothing checks that a result whose text is the buffer's
// own text replaces nothing, even one that RunRevert did not make. The undo history stays, so
// one undo still brings back the typed text.
func TestApplyRevertOfIdenticalTextChangesNothing(t *testing.T) {
	t.Parallel()
	e, _ := openRevertable(t, "a\n")
	typeText(t, e, "x")
	plan, err := e.PlanRevert(true)
	if err != nil {
		t.Fatalf("PlanRevert: %v", err)
	}
	if err := e.ApplyRevert(plan, RevertResult{Text: "xa\n"}); err != nil {
		t.Fatalf("ApplyRevert: %v", err)
	}
	if got := e.Active().Buffer.Text(); got != "xa\n" {
		t.Fatalf("text = %q, want it unchanged", got)
	}
	run(t, e, "undo")
	if got := e.Active().Buffer.Text(); got != "a\n" {
		t.Fatalf("after undo text = %q, want %q: the undo history was replaced", got, "a\n")
	}
}

// TestApplyRevertWithNoPathFindsNoDocument checks that a plan with no path does not match an
// untitled document, whose path is also empty. The untitled text is left alone.
func TestApplyRevertWithNoPathFindsNoDocument(t *testing.T) {
	t.Parallel()
	e := pathDoc(Options{}, "", "a\n")
	if err := e.ApplyRevert(RevertPlan{}, RevertResult{Text: "b\n"}); !errors.Is(err, ErrNoDocument) {
		t.Fatalf("ApplyRevert err = %v, want ErrNoDocument", err)
	}
	if got := e.Active().Buffer.Text(); got != "a\n" {
		t.Fatalf("text = %q, want it unchanged", got)
	}
}
