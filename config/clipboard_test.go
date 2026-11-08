package config

import "testing"

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
	// Unknown keys are diagnosed, the known ignored ones are not.
	if _, diags := loadBody(t, `{"clipboard": {"use_osc52": true, "use_system_clipboard": true}}`); len(diags) != 0 {
		t.Fatalf("ignored key diagnosed: %v", diags)
	}
	if _, diags := loadBody(t, `{"clipboard": {"nonsense": true}}`); len(diags) == 0 {
		t.Fatal("an unknown clipboard key was accepted")
	}
}
