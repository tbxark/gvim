package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tbxark/gvim/config"
	"github.com/tbxark/gvim/internal/tui"
	"github.com/tbxark/gvim/trust"
)

// useTrustStore points the app's trust store at a fresh temporary file and
// returns the store. The tests restore the constructor when they finish; none of
// them is parallel, because the constructors are package variables.
func useTrustStore(t *testing.T) *trust.Store {
	t.Helper()
	store := trust.NewStore(filepath.Join(t.TempDir(), "trust.json"))
	prev := newTrustStore
	newTrustStore = func() (*trust.Store, error) { return store, nil }
	t.Cleanup(func() { newTrustStore = prev })
	return store
}

func TestTrustRevokeSupersedesPendingGrant(t *testing.T) {
	root := t.TempDir()
	c := newTrustController(trust.NewStore(filepath.Join(t.TempDir(), "trust.json")), trust.State{Root: root, Level: trust.Restricted})
	started, release := make(chan struct{}), make(chan struct{})
	c.prepareTrust = func(trust.State) error { close(started); <-release; return nil }
	done := make(chan error, 1)
	go func() { _, err := c.Decide(trust.Trusted); done <- err }()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("trust preparation did not start")
	}
	if _, err := c.Decide(trust.Blocked); err != nil {
		t.Fatal(err)
	}
	close(release)
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("superseded grant succeeded")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("grant did not finish")
	}
	if got := c.State().Level; got != trust.Blocked {
		t.Fatalf("trust level = %s, want blocked", got)
	}
}

// TestOpenTrustAppliesTheStartupPolicy checks the policy the engine and the app
// share: a recorded decision is honored, a plain folder is trusted silently and
// recorded, and a folder that can execute code stays undecided and Restricted.
func TestOpenTrustAppliesTheStartupPolicy(t *testing.T) {
	isolateConfig(t)
	plain := t.TempDir()
	writeTestFile(t, filepath.Join(plain, "notes.txt"), "hello\n")
	executable := t.TempDir()
	writeTestFile(t, filepath.Join(executable, "Cargo.toml"), "[package]\n")
	decided := t.TempDir()
	writeTestFile(t, filepath.Join(decided, "Cargo.toml"), "[package]\n")
	store := useTrustStore(t)
	if err := store.Set(decided, trust.Blocked); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name        string
		root        string
		wantLevel   trust.Level
		wantDecided bool
		wantPrompt  bool
	}{
		{"a plain folder is trusted silently", plain, trust.Trusted, true, false},
		{"markers keep it restricted and undecided", executable, trust.Restricted, false, true},
		{"a recorded decision is honored", decided, trust.Blocked, true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotStore, state, warning := openTrust(tt.root)
			if gotStore == nil {
				t.Fatal("openTrust returned no store")
			}
			if warning != "" {
				t.Errorf("warning = %q, want none", warning)
			}
			if state.Level != tt.wantLevel || state.Decided != tt.wantDecided {
				t.Fatalf("state = %+v, want level %q decided %v", state, string(tt.wantLevel), tt.wantDecided)
			}
			if state.NeedsPrompt() != tt.wantPrompt {
				t.Errorf("NeedsPrompt() = %v, want %v", state.NeedsPrompt(), tt.wantPrompt)
			}
		})
	}
	// The silent decision of the plain folder was persisted.
	if level, decided := trust.NewStore(store.Path()).LevelFor(plain); !decided || level != trust.Trusted {
		t.Fatalf("plain folder decision = (%q, %v), want (trusted, true)", string(level), decided)
	}
}

// TestOpenTrustWithoutAUserConfigDirLeavesTrustUnavailable checks the fallback:
// a session that cannot build the store has no port and no restriction, and the
// status row is told once.
func TestOpenTrustWithoutAUserConfigDirLeavesTrustUnavailable(t *testing.T) {
	prev := newTrustStore
	newTrustStore = func() (*trust.Store, error) { return nil, errors.New("no home directory") }
	t.Cleanup(func() { newTrustStore = prev })

	store, state, warning := openTrust(t.TempDir())
	if store != nil {
		t.Error("openTrust returned a store")
	}
	// With no way to record a decision, nothing is gated: the state is trusted
	// for every restriction, and the nil port leaves the TUI without a prompt.
	if !state.AllowsProjectConfig() || !state.AllowsTooling() || state.NeedsPrompt() {
		t.Errorf("state = %+v, want a trusted state that asks nothing", state)
	}
	if !strings.Contains(warning, "no home directory") {
		t.Errorf("warning = %q, want the store error", warning)
	}
}

// TestBuildRuntimeGatesTheProjectConfigOnTrust checks the app wiring: the
// project's .gvim/config.json applies only to a Trusted workspace, and the
// model gets the trust port with the level that was resolved.
func TestBuildRuntimeGatesTheProjectConfigOnTrust(t *testing.T) {
	tests := []struct {
		name        string
		level       trust.Level
		wantTabSize int
	}{
		{"restricted skips the project layer", trust.Restricted, 2},
		{"trusted applies the project layer", trust.Trusted, 8},
		{"blocked skips the project layer", trust.Blocked, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := projectRoot(t, `{"tab_size": 8}`)
			// The user layer applies at every level, so the skip is visible: it
			// sets 2 and the project sets 8 over it.
			opts.Config = filepath.Join(t.TempDir(), "config.json")
			writeTestFile(t, opts.Config, `{"tab_size": 2}`)
			// A marker makes the folder executable content, so an undecided
			// workspace would ask; the decision is recorded explicitly here.
			writeTestFile(t, filepath.Join(opts.Root, "Cargo.toml"), "[package]\n")
			store := useTrustStore(t)
			if err := store.Set(opts.Root, tt.level); err != nil {
				t.Fatal(err)
			}
			got, err := buildCaptured(t, opts)
			if err != nil {
				t.Fatalf("buildModel error = %v", err)
			}
			if got.editor.TabSize != tt.wantTabSize {
				t.Errorf("TabSize = %d, want %d", got.editor.TabSize, tt.wantTabSize)
			}
			if got.deps.Trust == nil {
				t.Fatal("Deps.Trust is nil for an available store")
			}
			if state := got.deps.Trust.State(); state.Level != tt.level || !state.Decided {
				t.Errorf("trust state = %+v, want level %q decided", state, string(tt.level))
			}
		})
	}
}

// TestBuildRuntimeGatesLanguageServerAutoStartOnTrust checks Fresh's
// language-server rule: only a Trusted workspace starts a server on its own.
// The user's config file is untouched, and the rest of the user layer applies.
func TestBuildRuntimeGatesLanguageServerAutoStartOnTrust(t *testing.T) {
	tests := []struct {
		name          string
		level         trust.Level
		wantAutoStart bool
	}{
		{"untrusted workspace starts no server", trust.Restricted, false},
		{"trusted workspace starts its servers", trust.Trusted, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := projectRoot(t, `{"tab_size": 8}`)
			opts.Config = filepath.Join(t.TempDir(), "config.json")
			writeTestFile(t, opts.Config, `{"tab_size": 2, "lsp_enabled": true}`)
			writeTestFile(t, filepath.Join(opts.Root, "go.mod"), "module x\n")
			store := useTrustStore(t)
			if err := store.Set(opts.Root, tt.level); err != nil {
				t.Fatal(err)
			}
			var got config.Config
			prev := newLSP
			newLSP = func(cfg config.Config) tui.LSPSession {
				got = cfg
				return prev(cfg)
			}
			t.Cleanup(func() { newLSP = prev })
			if _, err := buildCaptured(t, opts); err != nil {
				t.Fatalf("buildModel error = %v", err)
			}
			if got.LSPEnabled != tt.wantAutoStart {
				t.Errorf("LSPEnabled = %v, want %v", got.LSPEnabled, tt.wantAutoStart)
			}
			// The rest of the user layer still applies, and the project layer
			// only when trusted.
			wantTabSize := 2
			if tt.wantAutoStart {
				wantTabSize = 8
			}
			if got.TabSize != wantTabSize {
				t.Errorf("TabSize = %d, want %d", got.TabSize, wantTabSize)
			}
		})
	}
}

// TestLSPConfigGatesAutoStartOnTrust pins the small copy the port is built from.
func TestLSPConfigGatesAutoStartOnTrust(t *testing.T) {
	base := config.Defaults()
	base.LSPEnabled = true
	base.TabSize = 7
	tests := []struct {
		name  string
		state trust.State
		want  bool
	}{
		{"trusted", trust.State{Level: trust.Trusted, Decided: true}, true},
		{"restricted", trust.State{Level: trust.Restricted, Decided: true}, false},
		{"blocked", trust.State{Level: trust.Blocked, Decided: true}, false},
		{"undecided", trust.State{Level: trust.Restricted, Markers: []string{"go.mod"}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := lspConfig(base, tt.state)
			if got.LSPEnabled != tt.want {
				t.Errorf("LSPEnabled = %v, want %v", got.LSPEnabled, tt.want)
			}
			if got.TabSize != 7 {
				t.Errorf("TabSize = %d, want the rest of the config unchanged", got.TabSize)
			}
		})
	}
}

// TestBuildRuntimeWritesTheSilentDecisionToTheStore checks the app's startup
// policy end to end: a plain folder is trusted and the decision is recorded, so
// the next start does not ask.
func TestBuildRuntimeWritesTheSilentDecisionToTheStore(t *testing.T) {
	store := useTrustStore(t)
	opts := productRootPlain(t)
	got, err := buildCaptured(t, opts)
	if err != nil {
		t.Fatalf("buildModel error = %v", err)
	}
	if got.deps.Trust == nil {
		t.Fatal("Deps.Trust is nil")
	}
	state := got.deps.Trust.State()
	if state.Level != trust.Trusted || !state.Decided {
		t.Fatalf("trust state = %+v, want trusted and decided", state)
	}
	if level, decided := trust.NewStore(store.Path()).LevelFor(opts.Root); !decided || level != trust.Trusted {
		t.Fatalf("stored decision = (%q, %v), want (trusted, true)", string(level), decided)
	}
}

// productRootPlain returns options that open a plain temporary folder with one
// file and no executable content.
func productRootPlain(t *testing.T) Options {
	t.Helper()
	root := t.TempDir()
	file := filepath.Join(root, "notes.txt")
	writeTestFile(t, file, "hello\n")
	return Options{Root: root, Files: []FileTarget{{Path: file}}}
}

// TestTrustControllerRecordsDecisions checks the TUI port over a real store: a
// decision is written through, and a workspace switch adopts the new root's
// recorded level.
func TestTrustControllerRecordsDecisions(t *testing.T) {
	store := trust.NewStore(filepath.Join(t.TempDir(), "trust.json"))
	root := t.TempDir()
	c := newTrustController(store, trust.State{Root: root, Level: trust.Restricted, Decided: true})

	got, err := c.Decide(trust.Trusted)
	if err != nil {
		t.Fatalf("Decide error = %v", err)
	}
	if got.Level != trust.Trusted || !got.Decided {
		t.Fatalf("state = %+v, want trusted and decided", got)
	}
	if state := c.State(); state.Level != trust.Trusted {
		t.Errorf("State() = %+v, want the decided level", state)
	}
	if level, decided := trust.NewStore(store.Path()).LevelFor(root); !decided || level != trust.Trusted {
		t.Fatalf("stored decision = (%q, %v), want (trusted, true)", string(level), decided)
	}

	other := t.TempDir()
	if err := store.Set(other, trust.Blocked); err != nil {
		t.Fatal(err)
	}
	canonical, err := trust.CanonicalRoot(other)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := c.Load(other)
	if err != nil {
		t.Fatalf("Load error = %v", err)
	}
	if loaded.Root != canonical || loaded.Level != trust.Blocked || !loaded.Decided {
		t.Fatalf("loaded = %+v, want root %q blocked", loaded, canonical)
	}
	if state := c.State(); state.Root != canonical {
		t.Errorf("State() root = %q, want %q", state.Root, canonical)
	}
}

// TestTrustControllerKeepsTheDecisionWhenTheWriteFails checks Fresh's
// set_level behavior: the session level changes and the error says the decision
// will not be remembered.
func TestTrustControllerKeepsTheDecisionWhenTheWriteFails(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "config")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	store := trust.NewStore(filepath.Join(blocker, "trust.json"))
	root := t.TempDir()
	c := newTrustController(store, trust.State{Root: root, Level: trust.Restricted, Decided: true})

	got, err := c.Decide(trust.Blocked)
	if err == nil {
		t.Fatal("Decide returned no error for an unwritable store")
	}
	if got.Level != trust.Blocked || !got.Decided {
		t.Fatalf("state = %+v, want blocked despite the failed write", got)
	}
	if _, err := c.Decide(trust.Undecided); err == nil {
		t.Fatal("Decide accepted an invalid level")
	}
}
