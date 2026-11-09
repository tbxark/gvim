package app

// The release check the root runs: the startup check behind the {update} status
// element, the Check for Updates command and the Show Update Output details.
// The check is bounded and silent on failure; the root runs every hook in a
// tea.Cmd, so no request ever runs on the update goroutine.

import (
	"context"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
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

// updateCommandArgv turns update_command into the argv the terminal dock runs. The
// config value is a command line, so it goes through the user's shell: $SHELL -c on
// Unix (POSIX shells and fish all accept -c) and cmd.exe /C on Windows. It returns nil
// for an unset command, which keeps the update rows hidden.
func updateCommandArgv(command string) []string {
	command = strings.TrimSpace(command)
	if command == "" {
		return nil
	}
	if goruntime.GOOS == "windows" {
		return []string{"cmd.exe", "/C", command}
	}
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
	}
	return []string{shell, "-c", command}
}

// newUpdateHooks builds the release check and update command of this launch. The
// command is independent of check_for_updates (a user may update without polling the
// feed); when the check is off the Check/Force/Status hooks stay nil and the two
// update-check commands say so. The feed is the configured update_check_url, else the
// GVIM_RELEASES_URL override, else the built-in feed; the transport policy is applied
// when the check runs, so a refused mirror only means no indicator.
func newUpdateHooks(cfg *config.Config) tui.UpdateHooks {
	if cfg == nil {
		return tui.UpdateHooks{}
	}
	hooks := tui.UpdateHooks{Command: updateCommandArgv(cfg.Update.Command)}
	if !cfg.Update.Check {
		return hooks
	}
	tracker := newUpdateTracker(cfg)
	hooks.Check = tracker.check
	hooks.Force = tracker.force
	hooks.Status = tracker.status
	return hooks
}

// check is the startup hook: the daily-debounced check, or nil when no newer release.
func (t *updateTracker) check(ctx context.Context) *tui.UpdateInfo {
	st := t.run(ctx, true)
	if !st.Available {
		return nil
	}
	return &tui.UpdateInfo{Version: st.Latest, URL: st.URL}
}

// force is the Check for Updates command: ignore the debounce and report the state.
func (t *updateTracker) force(ctx context.Context) tui.UpdateStatus { return t.run(ctx, false) }

// updateFeedURL is the feed this launch polls: the configured URL when it is set,
// else the environment override, else the built-in feed.
func updateFeedURL(cfg *config.Config) string {
	if cfg != nil && cfg.Update.URL != "" {
		return cfg.Update.URL
	}
	return updatecheck.ReleasesURL()
}
