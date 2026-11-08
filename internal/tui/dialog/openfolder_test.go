package dialog

import (
	"errors"
	"fmt"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestOpenFolderDrawsFieldAndRecentList(t *testing.T) {
	t.Parallel()
	c := newFakeClock()
	d := NewOpenFolder(c.Now, []string{"/src/one", "/src/two"}, "/src", nil, nil, testOv)
	g := draw(d, 80, 24)
	for _, s := range []string{"Open Folder", "/src", "Recent folders", "/src/one", "/src/two", "Open", "Cancel"} {
		mustFind(t, g, s)
	}
}

func TestOpenFolderEnterOpensTypedPath(t *testing.T) {
	t.Parallel()
	c := newFakeClock()
	d := NewOpenFolder(c.Now, []string{"/recent"}, "  /typed  ", "tag", nil, testOv)
	c.afterGrace()
	_, cmd := d.Update(key("enter"))
	mustClose(t, d, cmd, ChoiceOpen, "/typed")
}

func TestOpenFolderEmptyPathShowsError(t *testing.T) {
	t.Parallel()
	c := newFakeClock()
	d := NewOpenFolder(c.Now, nil, "", nil, nil, testOv)
	c.afterGrace()
	next, cmd := d.Update(key("enter"))
	mustOpen(t, next, cmd)
	mustFind(t, draw(d, 80, 24), "Enter a folder path")
	mustFind(t, draw(d, 80, 24), "No recent folders")
}

func TestOpenFolderValidateRejectsThenAccepts(t *testing.T) {
	t.Parallel()
	c := newFakeClock()
	validate := func(path string) error {
		if path != "/ok" {
			return errors.New("not a folder: " + path)
		}
		return nil
	}
	d := NewOpenFolder(c.Now, nil, "/nope", nil, validate, testOv)
	c.afterGrace()
	next, cmd := d.Update(key("enter"))
	mustOpen(t, next, cmd)
	mustFind(t, draw(d, 80, 24), "not a folder: /nope")

	for range len("/nope") {
		d.Update(key("backspace"))
	}
	typeText(t, d, "/ok")
	_, cmd = d.Update(key("enter"))
	mustClose(t, d, cmd, ChoiceOpen, "/ok")
}

func TestOpenFolderDownMovesIntoListAndEnterOpensSelected(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		keys []string
		want string
	}{
		{"down then enter opens the second entry", []string{"down", "enter"}, "/b"},
		{"down twice opens the third", []string{"down", "down", "enter"}, "/c"},
		{"down past the end stays on the last", []string{"down", "down", "down", "down", "enter"}, "/c"},
		{"up at the top stays on the first", []string{"up", "enter"}, "/a"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c := newFakeClock()
			d := NewOpenFolder(c.Now, []string{"/a", "/b", "/c"}, "/typed", nil, nil, testOv)
			c.afterGrace()
			var o Overlay = d
			var cmd tea.Cmd
			for _, k := range tt.keys {
				o, cmd = o.Update(key(k))
			}
			r, _ := answer(cmd)
			if r.Choice != ChoiceOpen || r.Text != tt.want {
				t.Fatalf("keys %v answered %+v, want Open %q", tt.keys, r, tt.want)
			}
		})
	}
}

func TestOpenFolderEnterOnEmptyListDoesNothing(t *testing.T) {
	t.Parallel()
	c := newFakeClock()
	d := NewOpenFolder(c.Now, nil, "/typed", nil, nil, testOv)
	c.afterGrace()
	d.Update(key("down"))
	next, cmd := d.Update(key("enter"))
	mustOpen(t, next, cmd)
}

func TestOpenFolderListScrollKeepsSelectionVisible(t *testing.T) {
	t.Parallel()
	var items []string
	for i := range 10 {
		items = append(items, fmt.Sprintf("/dir%02d", i))
	}
	c := newFakeClock()
	d := NewOpenFolder(c.Now, items, "/x", nil, nil, testOv)
	c.afterGrace()
	for range 8 {
		d.Update(key("down"))
	}
	g := draw(d, 80, 30)
	mustFind(t, g, "/dir08")
	if _, _, ok := find(g, "/dir00"); ok {
		t.Fatal("the first entry is still drawn after scrolling to the eighth")
	}
}

func TestOpenFolderClickSelectsRowThenEnterOpensIt(t *testing.T) {
	t.Parallel()
	c := newFakeClock()
	d := NewOpenFolder(c.Now, []string{"/a", "/b"}, "/x", nil, nil, testOv)
	g := draw(d, 80, 24)
	x, y := mustFind(t, g, "/b")
	next, cmd := d.Update(click(x, y))
	mustOpen(t, next, cmd)
	c.afterGrace()
	next, cmd = d.Update(key("enter"))
	if next != nil {
		t.Fatal("Enter after the click left the dialog open")
	}
	r, _ := answer(cmd)
	if r.Choice != ChoiceOpen || r.Text != "/b" {
		t.Fatalf("Enter after clicking /b answered %+v", r)
	}
}

func TestOpenFolderButtonsAndAltKeys(t *testing.T) {
	t.Parallel()
	c := newFakeClock()
	d := NewOpenFolder(c.Now, nil, "/x", nil, nil, testOv)
	c.afterGrace()
	_, cmd := d.Update(altKey('o'))
	mustClose(t, d, cmd, ChoiceOpen, "/x")

	e := NewOpenFolder(c.Now, nil, "/x", nil, nil, testOv)
	c.afterGrace()
	_, cmd = e.Update(altKey('c'))
	mustClose(t, e, cmd, ChoiceCancel, "")
}

func TestOpenFolderLetterTypesIntoField(t *testing.T) {
	t.Parallel()
	c := newFakeClock()
	d := NewOpenFolder(c.Now, nil, "/x", nil, nil, testOv)
	c.afterGrace()
	o := typeText(t, d, "o")
	if !o.Active() {
		t.Fatal("typing o closed the dialog")
	}
	_, cmd := o.Update(key("enter"))
	mustClose(t, d, cmd, ChoiceOpen, "/xo")
}

func TestOpenFolderTabOrderAndEsc(t *testing.T) {
	t.Parallel()
	c := newFakeClock()
	d := NewOpenFolder(c.Now, []string{"/a"}, "/x", nil, nil, testOv)
	c.afterGrace()
	// Stops: field, list, Open, Cancel. Four Tabs come back to the field.
	for range 4 {
		d.Update(key("tab"))
	}
	typeText(t, d, "y")
	_, cmd := d.Update(key("enter"))
	mustClose(t, d, cmd, ChoiceOpen, "/xy")

	e := NewOpenFolder(c.Now, []string{"/a"}, "/x", nil, nil, testOv)
	_, cmd = e.Update(key("esc"))
	mustClose(t, e, cmd, ChoiceCancel, "")
}

func TestOpenFolderCancelButtonClick(t *testing.T) {
	t.Parallel()
	c := newFakeClock()
	d := NewOpenFolder(c.Now, nil, "/x", nil, nil, testOv)
	g := draw(d, 80, 24)
	x, y := mustFind(t, g, "Cancel")
	_, cmd := d.Update(click(x, y))
	mustClose(t, d, cmd, ChoiceCancel, "")
}
