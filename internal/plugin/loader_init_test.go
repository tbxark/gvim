package plugin_test

import (
	"testing"

	"github.com/tbxark/gvim/internal/pluginapi"
)

// TestInitCheckOfAnUnsavedBuffer: init_check with text checks that text (the dirty buffer of
// init.lua) and not the file on disk.
func TestInitCheckOfAnUnsavedBuffer(t *testing.T) {
	e := newEnv(t)
	e.start()
	e.disk.put(initFile, `T.spy("fine")`)

	n := e.h.Control(pluginapi.Control{Op: "init_check", Text: "T.spy('x')\nlocal x = = 1"})
	if !e.noticeHas("init.lua (unsaved buffer): syntax error near '=' (init/init.lua:2:11)") {
		t.Fatalf("a broken buffer over a good file: %v / %v", n, e.notices())
	}
	n = e.h.Control(pluginapi.Control{Op: "init_check", Text: "T.spy('x')"})
	if !e.noticeHas("init.lua (unsaved buffer): syntax OK") {
		t.Fatalf("a good buffer: %v / %v", n, e.notices())
	}
}
