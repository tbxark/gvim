package session_test

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"github.com/tbxark/gvim/session"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func assertExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("%s should exist: %v", path, err)
	}
}

func assertGone(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("%s should be removed, stat error = %v", path, err)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	tests := []struct {
		name string
		in   session.Session
	}{
		{
			name: "every field set",
			in: session.Session{
				Root:        "/work/project",
				Explorer:    true,
				ShowHidden:  true,
				ShowIgnored: true,
				Active:      1,
				Files: []session.FileState{
					{Path: "/work/project/main.go", Line: 12, Col: 4, Top: 3},
					{Path: "/work/project/notes.md", Line: 1, Col: 0, Top: 0, StashFile: "stash-123.txt"},
				},
			},
		},
		{
			name: "no files and every flag off",
			in:   session.Session{Root: "/work/empty", Files: []session.FileState{}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := session.Save(dir, tt.in); err != nil {
				t.Fatal(err)
			}
			got, ok, err := session.Load(dir)
			if err != nil {
				t.Fatal(err)
			}
			if !ok {
				t.Fatal("Load() ok = false after Save")
			}
			want := tt.in
			want.Version = session.Version
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("Load() = %+v, want %+v", got, want)
			}
		})
	}
}

func TestLoadMissingFile(t *testing.T) {
	got, ok, err := session.Load(t.TempDir())
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}
	if ok {
		t.Fatal("Load() ok = true for a missing session.json")
	}
	if !reflect.DeepEqual(got, session.Session{}) {
		t.Fatalf("Load() = %+v, want zero Session", got)
	}
}

func TestSaveLeavesOnlySessionFile(t *testing.T) {
	dir := t.TempDir()
	for range 2 {
		if err := session.Save(dir, session.Session{Root: "/work"}); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if !slices.Equal(names, []string{"session.json"}) {
		t.Fatalf("directory after Save holds %q, want only session.json", names)
	}
}

func TestSavedFilesArePrivate(t *testing.T) {
	dir := t.TempDir()
	if err := session.Save(dir, session.Session{Root: "/work"}); err != nil {
		t.Fatal(err)
	}
	if err := session.SaveSearch(dir, session.Search{}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"session.json", "search.json"} {
		info, err := os.Stat(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("stat %s: %v", name, err)
		}
		if got := info.Mode().Perm(); got != 0o600 {
			t.Errorf("mode of a new %s = %o, want 600", name, got)
		}
	}
}

func TestLoadRejectsUnknownVersion(t *testing.T) {
	tests := []struct {
		name    string
		version int
	}{
		{name: "zero", version: 0},
		{name: "next version", version: session.Version + 1},
		{name: "far future", version: 99},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			body := fmt.Sprintf(`{"version":%d,"root":"/work","files":[]}`, tt.version)
			writeFile(t, filepath.Join(dir, "session.json"), body)
			_, ok, err := session.Load(dir)
			if err == nil {
				t.Fatalf("Load() error = nil for version %d, want an error", tt.version)
			}
			if ok {
				t.Fatalf("Load() ok = true for version %d, want false", tt.version)
			}
		})
	}
}

func TestStashRoundTrip(t *testing.T) {
	tests := []struct {
		name string
		text string
	}{
		{name: "plain text", text: "unsaved line\n"},
		{name: "empty", text: ""},
		{name: "multi-line unicode", text: "héllo\n世界\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			name, err := session.StashContent(dir, tt.text)
			if err != nil {
				t.Fatal(err)
			}
			if name == "" {
				t.Fatal("StashContent() returned an empty name")
			}
			assertExists(t, filepath.Join(dir, name))
			got, err := session.ReadStash(dir, name)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.text {
				t.Fatalf("ReadStash() = %q, want %q", got, tt.text)
			}
		})
	}
}

func TestReadStashRejectsForeignNames(t *testing.T) {
	// secret.txt sits beside the session directory. A traversal name must not
	// reach it, and a file without the stash prefix must not be read either.
	parent := t.TempDir()
	dir := filepath.Join(parent, "session")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(parent, "secret.txt"), "secret")
	writeFile(t, filepath.Join(dir, "notes.txt"), "not a stash")

	tests := []struct {
		name string
		file string
	}{
		{name: "empty", file: ""},
		{name: "session file", file: "session.json"},
		{name: "unprefixed text file", file: "notes.txt"},
		{name: "parent traversal", file: "../secret.txt"},
		{name: "prefixed traversal", file: "stash-/../../secret.txt"},
		{name: "nested path", file: "sub/stash-1.txt"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := session.ReadStash(dir, tt.file); err == nil {
				t.Fatalf("ReadStash(%q) error = nil, want an error", tt.file)
			}
		})
	}
}

func TestSaveRemovesOnlyStashesThePreviousSessionReferenced(t *testing.T) {
	dir := t.TempDir()
	keep, err := session.StashContent(dir, "keep me")
	if err != nil {
		t.Fatal(err)
	}
	orphan, err := session.StashContent(dir, "no session names me")
	if err != nil {
		t.Fatal(err)
	}
	notes := filepath.Join(dir, "notes.txt")
	writeFile(t, notes, "not ours")

	// No saved session referenced orphan, so this Save must not delete it.
	first := session.Session{Files: []session.FileState{{Path: "a.go", StashFile: keep}}}
	if err := session.Save(dir, first); err != nil {
		t.Fatal(err)
	}
	assertExists(t, filepath.Join(dir, keep))
	assertExists(t, filepath.Join(dir, orphan))
	assertExists(t, notes)
	if text, err := session.ReadStash(dir, keep); err != nil || text != "keep me" {
		t.Fatalf("ReadStash(kept) = %q, %v; want %q, nil", text, err, "keep me")
	}

	// The second Save drops keep, which the first session referenced.
	if err := session.Save(dir, session.Session{}); err != nil {
		t.Fatal(err)
	}
	assertGone(t, filepath.Join(dir, keep))
	assertExists(t, filepath.Join(dir, orphan))
	assertExists(t, notes)
}

func TestSaveKeepsStashWrittenByAnotherWriter(t *testing.T) {
	dir := t.TempDir()
	mine, err := session.StashContent(dir, "mine")
	if err != nil {
		t.Fatal(err)
	}
	if err := session.Save(dir, session.Session{Files: []session.FileState{{Path: "a.go", StashFile: mine}}}); err != nil {
		t.Fatal(err)
	}

	// Another writer stashes text after this writer's last Save, and this
	// writer's next Save does not list it.
	theirs, err := session.StashContent(dir, "theirs")
	if err != nil {
		t.Fatal(err)
	}
	if err := session.Save(dir, session.Session{}); err != nil {
		t.Fatal(err)
	}
	assertGone(t, filepath.Join(dir, mine))
	assertExists(t, filepath.Join(dir, theirs))
	if text, err := session.ReadStash(dir, theirs); err != nil || text != "theirs" {
		t.Fatalf("ReadStash(other writer's) = %q, %v; want %q, nil", text, err, "theirs")
	}
}

func TestSaveNeverDeletesFilesNamedByThePreviousSession(t *testing.T) {
	// secret.txt sits beside the session directory. A previous session.json that
	// names it, or a file that is not a stash, must not make Save delete it.
	parent := t.TempDir()
	dir := filepath.Join(parent, "session")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(parent, "secret.txt")
	writeFile(t, secret, "secret")
	notes := filepath.Join(dir, "notes.txt")
	writeFile(t, notes, "not ours")
	body := `{"version":1,"root":"/work","files":[` +
		`{"path":"a.go","stashFile":"../secret.txt"},` +
		`{"path":"b.go","stashFile":"notes.txt"},` +
		`{"path":"c.go","stashFile":"sub/stash-1.txt"}]}`
	writeFile(t, filepath.Join(dir, "session.json"), body)

	if err := session.Save(dir, session.Session{}); err != nil {
		t.Fatal(err)
	}
	assertExists(t, secret)
	assertExists(t, notes)
}

func TestSaveWithUnreadablePreviousSessionDeletesNoStash(t *testing.T) {
	dir := t.TempDir()
	stash, err := session.StashContent(dir, "unsaved text")
	if err != nil {
		t.Fatal(err)
	}
	// The previous session cannot be decoded, so nothing shows which stash
	// files it referenced. Save writes the new session and deletes none.
	writeFile(t, filepath.Join(dir, "session.json"), "{not json")

	if err := session.Save(dir, session.Session{Root: "/work"}); err != nil {
		t.Fatalf("Save() error = %v, want nil", err)
	}
	assertExists(t, filepath.Join(dir, stash))
	if got, ok, err := session.Load(dir); err != nil || !ok || got.Root != "/work" {
		t.Fatalf("Load() = %+v, %v, %v; want the new session", got, ok, err)
	}
}
