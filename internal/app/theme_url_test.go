package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tbxark/gvim/config"
	"github.com/tbxark/gvim/theme"
)

// urlThemeJSON is a minimal valid theme body for the URL tests.
const urlThemeJSON = `{"extends":"builtin://dark","editor":{"cursor":[1,2,3]}}`

// countingThemeServer serves a theme JSON at /cool.json and counts requests.
func countingThemeServer(t *testing.T, body string) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n.Add(1)
		if r.URL.Path != "/cool.json" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv, &n
}

// urlThemeCacheDir is the cache directory a launch with the isolated config uses.
func urlThemeCacheDir(t *testing.T) string {
	t.Helper()
	dir, err := config.CacheDir()
	if err != nil {
		t.Fatal(err)
	}
	return theme.URLCacheDir(dir)
}

func TestFetchURLThemeRoundTripAndCache(t *testing.T) {
	isolateConfig(t)
	srv, requests := countingThemeServer(t, urlThemeJSON)
	ref := srv.URL + "/cool.json"
	reg := theme.NewRegistry("")

	th, warning, err := fetchURLTheme(context.Background(), ref, reg)
	if err != nil || warning != "" {
		t.Fatalf("fetch = %+v, %q, %v", th, warning, err)
	}
	if th.Name != "cool" {
		t.Fatalf("theme name = %q, want cool", th.Name)
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("requests = %d, want 1", got)
	}
	// The fetched theme was cached under the URL hash and the runtime registry
	// path reads it without another request.
	path := theme.URLCachePath(urlThemeCacheDir(t), ref)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("cache file = %v", err)
	}
	if th, err := reg.Load(ref); err != nil || th.Name != "cool" {
		t.Fatalf("cached Load = %+v, %v", th, err)
	}
	if _, _, err := fetchURLTheme(context.Background(), ref, reg); err != nil {
		t.Fatal(err)
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("a fresh cache still fetched: %d requests", got)
	}
}

func TestFetchURLThemeOfflineUsesLastKnownGood(t *testing.T) {
	isolateConfig(t)
	srv, _ := countingThemeServer(t, urlThemeJSON)
	ref := srv.URL + "/cool.json"
	reg := theme.NewRegistry("")
	if _, _, err := fetchURLTheme(context.Background(), ref, reg); err != nil {
		t.Fatal(err)
	}
	// Make the cache stale so the next call tries the network, then take the
	// server away: the cached copy is the last-known-good fallback.
	path := theme.URLCachePath(urlThemeCacheDir(t), ref)
	old := time.Now().Add(-2 * themeURLCacheTTL)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	srv.Close()
	th, warning, err := fetchURLTheme(context.Background(), ref, reg)
	if err != nil {
		t.Fatalf("offline fetch = %v, want the cached copy", err)
	}
	if th.Name != "cool" || !strings.Contains(warning, "cached copy") {
		t.Fatalf("offline fallback = %+v, %q", th, warning)
	}
}

func TestFetchURLThemeInvalidAndOversized(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{name: "invalid JSON", body: "{not a theme", want: "theme:"},
		{name: "oversized", body: strings.Repeat("a", maxThemeURLBytes+64), want: "larger than"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isolateConfig(t)
			srv, _ := countingThemeServer(t, tt.body)
			reg := theme.NewRegistry("")
			_, _, err := fetchURLTheme(context.Background(), srv.URL+"/cool.json", reg)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("fetch = %v, want %q", err, tt.want)
			}
			if _, statErr := os.Stat(theme.URLCachePath(urlThemeCacheDir(t), srv.URL+"/cool.json")); statErr == nil {
				t.Fatal("an invalid fetch wrote the cache")
			}
		})
	}
}

func TestFetchURLThemePolicyRefusal(t *testing.T) {
	isolateConfig(t)
	// Plain http off the loopback interface is refused before any request.
	if _, _, err := fetchURLTheme(context.Background(), "http://example.com/theme.json", theme.NewRegistry("")); err == nil || !strings.Contains(err.Error(), "loopback") {
		t.Fatalf("policy refusal = %v", err)
	}
	// Unreachable loopback is allowed by policy but fails the fetch: no cache means
	// an error, not a silent wait.
	if _, _, err := fetchURLTheme(context.Background(), "http://127.0.0.1:1/theme.json", theme.NewRegistry("")); err == nil {
		t.Fatal("an unreachable URL with no cache was accepted")
	}
}

func TestBuildRuntimeURLThemeOfflineFallsBack(t *testing.T) {
	isolateConfig(t)
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	if err := os.WriteFile(cfgPath, []byte(`{"theme":"http://127.0.0.1:1/theme.json"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	rt, err := buildRuntime(context.Background(), Options{Root: dir, Config: cfgPath, NoPlugins: true, SessionDir: filepath.Join(dir, "session")}, false)
	if err != nil {
		t.Fatalf("a failed URL theme aborted the start: %v", err)
	}
	defer rt.close()
}

// A redirect off the transport policy (plain http beyond loopback) is refused
// before it is followed.
func TestFetchThemeURLBodyRefusesInsecureRedirect(t *testing.T) {
	var reached atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached.Store(true)
		http.Redirect(w, r, "http://example.com/cool.json", http.StatusFound)
	}))
	t.Cleanup(srv.Close)
	_, err := fetchThemeURLBody(context.Background(), srv.URL+"/cool.json")
	if err == nil || !strings.Contains(err.Error(), "loopback") {
		t.Fatalf("redirect to plain http = %v, want a transport policy refusal", err)
	}
	if !reached.Load() {
		t.Fatal("the loopback server was never asked")
	}
}
