package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSSHConfigParsing(t *testing.T) {
	cfg := Defaults()
	if cfg.SSH.KnownHostsPolicy != "strict" || len(cfg.SSH.HostKeyAlgorithms) != 0 || cfg.SSH.Keyring {
		t.Fatalf("ssh defaults = %+v", cfg.SSH)
	}
	cfg, diags := loadBody(t, `{"ssh": {"known_hosts_policy": "accept-new", "host_key_algorithms": ["ssh-ed25519", "rsa-sha2-512"], "keyring": true}}`)
	if len(diags) != 0 {
		t.Fatalf("diags = %+v", diags)
	}
	if cfg.SSH.KnownHostsPolicy != "accept-new" || cfg.SSH.Keyring != true || strings.Join(cfg.SSH.HostKeyAlgorithms, ",") != "ssh-ed25519,rsa-sha2-512" {
		t.Fatalf("ssh = %+v", cfg.SSH)
	}
	cfg, diags = loadBody(t, `{"ssh": {"known_hosts_policy": "off"}}`)
	if len(diags) != 1 || diags[0].Key != "ssh.known_hosts_policy" || !strings.Contains(diags[0].Message, "not supported") || cfg.SSH.KnownHostsPolicy != "strict" {
		t.Fatalf("off = %+v, %+v; want a not-supported diagnostic and the strict default kept", cfg.SSH, diags)
	}
	for _, body := range []string{
		`{"ssh": {"known_hosts_policy": "sometimes"}}`,
		`{"ssh": {"host_key_algorithms": "ssh-ed25519"}}`,
		`{"ssh": {"host_key_algorithms": [""]}}`,
		`{"ssh": {"keyring": "yes"}}`,
		`{"ssh": {"unknown": true}}`,
	} {
		if _, diags := loadBody(t, body); len(diags) == 0 {
			t.Fatalf("accepted invalid ssh config %s", body)
		}
	}
}

func TestSSHConfigProjectLayerRefused(t *testing.T) {
	root := t.TempDir()
	projectDir := filepath.Join(root, ".gvim")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "config.json"), []byte(`{"ssh": {"known_hosts_policy": "off"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	userPath := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(userPath, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, diags, err := Load(Options{Root: root, UserFile: userPath})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, d := range diags {
		if d.Key == "ssh" && strings.Contains(d.Message, "project config cannot set ssh") {
			found = true
		}
	}
	if !found {
		t.Fatalf("project ssh diagnostics = %+v", diags)
	}
	if cfg.SSH.KnownHostsPolicy != "strict" {
		t.Fatalf("project layer changed the policy to %q", cfg.SSH.KnownHostsPolicy)
	}
}
