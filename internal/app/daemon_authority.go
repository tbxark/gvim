package app

// Daemon-hosted remote authority (the Fresh "jump box" pattern): a daemon started with a
// remote target hosts that remote workspace over SSH while running locally, and an attach
// client only talks to the daemon socket. The remote target is consumed when the daemon
// starts; a reattach ignores it. One workspace binds one backend, so a local workspace and
// a remote workspace can coexist in one daemon.

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/charmbracelet/x/term"

	"github.com/tbxark/gvim/authority"
)

// authoritySpecText returns the canonical persisted form of a remote target: the user's
// parsed text when it round-trips unchanged, else an equivalent CLI target built from the
// structured fields. It fails for a programmatically built spec that no accepted target
// spelling can represent, rather than persisting something a restart cannot parse.
func authoritySpecText(spec *authority.RemoteSpec) (string, error) {
	if spec == nil {
		return "", nil
	}
	if spec.Raw != "" {
		if parsed, err := authority.ParseRemote(spec.Raw); err == nil && sameRemoteSpec(parsed, *spec) {
			return spec.Raw, nil
		}
	}
	if spec.User == "" || spec.Host == "" || spec.Path == "" {
		return "", errors.New("remote target needs a user, a host and a path")
	}
	pos := remotePositionSuffix(spec)
	if spec.Port == 0 || spec.Port == authority.DefaultSSHPort {
		// The scp form carries no port; the default port needs none.
		text := spec.User + "@" + spec.Host + ":" + spec.Path + pos
		if parsed, err := authority.ParseRemote(text); err == nil && sameRemoteSpec(parsed, *spec) {
			return text, nil
		}
	}
	if strings.HasPrefix(spec.Path, "/") {
		text := "ssh://" + spec.Display() + spec.Path + pos
		if parsed, err := authority.ParseRemote(text); err == nil && sameRemoteSpec(parsed, *spec) {
			return text, nil
		}
	}
	return "", fmt.Errorf("remote target %s cannot be stored as a canonical spec text; pass it in its user@host:/path or ssh:// form", spec.Display())
}

// remotePositionSuffix is the :LINE[:COL] tail of a target, "" without a position.
func remotePositionSuffix(spec *authority.RemoteSpec) string {
	if spec.Line <= 0 {
		return ""
	}
	if spec.Col > 0 {
		return ":" + strconv.Itoa(spec.Line) + ":" + strconv.Itoa(spec.Col)
	}
	return ":" + strconv.Itoa(spec.Line)
}

// sameRemoteSpec compares the fields a persisted target must preserve.
func sameRemoteSpec(a, b authority.RemoteSpec) bool {
	return a.User == b.User && a.Host == b.Host && a.Port == b.Port &&
		a.Path == b.Path && a.Line == b.Line && a.Col == b.Col
}

// parseAuthoritySpec parses the stored target text of a workspace; "" is local.
func parseAuthoritySpec(text string) (*authority.RemoteSpec, error) {
	if strings.TrimSpace(text) == "" {
		return nil, nil
	}
	spec, err := authority.ParseRemote(text)
	if err != nil {
		return nil, err
	}
	if err := validateRemote(&spec); err != nil {
		return nil, err
	}
	return &spec, nil
}

// daemonAuthorityPrompter reads the credentials a daemon start may need for its foreground
// remote workspace on the daemon's own terminal, before the socket hosts.
type daemonAuthorityPrompter interface {
	// Secret reads a password or passphrase without echoing it.
	Secret(prompt string) (string, error)
	// Line reads one answer (a 2FA question).
	Line(prompt string) (string, error)
}

// newDaemonAuthorityPrompter returns the terminal prompter, or a clear error when the
// daemon was not started interactively: a password prompt must fail, never hang waiting on
// a stdin nobody answers.
func newDaemonAuthorityPrompter() (daemonAuthorityPrompter, error) {
	if !term.IsTerminal(uintptr(os.Stdin.Fd())) {
		return nil, errors.New("daemon: the remote workspace needs SSH authentication, but stdin is not an interactive terminal; start the daemon from a terminal to answer prompts, or configure an SSH key/agent so no prompt is needed")
	}
	return &terminalDaemonPrompter{in: bufio.NewReader(os.Stdin)}, nil
}

// daemonPrompterFactory is replaceable by tests; production reads the daemon terminal.
var daemonPrompterFactory = newDaemonAuthorityPrompter

// terminalDaemonPrompter prompts on the terminal the daemon was started from.
type terminalDaemonPrompter struct {
	in *bufio.Reader
}

func (p *terminalDaemonPrompter) Secret(prompt string) (string, error) {
	_, _ = fmt.Fprint(os.Stderr, prompt)
	data, err := term.ReadPassword(uintptr(os.Stdin.Fd()))
	_, _ = fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (p *terminalDaemonPrompter) Line(prompt string) (string, error) {
	_, _ = fmt.Fprint(os.Stderr, prompt)
	line, err := p.in.ReadString('\n')
	if err != nil && line == "" {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}

// connectDaemonAuthority connects a foreground remote workspace before the daemon starts
// hosting. It answers the authority's interactive requests on the daemon terminal (password,
// encrypted-key passphrase, keyboard-interactive/2FA), with a small attempt bound. Errors
// name the host and the next action: an authentication failure must not leave a daemon that
// silently cannot reach its files.
func connectDaemonAuthority(ctx context.Context, slot authority.Slot, prompter daemonAuthorityPrompter) error {
	if slot == nil {
		return nil
	}
	if st := slot.State(); st.Kind == "local" || st.Connected {
		return nil
	}
	needsPassword := func() bool {
		p, ok := slot.(interface{ NeedsPassword() bool })
		return ok && p.NeedsPassword()
	}
	needsPassphrase := func() bool {
		p, ok := slot.(interface{ NeedsPassphrase() bool })
		return ok && p.NeedsPassphrase()
	}
	needsKeyboard := func() bool {
		p, ok := slot.(interface{ NeedsKeyboardInteractive() bool })
		return ok && p.NeedsKeyboardInteractive()
	}
	reconnectPassword := func(password string) error {
		if p, ok := slot.(interface {
			ReconnectWithPassword(context.Context, string) error
		}); ok {
			return p.ReconnectWithPassword(ctx, password)
		}
		return slot.Reconnect(ctx)
	}
	reconnectPassphrase := func(passphrase string) error {
		if p, ok := slot.(interface {
			ReconnectWithPassphrase(context.Context, string) error
		}); ok {
			return p.ReconnectWithPassphrase(ctx, passphrase)
		}
		return slot.Reconnect(ctx)
	}
	reconnectAnswers := func(answers []string) error {
		if p, ok := slot.(interface {
			ReconnectWithAnswers(context.Context, []string) error
		}); ok {
			return p.ReconnectWithAnswers(ctx, answers)
		}
		return slot.Reconnect(ctx)
	}
	const attempts = 5
	var last error
	for attempt := 0; attempt < attempts; attempt++ {
		err := slot.Reconnect(ctx)
		if err == nil {
			return nil
		}
		last = err
		switch {
		case needsPassword():
			answer, perr := prompter.Secret("SSH password for " + slot.Label() + ": ")
			if perr != nil {
				return promptFailed(slot, perr)
			}
			if err := reconnectPassword(answer); err == nil {
				return nil
			} else {
				last = err
			}
		case needsPassphrase():
			answer, perr := prompter.Secret("SSH key passphrase for " + slot.Label() + ": ")
			if perr != nil {
				return promptFailed(slot, perr)
			}
			if err := reconnectPassphrase(answer); err == nil {
				return nil
			} else {
				last = err
			}
		case needsKeyboard():
			ch, ok := slot.(interface {
				KeyboardInteractiveChallenge() (authority.KeyboardPrompt, bool)
			})
			if !ok {
				return actionableConnectError(slot, last)
			}
			prompt, ok := ch.KeyboardInteractiveChallenge()
			if !ok || len(prompt.Questions) == 0 {
				return actionableConnectError(slot, last)
			}
			answers := make([]string, len(prompt.Questions))
			for i, question := range prompt.Questions {
				label := question
				if prompt.Name != "" {
					label = prompt.Name + ": " + question
				}
				var answer string
				var perr error
				if i < len(prompt.Echos) && prompt.Echos[i] {
					answer, perr = prompter.Line(label + ": ")
				} else {
					answer, perr = prompter.Secret(label + ": ")
				}
				if perr != nil {
					return promptFailed(slot, perr)
				}
				answers[i] = answer
			}
			if err := reconnectAnswers(answers); err == nil {
				return nil
			} else {
				last = err
			}
		default:
			return actionableConnectError(slot, last)
		}
	}
	return actionableConnectError(slot, last)
}

// promptFailed wraps a terminal read failure with the actionable daemon error.
func promptFailed(slot authority.Slot, err error) error {
	return fmt.Errorf("daemon: SSH %s: reading the terminal prompt failed: %w", slot.Label(), err)
}

// actionableConnectError explains a foreground remote connection failure and the next step.
func actionableConnectError(slot authority.Slot, err error) error {
	label := "remote"
	if slot != nil && slot.Label() != "" {
		label = slot.Label()
	}
	if err == nil {
		err = errors.New("not connected")
	}
	return fmt.Errorf("daemon: SSH %s: %w; check the SSH host/user, key and agent (or start the daemon from a terminal to answer interactive prompts), then start the daemon again", label, err)
}
