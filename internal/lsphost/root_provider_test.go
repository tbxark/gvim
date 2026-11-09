package lsphost

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tbxark/gvim/config"
	"github.com/tbxark/gvim/lsp"
	"github.com/tbxark/gvim/lsp/manager"
)

// TestRootServerProviderAppliesToExactRootOnly checks the LSP-port seam used by Deno
// auto-selection: the provider replaces a language config only for the exact root and the
// baseline returns when it is cleared; another root keeps the configured server.
func TestRootServerProviderAppliesToExactRootOnly(t *testing.T) {
	port, ok := NewFor(config.Defaults()).(*lspPort)
	if !ok {
		t.Fatal("NewFor did not return the lspPort")
	}
	root, other := "/remote/deno-proj", "/remote/other-proj"
	port.SetRootServerProvider(root, func(language string) (manager.ServerConfig, bool) {
		if language != "typescript" {
			return manager.ServerConfig{}, false
		}
		return manager.ServerConfig{Command: []string{"deno"}, Args: []string{"lsp"}, Name: "deno"}, true
	})
	wantDeno := func(t *testing.T, got []manager.ServerConfig) {
		t.Helper()
		if len(got) != 1 || got[0].Command[0] != "deno" || got[0].Name != "deno" {
			t.Fatalf("typescript configs = %+v, want the provider's deno config", got)
		}
	}
	wantDeno(t, port.serverConfigsFor(root)["typescript"])
	baseline := port.serverConfigsFor(other)["typescript"]
	if len(baseline) != 1 || baseline[0].Command[0] == "deno" {
		t.Fatalf("other root typescript configs = %+v, want the configured server", baseline)
	}
	port.ClearRootServerProvider(root)
	cleared := port.serverConfigsFor(root)["typescript"]
	if len(cleared) != 1 || cleared[0].Command[0] == "deno" {
		t.Fatalf("cleared root typescript configs = %+v, want the baseline server", cleared)
	}
}

// TestRootServerProviderOnlyAnswersExistingLanguages checks a provider cannot invent a
// language entry absent from the plan.
func TestRootServerProviderOnlyAnswersExistingLanguages(t *testing.T) {
	port := NewFor(config.Defaults()).(*lspPort)
	port.SetRootServerProvider("/w", func(string) (manager.ServerConfig, bool) {
		return manager.ServerConfig{Command: []string{"unexpected"}}, true
	})
	for lang := range port.serverConfigsFor("/w") {
		if lang == "unexpected" {
			t.Fatal("provider invented a language entry")
		}
	}
}

// TestRootServerProviderReachesManager checks the provider actually reaches the manager: an
// auto_start:false Deno override does not start on Open, and a manual StartLanguage launches
// the deno command (captured by a fake spawner).
func TestRootServerProviderReachesManager(t *testing.T) {
	port := NewFor(config.Defaults()).(*lspPort)
	root := t.TempDir()
	port.SetRoot(root)
	port.SetRootServerProvider(root, func(language string) (manager.ServerConfig, bool) {
		if language != "typescript" {
			return manager.ServerConfig{}, false
		}
		return manager.ServerConfig{
			Command:   []string{"deno"},
			Args:      []string{"lsp"},
			Name:      "deno",
			AutoStart: false,
		}, true
	})
	var launched []string
	port.SetSpawner(func(_ context.Context, cfg lsp.Config) (*lsp.Client, error) {
		launched = append(launched, strings.Join(cfg.Command, " "))
		return nil, errors.New("captured")
	})
	path := filepath.Join(root, "a.ts")
	// AutoStart false means Open records the document but reports no running server; that is
	// the intended no-silent-spawn behaviour, not a failure of the provider.
	_, _ = port.Open(path, "typescript", "const x = 1\n")
	if len(launched) != 0 {
		t.Fatalf("auto_start:false Deno started on Open: %v", launched)
	}
	_ = port.StartLanguage("typescript")
	if len(launched) != 1 || !strings.Contains(launched[0], "deno") {
		t.Fatalf("manual start launched %v, want deno lsp", launched)
	}
}

// TestRootChangeListener checks SetRoot notifies the app outside the port lock (the Deno
// selector rebinds from it) and does not deadlock.
func TestRootChangeListener(t *testing.T) {
	port := NewFor(config.Defaults()).(*lspPort)
	var got string
	port.SetRootChangeListener(func(root string) { got = root })
	port.SetRoot("/remote/next")
	if got != "/remote/next" {
		t.Fatalf("root listener got %q", got)
	}
}
