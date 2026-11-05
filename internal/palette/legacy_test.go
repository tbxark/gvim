package palette

import (
	"encoding/json"
	"os"
	"slices"
	"testing"
)

// legacyLists holds full palette listings captured from the legacy Fresh
// binary, in the order its palette displays them. See testdata/legacy_lists.json.
type legacyLists struct {
	Queries map[string][]string `json:"queries"`
}

func loadLegacyLists(t *testing.T) map[string][]string {
	t.Helper()
	data, err := os.ReadFile("testdata/legacy_lists.json")
	if err != nil {
		t.Fatal(err)
	}
	var l legacyLists
	if err := json.Unmarshal(data, &l); err != nil {
		t.Fatal(err)
	}
	return l.Queries
}

// TestSearchMatchesLegacyPalette checks the complete ranked list, not only the
// first rows, for every query captured from the legacy palette.
func TestSearchMatchesLegacyPalette(t *testing.T) {
	for query, want := range loadLegacyLists(t) {
		var got []string
		for _, e := range Search(Commands(nil), ">"+query) {
			got = append(got, e.Name)
		}
		if !slices.Equal(got, want) {
			t.Errorf("query %q: got %d entries, want %d\n got: %q\nwant: %q",
				query, len(got), len(want), got, want)
		}
	}
}

// TestSaveQueryDisabledStates checks the parity scenario S21: with a clean
// buffer, Save All and Save File are listed last and disabled.
func TestSaveQueryDisabledStates(t *testing.T) {
	got := Search(Commands(nil), ">save")
	disabled := map[string]bool{}
	for _, e := range got {
		if e.Disabled {
			disabled[e.Name] = true
		}
	}
	for _, name := range []string{"Save All", "Save File", "Start/Restart LSP Server"} {
		if !disabled[name] {
			t.Errorf("%q should be disabled in a clean buffer", name)
		}
	}
	if got[0].Name != "Save File As" || got[0].Disabled {
		t.Errorf("first save match = %+v, want enabled Save File As", got[0])
	}
}

func TestSaveEnabledWhenModified(t *testing.T) {
	st := DefaultState()
	st.Modified = true
	st.AnyModified = true
	var saveDisabled bool
	for _, e := range SearchIn(Commands(nil), ">save all", st) {
		if e.Name == "Save All" {
			saveDisabled = e.Disabled
		}
	}
	if saveDisabled {
		t.Error("Save All should be enabled when a buffer is modified")
	}
}

func TestHiddenCommandsAreNotListed(t *testing.T) {
	for _, e := range Search(Commands(nil), ">detach") {
		if e.Name == "Detach" {
			t.Error("Detach is gated on SESSION_MODE and must be hidden")
		}
	}
}
