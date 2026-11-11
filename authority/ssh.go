package authority

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"

	"github.com/tbxark/gvim/plugin/hostos"
)

// The SSH backend of the authority slot: pure-Go x/crypto/ssh. It provides one-shot exec,
// long-running stdio children, the integrated terminal (exec channel + PTY request) and
// the remote FileSystem (bounded exec helper commands, ssh_files.go). Connection state
// feeds the status row and reconnect-on-activate.
//
// The SSH client carries no path translation: the editor's paths are the remote
// host's. Authentication: ssh-agent (SSH_AUTH_SOCK), identity files (plain or with the
// injected passphrase callback) and the injected password callback, tried in that order. Host keys come from ~/.ssh/known_hosts (knownhosts.New)
// unless the caller injects a callback (tests use ssh.FixedHostKey); an unknown host is a
// clear error, never silently trusted.

// SSHOptions configure a connection.
type SSHOptions struct {
	Spec RemoteSpec
	// Timeout bounds the TCP connect and the SSH handshake. Zero: 15 s.
	Timeout time.Duration
	// IdentityFiles are the private keys to try, in order. Empty: the usual
	// ~/.ssh/id_ed25519, id_ecdsa and id_rsa that exist.
	IdentityFiles []string
	// DisableAgent turns ssh-agent authentication off (it is on by default through
	// SSH_AUTH_SOCK).
	DisableAgent bool
	// Password supplies a password when the server asks for one; nil disables password
	// auth. The callback may prompt the user.
	Password func(ctx context.Context) (string, error)
	// Passphrase supplies the passphrase of an encrypted identity file; nil skips the file.
	// A callback returning ErrNeedsPassphrase records the request and lets the UI prompt.
	Passphrase func(ctx context.Context, path string) (string, error)
	// KeyboardInteractive answers a keyboard-interactive challenge (2FA). A callback
	// returning ErrNeedsKeyboardInteractive records the challenge for the UI prompt.
	KeyboardInteractive func(ctx context.Context, name, instruction string, questions []string, echos []bool) ([]string, error)
	// KnownHostsPath is the known_hosts file; empty uses ~/.ssh/known_hosts.
	KnownHostsPath string
	// KnownHostsPolicy is "strict" (default), "accept-new" (record a first-seen host key
	// with a notice) or "off" (refused at connect: host keys are always verified).
	KnownHostsPolicy string
	// HostKeyAlgorithms is the ordered server host-key algorithm preference; empty uses
	// DefaultHostKeyAlgorithms. An unknown name fails the dial clearly.
	HostKeyAlgorithms []string
	// PasswordStore, when set, is tried for a host password before the UI prompt; with
	// StorePassword an accepted password is stored after a notice (user consent).
	PasswordStore PasswordStore
	// StorePassword lets an accepted password be stored in PasswordStore. Default false:
	// nothing is stored without the config knob.
	StorePassword bool
	// HostKeyCallback overrides the known_hosts check (tests).
	HostKeyCallback ssh.HostKeyCallback
	// ConfigPath is the OpenSSH user config to apply. Empty disables config reading
	// (embedding/tests); the app passes DefaultSSHConfigPath(), and a missing file is
	// not an error.
	ConfigPath string
	// ForwardAgent requests ssh-agent forwarding on this connection's sessions
	// (terminal and one-shot channels), in addition to an ssh config ForwardAgent yes.
	ForwardAgent bool
	// Agent overrides the local ssh-agent (tests and embedding); nil uses the
	// resolved IdentityAgent/SSH_AUTH_SOCK socket.
	Agent agent.Agent
}

// The interactive-auth sentinels are returned by the UI-facing callbacks when the
// credential needs the user: the backend records the request and the app prompts on the
// next activation/background retry. They are never dial errors.
var (
	ErrNeedsPassphrase          = errors.New("ssh: identity file passphrase needed")
	ErrNeedsKeyboardInteractive = errors.New("ssh: keyboard-interactive authentication needs input")
)

// Keepalive bounds. The ping is an SSH global request; a failed or unanswered ping marks
// the carrier down before the next user operation notices, so a silent NAT/idle drop
// becomes the disconnected status instead of a hung file read.
var (
	keepaliveInterval = 30 * time.Second
	keepaliveTimeout  = 10 * time.Second
)

// KeyboardPrompt is a keyboard-interactive challenge (2FA): the name/instruction the
// server sent and its questions (each with whether the answer is echoed).
type KeyboardPrompt struct {
	Name, Instruction string
	Questions         []string
	Echos             []bool
}

// sshBackend is the mounted remote state, process surfaces and terminal wrapper.
type sshBackend struct {
	mu        sync.Mutex
	opts      SSHOptions
	spec      RemoteSpec
	client    *ssh.Client
	connected bool
	reason    string
	closed    bool
	// password is the secret supplied by the UI for the next dial; "" means none.
	password string
	// needsPassword is set when the last dial failed because no non-interactive
	// credential was accepted, so the UI should ask for a password.
	needsPassword bool
	// passphrase is the secret the UI supplied for the encrypted identity, and
	// needsPassphrase says the UI must ask for one.
	passphrase      string
	needsPassphrase bool
	// keyboardChallenge is the pending keyboard-interactive challenge (2FA) and
	// keyboardAnswers the answers the UI supplied for the next dial.
	keyboardChallenge *KeyboardPrompt
	keyboardAnswers   []string
	// connecting is true while a dial runs, for the status row.
	connecting bool
	// keepaliveStop stops the current keepalive loop; nil when none runs.
	keepaliveStop chan struct{}
	// keepaliveInterval/keepaliveTimeout override the package defaults for this backend
	// (tests set them before connecting); zero falls back to the package values. They live
	// per backend so a test never mutates package state that a keepalive goroutine reads.
	keepaliveInterval time.Duration
	keepaliveTimeout  time.Duration
	// resolved is the target/jump/identity resolution of the last dial.
	resolved sshResolved
	// jumpClient is the carrier through ProxyJump, kept alive for the target client.
	jumpClient *ssh.Client
	// agentConn is the local ssh-agent socket opened for forwarding, closed with the
	// backend.
	agentConn net.Conn
	// forwardAgent is true when this connection forwards the local agent.
	forwardAgent bool
	// notices are user-facing transport notices (accept-new host key recorded, password
	// stored) collected for the app to show once it has a UI; TakeNotices drains them.
	notices []string
	// hostKeyAlg is the verified host key's type from the last handshake, and detail is
	// the connection description including it.
	hostKeyAlg string
	detail     string
	// usedPassword is the password credential the last handshake actually used, and its
	// source ("ui", "prompt" or "keyring") for the consent-gated store.
	usedPassword       string
	usedPasswordSource string
}

// ConnectSSH dials the remote and mounts an authority slot over it.
func ConnectSSH(ctx context.Context, o SSHOptions) (*Authority, error) {
	b := &sshBackend{opts: o, spec: o.Spec}
	if err := b.connect(ctx); err != nil {
		return nil, err
	}
	return New(Parts{
		Files:       newSSHFiles(b),
		OneShot:     b,
		LongRunning: b,
		Terminal:    b,
		Label:       b.Label(),
		Remote:      b,
		RemoteSpec:  &b.spec,
	}), nil
}

// NewSSH mounts an SSH slot without dialing: the caller connects with Reconnect (a
// startup attempt, then reconnect-on-activate). This is the constructor the app wires so a
// password prompt can happen after the UI exists.
func NewSSH(o SSHOptions) *Authority {
	b := &sshBackend{opts: o, spec: o.Spec, reason: "not connected yet"}
	return New(Parts{
		Files:       newSSHFiles(b),
		OneShot:     b,
		LongRunning: b,
		Terminal:    b,
		Label:       b.Label(),
		Remote:      b,
		RemoteSpec:  &b.spec,
	})
}

// Label is the status-row text of the connection ("SSH:user@host"); the brackets are the
// status row's business.
func (b *sshBackend) Label() string { return "SSH:" + b.spec.Display() }

// Connected reports whether the carrier is believed alive.
func (b *sshBackend) Connected() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.connected
}

// Reason is why the connection is down.
func (b *sshBackend) Reason() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.reason
}

// clientSnapshot returns the live client, refusing when the connection is down.
func (b *sshBackend) clientSnapshot() (*ssh.Client, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return nil, errors.New("authority: SSH connection is closed")
	}
	if b.client == nil || !b.connected {
		return nil, fmt.Errorf("authority: SSH connection is down (%s)", b.reason)
	}
	return b.client, nil
}

// requestAgentForwarding asks the server to forward this session's agent channel
// to the local agent. It is a session request, never an error the caller must
// handle: a server that refuses it is reported by its own failure to open the
// agent channel, and the session still runs.
func (b *sshBackend) requestAgentForwarding(sess *ssh.Session) {
	b.mu.Lock()
	on := b.forwardAgent
	b.mu.Unlock()
	if on {
		_ = agent.RequestAgentForwarding(sess)
	}
}

// markDown records a broken carrier; the next operation or the activation hook reconnects.
func (b *sshBackend) markDown(err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.connected = false
	if err != nil {
		b.reason = err.Error()
	}
}

// dial opens one SSH client, applying the bounded ssh config (HostName/User/Port/
// IdentityFile/ProxyJump/IdentityAgent/ForwardAgent) and the one supported jump hop.
func (b *sshBackend) dial(ctx context.Context) (*ssh.Client, error) {
	timeout := b.opts.Timeout
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	res, err := b.resolveDial()
	if err != nil {
		return nil, err
	}
	algorithms, err := ParseHostKeyAlgorithms(b.opts.HostKeyAlgorithms)
	if err != nil {
		return nil, err
	}
	hostKey, err := b.hostKeyCallback()
	if err != nil {
		return nil, err
	}
	hostKey = b.captureHostKey(hostKey)
	ag, agentConn, err := b.openAgent(res)
	if err != nil {
		return nil, err
	}
	success := false
	defer func() {
		if !success && agentConn != nil {
			_ = agentConn.Close()
		}
	}()
	auth, err := b.authMethodsResolved(ctx, res, ag)
	if err != nil {
		return nil, err
	}
	targetAddr := net.JoinHostPort(res.host, strconv.Itoa(res.port))
	targetCfg := &ssh.ClientConfig{User: res.user, Auth: auth, HostKeyCallback: hostKey, HostKeyAlgorithms: algorithms, Timeout: timeout}

	var conn net.Conn
	var jumpClient *ssh.Client
	if res.jump != nil {
		jumpAddr := net.JoinHostPort(res.jump.host, strconv.Itoa(res.jump.port))
		jc, err := dialSSHConn(ctx, jumpAddr, &ssh.ClientConfig{User: res.jump.user, Auth: auth, HostKeyCallback: hostKey, HostKeyAlgorithms: algorithms, Timeout: timeout}, timeout, b.spec.Display())
		if err != nil {
			return nil, fmt.Errorf("ssh %s: jump %s: %w", b.spec.Display(), jumpAddr, err)
		}
		jumpClient = jc
		conn, err = jumpClient.Dial("tcp", targetAddr)
		if err != nil {
			_ = jumpClient.Close()
			return nil, fmt.Errorf("ssh %s: jump %s: reach %s: %w", b.spec.Display(), jumpAddr, targetAddr, err)
		}
	} else {
		var d net.Dialer
		dialCtx, cancel := context.WithTimeout(ctx, timeout)
		conn, err = d.DialContext(dialCtx, "tcp", targetAddr)
		cancel()
		if err != nil {
			return nil, fmt.Errorf("ssh %s: %w", b.spec.Display(), err)
		}
	}

	closeAll := func() {
		_ = conn.Close()
		if jumpClient != nil {
			_ = jumpClient.Close()
		}
	}
	b.clearUsedPassword()
	c, chans, reqs, err := ssh.NewClientConn(conn, targetAddr, targetCfg)
	if err != nil {
		closeAll()
		return nil, fmt.Errorf("ssh %s: %w", b.spec.Display(), err)
	}
	client := ssh.NewClient(c, chans, reqs)

	b.mu.Lock()
	b.resolved = res
	if b.jumpClient != nil {
		_ = b.jumpClient.Close()
	}
	b.jumpClient = jumpClient
	b.mu.Unlock()

	if res.forwardAgent {
		if ag == nil {
			_ = client.Close()
			closeAll()
			return nil, fmt.Errorf("ssh %s: ForwardAgent is on but no ssh-agent is available (SSH_AUTH_SOCK/IdentityAgent)", b.spec.Display())
		}
		if ferr := agent.ForwardToAgent(client, ag); ferr != nil {
			_ = client.Close()
			closeAll()
			return nil, fmt.Errorf("ssh %s: ForwardAgent: %w", b.spec.Display(), ferr)
		}
	}
	policy, _ := ParseKnownHostsPolicy(b.opts.KnownHostsPolicy)
	b.mu.Lock()
	b.forwardAgent = res.forwardAgent
	if b.agentConn != nil {
		_ = b.agentConn.Close()
	}
	b.agentConn = agentConn
	alg := b.hostKeyAlg
	if alg == "" {
		alg = "unknown"
	}
	b.detail = fmt.Sprintf("host key %s · known_hosts %s · algorithms %s", alg, policy, strings.Join(algorithms, ", "))
	b.mu.Unlock()
	b.storeAcceptedPassword(ctx, res)
	success = true
	return client, nil
}

// dialSSHConn dials addr and completes the SSH handshake.
func dialSSHConn(ctx context.Context, addr string, cfg *ssh.ClientConfig, timeout time.Duration, display string) (*ssh.Client, error) {
	var d net.Dialer
	dialCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	conn, err := d.DialContext(dialCtx, "tcp", addr)
	if err != nil {
		return nil, err
	}
	if dl, ok := dialCtx.Deadline(); ok {
		_ = conn.SetDeadline(dl)
	}
	c, chans, reqs, err := ssh.NewClientConn(conn, addr, cfg)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	_ = conn.SetDeadline(time.Time{})
	return ssh.NewClient(c, chans, reqs), nil
}

// connect establishes the carrier (first dial or reconnect).
func (b *sshBackend) connect(ctx context.Context) error {
	b.mu.Lock()
	b.connecting = true
	b.mu.Unlock()
	client, err := b.dial(ctx)
	if err != nil {
		b.mu.Lock()
		b.connected, b.reason, b.connecting = false, err.Error(), false
		lower := strings.ToLower(err.Error())
		b.needsPassword = strings.Contains(lower, "unable to authenticate") ||
			strings.Contains(lower, "no authentication method") ||
			strings.Contains(lower, "password")
		if errors.Is(err, ErrNeedsPassphrase) {
			b.needsPassphrase = true
		}
		b.mu.Unlock()
		return err
	}
	b.mu.Lock()
	old := b.client
	b.client, b.connected, b.reason, b.connecting = client, true, "", false
	b.needsPassword, b.needsPassphrase = false, false
	b.keyboardChallenge = nil
	b.mu.Unlock()
	if old != nil {
		_ = old.Close()
	}
	b.startKeepalive(client)
	return nil
}

// startKeepalive starts the periodic info ping of the live carrier, replacing a previous
// loop. The loop marks the backend disconnected when a ping fails or times out, so the
// status row turns disconnected (and the retry loop reconnects) before the next user
// operation hangs on a dead NAT mapping.
func (b *sshBackend) startKeepalive(client *ssh.Client) {
	stop := make(chan struct{})
	b.mu.Lock()
	old := b.keepaliveStop
	b.keepaliveStop = stop
	b.mu.Unlock()
	if old != nil {
		close(old)
	}
	go b.keepaliveLoop(client, stop)
}

// stopKeepalive stops the current loop, if any.
func (b *sshBackend) stopKeepalive() {
	b.mu.Lock()
	stop := b.keepaliveStop
	b.keepaliveStop = nil
	b.mu.Unlock()
	if stop != nil {
		close(stop)
	}
}

// keepaliveValues returns this backend's interval/timeout: the per-backend override or the
// package default. The read is under the backend lock; package defaults are written only at
// init, so production has no race, and tests override the backend fields before connecting.
func (b *sshBackend) keepaliveValues() (interval, timeout time.Duration) {
	b.mu.Lock()
	interval, timeout = b.keepaliveInterval, b.keepaliveTimeout
	b.mu.Unlock()
	if interval <= 0 {
		interval = keepaliveInterval
	}
	if timeout <= 0 {
		timeout = keepaliveTimeout
	}
	return interval, timeout
}

// keepaliveLoop pings the server until stop or a failed ping.
func (b *sshBackend) keepaliveLoop(client *ssh.Client, stop <-chan struct{}) {
	interval, timeout := b.keepaliveValues()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
		}
		if err := b.ping(client, timeout); err != nil {
			b.markDown(fmt.Errorf("keepalive failed: %w", err))
			_ = client.Close()
			return
		}
	}
}

// ping sends one keepalive global request, bounded by timeout.
func (b *sshBackend) ping(client *ssh.Client, timeout time.Duration) error {
	done := make(chan error, 1)
	go func() {
		_, _, err := client.SendRequest("keepalive@openssh.com", true, nil)
		done <- err
	}()
	select {
	case err := <-done:
		return err
	case <-time.After(timeout):
		return errors.New("keepalive timed out")
	}
}

// hostKeyCallback resolves the host-key check: injected, or known_hosts under the
// configured policy (strict default; accept-new records a first-seen key; off refused).
func (b *sshBackend) hostKeyCallback() (ssh.HostKeyCallback, error) {
	if b.opts.HostKeyCallback != nil {
		return b.opts.HostKeyCallback, nil
	}
	policy, err := ParseKnownHostsPolicy(b.opts.KnownHostsPolicy)
	if err != nil {
		return nil, err
	}
	path := b.opts.KnownHostsPath
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("ssh: no known_hosts path: %w", err)
		}
		path = filepath.Join(home, ".ssh", "known_hosts")
	}
	return newKnownHostsCallback(path, policy, b.addNotice)
}

// captureHostKey records the verified host key's type: the transport does not expose the
// negotiated host-key algorithm directly, so the key the server presented (as verified by
// the callback) is the reported algorithm (see docs/authority.md for the residual).
func (b *sshBackend) captureHostKey(cb ssh.HostKeyCallback) ssh.HostKeyCallback {
	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		if err := cb(hostname, remote, key); err != nil {
			return err
		}
		b.mu.Lock()
		b.hostKeyAlg = key.Type()
		b.mu.Unlock()
		return nil
	}
}

// addNotice queues a user-facing transport notice for the app to show once it can.
func (b *sshBackend) addNotice(msg string) {
	b.mu.Lock()
	b.notices = append(b.notices, msg)
	b.mu.Unlock()
}

// TakeNotices drains the user-facing notices of the transport.
func (b *sshBackend) TakeNotices() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := b.notices
	b.notices = nil
	return out
}

// ConnectionDetail describes the established connection, including the verified host key
// algorithm and the known_hosts policy.
func (b *sshBackend) ConnectionDetail() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.detail
}

func (b *sshBackend) clearUsedPassword() {
	b.mu.Lock()
	b.usedPassword, b.usedPasswordSource = "", ""
	b.mu.Unlock()
}

// storeAcceptedPassword stores a password the server accepted when the user config
// opted in, and queues an honest notice (never when the store is unavailable).
func (b *sshBackend) storeAcceptedPassword(ctx context.Context, res sshResolved) {
	if !b.opts.StorePassword || b.opts.PasswordStore == nil {
		return
	}
	b.mu.Lock()
	pw, source := b.usedPassword, b.usedPasswordSource
	b.mu.Unlock()
	if pw == "" || source == "keyring" {
		return
	}
	if err := b.opts.PasswordStore.Store(ctx, res.host, res.user, pw); err != nil {
		b.addNotice(fmt.Sprintf("SSH: the password for %s was accepted but not stored in the OS keyring: %v", res.host, err))
		return
	}
	b.addNotice(fmt.Sprintf("SSH: stored the accepted password for %s@%s in the OS keyring (ssh.keyring is on)", res.user, res.host))
}

// keyboardAuth returns the keyboard-interactive authentication method: stored answers
// from the UI are used once, then the app callback, and a callback that needs input
// records the challenge so the app can prompt for it.
func (b *sshBackend) keyboardAuth(ctx context.Context) func(name, instruction string, questions []string, echos []bool) ([]string, error) {
	return func(name, instruction string, questions []string, echos []bool) ([]string, error) {
		b.mu.Lock()
		answers := b.keyboardAnswers
		b.keyboardAnswers = nil
		b.mu.Unlock()
		if answers != nil {
			return answers, nil
		}
		prompt := &KeyboardPrompt{Name: name, Instruction: instruction, Questions: append([]string(nil), questions...), Echos: append([]bool(nil), echos...)}
		if b.opts.KeyboardInteractive != nil {
			answers, err := b.opts.KeyboardInteractive(ctx, name, instruction, questions, echos)
			if err == nil {
				return answers, nil
			}
			if errors.Is(err, ErrNeedsKeyboardInteractive) {
				b.mu.Lock()
				b.keyboardChallenge = prompt
				b.mu.Unlock()
			}
			return nil, err
		}
		b.mu.Lock()
		b.keyboardChallenge = prompt
		b.mu.Unlock()
		return nil, ErrNeedsKeyboardInteractive
	}
}

// Connecting reports whether a dial is in progress (the status row shows it).
func (b *sshBackend) Connecting() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.connecting && !b.connected
}

// NeedsPassphrase reports whether the last dial needs an encrypted identity's passphrase.
func (b *sshBackend) NeedsPassphrase() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.needsPassphrase && !b.connected
}

// ReconnectWithPassphrase stores the passphrase and redials (the UI prompt path).
func (b *sshBackend) ReconnectWithPassphrase(ctx context.Context, passphrase string) error {
	b.mu.Lock()
	b.passphrase = passphrase
	b.mu.Unlock()
	return b.Reconnect(ctx)
}

// NeedsKeyboardInteractive reports whether the last dial needs a 2FA challenge answered.
func (b *sshBackend) NeedsKeyboardInteractive() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.keyboardChallenge != nil && !b.connected
}

// KeyboardInteractiveChallenge returns the pending 2FA challenge.
func (b *sshBackend) KeyboardInteractiveChallenge() (KeyboardPrompt, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.keyboardChallenge == nil {
		return KeyboardPrompt{}, false
	}
	return *b.keyboardChallenge, true
}

// ReconnectWithAnswers stores the 2FA answers and redials (the UI prompt path).
func (b *sshBackend) ReconnectWithAnswers(ctx context.Context, answers []string) error {
	b.mu.Lock()
	b.keyboardAnswers = append([]string(nil), answers...)
	b.mu.Unlock()
	return b.Reconnect(ctx)
}

// existingIdentityFiles lists the usual private keys that exist.
func existingIdentityFiles(dir string) []string {
	var out []string
	for _, name := range []string{"id_ed25519", "id_ecdsa", "id_rsa"} {
		p := filepath.Join(dir, name)
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			out = append(out, p)
		}
	}
	return out
}

// parseIdentity reads one private key, asking the passphrase callback when it is encrypted.
func parseIdentity(path string, passphrase func(context.Context, string) (string, error), ctx context.Context) (ssh.Signer, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	signer, err := ssh.ParsePrivateKey(data)
	if err == nil {
		return signer, nil
	}
	var missing *ssh.PassphraseMissingError
	if !errors.As(err, &missing) || passphrase == nil {
		return nil, fmt.Errorf("ssh: identity %s: %w", path, err)
	}
	phrase, perr := passphrase(ctx, path)
	if perr != nil {
		return nil, perr
	}
	return ssh.ParsePrivateKeyWithPassphrase(data, []byte(phrase))
}

// NeedsPassword reports whether the last failure wants a password from the UI.
func (b *sshBackend) NeedsPassword() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.needsPassword && !b.connected
}

// ReconnectWithPassword stores password and redials (the UI prompt path).
func (b *sshBackend) ReconnectWithPassword(ctx context.Context, password string) error {
	b.mu.Lock()
	b.password = password
	b.mu.Unlock()
	return b.Reconnect(ctx)
}

// Reconnect closes the carrier and dials the stored target again.
func (b *sshBackend) Reconnect(ctx context.Context) error {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return errors.New("authority: SSH connection is closed")
	}
	old := b.client
	b.client, b.connected, b.reason = nil, false, "reconnecting"
	b.mu.Unlock()
	b.stopKeepalive()
	if old != nil {
		_ = old.Close()
	}
	return b.connect(ctx)
}

// Close shuts the carrier down.
func (b *sshBackend) Close() error {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return nil
	}
	b.closed, b.connected = true, false
	old := b.client
	oldJump := b.jumpClient
	oldAgent := b.agentConn
	b.client, b.jumpClient, b.agentConn = nil, nil, nil
	b.mu.Unlock()
	b.stopKeepalive()
	if oldAgent != nil {
		_ = oldAgent.Close()
	}
	if old != nil {
		_ = old.Close()
	}
	if oldJump != nil {
		_ = oldJump.Close()
	}
	return nil
}

// runSession runs a one-shot command and maps ssh exit statuses into ProcResult. The
// one-shot surface keeps the port's contract: output is collected without a transport
// cap here (callers bound it with ProcOptions.MaxOutput where they need to).
func (b *sshBackend) runSession(ctx context.Context, argv []string, o hostos.ProcOptions) (hostos.ProcResult, error) {
	return b.runSessionBuffered(ctx, argv, o, newBoundBuffer(0), newBoundBuffer(0))
}

// runSessionBuffered runs one exec session, pumping stdout and stderr into the given
// bounded buffers. A bounded buffer keeps draining the channel after its limit (so a
// remote process never blocks on a full pipe) while recording that the answer was over
// the cap; the caller can then reject the answer instead of using a truncated one.
func (b *sshBackend) runSessionBuffered(ctx context.Context, argv []string, o hostos.ProcOptions, stdout, stderr *boundBuffer) (hostos.ProcResult, error) {
	client, err := b.clientSnapshot()
	if err != nil {
		return hostos.ProcResult{}, err
	}
	sess, err := client.NewSession()
	if err != nil {
		b.markDown(err)
		return hostos.ProcResult{}, err
	}
	defer func() { _ = sess.Close() }()
	b.requestAgentForwarding(sess)
	cmd := RemoteCommand(argv, o.Cwd, o.Env)
	sess.Stdout, sess.Stderr = stdout, stderr
	if o.Stdin != nil {
		// An empty non-nil Stdin still means "send EOF": the remote helper's cat must
		// see end of input rather than wait for stdin that never closes.
		sess.Stdin = newSliceReader(o.Stdin)
	}
	done := make(chan error, 1)
	go func() { done <- sess.Run(cmd) }()
	select {
	case <-ctx.Done():
		_ = sess.Signal(ssh.SIGKILL)
		_ = sess.Close()
		return hostos.ProcResult{}, ctx.Err()
	case err := <-done:
		res := hostos.ProcResult{Stdout: stdout.Bytes(), Stderr: stderr.Bytes()}
		var exit *ssh.ExitError
		switch {
		case err == nil:
			return res, nil
		case errors.As(err, &exit):
			res.Code = exit.ExitStatus()
			if exit.Signal() != "" {
				res.Signal = exit.Signal()
			}
			return res, nil
		default:
			b.markDown(err)
			return hostos.ProcResult{}, err
		}
	}
}

// Run is the one-shot surface.
func (b *sshBackend) Run(ctx context.Context, argv []string, o hostos.ProcOptions) (hostos.ProcResult, error) {
	return b.runSession(ctx, argv, o)
}

// Start is the long-running stdio surface.
func (b *sshBackend) Start(ctx context.Context, argv []string, o hostos.ProcOptions) (hostos.Proc, error) {
	client, err := b.clientSnapshot()
	if err != nil {
		return nil, err
	}
	sess, err := client.NewSession()
	if err != nil {
		b.markDown(err)
		return nil, err
	}
	b.requestAgentForwarding(sess)
	stdin, err := sess.StdinPipe()
	if err != nil {
		_ = sess.Close()
		return nil, err
	}
	stdout, err := sess.StdoutPipe()
	if err != nil {
		_ = sess.Close()
		return nil, err
	}
	stderr, err := sess.StderrPipe()
	if err != nil {
		_ = sess.Close()
		return nil, err
	}
	cmd := RemoteCommand(argv, o.Cwd, o.Env)
	if err := sess.Start(cmd); err != nil {
		_ = sess.Close()
		return nil, err
	}
	p := &sshProc{sess: sess, stdin: stdin, backend: b, chunks: make(chan hostos.ProcChunk, 64)}
	p.pump(stdout, false)
	p.pump(stderr, true)
	return p, nil
}

// Launch starts the integrated terminal on the remote host and returns its transport.
func (b *sshBackend) Launch(ctx context.Context, o TerminalOptions) (TerminalLaunch, error) {
	client, err := b.clientSnapshot()
	if err != nil {
		return TerminalLaunch{}, err
	}
	sess, err := client.NewSession()
	if err != nil {
		b.markDown(err)
		return TerminalLaunch{}, err
	}
	b.requestAgentForwarding(sess)
	cols, rows := o.Cols, o.Rows
	if cols <= 0 {
		cols = 80
	}
	if rows <= 0 {
		rows = 24
	}
	modes := ssh.TerminalModes{ssh.ECHO: 1, ssh.TTY_OP_ISPEED: 14400, ssh.TTY_OP_OSPEED: 14400}
	if err := sess.RequestPty("xterm-256color", rows, cols, modes); err != nil {
		_ = sess.Close()
		return TerminalLaunch{}, err
	}
	pr, pw := io.Pipe()
	sess.Stdout, sess.Stderr = pw, pw
	stdin, err := sess.StdinPipe()
	if err != nil {
		_ = sess.Close()
		_ = pw.Close()
		return TerminalLaunch{}, err
	}
	cmd := remoteLoginCommand(o.Dir)
	if envCmd := remoteLoginCommandEnv(o.Dir, o.Env); envCmd != "" {
		// The terminal's remote environment delta (captured on the remote host by the
		// session's env provider) rides as data on the remote login command, so the
		// shell starts in the project environment. No local environment is involved.
		cmd = envCmd
	}
	if err := sess.Start(cmd); err != nil {
		_ = sess.Close()
		_ = pw.Close()
		return TerminalLaunch{}, err
	}
	t := &sshTerminal{sess: sess, stdin: stdin, out: pr, outW: pw, waitDone: make(chan struct{})}
	return TerminalLaunch{Dir: o.Dir, ManagesCwd: true, Label: b.Label(), Backend: t}, nil
}

// remoteLoginCommand is the remote shell command of the terminal: cd into the workspace and
// exec the remote login shell ($SHELL is the remote shell; the local terminal.shell
// override deliberately does not apply).
func remoteLoginCommand(dir string) string {
	shell := "exec " + "$" + "{SHELL:-/bin/sh}" + " -l"
	if dir == "" {
		return shell
	}
	return "cd " + QuoteDir(dir) + " && " + shell
}

// remoteLoginCommandEnv is remoteLoginCommand with the session environment delta applied as
// an env K=V prefix on the remote login shell (the remote host's SHELL, expanded there).
// Keys are sorted and every entry ShellQuote'd, so values travel as data; malformed entries
// and an empty delta return "" (the caller keeps the plain login command).
func remoteLoginCommandEnv(dir string, env []string) string {
	values := make(map[string]string, len(env))
	for _, kv := range env {
		k, v, ok := strings.Cut(kv, "=")
		if !ok || k == "" {
			continue
		}
		values[k] = v
	}
	if len(values) == 0 {
		return ""
	}
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString("env")
	for _, k := range keys {
		b.WriteByte(' ')
		b.WriteString(ShellQuote(k + "=" + values[k]))
	}
	// No exec here: env is the program; it execs the remote login shell itself.
	b.WriteString(" \"$" + "{SHELL:-/bin/sh}\" -l")
	cmd := b.String()
	if dir == "" {
		return cmd
	}
	return "cd " + QuoteDir(dir) + " && " + cmd
}

// sshProc is one long-running stdio child over an ssh session.
type sshProc struct {
	sess    *ssh.Session
	stdin   io.WriteCloser
	backend *sshBackend
	chunks  chan hostos.ProcChunk
}

// pump copies one stream into the chunk channel.
func (p *sshProc) pump(r io.Reader, stderr bool) {
	go func() {
		buf := make([]byte, 32<<10)
		for {
			n, err := r.Read(buf)
			if n > 0 {
				p.chunks <- hostos.ProcChunk{Stderr: stderr, Data: append([]byte(nil), buf[:n]...)}
			}
			if err != nil {
				return
			}
		}
	}()
}

func (p *sshProc) PID() int { return 0 }
func (p *sshProc) Write(data []byte) error {
	_, err := p.stdin.Write(data)
	if err != nil {
		p.backend.markDown(err)
	}
	return err
}
func (p *sshProc) CloseStdin() { _ = p.stdin.Close() }
func (p *sshProc) Kill(signal string) error {
	sig := ssh.SIGTERM
	if signal == "KILL" {
		sig = ssh.SIGKILL
	}
	return p.sess.Signal(sig)
}
func (p *sshProc) Terminate(grace time.Duration) {
	go func() {
		_ = p.Kill("TERM")
		time.Sleep(grace)
		_ = p.Kill("KILL")
	}()
}
func (p *sshProc) Output(ctx context.Context) (hostos.ProcChunk, bool) {
	select {
	case c := <-p.chunks:
		return c, true
	case <-ctx.Done():
		return hostos.ProcChunk{}, false
	}
}
func (p *sshProc) Wait(ctx context.Context) (hostos.ProcResult, error) {
	done := make(chan error, 1)
	go func() { done <- p.sess.Wait() }()
	select {
	case <-ctx.Done():
		return hostos.ProcResult{}, ctx.Err()
	case err := <-done:
		res := hostos.ProcResult{}
		var exit *ssh.ExitError
		switch {
		case err == nil:
			return res, nil
		case errors.As(err, &exit):
			res.Code = exit.ExitStatus()
			return res, nil
		default:
			p.backend.markDown(err)
			return hostos.ProcResult{}, err
		}
	}
}

// sshTerminal is the PTY transport of the integrated terminal.
type sshTerminal struct {
	sess  *ssh.Session
	stdin io.WriteCloser
	out   *io.PipeReader
	outW  *io.PipeWriter
	once  sync.Once
	// waitOnce makes Wait idempotent: the first caller reaps the ssh session and closes
	// the output pipe (so a Read after the remote end exits sees io.EOF); later callers
	// wait for that one result.
	waitOnce sync.Once
	waitDone chan struct{}
	waitErr  error
}

func (t *sshTerminal) Read(p []byte) (int, error)  { return t.out.Read(p) }
func (t *sshTerminal) Write(p []byte) (int, error) { return t.stdin.Write(p) }
func (t *sshTerminal) Resize(cols, rows int) error {
	if cols <= 0 || rows <= 0 {
		return nil
	}
	return t.sess.WindowChange(rows, cols)
}
func (t *sshTerminal) Kill() error {
	if err := t.sess.Signal(ssh.SIGKILL); err != nil {
		return err
	}
	return t.sess.Close()
}
func (t *sshTerminal) Wait() error {
	t.waitOnce.Do(func() {
		t.waitErr = t.sess.Wait()
		_ = t.outW.Close()
		close(t.waitDone)
	})
	<-t.waitDone
	return t.waitErr
}
func (t *sshTerminal) Close() error {
	var err error
	t.once.Do(func() {
		err = t.sess.Close()
		_ = t.stdin.Close()
		_ = t.outW.Close()
		_ = t.out.Close()
	})
	return err
}

// boundBuffer is a concurrency-safe bytes buffer for the session pumps. A positive
// limit caps the kept bytes and sets the overflow flag when more arrives; the write
// still reports every byte as consumed so the SSH channel keeps draining and the remote
// process never blocks on a full pipe. A limit <= 0 keeps everything.
type boundBuffer struct {
	mu      sync.Mutex
	buf     []byte
	limit   int64
	dropped bool
}

// newBoundBuffer returns a buffer keeping at most limit bytes (limit <= 0: unbounded).
func newBoundBuffer(limit int64) *boundBuffer { return &boundBuffer{limit: limit} }

func (b *boundBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := len(p)
	if b.limit > 0 {
		room := b.limit - int64(len(b.buf))
		switch {
		case room <= 0:
			b.dropped = true
			return n, nil
		case int64(len(p)) > room:
			b.dropped = true
			p = p[:room]
		}
	}
	b.buf = append(b.buf, p...)
	return n, nil
}

func (b *boundBuffer) Bytes() []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]byte(nil), b.buf...)
}

// Overflow reports whether more bytes arrived than the buffer kept.
func (b *boundBuffer) Overflow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.dropped
}

// sliceReader is a reader over a byte slice (a fresh copy per use).
type sliceReader struct {
	b []byte
	i int
}

func newSliceReader(b []byte) *sliceReader { return &sliceReader{b: append([]byte(nil), b...)} }

func (r *sliceReader) Read(p []byte) (int, error) {
	if r.i >= len(r.b) {
		return 0, io.EOF
	}
	n := copy(p, r.b[r.i:])
	r.i += n
	return n, nil
}
