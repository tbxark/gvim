package app

import (
	"os/exec"
	"path/filepath"
	"slices"
	"testing"

	"github.com/tbxark/gvim/internal/tui/searchpanel"
)

// gitTestRun runs one git command in dir and fails the test on an error.
func gitTestRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
}

// gitRepo makes dir a repository with its files added, so git grep sees them.
func gitRepo(t *testing.T, dir string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git binary not available")
	}
	gitTestRun(t, dir, "init", "-q")
	gitTestRun(t, dir, "add", "-A")
}

func TestGitGrepFindsTrackedFiles(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		"a.txt":     "one\nfoo bar\n",
		"sub/b.txt": "xfoo\n",
	})
	gitRepo(t, dir)
	// A file created after the add is untracked and must not match.
	writeTree(t, dir, map[string]string{"untracked.txt": "foo\n"})
	res, err := gitGrep{}.GitGrep(searchpanel.GitRequest{Root: dir, Query: "foo", Limit: 100})
	if err != nil {
		t.Fatalf("GitGrep: %v", err)
	}
	if len(res.Files) != 2 {
		t.Fatalf("files = %+v, want a.txt and sub/b.txt", res.Files)
	}
	if res.Files[0].Path != filepath.Join(dir, "a.txt") || res.Files[0].Name != "a.txt" {
		t.Fatalf("first file = %+v", res.Files[0])
	}
	if len(res.Files[0].Matches) != 1 || res.Files[0].Matches[0].Line != 2 {
		t.Fatalf("a.txt matches = %+v", res.Files[0].Matches)
	}
}

func TestGitGrepCaseAndExitCodes(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{"a.txt": "FOO\nfoo\n"})
	gitRepo(t, dir)
	res, err := gitGrep{}.GitGrep(searchpanel.GitRequest{Root: dir, Query: "FOO", Limit: 100})
	if err != nil || len(res.Files) != 1 || len(res.Files[0].Matches) != 2 {
		t.Fatalf("folded search = %+v, %v; want both lines", res, err)
	}
	res, err = gitGrep{}.GitGrep(searchpanel.GitRequest{Root: dir, Query: "FOO", CaseSensitive: true, Limit: 100})
	if err != nil || len(res.Files) != 1 || len(res.Files[0].Matches) != 1 {
		t.Fatalf("case-sensitive search = %+v, %v; want the FOO line", res, err)
	}
	// git grep exits 1 for no matches: that is a successful empty result.
	res, err = gitGrep{}.GitGrep(searchpanel.GitRequest{Root: dir, Query: "zzz-none", Limit: 100})
	if err != nil || len(res.Files) != 0 {
		t.Fatalf("no matches = %+v, %v; want an empty success", res, err)
	}
}

func TestGitGrepQueryWithSpacesAndQuotes(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{"a.txt": "say \"hi there\" now\n"})
	gitRepo(t, dir)
	res, err := gitGrep{}.GitGrep(searchpanel.GitRequest{Root: dir, Query: `"hi there`, Limit: 100})
	if err != nil {
		t.Fatalf("GitGrep: %v", err)
	}
	if len(res.Files) != 1 || len(res.Files[0].Matches) != 1 {
		t.Fatalf("quoted query = %+v, want the line", res.Files)
	}
}

func TestGitGrepOutsideARepositoryIsAnError(t *testing.T) {
	t.Parallel()
	if _, err := (gitGrep{}).GitGrep(searchpanel.GitRequest{Root: t.TempDir(), Query: "foo", Limit: 100}); err == nil {
		t.Fatal("a workspace outside a git repository gave no error")
	}
}

func TestGitGrepPathspecs(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		"a.go":      "needle\n",
		"sub/b.go":  "needle\n",
		"sub/c.txt": "needle\n",
		"d.txt":     "needle\n",
	})
	gitRepo(t, dir)
	tests := []struct {
		name      string
		pathspecs []string
		want      []string
	}{
		{"no pathspecs search every tracked file", nil, []string{"a.go", "d.txt", "sub/b.go", "sub/c.txt"}},
		{"a basename glob matches at any depth", []string{"*.go"}, []string{"a.go", "sub/b.go"}},
		{"a directory pathspec keeps its subtree", []string{"sub/**"}, []string{"sub/b.go", "sub/c.txt"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := gitGrep{}.GitGrep(searchpanel.GitRequest{Root: dir, Query: "needle", Pathspecs: tt.pathspecs, Limit: 100})
			if err != nil {
				t.Fatalf("GitGrep: %v", err)
			}
			var got []string
			for _, f := range res.Files {
				got = append(got, f.Name)
			}
			if !slices.Equal(got, tt.want) {
				t.Fatalf("files = %v, want %v", got, tt.want)
			}
		})
	}
}
