package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/tbxark/gvim/config"
)

// releaseServer answers the release feed with one fixed body and counts requests.
func releaseServer(t *testing.T, status int, body string) (*httptest.Server, *int) {
	t.Helper()
	count := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv, &count
}

// pinnedVersion stands in for a release build: a development build has no version
// and never checks for updates.
func pinnedVersion(t *testing.T, version string) {
	t.Helper()
	old := updateVersion
	updateVersion = func() string { return version }
	t.Cleanup(func() { updateVersion = old })
}

func TestUpdateHooksReportANewerRelease(t *testing.T) {
	isolateConfig(t)
	pinnedVersion(t, "0.4.1")
	srv, count := releaseServer(t, 200, `{"tag_name":"v0.4.2","html_url":"https://example.com/releases/0.4.2"}`)
	cfg := config.Defaults()
	cfg.Update.URL = srv.URL
	hooks := newUpdateHooks(&cfg)
	if hooks.Check == nil {
		t.Fatal("the check is off although check_for_updates defaults to on")
	}
	info := hooks.Check(context.Background())
	if info == nil || info.Version != "0.4.2" || info.URL != "https://example.com/releases/0.4.2" {
		t.Fatalf("Check = %+v, want 0.4.2 with its page", info)
	}
	if *count != 1 {
		t.Fatalf("requests = %d, want 1", *count)
	}
	// The daily debounce: a second call within the interval asks nothing and reports
	// the release the first call already found (the answer is remembered, not just the
	// timestamp).
	if info := hooks.Check(context.Background()); info == nil || info.Version != "0.4.2" {
		t.Fatalf("second check = %+v, want the remembered release", info)
	}
	if *count != 1 {
		t.Fatalf("requests after the debounce = %d, want 1", *count)
	}
}

func TestUpdateHooksRespectTheConfig(t *testing.T) {
	isolateConfig(t)
	pinnedVersion(t, "0.4.1")
	srv, count := releaseServer(t, 200, `{"tag_name":"v0.4.2"}`)
	cfg := config.Defaults()
	cfg.Update.URL = srv.URL
	cfg.Update.Check = false
	if hooks := newUpdateHooks(&cfg); hooks.Check != nil {
		t.Fatal("check_for_updates false still installs a check")
	}
	// A failed check is silent: no update, no error path for the user.
	bad, _ := releaseServer(t, 500, "")
	cfg.Update.Check = true
	cfg.Update.URL = bad.URL
	if info := newUpdateHooks(&cfg).Check(context.Background()); info != nil {
		t.Fatalf("a failed check reported %+v", info)
	}
	// A development build (no version) never asks the feed.
	pinnedVersion(t, "")
	updateVersion = func() string { return "" }
	cfg.Update.URL = srv.URL
	if info := newUpdateHooks(&cfg).Check(context.Background()); info != nil {
		t.Fatalf("a versionless build reported %+v", info)
	}
	if *count != 0 {
		t.Fatalf("requests from a versionless build = %d, want none", *count)
	}
}

func TestUpdateFeedPrecedence(t *testing.T) {
	cfg := config.Defaults()
	if got := updateFeedURL(&cfg); got == "" || got != "https://api.github.com/repos/tbxark/gvim/releases/latest" {
		t.Fatalf("default feed = %q", got)
	}
	t.Setenv("GVIM_RELEASES_URL", "https://mirror.example.com/latest")
	if got := updateFeedURL(&cfg); got != "https://mirror.example.com/latest" {
		t.Fatalf("environment feed = %q", got)
	}
	cfg.Update.URL = "https://configured.example.com/latest"
	if got := updateFeedURL(&cfg); got != "https://configured.example.com/latest" {
		t.Fatalf("configured feed = %q", got)
	}
}

// TestUpdateCommandHooks: update_command becomes a shell argv for the dock even when
// the release check is off, and an unset command stays nil (the rows stay hidden).
func TestUpdateCommandHooks(t *testing.T) {
	isolateConfig(t)
	cfg := config.Defaults()
	cfg.Update.Check = false
	cfg.Update.Command = "git pull --ff-only"
	hooks := newUpdateHooks(&cfg)
	if hooks.Check != nil {
		t.Fatal("check_for_updates false installed a check hook")
	}
	if len(hooks.Command) != 3 {
		t.Fatalf("command argv = %v, want three entries", hooks.Command)
	}
	if hooks.Command[len(hooks.Command)-1] != "git pull --ff-only" {
		t.Fatalf("command argv = %v, want the configured command last", hooks.Command)
	}
	if got := updateCommandArgv("  "); got != nil {
		t.Fatalf("blank command argv = %v, want nil", got)
	}
}
