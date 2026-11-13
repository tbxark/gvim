package authority

// SSH security policy for the pure-Go backend: known_hosts policy, host-key algorithm
// preferences, and the password-store (keyring) interface. Fresh delegates SSH to the
// system ssh(1), which applies OpenSSH's own StrictHostKeyChecking; gvim always verifies
// host keys itself and only exposes the bounded knobs below (docs/authority.md).

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// KnownHostsPolicy is what the transport does with an unknown host key.
type KnownHostsPolicy string

const (
	// KnownHostsStrict refuses a host whose key is not in known_hosts (the default).
	KnownHostsStrict KnownHostsPolicy = "strict"
	// KnownHostsAcceptNew records a first-seen host key in known_hosts after a clear
	// notice, then trusts it on later connections. A CHANGED key is always refused.
	KnownHostsAcceptNew KnownHostsPolicy = "accept-new"
)

// ParseKnownHostsPolicy validates the configured policy. Empty means strict.
func ParseKnownHostsPolicy(s string) (KnownHostsPolicy, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", string(KnownHostsStrict):
		return KnownHostsStrict, nil
	case string(KnownHostsAcceptNew):
		return KnownHostsAcceptNew, nil
	case "off":
		return "", errors.New("ssh: known_hosts policy \"off\" is not supported: host keys are always verified (use strict or accept-new)")
	}
	return "", fmt.Errorf("ssh: unknown known_hosts policy %q (use strict or accept-new)", s)
}

// DefaultHostKeyAlgorithms is the safe preference order: modern Ed25519, ECDSA and
// SHA-2 RSA. The SHA-1 ssh-rsa and DSA algorithms are never in the default.
var DefaultHostKeyAlgorithms = []string{
	"ssh-ed25519",
	"ecdsa-sha2-nistp256",
	"ecdsa-sha2-nistp384",
	"ecdsa-sha2-nistp521",
	"rsa-sha2-512",
	"rsa-sha2-256",
}

// knownHostKeyAlgorithms is the set x/crypto/ssh can negotiate for a server host key,
// with the certificate variants. It is the allowlist for configuration errors: an
// unknown name is refused before dialing instead of surfacing as "no common algorithm".
var knownHostKeyAlgorithms = map[string]bool{
	"ssh-ed25519":                              true,
	"ssh-ed25519-cert-v01@openssh.com":         true,
	"ecdsa-sha2-nistp256":                      true,
	"ecdsa-sha2-nistp256-cert-v01@openssh.com": true,
	"ecdsa-sha2-nistp384":                      true,
	"ecdsa-sha2-nistp384-cert-v01@openssh.com": true,
	"ecdsa-sha2-nistp521":                      true,
	"ecdsa-sha2-nistp521-cert-v01@openssh.com": true,
	"rsa-sha2-512":                             true,
	"rsa-sha2-512-cert-v01@openssh.com":        true,
	"rsa-sha2-256":                             true,
	"rsa-sha2-256-cert-v01@openssh.com":        true,
	"ssh-rsa":                                  true,
	"ssh-rsa-cert-v01@openssh.com":             true,
	"ssh-dss":                                  true,
	"ssh-dss-cert-v01@openssh.com":             true,
}

// ParseHostKeyAlgorithms validates and orders the configured server host-key
// preferences. An empty list uses DefaultHostKeyAlgorithms; an unknown name is a clear
// error naming the supported set; duplicates are removed keeping the first occurrence.
func ParseHostKeyAlgorithms(names []string) ([]string, error) {
	if len(names) == 0 {
		return append([]string(nil), DefaultHostKeyAlgorithms...), nil
	}
	out := make([]string, 0, len(names))
	seen := map[string]bool{}
	for _, name := range names {
		name = strings.ToLower(strings.TrimSpace(name))
		if name == "" {
			return nil, errors.New("ssh: empty host key algorithm name")
		}
		if !knownHostKeyAlgorithms[name] {
			return nil, fmt.Errorf("ssh: unknown host key algorithm %q (supported: %s)", name, strings.Join(sortedHostKeyAlgorithmNames(), ", "))
		}
		if seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	return out, nil
}

func sortedHostKeyAlgorithmNames() []string {
	return slices.Sorted(maps.Keys(knownHostKeyAlgorithms))
}

// newKnownHostsCallback builds the host-key check for path under policy. A missing file
// is a clear error for strict; accept-new creates an empty file, then records first-seen
// hosts. A changed key is always refused.
func newKnownHostsCallback(path string, policy KnownHostsPolicy, notice func(string)) (ssh.HostKeyCallback, error) {
	switch policy {
	case KnownHostsStrict, KnownHostsAcceptNew:
	default:
		return nil, fmt.Errorf("ssh: unsupported known_hosts policy %q", policy)
	}
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("ssh: no known_hosts path")
	}
	if policy == KnownHostsAcceptNew {
		if err := ensureKnownHostsFile(path); err != nil {
			return nil, fmt.Errorf("ssh: prepare %s for accept-new: %w", path, err)
		}
	}
	cb, err := knownhosts.New(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("ssh: no known_hosts file at %s: add the host key with ssh(1), or set ssh.known_hosts_policy to accept-new", path)
		}
		return nil, fmt.Errorf("ssh: read %s: %w", path, err)
	}
	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		err := cb(hostname, remote, key)
		if err == nil {
			return nil
		}
		var keyErr *knownhosts.KeyError
		if !errors.As(err, &keyErr) {
			return fmt.Errorf("ssh: host key of %s rejected by %s: %w", hostname, path, err)
		}
		if len(keyErr.Want) > 0 && !hasKeyType(keyErr.Want, key.Type()) {
			// known_hosts lists the host under other key types only: the server may offer
			// both, and this is a preference problem, not evidence of a changed key.
			return fmt.Errorf("ssh: %s presented a %s key but %s records only %s for it; this is not a changed key: set ssh.host_key_algorithms to prefer the recorded type", hostname, key.Type(), path, wantTypes(keyErr.Want))
		}
		if len(keyErr.Want) > 0 {
			return fmt.Errorf("ssh: HOST KEY CHANGED for %s: the key in %s does not match the server (possible man-in-the-middle); refusing (verify and remove the stale line before retrying)", hostname, path)
		}
		if policy == KnownHostsAcceptNew {
			normalized := knownhosts.Normalize(hostname)
			if err := appendKnownHost(path, normalized, key); err != nil {
				return fmt.Errorf("ssh: first-seen host %s could not be recorded in %s: %w", hostname, path, err)
			}
			if notice != nil {
				notice(fmt.Sprintf("SSH: first-seen host key for %s accepted and recorded in %s (known_hosts policy accept-new)", hostname, path))
			}
			return nil
		}
		return fmt.Errorf("ssh: host key of %s is not known: add it to %s with ssh-keyscan, or set ssh.known_hosts_policy to accept-new", hostname, path)
	}, nil
}

// hasKeyType reports whether any recorded key has the given type.
func hasKeyType(want []knownhosts.KnownKey, keyType string) bool {
	for _, k := range want {
		if k.Key.Type() == keyType {
			return true
		}
	}
	return false
}

// wantTypes lists the key types of the recorded keys, for an error message.
func wantTypes(want []knownhosts.KnownKey) string {
	var types []string
	for _, k := range want {
		if t := k.Key.Type(); !slices.Contains(types, t) {
			types = append(types, t)
		}
	}
	return strings.Join(types, ", ")
}

func ensureKnownHostsFile(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	return f.Close()
}

func appendKnownHost(path, hostname string, key ssh.PublicKey) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	_, err = f.WriteString(knownhosts.Line([]string{hostname}, key) + "\n")
	return err
}

// PasswordStore is the optional OS keyring for SSH host passwords. Lookup is tried
// before the UI prompt when enabled; Store is called only when the user config set
// ssh.keyring and the server accepted the password (never without that consent).
type PasswordStore interface {
	// Lookup returns the stored password for user@host, if any. ctx cancels the backing
	// keyring command.
	Lookup(ctx context.Context, host, user string) (password string, ok bool, err error)
	// Store records password for user@host. ctx cancels the backing keyring command.
	Store(ctx context.Context, host, user, password string) error
}

// ErrPasswordStoreUnavailable is returned by the no-op store, and by OS backends that
// are not implemented in this build.
var ErrPasswordStoreUnavailable = errors.New("authority: no OS keyring integration is available in this build")

// NoopPasswordStore is the default: no lookup hit and a clear unavailable error on
// store, so the caller never claims a password was saved when it was not.
type NoopPasswordStore struct{}

func (NoopPasswordStore) Lookup(context.Context, string, string) (string, bool, error) {
	return "", false, nil
}
func (NoopPasswordStore) Store(context.Context, string, string, string) error {
	return ErrPasswordStoreUnavailable
}

// SystemPasswordStore returns the platform password store when its tool is installed
// (macOS security, Linux secret-tool); otherwise the no-op store, whose Store reports
// ErrPasswordStoreUnavailable honestly. The app calls this only when ssh.keyring is on.
func SystemPasswordStore() PasswordStore { return systemPasswordStore() }
