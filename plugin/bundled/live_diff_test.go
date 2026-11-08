package bundled_test

import (
	"encoding/json"
	"io/fs"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/tbxark/gvim/plugin/bundled"
	"github.com/tbxark/gvim/plugin/plugintest"
	"github.com/tbxark/gvim/pluginapi"
)

// The live_diff port: a fake repository whose git show answers with HEAD text,
// and a fake host whose decorations the test reads back.
const liveDiffFile = "/w/a.txt"

// liveDiffFiles reads the plugin's files out of the embedded bundled tree,
// including the i18n table that the shared rig does not load.
func liveDiffFiles(t *testing.T) plugintest.Files {
	t.Helper()
	files := plugintest.Files{}
	for _, name := range []string{"init.lua", "live_diff.i18n.json"} {
		data, err := fs.ReadFile(bundled.FS, "plugins/live_diff/"+name)
		if err != nil {
			t.Fatalf("read the bundled plugin file %s: %v", name, err)
		}
		files["live_diff/"+name] = string(data)
	}
	return files
}

// startLiveDiff is the rig for live_diff: it loads the i18n file too, so the
// test sees the translated Fresh strings.
func startLiveDiff(t *testing.T, o rigOpts) *rig {
	t.Helper()
	r := &rig{t: t, host: plugintest.NewFakeHost(), os: plugintest.NewFakeOS(), ids: map[string]pluginapi.BufferID{}}
	if !o.noGit {
		r.os.Bin["git"] = "/usr/bin/git"
	}
	if o.script != nil {
		o.script(r.os)
	}
	r.os.Script(filtersArgv, plugintest.ProcScript{Code: 1})
	for _, b := range o.buffers {
		r.ids[b.path] = r.host.AddBuffer(b.path, b.text)
	}
	timeout := o.timeout
	if timeout == 0 {
		timeout = 500 * time.Millisecond
	}
	r.h = plugintest.New(t, liveDiffFiles(t), plugintest.Options{
		Host: r.host, OS: r.os, Timeout: timeout, EventBudget: 5 * time.Second,
	})
	r.h.Start()
	return r
}

// liveDiffRig opens liveDiffFile with text, scripts HEAD to be head, and loads
// live_diff. The plugin is opt-in and starts disabled.
func liveDiffRig(t *testing.T, head, text string) *rig {
	t.Helper()
	return startLiveDiff(t, rigOpts{
		buffers: []rigBuffer{{liveDiffFile, text}},
		script: func(o *plugintest.FakeOS) {
			o.Script(showArgv("a.txt"), plugintest.ProcScript{Stdout: head})
			o.SetFile(liveDiffFile, text)
		},
	})
}

// liveDiffEnable turns the global toggle on and opens the buffer.
func liveDiffEnable(t *testing.T, r *rig) {
	t.Helper()
	if err := r.h.Run("live_diff.toggle_global", nil); err != nil {
		t.Fatal(err)
	}
	r.emit("after_file_open", liveDiffFile, nil)
}

// liveDiffGutter reduces the gutter items to their glyphs.
func liveDiffGutter(r *rig) []string {
	var out []string
	for _, it := range r.decor("live_diff", "live_diff_gutter", liveDiffFile) {
		if it.Kind == pluginapi.DecorGutter {
			out = append(out, it.Text)
		}
	}
	return out
}

// liveDiffBgLines are the numbered lines with a diff background.
func liveDiffBgLines(r *rig) []int {
	var out []int
	for _, it := range r.decor("live_diff", "live_diff_overlay", liveDiffFile) {
		if it.Kind == pluginapi.DecorLine {
			out = append(out, it.Line)
		}
	}
	return out
}

// liveDiffWordUnderlines counts the bold+underline word highlights.
func liveDiffWordUnderlines(r *rig) int {
	n := 0
	for _, it := range r.decor("live_diff", "live_diff_overlay", liveDiffFile) {
		if it.Kind == pluginapi.DecorHighlight && it.Style.Bold && it.Style.Underline {
			n++
		}
	}
	return n
}

// liveDiffScroll reduces scrollbar items to "first-last" or "line".
func liveDiffScroll(r *rig) []string {
	var out []string
	for _, it := range r.decor("live_diff", "live_diff_scroll", liveDiffFile) {
		if it.Kind != pluginapi.DecorScroll {
			continue
		}
		if it.Range != (pluginapi.Range{}) {
			out = append(out, strconv.Itoa(it.Range.From.Line)+"-"+strconv.Itoa(it.Range.To.Line))
		} else {
			out = append(out, strconv.Itoa(it.Line))
		}
	}
	return out
}

func TestLiveDiffIsOffUntilToggled(t *testing.T) {
	r := liveDiffRig(t, "a\nb\nc\n", "a\nB\nc\n")
	r.emit("after_file_open", liveDiffFile, nil)
	if n := r.runs("show"); n != 0 {
		t.Errorf("git show ran %d times while the plugin is off", n)
	}
	if got := r.host.Decor().Entries; len(got) != 0 {
		t.Errorf("decorations while off: %v", got)
	}
	if got := r.h.NoticeTexts(); len(got) != 0 {
		t.Errorf("notices while off: %v", got)
	}
}

func TestLiveDiffMarksAModifiedLine(t *testing.T) {
	// Lines long enough and similar enough (>= 0.95 by char LCS) that Fresh
	// keeps them as one modified line instead of splitting removed + added.
	const head = "aaaaaaaaaaaaaaaaaaab"
	const edited = "aaaaaaaaaaaaaaaaaaac"
	r := liveDiffRig(t, head+"\n", head+"\n")
	liveDiffEnable(t, r)
	if n := r.runs("show"); n != 1 {
		t.Fatalf("git show ran %d times for one open, want 1", n)
	}
	if got := r.host.Decor().Entries; len(got) != 0 {
		t.Fatalf("decorations on a clean buffer: %v", got)
	}

	r.edit(liveDiffFile, edited+"\n")
	r.h.Emit(pluginapi.Event{Name: "buffer_changed", Data: map[string]any{"buf": r.ids[liveDiffFile]}})
	if got := r.host.Decor().Entries; len(got) != 0 {
		t.Fatalf("decorations before the debounce: %v", got)
	}
	r.h.Advance(75 * time.Millisecond)

	if got := liveDiffGutter(r); !slices.Equal(got, []string{"~"}) {
		t.Errorf("gutter = %v, want the modified glyph on line 0; log %v", got, r.h.LogText("live_diff"))
	}
	if got := liveDiffBgLines(r); !slices.Equal(got, []int{0}) {
		t.Errorf("line backgrounds = %v, want line 0", got)
	}
	if n := liveDiffWordUnderlines(r); n != 1 {
		t.Errorf("word underlines = %d, want the changed word on line 0", n)
	}
	if got := liveDiffScroll(r); !slices.Equal(got, []string{"0", "0-1"}) {
		t.Errorf("scrollbar marks = %v, want the removed point and the modified range", got)
	}
	if n := r.runs("show"); n != 1 {
		t.Errorf("typing ran git show %d more times; the HEAD text is cached", n-1)
	}
	if p := r.problems(); len(p) != 0 {
		t.Errorf("problems: %v", p)
	}

	// Back to HEAD: the decorations go away.
	r.edit(liveDiffFile, head+"\n")
	r.h.Emit(pluginapi.Event{Name: "buffer_changed", Data: map[string]any{"buf": r.ids[liveDiffFile]}})
	r.h.Advance(75 * time.Millisecond)
	if got := r.host.Decor().Entries; len(got) != 0 {
		t.Errorf("decorations after reverting: %v", got)
	}
}

func TestLiveDiffDiskMode(t *testing.T) {
	r := liveDiffRig(t, "a\nb\nc\n", "a\nb\nc\n")
	liveDiffEnable(t, r)
	if err := r.h.Run("live_diff.vs_disk", nil); err != nil {
		t.Fatal(err)
	}
	if got := r.h.NoticeTexts(); len(got) == 0 || got[len(got)-1] != "Live Diff: comparing against the file on disk" {
		t.Fatalf("notices = %v", got)
	}
	// The disk file now differs from the buffer on line 2.
	r.os.SetFile(liveDiffFile, "a\nb\nX\n")
	if err := r.h.Run("live_diff.refresh", nil); err != nil {
		t.Fatal(err)
	}
	// "c" -> "X" is a rewrite (similarity below the threshold), so Fresh's
	// reduced rendering marks the new line as added.
	if got := liveDiffGutter(r); !slices.Equal(got, []string{"+"}) {
		t.Errorf("gutter = %v, want the added glyph on line 2 (log %v)", got, r.h.LogText("live_diff"))
	}
	if n := r.runs("show"); n != 1 {
		t.Errorf("disk mode ran git show %d times; the disk reference is a file read", n-1)
	}
}

func TestLiveDiffUntrackedStaysIdle(t *testing.T) {
	r := startLiveDiff(t, rigOpts{
		buffers: []rigBuffer{{liveDiffFile, "a\nB\n"}},
		script: func(o *plugintest.FakeOS) {
			o.Script(showArgv("a.txt"), plugintest.ProcScript{Code: 128, Stderr: "fatal: path 'a.txt' does not exist in 'HEAD'"})
		},
	})
	liveDiffEnable(t, r)
	if n := r.runs("show"); n != 1 {
		t.Fatalf("git show ran %d times, want 1", n)
	}
	for i := 0; i < 3; i++ {
		r.edit(liveDiffFile, "a\nB\nx\n")
		r.h.Emit(pluginapi.Event{Name: "buffer_changed", Data: map[string]any{"buf": r.ids[liveDiffFile]}})
		r.h.Advance(75 * time.Millisecond)
	}
	if n := r.runs("show"); n != 1 {
		t.Errorf("typing asked git %d more times for a file it does not know", n-1)
	}
	if got := r.host.Decor().Entries; len(got) != 0 {
		t.Errorf("decorations for an untracked file: %v", got)
	}
	if p := r.problems(); len(p) != 0 {
		t.Errorf("problems: %v", p)
	}
}

func TestLiveDiffToggleBuffer(t *testing.T) {
	r := liveDiffRig(t, "a\nb\n", "a\nB\n")
	liveDiffEnable(t, r)
	if err := r.h.Run("live_diff.toggle_buffer", nil); err != nil {
		t.Fatal(err)
	}
	if got := r.h.NoticeTexts(); len(got) == 0 || got[len(got)-1] != "Live Diff: disabled for this buffer" {
		t.Fatalf("notices = %v", got)
	}
	for _, it := range r.host.Decor().Entries {
		if it.Buf == r.ids[liveDiffFile] {
			t.Errorf("decoration after disabling the buffer: %v", it)
		}
	}
}

func TestLiveDiffManifest(t *testing.T) {
	data, err := fs.ReadFile(bundled.FS, "plugins/live_diff/plugin.json")
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
	if m.Name != "live_diff" || m.Version == "" || m.Description == "" || m.API != 2 {
		t.Errorf("manifest = %+v", m)
	}
	if m.DefaultEnabled == nil || *m.DefaultEnabled {
		t.Errorf("default_enabled = %v, want false (live_diff is opt-in)", m.DefaultEnabled)
	}
}

func TestLiveDiffBranchMode(t *testing.T) {
	r := liveDiffRig(t, "a\nb\nc\n", "a\nb\nc\n")
	liveDiffEnable(t, r)
	r.os.Script(gitArgv("show", "--no-ext-diff", "--no-textconv", "feature:./a.txt"),
		plugintest.ProcScript{Stdout: "a\nX\nc\n"})
	if err := r.h.Run("live_diff.vs_branch", nil); err != nil {
		t.Fatal(err)
	}
	prompt := r.host.UI().Last("input")
	if prompt == nil || !prompt.Open || prompt.Input.Prompt != "Branch or ref" {
		t.Fatalf("prompt = %+v (log %v)", prompt, r.h.LogText("live_diff"))
	}
	r.h.Resolve(pluginapi.Completion{Token: prompt.Token, Values: []any{"feature"}})
	if got := r.h.NoticeTexts(); len(got) == 0 || got[len(got)-1] != "Live Diff: comparing against feature" {
		t.Errorf("notices = %v", got)
	}
	if got := liveDiffGutter(r); !slices.Equal(got, []string{"+"}) {
		t.Errorf("gutter = %v, want the added glyph on line 1; log %v", got, r.h.LogText("live_diff"))
	}
	runs := r.runs("show")
	if runs != 2 {
		t.Errorf("git show ran %d times; HEAD plus the branch reference, want 2", runs)
	}
	// The chosen branch is remembered for the next prompt.
	if err := r.h.Run("live_diff.vs_branch", nil); err != nil {
		t.Fatal(err)
	}
	again := r.host.UI().Last("input")
	if again == nil || again.Input.Default != "feature" {
		t.Errorf("second prompt default = %q, want feature", again.Input.Default)
	}
	r.h.Resolve(pluginapi.Completion{Token: again.Token, Values: []any{nil}})
}

func TestLiveDiffRefreshesWhenHeadMoves(t *testing.T) {
	r := liveDiffRig(t, "a\nb\n", "a\nb\n")
	// A repository that answers the watcher setup, so the .git directory is watched.
	r.os.Script(gitArgv("rev-parse", "--show-toplevel", "--git-path", "HEAD"),
		plugintest.ProcScript{Stdout: "/w\n/w/.git/HEAD\n"})
	r.os.SetFile("/w/.git/HEAD", "ref: refs/heads/main\n")
	liveDiffEnable(t, r)
	if n := r.runs("show"); n != 1 {
		t.Fatalf("git show ran %d times, want 1", n)
	}
	// A HEAD write (commit/checkout made elsewhere) re-fetches the reference.
	r.os.Emit("/w/.git/HEAD", "write")
	r.h.Advance(200 * time.Millisecond)
	r.h.Settle()
	if n := r.runs("show"); n != 2 {
		t.Fatalf("git show ran %d times after HEAD moved, want 2 (log %v)", n, r.h.LogText("live_diff"))
	}
	// HEAD still has the same content, so the clean buffer stays unmarked.
	if got := r.host.Decor().Entries; len(got) != 0 {
		t.Errorf("decorations after an unchanged HEAD: %v", got)
	}
	if p := r.problems(); len(p) != 0 {
		t.Errorf("problems: %v", p)
	}
}

func TestLiveDiffDiskWatcherUpdatesTheReference(t *testing.T) {
	r := liveDiffRig(t, "a\nb\n", "a\nb\n")
	liveDiffEnable(t, r)
	if err := r.h.Run("live_diff.vs_disk", nil); err != nil {
		t.Fatal(err)
	}
	// An external writer changes the file on disk; disk mode watches it.
	r.os.SetFile(liveDiffFile, "a\nX\n")
	r.os.Emit(liveDiffFile, "write")
	r.h.Advance(250 * time.Millisecond)
	r.h.Settle()
	if got := liveDiffGutter(r); !slices.Equal(got, []string{"+"}) {
		t.Errorf("gutter = %v, want the added glyph on line 1 (log %v)", got, r.h.LogText("live_diff"))
	}
	if n := r.runs("show"); n != 1 {
		t.Errorf("disk mode ran git show %d times; the file watch uses a disk read", n-1)
	}
}

func TestLiveDiffRefusesLargeFiles(t *testing.T) {
	r := startLiveDiff(t, rigOpts{
		buffers: []rigBuffer{{liveDiffFile, "a\nb\n"}},
		script: func(o *plugintest.FakeOS) {
			o.Script(showArgv("a.txt"), plugintest.ProcScript{Stdout: "a\nb\n"})
		},
	})
	r.host.Buffers[r.index(liveDiffFile)].Info.Large = true
	liveDiffEnable(t, r)
	if got := r.h.NoticeTexts(); !slices.Contains(got, "Live Diff: large file — not tracked") {
		t.Errorf("notices = %v, want the large-file notice", got)
	}
	if n := r.runs("show"); n != 0 {
		t.Errorf("git show ran %d times for a large file", n)
	}
	if got := r.host.Decor().Entries; len(got) != 0 {
		t.Errorf("decorations for a large file: %v", got)
	}
}
