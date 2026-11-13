package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/tui/frame"
	"github.com/tbxark/gvim/pluginapi"
	"github.com/tbxark/gvim/trust"
)

// envStatusRun finds the status row's env segment: it is a plugin-style Extra item
// (HitStatusPlugin) whose text carries the cached environment name.
func envStatusRun(t *testing.T, m *Model) frame.Run {
	t.Helper()
	for _, r := range m.layout().Status {
		if r.Kind == frame.HitStatusPlugin && strings.Contains(r.Text, "direnv") {
			return r
		}
	}
	t.Fatalf("no env status run in %+v", m.layout().Status)
	return frame.Run{}
}

// TestEnvStatusClickTrustEntry checks the real mouse path: on an untrusted workspace a
// click on the env segment opens the existing cancellable Workspace Trust dialog and
// never calls an activation or decides trust itself.
func TestEnvStatusClickTrustEntry(t *testing.T) {
	i18n := trust.State{Root: "/ws", Level: trust.Restricted, Decided: true}
	p := &fakeTrustPort{state: i18n}
	m := newTrustModel(t, p)
	m.env = &fakeEnvPort{detected: pluginapi.EnvInfo{Name: "direnv"}, hasDetected: true, trust: string(trust.Restricted)}
	r := envStatusRun(t, m)
	l := m.layout()
	m.Update(click(r.X+1, l.StatusY, tea.MouseLeft))
	topTrustDialog(t, m)
	port := m.env.(*fakeEnvPort)
	if port.mutations != 0 {
		t.Fatalf("a trust-entry click activated the environment %d time(s)", port.mutations)
	}
	if len(p.decided) != 0 {
		t.Fatalf("a status click decided the trust level itself: %v", p.decided)
	}
}

// TestEnvStatusClickFailures checks the safe fallbacks: trusted without the env-manager
// plugin says how to enable it (activate for a detected env, reload for an active one),
// and a workspace with no detected env says so; none of them activates or opens a dialog.
func TestEnvStatusClickFailures(t *testing.T) {
	m := newTestModel(t, newFakeEditor("hello"), newFakeTree(2), 80, 20)
	m.env = &fakeEnvPort{detected: pluginapi.EnvInfo{Name: "direnv"}, hasDetected: true, trust: "trusted"}
	if cmd := m.envStatusClick(); cmd != nil {
		t.Fatal("trusted click without the plugin returned a command")
	}
	if m.notice != "Env: Activate is unavailable — enable the env-manager plugin" {
		t.Fatalf("unavailable notice = %q", m.notice)
	}
	m.env = &fakeEnvPort{active: pluginapi.EnvInfo{Name: "direnv"}, hasActive: true, trust: "trusted"}
	m.notice = ""
	m.envStatusClick()
	if m.notice != "Env: Reload is unavailable — enable the env-manager plugin" {
		t.Fatalf("active unavailable notice = %q", m.notice)
	}
	m.env = &fakeEnvPort{trust: "trusted"}
	m.notice = ""
	m.envStatusClick()
	if m.notice != "No environment manager detected in this project" {
		t.Fatalf("no-env notice = %q", m.notice)
	}
	if len(m.overlays) != 0 {
		t.Fatalf("a failure click opened overlays: %d", len(m.overlays))
	}
	// The segment is registered as clickable so the frame routes it here.
	items := m.statusExtraItems()
	found := false
	for i, it := range items {
		if it.ID == "env" {
			found = true
			if _, ok := m.statusExtraEntryAt(i); !ok || !it.Clickable {
				t.Fatalf("env item %d is not clickable: %+v", i, it)
			}
		}
	}
	if !found && len(items) > 0 {
		t.Fatal("env item missing from statusExtraItems with a live EnvPort")
	}
}
