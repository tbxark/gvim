package dialog

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestSaveAsPrefillIsDrawnAndEnterSaves(t *testing.T) {
	t.Parallel()
	c := newFakeClock()
	d := NewSaveAs(c.Now, "Untitled-1.txt", "tag", PathChecks{}, testOv)
	g := draw(d, 80, 24)
	mustFind(t, g, "Save As")
	mustFind(t, g, "Untitled-1.txt")
	mustFind(t, g, "Save")
	mustFind(t, g, "Cancel")
	c.afterGrace()
	next, cmd := d.Update(key("enter"))
	if next != nil {
		t.Fatalf("Enter left the dialog open")
	}
	r, ok := answer(cmd)
	if !ok || r.Choice != ChoiceSave || r.Text != "Untitled-1.txt" || r.Tag != "tag" {
		t.Fatalf("Enter answered %+v, want Save of the prefill", r)
	}
}

func TestSaveAsLetterTypesIntoField(t *testing.T) {
	t.Parallel()
	c := newFakeClock()
	d := NewSaveAs(c.Now, "", nil, PathChecks{}, testOv)
	c.afterGrace()
	o := typeText(t, d, "s")
	if !o.Active() {
		t.Fatal("typing s closed the dialog, it should type into the field")
	}
	_, cmd := o.Update(key("enter"))
	mustClose(t, d, cmd, ChoiceSave, "s")
}

func TestSaveAsFirstTypedKeyReplacesPrefill(t *testing.T) {
	t.Parallel()
	c := newFakeClock()
	d := NewSaveAs(c.Now, "/work/Untitled-2.txt", nil, PathChecks{}, testOv)
	c.afterGrace()
	o := typeText(t, d, "new.txt")
	_, cmd := o.Update(key("enter"))
	mustClose(t, d, cmd, ChoiceSave, "new.txt")
}

func TestSaveAsMoveKeyKeepsPrefill(t *testing.T) {
	t.Parallel()
	c := newFakeClock()
	d := NewSaveAs(c.Now, "ab", nil, PathChecks{}, testOv)
	c.afterGrace()
	d.Update(key("left"))
	o := typeText(t, d, "x")
	_, cmd := o.Update(key("enter"))
	mustClose(t, d, cmd, ChoiceSave, "axb")
}

func TestSaveAsPrefillReplacedOnlyOnce(t *testing.T) {
	t.Parallel()
	c := newFakeClock()
	d := NewSaveAs(c.Now, "ab", nil, PathChecks{}, testOv)
	c.afterGrace()
	typeText(t, d, "x")
	o := typeText(t, d, "y")
	_, cmd := o.Update(key("enter"))
	mustClose(t, d, cmd, ChoiceSave, "xy")
}

func TestSaveAsLongPrefillIsDrawnInFull(t *testing.T) {
	t.Parallel()
	c := newFakeClock()
	path := "/var/folders/0000gn/T/acc-ltuuo_kf/ws/Untitled-2.txt"
	d := NewSaveAs(c.Now, path, nil, PathChecks{}, testOv)
	g := draw(d, 100, 30)
	mustFind(t, g, path)
	x, y, ok := find(g, path)
	if !ok || x < 0 || y < 0 {
		t.Fatalf("prefill not drawn whole:\n%s", screen(g))
	}
}

func TestSaveAsBackspaceAndCaret(t *testing.T) {
	t.Parallel()
	c := newFakeClock()
	d := NewSaveAs(c.Now, "dir/name", nil, PathChecks{}, testOv)
	c.afterGrace()
	d.Update(key("backspace"))
	_, cmd := d.Update(key("enter"))
	mustClose(t, d, cmd, ChoiceSave, "dir/nam")
}

func TestSaveAsEmptyPathShowsInlineError(t *testing.T) {
	t.Parallel()
	c := newFakeClock()
	d := NewSaveAs(c.Now, "", nil, PathChecks{}, testOv)
	c.afterGrace()
	next, cmd := d.Update(key("enter"))
	mustOpen(t, next, cmd)
	g := draw(d, 80, 24)
	mustFind(t, g, "Enter a file name")
}

func TestSaveAsValidateRejectionKeepsDialogOpenUntilFixed(t *testing.T) {
	t.Parallel()
	c := newFakeClock()
	checks := PathChecks{Validate: func(path string) error {
		if strings.HasPrefix(path, "missing/") {
			return errors.New("No such folder: missing")
		}
		return nil
	}}
	d := NewSaveAs(c.Now, "missing/a.txt", nil, checks, testOv)
	c.afterGrace()
	next, cmd := d.Update(key("enter"))
	mustOpen(t, next, cmd)
	mustFind(t, draw(d, 80, 24), "No such folder: missing")

	// Editing the path clears the message, and a good path saves.
	typeText(t, d, "x")
	if strings.Contains(screen(draw(d, 80, 24)), "No such folder") {
		t.Fatal("the rejection message stayed after the path changed")
	}
	for range len("missing/a.txt") + 1 {
		d.Update(key("backspace"))
	}
	typeText(t, d, "ok.txt")
	_, cmd = d.Update(key("enter"))
	mustClose(t, d, cmd, ChoiceSave, "ok.txt")
}

func TestSaveAsExistingPathAsksToReplace(t *testing.T) {
	t.Parallel()
	c := newFakeClock()
	checks := PathChecks{Exists: func(path string) bool { return path == "a.txt" }}
	d := NewSaveAs(c.Now, "a.txt", "tag", checks, testOv)
	c.afterGrace()
	next, cmd := d.Update(key("enter"))
	mustOpen(t, next, cmd)
	g := draw(d, 80, 24)
	mustFind(t, g, "a.txt already exists. Replace it?")
	mustFind(t, g, "Replace File")
	mustFind(t, g, "Replace")

	_, cmd = d.Update(key("r"))
	mustClose(t, d, cmd, ChoiceReplace, "a.txt")
}

func TestSaveAsOverwriteCancelReturnsToNameWithTextKept(t *testing.T) {
	t.Parallel()
	c := newFakeClock()
	checks := PathChecks{Exists: func(path string) bool { return path == "a.txt" }}
	d := NewSaveAs(c.Now, "a.txt", nil, checks, testOv)
	c.afterGrace()
	d.Update(key("enter")) // asks to replace a.txt; Cancel has the focus
	next, cmd := d.Update(key("enter"))
	mustOpen(t, next, cmd)
	g := draw(d, 80, 24)
	mustFind(t, g, "Save As")
	mustFind(t, g, "a.txt")
	if strings.Contains(screen(g), "already exists") {
		t.Fatal("the overwrite step is still drawn after Cancel")
	}
	// The name is still there, so a second Enter asks again.
	d.Update(key("enter"))
	mustFind(t, draw(d, 80, 24), "already exists")
}

func TestSaveAsOverwriteKeysAndEsc(t *testing.T) {
	t.Parallel()
	checks := PathChecks{Exists: func(string) bool { return true }}
	tests := []struct {
		name string
		key  string
		want Choice
	}{
		{"upper R replaces", "R", ChoiceReplace},
		{"c cancels the step", "c", ChoiceCancel},
		{"esc closes the dialog", "esc", ChoiceCancel},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c := newFakeClock()
			d := NewSaveAs(c.Now, "a.txt", nil, checks, testOv)
			c.afterGrace()
			d.Update(key("enter"))
			_, cmd := d.Update(key(tt.key))
			if tt.key == "c" {
				// Cancel only leaves the overwrite step, so the dialog stays open.
				if cmd != nil || !d.Active() {
					t.Fatal("Cancel in the overwrite step closed the dialog")
				}
				return
			}
			r, _ := answer(cmd)
			if r.Choice != tt.want {
				t.Fatalf("%q answered %v, want %v", tt.key, r.Choice, tt.want)
			}
		})
	}
}

func TestSaveAsAltLettersActivateButtons(t *testing.T) {
	t.Parallel()
	c := newFakeClock()
	d := NewSaveAs(c.Now, "a.txt", nil, PathChecks{}, testOv)
	c.afterGrace()
	_, cmd := d.Update(altKey('c'))
	mustClose(t, d, cmd, ChoiceCancel, "")

	e := NewSaveAs(c.Now, "a.txt", nil, PathChecks{}, testOv)
	c.afterGrace()
	_, cmd = e.Update(altKey('s'))
	mustClose(t, e, cmd, ChoiceSave, "a.txt")
}

func TestSaveAsTabToButtonsAndClick(t *testing.T) {
	t.Parallel()
	c := newFakeClock()
	d := NewSaveAs(c.Now, "a.txt", nil, PathChecks{}, testOv)
	c.afterGrace()
	d.Update(key("tab"))
	_, cmd := d.Update(key("enter"))
	mustClose(t, d, cmd, ChoiceSave, "a.txt")

	e := NewSaveAs(c.Now, "a.txt", nil, PathChecks{}, testOv)
	g := draw(e, 80, 24)
	x, y := mustFind(t, g, "Cancel")
	_, cmd = e.Update(click(x, y))
	mustClose(t, e, cmd, ChoiceCancel, "")
}

func TestSaveAsCursorFollowsFieldFocus(t *testing.T) {
	t.Parallel()
	c := newFakeClock()
	d := NewSaveAs(c.Now, "abc", nil, PathChecks{}, testOv)
	g := draw(d, 80, 24)
	x, y, ok := d.Cursor(80, 24)
	if !ok {
		t.Fatal("no caret with the field focused")
	}
	fx, fy := mustFind(t, g, "abc")
	if x != fx+3 || y != fy {
		t.Fatalf("caret at (%d,%d), want (%d,%d)", x, y, fx+3, fy)
	}
	d.Update(key("tab"))
	if _, _, ok := d.Cursor(80, 24); ok {
		t.Fatal("caret reported with a button focused")
	}
}

func TestSaveAsWithoutExistsSavesImmediately(t *testing.T) {
	t.Parallel()
	c := newFakeClock()
	d := NewSaveAs(c.Now, "a.txt", nil, PathChecks{Validate: func(string) error { return nil }}, testOv)
	c.afterGrace()
	_, cmd := d.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	mustClose(t, d, cmd, ChoiceSave, "a.txt")
}
