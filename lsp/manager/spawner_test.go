package manager

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/tbxark/gvim/lsp"
)

// TestSetSpawnerStartsThroughIt checks the injected spawner is used for a server start and
// that the workspace spawn gate still runs before it: a denied spawn never reaches the
// spawner.
func TestSetSpawnerStartsThroughIt(t *testing.T) {
	m := NewManager("", map[string][]ServerConfig{
		"go": {{Command: []string{"fake-lsp"}}},
	}, nil)
	var calls int
	var got lsp.Config
	m.SetSpawner(func(_ context.Context, cfg lsp.Config) (*lsp.Client, error) {
		calls++
		got = cfg
		return nil, errors.New("spawner says no")
	})
	err := m.StartLanguage("go")
	if calls != 1 {
		t.Fatalf("spawner calls = %d, want 1", calls)
	}
	if err == nil || !strings.Contains(err.Error(), "spawner says no") {
		t.Fatalf("StartLanguage = %v, want the spawner error", err)
	}
	if len(got.Command) == 0 || got.Command[0] != "fake-lsp" {
		t.Fatalf("spawner config Command = %v", got.Command)
	}

	// The gate decides first: a denial must not reach the spawner.
	m.SetGate(func(context.Context, []string, string, string) error { return errors.New("denied by trust") })
	calls = 0
	err = m.StartLanguage("go")
	if err == nil || !strings.Contains(err.Error(), "denied by trust") {
		t.Fatalf("StartLanguage with a denied gate = %v", err)
	}
	if calls != 0 {
		t.Fatalf("a denied gate reached the spawner (%d calls)", calls)
	}
}
