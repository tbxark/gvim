package env

// The remote environment provider (#R13-13): detection and activation happen on the
// REMOTE host, through the active backend, exactly as Fresh's env provider re-captures
// through a caller-supplied run closure. It replaces the local provider for a remote
// workspace, so the local overlay (and any secret in it) is never shipped to the remote
// host: every entry this provider hands to a spawn was captured on that host.
//
// The provider is backend-agnostic: the app supplies a Runner that executes one shell
// command on the remote host (the authority's one-shot surface, with the raw login
// environment - it must not apply this provider's env, or capture would recurse).
// Detection is one bounded remote shell probe of the detector markers (an existence test
// only, like the local passive scan); activation runs the detector's snippet on the
// remote host and diffs its environment against the remote login baseline, so the overlay
// carries only what the recipe changed. Trust gates activation exactly like the local
// provider: an untrusted workspace never runs the snippet.

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/tbxark/gvim/config"
	"github.com/tbxark/gvim/trust"
)

// RemoteRunner executes one shell command on the remote host and returns its stdout. The
// caller's closure owns the timeout and output bound; it must not apply this provider's
// environment.
type RemoteRunner func(ctx context.Context, command string) (string, error)

// MarkerProber checks the existence of workspace-relative paths on the remote host
// WITHOUT running a shell (the app backs it with authority.FileSystem.Stat). It is used
// for detection before a workspace is trusted, where the remote command gate would refuse
// the shell probe. The caller bounds the path count and timeout; the prober must return a
// map containing only existing paths and stop on ctx cancellation.
type MarkerProber func(ctx context.Context, root string, paths []string) (map[string]bool, error)

// MaxMarkerProbePaths bounds one authority-backed marker probe. The config allows up to
// 64 detectors x (16 markers + 16 require) paths; a workspace whose detector set needs
// more than this is refused with a clear notice instead of issuing unbounded Stat calls.
const MaxMarkerProbePaths = 256

// Remote is the environment provider of one remote workspace. It is safe for concurrent
// use; the spawn paths read it from their own goroutines and never block the UI update
// loop on a remote capture (the app warms it after connecting, and spawn paths get the
// cached overlay while a capture is in flight).
type Remote struct {
	mu        sync.Mutex
	root      string
	detectors []config.EnvDetector
	trusted   func() bool
	run       RemoteRunner
	// markerProbe, when set, is the shell-free authority-backed existence probe used in an
	// untrusted workspace (and as a trusted fallback when no shell runner exists).
	markerProbe MarkerProber
	// captureRun runs the activation baseline/recipe. The app supplies an
	// interactive-login-shell runner here (bash/zsh -ilc when available) while detection
	// stays on the plain sh -c runner: detection is passive and may run in an untrusted
	// workspace, so it must not source the user's interactive rc through a project cwd.
	// Nil falls back to run.
	captureRun RemoteRunner

	// Detection state (one bounded remote existence probe). Detection is trust-gated:
	// the probe only runs in a trusted workspace; an untrusted workspace gets an explicit
	// notice and no remote command. detectedTrusted records which gate produced the cache
	// so a later trust change re-probes.
	detected        trust.DetectedEnv
	found           bool
	detectedSet     bool
	detectedTrusted bool
	detectionNotice string
	// detectionGen increments whenever the inputs of a detection probe change (detector
	// list, root, marker prober) or the cached detection is invalidated (Refresh). An
	// in-flight probe snapshots it and discards its result if it changed, so an older
	// probe can never overwrite a newer invalidation/config.
	detectionGen uint64

	// Activation state: the recipe the plugin activated, and the captured delta.
	snippet   string
	dir       string
	kind      config.EnvKind
	set       []string
	unset     []string
	capturing bool
	lastErr   error
	// notice is a user-facing fallback/limitation message from the app's runner (for
	// example "interactive shell failed; using sh -c"); it is shown in the startup
	// notice and the Settings Environment rows.
	notice string
	// watchNotice is the state of the env-input file watcher on the remote host (an
	// unsupported watch capability or a retryable failure), for the Settings rows.
	watchNotice string
}

// NewRemote builds the provider for a remote workspace rooted at root (the remote
// absolute path; call SetRoot once the connection expanded a ~ root). None of the
// methods runs anything until Warm or Overlay is called: the app calls Warm in the
// background after the authority connects, so the UI never waits on the remote host.
func NewRemote(root string, detectors []config.EnvDetector, trusted func() bool, run RemoteRunner, captureRun ...RemoteRunner) *Remote {
	if len(detectors) == 0 {
		detectors = config.DefaultEnvDetectors()
	}
	p := &Remote{
		root:      root,
		detectors: append([]config.EnvDetector(nil), detectors...),
		trusted:   trusted,
		run:       run,
	}
	if len(captureRun) > 0 && captureRun[0] != nil {
		p.captureRun = captureRun[0]
	}
	return p
}

// SetMarkerProber installs the shell-free authority-backed marker probe. It is used in an
// untrusted workspace so detection can still show the remote facts without executing a
// remote command; activation remains trust-gated.
func (p *Remote) SetMarkerProber(probe MarkerProber) {
	p.mu.Lock()
	p.markerProbe = probe
	p.detectedSet = false
	p.detectionGen++
	p.mu.Unlock()
}

// SetNotice records the user-facing fallback/limitation message of the capture runner.
func (p *Remote) SetNotice(text string) {
	p.mu.Lock()
	p.notice = text
	p.mu.Unlock()
}

// Notice returns the last user-facing fallback/limitation message, or "".
func (p *Remote) Notice() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.notice
}

// SetWatchNotice records the env-input file watcher state ("" when watching works).
func (p *Remote) SetWatchNotice(text string) {
	p.mu.Lock()
	p.watchNotice = text
	p.mu.Unlock()
}

// WatchNotice returns the env-input file watcher state, or "" when watching works or has
// not been attempted.
func (p *Remote) WatchNotice() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.watchNotice
}

// DetectionNotice returns why remote marker detection is disabled or failed (untrusted
// workspace, remote probe failure), or "" when detection is healthy/trusted.
func (p *Remote) DetectionNotice() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.detectionNotice
}

// Root returns the remote root the probes use.
func (p *Remote) Root() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.root
}

// SetRoot sets the remote root once the connection resolved it (a ~ root is expanded on
// the remote host). It drops the detection and capture state, so the next Warm/Overlay
// probes the real root.
func (p *Remote) SetRoot(root string) {
	p.mu.Lock()
	if root != "" {
		p.root = root
	}
	p.detected, p.found, p.detectedSet, p.detectedTrusted = trust.DetectedEnv{}, false, false, false
	p.detectionNotice = ""
	p.detectionGen++
	p.invalidateLocked()
	p.mu.Unlock()
}

// Warm probes the remote markers and captures the activated environment when a recipe is
// active. The app runs it in a goroutine after connecting; a failure is recorded (and kept
// in LastError for a capture), it never blocks a UI update.
func (p *Remote) Warm(ctx context.Context) {
	p.ensureDetected(ctx)
	p.mu.Lock()
	hasRecipe := strings.TrimSpace(p.snippet) != ""
	trusted := p.trusted != nil && p.trusted()
	p.mu.Unlock()
	if hasRecipe && trusted && ctx.Err() == nil {
		_ = p.overlay(ctx)
	}
}

// SetDetectors replaces the configured detectors at runtime (a config Reload that changed
// env.detectors). It invalidates detection/capture so the next Warm/Overlay re-probes with
// the new markers; the app's watcher re-plans its bounded watch targets from the same list.
func (p *Remote) SetDetectors(detectors []config.EnvDetector) {
	if len(detectors) == 0 {
		detectors = config.DefaultEnvDetectors()
	}
	p.mu.Lock()
	p.detectors = append([]config.EnvDetector(nil), detectors...)
	p.detectionGen++
	p.detected, p.found = trust.DetectedEnv{}, false
	p.detectedSet, p.detectedTrusted = false, false
	p.detectionNotice = ""
	p.invalidateLocked()
	p.mu.Unlock()
}

// Detectors returns the configured detectors.
func (p *Remote) Detectors() []config.EnvDetector {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]config.EnvDetector(nil), p.detectors...)
}

// Detected returns the environment detected on the remote host, if a Warm/Overlay probe
// has run. Unlike the local provider it never probes synchronously here: the Settings rows
// read the cached answer and the app warms it off the UI loop.
func (p *Remote) Detected() (trust.DetectedEnv, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.detected, p.found
}

// Info returns the detected and active environment state.
func (p *Remote) Info() Info {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.infoLocked()
}

func (p *Remote) infoLocked() Info {
	active := strings.TrimSpace(p.snippet) != ""
	info := Info{Name: p.detected.Name, Kind: p.detected.Kind, Snippet: p.detected.Snippet, Dir: p.root, Active: active}
	if active {
		info.Kind = p.kind
		info.Snippet = p.snippet
		info.Dir = p.dir
	}
	return info
}

// Active reports the activated environment, if a recipe is active.
func (p *Remote) Active() (Info, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if strings.TrimSpace(p.snippet) == "" {
		return Info{}, false
	}
	return p.infoLocked(), true
}

// Trusted reports the live trust gate.
func (p *Remote) Trusted() bool {
	p.mu.Lock()
	fn := p.trusted
	p.mu.Unlock()
	return fn != nil && fn()
}

// Set activates snippet (run on the remote host) with kind. Only a trusted workspace may
// activate: the snippet is project-controlled shell code.
func (p *Remote) Set(snippet, dir string, kind config.EnvKind) error {
	if strings.TrimSpace(snippet) == "" {
		return errors.New("invalid: the activation snippet is empty")
	}
	if len(snippet) > maxSnippetBytes {
		return fmt.Errorf("invalid: the activation snippet is longer than %d bytes", maxSnippetBytes)
	}
	if !p.Trusted() {
		return ErrUntrusted
	}
	p.mu.Lock()
	if dir == "" {
		dir = p.root
	}
	if dir == "" {
		p.mu.Unlock()
		return errors.New("invalid: there is no remote workspace directory")
	}
	if kind != config.EnvKindPathOnly && kind != config.EnvKindShell {
		kind = config.EnvKindShell
	}
	p.snippet, p.dir, p.kind = snippet, dir, kind
	p.invalidateLocked()
	p.mu.Unlock()
	return nil
}

// Clear deactivates the environment.
func (p *Remote) Clear() {
	p.mu.Lock()
	p.snippet = ""
	p.invalidateLocked()
	p.mu.Unlock()
}

// Refresh drops the detection and capture state, so the next Warm/Overlay re-probes and
// re-captures on the remote host (Env: Reload, a changed .envrc).
func (p *Remote) Refresh() {
	p.mu.Lock()
	p.detected, p.found, p.detectedSet, p.detectedTrusted = trust.DetectedEnv{}, false, false, false
	p.detectionNotice = ""
	p.detectionGen++
	p.invalidateLocked()
	p.mu.Unlock()
}

// Overlay returns the set-only remote environment overlay ("KEY=VALUE", sorted): the delta
// the active recipe made to the remote login environment. It probes and captures lazily on
// the remote host (bounded by the supplied Runner); while a capture is in flight another
// caller gets the last cached delta, so no UI path waits. It returns nil when no recipe is
// active, the workspace is untrusted, or the capture failed.
// remoteDetectTimeout bounds a marker probe triggered from a spawn path (Overlay has no
// caller context); Warm/refresh probes pass their session/watcher context instead.
const remoteDetectTimeout = 15 * time.Second

func (p *Remote) Overlay() []string {
	detectCtx, cancel := context.WithTimeout(context.Background(), remoteDetectTimeout)
	p.ensureDetected(detectCtx)
	cancel()
	return p.overlay(context.Background())
}

// overlay is Overlay with a caller context: a background warm/invalidation passes the
// watcher/session context so cancellation reaches the two remote capture commands. A
// cancelled capture keeps the previous delta instead of clearing it (a reload interrupted
// by shutdown must not erase a valid environment).
func (p *Remote) overlay(ctx context.Context) []string {
	p.ensureDetected(ctx)
	p.mu.Lock()
	if strings.TrimSpace(p.snippet) == "" {
		p.mu.Unlock()
		return nil
	}
	if fn := p.trusted; fn == nil || !fn() {
		p.mu.Unlock()
		return nil
	}
	if p.capturing {
		out := append([]string(nil), p.set...)
		p.mu.Unlock()
		return out
	}
	p.capturing = true
	snippet, dir := p.snippet, p.dir
	p.mu.Unlock()

	set, unset, err := p.captureRemote(ctx, snippet, dir)

	p.mu.Lock()
	p.capturing = false
	if err != nil && ctx.Err() != nil {
		// Keep the last captured delta: a cancellation is not a capture change.
		out := append([]string(nil), p.set...)
		p.mu.Unlock()
		return out
	}
	if err != nil {
		p.lastErr = err
		p.set, p.unset = nil, nil
	} else {
		p.lastErr = nil
		p.set, p.unset = set, unset
	}
	out := append([]string(nil), p.set...)
	p.mu.Unlock()
	return out
}

// Unset returns the keys the activation removed from the remote login shell.
func (p *Remote) Unset() []string {
	_ = p.Overlay()
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.unset...)
}

// LastError returns the last capture error, if the last capture failed.
func (p *Remote) LastError() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.lastErr
}

// ensureDetected probes the remote markers once. In a trusted workspace it runs one
// bounded, cancellable sh existence command through the authority one-shot gate. Before
// trust that gate would refuse a shell command, so detection uses the authority-backed
// shell-free MarkerProber (FileSystem.Stat) when the app supplied one; the result is shown
// read-only and activation stays trust-gated. A failed probe is not cached: it clears the
// result and records an explicit notice, retried on the next Warm/Overlay. The number of
// paths and the wall time are bounded.
func (p *Remote) ensureDetected(ctx context.Context) {
	p.mu.Lock()
	trusted := p.trusted != nil && p.trusted()
	if p.detectedSet && p.detectedTrusted == trusted {
		p.mu.Unlock()
		return
	}
	root := p.root
	detectors := append([]config.EnvDetector(nil), p.detectors...)
	gen := p.detectionGen
	run := p.run
	prober := p.markerProbe
	p.mu.Unlock()

	var (
		det        trust.DetectedEnv
		found      bool
		incomplete bool
		err        error
	)
	if !trusted && prober == nil {
		p.mu.Lock()
		if p.detectionGen != gen {
			p.mu.Unlock()
			return
		}
		p.detected, p.found = trust.DetectedEnv{}, false
		p.detectedSet, p.detectedTrusted = true, false
		p.detectionNotice = "Remote environment: marker detection needs a trusted workspace (activation is trust-gated too)"
		p.mu.Unlock()
		return
	}
	if !trusted || run == nil {
		det, found, incomplete, err = detectRemoteProbe(ctx, root, detectors, prober)
	} else {
		det, found, incomplete, err = detectRemote(ctx, root, detectors, run)
	}
	_ = incomplete

	p.mu.Lock()
	if p.detectionGen != gen {
		// A runtime detector change landed while this probe was in flight: its result is
		// for the old marker set. Do not cache it; the next Warm/Overlay probes the new set.
		p.mu.Unlock()
		return
	}
	p.detectedTrusted = trusted
	if err != nil {
		p.detected, p.found = trust.DetectedEnv{}, false
		p.detectedSet = false
		p.detectionNotice = fmt.Sprintf("Remote environment: marker detection failed: %v; no detector is active until Env: Reload/reconnect succeeds", err)
	} else {
		p.detected, p.found = det, found
		p.detectedSet = true
		p.detectionNotice = ""
	}
	p.mu.Unlock()
}

// detectRemoteProbe is the shell-free detection path: one bounded authority probe over the
// marker+require existence set, then the same trust.DetectEnv rules as the shell path.
func detectRemoteProbe(ctx context.Context, root string, detectors []config.EnvDetector, prober MarkerProber) (trust.DetectedEnv, bool, bool, error) {
	if prober == nil {
		return trust.DetectedEnv{}, false, false, errors.New("env: no shell-free marker prober")
	}
	paths, err := remoteMarkerPaths(detectors)
	if err != nil {
		return trust.DetectedEnv{}, false, false, err
	}
	exists, err := prober(ctx, root, paths)
	if err != nil {
		return trust.DetectedEnv{}, false, false, fmt.Errorf("env: probing the remote markers through the authority: %w", err)
	}
	det, found, incomplete := detectedFromExists(root, detectors, exists)
	return det, found, incomplete, nil
}

// captureRemote runs the remote baseline and activation shell and diffs their
// environments. Activation runs with set -a in the remote workspace directory.
func (p *Remote) captureRemote(ctx context.Context, snippet, dir string) (set, unset []string, err error) {
	p.mu.Lock()
	run := p.captureRun
	if run == nil {
		run = p.run
	}
	root := p.root
	p.mu.Unlock()
	if run == nil {
		return nil, nil, errors.New("env: no remote runner")
	}
	if strings.TrimSpace(dir) == "" {
		dir = root
	}
	baseline, err := run(ctx, "cd "+shellQuoteRemote(dir)+" && env -0")
	if err != nil {
		return nil, nil, fmt.Errorf("env: reading the remote baseline environment: %w", err)
	}
	activated, err := run(ctx, "cd "+shellQuoteRemote(dir)+" && set -a; "+snippet+"\nenv -0")
	if err != nil {
		return nil, nil, fmt.Errorf("env: activating the remote environment: %w", err)
	}
	if len(baseline) > maxCaptureBytes || len(activated) > maxCaptureBytes {
		return nil, nil, fmt.Errorf("env: remote environment output is larger than %d bytes", maxCaptureBytes)
	}
	baseEnv := parseEnv(baseline)
	activeEnv := parseEnv(activated)
	for name, value := range activeEnv {
		if old, ok := baseEnv[name]; !ok || old != value {
			set = append(set, name+"="+value)
		}
	}
	for name := range baseEnv {
		if _, ok := activeEnv[name]; !ok {
			unset = append(unset, name)
		}
	}
	sort.Strings(set)
	sort.Strings(unset)
	return set, unset, nil
}

func (p *Remote) invalidateLocked() {
	p.set, p.unset = nil, nil
	p.lastErr = nil
}

// remoteMarkerPaths returns the deduplicated marker+require existence set, validated and
// bounded. Require paths are included because a detector such as .venv (needing
// .venv/bin/python) must not match from a marker alone; that is the same rule as
// trust.DetectEnv. The bound keeps an authority-backed Stat probe from becoming unbounded.
func remoteMarkerPaths(detectors []config.EnvDetector) ([]string, error) {
	var flat []string
	seen := map[string]bool{}
	addPath := func(rel string) {
		rel = strings.TrimSpace(rel)
		if rel == "" || filepath.IsAbs(rel) || seen[rel] {
			return
		}
		seen[rel] = true
		flat = append(flat, rel)
	}
	for _, d := range detectors {
		for _, rel := range d.Markers {
			addPath(rel)
		}
		for _, rel := range d.Require {
			addPath(rel)
		}
	}
	if len(flat) > MaxMarkerProbePaths {
		return nil, fmt.Errorf("env: %d marker/require paths exceed the %d-path remote probe bound; narrow env.detectors", len(flat), MaxMarkerProbePaths)
	}
	return flat, nil
}

// detectedFromExists applies the trust.DetectEnv order/require rules to an existence set.
func detectedFromExists(root string, detectors []config.EnvDetector, exists map[string]bool) (trust.DetectedEnv, bool, bool) {
	anyExists := func(paths []string) bool {
		for _, rel := range paths {
			rel = strings.TrimSpace(rel)
			if rel != "" && !filepath.IsAbs(rel) && exists[rel] {
				return true
			}
		}
		return false
	}
	incomplete := false
	for _, d := range detectors {
		if !anyExists(d.Markers) {
			continue
		}
		if len(d.Require) > 0 && !anyExists(d.Require) {
			incomplete = true
			continue
		}
		return trust.DetectedEnv{
			Name:    d.Name,
			Kind:    d.Kind,
			Snippet: strings.ReplaceAll(d.Snippet, "{dir}", root),
		}, true, incomplete
	}
	return trust.DetectedEnv{}, false, incomplete
}

// detectRemote probes the detector markers on the remote host with ONE bounded shell
// command that only tests existence, then applies the same detector order/require rules
// as trust.DetectEnv. Marker names come from the validated detector config; each is
// shell-quoted so a configured name can never become command syntax. This path runs only
// in a trusted workspace (the authority command gate would refuse it otherwise).
func detectRemote(ctx context.Context, root string, detectors []config.EnvDetector, run RemoteRunner) (trust.DetectedEnv, bool, bool, error) {
	if run == nil {
		return trust.DetectedEnv{}, false, false, errors.New("env: no remote runner")
	}
	flat, err := remoteMarkerPaths(detectors)
	if err != nil {
		return trust.DetectedEnv{}, false, false, err
	}
	var b strings.Builder
	b.WriteString("cd " + shellQuoteRemote(root) + " && ")
	if len(flat) == 0 {
		b.WriteString("true")
	} else {
		for i, rel := range flat {
			if i > 0 {
				b.WriteString("; ")
			}
			fmt.Fprintf(&b, "if [ -e %s ]; then printf '%%s\\n' %d; fi", shellQuoteRemote(rel), i)
		}
	}
	out, err := run(ctx, b.String())
	if err != nil {
		return trust.DetectedEnv{}, false, false, fmt.Errorf("env: probing the remote markers: %w", err)
	}
	exists := map[string]bool{}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		idx := 0
		if _, err := fmt.Sscanf(line, "%d", &idx); err != nil || idx < 0 || idx >= len(flat) {
			continue
		}
		exists[flat[idx]] = true
	}
	det, found, incomplete := detectedFromExists(root, detectors, exists)
	return det, found, incomplete, nil
}

// shellQuoteRemote quotes a string for one POSIX shell word: single quotes around it, an
// embedded single quote written as the usual quote-backslash-quote sequence. The remote
// probe and the activation command compose ONLY the fixed shell syntax around these
// quoted words.
func shellQuoteRemote(s string) string {
	if s == "" {
		return "''"
	}
	sq := "'"
	repl := sq + "\"" + sq + "\"" + sq
	return sq + strings.ReplaceAll(s, sq, repl) + sq
}
