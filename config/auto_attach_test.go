package config

import "testing"

func TestAutoAttachConfig(t *testing.T) {
	if cfg, diags := loadBody(t, `{}`); len(diags) != 0 || cfg.AutoAttach {
		t.Fatalf("auto_attach default = %v, %v; want off", cfg.AutoAttach, diags)
	}
	cfg, diags := loadBody(t, `{"auto_attach": true}`)
	if len(diags) != 0 || !cfg.AutoAttach {
		t.Fatalf("auto_attach true = %v, %v", cfg.AutoAttach, diags)
	}
	if cfg, diags := loadBody(t, `{"auto_attach": false}`); len(diags) != 0 || cfg.AutoAttach {
		t.Fatalf("auto_attach false = %v, %v", cfg.AutoAttach, diags)
	}
	if _, diags := loadBody(t, `{"auto_attach": "yes"}`); len(diags) == 0 {
		t.Fatal("a non-boolean auto_attach was accepted")
	}
}
