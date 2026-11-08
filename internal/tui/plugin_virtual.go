package tui

import "github.com/tbxark/gvim/pluginapi"

// Virtual buffers (stage S18, FINAL X10): the scratch tabs a plugin owns. The editor port
// makes and fills them (CreateBuffer, SetContent, ...); this file keeps who owns which, so
// that unloading a plugin closes the virtual buffers it made, and tells the language-server
// sync to leave them alone.

// pluginVirtual records the owner of each virtual buffer a plugin made.
type pluginVirtual struct {
	owner map[pluginapi.BufferID]string
}

// virtualDoc is implemented by a Document that can be a virtual buffer.
type virtualDoc interface {
	Virtual() bool
}

// isVirtualDoc reports whether doc is a virtual buffer: not a file, so it is never sent to
// a language server.
func isVirtualDoc(doc Document) bool {
	v, ok := doc.(virtualDoc)
	return ok && v.Virtual()
}

// CreateBuffer makes a virtual buffer for o.Plugin, and gives the keyboard to the editor
// when it takes the focus.
func (h *pluginHost) CreateBuffer(o pluginapi.CreateBuffer) (pluginapi.BufferID, error) {
	m := h.model()
	id, err := h.ed().CreateBuffer(o)
	if err != nil {
		return id, err
	}
	local, err := m.pluginResolveBuffer(id)
	if err != nil {
		return id, err
	}
	pv := &m.plug.virtual
	for old := range pv.owner {
		if _, ok := h.ed().Buffer(m.pluginGlobalBuffer(old)); !ok {
			delete(pv.owner, old) // the user closed it; ids are never reused
		}
	}
	if pv.owner == nil {
		pv.owner = make(map[pluginapi.BufferID]string)
	}
	pv.owner[local] = o.Plugin
	if o.Focus {
		m.focusEditor()
	}
	m.syncViewport()
	return id, nil
}

// plugVirtualGone closes the virtual buffers that the plugin (at generation gen) made, as
// the tab's close does, and without asking: a virtual buffer is never modified. A reload
// starts a new generation, which makes its buffers again when it needs them.
func (m *Model) plugVirtualGone(plugin string, gen uint64) {
	_ = gen
	var ids []pluginapi.BufferID
	for id, owner := range m.plug.virtual.owner {
		if owner == plugin {
			ids = append(ids, id)
		}
	}
	for _, id := range ids {
		delete(m.plug.virtual.owner, id)
		global := m.pluginGlobalBuffer(id)
		if _, ok := m.plug.host.ed().Buffer(global); ok {
			_ = m.plug.host.Close(global, true)
		}
	}
}
