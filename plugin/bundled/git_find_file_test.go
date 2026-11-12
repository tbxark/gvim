package bundled_test

import (
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/tbxark/gvim/plugin/plugintest"
	"github.com/tbxark/gvim/pluginapi"
)

var lsArgv = gitArgv("ls-files", "-z", "--full-name")

func findRig(t *testing.T, o rigOpts) *rig {
	t.Helper()
	o.plugins = []string{"git_find_file"}
	if o.buffers == nil {
		o.buffers = []rigBuffer{{"/w/sub/a.txt", "x\n"}}
	}
	inner := o.script
	o.script = func(os *plugintest.FakeOS) {
		os.SetDir("/w/sub")
		if inner != nil {
			inner(os)
		}
	}
	return startRig(t, o)
}

func repoWith(files string) func(o *plugintest.FakeOS) {
	return func(o *plugintest.FakeOS) {
		o.Script(repoArgv, plugintest.ProcScript{Stdout: "/w\n.git/HEAD\n"})
		o.Script(lsArgv, plugintest.ProcScript{Stdout: files})
	}
}

func pickerTexts(o *plugintest.FakeOverlay) []string {
	var out []string
	for _, it := range o.Picker.Items {
		out = append(out, it.Text)
	}
	return out
}

func TestGitFindFileOpensTheChosenFile(t *testing.T) {
	r := findRig(t, rigOpts{script: repoWith("README.md\x00cmd/main.go\x00sub/a.txt\x00")})
	r.host.SetFile("/w/cmd/main.go", "package main\n")
	if err := r.h.Run("git_find_file.find", nil); err != nil {
		t.Fatal(err)
	}
	o := r.host.UI().Last("picker")
	if o == nil || !o.Open {
		t.Fatalf("no picker (log %v, notices %v)", r.h.LogText(""), r.h.NoticeTexts())
	}
	if want := []string{"README.md", "cmd/main.go", "sub/a.txt"}; !slices.Equal(pickerTexts(o), want) || !o.Picker.Fuzzy || o.Picker.Title != "Find file" {
		t.Errorf("picker = %v fuzzy %v title %q", pickerTexts(o), o.Picker.Fuzzy, o.Picker.Title)
	}
	// ls-files runs at the root of the repository, which is not the directory of the file.
	if got := r.os.Runs()[len(r.os.Runs())-1].Opts.Cwd; got != "/w" {
		t.Errorf("ls-files ran in %q, want the repository root", got)
	}

	r.h.Resolve(pluginapi.Completion{Token: o.Token, Values: []any{1}})
	r.h.CompleteBuffers()
	if id := r.host.FindBuffer("/w/cmd/main.go"); id == 0 || r.host.ActiveBuffer() != id {
		t.Errorf("cmd/main.go is not open and active (buffers %v, log %v)", r.host.ListBuffers(), r.h.LogText(""))
	}
	if n := r.h.NoticeTexts(); len(n) != 0 {
		t.Errorf("notices: %v", n)
	}
}

func TestGitFindFileChineseFallsBackToEnglish(t *testing.T) {
	r := findRig(t, rigOpts{
		locale: "zh-CN",
		script: repoWith("a\x00b\x00"),
	})
	commands := map[string]string{}
	for _, c := range r.host.Commands["git_find_file"] {
		commands[c.ID] = c.Title
	}
	if got := commands["git_find_file.find"]; got != "Git Find File" {
		t.Errorf("Chinese find title = %q, want the English fallback", got)
	}
	if got := commands["git_find_file.reload"]; got != "Git Find File: Reload" {
		t.Errorf("Chinese reload title = %q, want the English fallback", got)
	}
}

func TestGitFindFileCancelOpensNothing(t *testing.T) {
	r := findRig(t, rigOpts{script: repoWith("a\x00b\x00")})
	if err := r.h.Run("git_find_file.find", nil); err != nil {
		t.Fatal(err)
	}
	o := r.host.UI().Last("picker")
	r.h.Resolve(pluginapi.Completion{Token: o.Token, Values: []any{nil}})
	r.h.CompleteBuffers()
	if n := len(r.host.ListBuffers()); n != 1 {
		t.Errorf("%d buffers after cancelling", n)
	}
}

func TestGitFindFileSaysWhyNot(t *testing.T) {
	tests := []struct {
		name string
		rig  rigOpts
		want string
	}{
		{"not a repository", rigOpts{script: func(o *plugintest.FakeOS) {
			o.Script(repoArgv, plugintest.ProcScript{Code: 128, Stderr: "fatal: not a git repository\n"})
		}}, "Git Find File: not inside a git repository"},
		{"no git binary", rigOpts{noGit: true}, "Git Find File: git is not installed"},
		{"nothing tracked", rigOpts{script: repoWith("")}, "No git-tracked files found"},
		{"ls-files fails", rigOpts{script: func(o *plugintest.FakeOS) {
			o.Script(repoArgv, plugintest.ProcScript{Stdout: "/w\n.git/HEAD\n"})
			o.Script(lsArgv, plugintest.ProcScript{Code: 128, Stderr: "fatal: oops\n"})
		}}, "Git Find File: cannot list the files (not_found: fatal: oops)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := findRig(t, tt.rig)
			if err := r.h.Run("git_find_file.find", nil); err != nil {
				t.Fatal(err)
			}
			if got := r.h.NoticeTexts(); !slices.Equal(got, []string{tt.want}) {
				t.Errorf("notices = %q, want %q", got, tt.want)
			}
			if o := r.host.UI().Last("picker"); o != nil {
				t.Errorf("a picker opened: %v", pickerTexts(o))
			}
		})
	}
}

func TestGitFindFileCapsHugeRepositories(t *testing.T) {
	var sb strings.Builder
	for i := 0; i < 50_010; i++ {
		sb.WriteString("dir/file")
		sb.WriteString(strconv.Itoa(i))
		sb.WriteByte(0)
	}
	sb.WriteString(strings.Repeat("x", 5000)) // a name too long for the picker is skipped
	sb.WriteByte(0)
	r := findRig(t, rigOpts{script: repoWith(sb.String())})
	if err := r.h.Run("git_find_file.find", nil); err != nil {
		t.Fatal(err)
	}
	// The plugin gives the scheduler a turn every 5 000 names (gvim.timer.sleep(0)): the
	// manual clock has to run for the picker to open.
	for i := 0; i < 50 && r.host.UI().Last("picker") == nil; i++ {
		r.h.Advance(time.Millisecond)
	}
	o := r.host.UI().Last("picker")
	if o == nil || len(o.Picker.Items) != 50_000 {
		t.Fatalf("picker = %v (log %v)", o, r.h.LogText(""))
	}
	if got := r.h.NoticeTexts(); !slices.Equal(got, []string{"Showing the first 50000 git-tracked files"}) {
		t.Errorf("notices = %q", got)
	}
}

func TestGitFindFileReloadReadsTheListAgain(t *testing.T) {
	r := findRig(t, rigOpts{script: repoWith("a\x00")})
	for range 2 {
		if err := r.h.Run("git_find_file.reload", nil); err != nil {
			t.Fatal(err)
		}
	}
	if n := r.runs("ls-files"); n != 2 {
		t.Errorf("ls-files ran %d times for two reloads", n)
	}
	if got := r.h.NoticeTexts(); !slices.Equal(got, []string{"Reloading git files...", "Reloading git files..."}) {
		t.Errorf("notices = %q", got)
	}
}

func TestGitFindFileUsesTheWorkspaceWhenNoFileIsOpen(t *testing.T) {
	r := findRig(t, rigOpts{buffers: []rigBuffer{}, script: repoWith("a\x00")})
	if err := r.h.Run("git_find_file.find", nil); err != nil {
		t.Fatal(err)
	}
	if o := r.host.UI().Last("picker"); o == nil || !slices.Equal(pickerTexts(o), []string{"a"}) {
		t.Errorf("picker = %v", o)
	}
	if got := r.os.Runs()[0].Opts.Cwd; got != "/work" {
		t.Errorf("rev-parse ran in %q, want the workspace root /work", got)
	}
}
