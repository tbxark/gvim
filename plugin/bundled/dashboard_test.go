package bundled_test

import (
	"encoding/json"
	"io/fs"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/tbxark/gvim/plugin/bundled"
	"github.com/tbxark/gvim/plugin/plugintest"
	"github.com/tbxark/gvim/pluginapi"
)

const dashboardPlugin = "dashboard"

func dashboardRig(t *testing.T, buffers []rigBuffer) *rig {
	t.Helper()
	return startRig(t, rigOpts{
		plugins: []string{dashboardPlugin},
		buffers: buffers,
		script:  dashboardGitScript,
	})
}

// dashboardGitScript answers the git/df commands the dashboard's sections run.
func dashboardGitScript(o *plugintest.FakeOS) {
	o.Bin["git"] = "/usr/bin/git"
	o.Script(gitArgv("rev-parse", "--abbrev-ref", "HEAD"), plugintest.ProcScript{Stdout: "main\n"})
	o.Script(gitArgv("remote", "get-url", "origin"), plugintest.ProcScript{Stdout: "git@example.com:me/repo.git\n"})
	o.Script(gitArgv("status", "--porcelain=v1"), plugintest.ProcScript{Stdout: " M a.txt\n?? b.txt\n"})
	o.Script(gitArgv("symbolic-ref", "--short", "refs/remotes/origin/HEAD"), plugintest.ProcScript{Code: 1})
	o.Script(gitArgv("rev-parse", "--verify", "main"), plugintest.ProcScript{Stdout: "abc\n"})
	o.Script(gitArgv("rev-list", "--left-right", "--count", "main...HEAD"), plugintest.ProcScript{Stdout: "2\t3\n"})
	o.Script(gitArgv("rev-list", "--left-right", "--count", "HEAD...@{upstream}"), plugintest.ProcScript{Stdout: "2\t4\n"})
}

// dashboardNoUpstreamScript answers the dashboard's git reads but not the upstream
// one: the branch has no @{upstream}, so the section must show "no upstream".
func dashboardNoUpstreamScript(o *plugintest.FakeOS) {
	o.Bin["git"] = "/usr/bin/git"
	o.Script(gitArgv("rev-parse", "--abbrev-ref", "HEAD"), plugintest.ProcScript{Stdout: "main\n"})
	o.Script(gitArgv("remote", "get-url", "origin"), plugintest.ProcScript{Stdout: "git@example.com:me/repo.git\n"})
	o.Script(gitArgv("status", "--porcelain=v1"), plugintest.ProcScript{Stdout: " M a.txt\n?? b.txt\n"})
	o.Script(gitArgv("symbolic-ref", "--short", "refs/remotes/origin/HEAD"), plugintest.ProcScript{Code: 1})
	o.Script(gitArgv("rev-parse", "--verify", "main"), plugintest.ProcScript{Stdout: "abc\n"})
	o.Script(gitArgv("rev-list", "--left-right", "--count", "main...HEAD"), plugintest.ProcScript{Stdout: "2\t3\n"})
}

// dashboardBuffer returns the dashboard's virtual buffer, or 0.
func dashboardBuffer(host *plugintest.FakeHost) pluginapi.BufferID {
	for _, info := range host.ListBuffers() {
		if strings.Contains(info.Name, "Dashboard") && info.Virtual {
			return info.ID
		}
	}
	return 0
}

// dashboardText is the concatenated text of the dashboard's content entries.
func dashboardText(t *testing.T, host *plugintest.FakeHost) string {
	t.Helper()
	id := dashboardBuffer(host)
	if id == 0 {
		t.Fatalf("no dashboard buffer (buffers %v)", host.ListBuffers())
	}
	entries, _ := host.VirtualContent(id)
	return contentText(entries)
}

// dashboardReady opens the dashboard through the plugin's startup event.
func dashboardReady(t *testing.T, r *rig) {
	t.Helper()
	r.h.Emit(pluginapi.Event{Name: "ready", Data: map[string]any{}})
	r.h.Advance(50 * 1000000) // 50ms: the debounced buffer_closed path is not involved
	r.h.Settle()
}

func TestDashboardManifestAndLoad(t *testing.T) {
	t.Parallel()
	data, err := fs.ReadFile(bundled.FS, "plugins/dashboard/plugin.json")
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
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	if m.Name != dashboardPlugin || m.Version == "" || m.Description == "" || m.API != 2 {
		t.Errorf("manifest = %+v", m)
	}
	if m.DefaultEnabled == nil || *m.DefaultEnabled {
		t.Errorf("default_enabled = %v, want false (memory gate)", m.DefaultEnabled)
	}

	r := dashboardRig(t, nil)
	if p := r.problems(); len(p) != 0 {
		t.Errorf("load problems: %v", p)
	}
	for _, c := range r.host.Commands[dashboardPlugin] {
		if c.Hidden {
			continue
		}
		if c.ID != "dashboard.show" || c.Title != "Show Dashboard" {
			t.Errorf("visible command = %q=%q, want dashboard.show=Show Dashboard", c.ID, c.Title)
		}
	}
	if bindings := r.host.Bindings[dashboardPlugin]; len(bindings) != 0 {
		t.Errorf("bindings = %v, want none (Fresh binds no dashboard key)", bindings)
	}
	if modes := r.host.Modes[dashboardPlugin]; len(modes) != 1 || modes[0].Name != "dashboard" {
		t.Errorf("modes = %+v, want the buffer-local dashboard mode", modes)
	}
}

func TestDashboardAutoOpensAndShowsSections(t *testing.T) {
	t.Parallel()
	r := dashboardRig(t, nil)
	dashboardReady(t, r)
	id := dashboardBuffer(r.host)
	if id == 0 {
		t.Fatalf("no dashboard after ready (notices %v log %v)", r.h.NoticeTexts(), r.h.LogText(""))
	}
	if got := r.host.ActiveBuffer(); got != id {
		t.Errorf("active buffer = %d, want the dashboard %d", got, id)
	}
	text := dashboardText(t, r.host)
	for _, want := range []string{"FRESH", "Git", "branch", "main", "tracking", "↑2 ↓4", "working tree", "1 changed", "1 untracked", "↑3 ↓2", "Actions", "Show Dashboard"} {
		if want == "Show Dashboard" {
			continue // the dashboard content lists actions, not the palette title
		}
		if !strings.Contains(text, want) {
			t.Errorf("dashboard text misses %q:\n%s", want, text)
		}
	}
	if !strings.Contains(text, "Open Settings") || !strings.Contains(text, "Git Log") {
		t.Errorf("actions section misses rows:\n%s", text)
	}
	if got := r.host.InputMode[dashboardPlugin]; got != "" {
		// The buffer-local mode is on the buffer, not the editor input mode.
		t.Logf("input mode = %q", got)
	}
}

func TestDashboardKeyboardActivationRunsActions(t *testing.T) {
	t.Parallel()
	r := dashboardRig(t, nil)
	dashboardReady(t, r)
	// With no recent files the first rows are the static actions: Open Settings first.
	if err := r.h.Run("dashboard.activate", nil); err != nil {
		t.Fatal(err)
	}
	if got := r.host.Cmd().Ran(); !slices.Equal(got, []string{"open_settings"}) {
		t.Errorf("actions run = %v, want open_settings", got)
	}
	if err := r.h.Run("dashboard.focus_next", nil); err != nil {
		t.Fatal(err)
	}
	if err := r.h.Run("dashboard.activate", nil); err != nil {
		t.Fatal(err)
	}
	if got := r.host.Cmd().Ran(); !slices.Equal(got, []string{"open_settings", "toggle_file_explorer"}) {
		t.Errorf("actions run = %v, want the second action", got)
	}
	if err := r.h.Run("dashboard.close", nil); err != nil {
		t.Fatal(err)
	}
	if id := dashboardBuffer(r.host); id != 0 {
		t.Errorf("dashboard %d is still open after close", id)
	}
}

func TestDashboardRecentFilesOpenAndClose(t *testing.T) {
	t.Parallel()
	base := []rigBuffer{{"/w/a.txt", "a\n"}, {"/w/b.txt", "b\n"}}
	r := dashboardRig(t, base)
	// Open a.txt through the event path so the plugin records it as recent.
	if err := r.host.Activate(r.ids["/w/a.txt"]); err != nil {
		t.Fatal(err)
	}
	r.emit("after_file_open", "/w/a.txt", nil)
	r.h.Advance(50 * 1000000)
	r.h.Settle()
	if err := r.h.Run("dashboard.show", nil); err != nil {
		t.Fatal(err)
	}
	text := dashboardText(t, r.host)
	if !strings.Contains(text, "Recent") || !strings.Contains(text, "a.txt") {
		t.Fatalf("dashboard has no recent file row:\n%s", text)
	}
	// The first focusable row is the recent file: activating it opens a.txt.
	if err := r.h.Run("dashboard.activate", nil); err != nil {
		t.Fatal(err)
	}
	r.h.CompleteBuffers()
	if got := r.host.ActiveBuffer(); got != r.ids["/w/a.txt"] {
		t.Errorf("active buffer = %d, want a.txt %d", got, r.ids["/w/a.txt"])
	}
	// A file opened after the automatic dashboard closes it again.
	r2 := dashboardRig(t, nil)
	dashboardReady(t, r2)
	if dashboardBuffer(r2.host) == 0 {
		t.Fatal("no auto dashboard")
	}
	b := r2.host.AddBuffer("/w/b.txt", "b"+"\n")
	r2.host.SetActive(b)
	r2.h.Emit(pluginapi.Event{Name: "after_file_open", Data: map[string]any{"buf": b, "path": "/w/b.txt"}})
	r2.h.Advance(50 * 1000000)
	r2.h.Settle()
	if id := dashboardBuffer(r2.host); id != 0 {
		t.Errorf("auto dashboard %d stayed open after a file opened", id)
	}
}

// TestDashboardRecentRowsAreLineTargets: the recent-file rows are clickable through
// gvim.buf.set_line_targets (a single click opens the file), while the command rows cannot
// be targets because a target opens a file, not an action. The keyboard mode is unchanged.
func TestDashboardRecentRowsAreLineTargets(t *testing.T) {
	t.Parallel()
	r := dashboardRig(t, []rigBuffer{{"/w/a.txt", "a\n"}})
	if err := r.host.Activate(r.ids["/w/a.txt"]); err != nil {
		t.Fatal(err)
	}
	r.emit("after_file_open", "/w/a.txt", nil)
	r.h.Advance(50 * 1000000)
	r.h.Settle()
	if err := r.h.Run("dashboard.show", nil); err != nil {
		t.Fatal(err)
	}
	id := dashboardBuffer(r.host)
	if id == 0 {
		t.Fatal("no dashboard")
	}
	entries, _ := r.host.VirtualContent(id)
	lines := strings.Split(contentText(entries), "\n")
	rowLine := -1
	for i, line := range lines {
		if strings.Contains(line, "a.txt") {
			rowLine = i
			break
		}
	}
	if rowLine < 0 {
		t.Fatalf("no a.txt row:\n%s", dashboardText(t, r.host))
	}
	var recent, actions []pluginapi.LineTarget
	for _, t := range r.host.LineTargets(id) {
		switch {
		case t.Path != "":
			recent = append(recent, t)
		case t.Action != "":
			actions = append(actions, t)
		}
	}
	if len(recent) != 1 || recent[0].Path != "/w/a.txt" || recent[0].Line != rowLine {
		t.Fatalf("recent target = %+v (row line %d, want path /w/a.txt), all %+v", recent, rowLine, r.host.LineTargets(id))
	}
	// Action rows are clickable through commands this plugin owns; git_log is not
	// loaded here, so the branch-log row is an explicit disabled line instead.
	if len(actions) != 6 {
		t.Fatalf("action targets = %+v, want the six action rows", actions)
	}
	for i, a := range actions {
		want := "dashboard.click_" + strconv.Itoa(i+1)
		if a.Action != want {
			t.Errorf("action target %d = %q, want %q", i, a.Action, want)
		}
	}
	if text := dashboardText(t, r.host); !strings.Contains(text, "branch log") || !strings.Contains(text, "unavailable") {
		t.Fatalf("dashboard misses the disabled branch-log notice:\n%s", text)
	}
	// Closing the dashboard clears its targets.
	if err := r.h.Run("dashboard.close", nil); err != nil {
		t.Fatal(err)
	}
	if got := r.host.LineTargets(id); len(got) != 0 {
		t.Errorf("targets after close = %+v", got)
	}
}

// TestDashboardGitShowsNoUpstream: a branch without an upstream still renders the
// tracking row, keyed "no upstream" and muted, while the rest of the section works.
func TestDashboardGitShowsNoUpstream(t *testing.T) {
	t.Parallel()
	r := startRig(t, rigOpts{
		plugins: []string{dashboardPlugin},
		script:  dashboardNoUpstreamScript,
	})
	dashboardReady(t, r)
	text := dashboardText(t, r.host)
	if !strings.Contains(text, "tracking") || !strings.Contains(text, "no upstream") {
		t.Fatalf("dashboard text misses the no-upstream tracking row:\n%s", text)
	}
	if strings.Contains(text, "↑2 ↓4") {
		t.Fatalf("dashboard invented upstream counts:\n%s", text)
	}
}

// TestDashboardBranchLogAvailableWhenGitLogLoads: the branch-log row is only
// advertised when the git_log command is actually registered, and its click target
// runs that command through this plugin's own hidden command (a target can never
// dispatch another plugin directly).
func TestDashboardBranchLogAvailableWhenGitLogLoads(t *testing.T) {
	t.Parallel()
	provider := strings.Join([]string{
		"local gvim = require 'gvim'",
		"gvim.command.register{ name = \"show_git_log\", title = \"Git Log\", run = function() gvim.notify(\"branch-log-ran\") end }",
	}, "\n")
	r := startRig(t, rigOpts{
		plugins: []string{dashboardPlugin},
		script:  dashboardGitScript,
		extra:   map[string]string{"git_log/init.lua": provider},
	})
	dashboardReady(t, r)
	id := dashboardBuffer(r.host)
	if id == 0 {
		t.Fatal("no dashboard")
	}
	text := dashboardText(t, r.host)
	if !strings.Contains(text, "branch log") || strings.Contains(text, "unavailable") {
		t.Fatalf("branch-log row should be enabled while git_log is loaded:\n%s", text)
	}
	var branchTarget pluginapi.LineTarget
	for _, target := range r.host.LineTargets(id) {
		if target.Action == "dashboard.click_branch_log" {
			branchTarget = target
		}
	}
	if branchTarget.Action == "" {
		t.Fatalf("no branch-log action target: %+v", r.host.LineTargets(id))
	}
	if err := r.h.Run("dashboard.click_branch_log", nil); err != nil {
		t.Fatal(err)
	}
	r.h.Settle()
	found := false
	for _, n := range r.h.NoticeTexts() {
		found = found || strings.Contains(n, "branch-log-ran")
	}
	if !found {
		t.Fatalf("branch-log click did not run the git_log command: %v", r.h.NoticeTexts())
	}
}
