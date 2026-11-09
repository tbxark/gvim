package palette

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

// remoteErrorFiles is a Files with an optional error and truncation flag.
type remoteErrorFiles struct {
	files     []string
	loading   bool
	err       error
	truncated bool
	notice    string
}

func (f remoteErrorFiles) Snapshot() ([]string, bool) { return f.files, f.loading }
func (f remoteErrorFiles) Err() error                 { return f.err }
func (f remoteErrorFiles) Truncated() bool            { return f.truncated }
func (f remoteErrorFiles) Notice() string             { return f.notice }

// remoteEmptyRanker returns no matches.
type remoteEmptyRanker struct{}

func (remoteEmptyRanker) Rank(string, []string, int) []Ranked { return nil }

// TestRemoteFileRowJoinsWithSlashes checks a remote row's path keeps the remote's
// slash-separated form (a local filepath.Join would use the client's separator).
func TestRemoteFileRowJoinsWithSlashes(t *testing.T) {
	r := row{kind: rowFile, rel: "src/main.go", line: 2, col: 3}
	remoteCmd := r.command("/remote/w", true, Owner{})
	if remoteCmd == nil {
		t.Fatal("no remote open command")
	}
	remote, ok := remoteCmd().(OpenFileMsg)
	if !ok || remote.Path != "/remote/w/src/main.go" {
		t.Fatalf("remote message = %#v", remoteCmd())
	}
	local, ok := r.command("/local/w", false, Owner{})().(OpenFileMsg)
	if !ok || local.Path != filepath.Join("/local/w", "src/main.go") {
		t.Fatalf("local path = %q", local.Path)
	}
}

// TestFileRowsSurfaceErrorAndTruncation checks the file mode distinguishes an index error
// and a truncated list from a complete empty result.
func TestFileRowsSurfaceErrorAndTruncation(t *testing.T) {
	rows := fileRows(nil, false, 0, 0, "connection down", false, "")
	if len(rows) != 1 || rows[0].kind != rowMessage || rows[0].name != "File search failed: connection down" {
		t.Fatalf("error rows = %+v", rows)
	}
	rows = fileRows([]Ranked{{Path: "a.go"}}, false, 0, 0, "", true, "")
	if len(rows) != 2 || rows[1].kind != rowMessage || rows[1].name == "" {
		t.Fatalf("truncated rows = %+v", rows)
	}
	if rows := fileRows(nil, false, 0, 0, "", false, ""); len(rows) != 0 {
		t.Fatalf("complete empty rows = %+v", rows)
	}
	// A non-fatal index notice is one informative row after the results; it never
	// replaces or disables them.
	rows = fileRows([]Ranked{{Path: "a.go"}}, false, 0, 0, "", false, "Remote quick-open: using a directory walk")
	if len(rows) != 2 || rows[0].kind != rowFile || rows[1].kind != rowMessage || !strings.Contains(rows[1].name, "using a directory walk") {
		t.Fatalf("notice rows = %+v", rows)
	}
}

// TestRankCmdSurfacesIndexErrorAndTruncation checks the ranked message carries the index
// error and truncation once loading finished.
func TestRankCmdSurfacesIndexErrorAndTruncation(t *testing.T) {
	p := New(Options{
		Files:  remoteErrorFiles{files: []string{"a.go"}, err: errors.New("remote gone"), truncated: true},
		Ranker: remoteEmptyRanker{},
	})
	cmd := p.rankCmd(1, "")
	if cmd == nil {
		t.Fatal("rankCmd returned nil")
	}
	msg, ok := cmd().(FileRankedMsg)
	if !ok {
		t.Fatalf("message = %#v", cmd())
	}
	if msg.Loading || msg.ErrText != "remote gone" || !msg.Truncated {
		t.Fatalf("file ranked message = %+v", msg)
	}
	p = New(Options{Files: remoteErrorFiles{files: []string{"a.go"}, notice: "using a directory walk"}, Ranker: remoteEmptyRanker{}})
	msg = p.rankCmd(2, "")().(FileRankedMsg)
	if msg.Notice != "using a directory walk" {
		t.Fatalf("file ranked notice = %q", msg.Notice)
	}
}

// recordingPreview records the paths the palette preview reads.
type recordingPreview struct{ paths []string }

func (r *recordingPreview) ReadHead(path string, _ int) ([]byte, bool, error) {
	r.paths = append(r.paths, path)
	return []byte("package a\n"), false, nil
}

// TestRemotePreviewReadsTheRemotePath checks a remote palette's preview reader receives the
// remote absolute path (slash-joined), not a local filepath.Join of the client.
func TestRemotePreviewReadsTheRemotePath(t *testing.T) {
	t.Parallel()
	pv := &recordingPreview{}
	p := New(Options{Query: "", Root: "/remote/w", Remote: true, Files: &fakeFiles{files: []string{"src/a.go"}}, Ranker: &fakeRanker{}, Preview: pv})
	settle(t, p, p.Init(), 8)
	if len(pv.paths) == 0 || pv.paths[0] != "/remote/w/src/a.go" {
		t.Fatalf("preview paths = %v, want the remote path", pv.paths)
	}
}

// remoteWindowPreviewer is a fake previewer exposing the bounded-window seam.
type remoteWindowPreviewer struct {
	paths  []string
	lines  []int
	frames int
}

func (p *remoteWindowPreviewer) ReadHead(string, int) ([]byte, bool, error) {
	return []byte("head\n"), false, nil
}

func (p *remoteWindowPreviewer) ReadWindow(_ context.Context, path string, line, _ int) ([]byte, int, bool, error) {
	p.paths = append(p.paths, path)
	p.lines = append(p.lines, line)
	p.frames++
	return []byte("line 10\nline 11\nline 12 target\nline 13\n"), 10, false, nil
}

// TestRemoteQuickOpenLineTargetUsesWindowPreview: a remote path:line query previews around
// the target line through WindowPreviewer, instead of only reading the file head.
func TestRemoteQuickOpenLineTargetUsesWindowPreview(t *testing.T) {
	pv := &remoteWindowPreviewer{}
	files := &fakeFiles{files: []string{"src/a.go"}}
	p := New(Options{
		Query:   "src/a.go:12",
		Root:    "/remote/w",
		Remote:  true,
		Files:   files,
		Ranker:  &fakeRanker{},
		Preview: pv,
	})
	settle(t, p, p.Init(), 8)
	if len(pv.paths) != 1 || pv.paths[0] != "/remote/w/src/a.go" || pv.lines[0] != 12 {
		t.Fatalf("window preview = paths %v lines %v, want /remote/w/src/a.go at 12", pv.paths, pv.lines)
	}
	joined := strings.Join(shownPreview(t, p), "\n")
	if !strings.Contains(joined, "12 │ line 12 target") {
		t.Fatalf("preview does not show the target line:\n%s", joined)
	}
}
