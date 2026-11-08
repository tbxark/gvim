package plugin_test

import (
	"strings"
	"testing"
	"time"

	"github.com/tbxark/gvim/plugin"
	"github.com/tbxark/gvim/plugin/plugintest"
)

// start builds a harness around one single-file plugin "t" with the given main chunk and
// starts it. The chunk can use the T table of export_test.go.
func start(t *testing.T, main string, o ...plugintest.Options) *plugintest.Harness {
	t.Helper()
	return startFiles(t, plugintest.Files{"t.lua": main}, o...)
}

func startFiles(t *testing.T, files plugintest.Files, o ...plugintest.Options) *plugintest.Harness {
	t.Helper()
	var opts plugintest.Options
	if len(o) > 0 {
		opts = o[0]
	}
	if opts.Timeout == 0 {
		// Generous: many rows build megabyte strings or run the interpreter hard, and a machine
		// that is busy with other work must not turn them into timeouts. A test of the time
		// budget itself passes its own Timeout.
		opts.Timeout = 2 * time.Second
	}
	h := plugintest.New(t, files, opts)
	h.Start()
	return h
}

// spy returns what the plugins wrote with T.spy.
func spy(h *plugintest.Harness) []string { return plugin.Spy(h.RT) }

// spyText joins the spy lines with "|".
func spyText(h *plugintest.Harness) string { return strings.Join(spy(h), "|") }

// status returns the manager status of a plugin ("" if unknown).
func status(h *plugintest.Harness, name string) string {
	p, ok := h.Plugin(name)
	if !ok {
		return ""
	}
	return p.Status
}

// logHas reports whether some log line of the plugin contains sub.
func logHas(h *plugintest.Harness, name, sub string) bool {
	for _, l := range h.LogText(name) {
		if strings.Contains(l, sub) {
			return true
		}
	}
	return false
}

// noticeHas reports whether some notice contains sub.
func noticeHas(h *plugintest.Harness, sub string) bool {
	for _, n := range h.NoticeTexts() {
		if strings.Contains(n, sub) {
			return true
		}
	}
	return false
}
