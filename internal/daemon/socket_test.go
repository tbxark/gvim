package daemon

import (
	"os"
	"strings"
	"testing"
)

// TestSocketPathRejectsUnsafeNames checks a daemon name is one safe file name, never a
// path that could escape the socket directory.
func TestSocketPathRejectsUnsafeNames(t *testing.T) {
	// daemonTestName gives a short HOME: the socket path length limit is real.
	daemonTestName(t)
	for _, name := range []string{"", ".", "..", "../x", "a/b", "a\\b", "a\x00b", strings.Repeat("a", 65)} {
		if _, err := SocketPath(name); err == nil {
			t.Errorf("SocketPath(%q) accepted an unsafe name", name)
		}
		if _, err := Attach(name); err == nil {
			t.Errorf("Attach(%q) accepted an unsafe name", name)
		}
	}
	if _, err := SocketPath("my-daemon_1"); err != nil {
		t.Errorf("SocketPath of a safe name: %v", err)
	}
}

// TestListenRefusesToReplaceNonSocket checks Listen never unlinks a regular file that
// happens to sit at the socket path.
func TestListenRefusesToReplaceNonSocket(t *testing.T) {
	name := daemonTestName(t)
	path, err := SocketPath(name)
	if err != nil {
		t.Fatal(err)
	}
	const content = "user data, not a socket"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Listen(name); err == nil || !strings.Contains(err.Error(), "not a socket") {
		t.Fatalf("Listen over a regular file = %v, want a non-socket refusal", err)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != content {
		t.Fatalf("the regular file was removed or changed: %q, %v", got, err)
	}
}

// TestListenReplacesStaleSocket checks a socket file left by a dead daemon is replaced.
func TestListenReplacesStaleSocket(t *testing.T) {
	name := daemonTestName(t)
	ln, _, err := Listen(name)
	if err != nil {
		t.Fatal(err)
	}
	if err := ln.Close(); err != nil {
		t.Fatal(err)
	}
	ln2, _, err := Listen(name)
	if err != nil {
		t.Fatalf("Listen over a stale socket: %v", err)
	}
	_ = ln2.Close()
}
