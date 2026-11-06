package session_test

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/tbxark/gvim/internal/session"
)

func TestSearchRoundTrip(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "session")
	if _, ok, err := session.LoadSearch(dir); ok || err != nil {
		t.Fatalf("LoadSearch on a missing file = ok %v, err %v", ok, err)
	}
	want := session.Search{
		Find:    []string{"foo", "a\nb"},
		Replace: []string{"bar"},
		Case:    true,
		Regex:   true,
	}
	if err := session.SaveSearch(dir, want); err != nil {
		t.Fatal(err)
	}
	got, ok, err := session.LoadSearch(dir)
	if err != nil || !ok || !reflect.DeepEqual(got, want) {
		t.Fatalf("LoadSearch = %+v ok %v err %v, want %+v", got, ok, err, want)
	}
}

func TestLoadSearchRejectsACorruptFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "search.json"), []byte("{nope"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := session.LoadSearch(dir); ok || err == nil {
		t.Fatalf("LoadSearch = ok %v, err %v, want an error", ok, err)
	}
}
