package plugin_test

import (
	"testing"

	"github.com/tbxark/gvim/internal/plugin"
	"github.com/tbxark/gvim/internal/pluginapi"
)

func TestEntryPointsContainPanics(t *testing.T) {
	// A bug in a closure the runtime runs (here a runtime-level wait handler) must not leave
	// the entry point: the ui goroutine would die with it.
	h := start(t, `T.command("go", function() T.spy("still runs") end)`)
	plugin.ExpectRuntime(h.RT, 77, func(pluginapi.Completion) { panic("handler bug") })
	h.RT.Resolve(pluginapi.Completion{Token: 77})
	h.Settle()
	if !logHas(h, "", "internal error: handler bug") {
		t.Fatalf("log = %v", h.LogText(""))
	}
	if h.RT.InSlice() || plugin.Depth(h.RT) != 0 {
		t.Fatal("depth not restored after a contained panic")
	}
	if err := h.Run("t.go", nil); err != nil || spyText(h) != "still runs" {
		t.Fatalf("runtime unusable afterwards: %v %q", err, spyText(h))
	}
}
