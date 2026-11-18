package apptest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIsolateConfigIsolatesAllUserDirectories(t *testing.T) {
	IsolateConfig(t)
	home := os.Getenv("HOME")
	if home == "" {
		t.Fatal("HOME is empty")
	}
	for _, key := range []string{"XDG_CONFIG_HOME", "XDG_STATE_HOME", "XDG_CACHE_HOME", "AppData", "LOCALAPPDATA"} {
		val := os.Getenv(key)
		if val == "" {
			t.Fatalf("%s is empty", key)
		}
		rel, err := filepath.Rel(home, val)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			t.Fatalf("%s = %q is outside HOME %q", key, val, home)
		}
	}
}
