package plugin_test

import (
	"strings"
	"testing"

	"github.com/tbxark/gvim/plugin"
)

// spyCount counts how often the spy recorded a line.
func spyCount(e *env, sub string) int {
	n := 0
	for _, line := range e.spyLines() {
		if strings.Contains(line, sub) {
			n++
		}
	}
	return n
}

// TestApplyWorkspacePluginConfigTouchesOnlyAffectedPlugins: a workspace's project plugin
// config disables one plugin and leaves the others running; enabling it again reloads only
// it; a setting change reloads only that plugin; no editor-wide plugin-state toggle is
// written for the project layer.
func TestApplyWorkspacePluginConfigTouchesOnlyAffectedPlugins(t *testing.T) {
	e := newEnv(t)
	e.bundle("alpha", map[string]string{"init.lua": `T.spy("load alpha"); T.command("alpha.run", function() end)`})
	e.bundle("beta", map[string]string{"init.lua": `T.spy("load beta"); T.command("beta.run", function() end)`})
	e.start()
	before := e.os.writeCount()
	if e.info("alpha").Status != "loaded" || e.info("beta").Status != "loaded" {
		t.Fatalf("initial rows: %v", e.names())
	}
	loadsAlpha, loadsBeta := spyCount(e, "load alpha"), spyCount(e, "load beta")

	// Workspace A's project config disables alpha.
	work := e.h.RT.ApplyWorkspacePluginConfig(map[string]bool{"alpha": false}, nil)
	e.h.RunWork(work)
	e.h.Settle()
	if p := e.info("alpha"); p.Status != "disabled" || p.Enabled {
		t.Fatalf("alpha after workspace A config = %+v", p)
	}
	if p := e.info("beta"); p.Status != "loaded" || !p.Enabled {
		t.Fatalf("beta was stopped by alpha's config: %+v", p)
	}
	if got := spyCount(e, "load beta"); got != loadsBeta {
		t.Fatalf("beta was reloaded (%d loads, was %d)", got, loadsBeta)
	}
	if e.os.writeCount() != before {
		t.Fatal("applying a workspace project config wrote the editor-wide plugin-state toggles")
	}

	// Workspace B does not disable alpha: it loads again, beta stays untouched.
	work = e.h.RT.ApplyWorkspacePluginConfig(nil, nil)
	e.h.RunWork(work)
	e.h.Settle()
	if p := e.info("alpha"); p.Status != "loaded" || !p.Enabled {
		t.Fatalf("alpha after workspace B config = %+v", p)
	}
	if got := spyCount(e, "load alpha"); got != loadsAlpha+1 {
		t.Fatalf("alpha loads = %d, want one reload", got)
	}
	if got := spyCount(e, "load beta"); got != loadsBeta {
		t.Fatalf("beta was reloaded by a config it is not in (%d loads)", got)
	}
	if e.os.writeCount() != before {
		t.Fatal("re-enabling alpha via a project config wrote the editor-wide plugin-state toggles")
	}

	// A setting change reloads only alpha.
	loadsAlpha = spyCount(e, "load alpha")
	work = e.h.RT.ApplyWorkspacePluginConfig(nil, map[string]plugin.PluginSettings{
		"alpha": {Project: map[string]any{"x": float64(1)}},
	})
	e.h.RunWork(work)
	e.h.Settle()
	if got := spyCount(e, "load alpha"); got != loadsAlpha+1 {
		t.Fatalf("alpha loads after a settings change = %d, want one reload", got)
	}
	if got := spyCount(e, "load beta"); got != loadsBeta {
		t.Fatalf("beta was reloaded by alpha's settings change (%d loads)", got)
	}
	if !e.noticeHas("workspace plugin config") {
		t.Fatalf("notices = %v", e.notices())
	}
	if e.os.writeCount() != before {
		t.Fatal("a project settings change wrote the editor-wide plugin-state toggles")
	}
}
