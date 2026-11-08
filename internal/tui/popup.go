package tui

import (
	"slices"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/tui/grid"
	"github.com/tbxark/gvim/internal/tui/popup"
	"github.com/tbxark/gvim/internal/tui/style"
)

// popupOverlay adapts *popup.Popup to Overlay. The popup closes itself when a row is
// chosen or a click falls outside it, and the adapter reports that to the stack.
type popupOverlay struct{ p *popup.Popup }

// Update forwards the message to the popup. It returns nil once the popup is closed,
// so the stack pops it.
func (o popupOverlay) Update(msg tea.Msg) (Overlay, tea.Cmd) {
	cmd := o.p.Update(msg)
	if !o.p.Active() {
		return nil, cmd
	}
	return o, cmd
}

// Draw paints the popup over the frame.
func (o popupOverlay) Draw(g *grid.Grid) { o.p.Draw(g) }

// Active reports whether the popup is still open.
func (o popupOverlay) Active() bool { return o.p.Active() }

// ShowPopup opens a popup at anchor with title and items on the overlay stack. The
// choice arrives as a popup.Chosen message. The root does nothing with it yet, so
// an owner that wants the choice handles that message in its own case.
func (m *Model) ShowPopup(anchor popup.Point, title string, items []popup.Item) {
	p := popup.New(popupStyles(m.styles))
	p.Show(anchor, title, items)
	m.pushOverlay(popupOverlay{p: p})
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
		if _, ok := top.(popupOverlay); ok {

			m.overlays = append(slices.Delete(m.overlays, i, i+1), top)
			return true
		}
	}
	return false
}
