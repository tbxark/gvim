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

	"github.com/tbxark/gvim/config"
	"github.com/tbxark/gvim/trust"
)

// RemoteRunner executes one shell command on the remote host and returns its stdout. The
// caller's closure owns the timeout and output bound; it must not apply this provider's
// environment.
type RemoteRunner func(ctx context.Context, command string) (string, error)

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

	// Detection state (one bounded remote probe).
	detected    trust.DetectedEnv
	found       bool
	detectedSet bool

	// Activation state: the recipe the plugin activated, and the captured delta.
	snippet   string
	dir       string
	kind      config.EnvKind
	set       []string
	unset     []string
	capturing bool
	lastErr   error
}

// NewRemote builds the provider for a remote workspace rooted at root (the remote
// absolute path; call SetRoot once the connection expanded a ~ root). None of the
// methods runs anything until Warm or Overlay is called: the app calls Warm in the
// background after the authority connects, so the UI never waits on the remote host.
func NewRemote(root string, detectors []config.EnvDetector, trusted func() bool, run RemoteRunner) *Remote {
	if len(detectors) == 0 {
		detectors = config.DefaultEnvDetectors()
	}
	return &Remote{
		root:      root,
		detectors: append([]config.EnvDetector(nil), detectors...),
		trusted:   trusted,
		run:       run,
	}
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
	p.detected, p.found, p.detectedSet = trust.DetectedEnv{}, false, false
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
	if hasRecipe && trusted {
		_ = p.Overlay()
	}
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
	p.detected, p.found, p.detectedSet = trust.DetectedEnv{}, false, false
	p.invalidateLocked()
	p.mu.Unlock()
}

// Overlay returns the set-only remote environment overlay ("KEY=VALUE", sorted): the delta
// the active recipe made to the remote login environment. It probes and captures lazily on
// the remote host (bounded by the supplied Runner); while a capture is in flight another
// caller gets the last cached delta, so no UI path waits. It returns nil when no recipe is
// active, the workspace is untrusted, or the capture failed.
func (p *Remote) Overlay() []string {
	p.ensureDetected(context.Background())
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

	set, unset, err := p.captureRemote(snippet, dir)

	p.mu.Lock()
	p.capturing = false
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

// ensureDetected runs the remote marker probe once.
func (p *Remote) ensureDetected(ctx context.Context) {
	p.mu.Lock()
	if p.detectedSet {
		p.mu.Unlock()
		return
	}
	root := p.root
	detectors := append([]config.EnvDetector(nil), p.detectors...)
	run := p.run
	p.mu.Unlock()

	det, found, _, err := detectRemote(ctx, root, detectors, run)

	p.mu.Lock()
	p.detected, p.found = det, found
	// A failed probe is retried by the next Warm/Overlay (a disconnected authority
	// reconnects); a successful one is cached until Refresh/SetRoot.
	p.detectedSet = err == nil
	p.mu.Unlock()
}

// captureRemote runs the remote baseline and activation shell and diffs their
// environments. Activation runs with set -a in the remote workspace directory.
func (p *Remote) captureRemote(snippet, dir string) (set, unset []string, err error) {
	p.mu.Lock()
	run := p.run
	root := p.root
	p.mu.Unlock()
	if run == nil {
		return nil, nil, errors.New("env: no remote runner")
	}
	if strings.TrimSpace(dir) == "" {
		dir = root
	}
	baseline, err := run(context.Background(), "cd "+shellQuoteRemote(dir)+" && env -0")
	if err != nil {
		return nil, nil, fmt.Errorf("env: reading the remote baseline environment: %w", err)
	}
	activated, err := run(context.Background(), "cd "+shellQuoteRemote(dir)+" && set -a; "+snippet+"\nenv -0")
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

// detectRemote probes the detector markers on the remote host with ONE bounded shell
// command that only tests existence, then applies the same detector order/require rules
// as trust.DetectEnv. Marker names come from the validated detector config; each is
// shell-quoted so a configured name can never become command syntax.
func detectRemote(ctx context.Context, root string, detectors []config.EnvDetector, run RemoteRunner) (trust.DetectedEnv, bool, bool, error) {
	if run == nil {
		return trust.DetectedEnv{}, false, false, errors.New("env: no remote runner")
	}
	var flat []string
	seen := map[string]bool{}
	for _, d := range detectors {
		for _, rel := range d.Markers {
			rel = strings.TrimSpace(rel)
			if rel == "" || filepath.IsAbs(rel) || seen[rel] {
				continue
			}
			seen[rel] = true
			flat = append(flat, rel)
		}
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
		}, true, incomplete, nil
	}
	return trust.DetectedEnv{}, false, incomplete, nil
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
