package ui

import (
	"fmt"
	"slices"

	tea "charm.land/bubbletea/v2"
)

// tourSlide is one slide of the built-in tour. Its lines are static text about the
// screen, and each line fits an 80 column frame.
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
		"Each open file has a tab across the top. Ctrl+N starts a new file.",
		"Alt+W closes the tab. Ctrl+PageUp and Ctrl+PageDown move between tabs.",
		"Ctrl+O opens a file in the browser, and Ctrl+S saves the current one.",
	}},
	{"File explorer", []string{
		"Ctrl+B shows or hides the file explorer on the left.",
		"Ctrl+E moves the focus to it. Enter opens the selected file.",
		"F2 renames the selected entry, and Delete moves it to the trash.",
	}},
	{"Quick open", []string{
		"Ctrl+P opens the palette. Type a file name to open that file.",
		"Type > for commands, : for a line number, and # for an open tab.",
	}},
	{"Find and replace", []string{
		"Ctrl+F searches the current file, and F3 jumps to the next match.",
		"Ctrl+R opens replace. Alt+A starts a search and replace.",
	}},
	{"Help and settings", []string{
		"F1 lists the keyboard shortcuts of the active keymap.",
		"F10 opens the menu bar. Edit > Settings... lists the effective settings.",
		"Ctrl+Alt+N and Ctrl+Alt+P step through this tour from the editor.",
	}},
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
	v := &boxView{tour: st, active: true, chrome: m.listChrome()}
	v.content = func() (string, []string, string) {
		s := tourSlides[st.slide]
		title := fmt.Sprintf("Tour: %s (%d of %d)", s.title, st.slide+1, len(tourSlides))
		return title, s.lines, tourFooter
	}
	v.onKey = func(k tea.Key) bool {
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
	m.pushOverlay(boxOverlay{v: v})
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
		if o, ok := v.(boxOverlay); ok && o.v.tour != nil {
			return o.v.tour
		}
	}
	return nil
}
