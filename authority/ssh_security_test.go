//go:build unix

package authority_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"

	"github.com/tbxark/gvim/authority"
)

func securityOptions(srv *fakeSSH, knownHostsPath string) authority.SSHOptions {
	return authority.SSHOptions{
		Spec:            authority.RemoteSpec{User: "u", Host: "127.0.0.1", Port: srv.port(), Path: "/x"},
		KnownHostsPath:  knownHostsPath,
		Password:        func(context.Context) (string, error) { return "pw", nil },
		DisableAgent:    true,
		Timeout:         5 * time.Second,
		HostKeyCallback: nil,
	}
}

func connectSecurity(t *testing.T, opts authority.SSHOptions) (*authority.Authority, error) {
	t.Helper()
	t.Setenv("SSH_AUTH_SOCK", "")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return authority.ConnectSSH(ctx, opts)
}

func knownHostsLine(t *testing.T, srv *fakeSSH) string {
	t.Helper()
	// The fake server's port is dynamic; build the real line with it.
	addr := knownhosts.Normalize("127.0.0.1:" + itoa(srv.port()))
	return knownhosts.Line([]string{addr}, srv.signer.PublicKey())
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

func TestKnownHostsPolicyTransitions(t *testing.T) {
	// strict: an empty known_hosts refuses the unknown host clearly.
	strictSrv := newFakeSSH(t)
	dir := t.TempDir()
	strictPath := filepath.Join(dir, "known_hosts")
	if err := os.WriteFile(strictPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	opts := securityOptions(strictSrv, strictPath)
	opts.KnownHostsPolicy = "strict"
	if _, err := connectSecurity(t, opts); err == nil || !strings.Contains(err.Error(), "not known") {
		t.Fatalf("strict unknown-host error = %v", err)
	}
	if data, _ := os.ReadFile(strictPath); len(data) != 0 {
		t.Fatalf("strict wrote known_hosts: %q", data)
	}

	// accept-new: the missing file is created, the first-seen key recorded with a notice.
	acceptSrv := newFakeSSH(t)
	acceptPath := filepath.Join(t.TempDir(), "ssh", "known_hosts")
	opts = securityOptions(acceptSrv, acceptPath)
	opts.KnownHostsPolicy = "accept-new"
	a, err := connectSecurity(t, opts)
	if err != nil {
		t.Fatalf("accept-new first connect: %v", err)
	}
	notices := a.TakeNotices()
	if len(notices) == 0 || !strings.Contains(strings.Join(notices, "; "), "first-seen host key") {
		t.Fatalf("accept-new notices = %v", notices)
	}
	data, err := os.ReadFile(acceptPath)
	if err != nil || !strings.Contains(string(data), knownHostsLine(t, acceptSrv)) {
		t.Fatalf("known_hosts = %q, %v; want the recorded first-seen line", data, err)
	}
	_ = a.Close()
	// A second connect is already known: no first-seen notice and no new line.
	a2, err := connectSecurity(t, opts)
	if err != nil {
		t.Fatalf("accept-new second connect: %v", err)
	}
	if n := a2.TakeNotices(); len(n) != 0 {
		t.Fatalf("second accept-new notices = %v, want none", n)
	}
	_ = a2.Close()

	// A changed key is always refused, even under accept-new, and the file is unchanged.
	_, otherPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	otherSigner, err := ssh.NewSignerFromKey(otherPriv)
	if err != nil {
		t.Fatal(err)
	}
	addr := knownhosts.Normalize("127.0.0.1:" + itoa(acceptSrv.port()))
	badLine := knownhosts.Line([]string{addr}, otherSigner.PublicKey())
	if err := os.WriteFile(acceptPath, []byte(badLine+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := connectSecurity(t, opts); err == nil || !strings.Contains(err.Error(), "HOST KEY CHANGED") {
		t.Fatalf("changed-key error = %v", err)
	}
	if data, _ := os.ReadFile(acceptPath); string(data) != badLine+"\n" {
		t.Fatalf("changed key rewrote known_hosts: %q", data)
	}

	// off is not a policy: it is refused before dialing.
	offSrv := newFakeSSH(t)
	off := securityOptions(offSrv, filepath.Join(t.TempDir(), "known_hosts"))
	off.KnownHostsPolicy = "off"
	if _, err := connectSecurity(t, off); err == nil || !strings.Contains(err.Error(), "not supported") {
		t.Fatalf("off policy error = %v", err)
	}
}

func TestHostKeyAlgorithmsParseOrderAndNegotiation(t *testing.T) {
	def, err := authority.ParseHostKeyAlgorithms(nil)
	if err != nil || strings.Join(def, ",") != strings.Join(authority.DefaultHostKeyAlgorithms, ",") {
		t.Fatalf("default algorithms = %v, %v", def, err)
	}
	got, err := authority.ParseHostKeyAlgorithms([]string{"rsa-sha2-512", "ssh-ed25519", "rsa-sha2-512"})
	if err != nil || strings.Join(got, ",") != "rsa-sha2-512,ssh-ed25519" {
		t.Fatalf("ordered algorithms = %v, %v", got, err)
	}
	if _, err := authority.ParseHostKeyAlgorithms([]string{"made-up"}); err == nil || !strings.Contains(err.Error(), "unknown host key algorithm") {
		t.Fatalf("unknown algorithm error = %v", err)
	}

	srv := newFakeSSH(t)
	opts := securityOptions(srv, filepath.Join(t.TempDir(), "known_hosts"))
	opts.KnownHostsPolicy = "accept-new"
	opts.HostKeyAlgorithms = []string{"ssh-ed25519"}
	a, err := connectSecurity(t, opts)
	if err != nil {
		t.Fatalf("ed25519-only connect: %v", err)
	}
	defer func() { _ = a.Close() }()
	if detail := a.State().Detail; !strings.Contains(detail, "host key ssh-ed25519") || !strings.Contains(detail, "known_hosts accept-new") {
		t.Fatalf("connection detail = %q", detail)
	}
	// A preference list without the server's algorithm fails the handshake; the error
	// still names the connection instead of hanging.
	opts2 := securityOptions(srv, filepath.Join(t.TempDir(), "known_hosts"))
	opts2.KnownHostsPolicy = "accept-new"
	opts2.HostKeyAlgorithms = []string{"rsa-sha2-512"}
	if _, err := connectSecurity(t, opts2); err == nil || !strings.Contains(err.Error(), "no common algorithm") {
		t.Fatalf("algorithm mismatch error = %v", err)
	}
	opts3 := securityOptions(srv, filepath.Join(t.TempDir(), "known_hosts"))
	opts3.HostKeyAlgorithms = []string{"not-a-real-algorithm"}
	if _, err := connectSecurity(t, opts3); err == nil || !strings.Contains(err.Error(), "unknown host key algorithm") {
		t.Fatalf("bad configured algorithm error = %v", err)
	}
}

type fakePasswordStore struct {
	mu       sync.Mutex
	password string
	has      bool
	lookErr  error
	storeErr error
	looked   []string
	stored   []string
}

func (f *fakePasswordStore) Lookup(_ context.Context, host, user string) (string, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.looked = append(f.looked, user+"@"+host)
	if f.lookErr != nil {
		return "", false, f.lookErr
	}
	return f.password, f.has, nil
}

func (f *fakePasswordStore) Store(_ context.Context, host, user, password string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.storeErr != nil {
		return f.storeErr
	}
	f.stored = append(f.stored, user+"@"+host+"="+password)
	return nil
}

func TestPasswordKeyringConsent(t *testing.T) {
	srv := newFakeSSH(t)
	fixed := ssh.FixedHostKey(srv.signer.PublicKey())

	// Retrieve: the stored password authenticates before the UI callback (which is nil).
	store := &fakePasswordStore{password: "pw", has: true}
	opts := securityOptions(srv, "")
	opts.HostKeyCallback = fixed
	opts.Password = nil
	opts.PasswordStore = store
	a, err := connectSecurity(t, opts)
	if err != nil {
		t.Fatalf("keyring retrieve connect: %v", err)
	}
	_ = a.Close()
	store.mu.Lock()
	looked := append([]string(nil), store.looked...)
	store.mu.Unlock()
	if len(looked) != 1 || looked[0] != "u@127.0.0.1" {
		t.Fatalf("keyring lookups = %v", looked)
	}

	// Store knob off: the accepted UI password is not stored.
	store = &fakePasswordStore{}
	opts = securityOptions(srv, "")
	opts.HostKeyCallback = fixed
	opts.PasswordStore = store
	opts.StorePassword = false
	a, err = connectSecurity(t, opts)
	if err != nil {
		t.Fatalf("no-store connect: %v", err)
	}
	_ = a.Close()
	if len(store.stored) != 0 {
		t.Fatalf("stored without consent: %v", store.stored)
	}

	// Store knob on: store after the accepted password and queue an honest notice.
	opts.StorePassword = true
	a, err = connectSecurity(t, opts)
	if err != nil {
		t.Fatalf("store connect: %v", err)
	}
	if len(store.stored) != 1 || store.stored[0] != "u@127.0.0.1=pw" {
		t.Fatalf("stored = %v", store.stored)
	}
	if notices := a.TakeNotices(); len(notices) == 0 || !strings.Contains(strings.Join(notices, "; "), "stored the accepted password") {
		t.Fatalf("store notices = %v", notices)
	}
	_ = a.Close()

	// An unavailable store is reported honestly, never as saved.
	store = &fakePasswordStore{storeErr: authority.ErrPasswordStoreUnavailable}
	opts.PasswordStore = store
	opts.StorePassword = true
	a, err = connectSecurity(t, opts)
	if err != nil {
		t.Fatalf("unavailable store connect: %v", err)
	}
	notices := strings.Join(a.TakeNotices(), "; ")
	if !strings.Contains(notices, "not stored in the OS keyring") {
		t.Fatalf("unavailable store notices = %q", notices)
	}
	_ = a.Close()

	// A lookup error falls through to the prompt instead of failing the dial, and the
	// failure is surfaced honestly as a notice (never silently swallowed).
	store = &fakePasswordStore{lookErr: errors.New("keyring locked")}
	opts = securityOptions(srv, "")
	opts.HostKeyCallback = fixed
	opts.PasswordStore = store
	if a, err := connectSecurity(t, opts); err != nil {
		t.Fatalf("lookup error fell through wrong: %v", err)
	} else {
		notices := strings.Join(a.TakeNotices(), "; ")
		if !strings.Contains(notices, "could not read the OS-keyring password") || !strings.Contains(notices, "keyring locked") {
			t.Fatalf("lookup-error notices = %q", notices)
		}
		_ = a.Close()
	}
}
