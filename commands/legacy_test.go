package commands

import (
	"encoding/json"
	"os"
	"slices"
	"strings"
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

// freshCommands is Commands as the legacy palette lists it: without the rows gvim adds
// for its plugin manager, and with the generated text of the rows that gvimRenames
// rewrites from init.ts to init.lua. The captured lists are Fresh's, so they stay as
// they are and the gvim additions are taken out of the table they are compared with.
func freshCommands() []Entry {
	orig := map[string]builtinCommand{}
	for _, c := range builtinCommands {
		orig[c.Action] = c
	}
	var out []Entry
	for _, e := range Commands(nil) {
		c, ok := orig[e.Action]
		if !ok {
			continue
		}
		e.Name, e.Description = c.Name, c.Description
		out = append(out, e)
	}
	return out
}

// TestSearchMatchesLegacyPalette checks the complete ranked list, not only the
// first rows, for every query captured from the legacy palette.
func TestSearchMatchesLegacyPalette(t *testing.T) {
	for query, want := range loadLegacyLists(t) {
		var got []string
		for _, e := range Search(freshCommands(), ">"+query) {
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

// TestResumeLiveGrepDescriptionMatchesCacheBehavior: the generated Fresh description
// promised cached results with no re-run unconditionally. The gvim override must say
// that the cache is used only while current and that invalidations search again.
func TestResumeLiveGrepDescriptionMatchesCacheBehavior(t *testing.T) {
	for _, e := range Commands(nil) {
		if e.Action != "resume_live_grep" {
			continue
		}
		if strings.Contains(e.Description, "no re-run") {
			t.Fatalf("description still overstates Resume: %q", e.Description)
		}
		for _, want := range []string{"cached matches", "files-only", "still current", "searches again", "Buffers/Terminals"} {
			if !strings.Contains(e.Description, want) {
				t.Fatalf("description = %q, want %q", e.Description, want)
			}
		}
		if e.DescKey != "gvim.command.resume_live_grep_desc" {
			t.Fatalf("description key = %q", e.DescKey)
		}
		return
	}
	t.Fatal("Resume Live Grep command not found")
}
