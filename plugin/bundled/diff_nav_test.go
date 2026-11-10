package bundled_test

import (
	"encoding/json"
	"io/fs"
	"strings"
	"testing"

	"github.com/tbxark/gvim/plugin/bundled"
	"github.com/tbxark/gvim/plugin/plugintest"
	"github.com/tbxark/gvim/pluginapi"
)

// diffProvider builds a tiny plugin that exports a hunks function returning src.
func diffProvider(src string) string {
	return "local gvim = require \"gvim\"\n" +
		"gvim.plugins.export {\n" +
		"  hunks = function(id)\n" +
		"    return " + src + "\n" +
		"  end,\n" +
		"}\n"
}

// diffRig loads diff_nav with one buffer and optional fake git_gutter/live_diff exports.
func diffRig(t *testing.T, text string, gitHunks, liveHunks string) *rig {
	t.Helper()
	extra := map[string]string{}
	if gitHunks != "" {
		extra["git_gutter/init.lua"] = diffProvider(gitHunks)
	}
	if liveHunks != "" {
		extra["live_diff/init.lua"] = diffProvider(liveHunks)
	}
	return startRig(t, rigOpts{
		plugins: []string{"diff_nav"},
		buffers: []rigBuffer{{"/work/a.txt", text}},
		extra:   extra,
		script:  func(o *plugintest.FakeOS) { o.SetDir("/work") },
	})
}

func diffCursorLine(t *testing.T, r *rig) int {
	t.Helper()
	carets, err := r.host.Cursors(r.ids["/work/a.txt"])
	if err != nil || len(carets) == 0 {
		t.Fatalf("cursors = %+v, %v", carets, err)
	}
	return carets[0].Pos.Line
}

func diffRun(t *testing.T, r *rig, command string) string {
	t.Helper()
	if err := r.h.Run(command, nil); err != nil {
		t.Fatal(err)
	}
	return lastNoticeText(r.h)
}

func TestDiffNavGermanNavigationNotices(t *testing.T) {
	r := startRig(t, rigOpts{
		locale:  "de",
		plugins: []string{"diff_nav"},
		buffers: []rigBuffer{{"/work/a.txt", "a\nb\nc\n"}},
		extra:   map[string]string{"live_diff/init.lua": diffProvider("{ { newStart = 1 } }")},
		script:  func(o *plugintest.FakeOS) { o.SetDir("/work") },
	})
	if got := diffRun(t, r, "diff_nav.next_change"); got != "Änderung 1/1" {
		t.Fatalf("notice = %q", got)
	}
	if got := diffRun(t, r, "diff_nav.next_change"); got != "Änderung 1/1 [umgebrochen]" {
		t.Fatalf("wrapped notice = %q", got)
	}
}

func TestDiffNavJumpFailureNotice(t *testing.T) {
	r := diffRig(t, "a\nb\nc\n", "", "{ { newStart = 1 } }")
	r.host.SetCursorErr = fs.ErrPermission
	if err := r.h.Run("diff_nav.next_change", nil); err != nil {
		t.Fatal(err)
	}
	notices := r.h.NoticeTexts()
	if len(notices) == 0 || !strings.Contains(notices[len(notices)-1], "Could not jump to the change") {
		t.Fatalf("notices = %v, want jump failure notice", notices)
	}
}

func TestDiffNavManifestAndCommands(t *testing.T) {
	t.Parallel()
	data, err := fs.ReadFile(bundled.FS, "plugins/diff_nav/plugin.json")
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	name, _ := m["name"].(string)
	version, _ := m["version"].(string)
	description, _ := m["description"].(string)
	api, _ := m["api"].(float64)
	defaultEnabled, _ := m["default_enabled"].(bool)
	if name != "diff_nav" || version == "" || description == "" || api != 2 {
		t.Errorf("manifest = %+v", m)
	}
	if defaultEnabled {
		t.Errorf("default_enabled = %v, want false: gvim keeps diff_nav opt-in for the idle-heap gate (Fresh ships it default-enabled)", m["default_enabled"])
	}
	r := diffRig(t, "l0\nl1\n", "", "")
	if info, ok := r.h.Plugin("diff_nav"); !ok || info.Status != "loaded" {
		t.Fatalf("plugin = %+v (log %v)", info, r.h.LogText(""))
	}
	want := map[string]string{
		"diff_nav.next_change": "Next Change",
		"diff_nav.prev_change": "Previous Change",
	}
	got := map[string]string{}
	for _, c := range r.host.Commands["diff_nav"] {
		got[c.ID] = c.Title
	}
	for id, title := range want {
		if got[id] != title {
			t.Errorf("command %s = %q, want %q", id, got[id], title)
		}
	}
	if bindings := r.host.Bindings["diff_nav"]; len(bindings) != 0 {
		t.Errorf("bindings = %v, want none", bindings)
	}
}

func TestDiffNavMergesSourcesAndWraps(t *testing.T) {
	t.Parallel()
	// git_gutter reports line 3, live_diff line 1: navigation must sort by position.
	r := diffRig(t, "l0\nl1\nl2\nl3\nl4\n",
		"{{type=\"added\", line=3, count=1}}",
		"{{kind=\"modified\", newStart=1, newCount=1}}")
	if got := diffRun(t, r, "diff_nav.next_change"); got != "Change 1/2" {
		t.Fatalf("first next status = %q", got)
	}
	if got := diffCursorLine(t, r); got != 1 {
		t.Fatalf("first next line = %d, want 1", got)
	}
	if got := diffRun(t, r, "diff_nav.next_change"); got != "Change 2/2" {
		t.Fatalf("second next status = %q", got)
	}
	if got := diffCursorLine(t, r); got != 3 {
		t.Fatalf("second next line = %d, want 3", got)
	}
	if got := diffRun(t, r, "diff_nav.next_change"); got != "Change 1/2 [wrapped]" {
		t.Fatalf("wrapped next status = %q", got)
	}
	if got := diffCursorLine(t, r); got != 1 {
		t.Fatalf("wrapped next line = %d, want 1", got)
	}
	if got := diffRun(t, r, "diff_nav.prev_change"); got != "Change 2/2 [wrapped]" {
		t.Fatalf("wrapped prev status = %q", got)
	}
	if got := diffCursorLine(t, r); got != 3 {
		t.Fatalf("wrapped prev line = %d, want 3", got)
	}
	if got := diffRun(t, r, "diff_nav.prev_change"); got != "Change 1/2" {
		t.Fatalf("prev status = %q", got)
	}
	if got := diffCursorLine(t, r); got != 1 {
		t.Fatalf("prev line = %d, want 1", got)
	}
}

func TestDiffNavDeduplicatesTheSameLine(t *testing.T) {
	t.Parallel()
	r := diffRig(t, "l0\nl1\nl2\n",
		"{{type=\"modified\", line=2, count=1}}",
		"{{kind=\"modified\", newStart=2, newCount=1}}")
	if got := diffRun(t, r, "diff_nav.next_change"); got != "Change 1/1" {
		t.Fatalf("status = %q, want one deduplicated target", got)
	}
	if got := diffCursorLine(t, r); got != 2 {
		t.Fatalf("line = %d, want 2", got)
	}
}

func TestDiffNavSavedDiffWithoutProviders(t *testing.T) {
	t.Parallel()
	r := diffRig(t, "one\ntwo\nthree\n", "", "")
	r.edit("/work/a.txt", "one\nTWO\nthree\n")
	// The edit leaves the fake caret at the end; navigation starts from the top.
	if err := r.host.SetCursor(r.ids["/work/a.txt"], pluginapi.Pos{}, false, true, false); err != nil {
		t.Fatal(err)
	}
	if got := diffRun(t, r, "diff_nav.next_change"); got != "Change 1/1" {
		t.Fatalf("saved-diff status = %q", got)
	}
	if got := diffCursorLine(t, r); got != 1 {
		t.Fatalf("saved-diff line = %d, want 1", got)
	}
	if got := diffRun(t, r, "diff_nav.next_change"); got != "Change 1/1 [wrapped]" {
		t.Fatalf("saved-diff wrapped status = %q", got)
	}
}

func TestDiffNavLiveOnlyAndNoChanges(t *testing.T) {
	t.Parallel()
	r := diffRig(t, "l0\nl1\nl2\nl3\n", "", "{{kind=\"added\", newStart=2, newCount=1}}")
	if got := diffRun(t, r, "diff_nav.next_change"); got != "Change 1/1" {
		t.Fatalf("live-only status = %q", got)
	}
	if got := diffCursorLine(t, r); got != 2 {
		t.Fatalf("live-only line = %d, want 2", got)
	}

	clean := diffRig(t, "l0\nl1\n", "", "")
	if got := diffRun(t, clean, "diff_nav.next_change"); got != "No changes" {
		t.Fatalf("clean status = %q", got)
	}
	if got := diffCursorLine(t, clean); got != 0 {
		t.Fatalf("clean cursor moved to line %d", got)
	}
}

func TestDiffNavBrokenProviderDegrades(t *testing.T) {
	t.Parallel()
	extra := map[string]string{
		"git_gutter/init.lua": "local gvim = require \"gvim\"\ngvim.plugins.export { hunks = function(id) error(\"boom\") end }\n",
	}
	r := startRig(t, rigOpts{
		plugins: []string{"diff_nav"},
		buffers: []rigBuffer{{"/work/a.txt", "one\ntwo\nthree\n"}},
		extra:   extra,
		script:  func(o *plugintest.FakeOS) { o.SetDir("/work") },
	})
	r.edit("/work/a.txt", "one\nTWO\nthree\n")
	if err := r.host.SetCursor(r.ids["/work/a.txt"], pluginapi.Pos{}, false, true, false); err != nil {
		t.Fatal(err)
	}
	if got := diffRun(t, r, "diff_nav.next_change"); got != "Change 1/1" {
		t.Fatalf("broken-provider status = %q", got)
	}
	if got := diffCursorLine(t, r); got != 1 {
		t.Fatalf("broken-provider line = %d", got)
	}
}

func TestDiffNavPreviousFromEnd(t *testing.T) {
	t.Parallel()
	r := diffRig(t, "l0\nl1\nl2\nl3\n", "{{type=\"added\", line=1, count=1}}", "")
	if err := r.host.SetCursor(r.ids["/work/a.txt"], pluginapi.Pos{Line: 3, Col: 0}, false, true, false); err != nil {
		t.Fatal(err)
	}
	if got := diffRun(t, r, "diff_nav.prev_change"); got != "Change 1/1" {
		t.Fatalf("status = %q", got)
	}
	if got := diffCursorLine(t, r); got != 1 {
		t.Fatalf("line = %d, want 1", got)
	}
}

// TestDiffNavUsesRealGitGutterHunks: interoperability with the actual git_gutter plugin,
// not a fake export. The real plugin computes HEAD hunks from git show; diff_nav must
// consume its line, type and count without a shape adapter.
func TestDiffNavUsesRealGitGutterHunks(t *testing.T) {
	r := startRig(t, rigOpts{
		plugins: []string{"diff_nav", "git_gutter"},
		buffers: []rigBuffer{{"/work/a.txt", "a\nB\nc\nd\n"}},
		script: func(o *plugintest.FakeOS) {
			o.Script(showArgv("a.txt"), plugintest.ProcScript{Stdout: "a\nb\nc\nd\n"})
		},
	})
	r.emit("after_file_open", "/work/a.txt", nil)
	if got := diffRun(t, r, "diff_nav.next_change"); got != "Change 1/1" {
		t.Fatalf("real git_gutter status = %q (log %v)", got, r.h.LogText(""))
	}
	if got := diffCursorLine(t, r); got != 1 {
		t.Fatalf("real git_gutter line = %d, want 1", got)
	}
}

// TestDiffNavUsesRealLiveDiffHunks: interoperability with the actual live_diff plugin.
func TestDiffNavUsesRealLiveDiffHunks(t *testing.T) {
	r := startRig(t, rigOpts{
		plugins: []string{"diff_nav", "live_diff"},
		buffers: []rigBuffer{{"/work/a.txt", "a\nB\nc\nd\n"}},
		script: func(o *plugintest.FakeOS) {
			o.Script(showArgv("a.txt"), plugintest.ProcScript{Stdout: "a\nb\nc\nd\n"})
		},
	})
	if err := r.h.Run("live_diff.toggle_global", nil); err != nil {
		t.Fatal(err)
	}
	r.emit("after_file_open", "/work/a.txt", nil)
	if got := diffRun(t, r, "diff_nav.next_change"); got != "Change 1/1" {
		t.Fatalf("real live_diff status = %q (log %v)", got, r.h.LogText(""))
	}
	if got := diffCursorLine(t, r); got != 1 {
		t.Fatalf("real live_diff line = %d, want 1", got)
	}
}

// TestDiffNavSavedDeletionJumpsBeforeTheSeam: b:diff_saved reports an empty new side for a
// deletion; the standalone navigation must land on the line before the seam like the
// git_gutter merged navigation, not on the line after the removed text.
func TestDiffNavSavedDeletionJumpsBeforeTheSeam(t *testing.T) {
	r := diffRig(t, "a\nb\nc\nd\n", "", "")
	r.edit("/work/a.txt", "a\nc\nd\n")
	if err := r.host.SetCursor(r.ids["/work/a.txt"], pluginapi.Pos{}, false, true, false); err != nil {
		t.Fatal(err)
	}
	if got := diffRun(t, r, "diff_nav.next_change"); got != "Change 1/1 [wrapped]" {
		t.Fatalf("deletion saved-diff status = %q", got)
	}
	if got := diffCursorLine(t, r); got != 0 {
		t.Fatalf("deletion saved-diff line = %d, want 0 (line before the seam)", got)
	}
}

// TestDiffNavI18nEnglish checks the plugin-owned Fresh catalogue is byte-identical and
// complete for the commands/status rows this plugin uses.
func TestDiffNavI18nEnglish(t *testing.T) {
	data, err := fs.ReadFile(bundled.FS, "plugins/diff_nav/diff_nav.i18n.json")
	if err != nil {
		t.Fatal(err)
	}
	var catalog map[string]map[string]string
	if err := json.Unmarshal(data, &catalog); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"cmd.next_change":       "Next Change",
		"cmd.next_change_desc":  "Jump to the next changed region",
		"cmd.prev_change":       "Previous Change",
		"cmd.prev_change_desc":  "Jump to the previous changed region",
		"status.no_changes":     "No changes",
		"status.change":         "Change %{n}/%{total}",
		"status.change_wrapped": "Change %{n}/%{total} [wrapped]",
		"status.jump_failed":    "Could not jump to the change: %{error}",
	}
	en := catalog["en"]
	if len(en) != len(want) {
		t.Fatalf("en keys = %d, want %d", len(en), len(want))
	}
	for key, value := range want {
		if en[key] != value {
			t.Errorf("en[%s] = %q, want %q", key, en[key], value)
		}
	}
}

// TestDiffNavMergesRealProvidersAndSavedDiff exercises all three sources against the real
// git_gutter and live_diff plugins: HEAD changes come from both exporters, the unsaved
// edit comes from b:diff_saved, and the duplicate HEAD line is deduplicated.
func TestDiffNavMergesRealProvidersAndSavedDiff(t *testing.T) {
	r := startRig(t, rigOpts{
		plugins: []string{"diff_nav", "git_gutter", "live_diff"},
		buffers: []rigBuffer{{"/work/a.txt", "a\nB\nc\nd\n"}},
		script: func(o *plugintest.FakeOS) {
			o.Script(showArgv("a.txt"), plugintest.ProcScript{Stdout: "a\nb\nc\nd\n"})
		},
	})
	if err := r.h.Run("live_diff.toggle_global", nil); err != nil {
		t.Fatal(err)
	}
	r.emit("after_file_open", "/work/a.txt", nil)
	// Make the saved source independent of the HEAD diff without re-running the providers.
	r.edit("/work/a.txt", "a\nB\nC\nd\n")
	if err := r.host.SetCursor(r.ids["/work/a.txt"], pluginapi.Pos{}, false, true, false); err != nil {
		t.Fatal(err)
	}
	if got := diffRun(t, r, "diff_nav.next_change"); got != "Change 1/2" {
		t.Fatalf("real three-source first status = %q (log %v)", got, r.h.LogText(""))
	}
	if got := diffCursorLine(t, r); got != 1 {
		t.Fatalf("real three-source first line = %d, want 1", got)
	}
	if got := diffRun(t, r, "diff_nav.next_change"); got != "Change 2/2" {
		t.Fatalf("real three-source second status = %q", got)
	}
	if got := diffCursorLine(t, r); got != 2 {
		t.Fatalf("real three-source second line = %d, want 2", got)
	}
}

// TestDiffNavUsesRealGitGutterDeletedHunk covers the deleted-run line contract: the real
// git_gutter export places the jump on the line before the seam.
func TestDiffNavUsesRealGitGutterDeletedHunk(t *testing.T) {
	r := startRig(t, rigOpts{
		plugins: []string{"diff_nav", "git_gutter"},
		buffers: []rigBuffer{{"/work/a.txt", "a\nc\nd\n"}},
		script: func(o *plugintest.FakeOS) {
			o.Script(showArgv("a.txt"), plugintest.ProcScript{Stdout: "a\nb\nc\nd\n"})
		},
	})
	r.emit("after_file_open", "/work/a.txt", nil)
	if got := diffRun(t, r, "diff_nav.next_change"); got != "Change 1/1 [wrapped]" {
		t.Fatalf("real deleted-hunk status = %q (log %v)", got, r.h.LogText(""))
	}
	if got := diffCursorLine(t, r); got != 0 {
		t.Fatalf("real deleted-hunk line = %d, want 0", got)
	}
}

// TestDiffNavUsesRealCompareFilesHunks: an open Compare view is a real diff-provider
// source for diff_nav. The compare plugin exports the active comparison's hunks for each
// side, so diff_nav Next/Previous Change navigates the same changes as Compare: Next
// Difference, from the left buffer, without a separate stub source.
func TestDiffNavUsesRealCompareFilesHunks(t *testing.T) {
	r := startRig(t, rigOpts{
		plugins:  []string{"diff_nav", "compare_files"},
		services: &plugintest.FakeServices{},
		buffers: []rigBuffer{
			{compareLeftPath, "a\nb\nc"},
			{compareRightPath, "a\nX\nc"},
		},
		script: func(o *plugintest.FakeOS) {
			o.SetFile(compareLeftPath, "a\nb\nc")
			o.SetFile(compareRightPath, "a\nX\nc")
		},
	})
	if err := r.h.Run("compare_files.compare_files", nil); err != nil {
		t.Fatalf("Compare Files: %v", err)
	}
	for i, value := range []string{compareLeftPath, compareRightPath} {
		_ = i
		var prompt *plugintest.FakeOverlay
		for _, o := range r.host.UI().Opened() {
			if o.Kind == "input" {
				prompt = o
			}
		}
		if prompt == nil {
			t.Fatalf("no Compare Files input prompt (log %v)", r.h.LogText(""))
		}
		r.h.Resolve(pluginapi.Completion{Token: prompt.Token, Values: []any{value}})
		r.h.Settle()
	}
	if err := r.host.SetCursor(r.ids[compareLeftPath], pluginapi.Pos{}, false, true, false); err != nil {
		t.Fatal(err)
	}
	got := diffRun(t, r, "diff_nav.next_change")
	if got != "Change 1/1" {
		t.Fatalf("compare-interop status = %q (log %v)", got, r.h.LogText(""))
	}
	carets, err := r.host.Cursors(r.ids[compareLeftPath])
	if err != nil || len(carets) == 0 {
		t.Fatalf("compare-interop cursors = %+v, %v", carets, err)
	}
	if got := carets[0].Pos.Line; got != 1 {
		t.Fatalf("compare-interop line = %d, want the changed line 1", got)
	}

	// The right side of the same comparison is a source for its buffer too.
	r.host.SetActive(r.ids[compareRightPath])
	if err := r.host.SetCursor(r.ids[compareRightPath], pluginapi.Pos{}, false, true, false); err != nil {
		t.Fatal(err)
	}
	if got := diffRun(t, r, "diff_nav.next_change"); got != "Change 1/1" {
		t.Fatalf("compare-interop right status = %q (log %v)", got, r.h.LogText(""))
	}
	carets, err = r.host.Cursors(r.ids[compareRightPath])
	if err != nil || len(carets) == 0 {
		t.Fatalf("compare-interop right cursors = %+v, %v", carets, err)
	}
	if got := carets[0].Pos.Line; got != 1 {
		t.Fatalf("compare-interop right line = %d, want the changed line 1", got)
	}
}
