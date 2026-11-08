package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/tbxark/gvim/authority"
	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/internal/pluginhost"
	"github.com/tbxark/gvim/plugin/hostos"
)

// The workspace authority of a session (docs/authority.md): the slot behind file access,
// one-shot processes, long-running children and the integrated terminal. A local launch
// mounts the local backend over the plugin host's OS; a remote launch mounts the SSH
// backend for its target and opens the remote terminal (remote file editing is the
// SFTP/remote-helper follow-up).
//
// Everything the session launches goes through the slot; no caller asks what kind the
// slot is.

// newAuthority mounts the slot of the session. It never dials a remote: the startup
// attempt (and the activation retry, with a password prompt when needed) call Reconnect,
// because a password prompt needs the running UI.
// mountAuthority builds the workspace authority slot of a launch. Tests replace it to
// inject an in-process fake authority (a remote daemon workspace without a real SSH
// carrier); production is newAuthority.
var mountAuthority = newAuthority

func newAuthority(opts Options, pw *pluginhost.Wiring, gate authority.Gate, gateEnv authority.GateEnv, env authority.EnvProvider) authority.Slot {
	if opts.Remote != nil {
		return authority.NewSSH(authority.SSHOptions{
			Spec:           *opts.Remote,
			Timeout:        15 * time.Second,
			KnownHostsPath: sshKnownHostsPath(),
			// OpenSSH user config (Host/HostName/User/Port/IdentityFile/ProxyJump/
			// IdentityAgent/ForwardAgent) is read by the pure-Go backend; it never
			// falls back to running ssh(1).
			ConfigPath: authority.DefaultSSHConfigPath(),
			// The UI supplies a password, an encrypted key's passphrase or a 2FA answer
			// after the matching failure (activation/background prompt). Returning the
			// sentinels records the request with the backend so the next reconnect can
			// carry the secret.
			Password: func(context.Context) (string, error) {
				return "", errors.New("no SSH password available yet")
			},
			Passphrase: func(context.Context, string) (string, error) {
				return "", authority.ErrNeedsPassphrase
			},
			KeyboardInteractive: func(context.Context, string, string, []string, []bool) ([]string, error) {
				return nil, authority.ErrNeedsKeyboardInteractive
			},
		})
	}
	var osys hostos.OS
	if pw != nil {
		osys = pw.OS
	}
	return authority.Local(authority.LocalOptions{
		OS:      osys,
		Gate:    gate,
		GateEnv: gateEnv,
		Env:     env,
	})
}

// attemptAuthorityConnect tries the startup dial of a remote authority and returns the
// notice to show when it fails. The slot then waits for activation to reconnect.
func attemptAuthorityConnect(ctx context.Context, slot authority.Slot) (string, error) {
	if slot == nil || slot.State().Kind == "local" {
		return "", nil
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	if err := slot.Reconnect(ctx); err != nil {
		return fmt.Sprintf("SSH %s: %v; activate the window to reconnect", slot.Label(), err), err
	}
	return "", nil
}

// remoteLocalRoot is the local process root of a remote launch: a remote launch opens no
// local files and no explorer, so the local-only pieces keep the current directory.
func remoteLocalRoot() string {
	wd, err := os.Getwd()
	if err != nil {
		return "."
	}
	return wd
}

// sshKnownHostsPath is the standard user known_hosts file; the authority reports the
// clear "add the host key first" error when it is missing.
func sshKnownHostsPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return home + "/.ssh/known_hosts"
}

// runGitGrepOneShot runs one git grep through the authority and returns stdout and the
// exit code, matching shellcmd.RunArgsEnv's text handling.
func runGitGrepOneShot(one authority.OneShot, root string, argv []string, extraEnv []string, timeout time.Duration) (string, int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	env := map[string]string{}
	for _, e := range extraEnv {
		if k, v, ok := strings.Cut(e, "="); ok {
			env[k] = v
		}
	}
	res, err := one.Run(ctx, argv, hostos.ProcOptions{Cwd: root, Env: env})
	if err != nil {
		if ctx.Err() != nil {
			return "", 0, fmt.Errorf("shell command timed out after %s", timeout)
		}
		return "", 0, err
	}
	text := strings.ReplaceAll(string(res.Stdout), "\r\n", "\n")
	return text, res.Code, nil
}

// authorityShellRunner turns the slot's one-shot into the shell/format runner the TUI
// uses: the same argv ("sh -c"), cwd, environment overlay, stdin and timeout as
// editor.RunShellJob, with the same error text a failed exit gets.
func authorityShellRunner(slot authority.Slot) func(editor.ShellJob) (string, error) {
	if slot == nil {
		return nil
	}
	one := slot.OneShot()
	return func(job editor.ShellJob) (string, error) {
		timeout := job.Timeout
		if timeout <= 0 {
			timeout = 30 * time.Second
		}
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		env := map[string]string{}
		for _, e := range job.Env {
			if k, v, ok := strings.Cut(e, "="); ok {
				env[k] = v
			}
		}
		res, err := one.Run(ctx, []string{"sh", "-c", job.Command}, hostos.ProcOptions{
			Cwd: job.Dir, Env: env, Stdin: []byte(job.Input),
		})
		if err != nil {
			if ctx.Err() != nil {
				return "", fmt.Errorf("shell command timed out after %s", timeout)
			}
			return "", fmt.Errorf("shell command: %w", err)
		}
		if res.Code != 0 {
			msg := fmt.Sprintf("shell command failed: exit status %d", res.Code)
			if s := strings.TrimSpace(string(res.Stderr)); s != "" {
				first, _, _ := strings.Cut(s, "\n")
				msg += ": " + first
			}
			return "", errors.New(msg)
		}
		return string(res.Stdout), nil
	}
}
