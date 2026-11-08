package authority

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSSHConfigResolve(t *testing.T) {
	text := `# global defaults
IdentityFile ~/.ssh/global_key

Host dev
  HostName dev.example.com
  User alice
  Port 2222
  IdentityFile ~/.ssh/id_dev
  ForwardAgent yes
  IdentityAgent ~/.ssh/agent.sock

Host dev-alias
  HostName first.example.com
  HostName second.example.com

Host *.corp !bad.corp
  User corp

Host proxied
  HostName target.example.com
  ProxyJump jump

Host *
  User fallback
  IdentityFile ~/.ssh/id_default
`
	cfg, err := parseSSHConfig(text)
	if err != nil {
		t.Fatal(err)
	}
	home, _ := os.UserHomeDir()
	dev := cfg.resolve("dev")
	if !dev.Matched || dev.HostName != "dev.example.com" || dev.User != "alice" || dev.Port != 2222 {
		t.Fatalf("dev = %+v", dev)
	}
	wantIdentity := []string{
		filepath.Join(home, ".ssh", "global_key"),
		filepath.Join(home, ".ssh", "id_dev"),
		filepath.Join(home, ".ssh", "id_default"),
	}
	if strings.Join(dev.IdentityFiles, ",") != strings.Join(wantIdentity, ",") {
		t.Fatalf("identity = %v, want %v", dev.IdentityFiles, wantIdentity)
	}
	if dev.ForwardAgent == nil || !*dev.ForwardAgent {
		t.Fatalf("ForwardAgent = %v", dev.ForwardAgent)
	}
	if dev.IdentityAgent != filepath.Join(home, ".ssh", "agent.sock") {
		t.Fatalf("IdentityAgent = %q", dev.IdentityAgent)
	}
	if got := cfg.resolve("dev-alias").HostName; got != "first.example.com" {
		t.Fatalf("first-obtained HostName = %q", got)
	}
	if got := cfg.resolve("x.corp").User; got != "corp" {
		t.Fatalf("x.corp User = %q, want corp", got)
	}
	proxied := cfg.resolve("proxied")
	if proxied.ProxyJump != "jump" || proxied.HostName != "target.example.com" {
		t.Fatalf("proxied = %+v", proxied)
	}
	unknown := cfg.resolve("nope")
	if unknown.User != "fallback" || unknown.ForwardAgent != nil {
		t.Fatalf("unknown = %+v", unknown)
	}
}

func TestSSHConfigTokensAndLimits(t *testing.T) {
	cfg, err := parseSSHConfig("Host web\n  HostName %h.internal\n  User deploy\n  Port 2200\n")
	if err != nil {
		t.Fatal(err)
	}
	got := cfg.resolve("web")
	if got.HostName != "web.internal" || got.User != "deploy" || got.Port != 2200 {
		t.Fatalf("tokens = %+v", got)
	}
	if _, err := parseSSHConfig("Host\n"); err == nil {
		t.Fatal("Host with no pattern was accepted")
	}
	if _, err := parseSSHConfig("Host " + strings.Repeat("x", sshConfigMaxLine) + "\n"); err == nil {
		t.Fatal("an over-long line was accepted")
	}
	if _, err := parseSSHConfig(strings.Repeat("Host h\n", sshConfigMaxBlocks+1)); err == nil {
		t.Fatal("too many Host blocks were accepted")
	}
	if cfg, err := loadSSHConfig(filepath.Join(t.TempDir(), "missing")); err != nil || cfg != nil {
		t.Fatalf("missing config = %v, %v; want nil, nil", cfg, err)
	}
	if cfg, err := loadSSHConfig(""); err != nil || cfg != nil {
		t.Fatalf("disabled config = %v, %v", cfg, err)
	}
	big := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(big, []byte(strings.Repeat("#", sshConfigMaxBytes+1)), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadSSHConfig(big); err == nil {
		t.Fatal("an oversize config was accepted")
	}
}

func TestSSHConfigTargetOverrides(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config")
	text := "Host alias\n  HostName real.example.com\n  User cfguser\n  Port 2201\n  IdentityFile /tmp/cfg_key\n"
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	b := &sshBackend{opts: SSHOptions{Spec: RemoteSpec{Host: "alias", User: "", Path: "/x"}, ConfigPath: path}}
	res, err := b.resolveDial()
	if err != nil {
		t.Fatal(err)
	}
	if res.host != "real.example.com" || res.user != "cfguser" || res.port != 2201 {
		t.Fatalf("config-only target = %+v", res)
	}
	b.opts.Spec = RemoteSpec{Host: "alias", User: "cli", Port: 2022, Path: "/x", UserExplicit: true, PortExplicit: true}
	res, err = b.resolveDial()
	if err != nil {
		t.Fatal(err)
	}
	if res.host != "real.example.com" || res.user != "cli" || res.port != 2022 {
		t.Fatalf("explicit target = %+v", res)
	}
	b = &sshBackend{opts: SSHOptions{Spec: RemoteSpec{Host: "h", User: "u", Path: "/x"}}}
	res, err = b.resolveDial()
	if err != nil || res.host != "h" || res.user != "u" || res.port != DefaultSSHPort || res.jump != nil {
		t.Fatalf("no-config fallback = %+v, %v", res, err)
	}
}

func TestSSHProxyJumpOneHop(t *testing.T) {
	cfg, err := parseSSHConfig("Host target\n  HostName real.example.com\n  ProxyJump jumpuser@bastion:2222\n")
	if err != nil {
		t.Fatal(err)
	}
	j, err := parseSSHJump(cfg.resolve("target").ProxyJump, cfg)
	if err != nil || j == nil || j.host != "bastion" || j.user != "jumpuser" || j.port != 2222 {
		t.Fatalf("jump = %+v, %v", j, err)
	}
	if _, err := parseSSHJump("a,b", cfg); err == nil {
		t.Fatal("a multi-hop ProxyJump list was accepted")
	}
	nested, err := parseSSHConfig("Host a\n  HostName a.example\n  ProxyJump b\nHost b\n  HostName b.example\n  ProxyJump c\n")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := parseSSHJump("b", nested); err == nil || !strings.Contains(err.Error(), "nested") {
		t.Fatalf("nested jump error = %v", err)
	}
}
