package app

import "testing"

// TestWorkspaceModelsGetUniqueScopedOwners: every built workspace model gets its stable
// workspace id and a fresh generation, and switching back to a warm model keeps that
// generation. Palette messages from an old generation can therefore never be mistaken
// for the re-created or newly shown model's work.
func TestWorkspaceModelsGetUniqueScopedOwners(t *testing.T) {
	m, firstID := newWorkspaceManagerTest(t)
	foreground := func() (owner, modelID string, generation uint64) {
		m.mu.Lock()
		defer m.mu.Unlock()
		o := m.sessions[firstID].rt.model.WorkspaceOwner()
		return o.Workspace, firstID, o.Generation
	}
	wsA, idA, genA := foreground()
	if wsA != idA || genA == 0 {
		t.Fatalf("foreground owner = %q generation %d, want workspace %q and nonzero", wsA, genA, idA)
	}

	dormant := createDormant(t, m)
	materializeWorkspace(t, m, dormant.File.StableID)
	m.mu.Lock()
	ownerB := m.sessions[dormant.File.StableID].rt.model.WorkspaceOwner()
	m.mu.Unlock()
	if ownerB.Workspace != dormant.File.StableID || ownerB.Generation == 0 {
		t.Fatalf("second owner = %+v, want workspace %q and nonzero", ownerB, dormant.File.StableID)
	}
	if ownerB.Generation == genA {
		t.Fatalf("two workspace models share generation %d", genA)
	}

	// Switching back reuses the warm model, so its generation must be unchanged.
	materializeWorkspace(t, m, firstID)
	m.mu.Lock()
	ownerA2 := m.sessions[firstID].rt.model.WorkspaceOwner()
	m.mu.Unlock()
	if ownerA2.Workspace != idA || ownerA2.Generation != genA {
		t.Fatalf("warm model owner = %+v, want workspace %q generation %d", ownerA2, idA, genA)
	}
}
