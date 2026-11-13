package authority

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// TestKnownHostsOtherKeyTypeIsNotReportedAsChanged: a host recorded with a key of one type
// that presents a key of another type is not a changed key; the error says so and names
// the setting that selects the recorded type, instead of crying man-in-the-middle.
func TestKnownHostsOtherKeyTypeIsNotReportedAsChanged(t *testing.T) {
	ecKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ecPub, err := ssh.NewPublicKey(&ecKey.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	edPub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	edSSH, err := ssh.NewPublicKey(edPub)
	if err != nil {
		t.Fatal(err)
	}
	other, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	otherSSH, err := ssh.NewPublicKey(other)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "known_hosts")
	line := knownhosts.Line([]string{knownhosts.Normalize("example.test:22")}, ecPub) + "\n"
	if err := os.WriteFile(path, []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	cb, err := newKnownHostsCallback(path, KnownHostsStrict, nil)
	if err != nil {
		t.Fatal(err)
	}
	addr := &net.TCPAddr{IP: net.IPv4(192, 0, 2, 1), Port: 22}
	err = cb("example.test:22", addr, edSSH)
	if err == nil || strings.Contains(err.Error(), "CHANGED") || !strings.Contains(err.Error(), "host_key_algorithms") {
		t.Fatalf("a key of another type = %v, want a hint to set ssh.host_key_algorithms and no CHANGED alarm", err)
	}
	// A second key of the recorded type is a real change.
	line = knownhosts.Line([]string{knownhosts.Normalize("example.test:22")}, edSSH) + "\n"
	if err := os.WriteFile(path, []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	cb, err = newKnownHostsCallback(path, KnownHostsStrict, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := cb("example.test:22", addr, otherSSH); err == nil || !strings.Contains(err.Error(), "HOST KEY CHANGED") {
		t.Fatalf("a different key of the recorded type = %v, want HOST KEY CHANGED", err)
	}
}
