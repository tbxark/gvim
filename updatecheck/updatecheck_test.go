package updatecheck

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCompare(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"1.0.0", "1.0.0", 0},
		{"v1.0.0", "1.0.0", 0},
		{"1.2", "1.2.0", 0},
		{"1.0.1", "1.0.0", 1},
		{"1.0.0", "1.0.1", -1},
		{"1.10.0", "1.9.0", 1},
		{"2.0.0", "1.99.99", 1},
		{"1.0.0-alpha", "1.0.0", -1},
		{"1.0.0", "1.0.0-alpha", 1},
		{"1.0.0-alpha", "1.0.0-alpha", 0},
		{"1.0.0-alpha.1", "1.0.0-alpha.2", -1},
		{"1.0.0-alpha.10", "1.0.0-alpha.9", 1},
		{"1.0.0-alpha.beta", "1.0.0-alpha.1", 1},
		{"1.0.0+build.5", "1.0.0", 0},
		{"1.0.0-rc.1+build", "1.0.0-rc.1", 0},
		{"(devel)", "0.1.0", -1},
		{"not-a-version", "1.0.0", -1},
	}
	for _, tt := range tests {
		if got := Compare(tt.a, tt.b); got != tt.want {
			t.Errorf("Compare(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
		}
	}
}

func TestValidateURL(t *testing.T) {
	tests := []struct {
		raw     string
		wantErr bool
	}{
		{"https://example.com/releases/latest", false},
		{"http://127.0.0.1:8080/releases/latest", false},
		{"http://[::1]:8080/releases/latest", false},
		{"http://localhost:9999/releases/latest", false},
		{"http://example.com/releases/latest", true},
		{"ftp://example.com/releases", true},
		{"/releases/latest", true},
		{"example.com/releases", true},
	}
	for _, tt := range tests {
		_, err := ValidateURL(tt.raw)
		if (err != nil) != tt.wantErr {
			t.Errorf("ValidateURL(%q) error = %v, wantErr %v", tt.raw, err, tt.wantErr)
		}
	}
}

func feedServer(t *testing.T, status int, body string) (*httptest.Server, *int) {
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

func TestCheckFindsANewerRelease(t *testing.T) {
	srv, count := feedServer(t, 200, `{"tag_name":"v0.4.2","html_url":"https://example.com/releases/0.4.2"}`)
	dir := t.TempDir()
	c := &Checker{Current: "0.4.1", URL: srv.URL, StampPath: filepath.Join(dir, "update-check.json")}
	rel, err := c.Check(context.Background())
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if rel == nil || rel.Version != "0.4.2" || rel.URL != "https://example.com/releases/0.4.2" {
		t.Fatalf("Check = %+v, want 0.4.2 with its page", rel)
	}
	if *count != 1 {
		t.Fatalf("requests = %d, want 1", *count)
	}
	if _, err := os.Stat(c.StampPath); err != nil {
		t.Fatalf("no stamp written: %v", err)
	}
}

func TestCheckReportsNoUpdateWhenCurrent(t *testing.T) {
	srv, _ := feedServer(t, 200, `{"tag_name":"v0.4.1"}`)
	c := &Checker{Current: "0.4.1", URL: srv.URL}
	if rel, err := c.Check(context.Background()); err != nil || rel != nil {
		t.Fatalf("Check = %+v, %v; want no update", rel, err)
	}
	srv2, _ := feedServer(t, 200, `{"version":"0.3.0"}`)
	c2 := &Checker{Current: "0.4.1", URL: srv2.URL}
	if rel, err := c2.Check(context.Background()); err != nil || rel != nil {
		t.Fatalf("older release: Check = %+v, %v; want no update", rel, err)
	}
}

func TestCheckErrors(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
	}{
		{"http error", 500, ""},
		{"not found", 404, ""},
		{"not json", 200, "not json"},
		{"no version", 200, `{"html_url":"https://example.com"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, _ := feedServer(t, tt.status, tt.body)
			c := &Checker{Current: "0.4.1", URL: srv.URL}
			if rel, err := c.Check(context.Background()); err == nil || rel != nil {
				t.Fatalf("Check = %+v, %v; want an error", rel, err)
			}
		})
	}
	t.Run("oversized feed", func(t *testing.T) {
		srv, _ := feedServer(t, 200, string(make([]byte, maxFeedBytes+10)))
		c := &Checker{Current: "0.4.1", URL: srv.URL}
		if _, err := c.Check(context.Background()); err == nil {
			t.Fatal("an oversized feed was accepted")
		}
	})
	t.Run("disabled by an empty current version", func(t *testing.T) {
		srv, count := feedServer(t, 200, `{"tag_name":"v9.9.9"}`)
		c := &Checker{Current: "", URL: srv.URL}
		if rel, err := c.Check(context.Background()); err != nil || rel != nil {
			t.Fatalf("Check = %+v, %v; want no check", rel, err)
		}
		if *count != 0 {
			t.Fatalf("requests = %d, want none", *count)
		}
	})
	t.Run("plain http feed is refused", func(t *testing.T) {
		c := &Checker{Current: "0.4.1", URL: "http://example.com/releases/latest"}
		if _, err := c.Check(context.Background()); err == nil {
			t.Fatal("a plain-http feed was polled")
		}
	})
}

func TestCheckDebounces(t *testing.T) {
	srv, count := feedServer(t, 200, `{"tag_name":"v0.4.2"}`)
	dir := t.TempDir()
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	c := &Checker{
		Current: "0.4.1", URL: srv.URL,
		StampPath: filepath.Join(dir, "update-check.json"),
		Now:       func() time.Time { return now },
		Interval:  time.Hour,
	}
	if !c.Due() {
		t.Fatal("a fresh stamp path is not due")
	}
	if _, err := c.CheckIfDue(context.Background()); err != nil {
		t.Fatal(err)
	}
	if c.Due() {
		t.Fatal("due right after the check")
	}
	if rel, err := c.CheckIfDue(context.Background()); err != nil || rel != nil {
		t.Fatalf("CheckIfDue within the interval = %+v, %v", rel, err)
	}
	if *count != 1 {
		t.Fatalf("requests = %d, want 1", *count)
	}
	now = now.Add(2 * time.Hour)
	if !c.Due() {
		t.Fatal("not due after the interval")
	}
	if err := os.WriteFile(c.StampPath, []byte("garbage"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !c.Due() {
		t.Fatal("a corrupt stamp is not due")
	}
	c2 := &Checker{Current: "0.4.1", URL: srv.URL}
	if !c2.Due() {
		t.Fatal("a checker without a stamp path is not due")
	}
}

func TestCheckKeepsTheDefaultFeed(t *testing.T) {
	t.Setenv(ReleasesURLEnv, "")
	if got := ReleasesURL(); got != DefaultReleasesURL {
		t.Fatalf("ReleasesURL() = %q, want the default", got)
	}
	t.Setenv(ReleasesURLEnv, "https://mirror.example.com/latest")
	if got := ReleasesURL(); got != "https://mirror.example.com/latest" {
		t.Fatalf("ReleasesURL() = %q, want the override", got)
	}
}

func TestCheckReadsAMirrorFeedVersionKey(t *testing.T) {
	srv, _ := feedServer(t, 200, `{"version":"1.0.0","url":"https://example.com/v1"}`)
	c := &Checker{Current: "0.9.0", URL: srv.URL}
	rel, err := c.Check(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if rel == nil || rel.Version != "1.0.0" || rel.URL != "https://example.com/v1" {
		t.Fatalf("Check = %+v, want the mirror feed's version", rel)
	}
}

var _ = json.Marshal

func TestLastSurvivesTheStamp(t *testing.T) {
	srv, count := feedServer(t, 200, `{"tag_name":"v0.4.2","html_url":"https://example.com/releases/0.4.2"}`)
	stampPath := filepath.Join(t.TempDir(), "update-check.json")
	now := time.Date(2026, 2, 2, 9, 0, 0, 0, time.UTC)
	c := &Checker{Current: "0.4.1", URL: srv.URL, StampPath: stampPath, Now: func() time.Time { return now }}
	if _, err := c.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	// A second launch inside the interval learns the release from the stamp without
	// asking the feed.
	next := &Checker{Current: "0.4.1", URL: srv.URL, StampPath: stampPath, Now: func() time.Time { return now.Add(time.Hour) }}
	if next.Due() {
		t.Fatal("a launch inside the interval is due")
	}
	rel, at, ok := next.Last()
	if !ok || rel.Version != "0.4.2" || rel.URL != "https://example.com/releases/0.4.2" {
		t.Fatalf("Last = %+v, %v, %v; want the stored release", rel, at, ok)
	}
	if !at.Equal(now) {
		t.Fatalf("Last checked at %v, want %v", at, now)
	}
	if *count != 1 {
		t.Fatalf("requests = %d, want 1", *count)
	}
	// A check that finds nothing current stores no release, so a later launch does not
	// paint a stale one.
	current := &Checker{Current: "0.4.2", URL: srv.URL, StampPath: stampPath}
	if _, err := current.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	if rel, _, ok := current.Last(); !ok || rel.Version != "" {
		t.Fatalf("Last after an up-to-date check = %+v, %v; want a stamp without a release", rel, ok)
	}
	// An old timestamp-only stamp still debounces and has no release.
	legacy := filepath.Join(t.TempDir(), "legacy.json")
	if err := os.WriteFile(legacy, []byte(`{"checked": 1700000000}`), 0o600); err != nil {
		t.Fatal(err)
	}
	old := &Checker{Current: "0.4.1", URL: srv.URL, StampPath: legacy, Now: func() time.Time { return time.Unix(1700000100, 0) }}
	if old.Due() {
		t.Fatal("a timestamp-only stamp inside the interval is due")
	}
	if _, _, ok := old.Last(); !ok {
		t.Fatal("a timestamp-only stamp should still be readable")
	}
}
