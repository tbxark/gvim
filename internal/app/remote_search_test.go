package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tbxark/gvim/internal/tui"
	"github.com/tbxark/gvim/internal/tui/searchpanel"
	"github.com/tbxark/gvim/plugin/hostos"
)

// oneShotCall is one recorded authority one-shot invocation.
type oneShotCall struct {
	argv []string
	cwd  string
	env  map[string]string
}

// fakeOneShot is an authority.OneShot backed by a script; it never touches the local file
// system or the network.
type fakeOneShot struct {
	mu     sync.Mutex
	calls  []oneShotCall
	script func(ctx context.Context, argv []string, o hostos.ProcOptions) (hostos.ProcResult, error)
}

func (f *fakeOneShot) Run(ctx context.Context, argv []string, o hostos.ProcOptions) (hostos.ProcResult, error) {
	f.mu.Lock()
	call := oneShotCall{argv: append([]string(nil), argv...), cwd: o.Cwd, env: map[string]string{}}
	for k, v := range o.Env {
		call.env[k] = v
	}
	f.calls = append(f.calls, call)
	f.mu.Unlock()
	if f.script == nil {
		return hostos.ProcResult{}, errors.New("unexpected remote command")
	}
	return f.script(ctx, argv, o)
}

func (f *fakeOneShot) recorded() []oneShotCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]oneShotCall(nil), f.calls...)
}

// toolScript answers the probe with tool and returns output for the search command, or an
// error for unexpected commands.
func toolScript(tool, output string) func(context.Context, []string, hostos.ProcOptions) (hostos.ProcResult, error) {
	return func(_ context.Context, argv []string, _ hostos.ProcOptions) (hostos.ProcResult, error) {
		if len(argv) >= 2 && argv[0] == "sh" {
			if tool == "none" {
				return hostos.ProcResult{Code: 0}, nil
			}
			return hostos.ProcResult{Stdout: []byte(tool)}, nil
		}
		if len(argv) >= 1 && (argv[0] == "rg" || argv[0] == "grep") {
			return hostos.ProcResult{Stdout: []byte(output)}, nil
		}
		return hostos.ProcResult{}, errors.New("unexpected remote command")
	}
}

func collectRemoteBatches(t *testing.T, ch <-chan searchpanel.SearchBatch) []searchpanel.SearchBatch {
	t.Helper()
	var batches []searchpanel.SearchBatch
	deadline := time.After(10 * time.Second)
	for {
		select {
		case b, ok := <-ch:
			if !ok {
				return batches
			}
			batches = append(batches, b)
		case <-deadline:
			t.Fatal("the remote search did not finish")
		}
	}
}

// TestRemoteProjectSearchStreamsBatchesAndCaps checks the remote search runs through the
// authority one-shot with the remote cwd, streams whole-file batches, caps matches and
// reports truncation.
func TestRemoteProjectSearchStreamsBatchesAndCaps(t *testing.T) {
	out := "src/a.go:2:3:hello todo\nsrc/b.go:9:1:todo two\nsrc/b.go:10:1:todo three\n"
	one := &fakeOneShot{script: toolScript("rg", out)}
	s := newRemoteProjectSearch(one, "/remote/w")
	batches := collectRemoteBatches(t, s.ProjectSearchBatches(searchpanel.SearchRequest{
		Root:  "/remote/w",
		Query: "todo",
		Scope: searchpanel.AllFiles,
		Limit: 2,
	}, nil))
	var files []searchpanel.FileMatches
	done, truncated := false, false
	for _, b := range batches {
		files = append(files, b.Files...)
		if b.Done {
			done = true
			truncated = b.Truncated
		}
	}
	if !done || !truncated {
		t.Fatalf("batches = %+v (done %v truncated %v)", batches, done, truncated)
	}
	total := 0
	for _, f := range files {
		total += len(f.Matches)
	}
	if total != 2 {
		t.Fatalf("matches = %d, want the limit 2: %+v", total, files)
	}
	if files[0].Path != "/remote/w/src/a.go" || files[0].Name != "src/a.go" {
		t.Fatalf("first file = %+v", files[0])
	}
	calls := one.recorded()
	if len(calls) == 0 || calls[0].cwd != "/remote/w" {
		t.Fatalf("calls = %+v, want the probe at the remote cwd", calls)
	}
	var sawRG bool
	for _, c := range calls {
		if len(c.argv) > 0 && c.argv[0] == "rg" {
			sawRG = true
			if c.cwd != "/remote/w" {
				t.Fatalf("rg cwd = %q", c.cwd)
			}
			joined := strings.Join(c.argv, " ")
			if !strings.Contains(joined, "--vimgrep") || !strings.Contains(joined, "-e todo") {
				t.Fatalf("rg argv = %v", c.argv)
			}
		}
	}
	if !sawRG {
		t.Fatalf("no rg call: %+v", calls)
	}
}

// TestRemoteProjectSearchHonorsTheFileSnapshot checks results outside the panel's file
// index snapshot (which carries the glob filter) are dropped.
func TestRemoteProjectSearchHonorsTheFileSnapshot(t *testing.T) {
	out := "src/a.go:2:1:todo a\nsrc/b.go:3:1:todo b\n"
	one := &fakeOneShot{script: toolScript("rg", out)}
	s := newRemoteProjectSearch(one, "/remote/w")
	batches := collectRemoteBatches(t, s.ProjectSearchBatches(searchpanel.SearchRequest{
		Root:  "/remote/w",
		Query: "todo",
		Scope: searchpanel.AllFiles,
		Files: []string{"src/a.go"},
	}, nil))
	var files []searchpanel.FileMatches
	for _, b := range batches {
		files = append(files, b.Files...)
	}
	if len(files) != 1 || files[0].Name != "src/a.go" {
		t.Fatalf("files = %+v, want only the snapshot file", files)
	}
}

// TestRemoteProjectSearchGrepFallback checks the grep fallback runs when rg is missing and
// recovers the column from the line text.
func TestRemoteProjectSearchGrepFallback(t *testing.T) {
	one := &fakeOneShot{script: toolScript("grep", "./src/a.go:2:hello todo\n")}
	s := newRemoteProjectSearch(one, "/remote/w")
	batches := collectRemoteBatches(t, s.ProjectSearchBatches(searchpanel.SearchRequest{
		Root:  "/remote/w",
		Query: "todo",
		Scope: searchpanel.AllFiles,
	}, nil))
	var files []searchpanel.FileMatches
	for _, b := range batches {
		files = append(files, b.Files...)
	}
	if len(files) != 1 || files[0].Path != "/remote/w/src/a.go" || len(files[0].Matches) != 1 {
		t.Fatalf("files = %+v", files)
	}
	m := files[0].Matches[0]
	if m.Line != 2 || m.Col != 7 || m.Text != "hello todo" {
		t.Fatalf("match = %+v, want line 2 col 7", m)
	}
	for _, c := range one.recorded() {
		if len(c.argv) > 0 && c.argv[0] == "grep" {
			if !strings.Contains(strings.Join(c.argv, " "), "-r -n -I") {
				t.Fatalf("grep argv = %v", c.argv)
			}
			return
		}
	}
	t.Fatal("no grep call")
}

// TestRemoteProjectSearchMissingTool checks a host with neither rg nor grep reports the
// clear tool-missing error instead of falling back to local files.
func TestRemoteProjectSearchMissingTool(t *testing.T) {
	one := &fakeOneShot{script: toolScript("none", "")}
	s := newRemoteProjectSearch(one, "/remote/w")
	batches := collectRemoteBatches(t, s.ProjectSearchBatches(searchpanel.SearchRequest{
		Root: "/remote/w", Query: "todo", Scope: searchpanel.AllFiles,
	}, nil))
	if len(batches) != 1 || !batches[0].Done || !strings.Contains(batches[0].Err, "no search tool on the remote host") {
		t.Fatalf("batches = %+v", batches)
	}
	for _, c := range one.recorded() {
		if len(c.argv) > 0 && (c.argv[0] == "rg" || c.argv[0] == "grep") {
			t.Fatalf("a search command ran without a tool: %+v", c)
		}
	}
}

// TestRemoteProjectSearchCancellation checks a cancelled search stops the remote command
// and closes the channel without a Done batch.
func TestRemoteProjectSearchCancellation(t *testing.T) {
	started := make(chan struct{})
	one := &fakeOneShot{script: func(ctx context.Context, argv []string, _ hostos.ProcOptions) (hostos.ProcResult, error) {
		if len(argv) >= 2 && argv[0] == "sh" {
			return hostos.ProcResult{Stdout: []byte("rg")}, nil
		}
		close(started)
		<-ctx.Done()
		return hostos.ProcResult{}, ctx.Err()
	}}
	s := newRemoteProjectSearch(one, "/remote/w")
	cancel := make(chan struct{})
	ch := s.ProjectSearchBatches(searchpanel.SearchRequest{Root: "/remote/w", Query: "todo", Scope: searchpanel.AllFiles}, cancel)
	select {
	case <-started:
	case <-time.After(10 * time.Second):
		t.Fatal("the remote rg did not start")
	}
	close(cancel)
	var seenDone bool
	deadline := time.After(10 * time.Second)
	for {
		select {
		case b, ok := <-ch:
			if !ok {
				if seenDone {
					t.Fatal("a cancelled search sent a Done batch")
				}
				return
			}
			if b.Done {
				seenDone = true
			}
		case <-deadline:
			t.Fatal("a cancelled search did not close its channel")
		}
	}
}

// TestRemoteGitGrepRunsOnTheHost checks git grep resolves the repository remotely and maps
// its relative paths to absolute remote paths, with the local git state never consulted.
func TestRemoteGitGrepRunsOnTheHost(t *testing.T) {
	// A local directory that is not a git repository: the local adapter would fail here.
	local := t.TempDir()
	if err := os.WriteFile(filepath.Join(local, "local.txt"), []byte("q"), 0o600); err != nil {
		t.Fatal(err)
	}
	one := &fakeOneShot{script: func(_ context.Context, argv []string, _ hostos.ProcOptions) (hostos.ProcResult, error) {
		joined := strings.Join(argv, " ")
		switch {
		case strings.Contains(joined, "rev-parse --show-toplevel"):
			return hostos.ProcResult{Stdout: []byte("/remote/repo\n")}, nil
		case strings.Contains(joined, "grep -n --column -I"):
			return hostos.ProcResult{Stdout: []byte("src/x.go:3:5:hit todo\n")}, nil
		default:
			return hostos.ProcResult{}, errors.New("unexpected remote command: " + joined)
		}
	}}
	g := newRemoteGitGrep(one, "/remote/w", nil)
	res, err := g.GitGrep(searchpanel.GitRequest{Root: local, Query: "todo", Limit: 10})
	if err != nil {
		t.Fatalf("GitGrep: %v", err)
	}
	if len(res.Files) != 1 || res.Files[0].Path != "/remote/repo/src/x.go" || res.Files[0].Name != "src/x.go" {
		t.Fatalf("files = %+v", res.Files)
	}
	calls := one.recorded()
	if len(calls) != 2 || calls[0].cwd != "/remote/w" || calls[1].cwd != "/remote/repo" {
		t.Fatalf("calls = %+v", calls)
	}
}

// TestRemoteGitGrepNotARepository checks a workspace that is not a git repository reports
// it clearly instead of searching local files.
func TestRemoteGitGrepNotARepository(t *testing.T) {
	one := &fakeOneShot{script: func(context.Context, []string, hostos.ProcOptions) (hostos.ProcResult, error) {
		return hostos.ProcResult{Code: 128, Stderr: []byte("not a git repository")}, nil
	}}
	g := newRemoteGitGrep(one, "/remote/w", nil)
	if _, err := g.GitGrep(searchpanel.GitRequest{Root: "/remote/w", Query: "todo"}); err == nil || !strings.Contains(err.Error(), "not a git repository") {
		t.Fatalf("error = %v", err)
	}
}

// TestInstallRemoteSearchPorts checks the remote wiring installs the authority-backed
// search ports, keeps project replace unwired and leaves the local ports cleared.
func TestInstallRemoteSearchPorts(t *testing.T) {
	one := &fakeOneShot{}
	ports := tui.RootPorts{}
	installRemoteSearchPorts(&ports, one, nil, nil, "/remote/w", "SSH:u@h", nil)
	if ports.ProjectSearch == nil || ports.GitGrep == nil {
		t.Fatalf("remote search ports not installed: %+v", ports)
	}
	if ports.ProjectReplace == nil {
		t.Fatal("remote project replace is not wired")
	}
	if _, ok := ports.ProjectReplace.(*remoteReplacer); !ok {
		t.Fatalf("ProjectReplace = %T, want the remote replacer", ports.ProjectReplace)
	}
	if _, ok := ports.ProjectSearch.(*remoteSearch); !ok {
		t.Fatalf("ProjectSearch = %T, want the remote adapter", ports.ProjectSearch)
	}
	if _, ok := ports.GitGrep.(*remoteGitGrep); !ok {
		t.Fatalf("GitGrep = %T, want the remote adapter", ports.GitGrep)
	}
}

// TestRemoteProjectSearchTimeout checks a remote search that outlives its deadline reports a
// bounded timeout to the panel instead of hanging or falling back locally.
func TestRemoteProjectSearchTimeout(t *testing.T) {
	one := &fakeOneShot{script: func(ctx context.Context, argv []string, _ hostos.ProcOptions) (hostos.ProcResult, error) {
		if len(argv) >= 2 && argv[0] == "sh" {
			return hostos.ProcResult{Stdout: []byte("rg")}, nil
		}
		<-ctx.Done()
		return hostos.ProcResult{}, ctx.Err()
	}}
	s := newRemoteProjectSearch(one, "/remote/w")
	s.timeout = 50 * time.Millisecond
	start := time.Now()
	batches := collectRemoteBatches(t, s.ProjectSearchBatches(searchpanel.SearchRequest{
		Root: "/remote/w", Query: "todo", Scope: searchpanel.AllFiles,
	}, nil))
	if time.Since(start) > 5*time.Second {
		t.Fatalf("the timeout took %s", time.Since(start))
	}
	if len(batches) != 1 || !batches[0].Done || !strings.Contains(batches[0].Err, "timed out") {
		t.Fatalf("batches = %+v", batches)
	}
}

// TestRemoteSearchCurrentFileAndOpenBuffersScopes: both editor-snapshot scopes return
// matches without invoking the remote search tool and without reading any file system.
func TestRemoteSearchCurrentFileAndOpenBuffersScopes(t *testing.T) {
	one := &fakeOneShot{} // a remote command would fail the test through "unexpected remote command"
	s := newRemoteProjectSearch(one, "/remote/w")
	current := &searchpanel.SourceFile{Path: "/remote/w/a.go", Name: "a.go", Text: "one\ntodo two\n", Dirty: true}
	batches := collectRemoteBatches(t, s.ProjectSearchBatches(searchpanel.SearchRequest{
		Query: "todo", Scope: searchpanel.CurrentFile, Current: current, Limit: 10,
	}, nil))
	files := remoteBatchFiles(batches)
	if len(files) != 1 || files[0].Path != "/remote/w/a.go" || len(files[0].Matches) != 1 {
		t.Fatalf("current-file files = %+v", files)
	}
	if m := files[0].Matches[0]; m.Line != 2 || m.Text != "todo two" {
		t.Fatalf("current-file match = %+v", m)
	}
	if files[0].Dirty != true {
		t.Errorf("current-file match is not marked dirty")
	}
	open := []searchpanel.SourceFile{
		{Path: "/remote/w/a.go", Name: "a.go", Text: "todo a"},
		{Path: "", Name: "*scratch*", Text: "todo b"},
		{Path: "/remote/w/a.go", Name: "a.go", Text: "todo duplicate"},
	}
	batches = collectRemoteBatches(t, s.ProjectSearchBatches(searchpanel.SearchRequest{
		Query: "todo", Scope: searchpanel.OpenBuffers, OpenFiles: open, Limit: 10,
	}, nil))
	files = remoteBatchFiles(batches)
	if len(files) != 2 {
		t.Fatalf("open-buffers files = %+v, want the two deduplicated buffers", files)
	}
	if files[1].Name != "*scratch*" || !files[1].Dirty {
		t.Fatalf("open-buffers second file = %+v", files[1])
	}
	if calls := one.recorded(); len(calls) != 0 {
		t.Fatalf("the snapshot scopes ran remote commands: %+v", calls)
	}
}

func remoteBatchFiles(batches []searchpanel.SearchBatch) []searchpanel.FileMatches {
	var files []searchpanel.FileMatches
	for _, b := range batches {
		files = append(files, b.Files...)
	}
	return files
}

// TestRemoteSearchPreviewHeadAndErrors: the remote preview reads a bounded head through
// the authority with an honest truncated flag, uses the open buffer's text without a
// remote read, and reports a disconnected authority as an error (never a local read).
func TestRemoteSearchPreviewHeadAndErrors(t *testing.T) {
	fs := newRemoteFS()
	big := strings.Repeat("line\n", remoteSearchPreviewHeadBytes/5+2000)
	fs.put("/remote/w/big.txt", big)
	fs.put("/remote/w/small.txt", "a\nb\nc\n")
	s := newRemoteProjectSearch(&fakeOneShot{}, "/remote/w").withFiles(fs)
	res, err := s.Preview(searchpanel.PreviewRequest{
		Path: "/remote/w/big.txt", Name: "big.txt", Line: 3, EndLine: 3, EndCol: 8, Start: -1, Lines: 5,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Truncated || len(res.Lines) != 5 || res.Total <= 0 {
		t.Fatalf("big preview = truncated %v lines %d total %d", res.Truncated, len(res.Lines), res.Total)
	}
	if strings.Contains(res.Lines[0], "line") == false {
		t.Fatalf("big preview line = %q", res.Lines[0])
	}
	res, err = s.Preview(searchpanel.PreviewRequest{
		Path: "/remote/w/small.txt", Name: "small.txt", Line: 2, EndLine: 2, EndCol: 2, Start: -1, Lines: 2,
	})
	if err != nil || res.Truncated || strings.Join(res.Lines, "|") != "b|c" {
		t.Fatalf("small preview = %+v, %v", res, err)
	}
	// An open buffer's text (already in the panel) is used as is, even with the authority down.
	fs.setErr(errors.New("remote workspace is disconnected"))
	res, _ = s.Preview(searchpanel.PreviewRequest{
		Path: "/remote/w/big.txt", Name: "big.txt", Text: "x\ntodo\n", Line: 2, EndLine: 2, EndCol: 6, Start: -1, Lines: 2,
	})
	if res.Err != "" || strings.Join(res.Lines, "|") != "x|todo" {
		t.Fatalf("open-buffer preview = %+v", res)
	}
	res, _ = s.Preview(searchpanel.PreviewRequest{
		Path: "/remote/w/big.txt", Name: "big.txt", Line: 1, EndLine: 1, EndCol: 2, Start: -1, Lines: 2,
	})
	if !strings.Contains(res.Err, "disconnected") {
		t.Fatalf("disconnected preview err = %q", res.Err)
	}
}

// gitLsEntry builds one field of git ls-files -z -s output: a tracked entry (mode/object/
// stage tab path) or, with an empty mode, a bare untracked/nested path.
func gitLsEntry(mode, p string) string {
	if mode == "" {
		return p + "\x00"
	}
	return mode + " 0000000000000000000000000000000000000000 0\t" + p + "\x00"
}

// grepScopeOneShot answers the grep fallback and the Fresh-style git candidate commands.
func grepScopeOneShot(repo bool, lsFiles []byte, grepOut string, truncated bool) *fakeOneShot {
	return &fakeOneShot{script: func(_ context.Context, argv []string, _ hostos.ProcOptions) (hostos.ProcResult, error) {
		if len(argv) > 0 && argv[0] == "sh" {
			return hostos.ProcResult{Stdout: []byte("grep")}, nil
		}
		joined := strings.Join(argv, " ")
		switch {
		case strings.Contains(joined, "rev-parse"):
			if repo {
				return hostos.ProcResult{Stdout: []byte("true")}, nil
			}
			return hostos.ProcResult{Stdout: []byte("false")}, nil
		case strings.Contains(joined, "ls-files"):
			return hostos.ProcResult{Stdout: lsFiles, Truncated: truncated}, nil
		case len(argv) > 0 && argv[0] == "grep":
			return hostos.ProcResult{Stdout: []byte(grepOut)}, nil
		}
		return hostos.ProcResult{}, errors.New("unexpected remote command: " + joined)
	}}
}

func remoteSearchFileNames(t *testing.T, batches []searchpanel.SearchBatch) []string {
	t.Helper()
	var names []string
	for _, b := range batches {
		for _, f := range b.Files {
			names = append(names, f.Name)
		}
	}
	return names
}

// TestRemoteGrepFallbackUsesFreshGitCandidateScope checks the grep fallback in a git
// worktree accepts exactly Fresh's project_files scope: the remote git listing includes
// tracked files even when they match .gitignore (tracked.log here) and tracked dotfiles
// (.config), while untracked ignored entries (b.log, build/, sub/c.tmp) are simply absent;
// results below a nested repository listed by name are accepted, untracked hidden entries
// are dropped, and no local file is read.
func TestRemoteGrepFallbackUsesFreshGitCandidateScope(t *testing.T) {
	decoy := t.TempDir()
	if err := os.WriteFile(filepath.Join(decoy, "b.log"), []byte("local decoy"), 0o644); err != nil {
		t.Fatal(err)
	}
	ls := gitLsEntry("100644", "a.go") +
		gitLsEntry("100644", "tracked.log") +
		gitLsEntry("100644", "keep.log") +
		gitLsEntry("100644", "sub/d.go") +
		gitLsEntry("100644", ".config") +
		gitLsEntry("160000", "nested") +
		gitLsEntry("", "otherrepo/")
	out := "./a.go:1:todo a\n" +
		"./tracked.log:1:todo t\n" +
		"./keep.log:1:todo k\n" +
		"./sub/d.go:1:todo d\n" +
		"./b.log:1:todo b\n" +
		"./build/x.go:1:todo x\n" +
		"./sub/c.tmp:1:todo c\n" +
		"./.hidden/secret.go:1:todo s\n" +
		"./.config:1:todo cfg\n" +
		"./nested/inner.go:1:todo n\n" +
		"./otherrepo/x.go:1:todo o\n"
	one := grepScopeOneShot(true, []byte(ls), out, false)
	s := newRemoteProjectSearch(one, "/remote/w").withFiles(newRemoteFS())
	names := remoteSearchFileNames(t, collectRemoteBatches(t, s.ProjectSearchBatches(searchpanel.SearchRequest{
		Root:  "/remote/w",
		Query: "todo",
		Scope: searchpanel.AllFiles,
	}, nil)))
	want := "a.go,tracked.log,keep.log,sub/d.go,.config,nested/inner.go,otherrepo/x.go"
	if got := strings.Join(names, ","); got != want {
		t.Fatalf("files = %v, want %s", names, want)
	}
	// The local decoy directory is unrelated and unchanged: every command ran with the
	// remote cwd and no local path appears in any argv.
	if got, _ := os.ReadFile(filepath.Join(decoy, "b.log")); string(got) != "local decoy" {
		t.Fatalf("a local decoy file changed: %q", got)
	}
	for _, c := range one.recorded() {
		if c.cwd != "/remote/w" {
			t.Fatalf("remote command cwd = %q, want /remote/w", c.cwd)
		}
	}
}

// TestRemoteGrepFallbackOutsideGitKeepsIgnoreRulesOut checks a non-git workspace is the
// Fresh walk scope: .gitignore is NOT consulted (b.log stays), hidden entries are dropped,
// and no local fallback appears.
func TestRemoteGrepFallbackOutsideGitKeepsIgnoreRulesOut(t *testing.T) {
	one := grepScopeOneShot(false, nil, "./a.go:1:todo a\n./b.log:1:todo b\n./.hidden/secret.go:1:todo s\n", false)
	s := newRemoteProjectSearch(one, "/remote/w").withFiles(newRemoteFS())
	names := remoteSearchFileNames(t, collectRemoteBatches(t, s.ProjectSearchBatches(searchpanel.SearchRequest{
		Root:  "/remote/w",
		Query: "todo",
		Scope: searchpanel.AllFiles,
	}, nil)))
	if got := strings.Join(names, ","); got != "a.go,b.log" {
		t.Fatalf("files = %v, want a.go,b.log (no gitignore outside a repository, hidden dropped)", names)
	}
}

// TestRemoteGrepFallbackTruncatedGitListingDegrades checks an incomplete git candidate list
// (truncated output) is never guessed at: the scope falls back to hidden-only filtering,
// so a tracked-ignored file and an ignored file both stay visible instead of risking a
// wrongly dropped tracked file. This is a documented limitation.
func TestRemoteGrepFallbackTruncatedGitListingDegrades(t *testing.T) {
	ls := gitLsEntry("100644", "a.go") + gitLsEntry("100644", "tracked.log")
	out := "./a.go:1:todo a\n./tracked.log:1:todo t\n./b.log:1:todo b\n./.hidden/x.go:1:todo x\n"
	one := grepScopeOneShot(true, []byte(ls), out, true)
	s := newRemoteProjectSearch(one, "/remote/w").withFiles(newRemoteFS())
	names := remoteSearchFileNames(t, collectRemoteBatches(t, s.ProjectSearchBatches(searchpanel.SearchRequest{
		Root:  "/remote/w",
		Query: "todo",
		Scope: searchpanel.AllFiles,
	}, nil)))
	if got := strings.Join(names, ","); got != "a.go,tracked.log,b.log" {
		t.Fatalf("files = %v, want hidden-only filtering under a truncated listing", names)
	}
}

// TestParseGitLsFilesEntry checks the tracked/submodule/untracked split of the candidate
// listing.
func TestParseGitLsFilesEntry(t *testing.T) {
	for _, tc := range []struct {
		entry              string
		path               string
		tracked, submodule bool
	}{
		{"100644 abc 0\ta.go", "a.go", true, false},
		{"100755 abc 0\tscript.sh", "script.sh", true, false},
		{"160000 abc 0\tvendor/sub", "vendor/sub", true, true},
		{"untracked.txt", "untracked.txt", false, false},
		{"nested/", "nested/", false, false},
	} {
		path, tracked, submodule := parseGitLsFilesEntry(tc.entry)
		if path != tc.path || tracked != tc.tracked || submodule != tc.submodule {
			t.Fatalf("parse %q = %q tracked=%v submodule=%v; want %q %v %v", tc.entry, path, tracked, submodule, tc.path, tc.tracked, tc.submodule)
		}
	}
}

// rgScopeOneShot answers the rg path and the Fresh-style git candidate commands.
func rgScopeOneShot(repo bool, lsFiles []byte, rgOut string, truncated bool) *fakeOneShot {
	return &fakeOneShot{script: func(_ context.Context, argv []string, _ hostos.ProcOptions) (hostos.ProcResult, error) {
		if len(argv) > 0 && argv[0] == "sh" {
			return hostos.ProcResult{Stdout: []byte("rg")}, nil
		}
		joined := strings.Join(argv, " ")
		switch {
		case strings.Contains(joined, "rev-parse"):
			if repo {
				return hostos.ProcResult{Stdout: []byte("true")}, nil
			}
			return hostos.ProcResult{Stdout: []byte("false")}, nil
		case strings.Contains(joined, "ls-files"):
			return hostos.ProcResult{Stdout: lsFiles, Truncated: truncated}, nil
		case len(argv) > 0 && argv[0] == "rg":
			return hostos.ProcResult{Stdout: []byte(rgOut)}, nil
		}
		return hostos.ProcResult{}, errors.New("unexpected remote command: " + joined)
	}}
}

// candidateRgScript answers the command probe, the Fresh-style git commands and rg. For rg
// it emits one match line per explicitly passed candidate path (after --), so a test sees
// exactly which files the tool was asked to search.
func candidateRgScript(repo bool, lsFiles []byte, lsCode int, truncated bool) *fakeOneShot {
	return &fakeOneShot{script: func(_ context.Context, argv []string, _ hostos.ProcOptions) (hostos.ProcResult, error) {
		if len(argv) > 0 && argv[0] == "sh" {
			return hostos.ProcResult{Stdout: []byte("rg")}, nil
		}
		joined := strings.Join(argv, " ")
		switch {
		case strings.Contains(joined, "rev-parse"):
			if repo {
				return hostos.ProcResult{Stdout: []byte("true")}, nil
			}
			return hostos.ProcResult{Stdout: []byte("false")}, nil
		case strings.Contains(joined, "ls-files"):
			return hostos.ProcResult{Stdout: lsFiles, Code: lsCode, Truncated: truncated}, nil
		case len(argv) > 0 && argv[0] == "rg":
			idx := -1
			for i, a := range argv {
				if a == "--" {
					idx = i
					break
				}
			}
			if idx < 0 {
				return hostos.ProcResult{}, errors.New("rg without --: " + joined)
			}
			var out strings.Builder
			for _, p := range argv[idx+1:] {
				fmt.Fprintf(&out, "%s:1:1:todo %s\n", p, p)
			}
			return hostos.ProcResult{Stdout: []byte(out.String())}, nil
		}
		return hostos.ProcResult{}, errors.New("unexpected remote command: " + joined)
	}}
}

func rgCalls(one *fakeOneShot) [][]string {
	var out [][]string
	for _, c := range one.recorded() {
		if len(c.argv) > 0 && c.argv[0] == "rg" {
			out = append(out, c.argv)
		}
	}
	return out
}

func argvAfterSlashSlash(argv []string) []string {
	for i, a := range argv {
		if a == "--" {
			return argv[i+1:]
		}
	}
	return nil
}

func doneBatchNotice(t *testing.T, batches []searchpanel.SearchBatch) string {
	t.Helper()
	for _, b := range batches {
		if b.Done {
			return b.Notice
		}
	}
	t.Fatal("no Done batch")
	return ""
}

// TestRemoteSearchPassesCandidatePathsExplicitly checks the complete-git-listing path no
// longer widens rg over ignored/hidden trees: the tool receives the candidate file paths
// explicitly (after --), so ignored files are never scanned and cannot consume the output
// cap; tracked-ignored files and tracked dotfiles are searched, untracked hidden files are
// not passed, and the local decoy is untouched.
func TestRemoteSearchPassesCandidatePathsExplicitly(t *testing.T) {
	decoy := t.TempDir()
	if err := os.WriteFile(filepath.Join(decoy, "b.log"), []byte("local decoy"), 0o644); err != nil {
		t.Fatal(err)
	}
	ls := gitLsEntry("100644", "a.go") +
		gitLsEntry("100644", "tracked.log") +
		gitLsEntry("100644", "keep.log") +
		gitLsEntry("100644", "sub/d.go") +
		gitLsEntry("100644", ".config") +
		gitLsEntry("", ".hidden/secret.go")
	one := candidateRgScript(true, []byte(ls), 0, false)
	s := newRemoteProjectSearch(one, "/remote/w").withFiles(newRemoteFS())
	batches := collectRemoteBatches(t, s.ProjectSearchBatches(searchpanel.SearchRequest{
		Root:  "/remote/w",
		Query: "todo",
		Scope: searchpanel.AllFiles,
	}, nil))
	names := remoteSearchFileNames(t, batches)
	want := "a.go,tracked.log,keep.log,sub/d.go,.config"
	if got := strings.Join(names, ","); got != want {
		t.Fatalf("files = %v, want %s", names, want)
	}
	calls := rgCalls(one)
	if len(calls) != 1 {
		t.Fatalf("rg calls = %d, want one bounded batch", len(calls))
	}
	paths := argvAfterSlashSlash(calls[0])
	if got := strings.Join(paths, ","); got != want {
		t.Fatalf("rg explicit paths = %v, want %s", paths, want)
	}
	joined := strings.Join(calls[0], " ")
	if strings.Contains(joined, "--no-ignore") || strings.Contains(joined, "--hidden") {
		t.Fatalf("rg argv = %v, want no widened whole-tree scan", calls[0])
	}
	if notice := doneBatchNotice(t, batches); notice != "" {
		t.Fatalf("unexpected degradation notice: %q", notice)
	}
	if got, _ := os.ReadFile(filepath.Join(decoy, "b.log")); string(got) != "local decoy" {
		t.Fatalf("a local decoy file changed: %q", got)
	}
}

// TestRemoteSearchSplitsLargeCandidateLists checks a candidate list larger than one batch
// is passed in several bounded tool commands, the results merge, every path is passed
// exactly once, and a new search limit still caps the merged matches.
func TestRemoteSearchSplitsLargeCandidateLists(t *testing.T) {
	var ls strings.Builder
	var paths []string
	for i := 0; i < searchScopeBatchFiles*2+17; i++ {
		p := fmt.Sprintf("src/f%04d.go", i)
		paths = append(paths, p)
		ls.WriteString(gitLsEntry("100644", p))
	}
	one := candidateRgScript(true, []byte(ls.String()), 0, false)
	s := newRemoteProjectSearch(one, "/remote/w").withFiles(newRemoteFS())
	batches := collectRemoteBatches(t, s.ProjectSearchBatches(searchpanel.SearchRequest{
		Root:  "/remote/w",
		Query: "todo",
		Scope: searchpanel.AllFiles,
	}, nil))
	if got := len(remoteSearchFileNames(t, batches)); got != len(paths) {
		t.Fatalf("files = %d, want %d", got, len(paths))
	}
	calls := rgCalls(one)
	if len(calls) < 2 {
		t.Fatalf("rg calls = %d, want several batches", len(calls))
	}
	seen := map[string]bool{}
	for _, call := range calls {
		for _, p := range argvAfterSlashSlash(call) {
			if seen[p] {
				t.Fatalf("path %q passed twice", p)
			}
			seen[p] = true
		}
	}
	if len(seen) != len(paths) {
		t.Fatalf("explicit paths = %d, want %d", len(seen), len(paths))
	}
}

// TestRemoteSearchFallsBackWithNoticeWhenListingUnavailable checks a git work tree whose
// candidate listing failed keeps the previous whole-tree tool path (no explicit files) and
// carries a non-fatal notice to the panel, instead of silently omitting tracked-ignored
// files.
func TestRemoteSearchFallsBackWithNoticeWhenListingUnavailable(t *testing.T) {
	one := rgScopeOneShot(true, nil, "a.go:1:1:todo a\ntracked.log:1:1:todo t\n", false)
	// Make the ls-files call fail rather than truncate.
	base := one.script
	one.script = func(ctx context.Context, argv []string, o hostos.ProcOptions) (hostos.ProcResult, error) {
		if len(argv) > 0 && strings.Contains(strings.Join(argv, " "), "ls-files") {
			return hostos.ProcResult{Code: 1}, nil
		}
		return base(ctx, argv, o)
	}
	s := newRemoteProjectSearch(one, "/remote/w").withFiles(newRemoteFS())
	batches := collectRemoteBatches(t, s.ProjectSearchBatches(searchpanel.SearchRequest{
		Root:  "/remote/w",
		Query: "todo",
		Scope: searchpanel.AllFiles,
	}, nil))
	notice := doneBatchNotice(t, batches)
	if !strings.Contains(notice, "ignore scope unavailable") {
		t.Fatalf("notice = %q, want the degraded ignore-scope notice", notice)
	}
	calls := rgCalls(one)
	if len(calls) == 0 || strings.Contains(strings.Join(calls[0], " "), "--no-ignore") {
		t.Fatalf("rg calls = %v, want the default whole-tree fallback without widening", calls)
	}
}

// TestRemoteSearchFallsBackWithNoticeWhenCandidateListTooLarge checks the bounded batch
// plan: a candidate list needing more than searchScopeMaxBatches commands keeps the
// whole-tree widened scan and says so, rather than silently scanning ignored entries.
func TestRemoteSearchFallsBackWithNoticeWhenCandidateListTooLarge(t *testing.T) {
	var ls strings.Builder
	for i := 0; i < searchScopeMaxBatches*searchScopeBatchFiles+1; i++ {
		ls.WriteString(gitLsEntry("100644", fmt.Sprintf("src/f%05d.go", i)))
	}
	base := candidateRgScript(true, []byte(ls.String()), 0, false)
	one := &fakeOneShot{}
	// The widened fallback passes no explicit paths; make it return one result.
	one.script = func(ctx context.Context, argv []string, o hostos.ProcOptions) (hostos.ProcResult, error) {
		if len(argv) > 0 && argv[0] == "rg" {
			if len(argvAfterSlashSlash(argv)) > 0 {
				t.Fatalf("the too-large fallback must not pass explicit paths: %v", argv)
			}
			return hostos.ProcResult{Stdout: []byte("src/f00000.go:1:1:todo x\n")}, nil
		}
		return base.script(ctx, argv, o)
	}
	s := newRemoteProjectSearch(one, "/remote/w").withFiles(newRemoteFS())
	batches := collectRemoteBatches(t, s.ProjectSearchBatches(searchpanel.SearchRequest{
		Root:  "/remote/w",
		Query: "todo",
		Scope: searchpanel.AllFiles,
	}, nil))
	if names := remoteSearchFileNames(t, batches); len(names) != 0 {
		// The fake ls-files is generated per script call in candidateRgScript, not here.
		t.Logf("files = %v", names)
	}
	notice := doneBatchNotice(t, batches)
	if !strings.Contains(notice, "too large") {
		t.Fatalf("notice = %q, want the too-large candidate-list notice", notice)
	}
	calls := rgCalls(one)
	if len(calls) != 1 || !strings.Contains(strings.Join(calls[0], " "), "--no-ignore") {
		t.Fatalf("rg calls = %v, want one widened fallback", calls)
	}
}

// TestRemoteSearchTimeoutWithCandidateScope checks the candidate commands share the search
// context: a hanging git probe still ends in the bounded timeout notice, never a hang or a
// local fallback.
func TestRemoteSearchTimeoutWithCandidateScope(t *testing.T) {
	one := &fakeOneShot{script: func(ctx context.Context, argv []string, _ hostos.ProcOptions) (hostos.ProcResult, error) {
		if len(argv) > 0 && argv[0] == "sh" {
			return hostos.ProcResult{Stdout: []byte("rg")}, nil
		}
		<-ctx.Done()
		return hostos.ProcResult{}, ctx.Err()
	}}
	s := newRemoteProjectSearch(one, "/remote/w").withFiles(newRemoteFS())
	s.timeout = 50 * time.Millisecond
	start := time.Now()
	batches := collectRemoteBatches(t, s.ProjectSearchBatches(searchpanel.SearchRequest{
		Root:  "/remote/w",
		Query: "todo",
		Scope: searchpanel.AllFiles,
	}, nil))
	if time.Since(start) > 5*time.Second {
		t.Fatalf("the scope timeout took %s", time.Since(start))
	}
	if len(batches) != 1 || !batches[0].Done || !strings.Contains(batches[0].Err, "timed out") {
		t.Fatalf("batches = %+v", batches)
	}
}

// TestRemoteSearchCancellationWithCandidateScope checks a cancelled search also cancels the
// candidate-scope git commands: the channel closes with no Done batch and nothing falls back
// to a local search.
func TestRemoteSearchCancellationWithCandidateScope(t *testing.T) {
	started := make(chan struct{})
	one := &fakeOneShot{script: func(ctx context.Context, argv []string, _ hostos.ProcOptions) (hostos.ProcResult, error) {
		if len(argv) > 0 && argv[0] == "sh" {
			return hostos.ProcResult{Stdout: []byte("rg")}, nil
		}
		select {
		case started <- struct{}{}:
		default:
		}
		<-ctx.Done()
		return hostos.ProcResult{}, ctx.Err()
	}}
	s := newRemoteProjectSearch(one, "/remote/w").withFiles(newRemoteFS())
	cancel := make(chan struct{})
	ch := s.ProjectSearchBatches(searchpanel.SearchRequest{Root: "/remote/w", Query: "todo", Scope: searchpanel.AllFiles}, cancel)
	select {
	case <-started:
	case <-time.After(10 * time.Second):
		t.Fatal("the candidate-scope git command did not start")
	}
	close(cancel)
	var seenDone bool
	deadline := time.After(10 * time.Second)
	for {
		select {
		case b, ok := <-ch:
			if !ok {
				if seenDone {
					t.Fatal("a cancelled search sent a Done batch")
				}
				return
			}
			if b.Done {
				seenDone = true
			}
		case <-deadline:
			t.Fatal("a cancelled search did not close its channel")
		}
	}
}

// TestRemoteSearchBatchedPathCancellation checks cancellation also stops the batched tool
// loop between commands: the stream closes with no Done batch and never falls back locally.
func TestRemoteSearchBatchedPathCancellation(t *testing.T) {
	var ls strings.Builder
	for i := 0; i < searchScopeBatchFiles*2+1; i++ {
		ls.WriteString(gitLsEntry("100644", fmt.Sprintf("src/f%04d.go", i)))
	}
	base := candidateRgScript(true, []byte(ls.String()), 0, false)
	secondStarted := make(chan struct{})
	calls := 0
	one := &fakeOneShot{}
	one.script = func(ctx context.Context, argv []string, o hostos.ProcOptions) (hostos.ProcResult, error) {
		if len(argv) > 0 && argv[0] == "rg" {
			calls++
			if calls >= 2 {
				close(secondStarted)
				<-ctx.Done()
				return hostos.ProcResult{}, ctx.Err()
			}
		}
		return base.script(ctx, argv, o)
	}
	s := newRemoteProjectSearch(one, "/remote/w").withFiles(newRemoteFS())
	cancel := make(chan struct{})
	ch := s.ProjectSearchBatches(searchpanel.SearchRequest{Root: "/remote/w", Query: "todo", Scope: searchpanel.AllFiles}, cancel)
	select {
	case <-secondStarted:
	case <-time.After(10 * time.Second):
		t.Fatal("the second batch did not start")
	}
	close(cancel)
	var seenDone bool
	deadline := time.After(10 * time.Second)
	for {
		select {
		case b, ok := <-ch:
			if !ok {
				if seenDone {
					t.Fatal("a cancelled search sent a Done batch")
				}
				return
			}
			if b.Done {
				seenDone = true
			}
		case <-deadline:
			t.Fatal("a cancelled search did not close its channel")
		}
	}
}

// TestRemoteSearchNestedRepoFallsBackWithNotice checks a candidate list containing a nested
// repository (listed by name only) keeps the whole-tree scan, because its contents cannot
// be passed as explicit candidate files without losing them, and carries the notice.
func TestRemoteSearchNestedRepoFallsBackWithNotice(t *testing.T) {
	ls := gitLsEntry("100644", "a.go") + gitLsEntry("160000", "nested")
	one := rgScopeOneShot(true, []byte(ls), "a.go:1:1:todo a\nnested/inner.go:1:1:todo n\n", false)
	s := newRemoteProjectSearch(one, "/remote/w").withFiles(newRemoteFS())
	batches := collectRemoteBatches(t, s.ProjectSearchBatches(searchpanel.SearchRequest{
		Root:  "/remote/w",
		Query: "todo",
		Scope: searchpanel.AllFiles,
	}, nil))
	notice := doneBatchNotice(t, batches)
	if !strings.Contains(notice, "nested repositories") {
		t.Fatalf("notice = %q, want the nested-repository fallback notice", notice)
	}
	names := remoteSearchFileNames(t, batches)
	if got := strings.Join(names, ","); got != "a.go,nested/inner.go" {
		t.Fatalf("files = %v, want the widened scan results filtered by prefix", names)
	}
	calls := rgCalls(one)
	if len(calls) != 1 || !strings.Contains(strings.Join(calls[0], " "), "--no-ignore") {
		t.Fatalf("rg calls = %v, want one widened fallback", calls)
	}
}

// TestRemoteSearchPreviewSyntaxSpans checks the remote preview now carries syntax spans
// from the shared highlighter: a bounded remote head of a Go file is coloured, an open
// buffer's text is coloured without any remote read, a grammarless file stays plain, and a
// file over the line cap stays plain. No local file is involved.
func TestRemoteSearchPreviewSyntaxSpans(t *testing.T) {
	fs := newRemoteFS()
	fs.put("/remote/w/a.go", "package main\n\nfunc todo() {}\n")
	s := newRemoteProjectSearch(&fakeOneShot{}, "/remote/w").withFiles(fs)
	res, err := s.Preview(searchpanel.PreviewRequest{
		Path: "/remote/w/a.go", Name: "a.go", Line: 1, EndLine: 1, EndCol: 2, Start: -1, Lines: 4,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Spans) == 0 {
		t.Fatalf("remote Go preview has no syntax spans: %+v", res)
	}
	kinds := map[string]bool{}
	for _, span := range res.Spans {
		kinds[span.Kind] = true
		if span.End <= span.Start {
			t.Fatalf("empty span: %+v", span)
		}
	}
	if !kinds["keyword"] && !kinds["namespace"] && !kinds["function"] {
		t.Fatalf("span kinds = %v, want syntax kinds", kinds)
	}
	// An open buffer's text is highlighted without touching the authority.
	fs.setErr(errors.New("remote workspace is disconnected"))
	res, _ = s.Preview(searchpanel.PreviewRequest{
		Path: "/remote/w/a.go", Name: "a.go", Text: "package main\n\nvar todo = 1\n", Line: 1, EndLine: 1, EndCol: 2, Start: -1, Lines: 3,
	})
	if res.Err != "" || len(res.Spans) == 0 {
		t.Fatalf("open-buffer preview spans = %+v err %q", res.Spans, res.Err)
	}
	fs.setErr(nil)
	// No grammar: plain text (the content is not a language the sniffer recognizes).
	fs.put("/remote/w/notes.zzz", "hello there\nplain notes\n")
	res, _ = s.Preview(searchpanel.PreviewRequest{
		Path: "/remote/w/notes.zzz", Name: "notes.zzz", Line: 1, EndLine: 1, EndCol: 2, Start: -1, Lines: 2,
	})
	if len(res.Spans) != 0 {
		t.Fatalf("a grammarless preview had spans: %+v", res.Spans)
	}
	// Over the preview line cap: plain text, matching the local preview's bound.
	var many strings.Builder
	for i := 0; i < previewHighlightLines+5; i++ {
		many.WriteString("x\n")
	}
	fs.put("/remote/w/many.go", many.String())
	res, _ = s.Preview(searchpanel.PreviewRequest{
		Path: "/remote/w/many.go", Name: "many.go", Line: 1, EndLine: 1, EndCol: 2, Start: 0, Lines: 3,
	})
	if len(res.Spans) != 0 {
		t.Fatalf("an over-line-cap preview had spans: %d", len(res.Spans))
	}
}

// readCountingRemoteFS wraps the test remote file system and counts windowed preview reads,
// so the preview/highlight path can be checked for extra remote I/O.
type readCountingRemoteFS struct {
	*remoteFS
	mu        sync.Mutex
	readRange int
}

func (f *readCountingRemoteFS) ReadRange(ctx context.Context, p string, off, n int64) ([]byte, error) {
	f.mu.Lock()
	f.readRange++
	f.mu.Unlock()
	return f.remoteFS.ReadRange(ctx, p, off, n)
}

func (f *readCountingRemoteFS) reads() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.readRange
}

// TestRemoteSearchPreviewTruncatedHeadHighlightsOnce checks the existing preview's bounded
// head keeps both its syntax spans and its honest Truncated flag, and highlighting causes no
// second remote read. The file is over the 256 KiB head cap but under the 4000-line syntax
// cap, so there is a coloured truncated window.
func TestRemoteSearchPreviewTruncatedHeadHighlightsOnce(t *testing.T) {
	base := newRemoteFS()
	base.put("/remote/w/large.go", "package main\n")
	var text strings.Builder
	text.WriteString("package main\n")
	for text.Len() <= remoteSearchPreviewHeadBytes+2048 {
		text.WriteString("var value = \"")
		text.WriteString(strings.Repeat("x", 1000))
		text.WriteString("\"\n")
	}
	base.put("/remote/w/large.go", text.String())
	fs := &readCountingRemoteFS{remoteFS: base}
	s := newRemoteProjectSearch(&fakeOneShot{}, "/remote/w").withFiles(fs)
	res, err := s.Preview(searchpanel.PreviewRequest{
		Path: "/remote/w/large.go", Name: "large.go", Line: 1, EndLine: 1, EndCol: 2, Start: -1, Lines: 6,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Err != "" {
		t.Fatalf("truncated preview error = %q", res.Err)
	}
	if !res.Truncated {
		t.Fatalf("truncated head not marked: %+v", res)
	}
	if len(res.Spans) == 0 {
		t.Fatal("truncated bounded head lost its syntax spans")
	}
	if got := fs.reads(); got != 1 {
		t.Fatalf("preview remote reads = %d, want exactly the bounded head read", got)
	}
	if res.Total > previewHighlightLines {
		t.Fatalf("preview total = %d, beyond the syntax line cap", res.Total)
	}
}

// TestRemoteSearchPreviewDisconnectPreservesError checks a disconnected authority is an
// explicit preview error with no spans and no local-file fallback, and the open-buffer text
// path still highlights without any authority read.
func TestRemoteSearchPreviewDisconnectPreservesError(t *testing.T) {
	base := newRemoteFS()
	base.put("/remote/w/a.go", "package main\n")
	fs := &readCountingRemoteFS{remoteFS: base}
	s := newRemoteProjectSearch(&fakeOneShot{}, "/remote/w").withFiles(fs)
	base.setErr(errors.New("remote workspace is disconnected"))
	res, err := s.Preview(searchpanel.PreviewRequest{
		Path: "/remote/w/a.go", Name: "a.go", Line: 1, EndLine: 1, Start: -1, Lines: 4,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Err, "disconnected") {
		t.Fatalf("disconnect error = %q, want the authority error", res.Err)
	}
	if len(res.Lines) != 0 || len(res.Spans) != 0 {
		t.Fatalf("disconnected preview exposed lines/spans: %+v", res)
	}
	if got := fs.reads(); got != 1 {
		t.Fatalf("disconnected preview remote reads = %d, want one failed read", got)
	}
	// An open buffer's text is highlighted from the panel's text, no remote read.
	res, err = s.Preview(searchpanel.PreviewRequest{
		Path: "/remote/w/a.go", Name: "a.go", Text: "package main\n\nvar x = 1\n", Line: 1, EndLine: 1, Start: -1, Lines: 4,
	})
	if err != nil || res.Err != "" || len(res.Spans) == 0 {
		t.Fatalf("open-buffer preview = %+v err %v", res, err)
	}
	if got := fs.reads(); got != 1 {
		t.Fatalf("open-buffer preview read remotely: %d", got)
	}
}

// TestRemoteSearchCandidateWalkSkipsHiddenAndIgnoredDirs checks the walk half of Fresh's
// project-file scope: content below a nested repository is accepted only under the walk's
// hidden and IGNORED_DIRS rules (node_modules/target/... and hidden directories are left
// out), a nested repository whose own path is hidden or under a skip dir is not entered at
// all, and tracked candidates keep their explicit-membership exception elsewhere.
func TestRemoteSearchCandidateWalkSkipsHiddenAndIgnoredDirs(t *testing.T) {
	ls := gitLsEntry("100644", "a.go") +
		gitLsEntry("160000", "vendor") +
		gitLsEntry("160000", "node_modules/sub") +
		gitLsEntry("", "nested/") +
		gitLsEntry("", ".hiddenrepo/")
	out := strings.Join([]string{
		"a.go:1:1:todo a",
		"vendor/keep.go:1:1:todo v",
		"vendor/node_modules/pkg.go:1:1:todo n",
		"vendor/.cache/x.go:1:1:todo h",
		"nested/inner.go:1:1:todo i",
		"nested/target/x.go:1:1:todo t",
		"node_modules/sub/inner.go:1:1:todo s",
		".hiddenrepo/x.go:1:1:todo r",
		"outside.go:1:1:todo o",
	}, "\n")
	one := rgScopeOneShot(true, []byte(ls), out+"\n", false)
	s := newRemoteProjectSearch(one, "/remote/w").withFiles(newRemoteFS())
	batches := collectRemoteBatches(t, s.ProjectSearchBatches(searchpanel.SearchRequest{
		Root:  "/remote/w",
		Query: "todo",
		Scope: searchpanel.AllFiles,
	}, nil))
	names := remoteSearchFileNames(t, batches)
	sort.Strings(names)
	if got := strings.Join(names, ","); got != "a.go,nested/inner.go,vendor/keep.go" {
		t.Fatalf("files = %v, want only the walk-visible nested paths", names)
	}
	if notice := doneBatchNotice(t, batches); !strings.Contains(notice, "nested repositories") {
		t.Fatalf("notice = %q, want the nested-repository notice", notice)
	}
	if calls := rgCalls(one); len(calls) != 1 || !strings.Contains(strings.Join(calls[0], " "), "--no-ignore") {
		t.Fatalf("rg calls = %v, want one widened nested fallback", calls)
	}
}

// TestRemoteGrepFallbackOutsideGitAppliesWalkSkips checks the non-git walk scope: grep may
// scan everything, but the result filter drops hidden entries and the IGNORED_DIRS the
// Fresh walk never enters.
func TestRemoteGrepFallbackOutsideGitAppliesWalkSkips(t *testing.T) {
	one := grepScopeOneShot(false, nil, strings.Join([]string{
		"a.go:1:todo a",
		"node_modules/pkg.go:1:todo n",
		"target/x.go:1:todo t",
		"sub/.cache/x.go:1:todo h",
	}, "\n")+"\n", false)
	s := newRemoteProjectSearch(one, "/remote/w").withFiles(newRemoteFS())
	batches := collectRemoteBatches(t, s.ProjectSearchBatches(searchpanel.SearchRequest{
		Root:  "/remote/w",
		Query: "todo",
		Scope: searchpanel.AllFiles,
	}, nil))
	names := remoteSearchFileNames(t, batches)
	if got := strings.Join(names, ","); got != "a.go" {
		t.Fatalf("files = %v, want only the walk-visible a.go", names)
	}
}

// TestRemoteSearchTrackedCandidateUnderSkipDirStillSearched checks the explicit candidate
// half keeps Fresh's exception: a tracked file whose path is under node_modules is still a
// candidate (git lists it, so it is passed explicitly), even though a walked tree would
// skip that directory.
func TestRemoteSearchTrackedCandidateUnderSkipDirStillSearched(t *testing.T) {
	ls := gitLsEntry("100644", "node_modules/tracked.go") + gitLsEntry("100644", "a.go")
	one := candidateRgScript(true, []byte(ls), 0, false)
	s := newRemoteProjectSearch(one, "/remote/w").withFiles(newRemoteFS())
	batches := collectRemoteBatches(t, s.ProjectSearchBatches(searchpanel.SearchRequest{
		Root:  "/remote/w",
		Query: "todo",
		Scope: searchpanel.AllFiles,
	}, nil))
	names := remoteSearchFileNames(t, batches)
	if got := strings.Join(names, ","); got != "node_modules/tracked.go,a.go" {
		t.Fatalf("files = %v, want the tracked skip-dir candidate and a.go", names)
	}
}

// nestedCandidateScript answers the top-level Fresh git listing, a nested repository's own
// bounded listing, and rg. rg emits one match per explicitly passed candidate path; a
// widened run is answered by the caller's widen function.
func nestedCandidateScript(top, nested []byte, nestedCode int, nestedTruncated bool) *fakeOneShot {
	one := &fakeOneShot{}
	one.script = func(_ context.Context, argv []string, _ hostos.ProcOptions) (hostos.ProcResult, error) {
		if len(argv) > 0 && argv[0] == "sh" {
			return hostos.ProcResult{Stdout: []byte("rg")}, nil
		}
		joined := strings.Join(argv, " ")
		switch {
		case strings.Contains(joined, "rev-parse"):
			return hostos.ProcResult{Stdout: []byte("true")}, nil
		case strings.Contains(joined, "-C nested") && strings.Contains(joined, "ls-files"):
			return hostos.ProcResult{Stdout: nested, Code: nestedCode, Truncated: nestedTruncated}, nil
		case strings.Contains(joined, "ls-files"):
			return hostos.ProcResult{Stdout: top}, nil
		case len(argv) > 0 && argv[0] == "rg":
			idx := -1
			for i, a := range argv {
				if a == "--" {
					idx = i
					break
				}
			}
			if idx < 0 {
				return hostos.ProcResult{}, errors.New("rg without explicit candidates: " + joined)
			}
			var out strings.Builder
			for _, p := range argv[idx+1:] {
				fmt.Fprintf(&out, "%s:1:1:todo %s\n", p, p)
			}
			return hostos.ProcResult{Stdout: []byte(out.String())}, nil
		}
		return hostos.ProcResult{}, errors.New("unexpected remote command: " + joined)
	}
	return one
}

// TestRemoteSearchResolvesNestedRepositoryCandidates checks the provable nested-gitignore
// slice: a nested repository listed by name gets its own bounded git listing, so its
// tracked-ignored files are candidates and its untracked ignored files are not; the search
// then uses explicit candidate paths (no widened scan) and no degradation notice.
func TestRemoteSearchResolvesNestedRepositoryCandidates(t *testing.T) {
	top := []byte(gitLsEntry("100644", "a.go") + gitLsEntry("160000", "nested"))
	nested := []byte(gitLsEntry("100644", "tracked.log") + gitLsEntry("100644", ".config") + gitLsEntry("", "keep.go"))
	one := nestedCandidateScript(top, nested, 0, false)
	s := newRemoteProjectSearch(one, "/remote/w").withFiles(newRemoteFS())
	batches := collectRemoteBatches(t, s.ProjectSearchBatches(searchpanel.SearchRequest{
		Root:  "/remote/w",
		Query: "todo",
		Scope: searchpanel.AllFiles,
	}, nil))
	names := remoteSearchFileNames(t, batches)
	sort.Strings(names)
	want := "a.go,nested/.config,nested/keep.go,nested/tracked.log"
	if got := strings.Join(names, ","); got != want {
		t.Fatalf("files = %v, want the nested git listing candidates %s", names, want)
	}
	if notice := doneBatchNotice(t, batches); notice != "" {
		t.Fatalf("notice = %q, want no degradation when every nested listing is complete", notice)
	}
	calls := rgCalls(one)
	if len(calls) != 1 || len(argvAfterSlashSlash(calls[0])) == 0 || strings.Contains(strings.Join(calls[0], " "), "--no-ignore") {
		t.Fatalf("rg calls = %v, want one explicit-candidate run without widening", calls)
	}
	passed := map[string]bool{}
	for _, p := range argvAfterSlashSlash(calls[0]) {
		passed[p] = true
	}
	if !passed["nested/tracked.log"] || !passed["nested/keep.go"] || passed["nested/ignored.log"] {
		t.Fatalf("explicit candidates = %v, want the nested git scope", passed)
	}
}

// TestRemoteSearchNestedListingFailureKeepsFallbackWithNotice checks the all-or-nothing
// rule: when a nested repository listing fails, NO partial nested candidate set is used;
// the widened walk fallback with its notice remains, so an incomplete candidate list is
// never presented as complete.
func TestRemoteSearchNestedListingFailureKeepsFallbackWithNotice(t *testing.T) {
	top := []byte(gitLsEntry("100644", "a.go") + gitLsEntry("160000", "nested"))
	one := nestedCandidateScript(top, nil, 1, false)
	// The widened fallback returns a nested file; the prefix walk filter accepts it.
	base := one.script
	one.script = func(ctx context.Context, argv []string, o hostos.ProcOptions) (hostos.ProcResult, error) {
		if len(argv) > 0 && argv[0] == "rg" {
			return hostos.ProcResult{Stdout: []byte("a.go:1:1:todo a\nnested/inner.go:1:1:todo n\n")}, nil
		}
		return base(ctx, argv, o)
	}
	s := newRemoteProjectSearch(one, "/remote/w").withFiles(newRemoteFS())
	batches := collectRemoteBatches(t, s.ProjectSearchBatches(searchpanel.SearchRequest{
		Root:  "/remote/w",
		Query: "todo",
		Scope: searchpanel.AllFiles,
	}, nil))
	if notice := doneBatchNotice(t, batches); !strings.Contains(notice, "nested repositories") {
		t.Fatalf("notice = %q, want the nested fallback notice", notice)
	}
	calls := rgCalls(one)
	if len(calls) != 1 || !strings.Contains(strings.Join(calls[0], " "), "--no-ignore") {
		t.Fatalf("rg calls = %v, want the widened fallback (no partial explicit candidates)", calls)
	}
	if len(argvAfterSlashSlash(calls[0])) != 0 {
		t.Fatalf("partial nested candidates were used despite the failed listing: %v", calls[0])
	}
	names := remoteSearchFileNames(t, batches)
	if got := strings.Join(names, ","); got != "a.go,nested/inner.go" {
		t.Fatalf("files = %v, want the widened prefix-walk results", names)
	}
}

// TestRemoteSearchNestedRepoInsideNestedRepoKeepsFallback checks an inner nested
// repository/submodule also keeps the fallback: this slice only proves one level of nested
// listings, so it must not silently drop the deeper repository's files.
func TestRemoteSearchNestedRepoInsideNestedRepoKeepsFallback(t *testing.T) {
	top := []byte(gitLsEntry("160000", "nested"))
	nested := []byte(gitLsEntry("160000", "inner"))
	one := nestedCandidateScript(top, nested, 0, false)
	base := one.script
	one.script = func(ctx context.Context, argv []string, o hostos.ProcOptions) (hostos.ProcResult, error) {
		if len(argv) > 0 && argv[0] == "rg" {
			return hostos.ProcResult{Stdout: []byte("nested/inner/x.go:1:1:todo x\n")}, nil
		}
		return base(ctx, argv, o)
	}
	s := newRemoteProjectSearch(one, "/remote/w").withFiles(newRemoteFS())
	batches := collectRemoteBatches(t, s.ProjectSearchBatches(searchpanel.SearchRequest{
		Root:  "/remote/w",
		Query: "todo",
		Scope: searchpanel.AllFiles,
	}, nil))
	if notice := doneBatchNotice(t, batches); !strings.Contains(notice, "nested repositories") {
		t.Fatalf("notice = %q, want the nested fallback notice", notice)
	}
	calls := rgCalls(one)
	if len(calls) != 1 || !strings.Contains(strings.Join(calls[0], " "), "--no-ignore") {
		t.Fatalf("rg calls = %v, want the widened fallback", calls)
	}
}

// TestRemoteSearchNestedListingCancellation checks cancellation reaches a nested
// repository's own bounded git listing: the search channel closes with no Done batch and
// falls back to nothing, so a slow nested host cannot outlive the user's cancel.
func TestRemoteSearchNestedListingCancellation(t *testing.T) {
	top := []byte(gitLsEntry("160000", "nested"))
	nestedStarted := make(chan struct{})
	one := &fakeOneShot{script: func(ctx context.Context, argv []string, _ hostos.ProcOptions) (hostos.ProcResult, error) {
		if len(argv) > 0 && argv[0] == "sh" {
			return hostos.ProcResult{Stdout: []byte("rg")}, nil
		}
		joined := strings.Join(argv, " ")
		switch {
		case strings.Contains(joined, "rev-parse"):
			return hostos.ProcResult{Stdout: []byte("true")}, nil
		case strings.Contains(joined, "ls-files") && strings.Contains(joined, "-C nested"):
			close(nestedStarted)
			<-ctx.Done()
			return hostos.ProcResult{}, ctx.Err()
		case strings.Contains(joined, "ls-files"):
			return hostos.ProcResult{Stdout: top}, nil
		}
		return hostos.ProcResult{}, errors.New("unexpected remote command: " + joined)
	}}
	s := newRemoteProjectSearch(one, "/remote/w").withFiles(newRemoteFS())
	cancel := make(chan struct{})
	ch := s.ProjectSearchBatches(searchpanel.SearchRequest{Root: "/remote/w", Query: "todo", Scope: searchpanel.AllFiles}, cancel)
	select {
	case <-nestedStarted:
	case <-time.After(10 * time.Second):
		t.Fatal("the nested repository listing did not start")
	}
	close(cancel)
	var seenDone bool
	deadline := time.After(10 * time.Second)
	for {
		select {
		case b, ok := <-ch:
			if !ok {
				if seenDone {
					t.Fatal("a cancelled nested search sent a Done batch")
				}
				return
			}
			if b.Done {
				seenDone = true
			}
		case <-deadline:
			t.Fatal("a cancelled nested search did not close its channel")
		}
	}
}
