package session_test

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/session"
)

func sampleOverlay() session.OverlayStash {
	return session.OverlayStash{
		Path:          "/data/big.log",
		Size:          123456789012,
		MTimeUnixNano: 1_700_000_000_123_456_789,
		Skip:          3,
		EOL:           "\r\n",
		Overlay: buffer.OverlayState{
			Pieces: []buffer.OverlayPiece{
				{Lines: []string{"héllo", "日本語", ""}, EOL: "r-n"},
				{Run: true, First: 0, Count: 7},
				{Run: true, First: 9, Count: 1},
				{Lines: []string{"tail"}},
			},
			TailFirst: 12,
		},
	}
}

func TestOverlayStashRoundTrip(t *testing.T) {
	dir := t.TempDir()
	want := sampleOverlay()
	name, err := session.StashOverlay(dir, want)
	if err != nil {
		t.Fatal(err)
	}
	assertExists(t, filepath.Join(dir, name))
	got, err := session.ReadOverlay(dir, name)
	if err != nil {
		t.Fatal(err)
	}
	want.Version = session.Version
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ReadOverlay() = %+v\nwant %+v", got, want)
	}
	// The edited text can be read from the file by hand.
	raw, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "日本語") {
		t.Errorf("the stash file does not hold the edited text in readable form:\n%s", raw)
	}
}

func TestOverlayStashIsNotAnInterruptedTextStash(t *testing.T) {
	dir := t.TempDir()
	name, err := session.StashOverlay(dir, sampleOverlay())
	if err != nil {
		t.Fatal(err)
	}
	// An overlay is not text: the text reader refuses it.
	if _, err := session.ReadStash(dir, name); err == nil {
		t.Errorf("ReadStash(%q) read an overlay stash as text", name)
	}
	text, err := session.StashContent(dir, "text")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := session.ReadOverlay(dir, text); err == nil {
		t.Errorf("ReadOverlay(%q) read a text stash as an overlay", text)
	}
}

func TestReadOverlayRejectsForeignNamesAndBadFiles(t *testing.T) {
	parent := t.TempDir()
	dir := filepath.Join(parent, "session")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(parent, "overlay-x.json"), "{}")
	writeFile(t, filepath.Join(dir, "overlay-garbage.json"), "not json")
	writeFile(t, filepath.Join(dir, "overlay-future.json"), `{"version":99}`)
	writeFile(t, filepath.Join(dir, "other.json"), `{"version":1}`)
	tests := []struct{ name, file string }{
		{"empty", ""},
		{"session file", "session.json"},
		{"unprefixed", "other.json"},
		{"traversal", "../overlay-x.json"},
		{"nested", "sub/overlay-1.json"},
		{"not json", "overlay-garbage.json"},
		{"unknown version", "overlay-future.json"},
		{"missing", "overlay-missing.json"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := session.ReadOverlay(dir, tt.file); err == nil {
				t.Fatalf("ReadOverlay(%q) error = nil, want an error", tt.file)
			}
		})
	}
}

func TestSaveKeepsAndRemovesOverlayStashes(t *testing.T) {
	dir := t.TempDir()
	keep, err := session.StashOverlay(dir, sampleOverlay())
	if err != nil {
		t.Fatal(err)
	}
	drop, err := session.StashOverlay(dir, sampleOverlay())
	if err != nil {
		t.Fatal(err)
	}
	// An overlay that no saved session names, such as the emergency copy that is written
	// when a file changes under an open document, must survive every Save.
	emergency, err := session.StashOverlay(dir, sampleOverlay())
	if err != nil {
		t.Fatal(err)
	}
	first := session.Session{Files: []session.FileState{
		{Path: "a.log", OverlayFile: keep},
		{Path: "b.log", OverlayFile: drop},
	}}
	if err := session.Save(dir, first); err != nil {
		t.Fatal(err)
	}
	got, ok, err := session.Load(dir)
	if err != nil || !ok || got.Files[0].OverlayFile != keep || got.Files[0].StashFile != "" {
		t.Fatalf("Load() = %+v, %v, %v: want the overlay file names back", got, ok, err)
	}
	second := session.Session{Files: []session.FileState{{Path: "a.log", OverlayFile: keep}}}
	if err := session.Save(dir, second); err != nil {
		t.Fatal(err)
	}
	assertExists(t, filepath.Join(dir, keep))
	assertGone(t, filepath.Join(dir, drop))
	assertExists(t, filepath.Join(dir, emergency))

	if err := session.Save(dir, session.Session{}); err != nil {
		t.Fatal(err)
	}
	assertGone(t, filepath.Join(dir, keep))
	assertExists(t, filepath.Join(dir, emergency))
}
