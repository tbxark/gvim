package app

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/internal/pluginhost"
	"github.com/tbxark/gvim/internal/tui/searchpanel"
)

// project search fixture helpers.

func projectSearchRequest(root string, files []string, query string) searchpanel.SearchRequest {
	return searchpanel.SearchRequest{
		Root:  root,
		Query: query,
		Files: files,
		Limit: searchpanel.DefaultLimit,
	}
}

func TestProjectSearchSearchesFilesAndSkipsWithReasons(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		"a.txt":     "foo bar\nFOO baz\n",
		"sub/b.txt": "xfoo\n",
		"bin.dat":   "foo\x00",
		"big.txt":   strings.Repeat("a", 1<<20) + "\nfoo\n",
		"bad.txt":   "foo\xff",
	})
	s := newProjectSearch(editor.Options{})
	req := projectSearchRequest(dir, []string{"a.txt", "sub/b.txt", "bin.dat", "big.txt", "bad.txt"}, "foo")
	res, err := s.ProjectSearch(req)
	if err != nil {
		t.Fatalf("ProjectSearch: %v", err)
	}
	if len(res.Files) != 2 {
		t.Fatalf("files = %+v, want a.txt and sub/b.txt", res.Files)
	}
	if res.Files[0].Path != filepath.Join(dir, "a.txt") || res.Files[0].Name != "a.txt" {
		t.Fatalf("first file = %+v", res.Files[0])
	}
	if len(res.Files[0].Matches) != 2 || res.Files[0].Matches[1].Text != "FOO baz" {
		t.Fatalf("a.txt matches = %+v, want both case-folded lines", res.Files[0].Matches)
	}
	if len(res.Files[1].Matches) != 1 || res.Files[1].Matches[0].Line != 1 {
		t.Fatalf("sub/b.txt matches = %+v", res.Files[1].Matches)
	}
	if res.Files[0].Hash == 0 || res.Files[0].Dirty {
		t.Fatalf("a.txt hash/dirty = %d/%v", res.Files[0].Hash, res.Files[0].Dirty)
	}
	if res.SkippedCount != 3 || len(res.Skipped) != 3 {
		t.Fatalf("skipped = %d %v, want three reasons", res.SkippedCount, res.Skipped)
	}
	skipped := strings.Join(res.Skipped, ", ")
	for _, want := range []string{"1 binary file", "1 large file", "1 non-UTF-8 file"} {
		if !strings.Contains(skipped, want) {
			t.Errorf("skipped = %q, want %q", skipped, want)
		}
	}
}

func TestProjectSearchMatchModes(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{"a.txt": "food\nfoo\nFOO bar\n"})
	s := newProjectSearch(editor.Options{})
	tests := []struct {
		name  string
		opts  searchpanel.MatchOptions
		query string
		want  []int
	}{
		{"literal folded", searchpanel.MatchOptions{}, "foo", []int{1, 2, 3}},
		{"case sensitive", searchpanel.MatchOptions{CaseSensitive: true}, "FOO", []int{3}},
		{"whole word", searchpanel.MatchOptions{WholeWord: true}, "foo", []int{2, 3}},
		{"regex", searchpanel.MatchOptions{Regex: true}, "fo+", []int{1, 2, 3}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := projectSearchRequest(dir, []string{"a.txt"}, tt.query)
			req.Options = tt.opts
			res, err := s.ProjectSearch(req)
			if err != nil {
				t.Fatal(err)
			}
			if len(res.Files) != 1 {
				t.Fatalf("files = %+v", res.Files)
			}
			var lines []int
			for _, m := range res.Files[0].Matches {
				lines = append(lines, m.Line)
			}
			if len(lines) != len(tt.want) {
				t.Fatalf("lines = %v, want %v", lines, tt.want)
			}
			for i, line := range lines {
				if line != tt.want[i] {
					t.Fatalf("lines = %v, want %v", lines, tt.want)
				}
			}
		})
	}
	req := projectSearchRequest(dir, []string{"a.txt"}, "(")
	req.Options = searchpanel.MatchOptions{Regex: true}
	if _, err := s.ProjectSearch(req); err == nil {
		t.Fatal("an invalid regex is not reported")
	}
}

func TestProjectSearchDirtyBufferAndCurrentFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{"a.txt": "saved text\n"})
	s := newProjectSearch(editor.Options{})
	dirty := searchpanel.SourceFile{Path: filepath.Join(dir, "a.txt"), Name: "a.txt", Text: "unsaved todo\n", Dirty: true}
	req := projectSearchRequest(dir, []string{"a.txt"}, "todo")
	req.OpenFiles = []searchpanel.SourceFile{dirty}
	res, err := s.ProjectSearch(req)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Files) != 1 || len(res.Files[0].Matches) != 1 || !res.Files[0].Dirty {
		t.Fatalf("dirty search = %+v, want the unsaved text", res.Files)
	}
	// The current-file scope searches only the supplied buffer.
	req = projectSearchRequest(dir, []string{"a.txt"}, "unsaved")
	req.Scope = searchpanel.CurrentFile
	req.Current = &dirty
	res, err = s.ProjectSearch(req)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Files) != 1 || res.Files[0].Name != "a.txt" || len(res.Files[0].Matches) != 1 {
		t.Fatalf("current file search = %+v", res.Files)
	}
	// An unnamed current buffer shows its placeholder name.
	unnamed := searchpanel.SourceFile{Name: "", Text: "unsaved todo", Dirty: true}
	req.Current = &unnamed
	res, err = s.ProjectSearch(req)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Files) != 1 || res.Files[0].Name != "[No Name]" {
		t.Fatalf("unnamed current search = %+v", res.Files)
	}
}

func TestProjectSearchCapsTheMatches(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{"a.txt": "todo\ntodo\ntodo\n", "b.txt": "todo\n"})
	s := newProjectSearch(editor.Options{})
	req := projectSearchRequest(dir, []string{"a.txt", "b.txt"}, "todo")
	req.Limit = 2
	res, err := s.ProjectSearch(req)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Truncated {
		t.Fatal("the result is not marked capped")
	}
	total := 0
	for _, f := range res.Files {
		total += len(f.Matches)
	}
	if total != 2 {
		t.Fatalf("matches = %d, want the cap of 2", total)
	}
}

func TestProjectReplacePlansExpandedEdits(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{"a.txt": "foo1 foo2\n"})
	s := newProjectSearch(editor.Options{})
	searchReq := projectSearchRequest(dir, []string{"a.txt"}, "foo([0-9])")
	searchReq.Options = searchpanel.MatchOptions{Regex: true}
	res, err := s.ProjectSearch(searchReq)
	if err != nil {
		t.Fatal(err)
	}
	hash := res.Files[0].Hash
	rep := searchpanel.ReplaceRequest{
		Root: dir, Query: "foo([0-9])", Replacement: "bar$1",
		Options: searchpanel.MatchOptions{Regex: true},
		Files:   []searchpanel.ReplaceFile{{Path: filepath.Join(dir, "a.txt"), Name: "a.txt", Hash: hash}},
		Limit:   searchpanel.DefaultLimit,
	}
	out, err := s.ProjectReplace(rep)
	if err != nil {
		t.Fatalf("ProjectReplace: %v", err)
	}
	if len(out.Files) != 1 || len(out.Files[0].Edits) != 2 {
		t.Fatalf("edits = %+v, want two", out.Files)
	}
	if got := out.Files[0].Edits[0].Text; got != "bar1" {
		t.Fatalf("first edit text = %q, want bar1", got)
	}
	if got := out.Files[0].Edits[1].Text; got != "bar2" {
		t.Fatalf("second edit text = %q, want bar2", got)
	}
	from := out.Files[0].Edits[1].From
	if from.Line != 0 || from.Col != 5 {
		t.Fatalf("second edit From = %+v, want the second match", from)
	}
}

func TestProjectReplaceDetectsStaleAndDirtyText(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{"a.txt": "foo\n"})
	s := newProjectSearch(editor.Options{})
	line := filepath.Join(dir, "a.txt")
	// Stale: the hash names other text.
	rep := searchpanel.ReplaceRequest{
		Root: dir, Query: "foo", Replacement: "bar",
		Files: []searchpanel.ReplaceFile{{Path: line, Name: "a.txt", Hash: 42}},
	}
	out, err := s.ProjectReplace(rep)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Files) != 1 || !out.Files[0].Stale {
		t.Fatalf("stale result = %+v, want the file refused", out.Files)
	}
	// Dirty: the text comes from the request, not the disk.
	dirtyText := "unsaved foo\n"
	rep = searchpanel.ReplaceRequest{
		Root: dir, Query: "foo", Replacement: "bar",
		Files: []searchpanel.ReplaceFile{{Path: line, Name: "a.txt", Hash: hashText(dirtyText), Dirty: true, Text: dirtyText}},
	}
	out, err = s.ProjectReplace(rep)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Files) != 1 || len(out.Files[0].Edits) != 1 || out.Files[0].Stale {
		t.Fatalf("dirty replace = %+v, want one edit", out.Files)
	}
	// A missing file is reported per file, not as a failed replacement.
	rep = searchpanel.ReplaceRequest{
		Root: dir, Query: "foo", Replacement: "bar",
		Files: []searchpanel.ReplaceFile{{Path: filepath.Join(dir, "gone.txt"), Name: "gone.txt"}},
	}
	out, err = s.ProjectReplace(rep)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Files) != 1 || out.Files[0].Err == "" {
		t.Fatalf("missing file = %+v, want a per-file error", out.Files)
	}
}

func TestProjectReplaceLimitsEdits(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{"a.txt": "todo todo todo\n"})
	s := newProjectSearch(editor.Options{})
	searchReq := projectSearchRequest(dir, []string{"a.txt"}, "todo")
	res, err := s.ProjectSearch(searchReq)
	if err != nil {
		t.Fatal(err)
	}
	rep := searchpanel.ReplaceRequest{
		Root: dir, Query: "todo", Replacement: "done",
		Files: []searchpanel.ReplaceFile{{Path: filepath.Join(dir, "a.txt"), Name: "a.txt", Hash: res.Files[0].Hash}},
		Limit: 1,
	}
	out, err := s.ProjectReplace(rep)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Files) != 1 || len(out.Files[0].Edits) != 1 {
		t.Fatalf("edits = %+v, want the limit of one", out.Files)
	}
}

func TestProjectReplaceOnlySelectedMatches(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{"a.txt": "foo foo foo\n"})
	s := newProjectSearch(editor.Options{})
	searchReq := projectSearchRequest(dir, []string{"a.txt"}, "foo")
	res, err := s.ProjectSearch(searchReq)
	if err != nil {
		t.Fatal(err)
	}
	line := filepath.Join(dir, "a.txt")
	rep := searchpanel.ReplaceRequest{
		Root: dir, Query: "foo", Replacement: "bar",
		Files: []searchpanel.ReplaceFile{{
			Path: line, Name: "a.txt", Hash: res.Files[0].Hash,
			Selected: []searchpanel.Position{{Line: 0, Col: 4}},
		}},
	}
	out, err := s.ProjectReplace(rep)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Files) != 1 || len(out.Files[0].Edits) != 1 {
		t.Fatalf("edits = %+v, want the one selected match", out.Files)
	}
	if got := out.Files[0].Edits[0]; got.From.Line != 0 || got.From.Col != 4 || got.Text != "bar" {
		t.Fatalf("selected edit = %+v, want the second foo", got)
	}
	// A selected start that is not a hit is dropped, never applied at a guessed
	// offset.
	rep.Files[0].Selected = []searchpanel.Position{{Line: 0, Col: 2}}
	out, err = s.ProjectReplace(rep)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Files) != 0 {
		t.Fatalf("a non-hit selection produced edits: %+v", out.Files)
	}
}

func TestProjectSearchMultiLineRanges(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{"a.txt": "x foo\nbar y\n"})
	s := newProjectSearch(editor.Options{})
	req := projectSearchRequest(dir, []string{"a.txt"}, `foo\nbar`)
	req.Options = searchpanel.MatchOptions{Regex: true}
	res, err := s.ProjectSearch(req)
	if err != nil {
		t.Fatalf("ProjectSearch: %v", err)
	}
	if len(res.Files) != 1 || len(res.Files[0].Matches) != 1 {
		t.Fatalf("matches = %+v, want one", res.Files)
	}
	m := res.Files[0].Matches[0]
	if m.Line != 1 || m.Col != 3 || !m.MultiLine() {
		t.Fatalf("match start = %d:%d multi %v, want 1:3 multi-line", m.Line, m.Col, m.MultiLine())
	}
	if m.EndLine != 2 || m.EndCol != 4 {
		t.Fatalf("match end = %d:%d, want 2:4", m.EndLine, m.EndCol)
	}
	// The replacement plan keeps the exact multi-line range.
	rep := searchpanel.ReplaceRequest{
		Root: dir, Query: `foo\nbar`, Replacement: "Z",
		Options: searchpanel.MatchOptions{Regex: true},
		Files: []searchpanel.ReplaceFile{{
			Path: filepath.Join(dir, "a.txt"), Name: "a.txt", Hash: res.Files[0].Hash,
		}},
	}
	out, err := s.ProjectReplace(rep)
	if err != nil {
		t.Fatalf("ProjectReplace: %v", err)
	}
	if len(out.Files) != 1 || len(out.Files[0].Edits) != 1 {
		t.Fatalf("edits = %+v, want one", out.Files)
	}
	edit := out.Files[0].Edits[0]
	if edit.From.Line != 0 || edit.From.Col != 2 || edit.To.Line != 1 || edit.To.Col != 3 {
		t.Fatalf("edit range = %+v..%+v, want 0:2..1:3", edit.From, edit.To)
	}
}

func TestProjectPreviewContext(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	lines := make([]string, 10)
	for i := range lines {
		lines[i] = fmt.Sprintf("line%d", i+1)
	}
	writeTree(t, dir, map[string]string{"a.txt": strings.Join(lines, "\n") + "\n"})
	s := newProjectSearch(editor.Options{})
	file := filepath.Join(dir, "a.txt")
	// The window is centred on a single-line match.
	res, err := s.Preview(searchpanel.PreviewRequest{Path: file, Start: -1, Line: 6, Col: 1, EndLine: 6, EndCol: 2, Lines: 5})
	if err != nil {
		t.Fatal(err)
	}
	if res.Err != "" || res.Start != 3 || len(res.Lines) != 5 {
		t.Fatalf("preview = start %d lines %v err %q, want lines 4-8", res.Start, res.Lines, res.Err)
	}
	if res.Lines[0] != "line4" || res.Lines[4] != "line8" {
		t.Fatalf("preview lines = %v", res.Lines)
	}
	// A match that fits the window is shown whole, even when it spans lines.
	res, err = s.Preview(searchpanel.PreviewRequest{Path: file, Start: -1, Line: 4, Col: 1, EndLine: 6, EndCol: 5, Lines: 5})
	if err != nil {
		t.Fatal(err)
	}
	if res.Start > 3 || res.Start+len(res.Lines) < 6 {
		t.Fatalf("multi-line preview start %d lines %d does not cover 4..6", res.Start, len(res.Lines))
	}
	// The panel's unsaved text is used instead of the disk.
	res, err = s.Preview(searchpanel.PreviewRequest{
		Path: file, Start: -1, Text: "unsaved one\nunsaved two\n", Dirty: true,
		Line: 2, Col: 1, EndLine: 2, EndCol: 2, Lines: 4,
	})
	if err != nil || len(res.Lines) != 2 || res.Lines[0] != "unsaved one" || res.Lines[1] != "unsaved two" {
		t.Fatalf("unsaved preview = %+v, %v", res, err)
	}
	// A large or unreadable file is a reason, not lines.
	res, err = s.Preview(searchpanel.PreviewRequest{Path: filepath.Join(dir, "gone.txt"), Start: -1, Line: 1, Col: 1, EndLine: 1, EndCol: 2, Lines: 4})
	if err != nil || res.Err != "unreadable file" {
		t.Fatalf("missing file preview = %+v, %v", res, err)
	}
}

// batchFiles makes n files of one line each, named so the finder order is clear.
func batchFiles(n int) ([]string, map[string]string) {
	files := make([]string, n)
	content := make(map[string]string, n)
	for i := range files {
		files[i] = fmt.Sprintf("f%03d.txt", i)
		content[files[i]] = "needle here\n"
	}
	return files, content
}

func TestProjectSearchBatchesStreamInFinderOrder(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	files, content := batchFiles(300)
	writeTree(t, dir, content)
	s := newProjectSearch(editor.Options{})
	ch := s.ProjectSearchBatches(projectSearchRequest(dir, files, "needle"), make(chan struct{}))
	var got []string
	batches := 0
	last := -1
	for batch := range ch {
		batches++
		if batch.Total != len(files) {
			t.Fatalf("batch total = %d, want %d", batch.Total, len(files))
		}
		if batch.Scanned < last {
			t.Fatalf("batch scanned went backwards: %d after %d", batch.Scanned, last)
		}
		last = batch.Scanned
		if len(batch.Files) > searchBatchFiles {
			t.Fatalf("batch carries %d files, want at most %d", len(batch.Files), searchBatchFiles)
		}
		for _, f := range batch.Files {
			got = append(got, f.Name)
		}
		if batch.Done && got[len(got)-1] != files[len(files)-1] {
			t.Fatalf("the done batch came before the last file: %q", got[len(got)-1])
		}
	}
	if batches < 3 {
		t.Fatalf("batches = %d, want the 300 files split", batches)
	}
	if !slices.Equal(got, files) {
		t.Fatalf("files = %v, want the finder order", got)
	}
	// The one-shot call drains the same stream to the same result.
	res, err := s.ProjectSearch(projectSearchRequest(dir, files, "needle"))
	if err != nil || len(res.Files) != len(files) {
		t.Fatalf("one-shot result = %d files, %v", len(res.Files), err)
	}
}

func TestProjectSearchBatchesAreBoundedByMatches(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	content := map[string]string{}
	files := make([]string, 20)
	for i := range files {
		files[i] = fmt.Sprintf("f%03d.txt", i)
		content[files[i]] = strings.Repeat("needle\n", 100)
	}
	writeTree(t, dir, content)
	s := newProjectSearch(editor.Options{})
	ch := s.ProjectSearchBatches(projectSearchRequest(dir, files, "needle"), make(chan struct{}))
	batches := 0
	for batch := range ch {
		batches++
		matches := 0
		for _, f := range batch.Files {
			matches += len(f.Matches)
		}
		if matches > searchBatchMatches {
			t.Fatalf("batch carries %d matches, want at most %d", matches, searchBatchMatches)
		}
	}
	if batches < 2 {
		t.Fatalf("batches = %d, want the match cap to split the stream", batches)
	}
}

func TestProjectSearchBatchesStopOnCancel(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	files, content := batchFiles(300)
	writeTree(t, dir, content)
	s := newProjectSearch(editor.Options{})
	cancel := make(chan struct{})
	ch := s.ProjectSearchBatches(projectSearchRequest(dir, files, "needle"), cancel)
	first := <-ch
	if first.Done {
		t.Fatal("the first batch was already the done batch")
	}
	close(cancel)
	got := len(first.Files)
	closed := make(chan struct{})
	go func() {
		for batch := range ch {
			got += len(batch.Files)
		}
		close(closed)
	}()
	select {
	case <-closed:
	case <-time.After(5 * time.Second):
		t.Fatal("the stream did not close after the cancellation")
	}
	if got >= len(files) {
		t.Fatalf("a cancelled search still walked every file (%d)", got)
	}
}

func TestProjectSearchBatchesReportAQueryErrorInTheDoneBatch(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	files, content := batchFiles(3)
	writeTree(t, dir, content)
	s := newProjectSearch(editor.Options{})
	req := projectSearchRequest(dir, files, "(")
	req.Options = searchpanel.MatchOptions{Regex: true}
	var batches []searchpanel.SearchBatch
	for batch := range s.ProjectSearchBatches(req, make(chan struct{})) {
		batches = append(batches, batch)
	}
	if len(batches) != 1 || !batches[0].Done || batches[0].Err == "" || len(batches[0].Files) != 0 {
		t.Fatalf("batches = %+v, want one done batch with the error", batches)
	}
	if _, err := s.ProjectSearch(req); err == nil {
		t.Fatal("the one-shot search did not return the query error")
	}
}

func TestProjectSearchOpenBuffersScope(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{"disk.txt": "needle on disk\n"})
	buffers := []searchpanel.SourceFile{
		{Path: filepath.Join(dir, "disk.txt"), Name: "disk.txt", Text: "needle unsaved\n", Dirty: true},
		{Path: filepath.Join(dir, "new.txt"), Name: "new.txt", Text: "needle new\n", Dirty: true},
		{Name: "[No Name]", Text: "needle unnamed\n", Dirty: true},
	}
	s := newProjectSearch(editor.Options{})
	req := projectSearchRequest(dir, []string{"disk.txt"}, "needle")
	req.Scope = searchpanel.OpenBuffers
	req.Files = []string{"disk.txt"} // ignore the disk index in this scope
	req.OpenFiles = buffers
	res, err := s.ProjectSearch(req)
	if err != nil {
		t.Fatalf("ProjectSearch: %v", err)
	}
	if len(res.Files) != 3 || !res.Files[0].Dirty || res.Files[0].Name != "disk.txt" || res.Files[1].Name != "new.txt" || res.Files[2].Name != "[No Name]" {
		t.Fatalf("open-buffer result = %+v", res.Files)
	}
	if got := res.Files[0].Matches[0].Text; got != "needle unsaved" {
		t.Fatalf("disk.txt matched %q, want the unsaved text", got)
	}
	// The replacement plans from the same Dirty snapshots.
	rep := searchpanel.ReplaceRequest{
		Root: dir, Query: "needle", Replacement: "done",
		Files: []searchpanel.ReplaceFile{
			{Path: buffers[0].Path, Name: "disk.txt", Hash: res.Files[0].Hash, Dirty: true, Text: buffers[0].Text},
			{Path: buffers[1].Path, Name: "new.txt", Hash: res.Files[1].Hash, Dirty: true, Text: buffers[1].Text},
			{Name: "[No Name]", Hash: res.Files[2].Hash, Dirty: true, Text: buffers[2].Text},
		},
		Limit: searchpanel.DefaultLimit,
	}
	out, err := s.ProjectReplace(rep)
	if err != nil {
		t.Fatalf("ProjectReplace: %v", err)
	}
	if len(out.Files) != 3 || len(out.Files[0].Edits) != 1 || len(out.Files[1].Edits) != 1 || len(out.Files[2].Edits) != 1 {
		t.Fatalf("open-buffer replacement = %+v", out.Files)
	}
	if got := out.Files[1].Edits[0].Text; got != "done" {
		t.Fatalf("replacement edit = %q", got)
	}
}

func TestProjectPreviewStartTotalAndSpans(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{"a.go": "package main\n\nfunc main() {\n\tx := \"hi\"\n}\n"})
	s := newProjectSearch(editor.Options{})
	file := filepath.Join(dir, "a.go")
	// An explicit window reports where it is in the file and highlights the syntax.
	res, err := s.Preview(searchpanel.PreviewRequest{Path: file, Start: 0, Lines: 5, Line: 3, Col: 2, EndLine: 3, EndCol: 3})
	if err != nil {
		t.Fatal(err)
	}
	if res.Total != 5 || res.Start != 0 || len(res.Lines) != 5 {
		t.Fatalf("preview window = start %d total %d lines %d", res.Start, res.Total, len(res.Lines))
	}
	if len(res.Spans) == 0 {
		t.Fatal("a Go file previewed without syntax spans")
	}
	kinds := map[string]bool{}
	for _, span := range res.Spans {
		kinds[span.Kind] = true
		if span.End <= span.Start {
			t.Fatalf("empty span: %+v", span)
		}
	}
	if !kinds["keyword"] {
		t.Fatalf("kinds = %v, want the Go keywords", kinds)
	}
	// Scrolling requests an explicit later window.
	res, err = s.Preview(searchpanel.PreviewRequest{Path: file, Start: 3, Lines: 2, Line: 3, Col: 2, EndLine: 3, EndCol: 3})
	if err != nil {
		t.Fatal(err)
	}
	if res.Start != 3 || len(res.Lines) != 2 || !strings.Contains(res.Lines[0], "x :=") {
		t.Fatalf("scrolled window = start %d lines %q", res.Start, res.Lines)
	}
	// A file with no grammar is plain text.
	writeTree(t, dir, map[string]string{"notes.zzz": "some plain text\n"})
	res, err = s.Preview(searchpanel.PreviewRequest{Path: filepath.Join(dir, "notes.zzz"), Start: 0, Lines: 4, Line: 1, Col: 1, EndLine: 1, EndCol: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Spans) != 0 {
		t.Fatalf("a grammarless file had spans: %+v", res.Spans)
	}
	// The unsaved text highlights too.
	res, err = s.Preview(searchpanel.PreviewRequest{Path: file, Text: "package main\n\nvar x = 1\n", Start: 0, Lines: 3, Line: 1, Col: 1, EndLine: 1, EndCol: 2})
	if err != nil || len(res.Spans) == 0 || res.Total != 3 {
		t.Fatalf("unsaved preview = %+v, %v", res, err)
	}
}

func TestProjectReplaceWorksInAnOpenLargeBuffer(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	big := "needle\n" + strings.Repeat("x\n", 600_000) + "needle\n"
	if len(big) <= pluginhost.GrepMaxFileBytes {
		t.Fatalf("the test buffer is %d bytes, want over the read cap", len(big))
	}
	buffer := searchpanel.SourceFile{Path: filepath.Join(dir, "big.txt"), Name: "big.txt", Text: big, Dirty: true}
	s := newProjectSearch(editor.Options{})
	req := projectSearchRequest(dir, nil, "needle")
	req.Scope = searchpanel.OpenBuffers
	req.OpenFiles = []searchpanel.SourceFile{buffer}
	res, err := s.ProjectSearch(req)
	if err != nil || len(res.Files) != 1 || len(res.Files[0].Matches) != 2 {
		t.Fatalf("open large buffer search = %+v, %v", res.Files, err)
	}
	rep := searchpanel.ReplaceRequest{
		Root: dir, Query: "needle", Replacement: "done",
		Files: []searchpanel.ReplaceFile{{
			Path: buffer.Path, Name: "big.txt", Hash: res.Files[0].Hash, Dirty: true, Text: big,
		}},
		Limit: searchpanel.DefaultLimit,
	}
	out, err := s.ProjectReplace(rep)
	if err != nil || len(out.Files) != 1 || len(out.Files[0].Edits) != 2 {
		t.Fatalf("open large buffer replacement = %+v, %v", out.Files, err)
	}
	if out.Files[0].Edits[0].From.Line != 0 || out.Files[0].Edits[1].From.Line != 600_001 {
		t.Fatalf("edits = %+v, want the two needles", out.Files[0].Edits)
	}
	// A closed file over the read cap stays refused with its reason.
	writeTree(t, dir, map[string]string{"big.txt": big})
	res, err = s.ProjectSearch(projectSearchRequest(dir, []string{"big.txt"}, "needle"))
	if err != nil || len(res.Files) != 0 || !strings.Contains(strings.Join(res.Skipped, ", "), "large file") {
		t.Fatalf("closed large file = %+v, %v; want it skipped as a large file", res, err)
	}
}

func TestProjectReplaceMultiLineRangeIsExact(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{"m.txt": "alpha\nbeta\ngamma\ndelta\n"})
	s := newProjectSearch(editor.Options{})
	req := projectSearchRequest(dir, []string{"m.txt"}, "beta\ngamma")
	req.Options = searchpanel.MatchOptions{Regex: true}
	res, err := s.ProjectSearch(req)
	if err != nil || len(res.Files) != 1 || len(res.Files[0].Matches) != 1 {
		t.Fatalf("multi-line search = %+v, %v", res.Files, err)
	}
	m := res.Files[0].Matches[0]
	if m.Line != 2 || m.Col != 1 || m.EndLine != 3 || m.EndCol != 6 {
		t.Fatalf("match = %d:%d-%d:%d, want 2:1-3:6", m.Line, m.Col, m.EndLine, m.EndCol)
	}
	rep := searchpanel.ReplaceRequest{
		Root: dir, Query: "beta\ngamma", Replacement: "X",
		Options: searchpanel.MatchOptions{Regex: true},
		Files: []searchpanel.ReplaceFile{{
			Path: res.Files[0].Path, Name: "m.txt", Hash: res.Files[0].Hash,
			Selected: []searchpanel.Position{{Line: m.Line - 1, Col: m.Col - 1}},
		}},
		Limit: searchpanel.DefaultLimit,
	}
	out, err := s.ProjectReplace(rep)
	if err != nil || len(out.Files) != 1 || len(out.Files[0].Edits) != 1 {
		t.Fatalf("multi-line replacement = %+v, %v", out.Files, err)
	}
	e := out.Files[0].Edits[0]
	if e.From.Line != 1 || e.From.Col != 0 || e.To.Line != 2 || e.To.Col != 5 || e.Text != "X" {
		t.Fatalf("edit = %+v, want 1:0-2:5 replacing the exact range", e)
	}
}
