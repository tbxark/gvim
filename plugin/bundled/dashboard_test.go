package bundled_test

import (
	"encoding/json"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/tbxark/gvim/plugin"

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

// dashboardRemoteScript answers the dashboard's git reads with one remote URL and the
// feature/x branch, so the URL-normalizing rows can be tested.
func dashboardRemoteScript(remote string) func(*plugintest.FakeOS) {
	return func(o *plugintest.FakeOS) {
		o.Bin["git"] = "/usr/bin/git"
		o.Script(gitArgv("rev-parse", "--abbrev-ref", "HEAD"), plugintest.ProcScript{Stdout: "feature/x\n"})
		o.Script(gitArgv("remote", "get-url", "origin"), plugintest.ProcScript{Stdout: remote + "\n"})
		o.Script(gitArgv("status", "--porcelain=v1"), plugintest.ProcScript{Stdout: " M a.txt\n"})
		o.Script(gitArgv("symbolic-ref", "--short", "refs/remotes/origin/HEAD"), plugintest.ProcScript{Code: 1})
		o.Script(gitArgv("rev-parse", "--verify", "main"), plugintest.ProcScript{Code: 1})
		o.Script(gitArgv("rev-parse", "--verify", "master"), plugintest.ProcScript{Code: 1})
		o.Script(gitArgv("rev-list", "--left-right", "--count", "HEAD...@{upstream}"), plugintest.ProcScript{Code: 1})
	}
}

// TestDashboardURLRowsClickable: a GitHub origin normalizes to an https repo URL and a
// branch URL; the Git lines are clickable through dashboard-owned commands (so line
// targets stay within the plugin), keyboard-equivalent rows exist, and invoking the
// commands reaches gvim.url.open with the exact URLs.
func TestDashboardURLRowsClickable(t *testing.T) {
	t.Parallel()
	r := startRig(t, rigOpts{
		plugins: []string{dashboardPlugin},
		script:  dashboardRemoteScript("git@github.com:me/repo.git"),
	})
	dashboardReady(t, r)
	id := dashboardBuffer(r.host)
	if id == 0 {
		t.Fatal("no dashboard")
	}
	text := dashboardText(t, r.host)
	if !strings.Contains(text, "https://github.com/me/repo") {
		t.Fatalf("normalized repo URL missing:\n%s", text)
	}
	if !strings.Contains(text, "Open Repository") || !strings.Contains(text, "Open Branch") {
		t.Fatalf("keyboard-equivalent URL rows missing:\n%s", text)
	}
	lines := strings.Split(text, "\n")
	var repoLine, branchLine int
	for _, target := range r.host.LineTargets(id) {
		if target.Line < 0 || target.Line >= len(lines) {
			continue
		}
		line := lines[target.Line]
		if target.Action == "dashboard.click_repo_url" && strings.Contains(line, "https://github.com/me/repo") {
			repoLine = target.Line
		}
		if target.Action == "dashboard.click_branch_url" && strings.Contains(line, "feature/x") {
			branchLine = target.Line
		}
	}
	if repoLine == 0 || branchLine == 0 {
		t.Fatalf("clickable URL lines not found: repo=%d branch=%d targets=%+v", repoLine, branchLine, r.host.LineTargets(id))
	}
	if err := r.h.Run("dashboard.click_repo_url", nil); err != nil {
		t.Fatal(err)
	}
	if err := r.h.Run("dashboard.click_branch_url", nil); err != nil {
		t.Fatal(err)
	}
	r.h.Settle()
	want := []string{"https://github.com/me/repo", "https://github.com/me/repo/tree/feature%2Fx"}
	if got := r.host.URLs(); !slices.Equal(got, want) {
		t.Fatalf("opened URLs = %v, want %v", got, want)
	}
}

// TestDashboardURLNormalization checks the common remote forms and the unsafe/unopenable
// ones that must stay plain text (no click target, no URL row).
func TestDashboardURLNormalization(t *testing.T) {
	cases := []struct {
		remote   string
		wantRepo string
		branch   bool
	}{
		{"git@github.com:me/repo.git", "https://github.com/me/repo", true},
		{"https://github.com/me/repo.git", "https://github.com/me/repo", true},
		{"ssh://git@github.com/me/repo.git", "https://github.com/me/repo", true},
		{"https://example.com/me/repo", "https://example.com/me/repo", false},
		{"git@example.com:me/repo.git", "", false},
		{"javascript:alert(1)", "", false},
		{"/local/path", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.remote, func(t *testing.T) {
			r := startRig(t, rigOpts{
				plugins: []string{dashboardPlugin},
				script:  dashboardRemoteScript(tc.remote),
			})
			dashboardReady(t, r)
			id := dashboardBuffer(r.host)
			if id == 0 {
				t.Fatal("no dashboard")
			}
			text := dashboardText(t, r.host)
			var repo, branch bool
			for _, target := range r.host.LineTargets(id) {
				repo = repo || target.Action == "dashboard.click_repo_url"
				branch = branch || target.Action == "dashboard.click_branch_url"
			}
			if tc.wantRepo == "" {
				if repo || branch || strings.Contains(text, "Open Repository") {
					t.Fatalf("unsafe/unopenable remote advertised a URL: %+v\n%s", r.host.LineTargets(id), text)
				}
				return
			}
			if !repo || !strings.Contains(text, tc.wantRepo) {
				t.Fatalf("repo URL %q missing (targets %+v):\n%s", tc.wantRepo, r.host.LineTargets(id), text)
			}
			if branch != tc.branch {
				t.Fatalf("branch target = %v, want %v (targets %+v)", branch, tc.branch, r.host.LineTargets(id))
			}
		})
	}
}

// githubTransport answers the GitHub PR request with a fixed response and records the
// requests, so the dashboard tests never touch the network.
type githubTransport struct {
	mu       sync.Mutex
	requests []*url.URL
	status   int
	body     string
	err      error
}

func (g *githubTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	g.mu.Lock()
	g.requests = append(g.requests, req.URL)
	g.mu.Unlock()
	if g.err != nil {
		return nil, g.err
	}
	status := g.status
	if status == 0 {
		status = 200
	}
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(g.body)),
		Request:    req,
	}, nil
}

func (g *githubTransport) count() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return len(g.requests)
}

func (g *githubTransport) first() *url.URL {
	g.mu.Lock()
	defer g.mu.Unlock()
	if len(g.requests) == 0 {
		return nil
	}
	return g.requests[0]
}

func dashboardGitHubSettings(network bool) map[string]plugin.PluginSettings {
	return map[string]plugin.PluginSettings{
		dashboardPlugin: {User: map[string]any{"github_prs": true, "network": network}},
	}
}

// dashboardSettle waits for the dashboard's async git/HTTP refresh to paint the wanted
// text, running Work and the plugin pump in between.
func dashboardSettle(t *testing.T, r *rig, want string) string {
	t.Helper()
	var text string
	for i := 0; i < 40; i++ {
		r.h.Settle()
		text = dashboardText(t, r.host)
		if strings.Contains(text, want) {
			return text
		}
	}
	return text
}

// TestDashboardGitHubOptIn: with github_prs + network in the user settings and a GitHub
// origin, the dashboard makes one bounded REST request and renders the PR rows.
func TestDashboardGitHubOptIn(t *testing.T) {
	t.Parallel()
	tr := &githubTransport{body: `[{"number":12,"title":"Fix the thing","state":"open"},{"number":34,"title":"Add docs","state":"open","draft":true}]`}
	r := startRig(t, rigOpts{
		plugins:    []string{dashboardPlugin},
		script:     dashboardRemoteScript("https://github.com/me/repo"),
		settings:   dashboardGitHubSettings(true),
		httpClient: &http.Client{Transport: tr},
	})
	dashboardReady(t, r)
	text := dashboardSettle(t, r, "#12")
	if !strings.Contains(text, "GitHub") || !strings.Contains(text, "#12") || !strings.Contains(text, "Fix the thing") {
		t.Fatalf("dashboard text misses the PR section:\n%s", text)
	}
	if tr.count() != 1 {
		t.Fatalf("GitHub requests = %d, want one", tr.count())
	}
	u := tr.first()
	if u == nil || u.Host != "api.github.com" || !strings.Contains(u.Path, "/repos/me/repo/pulls") || u.Query().Get("state") != "open" || u.Query().Get("per_page") != "6" {
		t.Fatalf("GitHub request = %v", u)
	}
}

// TestDashboardGitHubDisabled: without the explicit github_prs setting there is no GitHub
// section and no request at all.
func TestDashboardGitHubDisabled(t *testing.T) {
	t.Parallel()
	tr := &githubTransport{body: `[]`}
	r := startRig(t, rigOpts{
		plugins:    []string{dashboardPlugin},
		script:     dashboardRemoteScript("https://github.com/me/repo"),
		httpClient: &http.Client{Transport: tr},
	})
	dashboardReady(t, r)
	text := dashboardText(t, r.host)
	if strings.Contains(text, "GitHub") {
		t.Fatalf("the GitHub section rendered without opt-in:\n%s", text)
	}
	if tr.count() != 0 {
		t.Fatalf("GitHub requests = %d without opt-in", tr.count())
	}
}

// TestDashboardGitHubNetworkDisabled: github_prs without the host network setting is a
// silent unavailable section, and no request is attempted.
func TestDashboardGitHubNetworkDisabled(t *testing.T) {
	t.Parallel()
	tr := &githubTransport{body: `[]`}
	r := startRig(t, rigOpts{
		plugins:    []string{dashboardPlugin},
		script:     dashboardRemoteScript("https://github.com/me/repo"),
		settings:   dashboardGitHubSettings(false),
		httpClient: &http.Client{Transport: tr},
	})
	dashboardReady(t, r)
	text := dashboardSettle(t, r, "network disabled")
	if !strings.Contains(text, "GitHub") || !strings.Contains(text, "network disabled") {
		t.Fatalf("dashboard text misses the network-disabled state:\n%s", text)
	}
	if tr.count() != 0 {
		t.Fatalf("a request escaped the disabled network gate: %d", tr.count())
	}
	if len(r.h.NoticeTexts()) != 0 {
		t.Fatalf("the disabled section raised notices: %v", r.h.NoticeTexts())
	}
}

// TestDashboardGitHubUnavailable: a private/missing repo (404) or any failure shows one
// muted unavailable line, never a notice, and the rest of the dashboard renders.
func TestDashboardGitHubUnavailable(t *testing.T) {
	t.Parallel()
	tr := &githubTransport{status: 404, body: `{"message":"Not Found"}`}
	r := startRig(t, rigOpts{
		plugins:    []string{dashboardPlugin},
		script:     dashboardRemoteScript("https://github.com/me/repo"),
		settings:   dashboardGitHubSettings(true),
		httpClient: &http.Client{Transport: tr},
	})
	dashboardReady(t, r)
	text := dashboardSettle(t, r, "unavailable")
	if !strings.Contains(text, "GitHub") || !strings.Contains(text, "unavailable") {
		t.Fatalf("dashboard text misses the unavailable state:\n%s", text)
	}
	if !strings.Contains(text, "Open Settings") {
		t.Fatalf("the rest of the dashboard did not render:\n%s", text)
	}
	if len(r.h.NoticeTexts()) != 0 {
		t.Fatalf("a failed GitHub fetch raised notices: %v", r.h.NoticeTexts())
	}
}

// dashboardPRTargets returns the line targets whose action is a PR click command, in
// dashboard line order.
func dashboardPRTargets(host *plugintest.FakeHost, id pluginapi.BufferID) []pluginapi.LineTarget {
	var targets []pluginapi.LineTarget
	for _, target := range host.LineTargets(id) {
		if strings.HasPrefix(target.Action, "dashboard.click_pr_") {
			targets = append(targets, target)
		}
	}
	return targets
}

// TestDashboardGitHubPRRowsClickable: PR rows are action rows (so Tab/Enter reaches
// them), publish dashboard-owned action targets on their lines, and dispatch the same
// canonical pull URL through gvim.url.open as a click.
func TestDashboardGitHubPRRowsClickable(t *testing.T) {
	t.Parallel()
	tr := &githubTransport{body: `[{"number":12,"title":"Fix the thing"},{"number":34,"title":"Add docs","draft":true}]`}
	r := startRig(t, rigOpts{
		plugins:    []string{dashboardPlugin},
		script:     dashboardRemoteScript("https://github.com/me/repo"),
		settings:   dashboardGitHubSettings(true),
		httpClient: &http.Client{Transport: tr},
	})
	dashboardReady(t, r)
	text := dashboardSettle(t, r, "#12")
	id := dashboardBuffer(r.host)
	if id == 0 {
		t.Fatal("no dashboard")
	}
	if !strings.Contains(text, "#12") || !strings.Contains(text, "#34") {
		t.Fatalf("PR rows missing: %s", text)
	}
	targets := dashboardPRTargets(r.host, id)
	if len(targets) != 2 {
		t.Fatalf("PR action targets = %+v, want two", targets)
	}
	lines := strings.Split(text, "\n")
	found := map[string]bool{}
	for _, target := range targets {
		if target.Line < 0 || target.Line >= len(lines) {
			continue
		}
		line := lines[target.Line]
		if target.Action == "dashboard.click_pr_1" && strings.Contains(line, "#12") {
			found["12"] = true
		}
		if target.Action == "dashboard.click_pr_2" && strings.Contains(line, "#34") {
			found["34"] = true
		}
	}
	if !found["12"] || !found["34"] {
		t.Fatalf("PR action line targets are missing (#12=%v #34=%v targets=%+v): %s", found["12"], found["34"], targets, text)
	}
	// The click path and the keyboard row both dispatch the dashboard-owned command.
	if err := r.h.Run("dashboard.click_pr_1", nil); err != nil {
		t.Fatal(err)
	}
	if err := r.h.Run("dashboard.activate", nil); err != nil {
		t.Fatal(err)
	}
	r.h.Settle()
	want := []string{"https://github.com/me/repo/pull/12", "https://github.com/me/repo/pull/12"}
	if got := r.host.URLs(); !slices.Equal(got, want) {
		t.Fatalf("opened URLs = %v, want %v", got, want)
	}
}

// TestDashboardGitHubPRsIgnoreUntrustedFields: a hostile or malformed REST body cannot
// inject another host/scheme through html_url, non-numeric/invalid numbers are skipped,
// and titles lose control characters and overlong tails before painting.
func TestDashboardGitHubPRsIgnoreUntrustedFields(t *testing.T) {
	t.Parallel()
	body := `[
 {"number":12,"title":"Fix\n<script>alert(1)</script>","html_url":"javascript:alert(1)"},
 {"number":"13","title":"string number","html_url":"https://evil.example/string"},
 {"number":13.5,"title":"fraction","html_url":"https://evil.example/fraction"},
 {"number":-1,"title":"negative","html_url":"https://evil.example/negative"},
 {"number":9999999999,"title":"huge","html_url":"https://evil.example/huge"},
 {"number":14,"title":{"nested":"object"},"html_url":"https://evil.example/nested"},
 {"number":15,"title":"ok one","html_url":"https://evil.example/15"},
 {"number":16,"title":"control\u0000\u0001title","draft":false}
]`
	tr := &githubTransport{body: body}
	r := startRig(t, rigOpts{
		plugins:    []string{dashboardPlugin},
		script:     dashboardRemoteScript("https://github.com/me/repo"),
		settings:   dashboardGitHubSettings(true),
		httpClient: &http.Client{Transport: tr},
	})
	dashboardReady(t, r)
	text := dashboardSettle(t, r, "#12")
	if strings.Contains(text, "evil.example") || strings.Contains(text, "javascript:") {
		t.Fatalf("an API-provided URL reached the dashboard: %s", text)
	}
	for _, want := range []string{"#12", "#14", "#15", "#16"} {
		if !strings.Contains(text, want) {
			t.Errorf("valid PR %s missing: %s", want, text)
		}
	}
	for _, bad := range []string{"#13", "#-1", "#9999999999"} {
		if strings.Contains(text, bad) {
			t.Errorf("invalid PR %s was rendered: %s", bad, text)
		}
	}
	if !strings.Contains(text, "Fix <script>alert(1)</script>") {
		t.Errorf("title newline was not normalized: %s", text)
	}
	if !strings.Contains(text, "control title") {
		t.Errorf("control characters were not normalized: %s", text)
	}
	// The only URLs opened are canonical GitHub pulls, from the slug the dashboard
	// normalized itself and the validated number.
	for i := 1; i <= 4; i++ {
		if err := r.h.Run("dashboard.click_pr_"+strconv.Itoa(i), nil); err != nil {
			t.Fatal(err)
		}
	}
	r.h.Settle()
	want := []string{
		"https://github.com/me/repo/pull/12",
		"https://github.com/me/repo/pull/14",
		"https://github.com/me/repo/pull/15",
		"https://github.com/me/repo/pull/16",
	}
	if got := r.host.URLs(); !slices.Equal(got, want) {
		t.Fatalf("opened URLs = %v, want %v", got, want)
	}
}

// TestDashboardGitHubPRRowCap: the section renders at most GITHUB_MAX_ROWS valid PRs
// even when the response is larger.
func TestDashboardGitHubPRRowCap(t *testing.T) {
	t.Parallel()
	var entries []string
	for i := 1; i <= 40; i++ {
		entries = append(entries, `{"number":`+strconv.Itoa(i)+`,"title":"pr `+strconv.Itoa(i)+`"}`)
	}
	body := "[" + strings.Join(entries, ",") + "]"
	tr := &githubTransport{body: body}
	r := startRig(t, rigOpts{
		plugins:    []string{dashboardPlugin},
		script:     dashboardRemoteScript("https://github.com/me/repo"),
		settings:   dashboardGitHubSettings(true),
		httpClient: &http.Client{Transport: tr},
	})
	dashboardReady(t, r)
	dashboardSettle(t, r, "#1")
	id := dashboardBuffer(r.host)
	if id == 0 {
		t.Fatal("no dashboard")
	}
	if targets := dashboardPRTargets(r.host, id); len(targets) != 6 {
		t.Fatalf("PR targets = %d, want the six-row cap (%+v)", len(targets), targets)
	}
}

func dashboardWeatherSettings(network bool, extra map[string]any) map[string]plugin.PluginSettings {
	user := map[string]any{"weather": true, "network": network}
	for k, v := range extra {
		user[k] = v
	}
	return map[string]plugin.PluginSettings{dashboardPlugin: {User: user}}
}

const dashboardWeatherJSON = "{\"current_condition\":[{\"temp_C\":\"21\",\"temp_F\":\"70\",\"FeelsLikeC\":\"20\",\"FeelsLikeF\":\"68\",\"windspeedKmph\":\"12\",\"windspeedMiles\":\"7\",\"humidity\":\"55\",\"weatherDesc\":[{\"value\":\"Sunny\"}]}],\"weather\":[{\"mintempC\":\"15\",\"maxtempC\":\"24\",\"mintempF\":\"59\",\"maxtempF\":\"75\",\"hourly\":[{\"time\":\"0\",\"tempC\":\"15\",\"tempF\":\"59\",\"FeelsLikeC\":\"14\",\"FeelsLikeF\":\"57\",\"weatherDesc\":[{\"value\":\"Clear\"}]},{\"time\":\"1200\",\"tempC\":\"23\",\"tempF\":\"73\",\"weatherDesc\":[{\"value\":\"Sunny\"}]},{\"time\":\"1800\",\"tempC\":\"20\",\"tempF\":\"68\",\"FeelsLikeC\":\"19\",\"FeelsLikeF\":\"66\",\"weatherDesc\":[{\"value\":\"Cloudy\"}]}]},{\"mintempC\":\"16\",\"maxtempC\":\"25\",\"mintempF\":\"61\",\"maxtempF\":\"77\",\"hourly\":[{\"time\":\"1200\",\"weatherDesc\":[{\"value\":\"Rain\"}]}]}]}"

// TestDashboardWeatherOptIn: weather + network in the user settings with a location makes
// one encoded wttr.in j1 request and renders Fresh's now/5pm/tomorrow rows.
func TestDashboardWeatherOptIn(t *testing.T) {
	t.Parallel()
	tr := &githubTransport{body: dashboardWeatherJSON}
	r := startRig(t, rigOpts{
		plugins:    []string{dashboardPlugin},
		script:     dashboardRemoteScript("https://github.com/me/repo"),
		settings:   dashboardWeatherSettings(true, map[string]any{"weather_location": "New York"}),
		httpClient: &http.Client{Transport: tr},
	})
	dashboardReady(t, r)
	text := dashboardSettle(t, r, "Weather")
	for _, want := range []string{"Weather", "now", "Sunny", "21°C", "feels 20°C", "5pm", "Cloudy", "20°C", "tomorrow", "16°..25°C", "Rain"} {
		if !strings.Contains(text, want) {
			t.Fatalf("weather text missing %q:\n%s", want, text)
		}
	}
	if tr.count() != 1 {
		t.Fatalf("weather requests = %d, want one", tr.count())
	}
	u := tr.first()
	if u == nil || u.Host != "wttr.in" || u.EscapedPath() != "/New%20York" || u.Query().Get("format") != "j1" {
		t.Fatalf("weather request = %v", u)
	}
}

// TestDashboardWeatherImperial: the units setting selects Fahrenheit/mph fields.
func TestDashboardWeatherImperial(t *testing.T) {
	t.Parallel()
	tr := &githubTransport{body: dashboardWeatherJSON}
	r := startRig(t, rigOpts{
		plugins:    []string{dashboardPlugin},
		script:     dashboardRemoteScript("https://github.com/me/repo"),
		settings:   dashboardWeatherSettings(true, map[string]any{"weather_units": "imperial"}),
		httpClient: &http.Client{Transport: tr},
	})
	dashboardReady(t, r)
	text := dashboardSettle(t, r, "70°F")
	for _, want := range []string{"70°F", "feels 68°F", "7 mph", "55%", "68°F", "61°..77°F", "Rain"} {
		if !strings.Contains(text, want) {
			t.Fatalf("imperial weather text missing %q:\n%s", want, text)
		}
	}
}

// TestDashboardWeatherDisabled: without the explicit weather setting there is no Weather
// section and no request at all.
func TestDashboardWeatherDisabled(t *testing.T) {
	t.Parallel()
	tr := &githubTransport{body: dashboardWeatherJSON}
	r := startRig(t, rigOpts{
		plugins:    []string{dashboardPlugin},
		script:     dashboardRemoteScript("https://github.com/me/repo"),
		httpClient: &http.Client{Transport: tr},
	})
	dashboardReady(t, r)
	text := dashboardText(t, r.host)
	if strings.Contains(text, "Weather") {
		t.Fatalf("the Weather section rendered without opt-in:\n%s", text)
	}
	if tr.count() != 0 {
		t.Fatalf("weather requests = %d without opt-in", tr.count())
	}
}

// TestDashboardWeatherNetworkDisabled: weather enabled but the host network gate off is a
// muted network-disabled line, no request and no notice.
func TestDashboardWeatherNetworkDisabled(t *testing.T) {
	t.Parallel()
	tr := &githubTransport{body: dashboardWeatherJSON}
	r := startRig(t, rigOpts{
		plugins:    []string{dashboardPlugin},
		script:     dashboardRemoteScript("https://github.com/me/repo"),
		settings:   dashboardWeatherSettings(false, nil),
		httpClient: &http.Client{Transport: tr},
	})
	dashboardReady(t, r)
	text := dashboardSettle(t, r, "network disabled")
	if !strings.Contains(text, "Weather") || !strings.Contains(text, "network disabled") {
		t.Fatalf("weather network-disabled text missing:\n%s", text)
	}
	if tr.count() != 0 {
		t.Fatalf("a request escaped the disabled network gate: %d", tr.count())
	}
	if len(r.h.NoticeTexts()) != 0 {
		t.Fatalf("the disabled weather section raised notices: %v", r.h.NoticeTexts())
	}
}

// TestDashboardWeatherUnavailable: a 500 or malformed body is one muted line, no notice;
// the rest of the dashboard still renders.
func TestDashboardWeatherUnavailable(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		status int
		body   string
		want   string
	}{
		"http error": {status: 500, body: "oops", want: "unavailable"},
		"malformed":  {status: 200, body: "{not json", want: "invalid response"},
	} {
		tc := tc
		t.Run(name, func(t *testing.T) {
			tr := &githubTransport{status: tc.status, body: tc.body}
			r := startRig(t, rigOpts{
				plugins:    []string{dashboardPlugin},
				script:     dashboardRemoteScript("https://github.com/me/repo"),
				settings:   dashboardWeatherSettings(true, nil),
				httpClient: &http.Client{Transport: tr},
			})
			dashboardReady(t, r)
			text := dashboardSettle(t, r, tc.want)
			if !strings.Contains(text, "Weather") || !strings.Contains(text, tc.want) {
				t.Fatalf("weather failure state missing:\n%s", text)
			}
			if !strings.Contains(text, "Open Settings") {
				t.Fatalf("the rest of the dashboard did not render:\n%s", text)
			}
			if len(r.h.NoticeTexts()) != 0 {
				t.Fatalf("a failed weather fetch raised notices: %v", r.h.NoticeTexts())
			}
		})
	}
}

func TestDashboardAutoOpenOverride(t *testing.T) {
	t.Run("unset override keeps show_on_empty false", func(t *testing.T) {
		r := startRig(t, rigOpts{
			plugins:  []string{dashboardPlugin},
			settings: map[string]plugin.PluginSettings{dashboardPlugin: {User: map[string]any{"show_on_empty": false}}},
			script:   dashboardRemoteScript("https://github.com/me/repo"),
		})
		dashboardReady(t, r)
		if dashboardBuffer(r.host) != 0 {
			t.Fatalf("dashboard auto-opened despite show_on_empty=false with no override")
		}
	})

	t.Run("override true enables auto-open despite show_on_empty false", func(t *testing.T) {
		r := startRig(t, rigOpts{
			plugins:  []string{dashboardPlugin},
			probe:    "\nlocal gvim = require 'gvim'\ngvim.command.register { name = 'auto_on', run = function() gvim.dashboard.set_auto_open(true) end }\ngvim.command.register { name = 'auto_off', run = function() gvim.dashboard.set_auto_open(false) end }\ngvim.command.register {\n  name = 'auto_read',\n  run = function() gvim.notify('auto=' .. tostring(gvim.dashboard.auto_open())) end,\n}\n",
			settings: map[string]plugin.PluginSettings{dashboardPlugin: {User: map[string]any{"show_on_empty": false}}},
			script:   dashboardRemoteScript("https://github.com/me/repo"),
		})
		if err := r.h.Run("probe.auto_on", nil); err != nil {
			t.Fatal(err)
		}
		dashboardReady(t, r)
		if dashboardBuffer(r.host) == 0 {
			t.Fatalf("dashboard did not auto-open with a true session override (log %v)", r.h.LogText(""))
		}
	})

	t.Run("override false disables auto-open and can be re-enabled", func(t *testing.T) {
		r := startRig(t, rigOpts{
			plugins: []string{dashboardPlugin},
			probe:   "\nlocal gvim = require 'gvim'\ngvim.command.register { name = 'auto_on', run = function() gvim.dashboard.set_auto_open(true) end }\ngvim.command.register { name = 'auto_off', run = function() gvim.dashboard.set_auto_open(false) end }\ngvim.command.register {\n  name = 'auto_read',\n  run = function() gvim.notify('auto=' .. tostring(gvim.dashboard.auto_open())) end,\n}\n",
			script:  dashboardRemoteScript("https://github.com/me/repo"),
		})
		if err := r.h.Run("probe.auto_off", nil); err != nil {
			t.Fatal(err)
		}
		if err := r.h.Run("probe.auto_read", nil); err != nil {
			t.Fatal(err)
		}
		if got := lastNoticeText(r.h); got != "auto=false" {
			t.Fatalf("auto_open override = %q, want false", got)
		}
		dashboardReady(t, r)
		if dashboardBuffer(r.host) != 0 {
			t.Fatalf("dashboard auto-opened with a false session override")
		}
		if err := r.h.Run("probe.auto_on", nil); err != nil {
			t.Fatal(err)
		}
		r.h.Emit(pluginapi.Event{Name: "ready", Data: map[string]any{}})
		r.h.Settle()
		if dashboardBuffer(r.host) == 0 {
			t.Fatalf("dashboard did not auto-open after re-enabling the override (log %v)", r.h.LogText(""))
		}
	})
}
