package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/tbxark/gvim/config"
)

// writeUpdateStamp seeds the debounce stamp a previous launch would have left.
func writeUpdateStamp(t *testing.T, checked time.Time, latest, url string) {
	t.Helper()
	dir, err := config.StateDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := fmt.Sprintf(`{"checked":%d}`, checked.Unix())
	if latest != "" {
		body = fmt.Sprintf(`{"checked":%d,"latest":%q,"url":%q}`, checked.Unix(), latest, url)
	}
	if err := os.WriteFile(filepath.Join(dir, updateStampName), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestTrackerSeedsFromTheStamp checks that a launch inside the debounce interval
// paints the release the previous launch found, without asking the feed.
func TestTrackerSeedsFromTheStamp(t *testing.T) {
	isolateConfig(t)
	pinnedVersion(t, "0.4.1")
	srv, count := releaseServer(t, 200, `{"tag_name":"v0.9.9"}`)
	writeUpdateStamp(t, time.Now(), "0.4.2", "https://example.com/releases/0.4.2")
	cfg := config.Defaults()
	cfg.Update.URL = srv.URL
	hooks := newUpdateHooks(&cfg)
	info := hooks.Check(context.Background())
	if info == nil || info.Version != "0.4.2" || info.URL != "https://example.com/releases/0.4.2" {
		t.Fatalf("Check = %+v, want the stored release", info)
	}
	if *count != 0 {
		t.Fatalf("requests = %d, want none inside the interval", *count)
	}
	st := hooks.Status()
	if !st.Available || st.Latest != "0.4.2" || st.Current != "0.4.1" || st.CheckedAt.IsZero() {
		t.Fatalf("Status = %+v, want the stored release", st)
	}
	st = hooks.Force(context.Background())
	if *count != 1 {
		t.Fatalf("requests after Force = %d, want 1", *count)
	}
	if !st.Available || st.Latest != "0.9.9" || st.Err != "" {
		t.Fatalf("forced Status = %+v, want 0.9.9", st)
	}
}

// TestTrackerForceReportsFailures checks the failure path: a forced check keeps
// the release it already knew and records why the new check failed.
func TestTrackerForceReportsFailures(t *testing.T) {
	isolateConfig(t)
	pinnedVersion(t, "0.4.1")
	bad, _ := releaseServer(t, 500, "")
	writeUpdateStamp(t, time.Now().Add(-48*time.Hour), "0.4.2", "https://example.com/releases/0.4.2")
	cfg := config.Defaults()
	cfg.Update.URL = bad.URL
	hooks := newUpdateHooks(&cfg)
	st := hooks.Force(context.Background())
	if st.Err == "" {
		t.Fatal("a failed check recorded no reason")
	}
	if !st.Available || st.Latest != "0.4.2" {
		t.Fatalf("Status = %+v, want the release the last successful check found", st)
	}
}

// TestTrackerWithoutVersionNeverRequests checks a development build: nothing is
// asked and the state stays empty.
func TestTrackerWithoutVersionNeverRequests(t *testing.T) {
	isolateConfig(t)
	pinnedVersion(t, "")
	srv, count := releaseServer(t, 200, `{"tag_name":"v9.9.9"}`)
	cfg := config.Defaults()
	cfg.Update.URL = srv.URL
	hooks := newUpdateHooks(&cfg)
	if info := hooks.Check(context.Background()); info != nil {
		t.Fatalf("a versionless build reported %+v", info)
	}
	if st := hooks.Force(context.Background()); st.Available || st.Err != "" {
		t.Fatalf("a versionless forced check = %+v", st)
	}
	if *count != 0 {
		t.Fatalf("requests = %d, want none", *count)
	}
}
