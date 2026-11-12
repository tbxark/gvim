package tui

import (
	"strings"
	"testing"

	"github.com/tbxark/gvim/i18n"
	"github.com/tbxark/gvim/pluginapi"
)

// fakeEnvPort is the cached EnvPort view for the status segment tests. directReads counts
// calls to the plugin-facing DetectedEnv/ActiveEnv methods (which may trigger the watcher
// retry recovery); the render path must use CachedEnvStatus only. mutations counts env
// actions; the indicator must never call them.
type fakeEnvPort struct {
	detected, active       pluginapi.EnvInfo
	hasDetected, hasActive bool
	trust                  string
	mutations, directReads int
}

func (f *fakeEnvPort) DetectedEnv() (pluginapi.EnvInfo, bool) {
	f.directReads++
	return f.detected, f.hasDetected
}
func (f *fakeEnvPort) ActiveEnv() (pluginapi.EnvInfo, bool) {
	f.directReads++
	return f.active, f.hasActive
}
func (f *fakeEnvPort) CachedEnvStatus() (active, detected pluginapi.EnvInfo, hasActive, hasDetected bool, trust string) {
	return f.active, f.detected, f.hasActive, f.hasDetected, f.trust
}
func (f *fakeEnvPort) TrustLevel() string                  { return f.trust }
func (f *fakeEnvPort) SetEnv(string, string, string) error { f.mutations++; return nil }
func (f *fakeEnvPort) ClearEnv() error                     { f.mutations++; return nil }

// TestEnvStatusSegment checks the active-environment indicator against Fresh's env pill
// values: active "<name> ✓", active-unknown "active", detected "<name>", detected while
// untrusted "<name> (locked)", and no segment when nothing is detected. It proves the
// render path only reads cached data (no DetectedEnv/ActiveEnv retry entry point, no
// mutation), never prints the activation snippet, strips Cf/invalid UTF-8 from names, and
// keeps an already-active environment shown active after trust revocation. Not parallel.
func TestEnvStatusSegment(t *testing.T) {
	i18n.SetLocale("zh-CN")
	t.Cleanup(func() { i18n.SetLocale("en") })
	m := newTestModel(t, newFakeEditor("hello"), newFakeTree(2), 80, 20)
	if _, ok := m.envStatusSegment(); ok {
		t.Fatal("a nil EnvPort shows an env segment")
	}
	port := &fakeEnvPort{trust: "trusted"}
	m.env = port
	if _, ok := m.envStatusSegment(); ok {
		t.Fatal("no detected/active environment shows a segment")
	}
	port.detected = pluginapi.EnvInfo{Name: ".venv", Kind: "path-only", Snippet: "SECRET=1 source .venv/bin/activate"}
	port.hasDetected = true
	if seg, ok := m.envStatusSegment(); !ok || seg.Text != ".venv" {
		t.Fatalf("detected trusted segment = %+v", seg)
	}
	port.trust = "restricted"
	if seg, ok := m.envStatusSegment(); !ok || seg.Text != ".venv (locked)" {
		t.Fatalf("detected locked segment = %+v", seg)
	}
	port.active = pluginapi.EnvInfo{Name: ".venv", Kind: "path-only", Snippet: "SECRET=1"}
	port.hasActive = true
	if seg, ok := m.envStatusSegment(); !ok || seg.Text != ".venv ✓" {
		t.Fatalf("active segment = %+v", seg)
	}
	// Trust revocation does not hide an already-active environment (informational only).
	port.trust = "restricted"
	if seg, ok := m.envStatusSegment(); !ok || seg.Text != ".venv ✓" {
		t.Fatalf("active after trust revocation = %+v", seg)
	}
	port.active.Name = ""
	port.hasDetected = false
	port.detected = pluginapi.EnvInfo{}
	if seg, ok := m.envStatusSegment(); !ok || seg.Text != "active" {
		t.Fatalf("active unknown segment = %+v", seg)
	}
	// Untrusted detector names are cleaned: Cf (bidi/zero-width) and invalid UTF-8 go.
	port.hasActive = false
	port.active = pluginapi.EnvInfo{}
	port.hasDetected = true
	port.trust = "trusted"
	port.detected = pluginapi.EnvInfo{Name: "bad\u202e\u200b\xffname"}
	if seg, ok := m.envStatusSegment(); !ok || seg.Text != "badname" {
		t.Fatalf("cleaned name segment = %+v", seg)
	}
	if strings.Contains(segText(m), "SECRET") {
		t.Fatal("the indicator leaked the activation snippet")
	}
	if port.mutations != 0 || port.directReads != 0 {
		t.Fatalf("render used non-cached reads/mutations: direct=%d mutations=%d", port.directReads, port.mutations)
	}
	// Integration: the status row carries the env item in Extra.
	port.detected = pluginapi.EnvInfo{Name: "direnv"}
	port.hasDetected = true
	if view := m.statusView(); !hasStatusItem(view.Extra, "env", "direnv") {
		t.Fatalf("status row Extra = %+v, want the env segment", view.Extra)
	}
}

// segText is the last env segment's text (empty when absent).
func segText(m *Model) string {
	if seg, ok := m.envStatusSegment(); ok {
		return seg.Text
	}
	return ""
}

func hasStatusItem(items []statusItem, id, text string) bool {
	for _, it := range items {
		if it.ID == id && it.Text == text {
			return true
		}
	}
	return false
}
