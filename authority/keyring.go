package authority

// The real OS password-keyring backends for ssh.keyring (macOS security, Linux
// secret-tool). Both are invoked as argv with no shell; output is bounded, stdin is a
// byte slice, and the caller's context cancels the child. Defaults stay off: the app only
// passes a store when the user config sets ssh.keyring. Failures are returned as honest
// errors (shown as the existing "accepted but not stored"/lookup notice), never reported
// as success.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

// keyringService is the service/attribute name the entries are stored under.
const keyringService = "gvim-ssh"

// keyringMaxAnswer bounds one stdout/stderr answer from a keyring tool.
const keyringMaxAnswer = 64 << 10

type keyringKind int

const (
	keyringSecurity keyringKind = iota
	keyringSecretTool
)

// execPasswordStore is a PasswordStore backed by one external tool. run is injectable so
// tests use a fake command without a real keychain or session bus; production uses
// runKeyringCommand.
type execPasswordStore struct {
	kind keyringKind
	tool string
	run  func(ctx context.Context, name string, args []string, stdin []byte) (stdout, stderr []byte, code int, err error)
}

// systemPasswordStoreFor is the injectable selection: tests choose goos/lookPath/run.
func systemPasswordStoreFor(goos string, lookPath func(string) (string, error), run func(context.Context, string, []string, []byte) ([]byte, []byte, int, error)) PasswordStore {
	switch goos {
	case "darwin":
		if tool, err := lookPath("security"); err == nil && tool != "" {
			return &execPasswordStore{kind: keyringSecurity, tool: tool, run: run}
		}
	case "linux":
		if tool, err := lookPath("secret-tool"); err == nil && tool != "" {
			return &execPasswordStore{kind: keyringSecretTool, tool: tool, run: run}
		}
	}
	return NoopPasswordStore{}
}

func systemPasswordStore() PasswordStore {
	return systemPasswordStoreFor(runtime.GOOS, exec.LookPath, runKeyringCommand)
}

// keyringAccount is the account/attribute value: the SSH user@host. The host is the
// resolved dial host without a port (a recorded residual: one entry per user@host).
func keyringAccount(host, user string) string { return user + "@" + host }

// Lookup asks the tool for the stored host password. A tool-level "no such entry" is a
// miss (ok=false, nil error), not an error; every other failure is returned with the
// tool's bounded stderr.
func (s *execPasswordStore) Lookup(ctx context.Context, host, user string) (string, bool, error) {
	if err := ctx.Err(); err != nil {
		return "", false, err
	}
	account := keyringAccount(host, user)
	switch s.kind {
	case keyringSecurity:
		out, stderr, code, runErr := s.run(ctx, s.tool, []string{"find-generic-password", "-s", keyringService, "-a", account, "-w"}, nil)
		if runErr != nil {
			return "", false, runErr
		}
		if code != 0 {
			lower := strings.ToLower(string(stderr))
			if code == 44 || strings.Contains(lower, "could not be found") || strings.Contains(lower, "item not found") {
				return "", false, nil
			}
			return "", false, keyringFailure("read", stderr, code)
		}
		pw, ok := decodeKeyringAnswer(out)
		return pw, ok, nil
	case keyringSecretTool:
		out, stderr, code, runErr := s.run(ctx, s.tool, []string{"lookup", "service", keyringService, "account", account}, nil)
		if runErr != nil {
			return "", false, runErr
		}
		if code != 0 {
			// secret-tool exits 1 with no output when the secret is absent.
			if code == 1 && len(bytes.TrimSpace(out)) == 0 && len(bytes.TrimSpace(stderr)) == 0 {
				return "", false, nil
			}
			return "", false, keyringFailure("read", stderr, code)
		}
		pw, ok := decodeKeyringAnswer(out)
		return pw, ok, nil
	}
	return "", false, fmt.Errorf("ssh keyring: unknown backend")
}

// Store records the accepted password. macOS security takes it as the -w argument (an
// argv exposure recorded in docs/authority.md); secret-tool reads it from stdin, so it is
// never in argv. Nothing is stored unless the app passed this store (ssh.keyring on) and
// the server accepted a password.
func (s *execPasswordStore) Store(ctx context.Context, host, user, password string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	account := keyringAccount(host, user)
	var out, stderr []byte
	var code int
	var runErr error
	switch s.kind {
	case keyringSecurity:
		out, stderr, code, runErr = s.run(ctx, s.tool, []string{"add-generic-password", "-U", "-s", keyringService, "-a", account, "-w", password}, nil)
	case keyringSecretTool:
		out, stderr, code, runErr = s.run(ctx, s.tool,
			[]string{"store", "--label=gvim SSH password", "service", keyringService, "account", account},
			[]byte(password))
	default:
		return fmt.Errorf("ssh keyring: unknown backend")
	}
	if runErr != nil {
		return runErr
	}
	if code != 0 {
		return keyringFailure("store", stderr, code)
	}
	_ = out
	return nil
}

// decodeKeyringAnswer removes exactly the tool's one trailing newline. Password bytes are
// otherwise preserved (embedded/trailing LF and CR included): trimming whitespace would
// silently change a legal password.
func decodeKeyringAnswer(out []byte) (string, bool) {
	if len(out) == 0 {
		return "", false
	}
	raw := string(out)
	if strings.HasSuffix(raw, "\n") {
		return raw[:len(raw)-1], true
	}
	return raw, true
}

func keyringFailure(op string, stderr []byte, code int) error {
	msg := strings.TrimSpace(string(stderr))
	if msg == "" {
		msg = fmt.Sprintf("tool exited %d", code)
	}
	return fmt.Errorf("ssh keyring %s: %s", op, msg)
}

// limitedBuffer keeps at most max bytes but keeps draining, so the child never blocks on a
// full pipe; Over reports that the answer was too long instead of silently truncating it
// into a wrong password.
type limitedBuffer struct {
	buf  bytes.Buffer
	max  int
	over bool
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if room := b.max - b.buf.Len(); room > 0 {
		if len(p) > room {
			b.buf.Write(p[:room])
			b.over = true
		} else {
			b.buf.Write(p)
		}
	} else if len(p) > 0 {
		b.over = true
	}
	return len(p), nil
}

// runKeyringCommand runs name with argv (no shell) and optional stdin bytes, bounded.
func runKeyringCommand(ctx context.Context, name string, args []string, stdin []byte) (stdout, stderr []byte, code int, err error) {
	cmd := exec.CommandContext(ctx, name, args...)
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	out, errOut := &limitedBuffer{max: keyringMaxAnswer}, &limitedBuffer{max: keyringMaxAnswer}
	cmd.Stdout, cmd.Stderr = out, errOut
	runErr := cmd.Run()
	if runErr != nil {
		if ctx.Err() != nil {
			return nil, nil, 0, ctx.Err()
		}
		var exit *exec.ExitError
		if errors.As(runErr, &exit) {
			return out.buf.Bytes(), errOut.buf.Bytes(), exit.ExitCode(), nil
		}
		return nil, nil, 0, fmt.Errorf("ssh keyring: run %s: %w", name, runErr)
	}
	if out.over || errOut.over {
		return nil, nil, 0, fmt.Errorf("ssh keyring: %s answer exceeded %d bytes", name, keyringMaxAnswer)
	}
	return out.buf.Bytes(), errOut.buf.Bytes(), 0, nil
}
