package app

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tbxark/gvim/internal/tui"
	"github.com/tbxark/gvim/internal/tui/palette"
	"github.com/tbxark/gvim/internal/tui/searchpanel"
	"github.com/tbxark/gvim/plugin/hostos"
)

// execOneShot runs the command through a real local sh with HOME set, like the remote
// shell would, so the ~ expansion is exercised end to end (not just string-built).
type execOneShot struct{ home string }

func (o execOneShot) Run(ctx context.Context, argv []string, _ hostos.ProcOptions) (hostos.ProcResult, error) {
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Env = append(os.Environ(), "HOME="+o.home)
	var out, errb strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &errb
	err := cmd.Run()
	code := 0
	if exit, ok := err.(*exec.ExitError); ok {
		code = exit.ExitCode()
		err = nil
	}
	return hostos.ProcResult{Stdout: []byte(out.String()), Stderr: []byte(errb.String()), Code: code}, err
}

// fakePreviewPort is a non-nil local preview adapter used to prove a remote launch clears
// it (remote rows must not read local files).
type fakePreviewPort struct{}

func (fakePreviewPort) ReadHead(string, int) ([]byte, bool, error) { return nil, false, nil }

// remoteFakeProjects is a tui.Projects that records calls.
type remoteFakeProjects struct{ calls int }

func (p *remoteFakeProjects) Open(string) (tui.Project, error) {
	p.calls++
	return tui.Project{}, nil
}

// TestRemoteFileIndexTildeRootRealShell checks a ~/... workspace root is expanded by a
// real shell before enumeration (QuoteDir used to emit ~'/...' which sh does not expand),
// including a tail with spaces, and that a command substitution in the path is never run.
func TestRemoteFileIndexTildeRootRealShell(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no sh")
	}
	home := t.TempDir()
	proj := filepath.Join(home, "my proj")
	if err := os.MkdirAll(proj, 0o700); err != nil {
		t.Fatal(err)
	}
	fs := newRemoteFinderFake().dir(proj, "main.go")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	idx, root := newRemoteFileIndex(ctx, remoteFinderSlot(fs, execOneShot{home: home}), "~/my proj")
	select {
	case <-idx.Done():
	case <-time.After(10 * time.Second):
		t.Fatal("the remote index did not finish")
	}
	if root != proj {
		t.Fatalf("expanded root = %q, want %q", root, proj)
	}
	if files, loading := idx.Snapshot(); loading || strings.Join(files, ",") != "main.go" {
		t.Fatalf("files = %v, loading %v", files, loading)
	}
}

// TestRemoteFileIndexTildeInjectionNotRun checks a path containing $(...) is quoted
// literally: the command does not run and the root stays unexpanded rather than becoming
// a command result.
func TestRemoteFileIndexTildeInjectionNotRun(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no sh")
	}
	home := t.TempDir()
	pwn := filepath.Join(t.TempDir(), "pwn")
	injected := "~/$(touch " + pwn + ")/x"
	bad := newRemoteFinderFake()
	bad.err = os.ErrNotExist
	idx, root := newRemoteFileIndex(context.Background(), remoteFinderSlot(bad, execOneShot{home: home}), injected)
	select {
	case <-idx.Done():
	case <-time.After(10 * time.Second):
		t.Fatal("the failing remote index did not finish")
	}
	if root != injected && root != filepath.Join(home, "$(touch "+pwn+")", "x") {
		t.Fatalf("unexpected injection root %q", root)
	}
	if _, err := os.Stat(pwn); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a quoted path executed command substitution: %v", err)
	}
	if err := idx.Err(); err == nil {
		t.Fatal("a missing remote root reported no index error")
	}
}

// localSearchSentinel, localReplaceSentinel and localGitGrepSentinel stand in for the
// local project-search adapters; a remote launch must clear them.
type localSearchSentinel struct{}

func (localSearchSentinel) ProjectSearch(searchpanel.SearchRequest) (searchpanel.SearchResult, error) {
	return searchpanel.SearchResult{}, nil
}

type localReplaceSentinel struct{}

func (localReplaceSentinel) ProjectReplace(searchpanel.ReplaceRequest) (searchpanel.ReplaceResult, error) {
	return searchpanel.ReplaceResult{}, nil
}

type localGitGrepSentinel struct{}

func (localGitGrepSentinel) GitGrep(searchpanel.GitRequest) (searchpanel.GitResult, error) {
	return searchpanel.GitResult{}, nil
}

// TestRemoteRootPortsDisableLocalAdapters checks a remote launch turns off the root ports
// that would read or write the local workspace: preview, project switching, local git
// status, project search/replace and git grep.
func TestRemoteRootPortsDisableLocalAdapters(t *testing.T) {
	fake := &remoteFakeProjects{}
	ports := tui.RootPorts{
		Projects:       fake,
		Preview:        fakePreviewPort{},
		Git:            localGitStatusSentinel{},
		ProjectSearch:  localSearchSentinel{},
		ProjectReplace: localReplaceSentinel{},
		GitGrep:        localGitGrepSentinel{},
	}
	remoteRootPorts(&ports)
	if ports.Preview != nil {
		t.Fatal("the local file preview is still wired in a remote workspace")
	}
	if ports.ProjectSearch != nil || ports.ProjectReplace != nil || ports.GitGrep != nil {
		t.Fatalf("local search/replace ports are still wired in a remote workspace: %+v", ports)
	}
	if ports.Projects == nil {
		t.Fatal("the remote project opener is nil")
	}
	if _, err := ports.Projects.Open("/tmp/local"); err == nil {
		t.Fatal("the remote project opener allowed a local switch")
	}
	// The helper clears Git; the remote branch installs the authority-backed port after it.
	if ports.Git != nil {
		t.Fatalf("the local git adapter is still wired in a remote workspace: %T", ports.Git)
	}
}

// localGitStatusSentinel marks the local adapter being replaced.
type localGitStatusSentinel struct{}

func (localGitStatusSentinel) Marks(string) (map[string]rune, error) {
	return map[string]rune{"/remote/w/a.go": 'M'}, nil
}

var _ palette.Previewer = fakePreviewPort{}
