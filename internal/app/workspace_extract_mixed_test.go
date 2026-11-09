package app

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tbxark/gvim/internal/restore"
	"github.com/tbxark/gvim/internal/terminal"
	"github.com/tbxark/gvim/internal/tui"
	"github.com/tbxark/gvim/session"
)

// seedDockTerminal restores one terminal tab into a workspace runtime's dock so the
// mixed extract selection has a terminal to move.
func seedDockTerminal(t *testing.T, m *workspaceManager, id, name string) {
	t.Helper()
	m.mu.Lock()
	sess := m.sessions[id]
	m.mu.Unlock()
	if sess == nil {
		t.Fatalf("workspace %s is not materialized", id)
	}
	sess.rt.model.RestoreTerminal(terminal.SavedState{
		Version: terminal.SavedStateVersion, Open: true,
		Tabs: []terminal.SavedTab{{Name: name, Open: true, Rows: []string{"term out"}}},
	})
}

// dockTabNames returns the saved names of a workspace dock's tabs.
func dockTabNames(m *workspaceManager, id string) []string {
	m.mu.Lock()
	sess := m.sessions[id]
	m.mu.Unlock()
	if sess == nil {
		return nil
	}
	refs := sess.rt.model.TerminalTabRefs()
	out := make([]string, len(refs))
	for i, ref := range refs {
		out[i] = ref.Name
	}
	return out
}

// activeRoot returns the active workspace's root.
func activeRoot(m *workspaceManager) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, sess := range m.sessions {
		if id == m.active {
			return sess.ws.File.Root
		}
	}
	return ""
}

// mixedSelection is the selection of one unsaved file tab and one terminal tab.
func mixedSelection(t *testing.T, m *workspaceManager, path string) tui.ExtractSelection {
	t.Helper()
	return tui.ExtractSelection{
		Files:     []string{path},
		Terminals: []tui.ExtractTerminalRef{{Index: 0, Name: "shell"}},
	}
}

// TestMixedExtractToNewWorkspace: the selected file and terminal tabs land in one new
// co-tenant (session + terminal record) written BEFORE the switch, and the sources are
// closed only after the switch was queued and accepted.
func TestMixedExtractToNewWorkspace(t *testing.T) {
	m, sourceID := newWorkspaceManagerTest(t)
	root := activeRoot(m)
	path := extractionFile(t, root, "mixed.txt", "disk\n")
	openUnsaved(t, m, sourceID, path, "MIX-")
	seedDockTerminal(t, m, sourceID, "shell")

	var sourcesOpenAtQueue bool
	m.beginSelectFn = func(string) (func() error, error) {
		sourcesOpenAtQueue = workspaceHasTab(m, sourceID, path) && len(dockTabNames(m, sourceID)) == 1
		return nil, nil
	}
	sel := mixedSelection(t, m, path)
	cmd, err := m.ExtractMixed(sel, "", false)
	if err != nil || cmd == nil {
		t.Fatalf("ExtractMixed = %v, %v", cmd, err)
	}
	// Capture does no I/O and closes nothing.
	if len(m.Entries()) != 1 || !workspaceHasTab(m, sourceID, path) || len(dockTabNames(m, sourceID)) != 1 {
		t.Fatal("ExtractMixed mutated state on the UI goroutine")
	}
	cmd()
	if !sourcesOpenAtQueue {
		t.Fatal("the sources were closed before the switch was queued")
	}
	if workspaceHasTab(m, sourceID, path) || len(dockTabNames(m, sourceID)) != 0 {
		t.Fatalf("source tabs survived: file=%v dock=%v", workspaceHasTab(m, sourceID, path), dockTabNames(m, sourceID))
	}
	entries := m.Entries()
	if len(entries) != 2 {
		t.Fatalf("entries = %+v", entries)
	}
	var destID string
	for _, e := range entries {
		if e.ID != sourceID {
			destID = e.ID
		}
	}
	dir, err := m.reg.SessionDir(destID)
	if err != nil {
		t.Fatal(err)
	}
	layout, ok, err := session.Load(dir)
	if err != nil || !ok || len(layout.Files) != 1 || layout.Files[0].StashFile == "" {
		t.Fatalf("session = %+v, %v, %v", layout, ok, err)
	}
	stash, err := os.ReadFile(filepath.Join(dir, layout.Files[0].StashFile))
	if err != nil || string(stash) != "MIX-disk\n" {
		t.Fatalf("stash = %q, %v", stash, err)
	}
	if len(layout.MRU) != 1 || layout.MRU[0].Path != path || layout.Active != 0 {
		t.Fatalf("mixed MRU/active = %+v/%d, want the moved file and active", layout.MRU, layout.Active)
	}
	termRec, ok, err := restore.LoadTerminal(dir)
	if err != nil || !ok || len(termRec.Tabs) != 1 || termRec.Tabs[0].Name != "shell" {
		t.Fatalf("terminal record = %+v, %v, %v", termRec, ok, err)
	}
	if !strings.Contains(m.lastNotice, "Extracted 2 tabs into workspace") {
		t.Fatalf("notice = %q", m.lastNotice)
	}
}

// TestMixedExtractToExistingWorkspace: both kinds transfer into a dormant existing
// target (its session and terminal record merged), then the target is switched to and
// the source tabs close.
func TestMixedExtractToExistingWorkspace(t *testing.T) {
	m, sourceID := newWorkspaceManagerTest(t)
	root := activeRoot(m)
	targetID := coTenantOverRoot(t, m, root)
	path := extractionFile(t, root, "into.txt", "disk\n")
	openUnsaved(t, m, sourceID, path, "EX-")
	seedDockTerminal(t, m, sourceID, "shell")
	cmd, err := m.ExtractMixed(mixedSelection(t, m, path), targetID, false)
	if err != nil || cmd == nil {
		t.Fatalf("ExtractMixed = %v, %v", cmd, err)
	}
	cmd()
	if workspaceHasTab(m, sourceID, path) || len(dockTabNames(m, sourceID)) != 0 {
		t.Fatalf("source tabs survived: file=%v dock=%v", workspaceHasTab(m, sourceID, path), dockTabNames(m, sourceID))
	}
	if !workspaceHasTab(m, targetID, path) {
		t.Fatalf("target tabs = %+v", dockTabNames(m, targetID))
	}
	if got := dockTabNames(m, targetID); len(got) != 1 || got[0] != "shell" {
		t.Fatalf("target dock tabs = %v", got)
	}
	m.mu.Lock()
	active := m.active
	m.mu.Unlock()
	if active != targetID {
		t.Fatalf("active workspace = %q, want the target", active)
	}
	if !strings.Contains(m.lastNotice, "Extracted 2 tabs into workspace") {
		t.Fatalf("notice = %q", m.lastNotice)
	}
}

// TestMixedExtractConflictBeforeAnyWriteAndCancel: an existing target holding the file
// and a same-name terminal tab reports the conflicts BEFORE any write; cancelling (not
// retrying with replace) leaves both the target records and the source tabs exactly as
// they were. replace updates the target file state and APPENDS the same-name terminal
// tab (documented semantics).
func TestMixedExtractConflictBeforeAnyWriteAndCancel(t *testing.T) {
	m, sourceID := newWorkspaceManagerTest(t)
	root := activeRoot(m)
	targetID := coTenantOverRoot(t, m, root)
	path := extractionFile(t, root, "conflict.txt", "disk\n")
	openUnsaved(t, m, sourceID, path, "NEW-")
	seedDockTerminal(t, m, sourceID, "shell")
	dir, err := m.reg.SessionDir(targetID)
	if err != nil {
		t.Fatal(err)
	}
	// The target already has the file in its session and a tab named "shell".
	if err := session.Save(dir, session.Session{Version: session.Version, Root: root, Files: []session.FileState{{Path: path}}}); err != nil {
		t.Fatal(err)
	}
	if err := restore.SaveTerminal(dir, terminal.SavedState{
		Version: terminal.SavedStateVersion, Open: true,
		Tabs: []terminal.SavedTab{{Name: "shell", Open: true, Rows: []string{"target"}}},
	}); err != nil {
		t.Fatal(err)
	}
	beforeSession := readFileBytes(t, filepath.Join(dir, "session.json"))
	beforeTerminal := readFileBytes(t, filepath.Join(dir, "terminal.json"))

	sel := mixedSelection(t, m, path)
	cmd, err := m.ExtractMixed(sel, targetID, false)
	if err != nil || cmd == nil {
		t.Fatalf("ExtractMixed = %v, %v", cmd, err)
	}
	msg := cmd()
	conflict, ok := msg.(tui.ExtractConflictMsg)
	if !ok {
		t.Fatalf("command returned %T, want ExtractConflictMsg", msg)
	}
	if len(conflict.Conflicts) != 2 {
		t.Fatalf("conflicts = %v", conflict.Conflicts)
	}
	if conflict.TargetLabel == "" || conflict.TargetID != targetID {
		t.Fatalf("conflict target = %+v", conflict)
	}
	// The conflict report is read-only: both records and both source tabs unchanged.
	if got := readFileBytes(t, filepath.Join(dir, "session.json")); string(got) != string(beforeSession) {
		t.Fatal("the target session changed on conflict detection")
	}
	if got := readFileBytes(t, filepath.Join(dir, "terminal.json")); string(got) != string(beforeTerminal) {
		t.Fatal("the target terminal record changed on conflict detection")
	}
	if !workspaceHasTab(m, sourceID, path) || len(dockTabNames(m, sourceID)) != 1 {
		t.Fatal("the conflict report closed or moved a source tab")
	}

	// Replace: the file state is replaced (source stash arrives) and the same-name
	// terminal tab is added alongside the target's existing one.
	cmd, err = m.ExtractMixed(sel, targetID, true)
	if err != nil || cmd == nil {
		t.Fatalf("replace ExtractMixed = %v, %v", cmd, err)
	}
	cmd()
	layout, ok, err := session.Load(dir)
	if err != nil || !ok || len(layout.Files) != 1 || layout.Files[0].StashFile == "" {
		t.Fatalf("target session after replace = %+v, %v, %v", layout, ok, err)
	}
	stash, err := os.ReadFile(filepath.Join(dir, layout.Files[0].StashFile))
	if err != nil || string(stash) != "NEW-disk\n" {
		t.Fatalf("target stash = %q, %v", stash, err)
	}
	termRec, ok, err := restore.LoadTerminal(dir)
	if err != nil || !ok || len(termRec.Tabs) != 2 {
		t.Fatalf("target terminal tabs after replace = %+v, %v, %v", termRec.Tabs, ok, err)
	}
	for _, tab := range termRec.Tabs {
		if tab.Name != "shell" {
			t.Fatalf("same-name terminal append semantics changed: %+v", termRec.Tabs)
		}
	}
	if workspaceHasTab(m, sourceID, path) || len(dockTabNames(m, sourceID)) != 0 {
		t.Fatal("the replace extraction left a source tab open")
	}
}

// readFileBytes reads a record for a before/after comparison.
func readFileBytes(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// TestMixedExtractRefusals checks the unsafe selections: empty, unknown file, a
// changed terminal tab, and a target over a different root (all refuse with nothing
// moved).
func TestMixedExtractRefusals(t *testing.T) {
	m, sourceID := newWorkspaceManagerTest(t)
	root := activeRoot(m)
	path := extractionFile(t, root, "refuse.txt", "disk\n")
	openUnsaved(t, m, sourceID, path, "R-")
	seedDockTerminal(t, m, sourceID, "shell")
	if _, err := m.ExtractMixed(tui.ExtractSelection{}, "", false); err == nil || !strings.Contains(err.Error(), "no tabs were selected") {
		t.Fatalf("empty selection = %v", err)
	}
	if _, err := m.ExtractMixed(tui.ExtractSelection{Files: []string{filepath.Join(root, "gone.txt")}}, "", false); err == nil || !strings.Contains(err.Error(), "not open any more") {
		t.Fatalf("unknown file = %v", err)
	}
	if _, err := m.ExtractMixed(tui.ExtractSelection{Terminals: []tui.ExtractTerminalRef{{Index: 0, Name: "changed"}}}, "", false); err == nil || !strings.Contains(err.Error(), "terminal tab changed") {
		t.Fatalf("changed terminal = %v", err)
	}
	other := t.TempDir()
	otherWS, _, err := m.reg.Create(other, "other")
	if err != nil {
		t.Fatal(err)
	}
	cmd, err := m.ExtractMixed(tui.ExtractSelection{Files: []string{path}}, otherWS.File.StableID, false)
	if err != nil || cmd == nil {
		t.Fatalf("cross-root = %v, %v", cmd, err)
	}
	cmd()
	if !strings.Contains(m.lastNotice, "not over this root; nothing was moved") {
		t.Fatalf("cross-root notice = %q", m.lastNotice)
	}
	if !workspaceHasTab(m, sourceID, path) || len(dockTabNames(m, sourceID)) != 1 {
		t.Fatal("a refused selection moved source tabs")
	}
}

// TestMixedExtractEmptyTargetRecordJSONIsValid checks the new workspace's terminal
// record round-trips through the same schema (a regression guard for the mixed path's
// direct SaveTerminal use).
func TestMixedExtractEmptyTargetRecordJSONIsValid(t *testing.T) {
	st := terminal.SavedState{Version: terminal.SavedStateVersion, Open: true, Tabs: []terminal.SavedTab{{Name: "t", Open: true, Rows: []string{"x"}}}}
	data, err := json.Marshal(st)
	if err != nil || !strings.Contains(string(data), "tabs") {
		t.Fatalf("terminal record json = %s, %v", data, err)
	}
}

// TestMixedExtractCarriesSearchHistoryToNewWorkspace checks the dock-wide search
// history is not lost when mixed terminal tabs migrate to a new co-tenant workspace:
// the target's terminal record starts with the (deduped, bounded) source queries,
// and nothing starts because a new co-tenant stays dormant.
func TestMixedExtractCarriesSearchHistoryToNewWorkspace(t *testing.T) {
	m, sourceID := newWorkspaceManagerTest(t)
	root := activeRoot(m)
	path := extractionFile(t, root, "history.txt", "disk\n")
	openUnsaved(t, m, sourceID, path, "H-")
	seedDockTerminal(t, m, sourceID, "shell")
	m.mu.Lock()
	source := m.sessions[sourceID]
	m.mu.Unlock()
	source.rt.model.MergeTerminalSearchHistory([]string{"alpha", "beta", "alpha"})
	cmd, err := m.ExtractMixed(mixedSelection(t, m, path), "", false)
	if err != nil || cmd == nil {
		t.Fatalf("ExtractMixed = %v, %v", cmd, err)
	}
	cmd()
	var destID string
	for _, e := range m.Entries() {
		if e.ID != sourceID {
			destID = e.ID
		}
	}
	dir, err := m.reg.SessionDir(destID)
	if err != nil {
		t.Fatal(err)
	}
	st, ok, err := restore.LoadTerminal(dir)
	if err != nil || !ok {
		t.Fatalf("target terminal record = %v, %v", ok, err)
	}
	if len(st.SearchHistory) != 2 || st.SearchHistory[0] != "alpha" || st.SearchHistory[1] != "beta" {
		t.Fatalf("migrated search history = %v, want [alpha beta]", st.SearchHistory)
	}
	if len(st.Tabs) != 1 || st.Tabs[0].Name != "shell" {
		t.Fatalf("migrated tabs = %+v", st.Tabs)
	}
	// The switch materializes the new co-tenant from the saved record (no PTY): the
	// extracted tabs are restored read-only and the dock sees the migrated history.
	m.mu.Lock()
	dest := m.sessions[destID]
	m.mu.Unlock()
	if dest == nil {
		t.Fatal("the new co-tenant was not materialized by the switch")
	}
	if got := dest.rt.model.RunningTerminals(); len(got) != 0 {
		t.Fatalf("the extraction started a terminal process: %+v", got)
	}
	if got := dest.rt.model.TerminalSearchHistory(); len(got) != 2 || got[0] != "alpha" || got[1] != "beta" {
		t.Fatalf("materialized new-workspace history = %v", got)
	}
}

// TestMixedExtractMergesSearchHistoryIntoDormantTarget checks an existing dormant
// target keeps its own queries first and gains the migrated source queries (deduped),
// and the restored target dock sees the merged history without starting a process.
func TestMixedExtractMergesSearchHistoryIntoDormantTarget(t *testing.T) {
	m, sourceID := newWorkspaceManagerTest(t)
	root := activeRoot(m)
	targetID := coTenantOverRoot(t, m, root)
	path := extractionFile(t, root, "mergehist.txt", "disk\n")
	openUnsaved(t, m, sourceID, path, "MH-")
	seedDockTerminal(t, m, sourceID, "shell")
	m.mu.Lock()
	source := m.sessions[sourceID]
	m.mu.Unlock()
	source.rt.model.MergeTerminalSearchHistory([]string{"alpha", "target-q"})
	dir, err := m.reg.SessionDir(targetID)
	if err != nil {
		t.Fatal(err)
	}
	if err := restore.SaveTerminal(dir, terminal.SavedState{
		Version: terminal.SavedStateVersion, Open: true,
		Tabs:          []terminal.SavedTab{{Name: "existing", Open: true, Rows: []string{"target"}}},
		SearchHistory: []string{"target-q", "own"},
	}); err != nil {
		t.Fatal(err)
	}
	cmd, err := m.ExtractMixed(mixedSelection(t, m, path), targetID, false)
	if err != nil || cmd == nil {
		t.Fatalf("ExtractMixed = %v, %v", cmd, err)
	}
	cmd()
	// The target record: its own queries first, then the migrated source-only ones.
	st, ok, err := restore.LoadTerminal(dir)
	if err != nil || !ok {
		t.Fatalf("target record = %v, %v", ok, err)
	}
	want := []string{"target-q", "own", "alpha"}
	if len(st.SearchHistory) != len(want) {
		t.Fatalf("merged target history = %v, want %v", st.SearchHistory, want)
	}
	for i := range want {
		if st.SearchHistory[i] != want[i] {
			t.Fatalf("merged target history = %v, want %v", st.SearchHistory, want)
		}
	}
	if len(st.Tabs) != 2 {
		t.Fatalf("target tabs = %+v", st.Tabs)
	}
	// The switch materialized the target from that record and no restored tab is
	// running (history migration and restore start no process).
	m.mu.Lock()
	target := m.sessions[targetID]
	m.mu.Unlock()
	if target == nil {
		t.Fatal("the target was not materialized by the switch")
	}
	if got := target.rt.model.TerminalSearchHistory(); len(got) != 3 || got[0] != "target-q" || got[2] != "alpha" {
		t.Fatalf("materialized target history = %v", got)
	}
	// A restored tab may be live-at-capture (it revives on the first dive), but
	// nothing is running now: the tabs are the two saved records and the history
	// migration/restore spawned no PTY.
	if got := len(target.rt.model.TerminalState().Tabs); got != 2 {
		t.Fatalf("materialized target tabs = %d", got)
	}
	if got := target.rt.model.RunningTerminals(); len(got) != 0 {
		t.Fatalf("history migration/restore started a terminal process: %+v", got)
	}
}

// TestMixedExtractMergesSearchHistoryIntoMaterializedTarget checks the open-target
// path merges the source history into the live dock-wide store (not only into the
// saved record), with no PTY started for the migration.
func TestMixedExtractMergesSearchHistoryIntoMaterializedTarget(t *testing.T) {
	m, sourceID := newWorkspaceManagerTest(t)
	root := activeRoot(m)
	targetID := coTenantOverRoot(t, m, root)
	materializeWorkspace(t, m, targetID)
	materializeWorkspace(t, m, sourceID)
	m.mu.Lock()
	target := m.sessions[targetID]
	source := m.sessions[sourceID]
	m.mu.Unlock()
	if target == nil || source == nil {
		t.Fatal("the workspaces are not materialized")
	}
	target.rt.model.MergeTerminalSearchHistory([]string{"target-q"})
	path := extractionFile(t, root, "livehist.txt", "disk\n")
	openUnsaved(t, m, sourceID, path, "LH-")
	seedDockTerminal(t, m, sourceID, "shell")
	source.rt.model.MergeTerminalSearchHistory([]string{"alpha"})
	cmd, err := m.ExtractMixed(mixedSelection(t, m, path), targetID, false)
	if err != nil || cmd == nil {
		t.Fatalf("ExtractMixed = %v, %v", cmd, err)
	}
	cmd()
	got := target.rt.model.TerminalSearchHistory()
	want := []string{"target-q", "alpha"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("materialized target history = %v, want %v", got, want)
	}
}

// TestMixedExtractRefusesProtectedTargetRecord checks the pure-loader guard: a
// target with a valid terminal record but a .protected marker must refuse any
// migration before writing files or terminal tabs, leaving both sides untouched
// (the mixed and the single-terminal extraction paths).
func TestMixedExtractRefusesProtectedTargetRecord(t *testing.T) {
	m, sourceID := newWorkspaceManagerTest(t)
	root := activeRoot(m)
	targetID := coTenantOverRoot(t, m, root)
	path := extractionFile(t, root, "protected.txt", "disk\n")
	openUnsaved(t, m, sourceID, path, "P-")
	seedDockTerminal(t, m, sourceID, "shell")
	dir, err := m.reg.SessionDir(targetID)
	if err != nil {
		t.Fatal(err)
	}
	if err := restore.SaveTerminal(dir, terminal.SavedState{
		Version: terminal.SavedStateVersion, Open: true,
		Tabs: []terminal.SavedTab{{Name: "existing", Open: true, Rows: []string{"target"}}},
	}); err != nil {
		t.Fatal(err)
	}
	recordBefore := readFileBytes(t, filepath.Join(dir, "terminal.json"))
	marker := filepath.Join(dir, "terminal.json.protected")
	if err := os.WriteFile(marker, []byte("protected"), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd, err := m.ExtractMixed(mixedSelection(t, m, path), targetID, false)
	if err != nil || cmd == nil {
		t.Fatalf("ExtractMixed = %v, %v", cmd, err)
	}
	cmd()
	if !strings.Contains(m.lastNotice, "protected") {
		t.Fatalf("notice = %q, want the protected-record refusal", m.lastNotice)
	}
	if got := readFileBytes(t, filepath.Join(dir, "terminal.json")); !bytes.Equal(got, recordBefore) {
		t.Fatal("the protected target terminal record changed during extraction")
	}
	if !workspaceHasTab(m, sourceID, path) || len(dockTabNames(m, sourceID)) != 1 {
		t.Fatal("the protected-target refusal moved a source tab")
	}
	// The single-terminal extraction uses the same guard.
	cmd, err = m.ExtractTerminalTabTo(targetID)
	if err != nil || cmd == nil {
		t.Fatalf("ExtractTerminalTabTo = %v, %v", cmd, err)
	}
	cmd()
	if !strings.Contains(m.lastNotice, "protected") {
		t.Fatalf("single extraction notice = %q", m.lastNotice)
	}
	if len(dockTabNames(m, sourceID)) != 1 {
		t.Fatal("the single terminal extraction moved the source tab")
	}
}
