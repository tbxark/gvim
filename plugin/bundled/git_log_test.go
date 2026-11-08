package bundled_test

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/tbxark/gvim/plugin/bundled"
	"github.com/tbxark/gvim/plugin/hostos"
	"github.com/tbxark/gvim/plugin/plugintest"
	"github.com/tbxark/gvim/pluginapi"
)

// The Fresh git_log port (plugins/git_log.ts + lib/git_history.ts + lib/git_repo.ts) as a
// bundled Lua plugin: a group-lite log/detail/toolbar layout, git run through the hardened
// argv, and a streamed, cached diff. The tests drive it over the fake host + fake OS with
// scripted git output; no golden screens.

const gitLogPlugin = "git_log"

// gitLogFiles reads the plugin out of the embedded bundled tree in the shape plugintest
// groups into a directory plugin (init.lua plus the <name>.i18n.json table).
func jsonUnmarshal(data []byte, v any) error { return json.Unmarshal(data, v) }

func gitLogFiles(t *testing.T) plugintest.Files {
	t.Helper()
	files := plugintest.Files{}
	for _, name := range []string{"init.lua", gitLogPlugin + ".i18n.json"} {
		data, err := fs.ReadFile(bundled.FS, "plugins/"+gitLogPlugin+"/"+name)
		if err != nil {
			t.Fatalf("read the bundled plugin file %s: %v", name, err)
		}
		files[gitLogPlugin+"/"+name] = string(data)
	}
	return files
}

// gitBase is the hardened prefix (the bundled gvim.git module's settings, AMENDMENTS C28).
func gitBase() []string {
	return []string{
		"git", "-c", "core.fsmonitor=false", "-c", "core.pager=cat", "-c", "diff.external=",
		"-c", "core.attributesFile=/dev/null", "-c", "protocol.ext.allow=never",
	}
}

// diffConfig pins the patch format (Fresh's diffArgs) for the commands the parser reads.
func diffConfig() []string {
	return []string{
		"-c", "diff.noprefix=false", "-c", "diff.mnemonicPrefix=false",
		"-c", "diff.srcPrefix=a/", "-c", "diff.dstPrefix=b/",
		"-c", "core.quotepath=false", "-c", "diff.suppressBlankEmpty=false",
		"-c", "diff.relative=false", "-c", "diff.submodule=short",
		"-c", "core.bigFileThreshold=1m",
	}
}

// gitArgv builds the argv prefix of one plugin git call.
func gitLogArgv(pinned bool, extra ...string) []string {
	out := gitBase()
	if pinned {
		out = append(out, "--no-optional-locks")
		out = append(out, diffConfig()...)
	}
	return append(out, extra...)
}

// commitRecord is one record of the plugin's git log --format output.
func commitRecord(hash, short, author, relDate, refs, subject string) string {
	return strings.Join([]string{
		hash, short, author, author + "@example.com", "2024-01-02 03:04:05 +0000", relDate, refs, subject,
	}, "\x00") + "\x1e"
}

const patchText = "diff --git a/src/a.go b/src/a.go\n" +
	"index 1111111..2222222 100644\n" +
	"--- a/src/a.go\n" +
	"+++ b/src/a.go\n" +
	"@@ -1,2 +1,3 @@\n" +
	" context one\n" +
	"+added line\n" +
	" context two\n"

// gitLogHost is a fake host with the repository fixture: /work is the workspace and repo,
// /work/src/a.go is open.
func gitLogHost(t *testing.T) (*plugintest.FakeHost, *plugintest.FakeOS, pluginapi.BufferID) {
	t.Helper()
	host := plugintest.NewFakeHost()
	host.Workspace().RootDir = "/work"
	buf := host.AddBuffer("/work/src/a.go", "package a\n")
	os := plugintest.NewFakeOS()
	os.Bin["git"] = "/usr/bin/git"
	return host, os, buf
}

// scriptRepo scripts a repository at /work.
func scriptRepo(os *plugintest.FakeOS) {
	os.Script(gitLogArgv(false, "rev-parse"), plugintest.ProcScript{Stdout: "/work\n/work/.git/HEAD\n"})
}

// startGitLog starts the plugin over the fake host and OS.
func startGitLog(t *testing.T, host *plugintest.FakeHost, os *plugintest.FakeOS) *plugintest.Harness {
	t.Helper()
	h := plugintest.New(t, gitLogFiles(t), plugintest.Options{
		Host: host, OS: os, Timeout: 500 * time.Millisecond, EventBudget: 5 * time.Second,
	})
	h.Start()
	return h
}

// groupPanel returns the buffer id of one panel of the single mounted group.
func groupPanel(t *testing.T, h *plugintest.Harness, name string) pluginapi.BufferID {
	t.Helper()
	groups := h.Host.Groups()
	if len(groups.Order) != 1 {
		t.Fatalf("groups = %d, want one (log %v)", len(groups.Order), h.LogText(""))
	}
	g := groups.ByID[groups.Order[0]]
	p, ok := g.Panels[name]
	if !ok {
		t.Fatalf("group has no panel %q (panels %v)", name, g.Order)
	}
	return p.Buffer
}

// contentText is the plain text of a virtual buffer's content entries.
func contentText(entries []pluginapi.ContentEntry) string {
	var b strings.Builder
	for _, e := range entries {
		if len(e.Segments) == 0 {
			b.WriteString(e.Text)
		}
		for _, seg := range e.Segments {
			b.WriteString(seg.Text)
		}
	}
	return b.String()
}

// virtualText is the plain text of a virtual buffer's content.
func virtualText(t *testing.T, host *plugintest.FakeHost, id pluginapi.BufferID) string {
	t.Helper()
	entries, _ := host.VirtualContent(id)
	return contentText(entries)
}

// fullText is a buffer's text.
func fullText(t *testing.T, h *plugintest.Harness, id pluginapi.BufferID) string {
	t.Helper()
	text, _, err := h.Host.FullText(id)
	if err != nil {
		t.Fatalf("full text of %d: %v", id, err)
	}
	return text
}

func finishStreams(h *plugintest.Harness) { h.Advance(5 * time.Millisecond) }

func lastNotice(h *plugintest.Harness) string {
	texts := h.NoticeTexts()
	if len(texts) == 0 {
		return ""
	}
	return texts[len(texts)-1]
}

// openGitLog opens the view over n scripted commits and returns the harness and the group.
func openGitLog(t *testing.T, commits ...string) (*plugintest.Harness, *plugintest.FakeHost, *plugintest.FakeOS) {
	t.Helper()
	host, os, _ := gitLogHost(t)
	scriptRepo(os)
	os.Script(gitLogArgv(false, "log"), plugintest.ProcScript{Stdout: strings.Join(commits, "")})
	os.Script(gitLogArgv(true, "show", "--numstat"), plugintest.ProcScript{})
	os.Script(gitLogArgv(true, "show", "--stat"), plugintest.ProcScript{Stdout: patchText})
	os.Script(gitLogArgv(false, "show"), plugintest.ProcScript{Stdout: "line one\nline two\nline three\n"})
	h := startGitLog(t, host, os)
	if err := h.Run(gitLogPlugin+".show_git_log", nil); err != nil {
		t.Fatal(err)
	}
	finishStreams(h)
	return h, host, os
}

func TestGitLogManifestAndLoad(t *testing.T) {
	t.Parallel()
	data, err := fs.ReadFile(bundled.FS, "plugins/"+gitLogPlugin+"/plugin.json")
	if err != nil {
		t.Fatal(err)
	}
	var m struct {
		Name           string `json:"name"`
		Version        string `json:"version"`
		Description    string `json:"description"`
		API            int    `json:"api"`
		DefaultEnabled *bool  `json:"default_enabled"`
	}
	if err := jsonUnmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	if m.Name != gitLogPlugin || m.Version == "" || strings.TrimSpace(m.Description) == "" {
		t.Errorf("manifest = %+v", m)
	}
	if m.API != 2 {
		t.Errorf("api = %d, want 2 (groups + i18n + decorations level)", m.API)
	}
	// Off by default: with git_log on, the default set measured 8.68 MiB against the firm
	// 8 MiB idle-heap gate, and this plugin is the largest of the set (same rule as vi_mode).
	if m.DefaultEnabled == nil || *m.DefaultEnabled {
		t.Errorf("default_enabled = %v, want false (idle-heap gate; docs/plugins/bundled.md)", m.DefaultEnabled)
	}
	host, os, _ := gitLogHost(t)
	scriptRepo(os)
	h := startGitLog(t, host, os)
	if info, ok := h.Plugin(gitLogPlugin); !ok || info.Status != "loaded" {
		t.Fatalf("plugin = %+v (log %v)", info, h.LogText(""))
	}
	for _, l := range h.LogText("") {
		if strings.HasPrefix(l, "warn:") || strings.HasPrefix(l, "error:") {
			t.Errorf("load logged %s", l)
		}
	}
	if notices := h.NoticeTexts(); len(notices) != 0 {
		t.Errorf("load notices = %v", notices)
	}
	wantCommands := map[string]string{
		gitLogPlugin + ".show_git_log":              "Git Log",
		gitLogPlugin + ".show_git_log_current_file": "Git Log: Current File",
		gitLogPlugin + ".git_log_close":             "Git Log: Close",
		gitLogPlugin + ".git_log_refresh":           "Git Log: Refresh",
	}
	var visible []string
	for _, c := range h.Host.Commands[gitLogPlugin] {
		if c.Hidden {
			continue
		}
		visible = append(visible, c.ID+"="+c.Title)
		if want, ok := wantCommands[c.ID]; ok {
			if c.Title != want {
				t.Errorf("command %s title = %q, want %q", c.ID, c.Title, want)
			}
			delete(wantCommands, c.ID)
		}
	}
	if len(wantCommands) != 0 {
		t.Errorf("missing palette commands: %v (have %v)", wantCommands, visible)
	}
	if len(visible) != 4 {
		t.Errorf("palette rows = %d, want Fresh's four (%v)", len(visible), visible)
	}
	// The mode actions are registered as hidden commands and bound by buffer-local modes.
	hidden := map[string]bool{}
	for _, c := range h.Host.Commands[gitLogPlugin] {
		if c.Hidden {
			hidden[c.ID] = true
		}
	}
	for _, id := range []string{
		gitLogPlugin + ".git_log_prev", gitLogPlugin + ".git_log_enter",
		gitLogPlugin + ".git_log_tab", gitLogPlugin + ".git_log_copy_hash",
		gitLogPlugin + ".git_log_file_view_close", gitLogPlugin + ".detail_up",
	} {
		if !hidden[id] {
			t.Errorf("hidden action %s is not registered", id)
		}
	}
	modes := map[string]pluginapi.ModeInfo{}
	for _, mode := range h.Host.Modes[gitLogPlugin] {
		modes[mode.Name] = mode
	}
	for _, name := range []string{"git_log", "git_log_detail", "git_log_file_view"} {
		if _, ok := modes[name]; !ok {
			t.Errorf("mode %q is not defined", name)
		}
	}
	logMode, ok := modes["git_log"]
	if !ok {
		t.Fatal("git_log mode missing")
	}
	if got := logMode.Bindings["j"]; got != gitLogPlugin+".git_log_next" {
		t.Errorf("log mode j = %q", got)
	}
	if got := logMode.Bindings["enter"]; got != gitLogPlugin+".git_log_enter" {
		t.Errorf("log mode enter = %q", got)
	}
	if !logMode.Fallthrough {
		t.Error("the log mode should fall through for the editor's own motions")
	}
	if bindings := h.Host.Bindings[gitLogPlugin]; len(bindings) != 0 {
		t.Errorf("bindings = %v, want none (Fresh binds no default key for this plugin)", bindings)
	}
}

func TestGitLogOpensAndStreams(t *testing.T) {
	t.Parallel()
	h, host, os := openGitLog(t,
		commitRecord("aaaa1111", "aaaa111", "Ann Author", "2 days ago", "HEAD -> main, origin/main", "first commit"),
		commitRecord("bbbb2222", "bbbb222", "Bob Builder", "3 days ago", "tag: v1.0", "second commit"),
		commitRecord("cccc3333", "cccc333", "Cara Cash", "1 week ago", "", "third commit"),
	)
	groups := host.Groups()
	if len(groups.Order) == 0 {
		t.Fatalf("no group; notices=%v log=%v", h.NoticeTexts(), h.LogText(""))
	}
	g := groups.ByID[groups.Order[0]]
	if g.Spec.Name != "Git Log" {
		t.Errorf("group name = %q", g.Spec.Name)
	}
	if !slices.Equal(g.Order, []string{"toolbar", "log", "detail"}) {
		t.Errorf("panel order = %v, want toolbar/log/detail", g.Order)
	}
	for _, name := range g.Order {
		p := g.Panels[name]
		if _, ok := host.Buffer(p.Buffer); !ok {
			t.Errorf("panel %s buffer %d is not a real virtual buffer", name, p.Buffer)
		}
	}
	logBuf := groupPanel(t, h, "log")
	detailBuf := groupPanel(t, h, "detail")
	toolbarBuf := groupPanel(t, h, "toolbar")
	if info, _ := host.Buffer(logBuf); info.Mode != "git_log" || !info.Virtual || !info.ReadOnly {
		t.Errorf("log buffer = %+v, want virtual read-only git_log mode", info)
	}
	if info, _ := host.Buffer(detailBuf); info.Mode != "git_log_detail" || info.Language != "diff" {
		t.Errorf("detail buffer = %+v, want git_log_detail mode and diff language", info)
	}
	if g.Focused != "log" {
		t.Errorf("focused panel = %q, want log", g.Focused)
	}
	logText := virtualText(t, h.Host, logBuf)
	for _, want := range []string{"aaaa111", "first commit", "(AA)", "2 days ago", "HEAD -> main", "bbbb222", "(BB)", "tag: v1.0"} {
		if !strings.Contains(logText, want) {
			t.Errorf("log content misses %q: %s", want, logText)
		}
	}
	if toolbar := virtualText(t, h.Host, toolbarBuf); !strings.Contains(toolbar, "Commits:") || !strings.Contains(toolbar, "3") {
		t.Errorf("toolbar content = %q", toolbar)
	}
	detailText := fullText(t, h, detailBuf)
	if !strings.Contains(detailText, "+added line") || !strings.Contains(detailText, "context one") {
		t.Errorf("detail text = %q, want the streamed patch", detailText)
	}
	if entries := h.Host.Decor().Items(gitLogPlugin, "git_log_diff", detailBuf); len(entries) == 0 {
		t.Error("the detail panel has no diff decorations")
	} else {
		kinds := map[pluginapi.DecorKind]bool{}
		for _, e := range entries {
			kinds[e.Kind] = true
		}
		if !kinds[pluginapi.DecorHighlight] || !kinds[pluginapi.DecorLine] {
			t.Errorf("diff decorations kinds = %v, want highlight (+/-) and line (headers)", kinds)
		}
	}
	// Selection tint on the first row; the fold ranges (file + hunk) were published.
	sel := h.Host.Decor().Items(gitLogPlugin, "git_log", logBuf)
	if len(sel) != 1 || sel[0].Kind != pluginapi.DecorLine || sel[0].Line != 0 {
		t.Errorf("selection decorations = %+v", sel)
	}
	folds := h.Host.FoldRanges()
	if len(folds) != 1 || folds[0].NS != "git_log" || len(folds[0].Ranges) < 2 {
		t.Errorf("folds = %+v, want file and hunk ranges in namespace git_log (log %v)", folds, h.LogText(""))
	}
	// The diff cache carries the streamed text and the completion marker.
	dir := "/home/u/.config/gvim/plugin-data/git_log/git_log"
	for _, suffix := range []string{".diff", ".done", ".json"} {
		if !os.Exists(dir + "/aaaa1111" + suffix) {
			t.Errorf("cache file %s missing", suffix)
		}
	}
	if got, _ := os.File(dir + "/aaaa1111.diff"); !strings.Contains(got, "+added line") {
		t.Errorf("cached diff = %q", got)
	}
	runs := os.Runs()
	var patchRun bool
	for _, run := range runs {
		if run.Started && slices.Equal(run.Argv[:len(gitLogArgv(true, "show", "--stat"))], gitLogArgv(true, "show", "--stat")) {
			patchRun = true
		}
	}
	if !patchRun {
		t.Errorf("no start of the patch command: %v", runs)
	}
	if got := lastNotice(h); !strings.Contains(got, "Commit aaaa111") {
		t.Errorf("last notice = %q, want the detail footer (commit_ready)", got)
	}
}

func TestGitLogSelectionDebounceAndGeneration(t *testing.T) {
	t.Parallel()
	host, os, _ := gitLogHost(t)
	scriptRepo(os)
	os.Script(gitLogArgv(false, "log"), plugintest.ProcScript{Stdout: strings.Join([]string{
		commitRecord("aaaa1111", "aaaa111", "Ann", "1 hour ago", "", "first"),
		commitRecord("bbbb2222", "bbbb222", "Bob", "2 hours ago", "", "second"),
	}, "")})
	os.Script(gitLogArgv(true, "show", "--numstat"), plugintest.ProcScript{})
	// The first detail stream is held so the test can watch it being killed.
	os.Script(gitLogArgv(true, "show", "--stat"), plugintest.ProcScript{
		Chunks: []hostos.ProcChunk{{Data: []byte(patchText)}},
		Hold:   true,
	})
	h := startGitLog(t, host, os)
	if err := h.Run(gitLogPlugin+".show_git_log", nil); err != nil {
		t.Fatal(err)
	}
	logBuf := groupPanel(t, h, "log")
	detailBuf := groupPanel(t, h, "detail")
	before := fullText(t, h, detailBuf)
	if !strings.Contains(before, "+added line") {
		t.Fatalf("first stream did not reach the detail buffer: %q", before)
	}
	procs := os.Procs()
	if len(procs) == 0 {
		t.Fatal("the patch process was not started")
	}
	first := procs[0]
	// Move the log cursor to the second commit and let the debounce fire: the plugin
	// kills the held stream and starts a new generation.
	if err := host.SetCursor(logBuf, pluginapi.Pos{Line: 1}, false, false, false); err != nil {
		t.Fatal(err)
	}
	h.Emit(pluginapi.Event{Name: "cursor_moved", Data: map[string]any{
		"buf": logBuf, "pos": pluginapi.Pos{Line: 1},
	}})
	if got := lastNotice(h); !strings.Contains(got, "Commit 2/2") {
		t.Errorf("selection notice = %q", got)
	}
	h.Advance(60 * time.Millisecond)
	if !slices.Contains(first.Signals(), "TERM") {
		t.Errorf("the superseded stream signals = %v, want TERM", first.Signals())
	}
	afterKill := fullText(t, h, detailBuf)
	// The old held process exits late; its generation is stale, so it must not touch the
	// buffer again (the new stream cleared and re-filled it).
	first.Exit(0)
	h.Settle()
	if got := fullText(t, h, detailBuf); got != afterKill {
		t.Errorf("a stale stream wrote into the detail buffer: %q -> %q", afterKill, got)
	}
	if !strings.Contains(afterKill, "+added line") {
		t.Errorf("second stream detail = %q", afterKill)
	}
}

func TestGitLogRefreshAndClose(t *testing.T) {
	t.Parallel()
	h, host, os := openGitLog(t,
		commitRecord("aaaa1111", "aaaa111", "Ann", "1 hour ago", "", "first"),
		commitRecord("bbbb2222", "bbbb222", "Bob", "2 hours ago", "", "second"),
	)
	groups := host.Groups()
	g := groups.ByID[groups.Order[0]]
	logBuf := groupPanel(t, h, "log")
	if entries, _ := host.VirtualContent(logBuf); len(entries) != 2 {
		t.Fatalf("log rows before refresh = %d, want 2", len(entries))
	}
	os.Rescript(gitLogArgv(false, "log"), plugintest.ProcScript{Stdout: commitRecord("dddd4444", "dddd444", "Dee", "just now", "", "after refresh")})
	if err := h.Run(gitLogPlugin+".git_log_refresh", nil); err != nil {
		t.Fatal(err)
	}
	refreshed := virtualText(t, host, logBuf)
	if !strings.Contains(refreshed, "after refresh") || strings.Contains(refreshed, "first commit") {
		t.Errorf("log content after refresh = %q", refreshed)
	}
	finishStreams(h)
	found := false
	for _, n := range h.NoticeTexts() {
		if strings.Contains(n, "refreshed: 1 commits") {
			found = true
		}
	}
	if !found {
		t.Errorf("refresh notice missing: %v", h.NoticeTexts())
	}
	if err := h.Run(gitLogPlugin+".git_log_close", nil); err != nil {
		t.Fatal(err)
	}
	if !g.Closed {
		t.Error("the group was not closed")
	}
	for _, name := range []string{"log", "detail", "toolbar"} {
		if _, ok := host.Buffer(g.Panels[name].Buffer); ok {
			t.Errorf("panel buffer %s survived the close", name)
		}
	}
	if got := lastNotice(h); got != "Git log closed" {
		t.Errorf("close notice = %q", got)
	}
	// A second open builds a fresh group (the tab layout is restored after close).
	if err := h.Run(gitLogPlugin+".show_git_log", nil); err != nil {
		t.Fatal(err)
	}
	if got := len(host.Groups().Order); got != 2 {
		t.Errorf("groups after reopen = %d, want 2 (one closed, one open)", got)
	}
}

func TestGitLogModesNavigateAndAct(t *testing.T) {
	t.Parallel()
	h, host, _ := openGitLog(t,
		commitRecord("aaaa1111", "aaaa111", "Ann", "1 hour ago", "", "first"),
		commitRecord("bbbb2222", "bbbb222", "Bob", "2 hours ago", "", "second"),
		commitRecord("cccc3333", "cccc333", "Cara", "3 hours ago", "", "third"),
	)
	logBuf := groupPanel(t, h, "log")
	host.SetActive(logBuf)
	// prev at the first row stays there; next moves and clamps at the last.
	if err := h.Run(gitLogPlugin+".git_log_prev", nil); err != nil {
		t.Fatal(err)
	}
	if cur, _ := host.Cursors(logBuf); cur[0].Pos.Line != 0 {
		t.Errorf("prev at the first row moved to %d", cur[0].Pos.Line)
	}
	for range 3 {
		if err := h.Run(gitLogPlugin+".git_log_next", nil); err != nil {
			t.Fatal(err)
		}
	}
	if cur, _ := host.Cursors(logBuf); cur[0].Pos.Line != 2 {
		t.Errorf("next clamped cursor = %d, want 2", cur[0].Pos.Line)
	}
	if err := h.Run(gitLogPlugin+".git_log_copy_hash", nil); err != nil {
		t.Fatal(err)
	}
	if host.Clipboard != "cccc3333" {
		t.Errorf("clipboard = %q, want the selected commit hash", host.Clipboard)
	}
	if got := lastNotice(h); !strings.Contains(got, "cccc333") {
		t.Errorf("copy notice = %q", got)
	}
	// Enter on the log focuses the detail panel.
	if err := h.Run(gitLogPlugin+".git_log_enter", nil); err != nil {
		t.Fatal(err)
	}
	groups := host.Groups()
	if g := groups.ByID[groups.Order[0]]; g.Focused != "detail" {
		t.Errorf("focused panel after Enter = %q, want detail", g.Focused)
	}
}

func TestGitLogEnterOpensFileAtLine(t *testing.T) {
	t.Parallel()
	h, host, _ := openGitLog(t,
		commitRecord("aaaa1111", "aaaa111", "Ann", "1 hour ago", "", "first"),
	)
	detailBuf := groupPanel(t, h, "detail")
	// The fixture patch has its hunk header on line 4 and "+added line" on line 6; on that
	// row the new-side line is 2 (the hunk starts at new line 1 and context one advances).
	if err := host.SetCursor(detailBuf, pluginapi.Pos{Line: 6}, false, false, false); err != nil {
		t.Fatal(err)
	}
	host.SetActive(detailBuf)
	if err := h.Run(gitLogPlugin+".git_log_detail_open_file", nil); err != nil {
		t.Fatal(err)
	}
	var view *plugintest.FakeBuffer
	for _, b := range host.Buffers {
		if strings.Contains(b.Info.Name, "src/a.go") && b.Info.Virtual {
			view = b
			break
		}
	}
	if view == nil {
		t.Fatalf("no historical file view was created: %v", host.ListBuffers())
	}
	if view.Info.Mode != "git_log_file_view" || view.Info.Language != "go" || !view.Info.ReadOnly {
		t.Errorf("file view = %+v, want read-only go buffer in git_log_file_view mode", view.Info)
	}
	if got := strings.Join(view.Lines, "\n"); got != "line one\nline two\nline three" {
		t.Errorf("file view text = %q", got)
	}
	cursors, err := host.Cursors(view.Info.ID)
	if err != nil || len(cursors) == 0 || cursors[0].Pos.Line != 1 {
		t.Errorf("file view cursors = %+v, want line 1 (0-based line 2)", cursors)
	}
	if got := lastNotice(h); !strings.Contains(got, "Target: line 2") {
		t.Errorf("file view notice = %q", got)
	}
	// The view mode's q closes only the view, not the log group.
	host.SetActive(view.Info.ID)
	if err := h.Run(gitLogPlugin+".git_log_file_view_close", nil); err != nil {
		t.Fatal(err)
	}
	if _, ok := host.Buffer(view.Info.ID); ok {
		t.Error("the file view did not close")
	}
	if got := len(host.Groups().Order); got != 1 {
		t.Errorf("groups = %d, want the log group still open", got)
	}
}

// TestGitLogRemembersDetailScroll: the now-active tier-1c-C seam. Leaving a commit saves the
// detail scroll line for it (gvim.buf.set_window_state), and coming back through the cached
// path restores it (get_window_state) instead of landing at the top.
func TestGitLogRemembersDetailScroll(t *testing.T) {
	t.Parallel()
	h, host, _ := openGitLog(t,
		commitRecord("aaaa1111", "aaaa111", "Ann", "1 hour ago", "", "first"),
		commitRecord("bbbb2222", "bbbb222", "Bob", "2 hours ago", "", "second"),
	)
	logBuf := groupPanel(t, h, "log")
	detailBuf := groupPanel(t, h, "detail")
	// Scroll the first commit's detail, then select the second: load_detail saves the
	// first commit's line under scroll:<sha> before it retargets the buffer.
	if err := host.SetCursor(detailBuf, pluginapi.Pos{Line: 2}, false, false, false); err != nil {
		t.Fatal(err)
	}
	if err := host.SetCursor(logBuf, pluginapi.Pos{Line: 1}, false, false, false); err != nil {
		t.Fatal(err)
	}
	h.Emit(pluginapi.Event{Name: "cursor_moved", Data: map[string]any{
		"buf": logBuf, "pos": pluginapi.Pos{Line: 1},
	}})
	h.Advance(60 * time.Millisecond)
	if got, ok := host.WindowStateValue("git_log", detailBuf, "scroll:aaaa1111"); !ok || fmt.Sprint(got) != "2" {
		t.Fatalf("saved scroll state = %v, %v; want the line 2 for the first commit", got, ok)
	}
	// Selecting the cached first commit again restores that scroll line.
	if err := host.SetCursor(logBuf, pluginapi.Pos{Line: 0}, false, false, false); err != nil {
		t.Fatal(err)
	}
	h.Emit(pluginapi.Event{Name: "cursor_moved", Data: map[string]any{
		"buf": logBuf, "pos": pluginapi.Pos{Line: 0},
	}})
	h.Advance(60 * time.Millisecond)
	cursors, err := host.Cursors(detailBuf)
	if err != nil || len(cursors) == 0 {
		t.Fatalf("detail cursors: %v, %v", cursors, err)
	}
	if got := cursors[0].Pos.Line; got != 2 {
		t.Errorf("restored detail scroll = line %d, want 2", got)
	}
}
func TestGitLogBoundedAndErrors(t *testing.T) {
	t.Parallel()
	t.Run("commit cap", func(t *testing.T) {
		t.Parallel()
		records := make([]string, 0, 300)
		for i := range 300 {
			records = append(records, commitRecord(
				"hash"+gitLogItoa(i), "h"+gitLogItoa(i), "A", "then", "", "commit "+gitLogItoa(i)))
		}
		host, os, _ := gitLogHost(t)
		scriptRepo(os)
		os.Script(gitLogArgv(false, "log"), plugintest.ProcScript{Stdout: strings.Join(records, "")})
		os.Script(gitLogArgv(true, "show", "--numstat"), plugintest.ProcScript{})
		os.Script(gitLogArgv(true, "show", "--stat"), plugintest.ProcScript{Stdout: patchText})
		h := startGitLog(t, host, os)
		if err := h.Run(gitLogPlugin+".show_git_log", nil); err != nil {
			t.Fatal(err)
		}
		logBuf := groupPanel(t, h, "log")
		entries, _ := h.Host.VirtualContent(logBuf)
		if len(entries) != 200 {
			t.Errorf("log rows = %d, want the 200-commit cap", len(entries))
		}
		found := false
		for _, run := range os.Runs() {
			if slices.Contains(run.Argv, "-n200") {
				found = true
			}
		}
		if !found {
			t.Error("the log command does not ask for -n200")
		}
	})
	t.Run("oversized file excluded", func(t *testing.T) {
		t.Parallel()
		host, os, _ := gitLogHost(t)
		scriptRepo(os)
		os.Script(gitLogArgv(false, "log"), plugintest.ProcScript{Stdout: commitRecord("aaaa1111", "aaaa111", "A", "now", "", "big")})
		os.Script(gitLogArgv(true, "show", "--numstat"), plugintest.ProcScript{Stdout: "5000\t0\tgenerated/big.go\n"})
		os.Script(gitLogArgv(true, "show", "--stat"), plugintest.ProcScript{Stdout: patchText})
		h := startGitLog(t, host, os)
		if err := h.Run(gitLogPlugin+".show_git_log", nil); err != nil {
			t.Fatal(err)
		}
		finishStreams(h)
		detailBuf := groupPanel(t, h, "detail")
		text := fullText(t, h, detailBuf)
		if !strings.Contains(text, "large file") || !strings.Contains(text, "generated/big.go") {
			t.Errorf("detail text lacks the omitted-file footer: %s", text)
		}
		var excludes int
		for _, run := range os.Runs() {
			for _, arg := range run.Argv {
				if arg == ":(exclude,top)generated/big.go" {
					excludes++
				}
			}
		}
		if excludes == 0 {
			t.Error("the patch command does not exclude the oversized file")
		}
		cached, _ := os.File("/home/u/.config/gvim/plugin-data/git_log/git_log/aaaa1111.diff")
		if !strings.Contains(cached, "large file") {
			t.Errorf("the cache does not carry the footer: %q", cached)
		}
	})
	t.Run("stream truncation", func(t *testing.T) {
		t.Parallel()
		host, os, _ := gitLogHost(t)
		scriptRepo(os)
		os.Script(gitLogArgv(false, "log"), plugintest.ProcScript{Stdout: commitRecord("aaaa1111", "aaaa111", "A", "now", "", "huge")})
		os.Script(gitLogArgv(true, "show", "--numstat"), plugintest.ProcScript{})
		huge := "diff --git a/big b/big\n+" + strings.Repeat("x", 4*1024*1024+64)
		os.Script(gitLogArgv(true, "show", "--stat"), plugintest.ProcScript{Stdout: huge})
		h := startGitLog(t, host, os)
		if err := h.Run(gitLogPlugin+".show_git_log", nil); err != nil {
			t.Fatal(err)
		}
		finishStreams(h)
		detailBuf := groupPanel(t, h, "detail")
		text := fullText(t, h, detailBuf)
		if !strings.Contains(text, "diff truncated") {
			t.Errorf("detail text lacks the truncation footer (len %d)", len(text))
		}
		if len(text) > 5*1024*1024 {
			t.Errorf("detail text = %d bytes, over the stream cap", len(text))
		}
		if _, ok := os.File("/home/u/.config/gvim/plugin-data/git_log/git_log/aaaa1111.done"); ok {
			t.Error("a truncated diff was marked complete")
		}
	})
	t.Run("not a repository", func(t *testing.T) {
		t.Parallel()
		host, os, _ := gitLogHost(t)
		os.Script(gitLogArgv(false, "rev-parse"), plugintest.ProcScript{Code: 128, Stderr: "fatal: not a git repository\n"})
		h := startGitLog(t, host, os)
		if err := h.Run(gitLogPlugin+".show_git_log", nil); err != nil {
			t.Fatal(err)
		}
		if got := lastNotice(h); !strings.Contains(got, "No commits found") {
			t.Errorf("notice = %q", got)
		}
		if got := len(host.Groups().Order); got != 0 {
			t.Errorf("groups = %d, want none", got)
		}
	})
	t.Run("git missing", func(t *testing.T) {
		t.Parallel()
		host := plugintest.NewFakeHost()
		host.Workspace().RootDir = "/work"
		host.AddBuffer("/work/src/a.go", "package a\n")
		os := plugintest.NewFakeOS()
		h := startGitLog(t, host, os)
		if err := h.Run(gitLogPlugin+".show_git_log", nil); err != nil {
			t.Fatal(err)
		}
		if got := lastNotice(h); !strings.Contains(got, "git binary not found") {
			t.Errorf("notice = %q", got)
		}
	})
	t.Run("current file has no path", func(t *testing.T) {
		t.Parallel()
		host, os, _ := gitLogHost(t)
		scriptRepo(os)
		os.Script(gitLogArgv(false, "log"), plugintest.ProcScript{})
		untitled := host.AddBuffer("", "scratch\n")
		host.SetActive(untitled)
		h := startGitLog(t, host, os)
		if err := h.Run(gitLogPlugin+".show_git_log_current_file", nil); err != nil {
			t.Fatal(err)
		}
		if got := lastNotice(h); !strings.Contains(got, "No file to show history for") {
			t.Errorf("notice = %q", got)
		}
	})
}

// itoa is a tiny decimal converter (the tests avoid strconv for one call).
func gitLogItoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}

// TestGitLogViewStateRestoresTheDetailScroll: the tier-1c-C window state seam is used to
// remember how far the reader scrolled into each commit's diff, and to restore it when the
// commit is revisited (the second visit comes from the disk cache).
func TestGitLogViewStateRestoresTheDetailScroll(t *testing.T) {
	t.Parallel()
	h, host, _ := openGitLog(t,
		commitRecord("aaaa1111", "aaaa111", "Ann", "2 days ago", "", "first commit"),
		commitRecord("bbbb2222", "bbbb222", "Bob", "3 days ago", "", "second commit"),
	)
	detailBuf := groupPanel(t, h, "detail")
	if err := host.SetCursor(detailBuf, pluginapi.Pos{Line: 4}, false, false, false); err != nil {
		t.Fatal(err)
	}
	// Switching away saves the scroll line of the commit being left.
	if err := h.Run(gitLogPlugin+".git_log_next", nil); err != nil {
		t.Fatal(err)
	}
	finishStreams(h)
	h.Advance(80 * time.Millisecond) // the selection debounce (60 ms)
	finishStreams(h)
	value, ok := host.WindowStateValue(gitLogPlugin, detailBuf, "scroll:aaaa1111")
	if !ok || fmt.Sprint(value) != "4" {
		t.Fatalf("saved scroll for aaaa1111 = %v (%T), %v; want 4 (notices %v)", value, value, ok, h.NoticeTexts())
	}
	// Coming back restores the caret from the saved line.
	if err := h.Run(gitLogPlugin+".git_log_prev", nil); err != nil {
		t.Fatal(err)
	}
	finishStreams(h)
	h.Advance(80 * time.Millisecond)
	finishStreams(h)
	cursors, err := host.Cursors(detailBuf)
	if err != nil || len(cursors) == 0 {
		t.Fatalf("detail cursors = %v, %v", cursors, err)
	}
	if got := cursors[0].Pos.Line; got != 4 {
		t.Errorf("restored detail caret line = %d, want 4", got)
	}
	// Closing the view saves the current commit's line too.
	if err := h.Run(gitLogPlugin+".git_log_close", nil); err != nil {
		t.Fatal(err)
	}
	if _, ok := host.WindowStateValue(gitLogPlugin, detailBuf, "scroll:aaaa1111"); !ok {
		t.Error("closing the view dropped the saved scroll state")
	}
}
