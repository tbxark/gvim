package tui

import (
	"testing"

	"github.com/tbxark/gvim/i18n"
)

// TestTask154AuthorityRemoteStrings checks the new authority/remote/trust extraction:
// Fresh-mapped trust labels render German while the gvim-only SSH/remote/plugin strings
// fall back to English, and the language-server plural keys select .one/.other. Not
// parallel: locale.
func TestTask154AuthorityRemoteStrings(t *testing.T) {
	i18n.SetLocale("de")
	t.Cleanup(func() { i18n.SetLocale("en") })
	if got := uiText("gvim.status.trusted", "Trusted"); got != "Vertraut" {
		t.Errorf("German trust status = %q, want Fresh's translation", got)
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
