package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/tbxark/gvim/internal/tui/searchpanel"
	"github.com/tbxark/gvim/plugin/hostos"
)

// TestRemoteProjectSearchKeepsStdoutOnToolExitError: a tool that prints matches then exits 2
// is reported as an incomplete search, not as "no results": the parsed files stay in the
// stream and the Done batch carries the error.
func TestRemoteProjectSearchKeepsStdoutOnToolExitError(t *testing.T) {
	one := &fakeOneShot{script: func(_ context.Context, argv []string, _ hostos.ProcOptions) (hostos.ProcResult, error) {
		joined := strings.Join(argv, " ")
		switch {
		case len(argv) >= 2 && argv[0] == "sh":
			return hostos.ProcResult{Stdout: []byte("rg")}, nil
		case strings.Contains(joined, "rev-parse"):
			return hostos.ProcResult{Stdout: []byte("true")}, nil
		case strings.Contains(joined, "ls-files"):
			return hostos.ProcResult{Stdout: []byte(gitLsEntry("100644", "a.go"))}, nil
		case len(argv) > 0 && argv[0] == "rg":
			return hostos.ProcResult{Stdout: []byte("a.go:1:1:todo\n"), Code: 2}, nil
		}
		return hostos.ProcResult{}, errors.New("unexpected command: " + joined)
	}}
	s := newRemoteProjectSearch(one, "/remote/w").withFiles(newRemoteFS())
	var files []string
	var doneErr string
	for _, b := range collectRemoteBatches(t, s.ProjectSearchBatches(searchpanel.SearchRequest{
		Root:  "/remote/w",
		Query: "todo",
		Scope: searchpanel.AllFiles,
	}, nil)) {
		for _, f := range b.Files {
			files = append(files, f.Name)
		}
		if b.Done {
			doneErr = b.Err
		}
	}
	if len(files) != 1 || files[0] != "a.go" {
		t.Fatalf("files = %v, want the stdout match kept", files)
	}
	if !strings.Contains(doneErr, "failed on the remote host (exit 2)") {
		t.Fatalf("done error = %q, want the exit-2 error", doneErr)
	}
}
