package tui

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/tbxark/gvim/internal/tui/list"
)

// tourSlide is one slide of the built-in tour. Its lines are text about the screen. A
// {action} in a line stands for the shortcut of that action in the active keymap, or for
// its palette command when the keymap gives it none (see tourText), so the tour is right for
// every keymap. A line that grows past the box wraps.
type tourSlide struct {
	title string
	lines []string
}

// tourSlides are the slides of the tour, in order.
var tourSlides = []tourSlide{
	{"Welcome", []string{
		"gvim is a terminal editor. This tour shows the parts of the screen.",
		"Right or Enter steps forward, Left steps back, and Esc closes the tour.",
	}},
	{"Tabs and files", []string{
		"Each open file has a tab across the top. {new} starts a new file.",
		"{close_tab} closes the tab. {next_buffer} and {prev_buffer} move between tabs.",
		"{open} opens a file in the browser, and {save} saves the current one.",
	}},
	{"File explorer", []string{
		"{toggle_file_explorer} shows or hides the file explorer on the left.",
		"{focus_file_explorer} moves the focus to it. Enter opens the selected file.",
		"{file_explorer_rename} renames the selected entry, and {file_explorer_delete} moves it to the trash.",
	}},
	{"Quick open", []string{
		"{quick_open} opens the palette. Type a file name to open that file.",
		"Type > for commands, : for a line number, and # for an open tab.",
	}},
	{"Find and replace", []string{
		"{search} searches the current file, and {find_next} jumps to the next match.",
		"{replace} opens replace, and {goto_line} goes to a line.",
	}},
	{"Help and settings", []string{
		"{show_help} lists the keyboard shortcuts of the active keymap.",
		"The menus open with a click. Edit > Settings... lists the effective settings.",
		"Edit > Keybinding Editor... changes a shortcut and saves it to the config.",
	}},
}

// tourTextWidth is the widest a line of the tour is drawn: the box less its borders and padding.
const tourTextWidth = list.BoxMaxWidth - 4

// tourToken matches the {action} placeholders of a tour line.
var tourToken = regexp.MustCompile(`\{([a-z_]+)\}`)

// tourText writes a tour line for the active keymap: each {action} becomes the action's
// shortcut as every other label shows it, or, when the keymap binds it to nothing, the name
// of its palette command in quotes, so the line still tells the reader what to do.
func (m *Model) tourText(line string) string {
	describe := m.actionDescriber()
	return tourToken.ReplaceAllStringFunc(line, func(tok string) string {
		action := tok[1 : len(tok)-1]
		if key := m.shortcutOf(action); key != "" {
			return key
		}
		return `"` + describe(action) + `" in the palette`
	})
}

// tourFooter is the last row of the tour box.
const tourFooter = "Right or Enter: next   Left: back   Esc: close"

// tourState is the slide the tour shows.
type tourState struct{ slide int }

// step moves the tour by d slides, and stays on the first and last slides.
func (t *tourState) step(d int) {
	t.slide = min(max(t.slide+d, 0), len(tourSlides)-1)
}

// openTour shows the tour at slide. Enter steps forward, and Enter on the last slide
// closes the tour. Esc closes it too.
func (m *Model) openTour(slide int) {
	st := &tourState{slide: slide}
	v := list.NewTextBox(m.listChrome(), func() (string, []string, string) {
		s := tourSlides[st.slide]
		title := fmt.Sprintf("Tour: %s (%d of %d)", s.title, st.slide+1, len(tourSlides))
		var lines []string
		for _, l := range s.lines {
			// A shortcut can be long (a sequence) and a palette command longer, so a line
			// wraps to the box instead of losing its end.
			lines = append(lines, strings.Split(ansi.Wrap(m.tourText(l), tourTextWidth, ""), "\n")...)
		}
		return title, lines, tourFooter
	})
	v.Data = st
	v.OnKey = func(k tea.Key) bool {
		switch k.Keystroke() {
		case "enter":
			if st.slide == len(tourSlides)-1 {
				return true
			}
			st.step(1)
		case "right", "down", "pgdown", "space", "n", "ctrl+alt+n":
			st.step(1)
		case "left", "up", "pgup", "backspace", "p", "ctrl+alt+p":
			st.step(-1)
		}
		return false
	}
	m.pushOverlay(v)
}

// tourAction runs tour_next or tour_prev. With no tour open, either one opens the
// first slide. With a tour open, it steps that tour.
func (m *Model) tourAction(action string) {
	st := m.findTour()
	if st == nil {
		m.openTour(0)
		return
	}
	if action == "tour_next" {
		st.step(1)
		return
	}
	st.step(-1)
}

// findTour returns the state of the topmost tour on the overlay stack, or nil.
func (m *Model) findTour() *tourState {
	for _, v := range slices.Backward(m.overlays) {
		if o, ok := v.(*list.TextBox); ok {
			if st, ok := o.Data.(*tourState); ok {
				return st
			}
		}
	}
	return nil
}
