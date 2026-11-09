// Package env detects a project's environment at the workspace root and applies
// the activation of one environment to the processes the editor starts.
//
// Detection is passive: Detect scans marker files only and never runs anything.
// Activation is gated on workspace trust (a trusted workspace is the only place
// a project's activation snippet may run) and captures the environment by
// running the snippet in a bounded shell, lazily and cached on the environment
// input files. A capture that fails degrades to no overlay rather than an error
// at spawn time.
package env

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/tbxark/gvim/config"
	"github.com/tbxark/gvim/trust"
)

// ErrUntrusted is returned when activation is refused because the workspace is
// not trusted: the snippet runs project-controlled shell code.
var ErrUntrusted = errors.New("untrusted: activating an environment runs project code; trust the workspace first")

// Limits of one capture.
const (
	// maxSnippetBytes bounds an activation snippet.
	maxSnippetBytes = 4 << 10
	// maxCaptureBytes bounds the environment output of one capture.
	maxCaptureBytes = 8 << 20
	// captureTimeout bounds the two shell runs of one capture.
	captureTimeout = 10 * time.Second
)

// envInputFiles are the files whose contents define a project's environment.
// The capture cache is keyed on their modification times and sizes, so a
// changed .envrc/mise.toml re-captures on the next spawn.
var envInputFiles = []string{
	".envrc",
	"mise.toml",
	".mise.toml",
	".tool-versions",
	"pyvenv.cfg",
	".venv/pyvenv.cfg",
}

// EnvInputFiles returns the workspace-relative files whose changes affect the activated
// environment (a copy; the local fingerprint cache and the remote watcher both use it).
func EnvInputFiles() []string { return append([]string(nil), envInputFiles...) }

// EnvInputRootNames returns the top-level entries a workspace-root watch must treat as
// env-input changes: the direct files plus the directories that hold nested inputs
// (".venv" for ".venv/pyvenv.cfg"). A root watch reports direct children only, so a
// change inside .venv appears as a .venv directory event.
//
// EnvInputNestedDirs returns the workspace-relative parent directories of nested inputs
// (".venv"): the remote watcher must also watch those directories, because changing the
// content of .venv/pyvenv.cfg changes the file but not the .venv directory mtime the root
// watch sees.
func EnvInputNestedDirs() []string {
	seen := map[string]bool{}
	var out []string
	for _, name := range envInputFiles {
		i := strings.IndexByte(name, '/')
		if i <= 0 {
			continue
		}
		dir := name[:i]
		if seen[dir] {
			continue
		}
		seen[dir] = true
		out = append(out, dir)
	}
	return out
}

func EnvInputRootNames() []string {
	seen := map[string]bool{}
	var out []string
	for _, name := range envInputFiles {
		root := name
		if i := strings.IndexByte(root, '/'); i >= 0 {
			root = root[:i]
		}
		if root == "" || seen[root] {
			continue
		}
		seen[root] = true
		out = append(out, root)
	}
	return out
}

// Runner runs one shell command in dir and returns its standard output. It is a
// variable for tests; the default runs sh -c with a timeout.
type Runner func(dir, command string) (string, error)

// Info describes the active environment for the status row and the plugin API.
type Info struct {
	Name    string
	Kind    config.EnvKind
	Snippet string
	Dir     string
	Active  bool
}

// Provider is the activation state of one workspace. It is safe for concurrent
// use: spawn paths read it from their own goroutines.
type Provider struct {
	mu        sync.RWMutex
	root      string
	detectors []config.EnvDetector
	detected  trust.DetectedEnv
	found     bool
	trusted   func() bool
	runner    Runner

	// The active recipe. An empty snippet means no environment is applied.
	snippet string
	dir     string
	kind    config.EnvKind

	// Capture cache: the input fingerprint the delta was produced under and the
	// delta itself.
	cacheKey   string
	set        []string
	unset      []string
	lastErr    error
	incomplete bool
	capturing  bool
	// watchNotice is the local detector/marker watcher's degradation message ("" when
	// watching works or was not attempted), shown in the Settings Environment rows.
	watchNotice string
}

// New builds the provider for root with the configured detectors. It detects the
// environment immediately (shallow, no execution). trusted is consulted for the
// live trust level; it may be nil, which means untrusted.
func New(root string, detectors []config.EnvDetector, trusted func() bool) *Provider {
	p := &Provider{root: root, trusted: trusted, runner: runShell}
	if len(detectors) == 0 {
		detectors = config.DefaultEnvDetectors()
	}
	p.detectors = append([]config.EnvDetector(nil), detectors...)
	det, incomplete := trust.DetectEnv(root, detectors)
	p.detected, p.found, p.incomplete = det, det.Name != "", incomplete
	return p
}

// Detectors returns the detectors the provider detects with, in order.
func (p *Provider) Detectors() []config.EnvDetector {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return append([]config.EnvDetector(nil), p.detectors...)
}

// RefreshDetection re-runs the passive local marker/require scan with the current
// detector list and updates the detected environment. The app's local file watcher calls
// it off the UI update loop when a marker/require entry changes; the active recipe and its
// capture cache are untouched (detection and activation are separate, and the capture
// fingerprint already handles env-input content changes).
func (p *Provider) RefreshDetection() {
	p.mu.RLock()
	detectors := append([]config.EnvDetector(nil), p.detectors...)
	p.mu.RUnlock()
	det, incomplete := trust.DetectEnv(p.root, detectors)
	p.mu.Lock()
	p.detected, p.found, p.incomplete = det, det.Name != "", incomplete
	p.mu.Unlock()
}

// SetWatchNotice records the local detector watcher's state ("" when watching works).
func (p *Provider) SetWatchNotice(text string) {
	p.mu.Lock()
	p.watchNotice = text
	p.mu.Unlock()
}

// WatchNotice returns the local detector watcher's degradation message, or "".
func (p *Provider) WatchNotice() string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.watchNotice
}

// SetDetectors replaces the detector list at runtime (a config Reload that changed
// env.detectors) and re-runs the passive local marker scan immediately, so the Settings
// Environment/Detectors rows and the next spawn see the new list. Empty restores the
// built-in detectors. This is passive stat work bounded by the config's detector caps and
// runs on the Reload path, not the UI update loop; the active recipe and its capture cache
// are unaffected because detection and activation are separate.
func (p *Provider) SetDetectors(detectors []config.EnvDetector) {
	if len(detectors) == 0 {
		detectors = config.DefaultEnvDetectors()
	}
	detectors = append([]config.EnvDetector(nil), detectors...)
	det, incomplete := trust.DetectEnv(p.root, detectors)
	p.mu.Lock()
	p.detectors = detectors
	p.detected, p.found, p.incomplete = det, det.Name != "", incomplete
	p.mu.Unlock()
}

// Detected returns the environment detected at the workspace root, if any.
func (p *Provider) Detected() (trust.DetectedEnv, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.detected, p.found
}

// Trusted reports the live trust gate.
func (p *Provider) Trusted() bool {
	p.mu.RLock()
	fn := p.trusted
	p.mu.RUnlock()
	return fn != nil && fn()
}

// Active reports the active environment, if one is applied.
func (p *Provider) Active() (Info, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if strings.TrimSpace(p.snippet) == "" {
		return Info{}, false
	}
	return p.infoLocked(), true
}

// Info returns the provider state: the detected and active environment in one
// value (the Settings page and the plugin API read it).
func (p *Provider) Info() Info {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.infoLocked()
}

func (p *Provider) infoLocked() Info {
	active := strings.TrimSpace(p.snippet) != ""
	info := Info{Name: p.detected.Name, Kind: p.detected.Kind, Snippet: p.detected.Snippet, Dir: p.root, Active: active}
	if active {
		info.Kind = p.kind
		info.Snippet = p.snippet
		info.Dir = p.dir
	}
	return info
}

// Set activates snippet in dir with the given kind. Only a trusted workspace may
// activate anything: the snippet runs project shell code (and a path-only venv
// activation sources a repo file too), so the host gate mirrors Fresh core's.
func (p *Provider) Set(snippet, dir string, kind config.EnvKind) error {
	if strings.TrimSpace(snippet) == "" {
		return errors.New("invalid: the activation snippet is empty")
	}
	if len(snippet) > maxSnippetBytes {
		return fmt.Errorf("invalid: the activation snippet is longer than %d bytes", maxSnippetBytes)
	}
	if !p.Trusted() {
		return ErrUntrusted
	}
	if dir == "" {
		dir = p.root
	}
	if dir == "" {
		return errors.New("invalid: there is no workspace directory")
	}
	if kind != config.EnvKindPathOnly && kind != config.EnvKindShell {
		kind = config.EnvKindShell
	}
	p.mu.Lock()
	p.snippet, p.dir, p.kind = snippet, dir, kind
	p.invalidateLocked()
	p.mu.Unlock()
	return nil
}

// Clear deactivates the environment.
func (p *Provider) Clear() {
	p.mu.Lock()
	p.snippet = ""
	p.invalidateLocked()
	p.mu.Unlock()
}

// Refresh drops the capture cache, so the next spawn re-runs the activation
// snippet (Env: Reload and a changed .envrc use this).
func (p *Provider) Refresh() {
	p.mu.Lock()
	p.invalidateLocked()
	p.mu.Unlock()
}

func (p *Provider) invalidateLocked() {
	p.cacheKey = ""
	p.set, p.unset, p.lastErr = nil, nil, nil
}

// Overlay returns the set-only environment overlay of the active environment
// ("KEY=VALUE" entries, sorted), or nil when no environment is active, the
// workspace lost trust, or the capture failed. Spawn paths append it to their
// environment.
func (p *Provider) Overlay() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if strings.TrimSpace(p.snippet) == "" {
		return nil
	}
	if fn := p.trusted; fn == nil || !fn() {
		return nil
	}
	key := p.fingerprintLocked()
	if key == p.cacheKey {
		return append([]string(nil), p.set...)
	}
	if p.capturing {
		return append([]string(nil), p.set...)
	}
	p.capturing = true
	set, unset, err := p.captureLocked()
	p.capturing = false
	if err != nil {
		p.lastErr = err
		p.set, p.unset = nil, nil
		p.cacheKey = key
		return nil
	}
	p.set, p.unset = set, unset
	p.lastErr = nil
	p.cacheKey = key
	return append([]string(nil), p.set...)
}

// Unset returns the keys the activation removed from a clean shell, for the
// spawn paths that can express an unset by building a complete environment
// (the plugin process host). It shares the capture cache with Overlay.
func (p *Provider) Unset() []string {
	_ = p.Overlay()
	p.mu.RLock()
	defer p.mu.RUnlock()
	return append([]string(nil), p.unset...)
}

// LastError returns the last capture error, if the last capture failed.
func (p *Provider) LastError() error {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.lastErr
}

// SetRunner replaces the shell runner (tests use it to avoid exec).
func (p *Provider) SetRunner(r Runner) {
	p.mu.Lock()
	if r != nil {
		p.runner = r
	}
	p.invalidateLocked()
	p.mu.Unlock()
}

// EnvFor returns base with the active environment applied: the delta's keys are
// removed and the captured set entries appended. It is the exact environment a
// child process should see.
func (p *Provider) EnvFor(base []string) []string {
	overlay := p.Overlay()
	unset := p.Unset()
	if len(overlay) == 0 && len(unset) == 0 {
		return base
	}
	drop := append([]string(nil), unset...)
	for _, entry := range overlay {
		name, _, _ := strings.Cut(entry, "=")
		drop = append(drop, name)
	}
	return append(removeKeys(base, drop), overlay...)
}

func removeKeys(base, keys []string) []string {
	if len(keys) == 0 {
		return base
	}
	drop := make(map[string]bool, len(keys))
	for _, k := range keys {
		drop[k] = true
	}
	out := make([]string, 0, len(base))
	for _, entry := range base {
		name, _, _ := strings.Cut(entry, "=")
		if !drop[name] {
			out = append(out, entry)
		}
	}
	return out
}

// captureLocked runs the baseline and the activation shell and diffs their
// environments. The caller holds p.mu.
func (p *Provider) captureLocked() (set, unset []string, err error) {
	dir := p.dir
	if dir == "" {
		dir = p.root
	}
	fn := p.runner
	baseline, err := fn(dir, "env -0")
	if err != nil {
		return nil, nil, fmt.Errorf("env: reading the baseline environment: %w", err)
	}
	activated, err := fn(dir, "set -a; "+p.snippet+"\nenv -0")
	if err != nil {
		return nil, nil, fmt.Errorf("env: activating the environment: %w", err)
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

// fingerprintLocked hashes the environment input files' modification times and
// sizes. The caller holds p.mu.
func (p *Provider) fingerprintLocked() string {
	var b strings.Builder
	for _, name := range envInputFiles {
		info, err := os.Stat(filepath.Join(p.root, name))
		if err != nil {
			b.WriteString(name + ":-\n")
			continue
		}
		fmt.Fprintf(&b, "%s:%d:%d\n", name, info.ModTime().UnixNano(), info.Size())
	}
	return b.String()
}

// parseEnv parses NUL-separated KEY=VALUE entries.
func parseEnv(text string) map[string]string {
	out := make(map[string]string)
	for _, entry := range strings.Split(text, "\x00") {
		name, value, ok := strings.Cut(entry, "=")
		if !ok || name == "" {
			continue
		}
		out[name] = value
	}
	return out
}

// runShell is the default Runner: sh -c in dir with the parent environment,
// bounded by captureTimeout and maxCaptureBytes.
func runShell(dir, command string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), captureTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Dir = dir
	cmd.Env = os.Environ()
	cmd.WaitDelay = time.Second
	output, err := cmd.Output()
	if ctx.Err() != nil {
		return "", fmt.Errorf("timed out after %s", captureTimeout)
	}
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return "", fmt.Errorf("exit status %d: %s", exit.ExitCode(), strings.TrimSpace(string(exit.Stderr)))
		}
		return "", err
	}
	if len(output) > maxCaptureBytes {
		return "", fmt.Errorf("environment output is larger than %d bytes", maxCaptureBytes)
	}
	return string(output), nil
}
