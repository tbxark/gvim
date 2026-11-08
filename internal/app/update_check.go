package app

// The release check the root runs: the startup check behind the {update} status
// element, the Check for Updates command and the Show Update Output details.
// The check is bounded and silent on failure; the root runs every hook in a
// tea.Cmd, so no request ever runs on the update goroutine.

import (
	"context"
	"path/filepath"
	"sync"
	"time"

	"github.com/tbxark/gvim/config"
	"github.com/tbxark/gvim/internal/tui"
	"github.com/tbxark/gvim/updatecheck"
)

// updateStampName is the file under the user config directory that remembers the
// last successful release check (the daily debounce and its answer).
const updateStampName = "update-check.json"

// updateVersion is a variable so a test can stand in for a release build: a
// development build has no version and never checks.
var updateVersion = updatecheck.CurrentVersion

// updateTracker keeps the release check and the state the hooks report. The
// fields are guarded because the startup check, the command and the status-row
// render run on different goroutines.
type updateTracker struct {
	mu      sync.Mutex
	checker *updatecheck.Checker
	st      tui.UpdateStatus
}

// newUpdateTracker builds the checker of this launch and seeds the state from the
// last successful check, so a launch inside the debounce interval still paints
// the release the previous launch found.
func newUpdateTracker(cfg *config.Config) *updateTracker {
	t := &updateTracker{}
	t.checker = &updatecheck.Checker{Current: updateVersion(), URL: updateFeedURL(cfg)}
	if dir, err := config.Dir(); err == nil {
		t.checker.StampPath = filepath.Join(dir, updateStampName)
	}
	t.st.Current = t.checker.Current
	t.st.Feed = t.checker.URL
	if rel, at, ok := t.checker.Last(); ok {
		t.st.Latest, t.st.URL, t.st.CheckedAt = rel.Version, rel.URL, at
		if t.st.Latest != "" {
			t.st.Available = updatecheck.Compare(t.st.Latest, t.checker.Current) > 0
		}
	}
	return t
}

// run performs one check and records what it found. dueOnly is the startup check
// (it respects the daily debounce and does nothing when a check is not due); a
// command check always asks the feed.
func (t *updateTracker) run(ctx context.Context, dueOnly bool) tui.UpdateStatus {
	if dueOnly && !t.checker.Due() {
		return t.status()
	}
	rel, err := t.checker.Check(ctx)
	t.mu.Lock()
	defer t.mu.Unlock()
	if err != nil {
		t.st.Err = err.Error()
		return t.st
	}
	t.st.Err = ""
	t.st.CheckedAt = time.Now()
	if rel != nil {
		t.st.Latest, t.st.URL, t.st.Available = rel.Version, rel.URL, true
	} else {
		t.st.Latest, t.st.URL, t.st.Available = "", "", false
	}
	return t.st
}

// status returns the state the root shows.
func (t *updateTracker) status() tui.UpdateStatus {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.st
}

// newUpdateHooks builds the release check of this launch, or the zero value when
// check_for_updates is off. The feed is the configured update_check_url, else the
// GVIM_RELEASES_URL override, else the built-in feed; the transport policy is
// applied when the check runs, so a refused mirror only means no indicator.
func newUpdateHooks(cfg *config.Config) tui.UpdateHooks {
	if cfg == nil || !cfg.Update.Check {
		return tui.UpdateHooks{}
	}
	tracker := newUpdateTracker(cfg)
	return tui.UpdateHooks{
		Check: func(ctx context.Context) *tui.UpdateInfo {
			st := tracker.run(ctx, true)
			if !st.Available {
				return nil
			}
			return &tui.UpdateInfo{Version: st.Latest, URL: st.URL}
		},
		Force: func(ctx context.Context) tui.UpdateStatus {
			return tracker.run(ctx, false)
		},
		Status: tracker.status,
	}
}

// updateFeedURL is the feed this launch polls: the configured URL when it is set,
// else the environment override, else the built-in feed.
func updateFeedURL(cfg *config.Config) string {
	if cfg != nil && cfg.Update.URL != "" {
		return cfg.Update.URL
	}
	return updatecheck.ReleasesURL()
}
