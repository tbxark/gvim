package dialog

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/ui/grid"
)

// OpenFolder asks for a folder to open as the explorer root. The dialog has a
// path field, a list of recent folders, and the buttons Open and Cancel. Enter
// on the field opens the typed path. Up and Down move into the list, and Enter
// there opens the selected folder. A rejected path (see the validate function)
// shows its message inline and the dialog stays open.
type OpenFolder struct {
	modal
	input    *field
	validate func(path string) error
}

var _ Overlay = (*OpenFolder)(nil)

// NewOpenFolder returns the dialog with prefill in the path field and recent in
// the list, most recent first. Tag comes back in the Result. validate is
// optional. It answers why a path cannot be opened.
func NewOpenFolder(clock Clock, recent []string, prefill string, tag any, validate func(path string) error) *OpenFolder {
	d := &OpenFolder{input: newField(prefill), validate: validate}
	d.setup(clock, "Open Folder", tag, ChoiceCancel, []button{
		newButton("Open", 0, ChoiceOpen),
		newButton("Cancel", 0, ChoiceCancel),
	})
	d.field = d.input
	d.list = newRecentList(recent)
	d.onPress = d.press
	d.onPick = d.pick
	return d
}

// Update handles one message. See Overlay.
func (d *OpenFolder) Update(msg tea.Msg) (Overlay, tea.Cmd) {
	return d.advance(d, msg)
}

// Draw paints the dialog. See Overlay.
func (d *OpenFolder) Draw(g *grid.Grid) {
	d.frame(g, d.natural(d.list.items...), func(cw int) []row {
		rows := []row{d.fieldRow()}
		if d.errText != "" {
			rows = append(rows, textRows(d.errText, cw, stErr)...)
		}
		rows = append(rows, textRow("", stText))
		return append(rows, d.listRows()...)
	})
}

// press activates button i. Open opens the typed path and Cancel closes.
func (d *OpenFolder) press(i int) tea.Cmd {
	if d.buttons[i].choice == ChoiceCancel {
		return d.finish(ChoiceCancel, "")
	}
	return d.open(strings.TrimSpace(d.input.value()))
}

// pick opens the selected recent folder.
func (d *OpenFolder) pick() tea.Cmd {
	if len(d.list.items) == 0 {
		return nil
	}
	return d.open(d.list.selected())
}

// open closes the dialog with path, unless path is empty or rejected.
func (d *OpenFolder) open(path string) tea.Cmd {
	if path == "" {
		d.errText = "Enter a folder path"
		return nil
	}
	if d.validate != nil {
		if err := d.validate(path); err != nil {
			d.errText = err.Error()
			return nil
		}
	}
	return d.finish(ChoiceOpen, path)
}
