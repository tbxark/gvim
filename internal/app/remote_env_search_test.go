package app

import (
	"context"
	"testing"

	"github.com/tbxark/gvim/internal/tui/searchpanel"
	"github.com/tbxark/gvim/plugin/hostos"
)

// TestRemoteProjectSearchUsesTheRemoteEnv checks that remote project search runs its probe
// and the search command with the session's captured remote delta (the same environment as
// the terminal, LSP and git grep), not the local process environment.
func TestRemoteProjectSearchUsesTheRemoteEnv(t *testing.T) {
	var calls []oneShotCall
	script := func(_ context.Context, argv []string, o hostos.ProcOptions) (hostos.ProcResult, error) {
		call := oneShotCall{argv: append([]string(nil), argv...), cwd: o.Cwd, env: map[string]string{}}
		for k, v := range o.Env {
			call.env[k] = v
		}
		calls = append(calls, call)
		if len(argv) >= 2 && argv[0] == "sh" && argv[1] == "-c" {
			return hostos.ProcResult{Stdout: []byte("rg")}, nil
		}
		return hostos.ProcResult{Stdout: []byte("a.txt:1:1:x\n")}, nil
	}
	one := &fakeOneShot{script: script}
	s := newRemoteProjectSearch(one, "/remote/proj", func() []string {
		return []string{"PATH=/venv/bin", "REMOTE_ONLY=1"}
	})
	res, err := s.ProjectSearch(searchpanel.SearchRequest{
		Query: "x", Scope: searchpanel.AllFiles, Limit: 10,
	})
	if err != nil {
		t.Fatalf("ProjectSearch = %v", err)
	}
	if len(res.Files) != 1 || res.Files[0].Name != "a.txt" {
		t.Fatalf("result = %+v", res)
	}
	if len(calls) < 2 {
		t.Fatalf("remote calls = %+v, want the tool probe and the search", calls)
	}
	for _, call := range calls {
		if call.env["PATH"] != "/venv/bin" || call.env["REMOTE_ONLY"] != "1" {
			t.Fatalf("call %v does not carry the remote environment delta", call)
		}
		if call.cwd != "/remote/proj" {
			t.Fatalf("call cwd = %q", call.cwd)
		}
	}
	// The search command itself is the tool call, not a shell string.
	found := false
	for _, call := range calls {
		if len(call.argv) > 0 && call.argv[0] == "rg" {
			found = true
		}
	}
	if !found {
		t.Fatalf("no rg call: %+v", calls)
	}
}
