package app

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/tbxark/gvim/internal/tui/palette"
	"github.com/tbxark/gvim/plugin/hostos"
)

// TestRemoteLiveGrepFilesUsesTheProjectSearchScope: Files goes through the remote project
// search (authority one-shot, Fresh candidate scope), maps its per-line matches to Live
// Grep hits, and never reads a local file.
func TestRemoteLiveGrepFilesUsesTheProjectSearchScope(t *testing.T) {
	ls := gitLsEntry("100644", "a.go") + gitLsEntry("100644", "sub/b.log")
	one := rgScopeOneShot(true, []byte(ls), "a.go:2:1:todo a\nsub/b.log:3:4:todo b\n", false)
	g := newRemoteLiveGrep(newRemoteProjectSearch(one, "/remote/w"))
	hits, err := g.Search(palette.GrepRequest{
		Root:    "/remote/w",
		Text:    "todo",
		Query:   palette.SearchOptions{Regex: true},
		Sources: palette.GrepSources{Files: true},
		Limit:   10,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) != 2 || hits[0].Rel != "a.go" || hits[0].Line != 2 || hits[0].Source != palette.GrepFiles {
		t.Fatalf("hits = %+v, want the two remote project matches", hits)
	}
	for _, call := range one.recorded() {
		if len(call.argv) > 0 && call.argv[0] == "rg" {
			for _, a := range call.argv {
				if a == "--no-ignore" {
					t.Fatalf("the Files scope scanned ignored entries: %v", call.argv)
				}
			}
		}
	}
}

// TestRemoteLiveGrepFilesWithNoCandidatesStillSearchesSnapshots: an empty remote candidate
// list starts no tool run, but Buffers and Terminals are still searched in memory and in
// order after the (empty) file scope.
func TestRemoteLiveGrepFilesWithNoCandidatesStillSearchesSnapshots(t *testing.T) {
	one := rgScopeOneShot(true, nil, "", false)
	g := newRemoteLiveGrep(newRemoteProjectSearch(one, "/remote/w"))
	hits, err := g.Search(palette.GrepRequest{
		Root:      "/remote/w",
		Text:      "todo",
		Query:     palette.SearchOptions{Regex: true},
		Sources:   palette.GrepSources{Files: true, Buffers: true, Terminals: true},
		Buffers:   []palette.BufferSource{{Name: "buf", Index: 0, Text: "todo buffer\n"}},
		Terminals: []palette.TerminalSource{{Name: "term", Index: 1, Lines: []string{"todo terminal"}}},
		Limit:     10,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) != 2 || hits[0].Source != palette.GrepBuffers || hits[1].Source != palette.GrepTerminals {
		t.Fatalf("hits = %+v, want the buffer then terminal snapshots", hits)
	}
}

// TestRemoteLiveGrepIgnoredScansWidenedAuthority: the Ignored scope has no candidate list
// that can name untracked ignored files, so it runs the tool's widened scan through the
// same authority one-shot and drops .git/skip directories without any local fallback.
func TestRemoteLiveGrepIgnoredScansWidenedAuthority(t *testing.T) {
	var mu sync.Mutex
	var calls [][]string
	one := &fakeOneShot{script: func(_ context.Context, argv []string, _ hostos.ProcOptions) (hostos.ProcResult, error) {
		mu.Lock()
		calls = append(calls, append([]string(nil), argv...))
		mu.Unlock()
		if len(argv) >= 2 && argv[0] == "sh" {
			return hostos.ProcResult{Stdout: []byte("rg")}, nil
		}
		if len(argv) > 0 && argv[0] == "rg" {
			return hostos.ProcResult{Stdout: []byte(".hidden/x.go:1:1:todo hidden\nnode_modules/y.go:2:1:todo skip\n")}, nil
		}
		return hostos.ProcResult{}, errors.New("unexpected remote command: " + strings.Join(argv, " "))
	}}
	g := newRemoteLiveGrep(newRemoteProjectSearch(one, "/remote/w"))
	res, err := g.SearchContext(context.Background(), palette.GrepRequest{
		Root:    "/remote/w",
		Text:    "todo",
		Query:   palette.SearchOptions{Regex: true},
		Sources: palette.GrepSources{Ignored: true},
		Limit:   10,
	})
	if err != nil {
		t.Fatalf("SearchContext: %v", err)
	}
	if len(res.Hits) != 1 || res.Hits[0].Rel != ".hidden/x.go" || res.Hits[0].Col != 1 {
		t.Fatalf("ignored hits = %+v, want only the hidden file", res.Hits)
	}
	mu.Lock()
	defer mu.Unlock()
	foundWiden := false
	for _, call := range calls {
		if len(call) > 0 && call[0] == "rg" {
			joined := strings.Join(call, " ")
			if !strings.Contains(joined, "--no-ignore") || !strings.Contains(joined, "--hidden") {
				t.Fatalf("ignored rg argv = %v, want the widened flags", call)
			}
			foundWiden = true
		}
	}
	if !foundWiden {
		t.Fatalf("calls = %v, want one widened rg run", calls)
	}
}

// TestRemoteLiveGrepCancellationStopsTheRemoteSearch: the optional SearchContext path
// cancels the streaming project search; the authority command sees its context done and the
// error is returned instead of a local fallback.
func TestRemoteLiveGrepCancellationStopsTheRemoteSearch(t *testing.T) {
	started := make(chan struct{})
	var once sync.Once
	one := &fakeOneShot{script: func(ctx context.Context, argv []string, _ hostos.ProcOptions) (hostos.ProcResult, error) {
		joined := strings.Join(argv, " ")
		switch {
		case len(argv) >= 2 && argv[0] == "sh":
			return hostos.ProcResult{Stdout: []byte("rg")}, nil
		case strings.Contains(joined, "rev-parse"):
			return hostos.ProcResult{Stdout: []byte("true")}, nil
		case strings.Contains(joined, "ls-files"):
			return hostos.ProcResult{Stdout: []byte(gitLsEntry("100644", "a.go"))}, nil
		case len(argv) > 0 && argv[0] == "rg":
			once.Do(func() { close(started) })
			<-ctx.Done()
			return hostos.ProcResult{}, ctx.Err()
		}
		return hostos.ProcResult{}, errors.New("unexpected remote command: " + joined)
	}}
	g := newRemoteLiveGrep(newRemoteProjectSearch(one, "/remote/w"))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := g.SearchContext(ctx, palette.GrepRequest{
			Root:    "/remote/w",
			Text:    "todo",
			Query:   palette.SearchOptions{Regex: true},
			Sources: palette.GrepSources{Files: true},
			Limit:   10,
		})
		done <- err
	}()
	<-started
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled search err = %v, want context.Canceled", err)
	}
}

// TestRemoteLiveGrepTrustGate: the explicit workspace trust gate runs before the remote
// project search and a refusal starts no remote command.
func TestRemoteLiveGrepTrustGate(t *testing.T) {
	one := &fakeOneShot{script: func(context.Context, []string, hostos.ProcOptions) (hostos.ProcResult, error) {
		return hostos.ProcResult{}, errors.New("the fake should not run")
	}}
	g := newRemoteLiveGrep(newRemoteProjectSearch(one, "/remote/w"), func(context.Context, []string, string) error {
		return errors.New("workspace is not trusted")
	})
	_, err := g.Search(palette.GrepRequest{
		Root:    "/remote/w",
		Text:    "todo",
		Query:   palette.SearchOptions{Regex: true},
		Sources: palette.GrepSources{Files: true},
		Limit:   10,
	})
	if err == nil || !strings.Contains(err.Error(), "not trusted") {
		t.Fatalf("Search err = %v, want the trust refusal", err)
	}
	if calls := one.recorded(); len(calls) != 0 {
		t.Fatalf("a refused search ran %v", calls)
	}
}

// TestRemoteLiveGrepFilesHonorsTheIndexSnapshot: the remote quick-open snapshot bounds the
// Files scope through the project-search candidate filter; a path outside it is dropped
// even if the remote tool reports it.
func TestRemoteLiveGrepFilesHonorsTheIndexSnapshot(t *testing.T) {
	one := rgScopeOneShot(true, nil, "a.go:2:1:todo a\noutside.go:1:1:todo out\n", false)
	g := newRemoteLiveGrep(newRemoteProjectSearch(one, "/remote/w"))
	hits, err := g.Search(palette.GrepRequest{
		Root:    "/remote/w",
		Text:    "todo",
		Query:   palette.SearchOptions{Regex: true},
		Sources: palette.GrepSources{Files: true},
		Files:   []string{"a.go"},
		Limit:   10,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) != 1 || hits[0].Rel != "a.go" {
		t.Fatalf("hits = %+v, want only the indexed a.go", hits)
	}
}

// TestRemoteLiveGrepSnapshotsOnlyStartsNoRemoteCommand: a Buffers/Terminals-only search is
// pure in-memory text work: no authority command, no trust gate, no local file read.
func TestRemoteLiveGrepSnapshotsOnlyStartsNoRemoteCommand(t *testing.T) {
	one := &fakeOneShot{script: func(context.Context, []string, hostos.ProcOptions) (hostos.ProcResult, error) {
		return hostos.ProcResult{}, errors.New("a snapshot-only search must not run a remote command")
	}}
	gated := false
	g := newRemoteLiveGrep(newRemoteProjectSearch(one, "/remote/w"), func(context.Context, []string, string) error {
		gated = true
		return errors.New("trust gate must not run for snapshots")
	})
	hits, err := g.Search(palette.GrepRequest{
		Root:      "/remote/w",
		Text:      "todo",
		Query:     palette.SearchOptions{Regex: true},
		Sources:   palette.GrepSources{Buffers: true, Terminals: true},
		Buffers:   []palette.BufferSource{{Name: "buf", Text: "todo buf\n"}},
		Terminals: []palette.TerminalSource{{Name: "term", Lines: []string{"todo term"}}},
		Limit:     10,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if gated {
		t.Fatal("the trust gate ran for an in-memory snapshot search")
	}
	if len(one.recorded()) != 0 {
		t.Fatalf("snapshot-only search ran remote commands: %v", one.recorded())
	}
	if len(hits) != 2 {
		t.Fatalf("hits = %+v, want buffer and terminal hits", hits)
	}
}

// TestRemoteLiveGrepPropagatesProjectNotice: a project-search degradation notice reaches the
// Live Grep result together with the successful hits; it is not silently dropped.
func TestRemoteLiveGrepPropagatesProjectNotice(t *testing.T) {
	one := rgScopeOneShot(true, nil, "a.go:1:1:todo a\n", false)
	base := one.script
	one.script = func(ctx context.Context, argv []string, o hostos.ProcOptions) (hostos.ProcResult, error) {
		if len(argv) > 0 && strings.Contains(strings.Join(argv, " "), "ls-files") {
			return hostos.ProcResult{Code: 1}, nil
		}
		return base(ctx, argv, o)
	}
	g := newRemoteLiveGrep(newRemoteProjectSearch(one, "/remote/w").withFiles(newRemoteFS()))
	res, err := g.SearchContext(context.Background(), palette.GrepRequest{
		Root:    "/remote/w",
		Text:    "todo",
		Query:   palette.SearchOptions{Regex: true},
		Sources: palette.GrepSources{Files: true},
		Limit:   10,
	})
	if err != nil {
		t.Fatalf("SearchContext: %v", err)
	}
	if len(res.Hits) != 1 || res.Hits[0].Rel != "a.go" {
		t.Fatalf("hits = %+v, want the successful file hit kept", res.Hits)
	}
	if !strings.Contains(res.Notice, "ignore scope unavailable") {
		t.Fatalf("notice = %q, want the project-search degradation notice", res.Notice)
	}
}

// TestRemoteLiveGrepPropagatesProjectTruncation: a project batch/output cap hit is carried as
// Truncated while the hits collected before the cap remain available.
func TestRemoteLiveGrepPropagatesProjectTruncation(t *testing.T) {
	one := rgScopeOneShot(true, []byte(gitLsEntry("100644", "a.go")), "a.go:1:1:todo a\n", false)
	base := one.script
	one.script = func(ctx context.Context, argv []string, o hostos.ProcOptions) (hostos.ProcResult, error) {
		if len(argv) > 0 && argv[0] == "rg" {
			res, err := base(ctx, argv, o)
			res.Truncated = true
			return res, err
		}
		return base(ctx, argv, o)
	}
	g := newRemoteLiveGrep(newRemoteProjectSearch(one, "/remote/w"))
	res, err := g.SearchContext(context.Background(), palette.GrepRequest{
		Root:    "/remote/w",
		Text:    "todo",
		Query:   palette.SearchOptions{Regex: true},
		Sources: palette.GrepSources{Files: true},
		Limit:   10,
	})
	if err != nil {
		t.Fatalf("SearchContext: %v", err)
	}
	if !res.Truncated {
		t.Fatalf("result = %+v, want Truncated for the cap-hit batch", res)
	}
	if len(res.Hits) != 1 {
		t.Fatalf("hits = %+v, want the successful hit kept", res.Hits)
	}
}

// TestRemoteLiveGrepIgnoredTruncationNotice: the widened ignored scan's output cap is
// visible as an incomplete notice while its successful hits are kept.
func TestRemoteLiveGrepIgnoredTruncationNotice(t *testing.T) {
	one := &fakeOneShot{script: func(_ context.Context, argv []string, _ hostos.ProcOptions) (hostos.ProcResult, error) {
		if len(argv) >= 2 && argv[0] == "sh" {
			return hostos.ProcResult{Stdout: []byte("rg")}, nil
		}
		return hostos.ProcResult{Stdout: []byte(".hidden/x.go:1:1:todo hidden\n"), Truncated: true}, nil
	}}
	g := newRemoteLiveGrep(newRemoteProjectSearch(one, "/remote/w"))
	res, err := g.SearchContext(context.Background(), palette.GrepRequest{
		Root:    "/remote/w",
		Text:    "todo",
		Query:   palette.SearchOptions{Regex: true},
		Sources: palette.GrepSources{Ignored: true},
		Limit:   10,
	})
	if err != nil {
		t.Fatalf("SearchContext: %v", err)
	}
	if !res.Truncated || !strings.Contains(res.Notice, "incomplete") {
		t.Fatalf("result = %+v, want the incomplete ignored-cap notice", res)
	}
	if len(res.Hits) != 1 || res.Hits[0].Rel != ".hidden/x.go" {
		t.Fatalf("hits = %+v, want the successful ignored hit kept", res.Hits)
	}
}

// TestRemoteLiveGrepIgnoredPartialOnNonZeroExit: a tool that prints matches then exits 2
// keeps those hits and reports an incomplete notice instead of discarding stdout; with no
// parsed hits the error is still returned.
func TestRemoteLiveGrepIgnoredPartialOnNonZeroExit(t *testing.T) {
	one := &fakeOneShot{script: func(_ context.Context, argv []string, _ hostos.ProcOptions) (hostos.ProcResult, error) {
		if len(argv) >= 2 && argv[0] == "sh" {
			return hostos.ProcResult{Stdout: []byte("rg")}, nil
		}
		return hostos.ProcResult{Stdout: []byte(".hidden/x.go:1:1:todo hidden\n"), Code: 2}, nil
	}}
	g := newRemoteLiveGrep(newRemoteProjectSearch(one, "/remote/w"))
	res, err := g.SearchContext(context.Background(), palette.GrepRequest{
		Root:    "/remote/w",
		Text:    "todo",
		Query:   palette.SearchOptions{Regex: true},
		Sources: palette.GrepSources{Ignored: true},
		Limit:   10,
	})
	if err != nil {
		t.Fatalf("SearchContext returned an error despite partial hits: %v", err)
	}
	if len(res.Hits) != 1 || res.Hits[0].Rel != ".hidden/x.go" {
		t.Fatalf("partial hits = %+v, want the stdout match kept", res.Hits)
	}
	if !res.Truncated || !strings.Contains(res.Notice, "incomplete") {
		t.Fatalf("partial result = %+v, want the incomplete notice", res)
	}

	// No parsed hit: the non-zero exit remains a hard error.
	empty := &fakeOneShot{script: func(_ context.Context, argv []string, _ hostos.ProcOptions) (hostos.ProcResult, error) {
		if len(argv) >= 2 && argv[0] == "sh" {
			return hostos.ProcResult{Stdout: []byte("rg")}, nil
		}
		return hostos.ProcResult{Code: 2}, nil
	}}
	g = newRemoteLiveGrep(newRemoteProjectSearch(empty, "/remote/w"))
	if _, err := g.SearchContext(context.Background(), palette.GrepRequest{
		Root:    "/remote/w",
		Text:    "todo",
		Query:   palette.SearchOptions{Regex: true},
		Sources: palette.GrepSources{Ignored: true},
		Limit:   10,
	}); err == nil {
		t.Fatal("a non-zero exit without any hit returned no error")
	}
}

// TestRemoteLiveGrepFilesKeepsPartialStdoutOnExitError: the Files scope's tool exit>1 keeps
// the stdout matches as incomplete Live Grep hits instead of dropping them.
func TestRemoteLiveGrepFilesKeepsPartialStdoutOnExitError(t *testing.T) {
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
	g := newRemoteLiveGrep(newRemoteProjectSearch(one, "/remote/w").withFiles(newRemoteFS()))
	res, err := g.SearchContext(context.Background(), palette.GrepRequest{
		Root:    "/remote/w",
		Text:    "todo",
		Query:   palette.SearchOptions{Regex: true},
		Sources: palette.GrepSources{Files: true},
		Limit:   10,
	})
	if err != nil {
		t.Fatalf("SearchContext: %v", err)
	}
	if len(res.Hits) != 1 || res.Hits[0].Rel != "a.go" {
		t.Fatalf("hits = %+v, want the partial stdout hit kept", res.Hits)
	}
	if !res.Truncated || !strings.Contains(res.Notice, "stopped early") {
		t.Fatalf("result = %+v, want an incomplete notice for the tool exit error", res)
	}
}
