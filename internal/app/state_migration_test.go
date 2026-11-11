package app

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/tbxark/gvim/config"
	"github.com/tbxark/gvim/theme"
)

func TestUpdateTrackerMigratesLegacyStamp(t *testing.T) {
	isolateConfig(t)
	oldDir, err := config.Dir()
	if err != nil {
		t.Fatal(err)
	}
	stateDir, err := config.StateDir()
	if err != nil {
		t.Fatal(err)
	}
	old := filepath.Join(oldDir, updateStampName)
	dest := filepath.Join(stateDir, updateStampName)
	if err := os.MkdirAll(oldDir, 0755); err != nil {
		t.Fatal(err)
	}
	stamp := []byte(fmt.Sprintf(`{"checked":%d,"latest":"9.0.0"}`, time.Now().Unix()))
	if err := os.WriteFile(old, stamp, 0600); err != nil {
		t.Fatal(err)
	}
	cfg := config.Defaults()
	tracker := newUpdateTracker(&cfg)
	if tracker.checker.StampPath != dest || tracker.status().Latest != "9.0.0" {
		t.Fatalf("tracker path = %q, status = %+v", tracker.checker.StampPath, tracker.status())
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatalf("legacy stamp remains: %v", err)
	}
	if err := os.WriteFile(old, []byte("stale"), 0600); err != nil {
		t.Fatal(err)
	}
	newUpdateTracker(&cfg)
	if data, err := os.ReadFile(dest); err != nil || string(data) != string(stamp) {
		t.Fatalf("existing stamp overwritten: %q, %v", data, err)
	}
}

func TestThemeCacheMigratesLegacyEntries(t *testing.T) {
	isolateConfig(t)
	oldBase, err := config.Dir()
	if err != nil {
		t.Fatal(err)
	}
	cacheBase, err := config.CacheDir()
	if err != nil {
		t.Fatal(err)
	}
	old := theme.URLCacheDir(oldBase)
	dest := theme.URLCacheDir(cacheBase)
	if err := os.MkdirAll(old, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dest, 0755); err != nil {
		t.Fatal(err)
	}
	for name, value := range map[string]string{"old.json": "old", "existing.json": "stale"} {
		if err := os.WriteFile(filepath.Join(old, name), []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dest, "existing.json"), []byte("new"), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := themeURLCacheDir()
	if err != nil || got != dest {
		t.Fatalf("cache dir = %q, %v", got, err)
	}
	for name, want := range map[string]string{"old.json": "old", "existing.json": "new"} {
		data, err := os.ReadFile(filepath.Join(dest, name))
		if err != nil || string(data) != want {
			t.Fatalf("%s = %q, %v", name, data, err)
		}
	}
}
