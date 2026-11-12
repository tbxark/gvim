package config

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestLocaleDefaultsAndParsing(t *testing.T) {
	t.Parallel()
	cfg, diags := loadBody(t, "{}")
	if len(diags) != 0 || cfg.Locale != "" {
		t.Fatalf("default locale = %q, diags %v; want auto", cfg.Locale, diags)
	}
	cfg, diags = loadBody(t, `{"locale": "zh-CN"}`)
	if len(diags) != 0 || cfg.Locale != "zh-CN" {
		t.Fatalf("locale = %q, diags %v", cfg.Locale, diags)
	}
	// Fresh writes null for the automatic locale; "auto" is gvim's setting value.
	for _, body := range []string{`{"locale": null}`, `{"locale": "auto"}`} {
		cfg, diags = loadBody(t, body)
		if len(diags) != 0 || cfg.Locale != "" {
			t.Fatalf("%s: locale = %q, diags %v; want auto", body, cfg.Locale, diags)
		}
	}
	_, diags = loadBody(t, `{"locale": 5}`)
	if len(diags) != 1 || diags[0].Key != "locale" {
		t.Fatalf("diagnostics = %v, want one for the locale", diags)
	}
	_, diags = loadBody(t, `{"locale": ""}`)
	if len(diags) != 1 || diags[0].Key != "locale" {
		t.Fatalf("empty locale diagnostics = %v", diags)
	}
}

func TestLocaleProjectLayerRefused(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, ".gvim", "config.json"), `{"locale": "zh-CN"}`)
	cfg, diags, err := Load(Options{Root: root, UserFile: filepath.Join(t.TempDir(), "absent.json")})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Locale != "" {
		t.Fatalf("project locale applied: %q", cfg.Locale)
	}
	found := false
	for _, d := range diags {
		if d.Key == "locale" && strings.Contains(d.Message, "project config cannot set locale") {
			found = true
		}
	}
	if !found {
		t.Fatalf("diagnostics = %v, want the project locale refusal", diags)
	}
}

func TestLocaleSetting(t *testing.T) {
	t.Parallel()
	s, ok := SettingByKey("locale")
	if !ok {
		t.Fatal("locale is not a Settings setting")
	}
	if s.Kind != SettingEnum || s.Apply != ApplyLive || !s.Layers.Has(LayerUser) || s.Layers.Has(LayerProject) {
		t.Fatalf("locale setting = %+v, want a live user-layer enum", s)
	}
	if len(s.Choices) != 3 || s.Choices[0] != "auto" || s.Choices[1] != "en" || s.Choices[2] != "zh-CN" {
		t.Fatalf("choices = %v, want auto, en and zh-CN", s.Choices)
	}
	if got := s.Value(Defaults()); got != "auto" {
		t.Fatalf("default value = %q, want auto", got)
	}
	cfg := Defaults()
	cfg.Locale = "zh-CN"
	if got := s.Value(cfg); got != "zh-CN" {
		t.Fatalf("value = %q", got)
	}
	auto, err := s.Encode("auto")
	if err != nil || strings.TrimSpace(string(auto)) != "null" {
		t.Fatalf("encode auto = %q, %v; want null", auto, err)
	}
	raw, err := s.Encode("zh-CN")
	if err != nil || strings.TrimSpace(string(raw)) != `"zh-CN"` {
		t.Fatalf("encode zh-CN = %q, %v", raw, err)
	}
	if _, err := s.Encode("de"); err == nil {
		t.Fatal("encode accepted the removed German locale")
	}
	if _, err := s.Encode("xx"); err == nil {
		t.Fatal("encode accepted an unknown locale")
	}
}
