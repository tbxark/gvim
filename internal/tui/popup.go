package tui

import (
	"slices"

	"github.com/tbxark/gvim/internal/tui/popup"
	"github.com/tbxark/gvim/internal/tui/style"
)

// ShowPopup opens a popup at anchor with title and items on the overlay stack. The
// choice arrives as a popup.Chosen message. The root does nothing with it yet, so
// an owner that wants the choice handles that message in its own case.
func (m *Model) ShowPopup(anchor popup.Point, title string, items []popup.Item) {
	p := popup.New(popupStyles(m.styles))
	p.Show(anchor, title, items)
	m.pushOverlay(p)
}

// popupStyles are the popup's styles: the overlay palette, the same as the
// dialogs and the lists.
func popupStyles(st style.Styles) popup.Styles {
	ov := st.Overlay
	return popup.Styles{Panel: ov.Box, Border: ov.Border, Selected: ov.Selected, Detail: ov.Muted}
}

// popupFocus moves the topmost popup on the overlay stack to the top, so it takes
// the input next. It reports whether there was a popup.
func (m *Model) popupFocus() bool {
	for i, top := range slices.Backward(m.overlays) {
		if _, ok := top.(*popup.Popup); ok {

			m.overlays = append(slices.Delete(m.overlays, i, i+1), top)
			return true
		}
	}
	return false
}
