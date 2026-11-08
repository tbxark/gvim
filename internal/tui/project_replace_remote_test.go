package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/tbxark/gvim/internal/tui/prompt"
	"github.com/tbxark/gvim/internal/tui/searchpanel"
	"github.com/tbxark/gvim/theme"
)

// fakeRemoteReplacer is a searchpanel.RemoteReplacer: it records the request and returns
// the configured result. The root must take the remote branch for it and apply no local
// editor edits.
type fakeRemoteReplacer struct {
	target string
	calls  int
	req    searchpanel.ReplaceRequest
	result searchpanel.RemoteReplaceResult
	err    error
	ctxErr error
}

func (f *fakeRemoteReplacer) ProjectReplace(searchpanel.ReplaceRequest) (searchpanel.ReplaceResult, error) {
	return searchpanel.ReplaceResult{}, errors.New("local plan used for a remote replacer")
}

func (f *fakeRemoteReplacer) ProjectReplaceRemote(ctx context.Context, req searchpanel.ReplaceRequest) (searchpanel.RemoteReplaceResult, error) {
	f.calls++
	f.req = req
	f.ctxErr = ctx.Err()
	return f.result, f.err
}

func (f *fakeRemoteReplacer) RemoteTarget() string { return f.target }

// remoteReplaceModel builds the root with the fake remote replacer installed.
func remoteReplaceModel(t *testing.T, rr *fakeRemoteReplacer) (*Model, *lspEd) {
	t.Helper()
	ed := &lspEd{tabs: []*lspTab{{path: "/p/a.go", lines: []string{"package a"}}}}
	m := New(Deps{Editor: ed, Keys: fakeKeymap{}, Theme: theme.Theme{}, Clock: testClock})
	m.root.ports.ProjectReplace = rr
	return m, ed
}

// TestRemoteProjectReplaceDoesNotTouchTheLocalEditor checks a confirmed remote replacement
// runs the remote replacer with a cancellable context, reports the target and written and
// skipped counts, and applies NO local editor edits (no Save All claim).
func TestRemoteProjectReplaceDoesNotTouchTheLocalEditor(t *testing.T) {
	rr := &fakeRemoteReplacer{target: "SSH:u@h:/remote/w", result: searchpanel.RemoteReplaceResult{Files: 3, Skipped: 1}}
	m, ed := remoteReplaceModel(t, rr)
	req := searchpanel.ReplaceRequest{Query: "old", Replacement: "new", Matches: 4, Files: []searchpanel.ReplaceFile{{Path: "/p/a.go"}}}
	cmd := m.projectReplaceAnswer(projectReplaceTag{req: req}, prompt.Done{Outcome: prompt.Yes})
	if cmd == nil {
		t.Fatal("the confirmed remote replacement started no command")
	}
	msg, ok := cmd().(projectReplaceDoneMsg)
	if !ok || !msg.remote {
		t.Fatalf("replacement message = %#v, want the remote form", msg)
	}
	if rr.calls != 1 || rr.ctxErr != nil {
		t.Fatalf("remote calls = %d ctx error = %v, want one call with a live context", rr.calls, rr.ctxErr)
	}
	_, _ = m.Update(msg)
	if len(ed.applied) != 0 {
		t.Fatalf("local edits applied = %+v, want none for a remote replacement", ed.applied)
	}
	for _, want := range []string{"SSH:u@h:/remote/w", "3 files changed", "1 skipped"} {
		if !strings.Contains(m.notice, want) {
			t.Fatalf("notice = %q, missing %q", m.notice, want)
		}
	}
	if m.proj.replaceCancel != nil {
		t.Fatal("the replacement cancel was not cleared after the run")
	}
}

// TestRemoteProjectReplaceFailureKeepsThePartialCount checks a failed remote run reports the
// partial written count and does not apply local edits either.
func TestRemoteProjectReplaceFailureKeepsThePartialCount(t *testing.T) {
	rr := &fakeRemoteReplacer{target: "SSH:u@h:/remote/w", result: searchpanel.RemoteReplaceResult{Files: 2}, err: errors.New("write /remote/w/c.go: permission denied")}
	m, ed := remoteReplaceModel(t, rr)
	cmd := m.projectReplaceAnswer(projectReplaceTag{req: searchpanel.ReplaceRequest{Query: "old", Replacement: "new"}}, prompt.Done{Outcome: prompt.Yes})
	msg := cmd().(projectReplaceDoneMsg)
	_, _ = m.Update(msg)
	if len(ed.applied) != 0 {
		t.Fatalf("local edits applied = %+v after a failed remote run", ed.applied)
	}
	for _, want := range []string{"2 files already written", "permission denied"} {
		if !strings.Contains(m.notice, want) {
			t.Fatalf("notice = %q, missing %q", m.notice, want)
		}
	}
}

// TestRemoteProjectReplaceQuestionNamesTheTarget checks the confirmation text states where
// the replacement will be written.
func TestRemoteProjectReplaceQuestionNamesTheTarget(t *testing.T) {
	req := searchpanel.ReplaceRequest{Query: "old", Replacement: "new", Matches: 2, Files: []searchpanel.ReplaceFile{{Path: "/remote/w/a.go"}}}
	got := projectReplaceQuestion(req, "SSH:u@h:/remote/w")
	for _, want := range []string{"on SSH:u@h:/remote/w", "2 matches", "1 file"} {
		if !strings.Contains(got, want) {
			t.Fatalf("question = %q, missing %q", got, want)
		}
	}
	local := projectReplaceQuestion(req, "")
	if strings.Contains(local, " on ") {
		t.Fatalf("local question = %q, want no remote target", local)
	}
}
