package bundled_test

import (
	"fmt"
	"io/fs"
	"strings"
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

// TestGitExplorerDecoratesNestedSubRepos checks the monorepo behavior Fresh added in
// #2592: an independent nested repository below the workspace root is decorated from its
// own status root, not left blank because the outer repo stops at its boundary.
func TestGitExplorerDecoratesNestedSubRepos(t *testing.T) {
	r := startRig(t, rigOpts{plugins: []string{"git_explorer"}, script: func(o *plugintest.FakeOS) {
		explorerOS(" M a.go\x00")(o)
		o.SetDir("/work/vendor/lib")
		o.SetDir("/work/vendor/lib/.git")
		o.SetFile("/work/vendor/lib/.git/HEAD", "ref: refs/heads/main\n")
	}})
	r.emit("after_file_open", "", nil)
	decos := r.host.ExplorerDecorations("git_explorer", "git_explorer")
	got := map[string]string{}
	for _, d := range decos {
		got[d.Path] = d.Symbol
	}
	if got["/work/a.go"] != "M" {
		t.Errorf("root repo decoration = %v", got)
	}
	if got["/work/vendor/lib/a.go"] != "M" {
		t.Errorf("nested repository was not decorated from its own status root: %v", got)
	}
}

// TestGitExplorerWatchesNestedHead: a commit/checkout in another terminal for a nested
// monorepo repository refreshes its badges too, not just the root repository's.
func TestGitExplorerWatchesNestedHead(t *testing.T) {
	r := startRig(t, rigOpts{plugins: []string{"git_explorer"}, script: func(o *plugintest.FakeOS) {
		explorerOS(" M a.go\x00")(o)
		o.SetDir("/work/vendor/lib")
		o.SetDir("/work/vendor/lib/.git")
		o.SetFile("/work/vendor/lib/.git/HEAD", "ref: refs/heads/main\n")
		o.Script(explorerStatusArgv, plugintest.ProcScript{Stdout: " M a.go\x00"})
	}})
	r.emit("after_file_open", "", nil)
	if watches := r.os.Watches(); len(watches) < 2 {
		t.Fatalf("watchers = %d, want the root and nested HEAD directories", len(watches))
	}
	// Rewrite the fake status answer and poke the nested repository's git directory.
	r.os.Rescript(explorerStatusArgv, plugintest.ProcScript{Stdout: " M a.go\x00 M b.go\x00"})
	r.os.Emit("/work/vendor/lib/.git/index", "write")
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		r.h.Settle()
		found := false
		for _, d := range r.host.ExplorerDecorations("git_explorer", "git_explorer") {
			if d.Path == "/work/vendor/lib/b.go" && d.Symbol == "M" {
				found = true
			}
		}
		if found {
			return
		}
	}
	t.Fatalf("the nested watcher did not refresh the nested badges: %+v", r.host.ExplorerDecorations("git_explorer", "git_explorer"))
}

// TestGitExplorerWorksWithoutRootRepo: when the workspace root is not a repository but a
// nested repo is, that nested repo is still decorated (Fresh's sole discovery path).
func TestGitExplorerWorksWithoutRootRepo(t *testing.T) {
	r := startRig(t, rigOpts{plugins: []string{"git_explorer"}, script: func(o *plugintest.FakeOS) {
		o.SetDir("/work")
		o.SetDir("/work/vendor/lib")
		o.SetDir("/work/vendor/lib/.git")
		o.SetFile("/work/vendor/lib/.git/HEAD", "ref: refs/heads/main\n")
		o.Script(explorerRepoArgv, plugintest.ProcScript{Code: 1})
		o.Script(explorerStatusArgv, plugintest.ProcScript{Stdout: " M a.go\x00"})
	}})
	r.emit("after_file_open", "", nil)
	decos := r.host.ExplorerDecorations("git_explorer", "git_explorer")
	found := false
	for _, d := range decos {
		if d.Path == "/work/vendor/lib/a.go" && d.Symbol == "M" {
			found = true
		}
	}
	if !found {
		t.Fatalf("nested repo decorations = %+v", decos)
	}
}

// TestGitExplorerDiscoveryIsBounded: the nested-repo scan performs one bounded directory
// listing per candidate directory (no per-entry exists round-trip), stops at its directory
// budget, still decorates the root repo, and shows a one-time warning instead of silently
// scanning forever.
func TestGitExplorerDiscoveryIsBounded(t *testing.T) {
	r := startRig(t, rigOpts{plugins: []string{"git_explorer"}, script: func(o *plugintest.FakeOS) {
		explorerOS(" M a.go\x00")(o)
		for i := 0; i < 200; i++ {
			o.SetDir("/work/d" + fmt.Sprintf("%03d", i))
		}
	}})
	r.emit("after_file_open", "", nil)
	if got := len(r.host.ExplorerDecorations("git_explorer", "git_explorer")); got != 1 {
		t.Fatalf("root decorations = %d, want the root repo still decorated", got)
	}
	texts := r.h.NoticeTexts()
	found := false
	for _, text := range texts {
		if strings.Contains(text, "discovery stopped at its") {
			found = true
		}
	}
	if !found {
		t.Fatalf("notices = %v, want the bounded-discovery warning", texts)
	}
	reads := 0
	for _, call := range r.os.Calls() {
		if strings.HasPrefix(call, "ReadDir /work") {
			reads++
		}
	}
	if reads == 0 || reads > 2*128 {
		t.Fatalf("directory listings = %d, want at most 2 bounded refreshes (2*128)", reads)
	}
}
