package trust

import (
	"path/filepath"
	"testing"
)

// TestRecordKeyedDecisions checks the explicit record-key API persists a decision in the
// same atomic store as local roots, keeps keys distinct and honors the startup policy for
// undecided records (no markers: trusted and recorded; markers: Restricted undecided).
func TestRecordKeyedDecisions(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "trust.json")
	store := NewStore(path)
	key := RecordKey("u@h:/srv/proj")
	if _, decided := store.LevelForRecord(key); decided {
		t.Fatal("a fresh store reported a decision")
	}
	if got, err := store.OpenRecord(key, nil); err != nil || got.Level != Trusted || !got.Decided {
		t.Fatalf("OpenRecord(markerless) = %+v, %v; want trusted+decided", got, err)
	}
	if level, decided := NewStore(path).LevelForRecord(key); !decided || level != Trusted {
		t.Fatalf("reopened store level = %v decided=%v, want trusted", level, decided)
	}
	if err := store.SetRecord(key, Restricted); err != nil {
		t.Fatalf("SetRecord: %v", err)
	}
	if got, err := store.OpenRecord(key, nil); err != nil || got.Level != Restricted || !got.Decided {
		t.Fatalf("recorded restriction = %+v, %v", got, err)
	}
	// A marked, undecided record stays Restricted and asks, without recording a decision.
	other := RecordKey("u@h:/srv/other")
	got, err := store.OpenRecord(other, []string{"go.mod"})
	if err != nil || got.Level != Restricted || got.Decided {
		t.Fatalf("marked undecided = %+v, %v; want Restricted+undecided", got, err)
	}
	if _, decided := store.LevelForRecord(other); decided {
		t.Fatal("an undecided marked workspace was recorded")
	}
	if err := store.SetRecord("", Trusted); err == nil {
		t.Fatal("SetRecord accepted an empty key")
	}
	if err := store.SetRecord(key, Level("nonsense")); err == nil {
		t.Fatal("SetRecord accepted an invalid level")
	}
	// Local path decisions still use their canonical-root keys, untouched by record keys.
	root := t.TempDir()
	if err := store.Set(root, Trusted); err != nil {
		t.Fatal(err)
	}
	if level, decided := NewStore(path).LevelFor(root); !decided || level != Trusted {
		t.Fatalf("local root decision = %v decided=%v", level, decided)
	}
}
