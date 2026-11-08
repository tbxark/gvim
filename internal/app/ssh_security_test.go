package app

import (
	"testing"

	"github.com/tbxark/gvim/config"
)

func TestSSHSecurityFromConfig(t *testing.T) {
	c := config.SSHConfig{
		KnownHostsPolicy:  "accept-new",
		HostKeyAlgorithms: []string{"ssh-ed25519", "rsa-sha2-512"},
		Keyring:           true,
	}
	got := sshSecurityFromConfig(c)
	if got.KnownHostsPolicy != "accept-new" || !got.Keyring || len(got.HostKeyAlgorithms) != 2 {
		t.Fatalf("mapped security = %+v", got)
	}
	c.HostKeyAlgorithms[0] = "mutated"
	if got.HostKeyAlgorithms[0] != "ssh-ed25519" {
		t.Fatal("the mapped algorithm list aliases the config slice")
	}
	empty := sshSecurityFromConfig(config.SSHConfig{})
	if empty.KnownHostsPolicy != "" || len(empty.HostKeyAlgorithms) != 0 || empty.Keyring {
		t.Fatalf("zero config mapped = %+v; the authority applies strict/safe defaults", empty)
	}
}
