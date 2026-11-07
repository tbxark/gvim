package app

import (
	"github.com/tbxark/gvim/internal/buffer"
	"github.com/tbxark/gvim/internal/decor"
	"github.com/tbxark/gvim/internal/editor"
	"github.com/tbxark/gvim/internal/pluginapi"
)

// Plugin decorations (stage S15, AMENDMENTS C13). The decoration store belongs to the
// root model, which paints it; the app joins it to the editor. The store is made here and
// the root reads it through DecorStore, so the one store that the editor's change listener
// moves is the one the painter reads.

// decorListenerKey names the editor's change listener that moves the decorations. It is
// the first listener the app registers, so the anchors have moved before any other
// listener, or the edit call, returns.
const decorListenerKey = "decor"

// trackDecorations makes the store and registers its listener on the editor. Tracking
// stays on for the life of the editor: the listener is a single map lookup for a document
// with no decoration.
func (p *pluginEditor) trackDecorations() {
	p.decor = decor.New()
	p.ed.AddChangeListener(decorListenerKey, func(doc editor.DocID, c buffer.Change) {
		applyChange(p.decor, uint64(doc), c)
	})
	p.ed.TrackChanges(decorListenerKey, true)
}

// DecorStore returns the store of the plugin decorations (ui's decorSource).
func (p *pluginEditor) DecorStore() *decor.Store { return p.decor }

// applyChange queues one change of a document's text for the decorations: the store moves
// them through the changes of one operation (Replace All) together, when it is next read. A
// change whose extent is not reported (a large file, a replaced buffer) drops them.
func applyChange(st *decor.Store, doc uint64, c buffer.Change) {
	if c.Coarse {
		st.Apply(doc, nil, true)
		return
	}
	st.Queue(doc, decor.Edit{
		FromLine: c.From.Line, FromCol: c.FromByte,
		ToLine: c.To.Line, ToCol: c.ToByte,
		Text: c.Text,
	})
}

// BufferID is the id the plugins know the document by (ui's bufferIdentified), which is
// what the decorations of the document are filed under.
func (d documentAdapter) BufferID() pluginapi.BufferID { return pluginapi.BufferID(d.doc.ID()) }
