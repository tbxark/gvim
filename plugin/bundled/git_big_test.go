package bundled_test

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/tbxark/gvim/plugin/plugintest"
	"github.com/tbxark/gvim/pluginapi"
)

// fastServices are the fake services with a Myers diff, which is cheap for texts that differ
// in a few lines (the LCS table of the plain fake is quadratic).
type fastServices struct{ *plugintest.FakeServices }

func (fastServices) Diff(a, b []string) []pluginapi.Hunk {
	// Texts with no line in common are one hunk (Myers would need a table of size (n+m)^2).
	inB := make(map[string]bool, len(b))
	for _, l := range b {
		inB[l] = true
	}
	common := false
	for _, l := range a {
		if inB[l] {
			common = true
			break
		}
	}
	if !common {
		switch {
		case len(a) == 0 && len(b) == 0:
			return nil
		case len(a) == 0:
			return []pluginapi.Hunk{{Kind: "insert", NewCount: len(b)}}
		case len(b) == 0:
			return []pluginapi.Hunk{{Kind: "delete", OldCount: len(a)}}
		}
		return []pluginapi.Hunk{{Kind: "change", OldCount: len(a), NewCount: len(b)}}
	}
	return myers(a, b)
}

func (f fastServices) DiffText(a, b string, o pluginapi.DiffOptions) []pluginapi.Hunk {
	if o.IgnoreEOL {
		norm := strings.NewReplacer("\r\n", "\n", "\r", "\n")
		a, b = norm.Replace(a), norm.Replace(b)
	}
	return f.Diff(strings.Split(a, "\n"), strings.Split(b, "\n"))
}

// myers is the O(ND) diff of Myers (1986), returned as hunks like diff does.
func myers(a, b []string) []pluginapi.Hunk {
	n, m := len(a), len(b)
	off := n + m
	v := make([]int, 2*off+2)
	var trace [][]int
	last := 0
	for d := 0; d <= n+m; d++ {
		trace = append(trace, slices.Clone(v))
		done := false
		for k := -d; k <= d; k += 2 {
			var x int
			if k == -d || (k != d && v[off+k-1] < v[off+k+1]) {
				x = v[off+k+1]
			} else {
				x = v[off+k-1] + 1
			}
			y := x - k
			for x < n && y < m && a[x] == b[y] {
				x++
				y++
			}
			v[off+k] = x
			if x >= n && y >= m {
				done = true
				break
			}
		}
		if done {
			last = d
			break
		}
	}
	del, ins := make([]bool, n), make([]bool, m)
	x, y := n, m
	for d := last; d > 0; d-- {
		pv := trace[d]
		k := x - y
		pk := k - 1
		if k == -d || (k != d && pv[off+k-1] < pv[off+k+1]) {
			pk = k + 1
		}
		px := pv[off+pk]
		py := px - pk
		if pk == k+1 {
			ins[py] = true
		} else {
			del[px] = true
		}
		x, y = px, py
	}
	var hunks []pluginapi.Hunk
	i, j := 0, 0
	for i < n || j < m {
		if i < n && j < m && !del[i] && !ins[j] {
			i++
			j++
			continue
		}
		oi, oj := i, j
		for i < n && del[i] {
			i++
		}
		for j < m && ins[j] {
			j++
		}
		h := pluginapi.Hunk{OldStart: oi, OldCount: i - oi, NewStart: oj, NewCount: j - oj}
		switch {
		case h.OldCount == 0:
			h.Kind = "insert"
		case h.NewCount == 0:
			h.Kind = "delete"
		default:
			h.Kind = "change"
		}
		hunks = append(hunks, h)
	}
	return hunks
}

// bigRig opens a 150 000-line file that differs from HEAD by a changed and an inserted line.
func bigRig(t *testing.T, o rigOpts) (*rig, []string) {
	t.Helper()
	const n = 150_000
	headLines := make([]string, n)
	for i := range headLines {
		headLines[i] = "line " + strconv.Itoa(i)
	}
	text := slices.Clone(headLines)
	text[75_000] = "changed"
	text = slices.Insert(text, 100_000, "inserted")
	o.services = fastServices{&plugintest.FakeServices{}}
	o.timeout = 50 * time.Millisecond
	o.buffers = []rigBuffer{{gutterFile, strings.Join(text, "\n")}}
	o.script = func(os *plugintest.FakeOS) {
		os.Script(showArgv("a.txt"), plugintest.ProcScript{Stdout: strings.Join(headLines, "\n")})
	}
	return startRig(t, o), text
}

// linesRead counts the lines plugins read through Host.Lines.
func linesRead(r *rig) (calls, lines int) {
	for _, c := range r.host.Calls() {
		if c.Method == "Lines" {
			calls++
			if n := c.Args[2].(int); n > 0 {
				lines += n
			} else {
				lines += len(r.host.Buffers[0].Lines) // everything to the end
			}
		}
	}
	return calls, lines
}

// reporter is what the assertions of the budget scenarios need from a test: a *testing.T,
// or the collector of one attempt (see attempts).
type reporter interface {
	Helper()
	Errorf(format string, args ...any)
}

// attempt collects the failures of one attempt of a scenario.
type attempt struct{ msgs []string }

func (p *attempt) Helper() {}
func (p *attempt) Errorf(format string, args ...any) {
	p.msgs = append(p.msgs, fmt.Sprintf(format, args...))
}

// attempts runs the body of a budget scenario, and runs it again (up to n times) when it
// failed. The scenarios check that every handler fits the 50 ms event budget, which is wall
// clock time: a scheduler stall on a machine that is busy with other work can kill a slice
// that is fine. A scenario that really costs more than the budget fails every attempt, so
// the test fails only then.
func attempts(t *testing.T, n int, body func(p *attempt)) {
	t.Helper()
	var p *attempt
	for range n {
		p = &attempt{}
		body(p)
		if len(p.msgs) == 0 {
			return
		}
	}
	for _, m := range p.msgs {
		t.Errorf("%s (in each of %d attempts)", m, n)
	}
}

// noBudgetProblems fails the test on a log line that says a handler overran its time.
func noBudgetProblems(t reporter, r *rig, plugin string) {
	t.Helper()
	for _, l := range r.h.Log() {
		if strings.Contains(l.Msg, "timed out") || l.Level == "warn" || l.Level == "error" {
			t.Errorf("log: %s %s %s", l.Plugin, l.Level, l.Msg)
		}
	}
	if p, ok := r.h.Plugin(plugin); !ok || p.Status != "loaded" {
		t.Errorf("plugin %s = %+v", plugin, p)
	}
}

// TestGitGutterBigBufferStaysWithinBudgets is the rule for the ports: no handler
// reads the buffer by lines, and every handler finishes inside the 50 ms event budget (the
// runtime would otherwise log a timeout and count a strike).
func TestGitGutterBigBufferStaysWithinBudgets(t *testing.T) {
	attempts(t, 3, func(p *attempt) {
		r, text := bigRig(t, rigOpts{plugins: []string{"git_gutter"}})
		for _, name := range []string{"after_file_open", "buffer_activated", "after_file_save"} {
			r.emit(name, gutterFile, nil)
		}
		r.edit(gutterFile, strings.Join(slices.Insert(slices.Clone(text), 10, "typed"), "\n"))
		r.h.Emit(pluginapi.Event{Name: "buffer_changed", Data: map[string]any{"buf": r.ids[gutterFile]}})
		r.h.Advance(time.Second)

		want := []string{"100001:│:add", "10:│:add", "75001:│:change"}
		if got := marks(r.decor("git_gutter", "git_gutter", gutterFile)); !slices.Equal(got, want) {
			p.Errorf("marks = %v, want %v", got, want)
		}
		if calls, n := linesRead(r); n >= 2000 || calls != 0 {
			p.Errorf("the plugin read %d lines in %d calls; it reads none", n, calls)
		}
		noBudgetProblems(p, r, "git_gutter")
	})
}

// A long run of changed lines is built over several turns of the scheduler, within the
// time budget of a handler, and capped at 10 000 bars.
func TestGitGutterLongRunOfChanges(t *testing.T) {
	tests := []struct {
		name     string
		changed  int
		wantBars int
	}{
		{"5 000 lines", 5000, 5000},
		{"more than the cap", 12_000, 10_000},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			attempts(t, 3, func(p *attempt) {
				var head, text []string
				for i := 0; i < tt.changed; i++ {
					head = append(head, "old "+strconv.Itoa(i))
					text = append(text, "new "+strconv.Itoa(i))
				}
				r := startRig(t, rigOpts{
					plugins:  []string{"git_gutter"},
					services: fastServices{&plugintest.FakeServices{}},
					timeout:  50 * time.Millisecond,
					buffers:  []rigBuffer{{gutterFile, strings.Join(text, "\n")}},
					script: func(o *plugintest.FakeOS) {
						o.Script(showArgv("a.txt"), plugintest.ProcScript{Stdout: strings.Join(head, "\n")})
					},
				})
				r.emit("after_file_open", gutterFile, nil)
				for i := 0; i < 50 && len(r.decor("git_gutter", "git_gutter", gutterFile)) == 0; i++ {
					r.h.Advance(time.Millisecond)
				}
				if got := len(r.decor("git_gutter", "git_gutter", gutterFile)); got != tt.wantBars {
					p.Errorf("%d bars, want %d (log %v)", got, tt.wantBars, r.h.LogText(""))
				}
				if got := len(r.decor("git_gutter", "git_gutter_scroll", gutterFile)); got != 1 {
					p.Errorf("%d scrollbar marks, want 1 for one hunk", got)
				}
				noBudgetProblems(p, r, "git_gutter")
			})
		})
	}
}

// The inline blame of an edited 150 000-line buffer hands git the text of the buffer in one
// piece and reads no lines but the last, within the budget of a handler.
func TestGitBlameBigBufferStaysWithinBudgets(t *testing.T) {
	attempts(t, 3, func(p *attempt) {
		var sb strings.Builder
		for i := 0; i < 150_000; i++ {
			sb.WriteString("line " + strconv.Itoa(i) + "\n")
		}
		text := sb.String()
		r := startRig(t, rigOpts{
			plugins: []string{"git_blame"},
			timeout: 50 * time.Millisecond,
			buffers: []rigBuffer{{blameFile, text}},
			script: func(o *plugintest.FakeOS) {
				o.Script(gitArgv("blame", "--no-textconv", "--porcelain"),
					plugintest.ProcScript{Stdout: porcelain(1, blamed{shaAnn, "Ann Author", "first commit", time.Hour})})
			},
		})
		r.edit(blameFile, "x"+text)
		if err := r.host.SetCursor(r.ids[blameFile], pluginapi.Pos{}, false, false, false); err != nil {
			t.Fatal(err)
		}
		if err := r.h.Run("git_blame.toggle", nil); err != nil {
			t.Fatal(err)
		}
		r.moveCaret(0)
		r.h.Advance(time.Second)

		if got := blameText(r); !slices.Equal(got, []string{"0:  Ann Author, 1 hour ago \u2022 first commit"}) {
			p.Errorf("blame text = %q (log %v)", got, r.h.LogText(""))
		}
		runs := r.os.Runs()
		if last := runs[len(runs)-1]; len(last.Opts.Stdin) != len(text)+1 || !slices.Contains(last.Argv, "--contents") {
			p.Errorf("git was given %d bytes on stdin, argv %v", len(last.Opts.Stdin), last.Argv)
		}
		if calls, n := linesRead(r); n >= 2000 {
			p.Errorf("the plugin read %d lines in %d calls", n, calls)
		}
		noBudgetProblems(p, r, "git_blame")
	})
}
