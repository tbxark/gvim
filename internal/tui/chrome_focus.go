package tui

// chromeFocus is the part of the frame that holds the keyboard, as Fresh's
// chrome_focus_changed reports it. gvim's tab strip has no Fresh counterpart and
// is reported as the extension region "tabs"; the native Contents and Diagnostics
// sidebar sections map to "section" with no plugin, like a plugin section that is
// not a mounted panel.
type chromeFocus struct {
	region  string
	plugin  string
	panelID uint64
}

// chromeFocusOf reports the region that takes keys now. Overlays and the menu own
// the keyboard on top of a chrome region; they are transient surfaces, not chrome,
// so they do not raise the event (Fresh's event is about the stable chrome).
func (m *Model) chromeFocusOf() chromeFocus {
	if ps := &m.plug.panels; ps.focus != 0 {
		if mp := ps.get(ps.focus); mp != nil {
			return chromeFocus{region: "section", plugin: mp.owner, panelID: uint64(mp.id)}
		}
	}
	switch {
	case m.dock.Focus():
		return chromeFocus{region: "dock"}
	case m.explorerFocus:
		return chromeFocus{region: "explorer"}
	case m.tabFocus:
		return chromeFocus{region: "tabs"}
	case m.contentsFocus, m.diag.focus:
		return chromeFocus{region: "section"}
	}
	return chromeFocus{region: "editor"}
}

// emitChromeFocusChanged queues chrome_focus_changed after the region with the
// keyboard changed. The stable regions are editor, explorer, dock and section;
// gvim's tab strip is the extension region "tabs". It is queued through the normal
// plugin event pump, so Update does no blocking IO.
func (m *Model) emitChromeFocusChanged(prev, cur chromeFocus) {
	if prev == cur || m.plug.rt == nil || !m.plug.rt.Wants("chrome_focus_changed") {
		return
	}
	data := map[string]any{"region": cur.region}
	if cur.plugin != "" {
		data["plugin"] = cur.plugin
	}
	if cur.panelID != 0 {
		data["panel_id"] = cur.panelID
	}
	m.plugEmit("chrome_focus_changed", data)
}
