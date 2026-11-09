package app

// Project-root Deno LSP auto-selection. Fresh's deno_lsp plugin replaces the configured
// TypeScript/JavaScript server with `deno lsp` when the active project root has a
// deno.json/deno.jsonc and the Deno runtime is installed, keeping the configured entry as a
// baseline. gvim does the same with two safety rules: every confirmation goes through the
// workspace authority (a remote workspace is never checked against the local disk/PATH), and
// an explicit or disabled user LSP configuration is never overridden.
//
// The probe is asynchronous and bounded; its result is installed as a per-root provider on
// the LSP port, so a delayed answer can only affect a manager created for the exact probed
// root. The provider re-checks the live config when it is called, so a config change made
// after the probe cannot let a stale decision override a new explicit/disabled state.

import (
	"context"
	"errors"
	"io/fs"
	"path"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"go.lsp.dev/protocol"

	"github.com/tbxark/gvim/authority"
	"github.com/tbxark/gvim/config"
	"github.com/tbxark/gvim/lsp/manager"
	"github.com/tbxark/gvim/plugin/hostos"
)

const (
	// denoProbeTimeout bounds the one authority probe (two Stat calls plus one command).
	denoProbeTimeout = 5 * time.Second
	// denoProbeMaxOutput bounds the `command -v deno` helper's output.
	denoProbeMaxOutput = 4 << 10
)

// denoServerProviderPort is the narrow LSP-port seam used for auto-selection. It is
// optional: a port that does not implement it simply keeps the configured servers.
type denoServerProviderPort interface {
	SetRootServerProvider(root string, provider func(language string) (manager.ServerConfig, bool))
	ClearRootServerProvider(root string)
}

// denoAutoSelector owns the async Deno probe of one workspace root. Start may be called
// again after a reconnect: it increments the generation and clears the old provider before
// probing, so a late answer from the previous carrier cannot apply.
type denoAutoSelector struct {
	lsp    denoServerProviderPort
	slot   authority.Slot
	root   string
	remote bool
	gate   authority.Gate
	env    func() []string
	cfg    func() config.Config
	gen    atomic.Uint64
	mu     sync.Mutex
	// cancel stops the probe of the current root when the project root changes or a newer
	// probe starts. It is nil when no probe is in flight.
	cancel context.CancelFunc
}

// newDenoAutoSelector returns the selector for root, or nil when the LSP port has no
// provider seam or the inputs are missing.
func newDenoAutoSelector(lsp any, slot authority.Slot, root string, remote bool, gate authority.Gate, env func() []string, cfg func() config.Config) *denoAutoSelector {
	port, ok := lsp.(denoServerProviderPort)
	if !ok || slot == nil || root == "" || cfg == nil {
		return nil
	}
	return &denoAutoSelector{lsp: port, slot: slot, root: root, remote: remote, gate: gate, env: env, cfg: cfg}
}

// Start launches one bounded probe for the selector's current root. The previous provider
// is cleared immediately so the window while a new carrier/root is being probed never uses
// a stale decision. Rebind cancels a probe for a root that is no longer active.
func (s *denoAutoSelector) Start(ctx context.Context) {
	if s == nil {
		return
	}
	s.mu.Lock()
	if s.cancel != nil {
		s.cancel()
	}
	probeCtx, cancel := context.WithCancel(ctx)
	s.cancel = cancel
	root := s.root
	s.mu.Unlock()
	gen := s.gen.Add(1)
	s.lsp.ClearRootServerProvider(root)
	go func() {
		defer cancel()
		if !s.probe(probeCtx, root) {
			if gen == s.gen.Load() && ctx.Err() == nil {
				s.lsp.ClearRootServerProvider(root)
			}
			return
		}
		if gen != s.gen.Load() || ctx.Err() != nil {
			return
		}
		s.lsp.SetRootServerProvider(root, s.provider)
	}()
}

// Rebind moves auto-selection to a new project root: it cancels the old probe, clears the
// old root's provider (so future managers there restore the baseline) and probes the new
// root. Existing managers keep whatever they were created with.
func (s *denoAutoSelector) Rebind(ctx context.Context, root string) {
	if s == nil || root == "" {
		return
	}
	s.mu.Lock()
	old := s.root
	s.mu.Unlock()
	if old != root {
		s.lsp.ClearRootServerProvider(old)
	}
	s.mu.Lock()
	s.root = root
	s.mu.Unlock()
	s.Start(ctx)
}

// provider is evaluated when a manager for the exact root is created. It re-reads the live
// config each time: an explicit entry or a disabled language added after the probe wins.
func (s *denoAutoSelector) provider(language string) (manager.ServerConfig, bool) {
	if !denoLanguageEligible(s.cfg(), language) {
		return manager.ServerConfig{}, false
	}
	return denoServerConfig(), true
}

// probe confirms the Deno config and runtime through the authority only. Any error, timeout
// or cancellation is a no; no local stat/PATH fallback is attempted.
func (s *denoAutoSelector) probe(parent context.Context, root string) bool {
	cfg := s.cfg()
	if len(denoEligibleLanguages(cfg)) == 0 {
		return false
	}
	ctx, cancel := context.WithTimeout(parent, denoProbeTimeout)
	defer cancel()
	if !s.hasDenoConfig(ctx, root) {
		return false
	}
	return s.denoAvailable(ctx, root)
}

// hasDenoConfig checks deno.json/deno.jsonc through the authority file system.
func (s *denoAutoSelector) hasDenoConfig(ctx context.Context, root string) bool {
	files := s.slot.Files()
	if files == nil {
		return false
	}
	join := filepath.Join
	if s.remote {
		join = path.Join
	}
	for _, name := range []string{"deno.json", "deno.jsonc"} {
		st, err := files.Stat(ctx, join(root, name))
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			return false
		}
		if st.Kind == "file" {
			return true
		}
	}
	return false
}

// denoAvailable runs the fixed command -v deno through the authority one-shot, under the
// same trust gate every workspace command uses. MergeStderr keeps the probe bounded; a
// missing/denied/disconnected authority is a no.
func (s *denoAutoSelector) denoAvailable(ctx context.Context, root string) bool {
	one := s.slot.OneShot()
	if one == nil {
		return false
	}
	argv := []string{"sh", "-c", "command -v deno >/dev/null 2>&1"}
	if s.gate != nil {
		if err := s.gate(ctx, argv, root); err != nil {
			return false
		}
	}
	opts := hostos.ProcOptions{Cwd: root, MaxOutput: denoProbeMaxOutput, MergeStderr: true}
	if s.env != nil {
		opts.Env = remoteEnvMap(s.env())
	}
	res, err := one.Run(ctx, argv, opts)
	if err != nil {
		return false
	}
	return res.Code == 0
}

// denoEligibleLanguages lists the languages whose built-in server may be replaced: only
// TypeScript and JavaScript (Fresh's deno_lsp set), never a language with an explicit lsp
// entry or a disabled language.
func denoEligibleLanguages(cfg config.Config) []string {
	if !cfg.LSPEnabled {
		return nil
	}
	var out []string
	for _, language := range []string{"typescript", "javascript"} {
		if denoLanguageEligible(cfg, language) {
			out = append(out, language)
		}
	}
	return out
}

// denoLanguageEligible reports whether language may be auto-selected. An explicit lsp entry
// (even one with an empty command) and a disabled language are both user decisions and keep
// their configured server.
func denoLanguageEligible(cfg config.Config, language string) bool {
	if !cfg.LSPEnabled {
		return false
	}
	if language != "typescript" && language != "javascript" {
		return false
	}
	spec, ok := manager.DefaultFor(language)
	if !ok {
		return false
	}
	for _, name := range spec.Names() {
		if cfg.LSPDisabled[name] {
			return false
		}
		if _, explicit := cfg.LSP[name]; explicit {
			return false
		}
	}
	return true
}

// denoServerConfig is the server Fresh's deno_lsp installs. AutoStart stays false, as Fresh
// does: auto-selection must not silently spawn a process; the user starts it explicitly (the
// language status then shows a manual server).
func denoServerConfig() manager.ServerConfig {
	return manager.ServerConfig{
		Command:               []string{"deno"},
		Args:                  []string{"lsp"},
		Name:                  "deno",
		InitializationOptions: protocol.LSPAny([]byte("{\"enable\":true}")),
		AutoStart:             false,
	}
}
