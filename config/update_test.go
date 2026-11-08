package config

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestUpdateCheckDefaultsAndParsing(t *testing.T) {
	t.Parallel()
	cfg, diags := loadBody(t, "{}")
	if len(diags) != 0 || !cfg.Update.Check || cfg.Update.URL != "" {
		t.Fatalf("defaults: %+v, diags %v; want the check on with the built-in feed", cfg.Update, diags)
	}
	cfg, diags = loadBody(t, `{"check_for_updates": false}`)
	if len(diags) != 0 || cfg.Update.Check {
		t.Fatalf("check_for_updates false: %+v, diags %v", cfg.Update, diags)
	}
	cfg, diags = loadBody(t, `{"update_check_url": "https://mirror.example.com/latest"}`)
	if len(diags) != 0 || cfg.Update.URL != "https://mirror.example.com/latest" {
		t.Fatalf("update_check_url: %+v, diags %v", cfg.Update, diags)
	}
	// A local mirror is an http URL on the loopback interface; the transport
	// policy is applied when the check runs, so the load only requires a URL.
	cfg, diags = loadBody(t, `{"update_check_url": "http://127.0.0.1:8080/latest"}`)
	if len(diags) != 0 || cfg.Update.URL != "http://127.0.0.1:8080/latest" {
		t.Fatalf("loopback mirror: %+v, diags %v", cfg.Update, diags)
	}
	for _, body := range []string{
		`{"check_for_updates": "yes"}`,
		`{"update_check_url": 5}`,
		`{"update_check_url": "not a url"}`,
		`{"update_check_url": "ftp://example.com/latest"}`,
	} {
		if _, diags := loadBody(t, body); len(diags) != 1 {
			t.Fatalf("%s: diagnostics = %v, want one", body, diags)
		}
	}
	// self_update is a distribution concern gvim does not implement: the key is
	// accepted and ignored, like the other Fresh keys gvim has no use for.
	if _, diags := loadBody(t, `{"self_update": false}`); len(diags) != 0 {
		t.Fatalf("self_update diagnostics = %v, want none", diags)
	}
}

func TestUpdateCheckProjectLayerRefused(t *testing.T) {
	for _, body := range []string{
		`{"check_for_updates": false}`,
		`{"update_check_url": "https://example.com/latest"}`,
	} {
		root := t.TempDir()
		writeFile(t, filepath.Join(root, ".gvim", "config.json"), body)
		cfg, diags, err := Load(Options{Root: root, UserFile: filepath.Join(t.TempDir(), "absent.json")})
		if err != nil {
			t.Fatal(err)
		}
		// The project layer changed nothing.
		if !cfg.Update.Check || cfg.Update.URL != "" {
			t.Fatalf("%s: project layer applied: %+v", body, cfg.Update)
		}
		found := false
		for _, d := range diags {
			if strings.Contains(d.Message, "project config cannot set") && strings.Contains(d.Message, "release check") {
				found = true
			}
		}
		if !found {
			t.Fatalf("%s: diagnostics = %v, want the release-check refusal", body, diags)
		}
	}
}
