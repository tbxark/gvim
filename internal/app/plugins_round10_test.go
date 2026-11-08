package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestRealHostProjectEnvironmentDetectionAndInjection is the end-to-end env smoke:
// a custom detector matches a marker at the workspace root (passive detection), a
// plugin activates it over gvim.env (trusted workspace), and a plugin-spawned
// process inherits the activated variable through the host spawn overlay.
func TestRealHostProjectEnvironmentDetectionAndInjection(t *testing.T) {
	isolateConfig(t)
	root := t.TempDir()
	must(t, os.WriteFile(filepath.Join(root, "env.flag"), nil, 0o644))
	writeUserFile(t, "config.json", `{"plugin_timeout_ms":5000,"env":{"detectors":[
	  {"name":"smoke","markers":["env.flag"],"kind":"path-only","snippet":"export GVIM_ENV_SMOKE=on"}
	]}}`)
	writeUserFile(t, "plugins/env_probe/init.lua", `local gvim = require "gvim"
local function log(msg) gvim.log.info("envprobe " .. msg) end
gvim.command.register{
  name = "probe",
  title = "Env Probe",
  run = function()
    local d = gvim.env.detected()
    log("detected=" .. tostring(d and d.name) .. " kind=" .. tostring(d and d.kind))
    log("trust=" .. gvim.env.trust_level())
    local ok, err = gvim.env.set("export GVIM_ENV_SMOKE=on", {kind = "path-only"})
    log("set=" .. tostring(ok) .. " err=" .. tostring(err))
    local a = gvim.env.active()
    log("active=" .. tostring(a and a.name))
  end,
}`)
	s := newSession(t, Options{Root: root})
	s.waitStatus("env_probe", "loaded")
	if err := s.m.Run("env_probe.probe"); err != nil {
		t.Fatal(err)
	}
	s.until("the env probe results", func() bool {
		for _, e := range s.plug.Log() {
			if e.Plugin == "env_probe" && strings.Contains(e.Msg, "envprobe active=smoke") {
				return true
			}
		}
		return false
	})
	var lines []string
	for _, e := range s.plug.Log() {
		if e.Plugin == "env_probe" && strings.HasPrefix(e.Msg, "envprobe ") {
			lines = append(lines, strings.TrimPrefix(e.Msg, "envprobe "))
		}
	}
	joined := strings.Join(lines, "\n")
	for _, want := range []string{
		"detected=smoke kind=path-only",
		"trust=trusted",
		"set=true err=nil",
		"active=smoke",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("probe log misses %q: %s", want, joined)
		}
	}
	// One spawn path in the real host: the editor's shell command job inherits the
	// activated environment (the plugin-proc path is covered by the merge unit test
	// and the env Provider tests).
	s.openFile("a.txt", "x\n")
	s.settle(50 * time.Millisecond)
	if res := s.rt.ed.Do("shell_command", map[string]any{"command": "printf %s \"$GVIM_ENV_SMOKE\""}); res.Err != nil {
		t.Fatalf("shell_command: %v", res.Err)
	}
	if got := s.rt.ed.Active().Buffer.Text(); got != "on" {
		t.Errorf("shell command output = %q, want the activated variable", got)
	}
}
