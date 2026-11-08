package app

// The release check the root runs at startup: the {update} status element shows
// "Update: vX.Y.Z" when a newer release is published (Fresh's status.update_available).
// The check is bounded and silent on failure, and it never runs on the update
// goroutine: the root runs the hook in a tea.Cmd.

import (
	"context"
	"path/filepath"

	"github.com/tbxark/gvim/config"
	"github.com/tbxark/gvim/internal/tui"
	"github.com/tbxark/gvim/updatecheck"
)

// updateStampName is the file under the user config directory that remembers the
// last successful release check (the daily debounce).
const updateStampName = "update-check.json"

// updateVersion is a variable so a test can stand in for a release build: a
// development build has no version and never checks.
var updateVersion = updatecheck.CurrentVersion

// newUpdateHooks builds the release check of this launch, or the zero value when
// check_for_updates is off. The feed is the configured update_check_url, else the
// GVIM_RELEASES_URL override, else the built-in feed; the transport policy (https,
// or http on loopback) is applied when the check runs, so a refused mirror only
// means no indicator.
func newUpdateHooks(cfg *config.Config) tui.UpdateHooks {
	if cfg == nil || !cfg.Update.Check {
		return tui.UpdateHooks{}
	}
	checker := &updatecheck.Checker{
		Current: updateVersion(),
		URL:     updateFeedURL(cfg),
		Now:     nil,
	}
	if dir, err := config.Dir(); err == nil {
		checker.StampPath = filepath.Join(dir, updateStampName)
	}
	return tui.UpdateHooks{Check: func(ctx context.Context) *tui.UpdateInfo {
		rel, err := checker.CheckIfDue(ctx)
		if err != nil || rel == nil {
			// A failed check is not an error the user needs: the element stays
			// absent and the next launch asks again once the debounce allows it.
			return nil
		}
		return &tui.UpdateInfo{Version: rel.Version, URL: rel.URL}
	}}
}

// updateFeedURL is the feed this launch polls: the configured URL when it is set,
// else the environment override, else the built-in feed.
func updateFeedURL(cfg *config.Config) string {
	if cfg != nil && cfg.Update.URL != "" {
		return cfg.Update.URL
	}
	return updatecheck.ReleasesURL()
}
