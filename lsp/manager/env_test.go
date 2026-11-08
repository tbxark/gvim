package manager

import (
	"slices"
	"testing"
)

// TestSpawnEnvListMergesTheProjectEnvironment: the active env overlay is added
// to a server's configured environment, and a configured variable of the same
// name wins.
func TestSpawnEnvListMergesTheProjectEnvironment(t *testing.T) {
	m := NewManager("", nil, nil)
	m.SetSpawnEnv(func() []string { return []string{"A=2", "B=3"} })
	got := m.spawnEnvList([]string{"A=1"})
	if !slices.Equal(got, []string{"A=1", "B=3"}) {
		t.Fatalf("spawn env = %v, want [A=1 B=3]", got)
	}
	m.SetSpawnEnv(nil)
	if got := m.spawnEnvList([]string{"A=1"}); !slices.Equal(got, []string{"A=1"}) {
		t.Fatalf("spawn env without provider = %v", got)
	}
}
