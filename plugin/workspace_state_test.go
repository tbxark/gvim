package plugin_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/tbxark/gvim/plugin"
	"github.com/tbxark/gvim/plugin/plugintest"
)

// startWorkspace runs one plugin harness against a workspace-scoped gvim.state file.
func startWorkspace(t *testing.T, fos *plugintest.FakeOS, path string, data []byte, main string) *plugintest.Harness {
	t.Helper()
	return start(t, `local gvim = require "gvim"
`+main, plugintest.Options{OS: fos, WorkspaceStateFile: path, WorkspaceStateData: data})
}

// workspaceValue reads one plugin's value from a workspace state file.
func workspaceValue(t *testing.T, fos *plugintest.FakeOS, path, pluginName, key string) (any, bool) {
	t.Helper()
	data, ok := fos.File(path)
	if !ok {
		return nil, false
	}
	st, readOnly, err := plugin.ParseWorkspaceState([]byte(data))
	if err != nil || readOnly || st == nil {
		t.Fatalf("workspace state %s: st=%+v readOnly=%v err=%v", path, st, readOnly, err)
	}
	raw, ok := st.Plugins[pluginName][key]
	if !ok {
		return nil, false
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	return v, true
}

const workspaceStatePath = "/sessions/aaaa/plugin-state.json"

// TestWorkspaceStateIsPerRuntime: two runtimes with different workspace files keep their
// gvim.state independent, write ONE versioned workspace file each, and never fall back to
// the editor-wide per-plugin files.
func TestWorkspaceStateIsPerRuntime(t *testing.T) {
	pathA := workspaceStatePath
	pathB := "/sessions/bbbb/plugin-state.json"
	fosA := plugintest.NewFakeOS()
	hA := startWorkspace(t, fosA, pathA, nil, `T.command("set", function() gvim.state.set("k", "A") end)`)
	if err := hA.Run("t.set", nil); err != nil {
		t.Fatal(err)
	}
	hA.Advance(500 * time.Millisecond)
	if v, ok := workspaceValue(t, fosA, pathA, "t", "k"); !ok || v != "A" {
		t.Fatalf("workspace A value = %v, %v", v, ok)
	}
	if fosA.Exists("/home/u/.config/gvim/plugin-data/t/state.json") {
		t.Fatal("workspace mode wrote the editor-wide per-plugin state file")
	}

	fosB := plugintest.NewFakeOS()
	hB := startWorkspace(t, fosB, pathB, nil, `T.command("get", function() T.spy(tostring(gvim.state.get("k"))) end)
T.command("set", function() gvim.state.set("k", "B") end)`)
	if err := hB.Run("t.get", nil); err != nil {
		t.Fatal(err)
	}
	if got := spyText(hB); got != "nil" {
		t.Fatalf("workspace B saw another workspace's state: %q", got)
	}
	if err := hB.Run("t.set", nil); err != nil {
		t.Fatal(err)
	}
	hB.RT.Stop()
	if v, ok := workspaceValue(t, fosB, pathB, "t", "k"); !ok || v != "B" {
		t.Fatalf("workspace B value = %v, %v", v, ok)
	}
	// A's file is untouched by B.
	if v, ok := workspaceValue(t, fosA, pathA, "t", "k"); !ok || v != "A" {
		t.Fatalf("workspace A changed after B: %v, %v", v, ok)
	}
}

// TestWorkspaceStateRestoredOnMaterialize: a new runtime seeded with a workspace's file
// brings the state back (the daemon restart/materialize path).
func TestWorkspaceStateRestoredOnMaterialize(t *testing.T) {
	fos := plugintest.NewFakeOS()
	h := startWorkspace(t, fos, workspaceStatePath, nil, `T.command("set", function() gvim.state.set("k", {n = 42}) end)`)
	if err := h.Run("t.set", nil); err != nil {
		t.Fatal(err)
	}
	h.RT.Stop()
	data, ok := fos.File(workspaceStatePath)
	if !ok {
		t.Fatal("no workspace state file after Stop")
	}
	fos2 := plugintest.NewFakeOS()
	h2 := startWorkspace(t, fos2, workspaceStatePath, []byte(data), `T.command("get", function() local v = gvim.state.get("k") T.spy(v and v.n) end)`)
	if err := h2.Run("t.get", nil); err != nil {
		t.Fatal(err)
	}
	if got := spyText(h2); got != "42" {
		t.Fatalf("restored value = %q, want 42", got)
	}
}

// TestWorkspaceStateVersionGuard: a file from a newer gvim is never overwritten; writes
// report the read-only state and the file stays byte-identical.
func TestWorkspaceStateVersionGuard(t *testing.T) {
	fos := plugintest.NewFakeOS()
	newer := `{"version": 2, "plugins": {"t": {"k": "future"}}}`
	fos.SetFile(workspaceStatePath, newer)
	data, _ := fos.File(workspaceStatePath)
	h := startWorkspace(t, fos, workspaceStatePath, []byte(data), `
T.command("get", function() T.spy(tostring(gvim.state.get("k"))) end)
T.command("set", function() local v, e = gvim.state.set("k", "mine") T.spy(tostring(v), gvim.err_code(e)) end)`)
	if err := h.Run("t.get", nil); err != nil {
		t.Fatal(err)
	}
	if got := spyText(h); got != "nil" {
		t.Fatalf("newer state was loaded: %q", got)
	}
	if err := h.Run("t.set", nil); err != nil {
		t.Fatal(err)
	}
	if got := spyText(h); got != "nil|nil invalid" {
		t.Fatalf("set on a read-only workspace state = %q, want nil invalid", got)
	}
	h.Advance(500 * time.Millisecond)
	h.RT.Stop()
	after, _ := fos.File(workspaceStatePath)
	if after != newer {
		t.Fatalf("the newer workspace state file was overwritten:\n%s", after)
	}
}

// TestWorkspaceStateSizeBound: the per-plugin quota still applies in workspace mode (a
// write over the cap is refused, not silently truncated).
func TestWorkspaceStateSizeBound(t *testing.T) {
	fos := plugintest.NewFakeOS()
	h := startWorkspace(t, fos, workspaceStatePath, nil, `
T.command("set2", function()
  gvim.state.set("a", string.rep("x", 600000))
  local v, e = gvim.state.set("b", string.rep("y", 600000))
  T.spy(tostring(v), gvim.err_code(e))
end)`)
	if err := h.Run("t.set2", nil); err != nil {
		t.Fatal(err)
	}
	if got := spyText(h); got != "nil limit" {
		t.Fatalf("over-quota set = %q, want nil limit", got)
	}
}

// TestWorkspaceStateNoLeakAfterCloseAndDirectModeUnchanged: stopping one workspace runtime
// never exposes its state to another file, and a direct-mode runtime (no workspace file)
// still writes the editor-wide per-plugin file.
func TestWorkspaceStateNoLeakAfterCloseAndDirectModeUnchanged(t *testing.T) {
	fos := plugintest.NewFakeOS()
	h := startWorkspace(t, fos, workspaceStatePath, nil, `T.command("set", function() gvim.state.set("secret", "s") end)`)
	if err := h.Run("t.set", nil); err != nil {
		t.Fatal(err)
	}
	h.RT.Stop()

	fosOther := plugintest.NewFakeOS()
	hOther := startWorkspace(t, fosOther, "/sessions/cccc/plugin-state.json", nil,
		`T.command("get", function() T.spy(tostring(gvim.state.get("secret"))) end)`)
	if err := hOther.Run("t.get", nil); err != nil {
		t.Fatal(err)
	}
	if got := spyText(hOther); got != "nil" {
		t.Fatalf("state leaked to another workspace: %q", got)
	}

	// Direct mode keeps the editor-wide per-plugin file.
	fosDirect := plugintest.NewFakeOS()
	hDirect := start(t, `local gvim = require "gvim"
T.command("set", function() gvim.state.set("k", "direct") end)`, plugintest.Options{OS: fosDirect})
	if err := hDirect.Run("t.set", nil); err != nil {
		t.Fatal(err)
	}
	hDirect.Advance(500 * time.Millisecond)
	direct, ok := fosDirect.File("/home/u/.config/gvim/plugin-data/t/state.json")
	if !ok || !strings.Contains(direct, "\"k\":\"direct\"") {
		t.Fatalf("direct-mode state file = %q, %v", direct, ok)
	}
	if hDirect.RT.WorkspaceStatePath() != "" {
		t.Fatalf("direct runtime reports a workspace state file")
	}
}

// TestWorkspaceStateSwitchFlushesAndSeeds: switching namespaces on one runtime writes the
// left namespace to its own file and makes the next namespace's seed the active store.
func TestWorkspaceStateSwitchFlushesAndSeeds(t *testing.T) {
	fos := plugintest.NewFakeOS()
	pathA := "/sessions/aaaa/plugin-state.json"
	pathB := "/sessions/bbbb/plugin-state.json"
	h := startWorkspace(t, fos, pathA, nil, `
T.command("set", function() gvim.state.set("k", "A") end)
T.command("setB", function() gvim.state.set("k", "B") end)
T.command("get", function() T.spy(tostring(gvim.state.get("k"))) end)`)
	if err := h.Run("t.set", nil); err != nil {
		t.Fatal(err)
	}
	// Seed B's namespace with a different value and switch to it.
	stB, _, err := plugin.ParseWorkspaceState([]byte(`{"version":1,"plugins":{"t":{"k":"from-B"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	h.RT.SwitchWorkspaceState(pathB, stB, false)
	if err := h.Run("t.get", nil); err != nil {
		t.Fatal(err)
	}
	if got := spyText(h); !strings.HasSuffix(got, "from-B") {
		t.Fatalf("after switch get = %q, want the B namespace value", got)
	}
	if err := h.Run("t.setB", nil); err != nil {
		t.Fatal(err)
	}
	h.RT.Stop()
	// A was flushed to A's file and keeps its value; B holds what was written there.
	if v, ok := workspaceValue(t, fos, pathA, "t", "k"); !ok || v != "A" {
		t.Fatalf("A namespace after switch = %v, %v", v, ok)
	}
	if v, ok := workspaceValue(t, fos, pathB, "t", "k"); !ok || v != "B" {
		t.Fatalf("B namespace after switch = %v, %v", v, ok)
	}
}
