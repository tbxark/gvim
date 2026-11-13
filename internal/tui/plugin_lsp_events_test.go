package tui

import (
	"errors"
	"testing"

	"github.com/tbxark/gvim/pluginapi"
)

// TestLSPEventsReachPlugins checks the two language-server events the plugin host raises:
// the status pill click and a server error.
func TestLSPEventsReachPlugins(t *testing.T) {
	t.Parallel()
	ed := newFakeEditor("hello")
	m := newTestModel(t, ed, newFakeTree(3), 80, 24)
	rt := newFakeRuntime(t)
	rt.wants["lsp_status_clicked"] = true
	rt.wants["lsp_server_error"] = true
	m.plug = newPluginState(m, RootPorts{Plugins: rt, PluginEditor: &fakePluginEditor{active: 1, bufs: []pluginapi.BufferInfo{{ID: 1}}}})
	useHost(rt, m)
	// The click site: the show_lsp_status action. The model has no port, so the popup
	// command fails, but the event is queued before it.
	if _, ok := m.lspAction("show_lsp_status", nil); !ok {
		t.Fatal("show_lsp_status is not an LSP action")
	}
	m.pluginTail(pluginSnap{})
	m.pluginPump()
	// And the error site.
	m.lspMessage(lspErrMsg{err: errors.New("server exploded")})
	m.pluginPump()
	var clicked, errored bool
	for _, e := range rt.emitted {
		switch e.Name {
		case "lsp_status_clicked":
			clicked = true
		case "lsp_server_error":
			errored = e.Data["error"] == "server exploded"
		}
	}
	if !clicked {
		t.Fatalf("no lsp_status_clicked: %+v", rt.emitted)
	}
	if !errored {
		t.Fatalf("no lsp_server_error with the message: %+v", rt.emitted)
	}
}
