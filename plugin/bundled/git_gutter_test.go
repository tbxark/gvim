package bundled_test

import (
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/tbxark/gvim/plugin/plugintest"
	"github.com/tbxark/gvim/pluginapi"
)

const gutterFile = "/w/a.txt"

// gutterRig opens gutterFile with text, scripts HEAD to be head and loads git_gutter.
func gutterRig(t *testing.T, head, text string) *rig {
	t.Helper()
	return startRig(t, rigOpts{
		plugins: []string{"git_gutter"},
		buffers: []rigBuffer{{gutterFile, text}},
		script: func(o *plugintest.FakeOS) {
			o.Script(showArgv("a.txt"), plugintest.ProcScript{Stdout: head})
		},
	})
}

func TestGitGutterMarks(t *testing.T) {
	tests := []struct {
		name         string
		head, buffer string
		gutter       []string // line:text:color
		scroll       []string // line (or from-to) of the marks
	}{
		{"unchanged", "a\nb\nc\n", "a\nb\nc\n", nil, nil},
		{"one modified line", "a\nb\nc\n", "a\nB\nc\n", []string{"1:│:change"}, []string{"1-2:change"}},
		{"lines added at the end", "a\nb\n", "a\nb\nc\nd\n", []string{"2:│:add", "3:│:add"}, []string{"2-4:add"}},
		{"line deleted: the mark is on the line before the seam", "a\nb\nc\n", "a\nc\n", []string{"0:▾:delete"}, []string{"0:delete"}},
		{"lines deleted at the top", "a\nb\n", "b\n", []string{"0:▾:delete"}, []string{"0:delete"}},
		{
			"fewer lines than before: modified, then a delete mark on the first changed row",
			"a\nb\nc\nd\n", "a\nX\nd\n",
			[]string{"1:│:change", "1:▾:delete"}, []string{"1-2:change", "1:delete"},
		},
		{
			"more lines than before: modified, then added",
			"a\nb\nd\n", "a\nX\nY\nZ\nd\n",
			[]string{"1:│:change", "2:│:add", "3:│:add"}, []string{"1-2:change", "2-4:add"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := gutterRig(t, tt.head, tt.buffer)
			r.emit("after_file_open", gutterFile, nil)
			if got := marks(r.decor("git_gutter", "git_gutter", gutterFile)); !slices.Equal(got, tt.gutter) {
				t.Errorf("gutter = %v, want %v", got, tt.gutter)
			}
			var scroll []string
			for _, it := range r.decor("git_gutter", "git_gutter_scroll", gutterFile) {
				if it.Kind != pluginapi.DecorScroll {
					t.Fatalf("scroll namespace holds %v", it)
				}
				if it.Range != (pluginapi.Range{}) {
					scroll = append(scroll, strconv.Itoa(it.Range.From.Line)+"-"+strconv.Itoa(it.Range.To.Line)+":"+it.Color.Key)
				} else {
					scroll = append(scroll, strconv.Itoa(it.Line)+":"+it.Color.Key)
				}
			}
			slices.Sort(scroll)
			if !slices.Equal(scroll, tt.scroll) {
				t.Errorf("scrollbar = %v, want %v", scroll, tt.scroll)
			}
			if p := r.problems(); len(p) != 0 {
				t.Errorf("problems: %v", p)
			}
		})
	}
}

// The plugin loads after the file opened: the runtime replays the open and the activation
// to it (AMENDMENTS C21 4), and the two together mark the file once, with one git call.
func TestGitGutterMarksAFileThatWasOpenBeforeTheLoad(t *testing.T) {
	r := gutterRig(t, "a\nb\n", "a\nB\n")
	r.emit("after_file_open", gutterFile, map[string]any{"replay": true})
	r.emit("buffer_activated", gutterFile, map[string]any{"replay": true})
	if got := marks(r.decor("git_gutter", "git_gutter", gutterFile)); !slices.Equal(got, []string{"1:│:change"}) {
		t.Fatalf("gutter = %v (log %v)", got, r.h.LogText("git_gutter"))
	}
	if n := r.runs("show"); n != 1 {
		t.Errorf("git show ran %d times for the replayed open and activation, want 1", n)
	}
}

func TestGitGutterFollowsTyping(t *testing.T) {
	r := gutterRig(t, "a\nb\nc\n", "a\nb\nc\n")
	r.emit("after_file_open", gutterFile, nil)
	if got := r.decor("git_gutter", "git_gutter", gutterFile); len(got) != 0 {
		t.Fatalf("marks on a clean buffer: %v", got)
	}
	shows := r.runs("show")

	r.edit(gutterFile, "a\nb\nC\n")
	r.h.Emit(pluginapi.Event{Name: "buffer_changed", Data: map[string]any{"buf": r.ids[gutterFile]}})
	if got := r.decor("git_gutter", "git_gutter", gutterFile); len(got) != 0 {
		t.Fatalf("marks before the pause: %v", got)
	}
	r.h.Advance(299 * time.Millisecond)
	if got := r.decor("git_gutter", "git_gutter", gutterFile); len(got) != 0 {
		t.Fatalf("marks 299 ms after the change: %v", got)
	}
	r.h.Advance(time.Millisecond)
	if got := marks(r.decor("git_gutter", "git_gutter", gutterFile)); !slices.Equal(got, []string{"2:│:change"}) {
		t.Fatalf("marks after the pause = %v", got)
	}
	if n := r.runs("show"); n != shows {
		t.Errorf("typing ran git show %d more times; HEAD is read once", n-shows)
	}

	// Back to the HEAD text: the marks go.
	r.edit(gutterFile, "a\nb\nc\n")
	r.h.Emit(pluginapi.Event{Name: "buffer_changed", Data: map[string]any{"buf": r.ids[gutterFile]}})
	r.h.Advance(300 * time.Millisecond)
	if got := r.decor("git_gutter", "git_gutter", gutterFile); len(got) != 0 {
		t.Fatalf("marks after undoing the change: %v", got)
	}
}

// A save, a revert and a refresh read HEAD again, because it may have moved.
func TestGitGutterReadsHeadAgain(t *testing.T) {
	r := gutterRig(t, "a\nb\n", "a\nb\n")
	r.emit("after_file_open", gutterFile, nil)
	if n := r.runs("show"); n != 1 {
		t.Fatalf("show ran %d times", n)
	}
	r.emit("after_file_save", gutterFile, nil)
	r.emit("after_file_revert", gutterFile, nil)
	if n := r.runs("show"); n != 3 {
		t.Errorf("show ran %d times after open, save and revert, want 3", n)
	}
	// HEAD moves on: the saved file now equals the new HEAD except for one line.
	r.os.Script(showArgv("a.txt"), plugintest.ProcScript{Stdout: "a\nx\n"}) // the first script still wins
	if err := r.h.Run("git_gutter.refresh", nil); err != nil {
		t.Fatal(err)
	}
	if n := r.runs("show"); n != 4 {
		t.Errorf("Git Gutter: Refresh did not read HEAD (shows %d)", n)
	}
	if got := r.h.NoticeTexts(); len(got) == 0 || got[len(got)-1] != "Git Gutter: 0 change(s) detected" {
		t.Errorf("notices = %v", got)
	}
}

func TestGitGutterIdleWhereGitHasNothingToCompare(t *testing.T) {
	tests := []struct {
		name string
		rig  rigOpts
	}{
		{"untracked file", rigOpts{script: func(o *plugintest.FakeOS) {
			o.Script(showArgv("a.txt"), plugintest.ProcScript{Code: 128, Stderr: "fatal: path 'a.txt' exists on disk, but not in 'HEAD'\n"})
		}}},
		{"not a repository", rigOpts{script: func(o *plugintest.FakeOS) {
			o.Script(showArgv("a.txt"), plugintest.ProcScript{Code: 128, Stderr: "fatal: not a git repository (or any of the parent directories): .git\n"})
		}}},
		{"git is not installed", rigOpts{noGit: true}},
		{"git cannot start", rigOpts{script: func(o *plugintest.FakeOS) {
			o.Script(showArgv("a.txt"), plugintest.ProcScript{Err: pluginapi.Errf(pluginapi.CodeExec, "exec: permission denied")})
		}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.rig.plugins = []string{"git_gutter"}
			tt.rig.buffers = []rigBuffer{{gutterFile, "a\nb\n"}}
			r := startRig(t, tt.rig)
			r.emit("after_file_open", gutterFile, nil)
			r.edit(gutterFile, "a\nc\n")
			r.h.Emit(pluginapi.Event{Name: "buffer_changed", Data: map[string]any{"buf": r.ids[gutterFile]}})
			r.h.Advance(time.Second)
			if got := r.host.Decor().Entries; len(got) != 0 {
				t.Errorf("decorations: %v", got)
			}
			if got := r.h.NoticeTexts(); len(got) != 0 {
				t.Errorf("notices: %v", got)
			}
			if p := r.problems(); len(p) != 0 {
				t.Errorf("problems: %v", p)
			}
			if tt.rig.noGit && len(r.os.Runs()) != 0 {
				t.Errorf("runs without a git binary: %v", r.os.Runs())
			}
			if n := r.runs("show"); n > 1 {
				t.Errorf("show ran %d times for a file git does not know; typing must not ask again", n)
			}
		})
	}
}

func TestGitGutterIgnoresBuffersWithoutAFile(t *testing.T) {
	r := startRig(t, rigOpts{plugins: []string{"git_gutter"}, buffers: []rigBuffer{{"", "x\n"}}})
	r.h.Emit(pluginapi.Event{Name: "buffer_activated", Data: map[string]any{"buf": r.host.ActiveBuffer()}})
	if n := len(r.os.Runs()); n != 0 {
		t.Errorf("ran %d commands for a buffer with no path", n)
	}
}

func TestGitGutterRefreshWithoutAFile(t *testing.T) {
	r := startRig(t, rigOpts{plugins: []string{"git_gutter"}, buffers: []rigBuffer{{"", "x\n"}}})
	if err := r.h.Run("git_gutter.refresh", nil); err != nil {
		t.Fatal(err)
	}
	if got := r.h.NoticeTexts(); len(got) != 1 || got[0] != "Git Gutter: No file open" {
		t.Errorf("notices = %v, want the no-file notice", got)
	}
	if n := len(r.os.Runs()); n != 0 {
		t.Errorf("ran %d commands for a buffer with no path", n)
	}
}

func TestGitGutterForgetsClosedBuffers(t *testing.T) {
	r := gutterRig(t, "a\n", "a\n")
	r.emit("after_file_open", gutterFile, nil)
	id := r.ids[gutterFile]
	shows := r.runs("show")
	r.host.RemoveBuffer(id)
	r.h.Emit(pluginapi.Event{Name: "buffer_closed", Data: map[string]any{"id": id, "path": gutterFile}})
	r.h.Emit(pluginapi.Event{Name: "buffer_changed", Data: map[string]any{"buf": id}})
	r.h.Advance(time.Second)
	if p := r.problems(); len(p) != 0 {
		t.Errorf("problems: %v", p)
	}
	if n := r.runs("show"); n != shows {
		t.Errorf("git show ran %d more times for a closed buffer", n-shows)
	}
}

// Unload leaves nothing behind in the host (marks, commands).
func TestGitGutterUnloadLeavesNothing(t *testing.T) {
	r := gutterRig(t, "a\nb\n", "a\nB\n")
	r.emit("after_file_open", gutterFile, nil)
	if len(r.host.Decor().Entries) == 0 {
		t.Fatal("no marks")
	}
	r.h.Unload("git_gutter")
	if got := r.host.Resources(); len(got) != 0 {
		t.Errorf("resources after unload: %v", got)
	}
}

func TestGitGutterNextAndPreviousChange(t *testing.T) {
	head := lines("1", "2", "3", "4", "5", "6", "7", "8", "9", "")
	text := lines("1", "2x", "3", "4", "5", "6x", "7", "9", "")
	r := gutterRig(t, head, text)
	r.emit("after_file_open", gutterFile, nil)
	// Regions: line 1 (modified), line 5 (modified), line 7 (a deleted line: the mark is on
	// the line before the seam, which is line 6; 8 is missing so the seam is after 7).
	steps := []struct {
		cmd    string
		from   int
		want   int
		notice string
	}{
		{"git_gutter.next_change", 0, 1, "Change 1/3"},
		{"git_gutter.next_change", 1, 5, "Change 2/3"},
		{"git_gutter.next_change", 5, 6, "Change 3/3"},
		{"git_gutter.next_change", 6, 1, "Change 1/3 [wrapped]"},
		{"git_gutter.prev_change", 6, 5, "Change 2/3"},
		{"git_gutter.prev_change", 1, 6, "Change 3/3 [wrapped]"},
	}
	for _, s := range steps {
		if err := r.host.SetCursor(r.ids[gutterFile], pluginapi.Pos{Line: s.from}, false, false, false); err != nil {
			t.Fatal(err)
		}
		if err := r.h.Run(s.cmd, nil); err != nil {
			t.Fatal(err)
		}
		cs, _ := r.host.Cursors(r.ids[gutterFile])
		if cs[0].Pos.Line != s.want {
			t.Errorf("%s from line %d: caret on line %d, want %d (marks %v)", s.cmd, s.from, cs[0].Pos.Line, s.want,
				marks(r.decor("git_gutter", "git_gutter", gutterFile)))
		}
		if got := r.h.NoticeTexts(); got[len(got)-1] != s.notice {
			t.Errorf("%s from line %d: notice %q, want %q", s.cmd, s.from, got[len(got)-1], s.notice)
		}
	}
}

func TestGitGutterNavigationWithoutChanges(t *testing.T) {
	r := gutterRig(t, "a\n", "a\n")
	r.emit("after_file_open", gutterFile, nil)
	if err := r.h.Run("git_gutter.next_change", nil); err != nil {
		t.Fatal(err)
	}
	if got := r.h.NoticeTexts(); len(got) != 1 || got[0] != "No changes" {
		t.Errorf("notices = %v", got)
	}
}

// The plugin never scans a buffer in large-file mode, so navigation says that, not "No changes".
func TestGitGutterNavigationInLargeFileMode(t *testing.T) {
	r := gutterRig(t, "a\nb\n", "a\nB\n")
	for _, b := range r.host.Buffers {
		if b.Info.ID == r.ids[gutterFile] {
			b.Info.Large = true
		}
	}
	r.emit("after_file_open", gutterFile, nil)
	for _, cmd := range []string{"git_gutter.next_change", "git_gutter.prev_change"} {
		if err := r.h.Run(cmd, nil); err != nil {
			t.Fatal(err)
		}
	}
	got := r.h.NoticeTexts()
	if len(got) == 0 || got[len(got)-1] != "Git changes are not tracked in large-file mode" {
		t.Errorf("notices = %v, want the large-file message", got)
	}
	for _, n := range got {
		if n == "No changes" {
			t.Errorf("notices = %v: a large file was reported as unchanged", got)
		}
	}
}

// For a file git does not track, navigation falls back to the unsaved changes.
func TestGitGutterNavigationFallsBackToTheSavedText(t *testing.T) {
	r := startRig(t, rigOpts{
		plugins: []string{"git_gutter"},
		buffers: []rigBuffer{{gutterFile, lines("a", "b", "c", "d", "")}},
		script: func(o *plugintest.FakeOS) {
			o.Script(showArgv("a.txt"), plugintest.ProcScript{Code: 128, Stderr: "fatal: not in HEAD"})
		},
	})
	r.emit("after_file_open", gutterFile, nil)
	r.edit(gutterFile, lines("a", "b", "C", "d", ""))
	if err := r.h.Run("git_gutter.next_change", nil); err != nil {
		t.Fatal(err)
	}
	cs, _ := r.host.Cursors(r.ids[gutterFile])
	if cs[0].Pos.Line != 2 {
		t.Errorf("caret on line %d, want 2 (log %v)", cs[0].Pos.Line, r.h.LogText(""))
	}
}

func TestGitGutterExportsItsHunks(t *testing.T) {
	r := startRig(t, rigOpts{
		plugins: []string{"git_gutter"},
		buffers: []rigBuffer{{gutterFile, "a\nB\nc\nd\n"}},
		probe: `
local gvim = require "gvim"
gvim.command.register{ name = "go", run = function()
  local g = gvim.plugins.get("git_gutter")
  local hunks, err = g.hunks(gvim.buf.current().id)
  local parts = {}
  for _, h in ipairs(hunks or {}) do parts[#parts + 1] = h.type .. "@" .. h.line .. "x" .. h.count end
  gvim.notify(table.concat(parts, ",") .. "|" .. tostring(err) .. "|" .. tostring(g.hunks(9999)))
end }`,
		script: func(o *plugintest.FakeOS) {
			o.Script(showArgv("a.txt"), plugintest.ProcScript{Stdout: "a\nb\nc\nd\ne\n"})
		},
	})
	r.emit("after_file_open", gutterFile, nil)
	if err := r.h.Run("probe.go", nil); err != nil {
		t.Fatal(err)
	}
	got := r.h.NoticeTexts()
	// "b" became "B", and the trailing "e" line is gone.
	if len(got) != 1 || got[0] != "modified@1x1,deleted@3x1|nil|nil" {
		t.Errorf("notices = %v (log %v)", got, r.h.LogText(""))
	}
}

// A commit made outside the editor moves HEAD under an open file: the plugin watches the git
// directory and reads HEAD again, so the marks of lines that now match it go. Changes of other
// files of the directory (index.lock, written by every git command) are ignored.
func TestGitGutterFollowsAnExternalCommit(t *testing.T) {
	r := startRig(t, rigOpts{
		plugins: []string{"git_gutter"},
		buffers: []rigBuffer{{gutterFile, "a\nB\n"}},
		script: func(o *plugintest.FakeOS) {
			o.SetDir("/w")
			o.SetFile("/w/.git/HEAD", "ref: refs/heads/main\n")
			o.Script(repoArgv, plugintest.ProcScript{Stdout: "/w\n.git/HEAD\n"})
			o.Script(showArgv("a.txt"), plugintest.ProcScript{Stdout: "a\nb\n"})
		},
	})
	r.emit("after_file_open", gutterFile, nil)
	if got := marks(r.decor("git_gutter", "git_gutter", gutterFile)); !slices.Equal(got, []string{"1:│:change"}) {
		t.Fatalf("gutter = %v", got)
	}
	if n := len(r.os.Watches()); n != 1 {
		t.Fatalf("watchers = %d, want one on the git directory", n)
	}
	shows := r.runs("show")

	r.os.Emit("/w/.git/index.lock", "create")
	r.h.Settle()
	if n := r.runs("show"); n != shows {
		t.Errorf("a change of index.lock ran git show %d more times", n-shows)
	}

	// `git commit -a`: HEAD now holds the buffer's text.
	r.os.Rescript(showArgv("a.txt"), plugintest.ProcScript{Stdout: "a\nB\n"})
	r.os.Emit("/w/.git/COMMIT_EDITMSG", "write")
	r.h.Settle()
	if n := r.runs("show"); n != shows+1 {
		t.Errorf("after the commit git show ran %d times, want %d", n, shows+1)
	}
	if got := r.decor("git_gutter", "git_gutter", gutterFile); len(got) != 0 {
		t.Errorf("marks after an external commit of the same text: %v", marks(got))
	}
	if p := r.problems(); len(p) != 0 {
		t.Errorf("problems: %v", p)
	}
}
