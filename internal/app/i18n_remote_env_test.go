package app

import (
	"testing"

	"github.com/tbxark/gvim/i18n"
)

// TestRemoteEnvNoticesUseTheCatalogue checks the extracted remote-environment notices:
// Chinese shows the English fallback (gvim-only keys) and the placeholders compose the
// same text the shell/watch code produced before. Not parallel: locale.
func TestRemoteEnvNoticesUseTheCatalogue(t *testing.T) {
	i18n.SetLocale("zh-CN")
	t.Cleanup(func() { i18n.SetLocale("en") })
	got := appTextVars("gvim.notice.remote_env_watch_limit", "Remote environment: %{markers} detector marker path(s) in %{directories} director(ies) are not watched (watch bound reached); use Env: Reload after changing them", map[string]any{"markers": 2, "directories": 1})
	if got != "Remote environment: 2 detector marker path(s) in 1 director(ies) are not watched (watch bound reached); use Env: Reload after changing them" {
		t.Errorf("watch limit notice = %q", got)
	}
	if got := appText("gvim.notice.remote_env_shell_none", "Remote environment: no interactive bash/zsh login shell was found on the host; using sh -c (environment recipes that rely on interactive init may differ from the terminal)"); got != "Remote environment: no interactive bash/zsh login shell was found on the host; using sh -c (environment recipes that rely on interactive init may differ from the terminal)" {
		t.Errorf("shell notice = %q", got)
	}
	for _, key := range []string{"gvim.notice.remote_env_watch_limit", "gvim.notice.remote_env_watch_unsupported", "gvim.notice.remote_env_watch_failed", "gvim.notice.remote_env_shell_detect_failed", "gvim.notice.remote_env_shell_none", "gvim.notice.remote_env_capture_failed", "gvim.notice.terminal_dir_invalid", "gvim.notice.remote_terminal_dir_unavailable"} {
		if got := i18n.T("en", key, nil); got == key {
			t.Errorf("missing English entry for %s", key)
		}
	}
}
