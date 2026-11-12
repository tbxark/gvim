package app

import (
	"testing"

	"github.com/tbxark/gvim/i18n"
)

// TestLocalEnvWatchNoticesUseTheCatalogue checks the local environment-watch notices:
// Chinese shows the English fallback (gvim-only keys), the interpolation composes the old
// text, and the re-wired remote limit notice is back on its catalogue key. Not parallel.
func TestLocalEnvWatchNoticesUseTheCatalogue(t *testing.T) {
	i18n.SetLocale("zh-CN")
	t.Cleanup(func() { i18n.SetLocale("en") })
	if got := appTextVars("gvim.notice.local_env_watch_dirs", "Local environment: %{count} detector marker director(ies) cannot be watched; changed markers there are picked up on Env: Reload", map[string]any{"count": 2}); got != "Local environment: 2 detector marker director(ies) cannot be watched; changed markers there are picked up on Env: Reload" {
		t.Errorf("local watch dirs notice = %q", got)
	}
	if got := appText("gvim.notice.local_env_watch_ended", "Local environment: the file watcher ended; changed detector markers are picked up on Env: Reload"); got != "Local environment: the file watcher ended; changed detector markers are picked up on Env: Reload" {
		t.Errorf("local watch ended notice = %q", got)
	}
	for _, key := range []string{"gvim.notice.local_env_watch_unavailable", "gvim.notice.local_env_watch_ended", "gvim.notice.local_env_watch_root_failed", "gvim.notice.local_env_watch_root_unwatchable", "gvim.notice.local_env_watch_dirs", "gvim.notice.remote_env_watch_limit", "gvim.notice.local_env_watch_limit"} {
		if got := i18n.T("en", key, nil); got == key {
			t.Errorf("missing English entry for %s", key)
		}
	}
}
