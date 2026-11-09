package bundled_test

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tbxark/gvim/plugin/hostos"
	"github.com/tbxark/gvim/plugin/plugintest"
	"github.com/tbxark/gvim/pluginapi"
)

const blameFile = "/w/dir/a.txt"

// harnessEpoch is the time of the plugin harness's manual clock, which is what gvim.time
// reports in these tests.
var harnessEpoch = time.Unix(1_700_000_000, 0)

type blamed struct {
	sha, author, summary string
	age                  time.Duration
}

var (
	longSummary = "fix the parser, and then some more words after it, and a lot more words to make it long"
	shaAnn      = strings.Repeat("a1", 20)
	shaBob      = strings.Repeat("b2", 20)
	shaNew      = strings.Repeat("0", 40)
)

// porcelain writes `git blame --porcelain` output for the lines from first (1-based): the
// header lines of a commit appear the first time it does.
func porcelain(first int, rows ...blamed) string {
	var sb strings.Builder
	seen := map[string]bool{}
	for i, b := range rows {
		fmt.Fprintf(&sb, "%s %d %d\n", b.sha, first+i, first+i)
		if !seen[b.sha] {
			seen[b.sha] = true
			fmt.Fprintf(&sb, "author %s\nauthor-time %d\nsummary %s\nfilename a.txt\n", b.author, harnessEpoch.Add(-b.age).Unix(), b.summary)
		}
		sb.WriteString("\ttext\n")
	}
	return sb.String()
}

func blameArgv(window string, extra ...string) []string {
	return gitArgv(append(append([]string{"blame", "--no-textconv", "--porcelain", window}, extra...), "--", "a.txt")...)
}

var blameRows = []blamed{
	{shaAnn, "Ann Author", "first commit", 3 * 24 * time.Hour},
	{shaAnn, "Ann Author", "first commit", 3 * 24 * time.Hour},
	{shaBob, "Bob B", longSummary, 2 * time.Hour},
	{shaNew, "Not Committed Yet", "Version of a.txt from a.txt", 0},
}

func blameRig(t *testing.T, o rigOpts) *rig {
	t.Helper()
	o.plugins = []string{"git_blame"}
	o.buffers = []rigBuffer{{blameFile, "one\ntwo\nthree\nfour\n"}} // four lines and the empty one after the final newline
	inner := o.script
	o.script = func(os *plugintest.FakeOS) {
		if inner != nil {
			inner(os)
		}
	}
	return startRig(t, o)
}

// gatedBlameOS's first Run matching target blocks until release: the regression tests use
// it to switch the cursor/toggle blame while a real git await is in flight.
type gatedBlameOS struct {
	*plugintest.FakeOS
	target  string
	block   atomic.Bool
	blocked chan struct{}
	release chan struct{}
}

func (g *gatedBlameOS) Run(ctx context.Context, argv []string, o hostos.ProcOptions) (hostos.ProcResult, error) {
	if slices.Contains(argv, g.target) && g.block.CompareAndSwap(true, false) {
		select {
		case g.blocked <- struct{}{}:
		default:
		}
		select {
		case <-g.release:
		case <-ctx.Done():
			return hostos.ProcResult{}, ctx.Err()
		}
	}
	return g.FakeOS.Run(ctx, argv, o)
}

func gatedBlameRig(t *testing.T, target string, script func(*plugintest.FakeOS)) (*rig, *gatedBlameOS) {
	t.Helper()
	gate := &gatedBlameOS{
		target:  target,
		blocked: make(chan struct{}, 1),
		release: make(chan struct{}),
	}
	r := blameRig(t, rigOpts{
		script: script,
		osWrap: func(o *plugintest.FakeOS) hostos.OS {
			gate.FakeOS = o
			return gate
		},
	})
	return r, gate
}

func blameScript(o *plugintest.FakeOS) {
	o.Script(blameArgv("-L1,4"), plugintest.ProcScript{Stdout: porcelain(1, blameRows...)})
	o.Script(gitArgv("show", "--no-ext-diff", "--no-textconv", "--stat", "--format=fuller", shaBob),
		plugintest.ProcScript{Stdout: "commit " + shaBob + "\nAuthor: Bob B\n\n    fix the parser\n\n a.txt | 2 +-\n"})
}

var parentBlameRows = []blamed{
	{shaAnn, "Ann Author", "parent commit", 5 * 24 * time.Hour},
	{shaAnn, "Ann Author", "parent commit", 5 * 24 * time.Hour},
	{shaAnn, "Ann Author", "parent commit", 5 * 24 * time.Hour},
	{shaAnn, "Ann Author", "parent commit", 5 * 24 * time.Hour},
}

func blameHistoryScript(o *plugintest.FakeOS) {
	blameScript(o)
	o.Script(gitArgv("rev-parse", "--verify", shaBob+"^"), plugintest.ProcScript{Stdout: shaAnn + "\n"})
	o.Script(blameArgv("-L1,4", shaAnn), plugintest.ProcScript{Stdout: porcelain(1, parentBlameRows...)})
}

// blameText returns the lines that carry blame text, as "line: text".
func blameText(r *rig) []string {
	var out []string
	for _, it := range r.decor("git_blame", "blame", blameFile) {
		if it.Kind != pluginapi.DecorEOL {
			continue
		}
		out = append(out, fmt.Sprintf("%d:%s", it.Line, it.Text))
	}
	return out
}

func (r *rig) moveCaret(line int) {
	r.t.Helper()
	if err := r.host.SetCursor(r.ids[blameFile], pluginapi.Pos{Line: line}, false, false, false); err != nil {
		r.t.Fatal(err)
	}
	r.h.Emit(pluginapi.Event{Name: "cursor_moved", Data: map[string]any{
		"buf": r.ids[blameFile], "pos": pluginapi.Pos{Line: line}, "previous": pluginapi.Pos{},
	}})
}

func TestGitBlameInlineFollowsTheCaret(t *testing.T) {
	r := blameRig(t, rigOpts{script: blameScript})
	if got := blameText(r); len(got) != 0 {
		t.Fatalf("blame text before the toggle: %v", got)
	}
	if r.h.RT.Wants("cursor_moved") {
		t.Fatal("the plugin listens to the caret while it is off")
	}
	if err := r.h.Run("git_blame.toggle", nil); err != nil {
		t.Fatal(err)
	}
	want := []string{"0:  Ann Author, 3 days ago • first commit"}
	if got := blameText(r); !slices.Equal(got, want) {
		t.Fatalf("blame text = %q, want %q (log %v)", got, want, r.h.LogText(""))
	}
	it := r.decor("git_blame", "blame", blameFile)[0]
	if !it.Style.Italic || it.Style.FG.Key != "dim" {
		t.Errorf("style = %+v, want dim italic", it.Style)
	}
	if n := r.runs("blame"); n != 1 {
		t.Errorf("blame ran %d times", n)
	}

	// Moving inside the blamed window needs no new git call, and shows after the 150 ms pause.
	tests := []struct {
		line int
		want string
	}{
		{2, "2:  Bob B, 2 hours ago • " + longSummary[:59] + "…"}, // cut to 60 cells
		{3, "3:  Not committed yet"},
		{1, "1:  Ann Author, 3 days ago • first commit"},
	}
	for _, tt := range tests {
		before := blameText(r)
		r.moveCaret(tt.line)
		r.h.Advance(149 * time.Millisecond)
		if got := blameText(r); !slices.Equal(got, before) {
			t.Fatalf("blame text changed before the pause: %q -> %q", before, got)
		}
		r.h.Advance(time.Millisecond)
		if got := blameText(r); !slices.Equal(got, []string{tt.want}) {
			t.Errorf("caret on line %d: blame text %q, want %q", tt.line, got, tt.want)
		}
	}
	if n := r.runs("blame"); n != 1 {
		t.Errorf("blame ran %d times for moves inside the window", n)
	}

	// Off: nothing is left, nothing listens.
	if err := r.h.Run("git_blame.toggle", nil); err != nil {
		t.Fatal(err)
	}
	if got := blameText(r); len(got) != 0 {
		t.Errorf("blame text after toggling off: %q", got)
	}
	if r.h.RT.Wants("cursor_moved") || r.h.RT.Wants("buffer_changed") {
		t.Error("the plugin still listens after it was turned off")
	}
}

func TestGitBlameUsesTheTextOfAnEditedBuffer(t *testing.T) {
	r := blameRig(t, rigOpts{script: func(o *plugintest.FakeOS) {
		blameScript(o)
		o.Script(blameArgv("-L1,5", "--contents", "-"), plugintest.ProcScript{Stdout: porcelain(1, append([]blamed{blameRows[3]}, blameRows...)...)})
	}})
	if err := r.h.Run("git_blame.toggle", nil); err != nil {
		t.Fatal(err)
	}
	// Type a new first line: the file on disk is blamed for old line numbers, so the text
	// of the buffer is what git is given.
	r.edit(blameFile, "zero\none\ntwo\nthree\nfour\n")
	if err := r.host.SetCursor(r.ids[blameFile], pluginapi.Pos{}, false, false, false); err != nil {
		t.Fatal(err)
	}
	r.h.Emit(pluginapi.Event{Name: "buffer_changed", Data: map[string]any{"buf": r.ids[blameFile]}})
	r.h.Advance(500 * time.Millisecond)
	runs := r.os.Runs()
	last := runs[len(runs)-1]
	if !slices.Equal(last.Argv, blameArgv("-L1,5", "--contents", "-")) || string(last.Opts.Stdin) != "zero\none\ntwo\nthree\nfour\n" {
		t.Fatalf("last run = %v stdin %q", last.Argv, last.Opts.Stdin)
	}
	if got := blameText(r); len(got) != 1 || got[0] != "0:  Not committed yet" {
		t.Errorf("blame text = %q", got)
	}
}

func TestGitBlameSilentWhereThereIsNothingToBlame(t *testing.T) {
	tests := []struct {
		name string
		rig  rigOpts
	}{
		{"untracked file", rigOpts{script: func(o *plugintest.FakeOS) {
			o.Script(gitHead, plugintest.ProcScript{Code: 128, Stderr: "fatal: no such path a.txt in HEAD\n"})
		}}},
		{"no git", rigOpts{noGit: true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := blameRig(t, tt.rig)
			if err := r.h.Run("git_blame.toggle", nil); err != nil {
				t.Fatal(err)
			}
			r.moveCaret(1)
			r.h.Advance(time.Second)
			if got := blameText(r); len(got) != 0 {
				t.Errorf("blame text: %q", got)
			}
			if p := r.problems(); len(p) != 0 {
				t.Errorf("problems: %v", p)
			}
			if got := r.h.NoticeTexts(); !slices.Equal(got, []string{"Git blame on"}) {
				t.Errorf("notices = %q", got)
			}
		})
	}
}

func TestGitBlameCaretBelowTheLastLine(t *testing.T) {
	r := blameRig(t, rigOpts{script: blameScript})
	if err := r.h.Run("git_blame.toggle", nil); err != nil {
		t.Fatal(err)
	}
	r.moveCaret(4) // the empty line after the final newline: git has no line there
	r.h.Advance(time.Second)
	if got := blameText(r); len(got) != 0 {
		t.Errorf("blame text on the phantom line: %q", got)
	}
}

func TestGitBlameStepsToParentRevisionAndBack(t *testing.T) {
	r := blameRig(t, rigOpts{script: blameHistoryScript})
	r.moveCaret(2)
	if err := r.h.Run("git_blame.toggle", nil); err != nil {
		t.Fatal(err)
	}
	if got := blameText(r); len(got) != 1 || !strings.Contains(got[0], "Bob B") {
		t.Fatalf("HEAD blame = %q, want Bob", got)
	}
	if err := r.h.Run("git_blame.blame_previous", nil); err != nil {
		t.Fatal(err)
	}
	if got := blameText(r); len(got) != 1 || !strings.Contains(got[0], "Ann Author, 5 days ago • parent commit") {
		t.Fatalf("parent blame = %q, want the parent revision", got)
	}
	if got := r.h.NoticeTexts(); len(got) == 0 || got[len(got)-1] != "Blame at parent "+shaAnn[:7] {
		t.Fatalf("notices = %q", got)
	}
	if err := r.h.Run("git_blame.blame_head", nil); err != nil {
		t.Fatal(err)
	}
	if got := blameText(r); len(got) != 1 || !strings.Contains(got[0], "Bob B") {
		t.Fatalf("HEAD blame after reset = %q, want Bob", got)
	}
}

func TestGitBlameHistoryRequiresSavedBuffer(t *testing.T) {
	r := blameRig(t, rigOpts{script: blameHistoryScript})
	r.moveCaret(2)
	if err := r.h.Run("git_blame.toggle", nil); err != nil {
		t.Fatal(err)
	}
	// Insert a line so the buffer becomes modified; the parent view cannot map its
	// unsaved lines to a committed revision.
	r.edit(blameFile, "zero\none\ntwo\nthree\nfour\n")
	r.h.Emit(pluginapi.Event{Name: "buffer_changed", Data: map[string]any{"buf": r.ids[blameFile]}})
	if err := r.h.Run("git_blame.blame_previous", nil); err != nil {
		t.Fatal(err)
	}
	if got := r.h.NoticeTexts(); len(got) == 0 || !strings.Contains(got[len(got)-1], "Save the file") {
		t.Fatalf("notices = %q, want the saved-buffer refusal", got)
	}
}

func TestGitBlameShowCommit(t *testing.T) {
	r := blameRig(t, rigOpts{script: blameScript})
	r.moveCaret(2)
	if err := r.h.Run("git_blame.show_commit", nil); err != nil {
		t.Fatal(err)
	}
	o := r.host.UI().Last("popup")
	if o == nil || !o.Open {
		t.Fatalf("no popup (notices %v, log %v)", r.h.NoticeTexts(), r.h.LogText(""))
	}
	if o.Popup.Title != shaBob[:7] || o.Popup.Anchor != "center" || len(o.Popup.Lines) != 6 || o.Popup.Lines[0] != "commit "+shaBob {
		t.Errorf("popup = %+v", o.Popup)
	}
}

func TestGitBlameCopyHash(t *testing.T) {
	r := blameRig(t, rigOpts{script: blameScript})
	r.moveCaret(2)
	if err := r.h.Run("git_blame.copy_hash", nil); err != nil {
		t.Fatal(err)
	}
	if r.host.Clipboard != shaBob {
		t.Errorf("clipboard = %q", r.host.Clipboard)
	}
	if got := r.h.NoticeTexts(); !slices.Equal(got, []string{fmt.Sprintf("Copied: %s (%s)", shaBob[:7], shaBob)}) {
		t.Errorf("notices = %q", got)
	}
}

func TestGitBlameCommandsOnUncommittedAndUnknownLines(t *testing.T) {
	tests := []struct {
		name string
		rig  rigOpts
		line int
		want string
	}{
		{"uncommitted line", rigOpts{script: blameScript}, 3, "This line is not yet committed"},
		{"untracked file", rigOpts{script: func(o *plugintest.FakeOS) {
			o.Script(gitHead, plugintest.ProcScript{Code: 128, Stderr: "fatal: no such path\n"})
		}}, 0, "No blame information available (not a git file or error)"},
	}
	for _, tt := range tests {
		for _, cmd := range []string{"git_blame.show_commit", "git_blame.copy_hash"} {
			t.Run(tt.name+" "+cmd, func(t *testing.T) {
				r := blameRig(t, tt.rig)
				r.moveCaret(tt.line)
				if err := r.h.Run(cmd, nil); err != nil {
					t.Fatal(err)
				}
				if got := r.h.NoticeTexts(); !slices.Equal(got, []string{tt.want}) {
					t.Errorf("notices = %q, want %q", got, tt.want)
				}
				if r.host.Clipboard != "" || r.host.UI().Last("popup") != nil {
					t.Errorf("clipboard %q, popup %v", r.host.Clipboard, r.host.UI().Last("popup"))
				}
			})
		}
	}
}

func TestGitBlameUnloadLeavesNothing(t *testing.T) {
	r := blameRig(t, rigOpts{script: blameScript})
	if err := r.h.Run("git_blame.toggle", nil); err != nil {
		t.Fatal(err)
	}
	r.h.Unload("git_blame")
	if got := r.host.Resources(); len(got) != 0 {
		t.Errorf("resources after unload: %v", got)
	}
	if r.h.RT.Wants("cursor_moved") {
		t.Error("a subscription survived the unload")
	}
}

func TestGitBlamePreviousRevisionWithoutParent(t *testing.T) {
	r := blameRig(t, rigOpts{script: func(o *plugintest.FakeOS) {
		blameScript(o)
		o.Script(gitArgv("rev-parse", "--verify", shaBob+"^"), plugintest.ProcScript{Code: 128, Stderr: "fatal: bad revision\n"})
	}})
	r.moveCaret(2)
	if err := r.h.Run("git_blame.toggle", nil); err != nil {
		t.Fatal(err)
	}
	if err := r.h.Run("git_blame.blame_previous", nil); err != nil {
		t.Fatal(err)
	}
	got := r.h.NoticeTexts()
	if len(got) == 0 || !strings.Contains(got[len(got)-1], "no parent revision") {
		t.Fatalf("notices = %q, want the parent-refusal notice", got)
	}
	if annot := blameText(r); len(annot) != 1 || !strings.Contains(annot[0], "Bob B") {
		t.Fatalf("blame changed after a refused parent step: %q", annot)
	}
}

func TestGitBlameUpdateDiscardsResultAfterToggleOff(t *testing.T) {
	r, gate := gatedBlameRig(t, "blame", blameScript)
	gate.block.Store(true)
	if err := r.h.RT.Run("git_blame.toggle", nil, pluginapi.SrcAPI); err != nil {
		t.Fatal(err)
	}
	// Disable while the first update's blame await is still pending, then let the delayed
	// runner finish. The old completion must not paint over the off state.
	if err := r.h.RT.Run("git_blame.toggle", nil, pluginapi.SrcAPI); err != nil {
		t.Fatal(err)
	}
	go func() {
		<-gate.blocked
		close(gate.release)
	}()
	r.h.Settle()
	if got := blameText(r); len(got) != 0 {
		t.Fatalf("a stale update painted after blame was turned off: %q", got)
	}
	// The next enable starts clean and shows HEAD, not a stale parent/revision view.
	if err := r.h.Run("git_blame.toggle", nil); err != nil {
		t.Fatal(err)
	}
	if got := blameText(r); len(got) != 1 || !strings.Contains(got[0], "Ann Author") {
		t.Fatalf("blame after re-enable = %q", got)
	}
}

func TestGitBlamePreviousRevisionAbortsOnCursorChange(t *testing.T) {
	r, gate := gatedBlameRig(t, "rev-parse", blameHistoryScript)
	r.moveCaret(2)
	if err := r.h.Run("git_blame.toggle", nil); err != nil {
		t.Fatal(err)
	}
	gate.block.Store(true)
	if err := r.h.RT.Run("git_blame.blame_previous", nil, pluginapi.SrcAPI); err != nil {
		t.Fatal(err)
	}
	// The parent resolve is inside the delayed runner. Move the live cursor from another
	// goroutine, then release it: the command must not read the new cursor with the old
	// entry.commit.
	changed := make(chan struct{})
	go func() {
		defer close(changed)
		<-gate.blocked
		if err := r.host.SetCursor(r.ids[blameFile], pluginapi.Pos{Line: 0}, false, false, false); err != nil {
			t.Errorf("set cursor: %v", err)
		}
		close(gate.release)
	}()
	r.h.Settle()
	<-changed
	for _, notice := range r.h.NoticeTexts() {
		if strings.Contains(notice, "Blame at parent") {
			t.Fatalf("stale cursor step reported success: notices %q", r.h.NoticeTexts())
		}
	}
}

func TestGitBlamePreviousRevisionAbortsWhenTurnedOff(t *testing.T) {
	r, gate := gatedBlameRig(t, "rev-parse", blameHistoryScript)
	r.moveCaret(2)
	if err := r.h.Run("git_blame.toggle", nil); err != nil {
		t.Fatal(err)
	}
	gate.block.Store(true)
	if err := r.h.RT.Run("git_blame.blame_previous", nil, pluginapi.SrcAPI); err != nil {
		t.Fatal(err)
	}
	// Turn off while the parent await is pending; release the delayed runner afterwards.
	if err := r.h.RT.Run("git_blame.toggle", nil, pluginapi.SrcAPI); err != nil {
		t.Fatal(err)
	}
	go func() {
		<-gate.blocked
		close(gate.release)
	}()
	r.h.Settle()
	if got := blameText(r); len(got) != 0 {
		t.Fatalf("blame text after off-race = %q", got)
	}
	for _, notice := range r.h.NoticeTexts() {
		if strings.Contains(notice, "Blame at parent") {
			t.Fatalf("off-race reported a parent step: notices %q", r.h.NoticeTexts())
		}
	}
}

func TestGitBlameHistoryCommandsWhileOffDoNotSetNextState(t *testing.T) {
	r := blameRig(t, rigOpts{script: blameHistoryScript})
	r.moveCaret(2)
	if err := r.h.Run("git_blame.toggle", nil); err != nil {
		t.Fatal(err)
	}
	if err := r.h.Run("git_blame.toggle", nil); err != nil {
		t.Fatal(err)
	}
	for _, cmd := range []string{"git_blame.blame_previous", "git_blame.blame_head"} {
		if err := r.h.Run(cmd, nil); err != nil {
			t.Fatal(err)
		}
	}
	notices := r.h.NoticeTexts()
	if len(notices) < 2 || !strings.Contains(strings.Join(notices, "\n"), "Git blame is off") {
		t.Fatalf("notices = %q, want the off refusal", notices)
	}
	if err := r.h.Run("git_blame.toggle", nil); err != nil {
		t.Fatal(err)
	}
	if got := blameText(r); len(got) != 1 || !strings.Contains(got[0], "Bob B") {
		t.Fatalf("the off-state commands changed the next view: %q", got)
	}
}

func TestGitBlameSaveAsInvalidatesParentRevision(t *testing.T) {
	r := blameRig(t, rigOpts{script: blameHistoryScript})
	r.moveCaret(2)
	if err := r.h.Run("git_blame.toggle", nil); err != nil {
		t.Fatal(err)
	}
	if err := r.h.Run("git_blame.blame_previous", nil); err != nil {
		t.Fatal(err)
	}
	if got := blameText(r); len(got) != 1 || !strings.Contains(got[0], "parent commit") {
		t.Fatalf("parent blame before Save As = %q", got)
	}
	// Save As changes the path of the same buffer id. The old parent revision belongs to
	// the old path and must not be reused for the new one.
	b := r.host.Buffers[r.index(blameFile)]
	b.Info.Path = "/w/other/a.txt"
	b.Info.Name = "a.txt"
	r.h.Emit(pluginapi.Event{Name: "after_file_save", Data: map[string]any{
		"buf": r.ids[blameFile], "path": "/w/other/a.txt",
	}})
	if got := blameText(r); len(got) != 1 || !strings.Contains(got[0], "Bob B") {
		t.Fatalf("blame after Save As = %q, want HEAD for the new path", got)
	}
}

func TestGitBlameShowCommitDiscardsStaleTargetAfterCommitRead(t *testing.T) {
	r, gate := gatedBlameRig(t, "show", blameScript)
	r.moveCaret(2)
	if err := r.h.Run("git_blame.toggle", nil); err != nil {
		t.Fatal(err)
	}
	gate.block.Store(true)
	if err := r.h.RT.Run("git_blame.show_commit", nil, pluginapi.SrcAPI); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		<-gate.blocked
		if err := r.host.SetCursor(r.ids[blameFile], pluginapi.Pos{Line: 0}, false, false, false); err != nil {
			t.Errorf("set cursor: %v", err)
		}
		close(gate.release)
	}()
	r.h.Settle()
	<-done
	if popup := r.host.UI().Last("popup"); popup != nil {
		t.Fatalf("stale Show Commit popup opened: %+v", popup)
	}
}

func TestGitBlameCopyHashDiscardsStaleTargetAfterBlameRead(t *testing.T) {
	r, gate := gatedBlameRig(t, "blame", blameScript)
	r.moveCaret(2)
	gate.block.Store(true)
	if err := r.h.RT.Run("git_blame.copy_hash", nil, pluginapi.SrcAPI); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		<-gate.blocked
		if err := r.host.SetCursor(r.ids[blameFile], pluginapi.Pos{Line: 0}, false, false, false); err != nil {
			t.Errorf("set cursor: %v", err)
		}
		close(gate.release)
	}()
	r.h.Settle()
	<-done
	if r.host.Clipboard != "" {
		t.Fatalf("stale Copy Commit Hash copied %q", r.host.Clipboard)
	}
}
