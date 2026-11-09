package app

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/tbxark/gvim/authority"
	"github.com/tbxark/gvim/authority/authoritytest"
	"github.com/tbxark/gvim/config"
	"github.com/tbxark/gvim/lsp/manager"
	"github.com/tbxark/gvim/plugin/hostos"
)

// denoFakeFiles answers Stat from a map; every other file operation is unsupported. It is
// the remote authority in these tests, so a same-named local file must never be consulted.
type denoFakeFiles struct {
	authority.UnsupportedFiles
	mu      sync.Mutex
	present map[string]hostos.FileStat
	err     error
}

func (f *denoFakeFiles) Stat(_ context.Context, p string) (hostos.FileStat, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return hostos.FileStat{}, f.err
	}
	if st, ok := f.present[p]; ok {
		return st, nil
	}
	return hostos.FileStat{}, &os.PathError{Op: "stat", Path: p, Err: fs.ErrNotExist}
}

// denoFakePort records the per-root providers the selector installs.
type denoFakePort struct {
	mu        sync.Mutex
	providers map[string]func(string) (manager.ServerConfig, bool)
	sets      int
	clears    int
}

func (p *denoFakePort) SetRootServerProvider(root string, provider func(string) (manager.ServerConfig, bool)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.providers == nil {
		p.providers = map[string]func(string) (manager.ServerConfig, bool){}
	}
	p.providers[root] = provider
	p.sets++
}

func (p *denoFakePort) ClearRootServerProvider(root string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.providers, root)
	p.clears++
}

func (p *denoFakePort) provider(root string) func(string) (manager.ServerConfig, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.providers[root]
}

func waitDenoProvider(t *testing.T, p *denoFakePort, root string, want bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if (p.provider(root) != nil) == want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("provider for %q present = %v, want %v (sets %d clears %d)", root, p.provider(root) != nil, want, p.sets, p.clears)
}

// newDenoSelectorSlot returns a fake authority slot with files and a one-shot script.
func newDenoSelectorSlot(files *denoFakeFiles, one authority.OneShot) authority.Slot {
	slot := authoritytest.New()
	slot.FileSystem = files
	slot.OneShotter = one
	return slot
}

// TestDenoAutoSelectorInstallsProvider checks the successful path: authority Stat confirms
// deno.json, the authority one-shot under the gate confirms deno, and the provider yields
// the non-auto-start deno lsp config for TS/JS only; a later explicit config makes the
// provider refuse, so a delayed decision cannot override the new state.
func TestDenoAutoSelectorInstallsProvider(t *testing.T) {
	root := "/remote/proj"
	files := &denoFakeFiles{present: map[string]hostos.FileStat{path.Join(root, "deno.json"): {Kind: "file"}}}
	var gateCalls int
	one := &fakeOneShot{script: func(_ context.Context, argv []string, o hostos.ProcOptions) (hostos.ProcResult, error) {
		if len(argv) != 3 || argv[0] != "sh" {
			return hostos.ProcResult{}, errors.New("unexpected probe argv")
		}
		if o.Cwd != root {
			t.Errorf("probe cwd = %q, want %q", o.Cwd, root)
		}
		return hostos.ProcResult{Code: 0}, nil
	}}
	slot := newDenoSelectorSlot(files, one)
	port := &denoFakePort{}
	cfg := config.Defaults()
	selector := newDenoAutoSelector(port, slot, root, true, func(context.Context, []string, string) error {
		gateCalls++
		return nil
	}, func() []string { return []string{"PATH=/remote/bin"} }, func() config.Config { return cfg })
	if selector == nil {
		t.Fatal("selector was not built")
	}
	selector.Start(context.Background())
	waitDenoProvider(t, port, root, true)
	if gateCalls != 1 {
		t.Fatalf("gate calls = %d, want 1", gateCalls)
	}
	provider := port.provider(root)
	got, ok := provider("typescript")
	if !ok || len(got.Command) != 1 || got.Command[0] != "deno" || len(got.Args) != 1 || got.Args[0] != "lsp" || got.AutoStart {
		t.Fatalf("deno config = %+v ok=%v, want deno lsp AutoStart=false", got, ok)
	}
	if string(got.InitializationOptions) != "{\"enable\":true}" {
		t.Fatalf("initializationOptions = %s", got.InitializationOptions)
	}
	if _, ok := provider("tsx"); ok {
		t.Fatal("tsx was auto-selected; only typescript/javascript may be")
	}
	// A config change after the probe: an explicit user entry must win over the cached
	// Deno decision when the provider is evaluated for a future manager.
	cfg.LSP = map[string][]config.LSPServer{"typescript": {{Command: "user-ts"}}}
	if _, ok := provider("typescript"); ok {
		t.Fatal("provider overrode an explicit user typescript config")
	}
	if got, ok := provider("javascript"); !ok || got.Command[0] != "deno" {
		t.Fatalf("javascript provider = %+v ok=%v, want deno still", got, ok)
	}
}

// TestDenoAutoSelectorRefusesMissingConfirmations checks every no-answer: missing config,
// failing deno command, a non-not-exist Stat error, a gate refusal and a cancelled probe
// install no provider (and clear one previously installed).
func TestDenoAutoSelectorRefusesMissingConfirmations(t *testing.T) {
	root := "/remote/proj"
	cases := map[string]struct {
		files *denoFakeFiles
		one   *fakeOneShot
		gate  authority.Gate
		ctx   func() context.Context
	}{
		"missing deno config": {
			files: &denoFakeFiles{},
			one: &fakeOneShot{script: func(context.Context, []string, hostos.ProcOptions) (hostos.ProcResult, error) {
				return hostos.ProcResult{Code: 0}, nil
			}},
		},
		"missing deno command": {
			files: &denoFakeFiles{present: map[string]hostos.FileStat{path.Join(root, "deno.json"): {Kind: "file"}}},
			one: &fakeOneShot{script: func(context.Context, []string, hostos.ProcOptions) (hostos.ProcResult, error) {
				return hostos.ProcResult{Code: 1}, nil
			}},
		},
		"stat error": {
			files: &denoFakeFiles{err: errors.New("connection down")},
			one: &fakeOneShot{script: func(context.Context, []string, hostos.ProcOptions) (hostos.ProcResult, error) {
				return hostos.ProcResult{Code: 0}, nil
			}},
		},
		"gate refusal": {
			files: &denoFakeFiles{present: map[string]hostos.FileStat{path.Join(root, "deno.json"): {Kind: "file"}}},
			one: &fakeOneShot{script: func(context.Context, []string, hostos.ProcOptions) (hostos.ProcResult, error) {
				return hostos.ProcResult{Code: 0}, nil
			}},
			gate: func(context.Context, []string, string) error { return errors.New("untrusted") },
		},
		"cancelled probe": {
			files: &denoFakeFiles{present: map[string]hostos.FileStat{path.Join(root, "deno.json"): {Kind: "file"}}},
			one: &fakeOneShot{script: func(ctx context.Context, _ []string, _ hostos.ProcOptions) (hostos.ProcResult, error) {
				<-ctx.Done()
				return hostos.ProcResult{}, ctx.Err()
			}},
			ctx: func() context.Context { c, cancel := context.WithCancel(context.Background()); cancel(); return c },
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			port := &denoFakePort{}
			ctx := context.Background()
			if tc.ctx != nil {
				ctx = tc.ctx()
			}
			selector := newDenoAutoSelector(port, newDenoSelectorSlot(tc.files, tc.one), root, true, tc.gate, nil, func() config.Config { return config.Defaults() })
			selector.Start(ctx)
			waitDenoProvider(t, port, root, false)
			time.Sleep(10 * time.Millisecond)
			if port.provider(root) != nil {
				t.Fatal("a failed confirmation installed a Deno provider")
			}
		})
	}
}

// TestDenoAutoSelectorDoesNotUseLocalConfigOrPath checks the no-local-fallback rule: the
// same-named local deno.json and a local deno command do not count for a remote selector.
func TestDenoAutoSelectorDoesNotUseLocalConfigOrPath(t *testing.T) {
	local := t.TempDir()
	if err := os.WriteFile(filepath.Join(local, "deno.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	port := &denoFakePort{}
	one := &fakeOneShot{script: func(context.Context, []string, hostos.ProcOptions) (hostos.ProcResult, error) {
		return hostos.ProcResult{Code: 0}, nil
	}}
	selector := newDenoAutoSelector(port, newDenoSelectorSlot(&denoFakeFiles{}, one), local, true, nil, nil, func() config.Config { return config.Defaults() })
	selector.Start(context.Background())
	waitDenoProvider(t, port, local, false)
	time.Sleep(10 * time.Millisecond)
	if port.provider(local) != nil {
		t.Fatal("a remote selector used the local deno.json/deno")
	}
}

// TestDenoAutoSelectorGenerationDropsOldAnswer checks reconnects: a second Start supersedes
// the first probe; the old answer (even if it succeeds later) must not replace the newer
// provider. The first probe blocks on its one-shot until released.
func TestDenoAutoSelectorGenerationDropsOldAnswer(t *testing.T) {
	root := "/remote/proj"
	files := &denoFakeFiles{present: map[string]hostos.FileStat{path.Join(root, "deno.json"): {Kind: "file"}}}
	started := make(chan struct{})
	release := make(chan struct{})
	var mu sync.Mutex
	calls := 0
	one := &fakeOneShot{script: func(_ context.Context, _ []string, _ hostos.ProcOptions) (hostos.ProcResult, error) {
		mu.Lock()
		calls++
		call := calls
		mu.Unlock()
		if call == 1 {
			close(started)
			<-release
			return hostos.ProcResult{Code: 0}, nil
		}
		return hostos.ProcResult{Code: 0}, nil
	}}
	port := &denoFakePort{}
	selector := newDenoAutoSelector(port, newDenoSelectorSlot(files, one), root, true, nil, nil, func() config.Config { return config.Defaults() })
	selector.Start(context.Background())
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("the first probe did not start")
	}
	selector.Start(context.Background())
	waitDenoProvider(t, port, root, true)
	close(release)
	time.Sleep(20 * time.Millisecond)
	if port.provider(root) == nil {
		t.Fatal("the late old answer removed the newer provider")
	}
}

// TestDenoEligibilityProtectsExplicitAndDisabledConfig checks the config gate table.
func TestDenoEligibilityProtectsExplicitAndDisabledConfig(t *testing.T) {
	defaults := config.Defaults()
	if got := denoEligibleLanguages(defaults); len(got) != 2 {
		t.Fatalf("default eligible = %v, want typescript/javascript", got)
	}
	off := defaults
	off.LSPEnabled = false
	if got := denoEligibleLanguages(off); len(got) != 0 {
		t.Fatalf("global-off eligible = %v", got)
	}
	disabled := defaults
	disabled.LSPDisabled = map[string]bool{"typescript": true}
	if got := denoEligibleLanguages(disabled); len(got) != 1 || got[0] != "javascript" {
		t.Fatalf("disabled eligible = %v", got)
	}
	explicit := defaults
	explicit.LSP = map[string][]config.LSPServer{"javascript": {{Command: "user-js"}}}
	if got := denoEligibleLanguages(explicit); len(got) != 1 || got[0] != "typescript" {
		t.Fatalf("explicit eligible = %v", got)
	}
	// tsx is a separate language: a tsx user entry does not disable typescript.
	tsxCfg := defaults
	tsxCfg.LSP = map[string][]config.LSPServer{"tsx": {{Command: "tsx-ls"}}}
	if got := denoEligibleLanguages(tsxCfg); len(got) != 2 {
		t.Fatalf("tsx entry changed TS/JS eligibility: %v", got)
	}
}

// TestDenoAutoSelectorRebindDropsLateOldAnswer checks the root-switch race: an old root's
// probe that answers after the switch must not install its provider over the new root.
func TestDenoAutoSelectorRebindDropsLateOldAnswer(t *testing.T) {
	rootA, rootB := "/remote/a", "/remote/b"
	files := &denoFakeFiles{present: map[string]hostos.FileStat{
		path.Join(rootA, "deno.json"): {Kind: "file"},
		path.Join(rootB, "deno.json"): {Kind: "file"},
	}}
	started := make(chan struct{})
	release := make(chan struct{})
	var mu sync.Mutex
	calls := 0
	one := &fakeOneShot{script: func(_ context.Context, _ []string, _ hostos.ProcOptions) (hostos.ProcResult, error) {
		mu.Lock()
		calls++
		call := calls
		mu.Unlock()
		if call == 1 {
			close(started)
			<-release
		}
		return hostos.ProcResult{Code: 0}, nil
	}}
	port := &denoFakePort{}
	selector := newDenoAutoSelector(port, newDenoSelectorSlot(files, one), rootA, true, nil, nil, func() config.Config { return config.Defaults() })
	ctx := context.Background()
	selector.Start(ctx)
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("the first root probe did not start")
	}
	selector.Rebind(ctx, rootB)
	waitDenoProvider(t, port, rootB, true)
	waitDenoProvider(t, port, rootA, false)
	close(release)
	time.Sleep(20 * time.Millisecond)
	if port.provider(rootB) == nil || port.provider(rootA) != nil {
		t.Fatal("the late old-root answer changed the new root state")
	}
}
