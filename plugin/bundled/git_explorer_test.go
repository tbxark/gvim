package bundled_test

import (
	"io/fs"
	"testing"
	"time"

	"github.com/tbxark/gvim/plugin"
	"github.com/tbxark/gvim/plugin/bundled"
	"github.com/tbxark/gvim/plugin/plugintest"
	"github.com/tbxark/gvim/pluginapi"
)

// explorerArgv are the git commands the git_explorer port runs.
var (
	explorerRepoArgv   = gitArgv("rev-parse", "--show-toplevel", "--git-path", "HEAD")
	explorerStatusArgv = gitArgv("status", "--porcelain", "-z")
)

// explorerOS prepares a fake repository at /work whose git status is status.
func explorerOS(status string) func(o *plugintest.FakeOS) {
	return func(o *plugintest.FakeOS) {
		o.SetDir("/work")
		o.SetDir("/work/.git")
		o.SetFile("/work/.git/HEAD", "ref: refs/heads/main\n")
		o.Script(explorerRepoArgv, plugintest.ProcScript{Stdout: "/work\n/work/.git/HEAD\n"})
		o.Script(explorerStatusArgv, plugintest.ProcScript{Stdout: status})
	}
}

// TestGitExplorerDecorations checks the git status badges and priorities the port sets.
func TestGitExplorerDecorations(t *testing.T) {
	status := " M a.go\x00?? b.go\x00A  c.go\x00R  new.go\x00old.go\x00"
	r := startRig(t, rigOpts{plugins: []string{"git_explorer"}, script: explorerOS(status)})
	r.emit("after_file_open", "", nil)
	decos := r.host.ExplorerDecorations("git_explorer", "git_explorer")
	got := map[string]string{}
	for _, d := range decos {
		got[d.Path] = d.Symbol + ":" + d.Color.Key
	}
	want := map[string]string{
		"/work/a.go":   "M:ui.file_status_modified_fg",
		"/work/b.go":   "U:ui.file_status_untracked_fg",
		"/work/c.go":   "A:ui.file_status_added_fg",
		"/work/new.go": "R:ui.file_status_renamed_fg",
	}
	for path, w := range want {
		if got[path] != w {
			t.Errorf("decoration for %s = %q, want %q (all %v)", path, got[path], w, got)
		}
	}
	if _, ok := got["/work/old.go"]; ok {
		t.Errorf("the rename source was decorated: %v", got)
	}
	if len(decos) != 4 {
		t.Errorf("decorations = %+v", decos)
	}
	if slots := r.host.ExplorerSlots("git_explorer", "git_explorer"); len(slots) != 0 {
		t.Errorf("slots with color_names off = %+v", slots)
	}
}

// TestGitExplorerClearsOutsideARepository checks the decorations are cleared when git has
// no repository to answer for.
func TestGitExplorerClearsOutsideARepository(t *testing.T) {
	r := startRig(t, rigOpts{plugins: []string{"git_explorer"}, script: func(o *plugintest.FakeOS) {
		o.SetDir("/work")
		o.Script(explorerRepoArgv, plugintest.ProcScript{Code: 1})
	}})
	r.emit("after_file_open", "", nil)
	if got := r.host.ExplorerDecorations("git_explorer", "git_explorer"); len(got) != 0 {
		t.Fatalf("decorations outside a repo = %+v", got)
	}
}

// TestGitExplorerRefreshesOnWatcher checks a repository change in another terminal updates
// the badges: the watcher of the git directory triggers a new status read.
func TestGitExplorerRefreshesOnWatcher(t *testing.T) {
	status := " M a.go\x00"
	r := startRig(t, rigOpts{plugins: []string{"git_explorer"}, script: func(o *plugintest.FakeOS) {
		explorerOS(status)(o)
		o.Script(explorerStatusArgv, plugintest.ProcScript{Stdout: status})
	}})
	r.emit("after_file_open", "", nil)
	if got := len(r.host.ExplorerDecorations("git_explorer", "git_explorer")); got != 1 {
		t.Fatalf("decorations = %d", got)
	}
	// Rewrite the fake status answer and poke the git directory watcher.
	r.os.Rescript(explorerStatusArgv, plugintest.ProcScript{Stdout: " M a.go\x00 M b.go\x00"})
	r.os.Emit("/work/.git/index", "write")
	r.h.Settle()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(r.host.ExplorerDecorations("git_explorer", "git_explorer")) == 2 {
			return
		}
		r.h.Settle()
	}
	t.Fatalf("the watcher did not refresh the decorations: %+v", r.host.ExplorerDecorations("git_explorer", "git_explorer"))
}

// TestGitExplorerColorNames checks the color_names setting produces name-color slots
// bubbled to the ancestor directories.
func TestGitExplorerColorNames(t *testing.T) {
	files := plugintest.Files{}
	src, err := fs.ReadFile(bundled.FS, "plugins/git_explorer/init.lua")
	if err != nil {
		t.Fatal(err)
	}
	files["git_explorer/init.lua"] = string(src)
	host := plugintest.NewFakeHost()
	os := plugintest.NewFakeOS()
	os.Bin["git"] = "/usr/bin/git"
	explorerOS(" M src/a.go\x00")(os)
	h := plugintest.New(t, files, plugintest.Options{
		Host: host, OS: os, Timeout: 500 * time.Millisecond,
		Settings: map[string]plugin.PluginSettings{"git_explorer": {User: map[string]any{"color_names": true}}},
	})
	h.Start()
	h.Emit(pluginapi.Event{Name: "after_file_open", Data: map[string]any{"path": ""}})
	slots := host.ExplorerSlots("git_explorer", "git_explorer")
	got := map[string]string{}
	for _, s := range slots {
		got[s.Path] = s.NameColor.Key
	}
	if got["/work/src/a.go"] != "ui.file_status_modified_fg" {
		t.Errorf("file slot = %q (all %v)", got["/work/src/a.go"], got)
	}
	if got["/work/src"] != "ui.file_status_modified_fg" || got["/work"] != "ui.file_status_modified_fg" {
		t.Errorf("ancestor slots = %v", got)
	}
}
