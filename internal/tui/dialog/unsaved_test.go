package dialog

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestUnsavedChangesEnterSavesAfterGrace(t *testing.T) {
	t.Parallel()
	c := newFakeClock()
	d := NewUnsavedChanges(c.Now, "a.txt", "tag", testOv)
	c.afterGrace()
	next, cmd := d.Update(key("enter"))
	if next != nil {
		t.Fatalf("Update returned an open overlay after the answer")
	}
	r, ok := answer(cmd)
	if !ok || r.Choice != ChoiceSave || r.Tag != "tag" {
		t.Fatalf("Enter answered %+v, ok=%v, want Save with the tag", r, ok)
	}
}

func TestUnsavedChangesLetters(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		key  string
		want Choice
	}{
		{"lower s saves", "s", ChoiceSave},
		{"upper S saves", "S", ChoiceSave},
		{"lower d discards", "d", ChoiceDiscard},
		{"upper D discards", "D", ChoiceDiscard},
		{"lower c cancels", "c", ChoiceCancel},
		{"upper C cancels", "C", ChoiceCancel},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c := newFakeClock()
			d := NewUnsavedChanges(c.Now, "a.txt", nil, testOv)
			c.afterGrace()
			next, cmd := d.Update(key(tt.key))
			if next != nil {
				t.Fatalf("overlay still open after %q", tt.key)
			}
			r, ok := answer(cmd)
			if !ok || r.Choice != tt.want {
				t.Fatalf("%q answered %+v, ok=%v, want %v", tt.key, r, ok, tt.want)
			}
		})
	}
}

func TestUnsavedChangesEscCancels(t *testing.T) {
	t.Parallel()
	c := newFakeClock()
	d := NewUnsavedChanges(c.Now, "a.txt", nil, testOv)
	c.afterGrace()
	_, cmd := d.Update(key("esc"))
	mustClose(t, d, cmd, ChoiceCancel, "")
}

func TestUnsavedChangesFocusMovesWithTabAndArrows(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		keys  []string
		after Choice // what Enter answers at the end
	}{
		{"starts on Save", nil, ChoiceSave},
		{"tab moves to Discard", []string{"tab"}, ChoiceDiscard},
		{"tab twice moves to Cancel", []string{"tab", "tab"}, ChoiceCancel},
		{"tab wraps to Save", []string{"tab", "tab", "tab"}, ChoiceSave},
		{"shift+tab wraps to Cancel", []string{"shift+tab"}, ChoiceCancel},
		{"right moves to Discard", []string{"right"}, ChoiceDiscard},
		{"left wraps to Cancel", []string{"left"}, ChoiceCancel},
		{"right then left returns to Save", []string{"right", "left"}, ChoiceSave},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c := newFakeClock()
			d := NewUnsavedChanges(c.Now, "a.txt", nil, testOv)
			c.afterGrace()
			var o Overlay = d
			for _, k := range tt.keys {
				o, _ = o.Update(key(k))
			}
			next, cmd := o.Update(key("enter"))
			if next != nil {
				t.Fatalf("Enter left the dialog open")
			}
			r, _ := answer(cmd)
			if r.Choice != tt.after {
				t.Fatalf("Enter after %v answered %v, want %v", tt.keys, r.Choice, tt.after)
			}
		})
	}
}

func TestUnsavedChangesClickActivatesButton(t *testing.T) {
	t.Parallel()
	c := newFakeClock()
	d := NewUnsavedChanges(c.Now, "a.txt", nil, testOv)
	g := draw(d, 80, 24)
	x, y := mustFind(t, g, "Discard")
	next, cmd := d.Update(click(x+1, y))
	if next != nil {
		t.Fatalf("a click on Discard left the dialog open")
	}
	mustClose(t, d, cmd, ChoiceDiscard, "")
}

func TestUnsavedChangesClickBeforeDrawDoesNothing(t *testing.T) {
	t.Parallel()
	c := newFakeClock()
	d := NewUnsavedChanges(c.Now, "a.txt", nil, testOv)
	c.afterGrace()
	next, cmd := d.Update(click(10, 10))
	mustOpen(t, next, cmd)
}

func TestUnsavedChangesClickOutsideDoesNothing(t *testing.T) {
	t.Parallel()
	c := newFakeClock()
	d := NewUnsavedChanges(c.Now, "a.txt", nil, testOv)
	g := draw(d, 80, 24)
	if _, _, ok := find(g, "Save changes"); !ok {
		t.Fatal("body is not drawn")
	}
	next, cmd := d.Update(click(0, 0))
	mustOpen(t, next, cmd)
}

func TestUnsavedChangesDrawsBodyAndButtons(t *testing.T) {
	t.Parallel()
	c := newFakeClock()
	d := NewUnsavedChanges(c.Now, "notes.md", nil, testOv)
	g := draw(d, 80, 24)
	for _, s := range []string{"Unsaved Changes", "Save changes to notes.md?", "Save", "Discard", "Cancel"} {
		mustFind(t, g, s)
	}
}

func TestConfirmTrashKeysAndFocus(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		keys []string
		want Choice
	}{
		{"enter starts on Cancel", []string{"enter"}, ChoiceCancel},
		{"t moves to trash", []string{"t"}, ChoiceMoveToTrash},
		{"upper T moves to trash", []string{"T"}, ChoiceMoveToTrash},
		{"c cancels", []string{"c"}, ChoiceCancel},
		{"esc cancels", []string{"esc"}, ChoiceCancel},
		{"left then enter moves to trash", []string{"left", "enter"}, ChoiceMoveToTrash},
		{"tab then enter moves to trash", []string{"tab", "enter"}, ChoiceMoveToTrash},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c := newFakeClock()
			d := NewConfirmTrash(c.Now, "old.txt", "tag", testOv)
			c.afterGrace()
			var o Overlay = d
			var last Result
			var answered bool
			for _, k := range tt.keys {
				var cmd tea.Cmd
				o, cmd = o.Update(key(k))
				if r, ok := answer(cmd); ok {
					last, answered = r, true
					break
				}
			}
			if !answered {
				t.Fatalf("keys %v did not answer", tt.keys)
			}
			if last.Choice != tt.want || last.Tag != "tag" {
				t.Fatalf("answered %+v, want %v with the tag", last, tt.want)
			}
		})
	}
}

func TestConfirmTrashDrawsQuestionAndUnderlinesT(t *testing.T) {
	t.Parallel()
	c := newFakeClock()
	d := NewConfirmTrash(c.Now, "old.txt", nil, testOv)
	g := draw(d, 80, 24)
	mustFind(t, g, `Move "old.txt" to Trash?`)
	x, y := mustFind(t, g, "Move to Trash")
	if !g.At(x+8, y).Style.Underline {
		t.Fatalf("the T of Trash is not underlined")
	}
	if g.At(x, y).Style.Underline {
		t.Fatalf("the M of Move is underlined, only the hotkey letter should be")
	}
}

func TestConfirmTrashWideButtonClick(t *testing.T) {
	d := NewConfirmTrash(newFakeClock().Now, "old.txt", nil, testOv)
	d.buttons[0] = newButton("删除文件", 0, ChoiceMoveToTrash)
	g := draw(d, 80, 24)
	x, y := mustFind(t, g, "删除文件")
	_, cmd := d.Update(click(x+7, y))
	mustClose(t, d, cmd, ChoiceMoveToTrash, "")
}

func TestConfirmTrashClickMovesToTrash(t *testing.T) {
	t.Parallel()
	c := newFakeClock()
	d := NewConfirmTrash(c.Now, "old.txt", nil, testOv)
	g := draw(d, 80, 24)
	x, y := mustFind(t, g, "Move to Trash")
	_, cmd := d.Update(click(x+2, y))
	mustClose(t, d, cmd, ChoiceMoveToTrash, "")
}
