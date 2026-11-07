package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	expanel "github.com/tbxark/gvim/internal/ui/explorer"
)

// idlePlugin registers a command and listens to nothing.
const idlePlugin = `
local gvim = require "gvim"
gvim.command.register{ name = "noop", title = "Noop %d", run = function() end }
`

// busyPlugin listens to every hot event of the root with a handler that does nothing.
const busyPlugin = `
local gvim = require "gvim"
gvim.command.register{ name = "noop", title = "Noop %d", run = function() end }
for _, ev in ipairs{"cursor_moved", "selection_changed", "viewport_changed", "buffer_modified",
    "buffer_changed", "pre_command", "post_command"} do
  gvim.on(ev, function() end)
end
`

// TestHotEventCost measures what an Update costs with plugins off, with ten plugins that
// listen to nothing, and with ten plugins that listen to every hot event. It prints the
// numbers; it asserts nothing because wall time is not a stable thing to assert. Run it with
//
//	GVIM_MEASURE_EVENTS=1 go test ./internal/app -run TestHotEventCost -v
func TestHotEventCost(t *testing.T) {
	if os.Getenv("GVIM_MEASURE_EVENTS") == "" {
		t.Skip("set GVIM_MEASURE_EVENTS=1 to measure")
	}
	configs := []struct {
		name   string
		src    string
		plugin bool
	}{
		{"plugins off", "", false},
		{"10 idle plugins", idlePlugin, true},
		{"10 plugins on every hot event", busyPlugin, true},
	}
	ops := []struct {
		name string
		keys []tea.KeyPressMsg
	}{
		{"caret move (Right, Left)", []tea.KeyPressMsg{{Code: tea.KeyRight}, {Code: tea.KeyLeft}}},
		{"edit (type a, Backspace)", []tea.KeyPressMsg{{Code: 'a', Text: "a"}, {Code: tea.KeyBackspace}}},
	}
	for _, cfg := range configs {
		t.Run(cfg.name, func(t *testing.T) {
			isolateConfig(t)
			if cfg.plugin {
				for i := range 10 {
					writeUserFile(t, fmt.Sprintf("plugins/p%d.lua", i), fmt.Sprintf(cfg.src, i))
				}
			}
			s := newSession(t, Options{NoPlugins: !cfg.plugin})
			if cfg.plugin {
				s.waitStatus("p9", "loaded")
				s.send(tea.WindowSizeMsg{Width: 120, Height: 30})
			}
			path := filepath.Join(t.TempDir(), "big.txt")
			must(t, os.WriteFile(path, []byte(strings.Repeat("some text on a line\n", 500)), 0o644))
			s.send(expanel.OpenFileMsg{Path: path})
			s.settle(20 * time.Millisecond)
			for _, op := range ops {
				// The runtime must be called from this goroutine, so no testing.Benchmark.
				// The best of five batches is the least disturbed one.
				const batch = 400
				best := time.Duration(1 << 62)
				for range 5 {
					start := time.Now()
					for i := range batch {
						s.m.Update(op.keys[i%len(op.keys)])
					}
					best = min(best, time.Since(start))
				}
				t.Logf("%-32s %-30s %8d ns/Update", cfg.name, op.name, best.Nanoseconds()/batch)
			}
		})
	}
}
