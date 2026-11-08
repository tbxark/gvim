package pluginhost

import (
	"strings"
	"testing"

	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/pluginapi"
)

// withSmallVirtualQuotas lowers the virtual-buffer quotas for one test, so that a few
// buffers fill them, and restores the defaults afterwards.
func withSmallVirtualQuotas(t *testing.T, perPlugin, all, bytesPerPlugin, allBytes int) {
	t.Helper()
	old := [...]int{pluginMaxVirtual, pluginMaxVirtualAll, pluginMaxVirtualBytes, pluginMaxVirtualBytesAll}
	pluginMaxVirtual, pluginMaxVirtualAll = perPlugin, all
	pluginMaxVirtualBytes, pluginMaxVirtualBytesAll = bytesPerPlugin, allBytes
	t.Cleanup(func() {
		pluginMaxVirtual, pluginMaxVirtualAll = old[0], old[1]
		pluginMaxVirtualBytes, pluginMaxVirtualBytesAll = old[2], old[3]
	})
}

// TestVirtualBufferBudgetBoundsAHog: a plugin that fills virtual buffers with text is
// stopped by the byte budget of its own, however often it runs again, and the budget is
// not shared with other plugins' buffers the way an editor-wide count would be (S18-1,
// S18-2).
func TestVirtualBufferBudgetBoundsAHog(t *testing.T) {
	withSmallVirtualQuotas(t, 64, 256, 4<<20, 64<<20)
	ed := editor.New(editor.Options{})
	pe := newPluginEditor(ed, func(*editor.Document) {})
	big := strings.Repeat("a", 1024*1024)
	hog := func() {
		for range 100 {
			id, err := pe.CreateBuffer(pluginapi.CreateBuffer{Name: "B", Plugin: "hog"})
			if err != nil {
				return
			}
			// The text is refused once the byte budget is full; the buffer stays empty.
			_ = pe.SetContent(id, []pluginapi.ContentEntry{{Text: big}})
		}
	}
	for run := range 3 {
		hog()
		n, bytes := ed.VirtualUsage("hog")
		if n != 64 || bytes != 4<<20 {
			t.Fatalf("after run %d the plugin holds %d buffers and %d bytes, want 64 and %d", run+1, n, bytes, 4<<20)
		}
	}
	// Another plugin still gets its buffer.
	id, err := pe.CreateBuffer(pluginapi.CreateBuffer{Name: "Other", Plugin: "outline", Text: "small"})
	_ = id
	if err != nil {
		t.Fatalf("another plugin's buffer was refused: %v", err)
	}
}
