package uiadapter

import (
	"github.com/tbxark/gvim/internal/highlight"
	"testing"

	"github.com/tbxark/gvim/editor"
)

// TestDocumentAdapterReportsVirtual checks that the document adapter reports the virtual
// buffers of plugins to the root, which leaves them out of the language-server sync.
func TestDocumentAdapterReportsVirtual(t *testing.T) {
	ed := editor.New(editor.Options{})
	ed.Do("new", nil)
	id := ed.NewVirtual(editor.VirtualOptions{Name: "v"})
	a := &Editor{ed: ed, spans: highlight.New(), others: map[*editor.Document]*highlight.Cache{}}
	for i := range a.ed.Tabs() {
		doc, ok := a.DocAt(i).(documentAdapter)
		if !ok {
			t.Fatalf("DocAt(%d) is not a documentAdapter", i)
		}
		want := a.ed.DocAt(i).ID() == id
		if doc.Virtual() != want {
			t.Errorf("tab %d: Virtual() = %v, want %v", i, doc.Virtual(), want)
		}
	}
}
