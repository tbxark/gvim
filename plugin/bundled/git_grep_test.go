package bundled_test

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/tbxark/gvim/plugin/plugintest"
	"github.com/tbxark/gvim/pluginapi"
)

// The git_grep port over the shared git test rig (fake OS, scripted git) and the shared
// finder library's live search source.

func grepArgv(caseSensitive bool, query string) []string {
	args := []string{"grep", "-n", "--column", "-I"}
	if !caseSensitive {
		args = append(args, "-i")
	}
	args = append(args, "-e", query)
	return gitArgv(args...)
}

// grepRig loads git_grep with one open file and a repository at /w; script adds the git
// answers.
func grepRig(t *testing.T, script func(o *plugintest.FakeOS)) *rig {
	t.Helper()
	inner := script
	return startRig(t, rigOpts{
		plugins: []string{"git_grep"},
		buffers: []rigBuffer{{"/w/sub/a.txt", "x\n"}},
		script: func(o *plugintest.FakeOS) {
			o.SetDir("/w/sub")
			o.SetDir("/w/cmd")
			if inner != nil {
				inner(o)
			}
		},
	})
}

func grepRepo(o *plugintest.FakeOS) {
	o.Script(repoArgv, plugintest.ProcScript{Stdout: "/w\n.git/HEAD\n"})
}

func grepItems(o *plugintest.FakeOverlay) []string {
	var out []string
	for _, it := range o.Picker.Items {
		out = append(out, it.Text)
	}
	return out
}

func grepNotice(h *plugintest.Harness) string {
	texts := h.NoticeTexts()
	if len(texts) == 0 {
		return ""
	}
	return texts[len(texts)-1]
}

func grepRun(t *testing.T, r *rig, query string) *plugintest.FakeOverlay {
	t.Helper()
	if err := r.h.Run("git_grep.git_grep", nil); err != nil {
		t.Fatal(err)
	}
	o := r.host.UI().Last("picker")
	if o == nil {
		t.Fatalf("no picker (log %v, notices %v)", r.h.LogText(""), r.h.NoticeTexts())
	}
	r.h.Resolve(pluginapi.Completion{Token: o.Token, Kind: "query", Values: []any{query}, More: true})
	r.h.Advance(70 * time.Millisecond)
	return o
}

func TestGitGrepManifestLoadAndToggleCase(t *testing.T) {
	t.Parallel()
	r := grepRig(t, grepRepo)
	if info, ok := r.h.Plugin("git_grep"); !ok || info.Status != "loaded" {
		t.Fatalf("plugin = %+v (log %v)", info, r.h.LogText(""))
	}
	titles := map[string]string{}
	for _, c := range r.host.Commands["git_grep"] {
		if !c.Hidden {
			titles[c.ID] = c.Title
		}
	}
	want := map[string]string{
		"git_grep.git_grep":    "Git Grep",
		"git_grep.toggle_case": "Git Grep: Toggle Case Sensitive",
	}
	for id, title := range want {
		if titles[id] != title {
			t.Errorf("command %s = %q, want %q", id, titles[id], title)
		}
	}
	if bindings := r.host.Bindings["git_grep"]; len(bindings) != 0 {
		t.Errorf("bindings = %v, want none (Fresh binds none)", bindings)
	}
}

func TestGitGrepListsMatchesAndJumps(t *testing.T) {
	t.Parallel()
	r := grepRig(t, func(o *plugintest.FakeOS) {
		grepRepo(o)
		o.Script(grepArgv(false, "foo"), plugintest.ProcScript{Stdout: "sub/a.txt:2:3:foo bar\ncmd/main.go:5:1:TODO foo\n"})
	})
	r.host.SetFile("/w/cmd/main.go", "1\n2\n3\n4\nfunc foo() {}\n")
	o := grepRun(t, r, "foo")
	if o.Picker.Title != "Git grep: " {
		t.Errorf("title = %q", o.Picker.Title)
	}
	if got := grepItems(o); !slices.Equal(got, []string{"sub/a.txt:2", "cmd/main.go:5"}) {
		t.Fatalf("rows = %v", got)
	}
	if got := o.Picker.Items[0].Detail; got != "foo bar" {
		t.Errorf("detail = %q", got)
	}
	if !strings.Contains(o.Picker.Footer, "Case: off") || !strings.Contains(o.Picker.Footer, "Enter: jump") {
		t.Errorf("footer = %q", o.Picker.Footer)
	}
	if last := o.Updates[len(o.Updates)-1]; last.Status == nil || *last.Status != "Found 2 matches" {
		t.Errorf("status = %+v", last.Status)
	}
	if got := r.runs("grep"); got != 1 {
		t.Errorf("git grep runs = %d", got)
	}
	// Enter opens the match at its line and column.
	r.h.Resolve(pluginapi.Completion{Token: o.Token, Values: []any{1}})
	r.h.CompleteBuffers()
	id := r.host.FindBuffer("/w/cmd/main.go")
	if id == 0 {
		t.Fatalf("the match file did not open: %v", r.host.ListBuffers())
	}
	if cursors, _ := r.host.Cursors(id); len(cursors) == 0 || cursors[0].Pos.Line != 4 || cursors[0].Pos.Col != 0 {
		t.Errorf("cursor = %+v, want line 4 col 0", cursors)
	}
	if got := grepNotice(r.h); got != "Opened cmd/main.go:5" {
		t.Errorf("open notice = %q", got)
	}
}

func TestGitGrepCaseToggleRefreshesOpenPrompt(t *testing.T) {
	t.Parallel()
	r := grepRig(t, func(o *plugintest.FakeOS) {
		grepRepo(o)
		o.Script(grepArgv(false, "foo"), plugintest.ProcScript{Stdout: "sub/a.txt:2:3:foo bar\n"})
		o.Script(grepArgv(true, "foo"), plugintest.ProcScript{Stdout: "sub/a.txt:9:1:FOO bar\n"})
	})
	o := grepRun(t, r, "foo")
	if got := grepItems(o); !slices.Equal(got, []string{"sub/a.txt:2"}) {
		t.Fatalf("case-insensitive rows = %v", got)
	}
	if err := r.h.Run("git_grep.toggle_case", nil); err != nil {
		t.Fatal(err)
	}
	if got := grepNotice(r.h); got != "Case: on" {
		t.Errorf("toggle notice = %q", got)
	}
	if got := grepItems(o); !slices.Equal(got, []string{"sub/a.txt:9"}) {
		t.Errorf("case-sensitive rows = %v", got)
	}
	if !strings.Contains(o.Picker.Footer, "Case: on") {
		t.Errorf("footer = %q", o.Picker.Footer)
	}
	caseSensitiveRun := false
	for _, run := range r.os.Runs() {
		if len(run.Argv) > len(gitHead) && run.Argv[len(gitHead)] == "grep" {
			if slices.Contains(run.Argv, "-i") {
				continue
			}
			caseSensitiveRun = true
		}
	}
	if !caseSensitiveRun {
		t.Errorf("no case-sensitive git grep run: %v", r.os.Runs())
	}
}

func TestGitGrepErrorsAndEmptyStates(t *testing.T) {
	t.Parallel()
	t.Run("not a repository", func(t *testing.T) {
		t.Parallel()
		r := grepRig(t, func(o *plugintest.FakeOS) {
			o.Script(repoArgv, plugintest.ProcScript{Code: 128, Stderr: "fatal: not a git repository\n"})
		})
		if err := r.h.Run("git_grep.git_grep", nil); err != nil {
			t.Fatal(err)
		}
		if got := grepNotice(r.h); got != "Not in git" {
			t.Errorf("notice = %q", got)
		}
	})
	t.Run("no git binary", func(t *testing.T) {
		t.Parallel()
		r := startRig(t, rigOpts{noGit: true, plugins: []string{"git_grep"}, buffers: []rigBuffer{{"/w/sub/a.txt", "x\n"}}})
		if err := r.h.Run("git_grep.git_grep", nil); err != nil {
			t.Fatal(err)
		}
		if got := grepNotice(r.h); got != "Git Grep: git is not installed" {
			t.Errorf("notice = %q", got)
		}
	})
	t.Run("no matches", func(t *testing.T) {
		t.Parallel()
		r := grepRig(t, func(o *plugintest.FakeOS) {
			grepRepo(o)
			o.Script(grepArgv(false, "zzz"), plugintest.ProcScript{Code: 1})
		})
		o := grepRun(t, r, "zzz")
		if len(o.Picker.Items) != 0 {
			t.Fatalf("rows = %+v", o.Picker.Items)
		}
		if last := o.Updates[len(o.Updates)-1]; last.Status == nil || *last.Status != "No matches found" {
			t.Errorf("status = %+v", last.Status)
		}
	})
	t.Run("real git failure", func(t *testing.T) {
		t.Parallel()
		r := grepRig(t, func(o *plugintest.FakeOS) {
			grepRepo(o)
			o.Script(grepArgv(false, "foo("), plugintest.ProcScript{Code: 2, Stderr: "fatal: bad pattern\n"})
		})
		o := grepRun(t, r, "foo(")
		if len(o.Picker.Items) != 0 {
			t.Fatalf("rows = %+v", o.Picker.Items)
		}
		if got := grepNotice(r.h); !strings.Contains(got, "Git grep error: exit 2: fatal: bad pattern") {
			t.Errorf("notice = %q", got)
		}
	})
}

// Clearing the query goes back to the prompt's hint: the empty query never reaches git grep, so
// the status must not keep the last search's "No matches found".
func TestGitGrepClearedQueryShowsTheHint(t *testing.T) {
	t.Parallel()
	r := grepRig(t, func(o *plugintest.FakeOS) {
		grepRepo(o)
		o.Script(grepArgv(false, "zzz"), plugintest.ProcScript{Code: 1})
	})
	o := grepRun(t, r, "zzz")
	if last := o.Updates[len(o.Updates)-1]; last.Status == nil || *last.Status != "No matches found" {
		t.Fatalf("status after a search without matches = %+v", last.Status)
	}
	r.h.Resolve(pluginapi.Completion{Token: o.Token, Kind: "query", Values: []any{""}, More: true})
	r.h.Advance(70 * time.Millisecond)
	if last := o.Updates[len(o.Updates)-1]; last.Status == nil || *last.Status != "Type to search..." {
		t.Errorf("status after clearing the query = %+v, want the hint", last.Status)
	}
}

func TestGitGrepCapsAndColonPaths(t *testing.T) {
	t.Parallel()
	var stdout strings.Builder
	for i := 1; i <= 150; i++ {
		stdout.WriteString("sub/a.txt:")
		stdout.WriteString(itoa(i))
		stdout.WriteString(":1:hit\n")
	}
	r := grepRig(t, func(o *plugintest.FakeOS) {
		grepRepo(o)
		o.Script(grepArgv(false, "hit"), plugintest.ProcScript{Stdout: stdout.String()})
	})
	o := grepRun(t, r, "hit")
	if len(o.Picker.Items) != 100 {
		t.Errorf("rows = %d, want the 100-row cap", len(o.Picker.Items))
	}
	// A path with colons still parses: the numeric line/column fields anchor from the left.
	r2 := grepRig(t, func(o *plugintest.FakeOS) {
		grepRepo(o)
		o.Script(grepArgv(false, "x"), plugintest.ProcScript{Stdout: "dir:name.txt:3:2:x here\n"})
	})
	o2 := grepRun(t, r2, "x")
	if got := grepItems(o2); !slices.Equal(got, []string{"dir:name.txt:3"}) {
		t.Errorf("colon-path rows = %v", got)
	}
}
