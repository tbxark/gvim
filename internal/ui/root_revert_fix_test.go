package ui

import (
	"errors"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/editor"
	"github.com/tbxark/gvim/internal/theme"
)

// revertDoc is a document with a file path and a modified flag.
type revertDoc struct {
	*fakeDoc
	path  string
	dirty bool
}

func (d revertDoc) FilePath() string { return d.path }
func (d revertDoc) HasPath() bool    { return d.path != "" }
func (d revertDoc) Modified() bool   { return d.dirty }

// docEditor is a fake editor whose active document is doc. A nil doc means no tab.
type docEditor struct {
	*fakeEditor
	doc *revertDoc
}

func newDocEditor(path string, dirty bool) docEditor {
	return docEditor{
		fakeEditor: newFakeEditor("x"),
		doc:        &revertDoc{fakeDoc: &fakeDoc{lines: []string{"x"}}, path: path, dirty: dirty},
	}
}

func (e docEditor) Active() Document {
	if e.doc == nil {
		return nil
	}
	return e.doc
}

// revertEditor is a document editor with the revert split. It records each step. The
// result of an apply is applyErr, so a test can make the text look changed.
type revertEditor struct {
	docEditor
	planErr  error
	planned  int  // PlanRevert calls
	force    bool // the force argument of the last PlanRevert call
	read     int  // RunRevert calls
	plan     editor.RevertPlan
	applied  int // ApplyRevert calls
	applyErr error
}

func (e *revertEditor) PlanRevert(force bool) (editor.RevertPlan, error) {
	e.planned++
	e.force = force
	if e.planErr != nil {
		return editor.RevertPlan{}, e.planErr
	}
	return editor.RevertPlan{Path: e.doc.path, Force: force, Modified: e.doc.dirty}, nil
}

func (e *revertEditor) RunRevert(plan editor.RevertPlan) editor.RevertResult {
	e.read++
	e.plan = plan
	return editor.RevertResult{}
}

func (e *revertEditor) ApplyRevert(plan editor.RevertPlan, res editor.RevertResult) error {
	e.applied++
	e.plan = plan
	return e.applyErr
}

// newRevertModel returns a model over ed, sized 80 by 24.
func newRevertModel(t *testing.T, ed EditorState) *Model {
	t.Helper()
	m := New(Deps{Editor: ed, Keys: testKeys(), Theme: theme.Theme{}, Clock: testClock})
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	return m
}

// feedMessages runs cmd and feeds each message it produces back into m, which is what
// the program does with the results of commands.
func feedMessages(m *Model, cmd tea.Cmd) {
	for _, msg := range collect(cmd) {
		_, next := m.Update(msg)
		feedMessages(m, next)
	}
}

// TestRevertFileAsksBeforeDiscardingChanges checks that Revert File asks only when the
// buffer is modified, that Yes discards the changes, and that No keeps the text.
func TestRevertFileAsksBeforeDiscardingChanges(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		dirty     bool
		key       rune // the answer to the confirm, when one opens
		wantAsk   bool
		wantPlans int // the PlanRevert calls
	}{
		{name: "a clean buffer reverts without asking", wantPlans: 1},
		{name: "a modified buffer asks, and Yes discards the changes", dirty: true, key: 'y', wantAsk: true, wantPlans: 1},
		{name: "a modified buffer asks, and No keeps the text", dirty: true, key: 'n', wantAsk: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ed := &revertEditor{docEditor: newDocEditor("/ws/a.txt", tt.dirty)}
			m := newRevertModel(t, ed)
			feedMessages(m, rootPaletteRun(m, "revert"))
			if tt.wantAsk {
				if len(m.overlays) == 0 {
					t.Fatal("revert of a modified buffer did not open a confirm")
				}
				if _, ok := m.overlays[len(m.overlays)-1].(confirmOverlay); !ok {
					t.Fatal("the overlay on top of a modified buffer's revert is not a confirm")
				}
				if !strings.Contains(m.paint().String(), "Discard changes?") {
					t.Fatal("the confirm does not ask Discard changes?")
				}
				feedMessages(m, press(m, typed(string(tt.key))))
			}
			if ed.planned != tt.wantPlans {
				t.Fatalf("PlanRevert calls = %d, want %d", ed.planned, tt.wantPlans)
			}
		})
	}
}

// TestRevertFileReadsInACommandAndAppliesInUpdate checks that the disk read runs in the
// command the revert returns, and that the result is applied when its message arrives.
func TestRevertFileReadsInACommandAndAppliesInUpdate(t *testing.T) {
	t.Parallel()
	ed := &revertEditor{docEditor: newDocEditor("/ws/a.txt", false)}
	m := newRevertModel(t, ed)
	cmd := rootPaletteRun(m, "revert")
	if ed.read != 0 {
		t.Fatalf("RunRevert ran %d times in Update, want 0", ed.read)
	}
	msgs := collect(cmd)
	if ed.read != 1 {
		t.Fatalf("RunRevert ran %d times in the command, want 1", ed.read)
	}
	if ed.applied != 0 {
		t.Fatal("ApplyRevert ran before the result message reached Update")
	}
	for _, msg := range msgs {
		_, next := m.Update(msg)
		feedMessages(m, next)
	}
	if ed.applied != 1 || ed.plan.Path != "/ws/a.txt" {
		t.Fatalf("ApplyRevert calls = %d for plan %q, want one for /ws/a.txt", ed.applied, ed.plan.Path)
	}
}

// TestRevertFilePlansWithTheForceFlag checks that the plan carries the force flag of the
// revert: a clean buffer plans without force, and a modified buffer plans with force only
// after Yes.
func TestRevertFilePlansWithTheForceFlag(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		dirty     bool
		key       rune
		wantForce bool
		wantPlans int
	}{
		{name: "a clean buffer plans without force", wantPlans: 1},
		{name: "a modified buffer plans with force after Yes", dirty: true, key: 'y', wantForce: true, wantPlans: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ed := &revertEditor{docEditor: newDocEditor("/ws/a.txt", tt.dirty)}
			m := newRevertModel(t, ed)
			feedMessages(m, rootPaletteRun(m, "revert"))
			if tt.dirty {
				feedMessages(m, press(m, typed(string(tt.key))))
			}
			if ed.planned != tt.wantPlans {
				t.Fatalf("PlanRevert calls = %d, want %d", ed.planned, tt.wantPlans)
			}
			if ed.planned > 0 && ed.force != tt.wantForce {
				t.Fatalf("PlanRevert force = %v, want %v", ed.force, tt.wantForce)
			}
		})
	}
}

// TestRevertFileAsksAgainWhenTheTextChangedAfterThePlan checks that a stale apply opens the
// retry confirm, that Yes runs the revert again, and that No stops.
func TestRevertFileAsksAgainWhenTheTextChangedAfterThePlan(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		key       rune
		wantPlans int
	}{
		{name: "Yes runs the revert again", key: 'y', wantPlans: 2},
		{name: "No stops", key: 'n', wantPlans: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ed := &revertEditor{docEditor: newDocEditor("/ws/a.txt", false), applyErr: editor.ErrStaleRevert}
			m := newRevertModel(t, ed)
			feedMessages(m, rootPaletteRun(m, "revert"))
			if len(m.overlays) == 0 {
				t.Fatal("a stale revert did not open the retry confirm")
			}
			if !strings.Contains(m.paint().String(), "Try again?") {
				t.Fatal("the confirm does not ask to try again")
			}
			feedMessages(m, press(m, typed(string(tt.key))))
			if ed.planned != tt.wantPlans {
				t.Fatalf("PlanRevert calls = %d, want %d", ed.planned, tt.wantPlans)
			}
		})
	}
}

// TestRevertFileNoticesWhatItCannotRevert checks the three refusals the notice row shows:
// no tab, an untitled buffer and a plan the editor refuses.
func TestRevertFileNoticesWhatItCannotRevert(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		ed      *revertEditor
		want    string
		planned int
	}{
		{name: "no tab", ed: &revertEditor{fakeEditor: newFakeEditor("x")}, want: noDocumentNotice},
		{name: "untitled buffer", ed: &revertEditor{docEditor: newDocEditor("", false)}, want: editor.ErrNoPath.Error()},
		{
			name: "plan refused",
			ed:   &revertEditor{docEditor: newDocEditor("/ws/a.txt", false), planErr: editor.ErrUnsaved},
			want: editor.ErrUnsaved.Error(), planned: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := newRevertModel(t, tt.ed)
			feedMessages(m, rootPaletteRun(m, "revert"))
			if m.notice != tt.want {
				t.Fatalf("notice = %q, want %q", m.notice, tt.want)
			}
			if tt.ed.planned != tt.planned {
				t.Fatalf("PlanRevert calls = %d, want %d", tt.ed.planned, tt.planned)
			}
		})
	}
}

// TestRevertFileFallsBackToTheEditorAction checks that an editor without the revert split
// runs the revert action through Do, with force only after the Discard answer.
func TestRevertFileFallsBackToTheEditorAction(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		dirty     bool
		key       rune
		wantForce []bool // the force argument of each "revert" the editor ran
	}{
		{name: "a clean buffer runs revert without force", wantForce: []bool{false}},
		{name: "a modified buffer runs revert with force after Yes", dirty: true, key: 'y', wantForce: []bool{true}},
		{name: "a modified buffer runs nothing after No", dirty: true, key: 'n'},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ed := newDocEditor("/ws/a.txt", tt.dirty)
			m := newRevertModel(t, ed)
			feedMessages(m, rootPaletteRun(m, "revert"))
			if tt.dirty {
				feedMessages(m, press(m, typed(string(tt.key))))
			}
			var force []bool
			for i, a := range ed.actions {
				if a != "revert" {
					continue
				}
				f, _ := ed.args[i]["force"].(bool)
				force = append(force, f)
			}
			if !slices.Equal(force, tt.wantForce) {
				t.Fatalf("revert calls force = %v, want %v", force, tt.wantForce)
			}
		})
	}
}

// TestRevertFileDoesNotHideAnEditorError checks that an editor refusal of the fallback
// shows on the notice row, as the plan path does.
func TestRevertFileDoesNotHideAnEditorError(t *testing.T) {
	t.Parallel()
	err := errors.New("revert a.txt: unsaved changes")
	ed := &failingRevert{docEditor: newDocEditor("/ws/a.txt", false), err: err}
	m := newRevertModel(t, ed)
	feedMessages(m, rootPaletteRun(m, "revert"))
	if m.notice != err.Error() {
		t.Fatalf("notice = %q, want %q", m.notice, err.Error())
	}
}

// failingRevert is a fake editor whose revert action fails with err.
type failingRevert struct {
	docEditor
	err error
}

func (f *failingRevert) Do(a editor.Action, args map[string]any) editor.Result {
	if a == "revert" {
		return editor.Result{Err: f.err}
	}
	return f.docEditor.Do(a, args)
}
