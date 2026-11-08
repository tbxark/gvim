package ui

import (
	"slices"
	"strings"
	"testing"

	"github.com/tbxark/gvim/internal/pluginapi"
)

// The label of a modal plugin (vi's "-- NORMAL --") is the only sign of the state, so a narrow
// row gives up the right group and " Local " before it cuts the label, and a message does
// not shorten it.
func TestStatusModeLabelIsNotCut(t *testing.T) {
	tests := []struct {
		name    string
		w       int
		label   string
		message string
	}{
		{"60 columns, no message", 60, "-- NORMAL --", ""},
		{"60 columns, visual", 60, "-- VISUAL LINE --", ""},
		{"80 columns, a notice", 80, "-- NORMAL --", "Opened t.txt"},
		{"80 columns, a pending command", 80, "-- NORMAL -- 2d", "no registers"},
		{"50 columns, a buffer-local hint", 50, "-- LIST -- ctrl+^ leaves", ""},
		{"40 columns", 40, "-- INSERT --", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sv := statusView{
				position: "Ln 1, Col 1", message: tt.message, eol: "LF", encoding: "UTF-8", language: "Text", lines: 10,
				extra: []statusItem{{id: "vi", plugin: "vi_mode", text: tt.label}},
			}
			var row strings.Builder
			for _, r := range statusRuns(tt.w, sv) {
				row.WriteString(r.text)
			}
			if !strings.Contains(row.String(), tt.label) {
				t.Errorf("row %q lost the label %q", row.String(), tt.label)
			}
		})
	}
}

// A long notice of a plugin keeps its informative head: the model holds the whole text, the
// status band wraps it from the start, and Show Warnings keeps all of it.
func TestLongPluginNoticeKeepsItsHead(t *testing.T) {
	e := newKeyEnv(t)
	head := "t/init.lua:12: attempt to call a nil value (field 'x')"
	msg := head + " " + strings.Repeat("and then some detail of the failure ", 12)
	e.m.plug.host.Notify("t", pluginapi.LevelError, msg)
	if want := "t: " + msg; e.m.notice != want {
		t.Fatalf("notice = %q, want the whole text with the plugin name in front", e.m.notice)
	}
	l := computeLayout(80, 30, noticeInput(e.m.notice))
	if len(l.notice) == 0 || !strings.HasPrefix(l.notice[0], "t: "+head) {
		t.Fatalf("band = %q, want it to start with the head", l.notice)
	}
	if !slices.Contains(e.m.notes.items, e.m.notice) {
		t.Errorf("Show Warnings does not keep the whole notice")
	}
}
