package config

import (
	"strings"
	"testing"
)

func TestClipboardDefaults(t *testing.T) {
	t.Parallel()
	cfg, diags := loadBody(t, "{}")
	if len(diags) != 0 {
		t.Fatalf("diagnostics = %v", diags)
	}
	if cfg.Clipboard.UseOSC52 {
		t.Fatal("clipboard.use_osc52 defaults on; it must default off (untrusted output)")
	}
	if cfg.Clipboard.OSC52Paste {
		t.Fatal("clipboard.osc52_paste defaults on; a child reading the host clipboard must default off")
	}
}

func TestClipboardSection(t *testing.T) {
	t.Parallel()
	cfg, diags := loadBody(t, `{"clipboard": {"use_osc52": true}}`)
	if len(diags) != 0 {
		t.Fatalf("diagnostics = %v", diags)
	}
	if !cfg.Clipboard.UseOSC52 {
		t.Fatal("clipboard.use_osc52 was not read")
	}
	if cfg, diags := loadBody(t, `{"clipboard": {"osc52_paste": true}}`); len(diags) != 0 || !cfg.Clipboard.OSC52Paste {
		t.Fatalf("clipboard.osc52_paste = %v, %v", cfg.Clipboard.OSC52Paste, diags)
	}
	if _, diags := loadBody(t, `{"clipboard": {"nonsense": true}}`); len(diags) == 0 {
		t.Fatal("an unknown clipboard key was accepted")
	}
}

func TestClipboardProjectLayerRefused(t *testing.T) {
	t.Parallel()
	cfg, diags := loadLayers(t, `{}`, `{"clipboard": {"use_osc52": true, "osc52_paste": true}}`)
	if cfg.Clipboard.UseOSC52 || cfg.Clipboard.OSC52Paste {
		t.Fatalf("project clipboard applied: %+v", cfg.Clipboard)
	}
	if len(diags) != 1 || diags[0].Key != "clipboard" || !strings.Contains(diags[0].Message, "project config cannot set clipboard") {
		t.Fatalf("diags = %+v, want project clipboard refusal", diags)
	}
}
