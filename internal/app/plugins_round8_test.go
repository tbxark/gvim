package app

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// Round-8 real-host smoke: live_diff against a real git repository (full tier-1c
// rendering) and git_log opened from the real palette with its group/splits, streamed
// detail and plugin folds. No fake OS: the plugins run their real git commands against a
// temporary repository.

// requireGit skips the test when the machine has no git binary (the plugins report that
// as unsupported, which the fake-host tests already cover).
func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git binary")
	}
}

// realGit runs one git command in dir with a fixed identity and returns its output.
func realGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@example.com",
		"GIT_CONFIG_NOSYSTEM=1", "GIT_TERMINAL_PROMPT=0",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

// commitFile writes one file in the repository and commits it.
func commitFile(t *testing.T, root, name, text, message string) string {
	t.Helper()
	path := filepath.Join(root, name)
	must(t, os.MkdirAll(filepath.Dir(path), 0o755))
	must(t, os.WriteFile(path, []byte(text), 0o644))
	realGit(t, root, "add", name)
	realGit(t, root, "commit", "-qm", message)
	return path
}

// newRealRepo initializes a repository at root and commits one file.
func newRealRepo(t *testing.T, root, name, text string) string {
	t.Helper()
	realGit(t, root, "init", "-q")
	return commitFile(t, root, name, text, "first commit")
}

// openReal opens a path through the real editor session.
func (s *pluginSession) openReal(path string) {
	s.t.Helper()
	must(s.t, s.rt.ed.OpenFile(path))
}

// rowShowing returns the first painted row that contains sub, or "".
func rowShowing(s *pluginSession, sub string) string {
	for _, row := range s.rows() {
		if strings.Contains(row, sub) {
			return row
		}
	}
	return ""
}

// TestRealHostLiveDiffVsHeadShowsTheDiff: the config-enabled, default-off plugin loads,
// its palette rows are present, and vs HEAD paints the changed line (gutter, background,
// word emphasis) plus, on the shipped tier-1c host, the deleted old-content virtual row
// above it.
func TestRealHostLiveDiffVsHeadShowsTheDiff(t *testing.T) {
	requireGit(t)
	isolateConfig(t)
	root := t.TempDir()
	// Head and worktree differ by one character in a long line, so the pair stays
	// "modified" (similarity >= Fresh's threshold) and draws the ~ gutter glyph.
	const head = "aaaaaaaaaaaaaaaaaaab"
	const edited = "aaaaaaaaaaaaaaaaaaac"
	path := newRealRepo(t, root, "notes.txt", head+"\n")
	must(t, os.WriteFile(path, []byte(edited+"\n"), 0o644))

	writeUserFile(t, "config.json", `{"plugins":{"live_diff":{"enabled":true}}}`)
	s := newSession(t, Options{Root: root})
	s.waitStatus("live_diff", "loaded")
	if !s.paletteHas("Live Diff: Toggle (Global)") {
		t.Fatalf("palette lacks the loaded plugin's row (log %v)", s.plug.Log())
	}
	s.openReal(path)
	s.settle(40 * time.Millisecond)
	if row := rowShowing(s, edited); row != "" && strings.Contains(row, "│~") {
		t.Fatalf("the disabled plugin already painted the gutter marker: %q", row)
	}

	s.runCommand("Live Diff: Toggle (Global)")
	s.runCommand("Live Diff: vs HEAD")
	s.settle(80 * time.Millisecond)
	if got := s.noticeText(); !strings.Contains(got, "comparing against HEAD") {
		t.Fatalf("notice = %q, want the vs HEAD status", got)
	}
	// The changed line's change-bar cell (between the pane border and the line
	// number) shows the modified glyph; post-EOF filler rows use ~ as well, so the
	// assertion is anchored to the edited line's own row.
	row := rowShowing(s, edited)
	if row == "" || !strings.Contains(row, "│~") {
		t.Fatalf("no gutter marker on the edited row %q (log %v)", row, s.plug.Log())
	}
	// Full tier-1c rendering: the deleted old word's line is painted as a virtual
	// row above the edited one, with the '-' gutter glyph.
	oldRow := rowShowing(s, head)
	if oldRow == "" {
		t.Fatalf("the old line row is missing on a host with decor_virtual_paint (log %v)", s.plug.Log())
	}
	if !strings.Contains(oldRow, "-") {
		t.Errorf("the old-content row %q lacks the deleted gutter glyph", oldRow)
	}
	oldIdx, newIdx := -1, -1
	for i, line := range s.rows() {
		if strings.Contains(line, head) && oldIdx < 0 {
			oldIdx = i
		}
		if strings.Contains(line, edited) {
			newIdx = i
		}
	}
	if oldIdx < 0 || newIdx < 0 || oldIdx >= newIdx {
		t.Errorf("old row index %d, edited row index %d: the old line must be above the edit", oldIdx, newIdx)
	}
	if p := s.pluginProblems("live_diff"); len(p) != 0 {
		t.Errorf("live_diff problems: %v", p)
	}
}

// TestRealHostGitLogFromThePalette opens Git Log from the real palette over a real
// repository and checks the group/splits, the commit rows and the streamed diff detail.
func TestRealHostGitLogFromThePalette(t *testing.T) {
	requireGit(t)
	isolateConfig(t)
	root := t.TempDir()
	path := newRealRepo(t, root, "notes.txt", "one\n")
	commitFile(t, root, "notes.txt", "one\ntwo\n", "second commit")
	// git_log ships default_enabled=false (memory budget); enable it for this session.
	writeUserFile(t, "config.json", `{"plugins":{"git_log":{"enabled":true}}}`)

	s := newSession(t, Options{Root: root})
	// The classic frame and the plugin group's split leave the log column too narrow at
	// 120 columns for both subjects to stay on one row, so the session is widened here;
	// the list is expected to show each subject whole.
	s.send(tea.WindowSizeMsg{Width: 200, Height: 50})
	s.waitStatus("git_log", "loaded")
	s.openReal(path)
	s.settle(40 * time.Millisecond)
	s.runCommand("Git Log")
	s.settle(400 * time.Millisecond)

	// The group is mounted with the log list; both commit subjects are rows.
	s.until("the commit list", func() bool {
		return s.screenHas("Commits:") && s.screenHas("second commit") && s.screenHas("first commit")
	})
	// The detail panel streams the selected commit's diff.
	s.until("the streamed diff", func() bool {
		return s.screenHas("diff --git") && s.screenHas("+two")
	})
	// The group's hint row proves the panel layout/toolbar is mounted, not just a buffer.
	if !s.screenHas("RET: detail") && !s.screenHas("RET: show") {
		t.Errorf("no log hint row; rows %v", s.rows())
	}
	// The plugin fold ranges for the file/hunk diff are painted by the pane: a fold
	// marker glyph shows at each range start in the detail gutter.
	if !s.screenHas("▾") && !s.screenHas("▸") && !s.screenHas("▼") && !s.screenHas("▶") {
		t.Errorf("no fold marker in the detail pane; rows %v", s.rows())
	}
	if p := s.pluginProblems("git_log"); len(p) != 0 {
		t.Errorf("git_log problems: %v", p)
	}
}

// TestRealHostDefaultOffPluginsInThePalette is the durable palette smoke for the round-7
// finding: a plugin whose manifest says default_enabled=false is loaded when the config
// enables it, and its registered commands are palette rows after the load settles. (The
// earlier "rows missing" report did not reproduce on the current tree once plugins_loaded
// was awaited; the remaining exclusion is by design: plugin commands whose when-clause
// fails now are omitted, not disabled, so a context-gated row can be absent.)
func TestRealHostDefaultOffPluginsInThePalette(t *testing.T) {
	isolateConfig(t)
	writeUserFile(t, "config.json",
		`{"plugins":{"live_diff":{"enabled":true},"git_log":{"enabled":true}}}`)
	s := newSession(t, Options{})
	s.waitStatus("live_diff", "loaded")
	s.waitStatus("git_log", "loaded")
	s.settle(40 * time.Millisecond)
	for _, q := range []string{
		"Live Diff: Toggle (Global)",
		"Live Diff: vs HEAD",
		"Git Log",
		"Git Log: Refresh",
	} {
		if !s.paletteHas(q) {
			t.Errorf("palette lacks %q (log %v)", q, s.plug.Log())
		}
	}
	// The runtime registered them too, not only the palette.
	for _, action := range []string{"live_diff.toggle_global", "live_diff.vs_head", "git_log.show_git_log"} {
		if !s.plug.Owns(action) {
			t.Errorf("runtime does not own %q", action)
		}
	}
	// "Git Log: Current File" is gated by has_buffer: with no file open it is omitted
	// by design (allCommands skips a failing when-clause). Open a file and it appears.
	if s.paletteHas("Git Log: Current File") {
		t.Errorf("a has_buffer-gated row is listed with no buffer open")
	}
	// A .txt file: a .go file would start the language server, whose go toolchain
	// writes its build cache under the isolated HOME and races the TempDir cleanup.
	s.openFile("a.txt", "text\n")
	s.settle(40 * time.Millisecond)
	if !s.paletteHas("Git Log: Current File") {
		t.Errorf("the has_buffer-gated row is missing with a buffer open")
	}
}
