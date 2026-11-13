package tui

import (
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/tui/frame"
	"github.com/tbxark/gvim/pluginapi"
)

// TestOfficialEnvPillSuppressesNativeSegment checks the duplicate rule: the official
// env-manager plugin's own "env" status pill (plugin name env_manager) replaces the native
// environment segment, so exactly one env item is rendered and its click runs the plugin's
// own status action, not the native fallback.
func TestOfficialEnvPillSuppressesNativeSegment(t *testing.T) {
	e := newKeyEnv(t)
	e.m.env = &fakeEnvPort{detected: pluginapi.EnvInfo{Name: "direnv"}, hasDetected: true, trust: "trusted"}
	e.m.plug.host.SetStatusItems("env_manager", []pluginapi.StatusItem{
		{ID: "env", Plugin: "env_manager", Text: "official env", Clickable: true, Right: true},
	})
	entries := e.m.statusExtraEntries()
	var envSources []statusExtraSource
	for _, entry := range entries {
		if entry.item.ID == "env" {
			envSources = append(envSources, entry.source)
		}
	}
	if !slices.Equal(envSources, []statusExtraSource{statusExtraPlugin}) {
		t.Fatalf("env items = %v, want only the official plugin pill", envSources)
	}
	l := e.m.layout()
	x := -1
	for _, r := range l.Status {
		if r.Kind == frame.HitStatusPlugin && strings.Contains(r.Text, "official env") {
			x = r.X
		}
	}
	if x < 0 {
		t.Fatalf("the official env pill is not on the status row: %+v", l.Status)
	}
	e.m.Update(click(x, l.StatusY, tea.MouseLeft))
	if !slices.Equal(e.rt.runs, []string{"env_manager.__status_env"}) {
		t.Fatalf("runs = %v, want the official plugin status-click action", e.rt.runs)
	}
	if len(e.m.overlays) != 0 || strings.Contains(e.m.notice, "environment") {
		t.Fatalf("the official pill click reached the native env action: notice=%q overlays=%d", e.m.notice, len(e.m.overlays))
	}
}

// TestOtherPluginEnvIDDoesNotSuppressNativeSegment checks the flip side: a non-official
// plugin item whose id is also "env" must stay independent, so the native segment is still
// rendered and its click reaches the native env action while the plugin item runs its own.
func TestOtherPluginEnvIDDoesNotSuppressNativeSegment(t *testing.T) {
	e := newKeyEnv(t)
	e.m.env = &fakeEnvPort{detected: pluginapi.EnvInfo{Name: "direnv"}, hasDetected: true, trust: "trusted"}
	e.m.plug.host.SetStatusItems("p", []pluginapi.StatusItem{
		{ID: "env", Plugin: "p", Text: "other env", Clickable: true},
	})
	entries := e.m.statusExtraEntries()
	sources := map[statusExtraSource]bool{}
	for _, entry := range entries {
		if entry.item.ID == "env" || entry.source == statusExtraEnv {
			sources[entry.source] = true
		}
	}
	if !sources[statusExtraPlugin] || !sources[statusExtraEnv] {
		t.Fatalf("sources = %v, want both the other plugin item and the native segment", sources)
	}
	l := e.m.layout()
	pluginX, nativeX := -1, -1
	for _, r := range l.Status {
		if r.Kind != frame.HitStatusPlugin {
			continue
		}
		switch {
		case strings.Contains(r.Text, "other env"):
			pluginX = r.X
		case strings.Contains(r.Text, "direnv"):
			nativeX = r.X
		}
	}
	if pluginX < 0 || nativeX < 0 {
		t.Fatalf("status row missing plugin/native env items: %+v", l.Status)
	}
	// The plugin item runs its own action and does not reach the native click.
	e.m.Update(click(pluginX, l.StatusY, tea.MouseLeft))
	if !slices.Equal(e.rt.runs, []string{"p.__status_env"}) {
		t.Fatalf("plugin runs = %v", e.rt.runs)
	}
	// The native item still opens the trust entry (untrusted) rather than a plugin action.
	e.m.env = &fakeEnvPort{detected: pluginapi.EnvInfo{Name: "direnv"}, hasDetected: true, trust: "restricted"}
	l = e.m.layout()
	nativeX = -1
	for _, r := range l.Status {
		if r.Kind == frame.HitStatusPlugin && strings.Contains(r.Text, "direnv") {
			nativeX = r.X
		}
	}
	if nativeX < 0 {
		t.Fatal("native env item disappeared")
	}
	e.m.Update(click(nativeX, l.StatusY, tea.MouseLeft))
	if got := e.m.statusExtraEntries(); len(got) == 0 {
		t.Fatal("status entries disappeared")
	}
	if e.m.notice == "Env: Activate is unavailable — enable the env-manager plugin" {
		t.Fatal("the native click was routed to a plugin action")
	}
}
