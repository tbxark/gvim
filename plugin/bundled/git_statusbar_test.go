package bundled_test

import (
	"testing"

	"github.com/tbxark/gvim/plugin/plugintest"
	"github.com/tbxark/gvim/pluginapi"
)

var (
	repoArgv   = gitArgv("rev-parse", "--show-toplevel", "--git-path", "HEAD")
	branchArgv = gitArgv("symbolic-ref", "--short", "-q", "HEAD")
)

// branchItem returns the text of the branch item the plugin shows, and whether it shows one.
func branchItem(r *rig) (string, bool) {
	for _, it := range r.host.Status["git_statusbar"] {
		if it.ID == "branch" {
			return it.Text, true
		}
	}
	return "", false
}

func statusbarRig(t *testing.T, o rigOpts) *rig {
	t.Helper()
	o.plugins = []string{"git_statusbar"}
	if o.buffers == nil {
		o.buffers = []rigBuffer{{"/w/a.txt", "x\n"}}
	}
	inner := o.script
	o.script = func(os *plugintest.FakeOS) {
		os.SetDir("/w")
		os.SetFile("/w/.git/HEAD", "ref: refs/heads/main\n")
		if inner != nil {
			inner(os)
		}
	}
	return startRig(t, o)
}

func TestGitStatusbarShowsTheBranch(t *testing.T) {
	r := statusbarRig(t, rigOpts{script: func(o *plugintest.FakeOS) {
		o.Script(repoArgv, plugintest.ProcScript{Stdout: "/w\n.git/HEAD\n"})
		o.Script(branchArgv, plugintest.ProcScript{Stdout: "feature/x\n"})
	}})
	text, ok := branchItem(r)
	if !ok || text != "feature/x" {
		t.Fatalf("item = %q, %v (log %v)", text, ok, r.h.LogText(""))
	}
	it := r.host.Status["git_statusbar"][0]
	if !it.Right || it.Priority != 20 {
		t.Errorf("item = %+v, want right-aligned with priority 20", it)
	}
	if got := r.runs("symbolic-ref"); got != 1 {
		t.Errorf("symbolic-ref ran %d times at load", got)
	}
}

func TestGitStatusbarFollowsHEAD(t *testing.T) {
	r := statusbarRig(t, rigOpts{
		buffers: []rigBuffer{{"/w/a.txt", "x\n"}, {"/w/b.txt", "y\n"}},
		script: func(o *plugintest.FakeOS) {
			o.Script(repoArgv, plugintest.ProcScript{Stdout: "/w\n.git/HEAD\n"})
			o.Script(branchArgv, plugintest.ProcScript{Stdout: "main\n"})
		},
	})
	watches := r.os.Watches()
	if len(watches) != 1 {
		t.Fatalf("watchers = %d, want one on the directory of HEAD", len(watches))
	}
	runs := func() int { return r.runs("symbolic-ref") }
	base := runs()

	// Switching to another file of the same repository asks git nothing: the watch on HEAD
	// reports a change of branch.
	r.host.SetActive(r.ids["/w/b.txt"])
	r.h.Emit(pluginapi.Event{Name: "buffer_activated", Data: map[string]any{"buf": r.ids["/w/b.txt"]}})
	if got := runs(); got != base {
		t.Errorf("a tab switch inside the repository ran git %d more times", got-base)
	}

	// Other files of the git directory change all the time: they are ignored.
	r.os.Emit("/w/.git/index.lock", "create")
	r.h.Settle()
	if got := runs(); got != base {
		t.Errorf("a change of another file in .git ran git %d more times", got-base)
	}

	// HEAD itself changed: the branch is read again, and the item shows the new name.
	r.os.Rescript(branchArgv, plugintest.ProcScript{Stdout: "other\n"})
	r.os.Emit("/w/.git/HEAD", "create")
	r.h.Settle()
	if got := runs(); got != base+1 {
		t.Errorf("after a change of HEAD symbolic-ref ran %d times, want %d", got, base+1)
	}
	if text, ok := branchItem(r); !ok || text != "other" {
		t.Errorf("after a change of HEAD the item = %q, %v, want \"other\"", text, ok)
	}

	// A save is the user's cue that they may have committed elsewhere: ask again.
	r.h.Emit(pluginapi.Event{Name: "after_file_save", Data: map[string]any{"buf": r.ids["/w/b.txt"], "path": "/w/b.txt"}})
	if got := runs(); got != base+2 {
		t.Errorf("after a save symbolic-ref ran %d times, want %d", got, base+2)
	}
}

func TestGitStatusbarDetachedHEAD(t *testing.T) {
	r := statusbarRig(t, rigOpts{script: func(o *plugintest.FakeOS) {
		o.Script(repoArgv, plugintest.ProcScript{Stdout: "/w\n.git/HEAD\n"})
		o.Script(branchArgv, plugintest.ProcScript{Code: 1})
		o.Script(gitArgv("rev-parse", "--short=7", "HEAD"), plugintest.ProcScript{Stdout: "abc1234\n"})
	}})
	if text, ok := branchItem(r); !ok || text != "abc1234" {
		t.Fatalf("item = %q, %v", text, ok)
	}
}

func TestGitStatusbarHiddenWhereThereIsNoRepository(t *testing.T) {
	tests := []struct {
		name string
		rig  rigOpts
	}{
		{"not a repository", rigOpts{script: func(o *plugintest.FakeOS) {
			o.Script(repoArgv, plugintest.ProcScript{Code: 128, Stderr: "fatal: not a git repository\n"})
		}}},
		{"git cannot start", rigOpts{script: func(o *plugintest.FakeOS) {
			o.Script(repoArgv, plugintest.ProcScript{Err: pluginapi.Errf(pluginapi.CodeExec, "denied")})
		}}},
		{"no git and no repository known to the editor", rigOpts{noGit: true}},
		{"no file open", rigOpts{buffers: []rigBuffer{}, script: func(o *plugintest.FakeOS) {
			o.Script(repoArgv, plugintest.ProcScript{Code: 128})
		}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.rig.buffers == nil {
				tt.rig.buffers = []rigBuffer{{"/w/a.txt", "x\n"}}
			}
			r := statusbarRig(t, tt.rig)
			if text, ok := branchItem(r); ok {
				t.Errorf("item %q is shown", text)
			}
			if n := len(r.h.NoticeTexts()); n != 0 || len(r.problems()) != 0 {
				t.Errorf("notices %v problems %v", r.h.NoticeTexts(), r.problems())
			}
			if n := len(r.os.Watches()); n != 0 {
				t.Errorf("%d watchers outside a repository", n)
			}
		})
	}
}

// Without a git binary the editor's own reader answers (AMENDMENTS C28).
func TestGitStatusbarWithoutTheBinary(t *testing.T) {
	svc := &plugintest.FakeServices{GitRoots: map[string]string{"/w": "/w"}, Branches: map[string]string{"/w": "trunk"}}
	r := statusbarRig(t, rigOpts{noGit: true, services: svc})
	if text, ok := branchItem(r); !ok || text != "trunk" {
		t.Fatalf("item = %q, %v (log %v)", text, ok, r.h.LogText(""))
	}
	if runs := r.os.Runs(); len(runs) != 0 {
		t.Errorf("processes: %v", runs)
	}
}

func TestGitStatusbarUnloadLeavesNothing(t *testing.T) {
	r := statusbarRig(t, rigOpts{script: func(o *plugintest.FakeOS) {
		o.Script(repoArgv, plugintest.ProcScript{Stdout: "/w\n.git/HEAD\n"})
		o.Script(branchArgv, plugintest.ProcScript{Stdout: "main\n"})
	}})
	if len(r.host.Resources()) == 0 {
		t.Fatal("no resources while loaded")
	}
	r.h.Unload("git_statusbar")
	if got := r.host.Resources(); len(got) != 0 {
		t.Errorf("resources after unload: %v", got)
	}
}

func TestGitStatusbarRefreshesWhenWindowRegainsFocus(t *testing.T) {
	r := statusbarRig(t, rigOpts{script: func(o *plugintest.FakeOS) {
		o.Script(repoArgv, plugintest.ProcScript{Stdout: "/w\n.git/HEAD\n"})
		o.Script(branchArgv, plugintest.ProcScript{Stdout: "main\n"})
	}})
	base := r.runs("symbolic-ref")
	// A checkout in another window while gvim is unfocused can be missed by the HEAD
	// watcher; regaining OS focus is the backstop Fresh's focus_gained provides.
	r.os.Rescript(branchArgv, plugintest.ProcScript{Stdout: "after-focus\n"})
	r.h.Emit(pluginapi.Event{Name: "window_focused", Data: map[string]any{}})
	if got := r.runs("symbolic-ref"); got != base+1 {
		t.Fatalf("symbolic-ref after window_focused = %d, want %d", got, base+1)
	}
	if text, ok := branchItem(r); !ok || text != "after-focus" {
		t.Fatalf("item = %q, %v, want after-focus", text, ok)
	}
}
