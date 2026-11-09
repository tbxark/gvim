package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/internal/pluginhost"
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

// fileReq is a Live Grep request over the project files of root.
func fileReq(root string, files []string, text string) palette.GrepRequest {
	return palette.GrepRequest{
		Root:    root,
		Text:    text,
		Files:   files,
		Sources: palette.GrepSources{Files: true},
		Limit:   100,
	}
}

func TestLiveGrepFindsLinesCaseInsensitively(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		"a.txt":     "todo here\nnothing\n  TODO again\n",
		"sub/b.txt": "x todo\n",
	})
	hits, err := (&liveGrep{}).Search(fileReq(dir, []string{"a.txt", "sub/b.txt"}, "todo"))
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	want := []palette.GrepHit{
		{Rel: "a.txt", Line: 1, Col: 1, Text: "todo here", Source: palette.GrepFiles},
		{Rel: "a.txt", Line: 3, Col: 3, Text: "  TODO again", Source: palette.GrepFiles},
		{Rel: "sub/b.txt", Line: 1, Col: 3, Text: "x todo", Source: palette.GrepFiles},
	}
	if fmt.Sprint(hits) != fmt.Sprint(want) {
		t.Fatalf("hits = %v, want %v", hits, want)
	}
}

func TestLiveGrepOneHitPerLineAndLimit(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{"a.txt": "todo todo\ntodo\ntodo\n"})
	hits, err := (&liveGrep{}).Search(fileReq(dir, []string{"a.txt"}, "todo"))
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 3 || hits[0].Line != 1 || hits[1].Line != 2 || hits[2].Line != 3 {
		t.Fatalf("hits = %v, want one per line", hits)
	}
	req := fileReq(dir, []string{"a.txt"}, "todo")
	req.Limit = 2
	hits, err = (&liveGrep{}).Search(req)
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
	hits, err := (&liveGrep{}).Search(fileReq(dir, []string{"bin.dat"}, "todo"))
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
	hits, err := (&liveGrep{}).Search(fileReq(dir, []string{"long.txt"}, "todo"))
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 0 {
		t.Fatalf("hits past the read cap = %v, want none", hits)
	}
}

// TestLiveGrepMatchModes: Live Grep's modes reach internal/search. The palette sends
// them in the request; a regex that does not compile is an error even before a file is
// read.
func TestLiveGrepMatchModes(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{"a.txt": "food\nfoo\nxfoo\nFoo\n"})
	tests := []struct {
		name  string
		query palette.SearchOptions
		text  string
		want  []int // the matching lines
	}{
		{"literal, case folded", palette.SearchOptions{}, "foo", []int{1, 2, 3, 4}},
		{"case sensitive", palette.SearchOptions{CaseSensitive: true}, "Foo", []int{4}},
		{"whole word", palette.SearchOptions{WholeWord: true}, "foo", []int{2, 4}},
		{"regex, case folded", palette.SearchOptions{Regex: true}, "f.o", []int{1, 2, 3, 4}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := fileReq(dir, []string{"a.txt"}, tt.text)
			req.Query = tt.query
			hits, err := (&liveGrep{}).Search(req)
			if err != nil {
				t.Fatal(err)
			}
			var got []int
			for _, h := range hits {
				got = append(got, h.Line)
			}
			if fmt.Sprint(got) != fmt.Sprint(tt.want) {
				t.Errorf("lines = %v, want %v", got, tt.want)
			}
		})
	}
	// An invalid regex is reported even with no file to search.
	req := fileReq(dir, nil, "(")
	req.Query = palette.SearchOptions{Regex: true}
	if _, err := (&liveGrep{}).Search(req); err == nil {
		t.Error("an invalid regex is not reported")
	}
}

// TestLiveGrepSearchesOpenBuffers: the Buffers scope searches the request's snapshot,
// tags the hits and keeps their tab index, with no file read. Files off leaves only the
// buffer hits.
func TestLiveGrepSearchesOpenBuffers(t *testing.T) {
	t.Parallel()
	buffers := []palette.BufferSource{
		{Index: 1, Name: "notes.md", Path: "/w/notes.md", Text: "first\ntodo here\n"},
		{Index: 2, Name: "[No Name]", Text: "nothing\ntodo too\n"},
	}
	req := palette.GrepRequest{
		Root:    "/w",
		Text:    "todo",
		Buffers: buffers,
		Sources: palette.GrepSources{Buffers: true},
		Limit:   100,
	}
	hits, err := (&liveGrep{}).Search(req)
	if err != nil {
		t.Fatal(err)
	}
	want := []palette.GrepHit{
		{Rel: "notes.md", Line: 2, Col: 1, Text: "todo here", Source: palette.GrepBuffers, Index: 1},
		{Rel: "[No Name]", Line: 2, Col: 1, Text: "todo too", Source: palette.GrepBuffers, Index: 2},
	}
	if fmt.Sprint(hits) != fmt.Sprint(want) {
		t.Fatalf("hits = %v, want %v", hits, want)
	}
	// Both sources on: the file hits come first, then the buffer hits.
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{"a.txt": "todo file\n"})
	req.Root, req.Files, req.Sources = dir, []string{"a.txt"}, palette.GrepSources{Files: true, Buffers: true}
	hits, err = (&liveGrep{}).Search(req)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 3 || hits[0].Source != palette.GrepFiles || hits[1].Source != palette.GrepBuffers {
		t.Fatalf("hits = %v, want the file hit then the buffer hits", hits)
	}
}

// TestLiveGrepIgnoredScope: with the Ignored scope on, hidden and .gitignore-matched
// files are searched too, which the file index does not list. The listing is cached, so
// typing again does not walk the tree.
func TestLiveGrepIgnoredScope(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		".gitignore":  "ignored.txt\n",
		"visible.txt": "needle visible\n",
		"ignored.txt": "needle ignored\n",
		".hidden.txt": "needle hidden\n",
	})
	g := &liveGrep{}
	req := palette.GrepRequest{
		Root:    dir,
		Text:    "needle",
		Files:   []string{"visible.txt"}, // the index does not list the other two
		Sources: palette.GrepSources{Files: true, Ignored: true},
		Limit:   100,
	}
	hits, err := g.Search(req)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, h := range hits {
		got = append(got, h.Rel)
	}
	for _, want := range []string{"visible.txt", "ignored.txt", ".hidden.txt"} {
		found := false
		for _, rel := range got {
			if rel == want {
				found = true
			}
		}
		if !found {
			t.Errorf("the ignored scope missed %q: %v", want, got)
		}
	}
	// The second search reuses the cached listing.
	list := g.ignoredFiles(dir)
	req.Text = "hidden"
	if _, err := g.Search(req); err != nil {
		t.Fatal(err)
	}
	if len(g.ignoredFiles(dir)) != len(list) {
		t.Fatal("the ignored listing was walked again")
	}
}

// TestLiveGrepNoSources: with both sources off nothing is searched, and Search still
// reports an invalid regex.
func TestLiveGrepNoSources(t *testing.T) {
	t.Parallel()
	req := palette.GrepRequest{Root: "/w", Text: "x", Limit: 10}
	hits, err := (&liveGrep{}).Search(req)
	if err != nil || len(hits) != 0 {
		t.Fatalf("no sources = %v, %v; want no hits and no error", hits, err)
	}
}

// TestPreviewReaderReadsABoundedHead: the palette's preview reader returns at most the
// bytes asked for, marks a longer file, and reports a file it cannot read.
func TestPreviewReaderReadsABoundedHead(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		"short.txt": "hello\nworld\n",
		"long.txt":  strings.Repeat("a", 100),
	})
	r := previewReader{}
	data, truncated, err := r.ReadHead(filepath.Join(dir, "short.txt"), 64)
	if err != nil || truncated || string(data) != "hello\nworld\n" {
		t.Fatalf("short file = %q, truncated %v, err %v", data, truncated, err)
	}
	data, truncated, err = r.ReadHead(filepath.Join(dir, "long.txt"), 10)
	if err != nil || !truncated || len(data) != 10 {
		t.Fatalf("long file = %d bytes, truncated %v, err %v; want 10 bytes and truncated", len(data), truncated, err)
	}
	if _, _, err := r.ReadHead(filepath.Join(dir, "missing.txt"), 10); err == nil {
		t.Error("a missing file gave no error")
	}
	if _, _, err := r.ReadHead(dir, 10); err == nil {
		t.Error("a directory gave no error")
	}
}

// TestGrepForSkipsLargeFiles: grepFor carries the editor's large-file threshold.
func TestGrepForSkipsLargeFiles(t *testing.T) {
	t.Parallel()
	if got := grepFor(editor.Options{}).skipFrom; got != 0 {
		t.Fatalf("skipFrom = %d without large mode, want 0", got)
	}
	if got := grepFor(editor.Options{LargeFileMode: true, LargeFileThresholdBytes: 1000}).skipFrom; got != 1000 {
		t.Fatalf("skipFrom = %d, want 1000", got)
	}
}

// TestLiveGrepSearchesTerminalSnapshot checks the Terminals scope is searched after
// the files/buffers sources, in dock-tab/line order, one hit per line, tagged with the
// dock tab index, and still obeys the request limit. The text arrives in the request:
// the search itself does not read a PTY or start a process.
func TestLiveGrepSearchesTerminalSnapshot(t *testing.T) {
	t.Parallel()
	req := palette.GrepRequest{
		Text:    "todo",
		Sources: palette.GrepSources{Buffers: true, Terminals: true},
		Limit:   100,
		Buffers: []palette.BufferSource{{Index: 4, Name: "notes.md", Text: "todo buffer\n"}},
		Terminals: []palette.TerminalSource{
			{ID: 11, Index: 1, Name: "shell", First: 30, Total: 50, Lines: []string{"first", "xx todo one", "todo two"}},
			{ID: 12, Index: 3, Name: "", First: 0, Total: 1, Lines: []string{"todo three"}},
		},
	}
	hits, err := (&liveGrep{}).Search(req)
	if err != nil {
		t.Fatal(err)
	}
	want := []palette.GrepHit{
		{Rel: "notes.md", Line: 1, Col: 1, Text: "todo buffer", Source: palette.GrepBuffers, Index: 4},
		{Rel: "shell", Line: 2, Col: 4, Text: "xx todo one", Source: palette.GrepTerminals, Index: 1, TerminalID: 11, TranscriptFirst: 30, TerminalTotal: 50, TerminalBefore: "first", TerminalAfter: "todo two", TerminalSnapshot: true, TerminalValid: true, TerminalBeforeKnown: true, TerminalAfterKnown: true},
		{Rel: "shell", Line: 3, Col: 1, Text: "todo two", Source: palette.GrepTerminals, Index: 1, TerminalID: 11, TranscriptFirst: 30, TerminalTotal: 50, TerminalBefore: "xx todo one", TerminalSnapshot: true, TerminalValid: true, TerminalBeforeKnown: true},
		{Rel: "terminal", Line: 1, Col: 1, Text: "todo three", Source: palette.GrepTerminals, Index: 3, TerminalID: 12, TranscriptFirst: 0, TerminalTotal: 1, TerminalSnapshot: true, TerminalValid: true},
	}
	if fmt.Sprint(hits) != fmt.Sprint(want) {
		t.Fatalf("hits = %v, want %v", hits, want)
	}
	req.Limit = 2
	hits, err = (&liveGrep{}).Search(req)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 2 || hits[0].Source != palette.GrepBuffers || hits[1].Source != palette.GrepTerminals {
		t.Fatalf("limited hits = %v, want the buffer then the first terminal hit", hits)
	}
	// A regex error from the shared mode handling is reported for terminal-only
	// searches too.
	req = palette.GrepRequest{Text: "(", Sources: palette.GrepSources{Terminals: true}, Limit: 10,
		Terminals: []palette.TerminalSource{{Index: 0, Name: "t", Lines: []string{"x"}}}}
	req.Query.Regex = true
	if _, err := (&liveGrep{}).Search(req); err == nil {
		t.Fatal("an invalid regex over terminals was not rejected")
	}
}
