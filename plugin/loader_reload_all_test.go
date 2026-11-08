package plugin_test

import (
	"strings"
	"testing"

	"github.com/tbxark/gvim/pluginapi"
)

// Plugin: Reload All must not end with a plain "N plugin(s) loaded" when a reload aborted: the
// last thing the status row says is the count, the failure and where to read more.
func TestReloadAllReportsFailures(t *testing.T) {
	e := newEnv(t)
	e.user("a.lua", `T.command("v", function() T.spy("a1") end)`)
	e.user("b.lua", `T.command("v", function() T.spy("b1") end)`)
	e.start()

	e.user("b.lua", `T.command("v", function() T.spy("b2") end) +`)
	e.control("reload_all", "")
	n := e.h.Host.Notices
	last := n[len(n)-1]
	if last.Level == pluginapi.LevelInfo {
		t.Errorf("the last notice %q is only informational", last.Msg)
	}
	for _, want := range []string{"plugin 'b'", "reload aborted", "Show Log"} {
		if !strings.Contains(last.Msg, want) {
			t.Errorf("last notice = %q, want it to contain %q", last.Msg, want)
		}
	}
	if strings.Contains(last.Msg, "plugin(s) loaded") && !strings.Contains(last.Msg, "reload aborted") {
		t.Errorf("last notice = %q claims a clean reload", last.Msg)
	}

	// a clean reload still says so
	e.user("b.lua", `T.command("v", function() T.spy("b3") end)`)
	e.control("reload_all", "")
	n = e.h.Host.Notices
	if last := n[len(n)-1]; last.Level != pluginapi.LevelInfo || !strings.Contains(last.Msg, "plugin(s) loaded") {
		t.Errorf("a clean reload said %+v", last)
	}
}
