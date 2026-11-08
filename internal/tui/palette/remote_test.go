package palette

import (
	"errors"
	"path/filepath"
	"testing"
)

// remoteErrorFiles is a Files with an optional error and truncation flag.
type remoteErrorFiles struct {
	files     []string
	loading   bool
	err       error
	truncated bool
}

func (f remoteErrorFiles) Snapshot() ([]string, bool) { return f.files, f.loading }
func (f remoteErrorFiles) Err() error                 { return f.err }
func (f remoteErrorFiles) Truncated() bool            { return f.truncated }

// remoteEmptyRanker returns no matches.
type remoteEmptyRanker struct{}

func (remoteEmptyRanker) Rank(string, []string, int) []Ranked { return nil }

// TestRemoteFileRowJoinsWithSlashes checks a remote row's path keeps the remote's
// slash-separated form (a local filepath.Join would use the client's separator).
func TestRemoteFileRowJoinsWithSlashes(t *testing.T) {
	r := row{kind: rowFile, rel: "src/main.go", line: 2, col: 3}
	remoteCmd := r.command("/remote/w", true)
	if remoteCmd == nil {
		t.Fatal("no remote open command")
	}
	remote, ok := remoteCmd().(OpenFileMsg)
	if !ok || remote.Path != "/remote/w/src/main.go" {
		t.Fatalf("remote message = %#v", remoteCmd())
	}
	local, ok := r.command("/local/w", false)().(OpenFileMsg)
	if !ok || local.Path != filepath.Join("/local/w", "src/main.go") {
		t.Fatalf("local path = %q", local.Path)
	}
}

// TestFileRowsSurfaceErrorAndTruncation checks the file mode distinguishes an index error
// and a truncated list from a complete empty result.
func TestFileRowsSurfaceErrorAndTruncation(t *testing.T) {
	rows := fileRows(nil, false, 0, 0, "connection down", false)
	if len(rows) != 1 || rows[0].kind != rowMessage || rows[0].name != "File search failed: connection down" {
		t.Fatalf("error rows = %+v", rows)
	}
	rows = fileRows([]Ranked{{Path: "a.go"}}, false, 0, 0, "", true)
	if len(rows) != 2 || rows[1].kind != rowMessage || rows[1].name == "" {
		t.Fatalf("truncated rows = %+v", rows)
	}
	if rows := fileRows(nil, false, 0, 0, "", false); len(rows) != 0 {
		t.Fatalf("complete empty rows = %+v", rows)
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
