package app

import (
	"fmt"
	"github.com/tbxark/gvim/internal/pluginhost"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tbxark/gvim/internal/tui/palette"
)

// writeTree writes each file under dir, creating the folders it needs.
func writeTree(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for rel, text := range files {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		must(t, os.MkdirAll(filepath.Dir(p), 0o755))
		must(t, os.WriteFile(p, []byte(text), 0o644))
	}
}

func TestLiveGrepFindsLinesCaseInsensitively(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		"a.txt":     "todo here\nnothing\n  TODO again\n",
		"sub/b.txt": "x todo\n",
	})
	hits, err := liveGrep{}.Search(dir, []string{"a.txt", "sub/b.txt"}, "todo", 100)
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	want := []palette.GrepHit{
		{Rel: "a.txt", Line: 1, Col: 1, Text: "todo here"},
		{Rel: "a.txt", Line: 3, Col: 3, Text: "  TODO again"},
		{Rel: "sub/b.txt", Line: 1, Col: 3, Text: "x todo"},
	}
	if fmt.Sprint(hits) != fmt.Sprint(want) {
		t.Fatalf("hits = %v, want %v", hits, want)
	}
}

func TestLiveGrepOneHitPerLineAndLimit(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{"a.txt": "todo todo\ntodo\ntodo\n"})
	hits, err := liveGrep{}.Search(dir, []string{"a.txt"}, "todo", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 2 || hits[0].Line != 1 || hits[1].Line != 2 {
		t.Fatalf("hits = %v, want the first match of lines 1 and 2 (limit 2)", hits)
	}
}

func TestLiveGrepSkipsBinaryFiles(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{"bin.dat": "todo\x00\x01"})
	hits, err := liveGrep{}.Search(dir, []string{"bin.dat"}, "todo", 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 0 {
		t.Fatalf("binary file gave hits %v, want none", hits)
	}
}

func TestLiveGrepReadsAtMostOneMegabytePerFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	long := strings.Repeat("a", pluginhost.GrepMaxFileBytes) + "\ntodo\n"
	writeTree(t, dir, map[string]string{"long.txt": long})
	hits, err := liveGrep{}.Search(dir, []string{"long.txt"}, "todo", 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 0 {
		t.Fatalf("a match past the first megabyte was found: %v", hits)
	}
}

func TestLiveGrepSearchesAtMostTwoThousandFiles(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	files := make([]string, pluginhost.GrepMaxFiles+1)
	for i := range files {
		files[i] = fmt.Sprintf("f%04d.txt", i)
	}
	writeTree(t, dir, map[string]string{files[pluginhost.GrepMaxFiles]: "todo\n"})
	hits, err := liveGrep{}.Search(dir, files, "todo", 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 0 {
		t.Fatalf("the file past the cap was searched: %v", hits)
	}
}

func TestLiveGrepSkipsFilesThatCannotBeRead(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{"a.txt": "todo\n"})
	hits, err := liveGrep{}.Search(dir, []string{"gone.txt", "a.txt"}, "todo", 100)
	if err != nil {
		t.Fatalf("a missing file failed the search: %v", err)
	}
	if len(hits) != 1 || hits[0].Rel != "a.txt" {
		t.Fatalf("hits = %v, want the one in a.txt", hits)
	}
}

func TestLiveGrepEmptyQueryFindsNothing(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{"a.txt": "todo\n"})
	hits, err := liveGrep{}.Search(dir, []string{"a.txt"}, "", 100)
	if err != nil || len(hits) != 0 {
		t.Fatalf("empty query gave %v, %v; want nothing", hits, err)
	}
}
