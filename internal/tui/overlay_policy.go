package tui

import (
	"slices"

	"github.com/tbxark/gvim/internal/tui/find"
	"github.com/tbxark/gvim/internal/tui/overlay"
	"github.com/tbxark/gvim/internal/tui/palette"
)

// placeOverlay applies the stack's one-at-a-time rule to an exclusive overlay and
// reports whether it placed o itself: true means o is in the stack, or it closed with
// the layers it was re-pushed over; false means the caller appends it as usual. An
// Exclusive overlay is a transient
// interaction that owns the keyboard alone (the palette, the find bar, the browser,
// a picker list), so it closes the Exclusive overlays below it. An overlay that is
// not Exclusive keeps the ordinary stack behaviour: dialogs, prompts, menus, popups,
// information boxes and the overlays plugins open may cover it, and closing them
// returns to the layer under them.
//
// An Alert owns the input until it is answered, so a new Exclusive overlay goes under
// the bottom-most Alert instead of on top of the stack: a request that arrives while
// a dialog or a prompt is up cannot take the keyboard from it. The alert stack and
// the overlay the alert covered keep their order, and the new overlay becomes the
// top ordinary layer once the alerts close.
func (m *Model) placeOverlay(o Overlay) bool {
	if _, ok := o.(overlay.Exclusive); !ok {
		return false
	}
	m.closeExclusiveOverlays()
	if !o.Active() {
		// Closing the exclusive overlays closed o too: o was already on the stack.
		// It left the stack with them, so there is nothing to insert or append.
		return true
	}
	if _, isFind := o.(*find.Bar); isFind {
		// The bar is being opened (or reopened), not replaced: it is not part of the
		// stack yet, so the walk above could not have closed it.
		return m.insertUnderAlerts(o)
	}
	// The find bar can be open without being on the stack: find_selection builds one
	// to hold its query. The stack walk cannot see it, so cancel it here as Esc would,
	// which also puts back the caret and selection it moved.
	if m.findBar != nil && m.findBar.Active() {
		m.findBar.Dismiss()
	}
	return m.insertUnderAlerts(o)
}

// insertUnderAlerts puts o under the bottom-most Alert, or reports false when no
// alert is open and the caller appends it to the top as usual. Going under the whole
// alert block keeps every alert's input: with two stacked alerts, closing the upper
// one reveals the lower one, then o, never o over the lower one.
func (m *Model) insertUnderAlerts(o Overlay) bool {
	for i, under := range m.overlays {
		if _, ok := under.(overlay.Alert); ok {
			m.overlays = slices.Insert(m.overlays, i, o)
			return true
		}
	}
	return false
}

// closeExclusiveOverlays closes every Exclusive overlay on the stack, whatever its
// depth, and leaves the stackable layers (the alerts and the widgets stacked over
// them) in place and in order. Each overlay runs its cancel path first, exactly as Esc
// does: Dismiss stops the palette's probe, search and preview, and the find bar's
// Dismiss puts the caret and selection back where they were before it moved them. The
// Live Grep controls are captured before the palette goes, so Resume still works. An
// alert keeps the input and its question; an alert under the closed layers stays.
func (m *Model) closeExclusiveOverlays() {
	for i := len(m.overlays) - 1; i >= 0; i-- {
		o := m.overlays[i]
		if _, ok := o.(overlay.Exclusive); !ok {
			continue
		}
		if d, ok := o.(interface{ Dismiss() }); ok {
			d.Dismiss()
		}
		m.captureLiveResume(o)
		m.overlays = slices.Delete(m.overlays, i, i+1)
	}
}

// captureLiveResume keeps a closing palette's Live Grep controls for the Resume
// command, as popOverlay does for the top overlay.
func (m *Model) captureLiveResume(o Overlay) {
	r, ok := o.(interface{ LiveResumeState() *palette.LiveResume })
	if !ok {
		return
	}
	if state := r.LiveResumeState(); state != nil {
		m.root.lastGrep = state
	}
}
