package tui

import (
	"testing"

	"github.com/tbxark/gvim/i18n"
	"github.com/tbxark/gvim/trust"
)

// TestTask154AuthorityRemoteStrings checks the new authority/remote/trust extraction:
// Fresh-mapped trust labels render Chinese while the gvim-only SSH/remote/plugin strings
// fall back to English, and the language-server plural keys select .one/.other. Not
// parallel: locale.
func TestTask154AuthorityRemoteStrings(t *testing.T) {
	i18n.SetLocale("zh-CN")
	t.Cleanup(func() { i18n.SetLocale("en") })
	if got := uiText("gvim.status.trusted", "Trusted"); got != "已信任" {
		t.Errorf("Chinese trust status = %q, want Fresh's translation", got)
	}
	if got := uiText("gvim.notice.trusted", "Workspace trusted — project tooling may run processes"); got == "gvim.notice.trusted" {
		t.Errorf("trust notice leaked the key")
	}
	if got := uiTextVars("gvim.notice.ssh_reconnect_failed", "SSH reconnect failed: %{error}", map[string]any{"error": "boom"}); got != "SSH reconnect failed: boom" {
		t.Errorf("SSH notice = %q", got)
	}
	// The gvim-only plural keys fall back to English and select the right form.
	if got := i18n.TN("en", "gvim.notice.language_servers_restarted", 1, nil); got != "; 1 language server restarted" {
		t.Errorf("plural one = %q", got)
	}
	if got := i18n.TN("en", "gvim.notice.language_servers_restarted", 2, nil); got != "; 2 language servers restarted" {
		t.Errorf("plural other = %q", got)
	}
	for _, key := range []string{"gvim.notice.ssh_reconnected", "gvim.notice.post_reconnect_refresh_failed", "gvim.prompt.ssh_key_passphrase", "gvim.notice.remote_workspace_restore", "gvim.notice.workspace_trust", "gvim.status.remote_local", "gvim.remote.title", "gvim.plugin.scope_invalid", "gvim.notice.lsp_restarted_after_reconnect.one"} {
		if got := i18n.T("en", key, nil); got == key {
			t.Errorf("missing English entry for %s", key)
		}
	}
}

// TestTrustStatusLabelFollowsLocale checks the status-bar trust pill reads the catalogue
// (gvim.status.*) instead of the English trust.Level names. Not parallel: locale.
func TestTrustStatusLabelFollowsLocale(t *testing.T) {
	i18n.SetLocale("zh-CN")
	t.Cleanup(func() { i18n.SetLocale("en") })
	tests := []struct {
		level trust.Level
		want  string
	}{
		{trust.Trusted, "已信任"},
		{trust.Restricted, uiText("gvim.status.restricted", "Restricted")},
		{trust.Blocked, uiText("gvim.status.blocked", "Blocked")},
	}
	for _, tt := range tests {
		m := newTrustModel(t, &fakeTrustPort{state: trust.State{Root: "/ws", Level: tt.level, Decided: true}})
		label, _ := m.trustStatus()
		if label != tt.want {
			t.Errorf("trustStatus() under zh-CN for %v = %q, want %q", tt.level, label, tt.want)
		}
	}
	if got := uiText("gvim.status.restricted", "Restricted"); got == "Restricted" {
		t.Errorf("zh-CN catalogue has no gvim.status.restricted translation: %q", got)
	}
}
