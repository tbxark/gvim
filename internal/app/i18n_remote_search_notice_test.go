package app

import (
	"testing"

	"github.com/tbxark/gvim/i18n"
)

// TestRemoteSearchAndLocalWatchNoticesUseTheCatalogue checks this round's new notices:
// German shows the English fallback (gvim-only keys) and the remote-search degradation
// strings keep their exact English. Not parallel: locale.
func TestRemoteSearchAndLocalWatchNoticesUseTheCatalogue(t *testing.T) {
	i18n.SetLocale("de")
	t.Cleanup(func() { i18n.SetLocale("en") })
	if got := appText("gvim.notice.local_env_watch_off", "Local environment: file watching is off; changed detector markers are picked up on Env: Reload"); got != "Local environment: file watching is off; changed detector markers are picked up on Env: Reload" {
		t.Errorf("local watch off = %q", got)
	}
	if got := appText("gvim.notice.remote_search_stale_snapshot", "remote search was limited to the quick-open file-index snapshot; matching remote files outside it were not searched (they may be new since the index last refreshed, or excluded by the glob) — use File Explorer: Refresh to re-enumerate the index"); got != "remote search was limited to the quick-open file-index snapshot; matching remote files outside it were not searched (they may be new since the index last refreshed, or excluded by the glob) — use File Explorer: Refresh to re-enumerate the index" {
		t.Errorf("stale snapshot notice = %q", got)
	}
	for _, key := range []string{"gvim.notice.local_env_watch_off", "gvim.notice.remote_search_nested_repos", "gvim.notice.remote_search_ignored_candidates", "gvim.notice.remote_ignore_git_unavailable", "gvim.notice.remote_ignore_listing_failed", "gvim.notice.remote_ignore_listing_truncated", "gvim.notice.remote_search_cap", "gvim.notice.remote_search_stale_snapshot"} {
		if got := i18n.T("en", key, nil); got == key {
			t.Errorf("missing English entry for %s", key)
		}
	}
}
