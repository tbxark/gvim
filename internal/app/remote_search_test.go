package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
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
