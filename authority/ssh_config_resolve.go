package authority

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

// The resolved connection of one dial: the target alias after ~/.ssh/config, the
// optional one-hop jump, and the identity/agent inputs. It is recomputed on every
// dial (config edits and reconnects take effect) and kept on the backend for
// Label/session forwarding.
type sshResolved struct {
	host         string
	port         int
	user         string
	identity     []string
	agentSock    string
	forwardAgent bool
	jump         *sshJump
}

// sshJump is the one supported ProxyJump hop.
type sshJump struct {
	host string
	port int
	user string
}

// resolveDial applies the bounded ssh config to the launch target. Target fields
// that the user spelled (user@host, ssh://host:port) win over the config; the
// host name is always the config alias's HostName when one is configured.
func (b *sshBackend) resolveDial() (sshResolved, error) {
	spec := b.opts.Spec
	res := sshResolved{
		host:      spec.Host,
		port:      spec.Port,
		user:      spec.User,
		identity:  append([]string(nil), b.opts.IdentityFiles...),
		agentSock: os.Getenv("SSH_AUTH_SOCK"),
	}
	cfg, err := loadSSHConfig(b.opts.ConfigPath)
	if err != nil {
		return sshResolved{}, err
	}
	if cfg != nil {
		v := cfg.resolve(spec.Host)
		if v.HostName != "" {
			res.host = v.HostName
		}
		if v.User != "" && !spec.UserExplicit {
			res.user = v.User
		}
		if v.Port != 0 && !spec.PortExplicit {
			res.port = v.Port
		}
		if len(b.opts.IdentityFiles) == 0 && len(v.IdentityFiles) > 0 {
			res.identity = v.IdentityFiles
		}
		switch strings.ToLower(v.IdentityAgent) {
		case "":
		case "none":
			res.agentSock = ""
		case "ssh_auth_sock":
			// The environment socket, already the default.
		default:
			res.agentSock = expandSSHPath(v.IdentityAgent, homeOrEmpty())
		}
		if v.ForwardAgent != nil {
			res.forwardAgent = *v.ForwardAgent
		}
		if pj := strings.TrimSpace(v.ProxyJump); pj != "" && !strings.EqualFold(pj, "none") {
			jump, err := parseSSHJump(pj, cfg)
			if err != nil {
				return sshResolved{}, err
			}
			res.jump = jump
		}
	}
	if b.opts.DisableAgent {
		res.agentSock = ""
	}
	if b.opts.ForwardAgent {
		res.forwardAgent = true
	}
	if res.port == 0 {
		res.port = DefaultSSHPort
	}
	return res, nil
}

// parseSSHJump parses ProxyJump: one hop only, [user@]host[:port]. The jump alias
// is resolved through the same config (HostName/User/Port); a jump that itself
// has ProxyJump is a clear error rather than a silent second hop.
func parseSSHJump(v string, cfg *sshConfig) (*sshJump, error) {
	if strings.Contains(v, ",") {
		return nil, fmt.Errorf("ssh config ProxyJump %q: only one jump hop is supported", v)
	}
	alias, user, port, err := splitSSHJump(v)
	if err != nil {
		return nil, err
	}
	j := &sshJump{host: alias, user: user, port: port}
	if cfg != nil {
		jv := cfg.resolve(alias)
		if jv.HostName != "" {
			j.host = jv.HostName
		}
		if j.user == "" && jv.User != "" {
			j.user = jv.User
		}
		if j.port == 0 && jv.Port != 0 {
			j.port = jv.Port
		}
		if pj := strings.TrimSpace(jv.ProxyJump); pj != "" && !strings.EqualFold(pj, "none") {
			return nil, fmt.Errorf("ssh config ProxyJump %q resolves to another jump %q: nested jumps are not supported", v, pj)
		}
	}
	if j.port == 0 {
		j.port = DefaultSSHPort
	}
	return j, nil
}

// splitSSHJump splits [user@]host[:port], accepting a bracketed IPv6 host.
func splitSSHJump(s string) (host, user string, port int, err error) {
	if at := strings.LastIndex(s, "@"); at >= 0 {
		user, s = s[:at], s[at+1:]
		if user == "" {
			return "", "", 0, fmt.Errorf("ssh config ProxyJump: empty user")
		}
	}
	host, port, err = splitHostPort(s)
	if err != nil {
		return "", "", 0, fmt.Errorf("ssh config ProxyJump: %w", err)
	}
	if host == "" {
		return "", "", 0, fmt.Errorf("ssh config ProxyJump: empty host")
	}
	return host, user, port, nil
}

func homeOrEmpty() string {
	home, _ := os.UserHomeDir()
	return home
}

// openAgent returns the local agent for authentication and forwarding: the
// injected Agent first (tests/embedding), else SSH_AUTH_SOCK. The returned
// net.Conn, when non-nil, must be closed by the backend.
func (b *sshBackend) openAgent(res sshResolved) (agent.Agent, net.Conn, error) {
	if b.opts.Agent != nil {
		return b.opts.Agent, nil, nil
	}
	if res.agentSock == "" {
		return nil, nil, nil
	}
	conn, err := net.Dial("unix", res.agentSock)
	if err != nil {
		return nil, nil, fmt.Errorf("ssh: connect to ssh-agent %s: %w", res.agentSock, err)
	}
	return agent.NewClient(conn), conn, nil
}

// authMethodsResolved builds the authentication order for a resolved target. It
// is used for the target and for the jump hop (the same credentials; a
// jump-specific IdentityFile is a recorded residual).
func (b *sshBackend) authMethodsResolved(ctx context.Context, res sshResolved, ag agent.Agent) ([]ssh.AuthMethod, error) {
	var methods []ssh.AuthMethod
	if ag != nil {
		methods = append(methods, ssh.PublicKeysCallback(ag.Signers))
	}
	files := res.identity
	if len(files) == 0 {
		if home, herr := os.UserHomeDir(); herr == nil {
			files = existingIdentityFiles(home + "/.ssh")
		}
	}
	passphrase := func(ctx context.Context, path string) (string, error) {
		b.mu.Lock()
		stored := b.passphrase
		b.mu.Unlock()
		if stored != "" {
			return stored, nil
		}
		if b.opts.Passphrase != nil {
			return b.opts.Passphrase(ctx, path)
		}
		return "", ErrNeedsPassphrase
	}
	var signers []ssh.Signer
	for _, path := range files {
		signer, err := parseIdentity(path, passphrase, ctx)
		if err != nil {
			if errors.Is(err, ErrNeedsPassphrase) {
				b.mu.Lock()
				b.needsPassphrase = true
				b.mu.Unlock()
			}
			if len(b.opts.IdentityFiles) > 0 {
				return nil, err
			}
			continue
		}
		signers = append(signers, signer)
	}
	if len(signers) > 0 {
		methods = append(methods, ssh.PublicKeys(signers...))
	}
	b.mu.Lock()
	stored := b.password
	hasChallenge := b.keyboardChallenge != nil
	b.mu.Unlock()
	if stored != "" {
		methods = append(methods, b.passwordAuth("ui", func() (string, error) { return stored, nil }))
	} else if b.opts.PasswordStore != nil {
		// The OS keyring is tried before the UI prompt: a stored password is not a new
		// secret and does not need the interactive callback.
		if pw, ok, err := b.opts.PasswordStore.Lookup(res.host, res.user); err == nil && ok && pw != "" {
			methods = append(methods, b.passwordAuth("keyring", func() (string, error) { return pw, nil }))
		}
	}
	if b.opts.Password != nil {
		methods = append(methods, b.passwordAuth("prompt", func() (string, error) { return b.opts.Password(ctx) }))
	}
	if b.opts.KeyboardInteractive != nil || hasChallenge {
		methods = append(methods, ssh.KeyboardInteractive(b.keyboardAuth(ctx)))
	}
	if len(methods) == 0 {
		return nil, errors.New("ssh: no authentication method (no agent, no usable identity file, no password callback, no keyboard-interactive callback)")
	}
	return methods, nil
}

// passwordAuth wraps a password credential so the backend knows one was used (and which
// value/source) for the consent-gated keyring store after a successful handshake.
func (b *sshBackend) passwordAuth(source string, fn func() (string, error)) ssh.AuthMethod {
	return ssh.PasswordCallback(func() (string, error) {
		pw, err := fn()
		if err == nil && pw != "" {
			b.mu.Lock()
			b.usedPassword, b.usedPasswordSource = pw, source
			b.mu.Unlock()
		}
		return pw, err
	})
}
