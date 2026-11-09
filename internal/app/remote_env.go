package app

// The remote environment provider's host seam (#R13-13). The provider is backend-agnostic
// (env.Remote): this file supplies the run closures that execute its probe and capture on
// the REMOTE host through the workspace authority's one-shot surface. Commands run with NO
// Env overlay: the capture must see the remote login environment, not this provider's own
// delta (that would recurse) and never the local process environment (the data leak the
// provider replaces).
//
// Detection is a passive marker probe and runs through plain sh -c even in an untrusted
// workspace, so it never sources an interactive rc from a project cwd. Activation capture
// is trusted-gated by env.Remote and uses the user's interactive login shell (bash -ilc /
// zsh -ilc when the probe finds one) so detectors whose snippet depends on interactive rc
// initialization see the same environment the integrated terminal does. If the interactive
// shell cannot be found or a capture fails, it falls back once to the previous sh -c path
// and records an explicit notice.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/tbxark/gvim/authority"
	"github.com/tbxark/gvim/env"
	"github.com/tbxark/gvim/plugin/hostos"
)

const (
	// remoteEnvCommandTimeout bounds one remote env probe or capture command.
	remoteEnvCommandTimeout = 15 * time.Second
	// remoteEnvMaxOutput matches the env package's capture bound.
	remoteEnvMaxOutput = 8 << 20
	// remoteEnvShellMarker precedes the real output of an interactive shell run; the
	// shell's rc files may print to stdout before the command starts.
	remoteEnvShellMarker = "\x00GVIM_ENV_OUT\x00"
	// remoteEnvShellProbe picks the interactive login shell. SHELL is preferred when it
	// is bash or zsh (the user's terminal shell); otherwise one installed bash/zsh is
	// used, and plain sh is the documented fallback.
	remoteEnvShellProbe = `if [ -n "${SHELL:-}" ]; then s=${SHELL##*/}; if [ "$s" = bash ] || [ "$s" = zsh ]; then command -v "$s" >/dev/null 2>&1 && { printf '%s' "$s"; exit 0; }; fi; fi
if command -v bash >/dev/null 2>&1; then printf bash; elif command -v zsh >/dev/null 2>&1; then printf zsh; else printf sh; fi`
)

// runRemoteEnvArgv runs one remote argv through the authority one-shot with the env
// capture's timeout and output bound. It never applies an environment overlay.
func runRemoteEnvArgv(ctx context.Context, slot func() authority.Slot, root func() string, argv []string) (hostos.ProcResult, error) {
	s := slot()
	if s == nil {
		return hostos.ProcResult{}, errors.New("remote environment: the remote authority is not mounted")
	}
	one := s.OneShot()
	if one == nil {
		return hostos.ProcResult{}, errors.New("remote environment: the remote authority has no one-shot surface")
	}
	ctx, cancel := context.WithTimeout(ctx, remoteEnvCommandTimeout)
	defer cancel()
	return one.Run(ctx, argv, hostos.ProcOptions{Cwd: root(), MaxOutput: remoteEnvMaxOutput})
}

// runRemoteEnvStdout is runRemoteEnvArgv plus the shared exit-code error formatting.
func runRemoteEnvStdout(ctx context.Context, slot func() authority.Slot, root func() string, argv []string) (string, error) {
	res, err := runRemoteEnvArgv(ctx, slot, root, argv)
	if err != nil {
		return "", err
	}
	if res.Code != 0 {
		msg := strings.TrimSpace(string(res.Stderr))
		if msg == "" {
			return "", fmt.Errorf("remote environment command failed (exit %d)", res.Code)
		}
		return "", fmt.Errorf("remote environment command failed (exit %d): %s", res.Code, msg)
	}
	return string(res.Stdout), nil
}

// newRemoteEnvRunner returns the env.RemoteRunner of a remote session for detection and
// the plain fallback path: sh -c, exactly as before. slot and root are called per command
// because the authority connects after the provider is built and the remote root may be
// expanded from ~ once the connection exists.
func newRemoteEnvRunner(slot func() authority.Slot, root func() string) env.RemoteRunner {
	return func(ctx context.Context, command string) (string, error) {
		return runRemoteEnvStdout(ctx, slot, root, []string{"sh", "-c", command})
	}
}

// remoteEnvShell is the app's capture runner: it detects the remote interactive login
// shell once, uses it for trusted activation captures, and falls back to sh -c with a
// clear notice when unavailable or failing. Detection probes stay on sh -c.
type remoteEnvShell struct {
	slot     func() authority.Slot
	root     func() string
	onNotice func(string)

	mu       sync.Mutex
	prepared bool
	// probing/probeWait single-flight the shell probe: Prepare, a capture and a
	// post-reconnect Reprobes never run two probes or read a half-updated shell.
	probing   bool
	probeWait chan struct{}
	shell     string
	notice    string
}

func newRemoteEnvShell(slot func() authority.Slot, root func() string, onNotice func(string)) *remoteEnvShell {
	return &remoteEnvShell{slot: slot, root: root, onNotice: onNotice}
}

// RunDetect is the env.RemoteRunner for passive marker detection: always plain sh -c.
func (r *remoteEnvShell) RunDetect(ctx context.Context, command string) (string, error) {
	return runRemoteEnvStdout(ctx, r.slot, r.root, []string{"sh", "-c", command})
}

// Prepare probes the interactive login shell once. It is called synchronously after the
// authority connects so the fallback notice can join the startup notices; a probe failure
// is not fatal (capture falls back to sh -c).
func (r *remoteEnvShell) Prepare(ctx context.Context) {
	r.ensurePrepared(ctx)
}

// Notice returns the current fallback/limitation notice, if any.
func (r *remoteEnvShell) Notice() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.notice
}

func (r *remoteEnvShell) ensurePrepared(ctx context.Context) {
	for {
		r.mu.Lock()
		if r.prepared {
			r.mu.Unlock()
			return
		}
		if r.probing {
			wait := r.probeWait
			r.mu.Unlock()
			select {
			case <-wait:
				continue
			case <-ctx.Done():
				return
			}
		}
		wait := make(chan struct{})
		r.probing, r.probeWait = true, wait
		r.mu.Unlock()
		shell, notice := r.probe(ctx)
		r.finishProbe(shell, notice, wait)
		return
	}
}

// Reprobes forces one new interactive-shell probe after a connection recovery and
// refreshes the fallback state/notice. It is called off the UI update loop; a concurrent
// capture waits for it through the same single-flight. A cancelled context abandons the
// wait without claiming a shell (the previous state is kept).
func (r *remoteEnvShell) Reprobes(ctx context.Context) {
	r.mu.Lock()
	r.prepared = false
	r.mu.Unlock()
	for {
		r.mu.Lock()
		if !r.probing {
			wait := make(chan struct{})
			r.probing, r.probeWait = true, wait
			r.mu.Unlock()
			shell, notice := r.probe(ctx)
			r.finishProbe(shell, notice, wait)
			return
		}
		wait := r.probeWait
		r.mu.Unlock()
		select {
		case <-wait:
			continue
		case <-ctx.Done():
			return
		}
	}
}

func (r *remoteEnvShell) probe(ctx context.Context) (shell, notice string) {
	out, err := runRemoteEnvStdout(ctx, r.slot, r.root, []string{"sh", "-c", remoteEnvShellProbe})
	if err != nil {
		return "sh", fmt.Sprintf("Remote environment: could not detect an interactive login shell on the host (%v); using sh -c (interactive init may differ from the terminal)", err)
	}
	switch strings.TrimSpace(out) {
	case "bash":
		return "bash", ""
	case "zsh":
		return "zsh", ""
	}
	return "sh", "Remote environment: no interactive bash/zsh login shell was found on the host; using sh -c (environment recipes that rely on interactive init may differ from the terminal)"
}

// finishProbe publishes one probe result and notifies the observer (an empty notice
// clears a previous fallback state).
func (r *remoteEnvShell) finishProbe(shell, notice string, wait chan struct{}) {
	r.mu.Lock()
	r.prepared, r.shell, r.notice = true, shell, notice
	r.probing = false
	if r.probeWait == wait {
		r.probeWait = nil
	}
	cb := r.onNotice
	r.mu.Unlock()
	close(wait)
	if cb != nil {
		cb(notice)
	}
}

// setNotice updates only the fallback notice, keeping the chosen shell (a per-capture
// interactive failure does not discard the shell a later re-probe selected).
func (r *remoteEnvShell) setNotice(notice string) {
	r.mu.Lock()
	r.notice = notice
	cb := r.onNotice
	r.mu.Unlock()
	if cb != nil {
		cb(notice)
	}
}

// RunCapture is the env.RemoteRunner for the trusted activation baseline/recipe. It uses
// <bash|zsh> -ilc when available and falls back to sh -c with an explicit notice if the
// interactive invocation fails. The shell's rc output before the command is discarded via
// a NUL marker so it cannot pollute the KEY=VALUE stream.
func (r *remoteEnvShell) RunCapture(ctx context.Context, command string) (string, error) {
	r.ensurePrepared(ctx)
	r.mu.Lock()
	shell := r.shell
	r.mu.Unlock()
	if shell == "" || shell == "sh" {
		return r.RunDetect(ctx, command)
	}
	wrapped := "printf '\000GVIM_ENV_OUT\000'; " + command
	res, err := runRemoteEnvArgv(ctx, r.slot, r.root, []string{shell, "-ilc", wrapped})
	if err == nil && res.Code == 0 {
		out := string(res.Stdout)
		if idx := strings.Index(out, remoteEnvShellMarker); idx >= 0 {
			return out[idx+len(remoteEnvShellMarker):], nil
		}
		err = fmt.Errorf("%s -ilc produced no environment-output marker", shell)
	} else if err == nil {
		msg := strings.TrimSpace(string(res.Stderr))
		if msg == "" {
			msg = fmt.Sprintf("exit %d", res.Code)
		}
		err = fmt.Errorf("%s -ilc: %s", shell, msg)
	}
	notice := fmt.Sprintf("Remote environment: %s -ilc failed (%v); using sh -c for this capture (interactive init may not be available)", shell, err)
	r.setNotice(notice)
	return r.RunDetect(ctx, command)
}

// remoteEnvMap converts set-only environment entries ("K=V") into the map the authority's
// ProcOptions takes. A malformed entry is dropped (it cannot name a variable).
func remoteEnvMap(entries []string) map[string]string {
	out := make(map[string]string, len(entries))
	for _, entry := range entries {
		if k, v, ok := strings.Cut(entry, "="); ok && k != "" {
			out[k] = v
		}
	}
	return out
}
