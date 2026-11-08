package plugin_test

import (
	"testing"
	"time"

	"github.com/tbxark/gvim/plugin/plugintest"
	"github.com/tbxark/gvim/pluginapi"
)

// TestEnvAPI covers the plugin-visible gvim.env surface: detection, active
// state, trust level, activation and deactivation through the host.
func TestEnvAPI(t *testing.T) {
	host := plugintest.NewFakeHost()
	host.HasEnvDetected = true
	host.EnvDetected = pluginapi.EnvInfo{Name: ".venv", Kind: "path-only", Snippet: "source .venv/bin/activate"}
	host.HasEnvActive = true
	host.EnvActiveInfo = pluginapi.EnvInfo{Name: ".venv", Kind: "path-only", Snippet: "source .venv/bin/activate"}
	host.EnvTrust = "trusted"
	h := plugintest.New(t, plugintest.Files{"t.lua": `local gvim = require "gvim"
local d = gvim.env.detected()
local a = gvim.env.active()
T.spy(d.name, d.kind, a.name, tostring(a.kind == "path-only"))
T.spy(gvim.env.trust_level())
local ok, err = gvim.env.set("source .venv/bin/activate", {kind = "path-only", dir = "/w"})
T.spy(tostring(ok), tostring(err))
T.spy(tostring(gvim.env.clear()))
`}, plugintest.Options{Host: host, Timeout: time.Second})
	h.Start()
	if got, want := spyText(h), ".venv path-only .venv true|trusted|true nil|true"; got != want {
		t.Fatalf("spy = %q log = %v", got, h.LogText(""))
	}
	if len(host.EnvSets) != 1 || host.EnvSets[0].Snippet != "source .venv/bin/activate" || host.EnvSets[0].Kind != "path-only" || host.EnvSets[0].Dir != "/w" {
		t.Fatalf("SetEnv calls = %+v", host.EnvSets)
	}
	if host.EnvClears != 1 {
		t.Fatalf("ClearEnv calls = %d, want 1", host.EnvClears)
	}
}

// TestEnvAPIWithoutAProvider: the embedded Unsupported host answers "unsupported"
// instead of lying about an environment.
func TestEnvAPIWithoutAProvider(t *testing.T) {
	h := startWith(t, plugintest.Files{"t.lua": `local gvim = require "gvim"
local function code(v, e) return tostring(v) .. " " .. tostring(gvim.err_code(e)) end
T.spy(tostring(gvim.env.detected()), tostring(gvim.env.active()))
T.spy(code(gvim.env.set("x")))
T.spy(code(gvim.env.clear()))
`}, pluginapi.Unsupported{}, plugintest.Options{})
	if got, want := spyText(h), "nil nil|nil unsupported|nil unsupported"; got != want {
		t.Fatalf("spy = %q log = %v", got, h.LogText(""))
	}
}
