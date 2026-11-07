package plugintest_test

import (
	"context"
	"reflect"
	"runtime"
	"testing"
	"time"

	"github.com/tbxark/gvim/internal/plugin/plugintest"
	"github.com/tbxark/gvim/internal/pluginapi"
)

func TestFakeServicesWidth(t *testing.T) {
	s := &plugintest.FakeServices{}
	rows := []struct {
		in   string
		want int
	}{
		{"", 0}, {"abc", 3}, {"日本語", 6}, {"é", 1}, {"é", 1}, {"a日b", 4}, {"ｱ", 1}, {"Ａ", 2}, {"한글", 4}, {"😀", 2},
	}
	for _, r := range rows {
		if got := s.Width(r.in); got != r.want {
			t.Errorf("Width(%q) = %d, want %d", r.in, got, r.want)
		}
	}
	trunc := []struct {
		in       string
		w        int
		ellipsis string
		want     string
	}{
		{"abcdef", 4, "…", "abc…"}, {"abc", 4, "…", "abc"}, {"日本語", 5, "…", "日本…"}, {"日本語", 4, "", "日本"}, {"abcdef", 0, "…", ""},
	}
	for _, r := range trunc {
		if got := s.Truncate(r.in, r.w, r.ellipsis); got != r.want {
			t.Errorf("Truncate(%q, %d, %q) = %q, want %q", r.in, r.w, r.ellipsis, got, r.want)
		}
	}
}

func TestFakeServicesDiff(t *testing.T) {
	s := &plugintest.FakeServices{}
	h := func(kind string, os, oc, ns, nc int) pluginapi.Hunk {
		return pluginapi.Hunk{Kind: kind, OldStart: os, OldCount: oc, NewStart: ns, NewCount: nc}
	}
	rows := []struct {
		name string
		a, b []string
		want []pluginapi.Hunk
	}{
		{"equal", []string{"a", "b"}, []string{"a", "b"}, nil},
		{"insert in the middle", []string{"a", "c"}, []string{"a", "b", "c"}, []pluginapi.Hunk{h("insert", 1, 0, 1, 1)}},
		{"delete", []string{"a", "b", "c"}, []string{"a", "c"}, []pluginapi.Hunk{h("delete", 1, 1, 1, 0)}},
		{"change", []string{"a", "b", "c"}, []string{"a", "x", "c"}, []pluginapi.Hunk{h("change", 1, 1, 1, 1)}},
		{"two hunks", []string{"a", "b", "c", "d", "e"}, []string{"a", "B", "c", "d", "e", "f"}, []pluginapi.Hunk{h("change", 1, 1, 1, 1), h("insert", 5, 0, 5, 1)}},
		{"all new", nil, []string{"a", "b"}, []pluginapi.Hunk{h("insert", 0, 0, 0, 2)}},
		{"all gone", []string{"a", "b"}, nil, []pluginapi.Hunk{h("delete", 0, 2, 0, 0)}},
		{"replace whole", []string{"a"}, []string{"b"}, []pluginapi.Hunk{h("change", 0, 1, 0, 1)}},
	}
	for _, r := range rows {
		t.Run(r.name, func(t *testing.T) {
			if got := s.Diff(r.a, r.b); !reflect.DeepEqual(got, r.want) {
				t.Fatalf("Diff = %+v, want %+v", got, r.want)
			}
		})
	}
	if got := s.DiffText("a\r\nb\r\n", "a\nb\n", pluginapi.DiffOptions{IgnoreEOL: true}); got != nil {
		t.Fatalf("DiffText ignoring EOL = %+v", got)
	}
	if got := s.DiffText("a\r\nb", "a\nb", pluginapi.DiffOptions{}); len(got) != 1 {
		t.Fatalf("DiffText without IgnoreEOL = %+v", got)
	}
}

func TestFakeServicesFuzzyGrepDecodeGit(t *testing.T) {
	s := &plugintest.FakeServices{
		GrepHits: []pluginapi.GrepHit{{Path: "a.go", Line: 1, Text: "func Main()"}, {Path: "b.go", Line: 2, Text: "var main"}, {Path: "c.go", Text: "other"}},
		GitRoots: map[string]string{"/repo": "/repo"},
		Branches: map[string]string{"/repo": "main"},
	}
	m := s.Fuzzy(context.Background(), "fb", []string{"foo/bar", "fb", "barfoo", "xyz"}, 10)
	if len(m) != 2 || m[0].Index != 1 || m[1].Index != 0 || !reflect.DeepEqual(m[1].Indexes, []int{0, 4}) {
		t.Fatalf("Fuzzy = %+v", m)
	}
	if m := s.Fuzzy(context.Background(), "", []string{"a", "b"}, 1); len(m) != 1 {
		t.Fatalf("limit ignored: %+v", m)
	}
	hits, err := s.Grep(context.Background(), pluginapi.GrepRequest{Query: "main"})
	if err != nil || len(hits) != 2 {
		t.Fatalf("Grep = %+v %v", hits, err)
	}
	if hits, _ := s.Grep(context.Background(), pluginapi.GrepRequest{Query: "main", CaseSensitive: true}); len(hits) != 1 {
		t.Fatalf("case sensitive Grep = %+v", hits)
	}
	if hits, _ := s.Grep(context.Background(), pluginapi.GrepRequest{Query: "main", Max: 1}); len(hits) != 1 {
		t.Fatalf("Max ignored: %+v", hits)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.Grep(ctx, pluginapi.GrepRequest{Query: "x"}); pluginapi.CodeOf(err) != pluginapi.CodeCancelled {
		t.Fatalf("cancelled Grep: %v", err)
	}
	if out, err := s.Decode("latin1", []byte{0xe9}); err != nil || out != "é" {
		t.Fatalf("Decode latin1 = %q %v", out, err)
	}
	if _, err := s.Decode("klingon", nil); pluginapi.CodeOf(err) != pluginapi.CodeUnsupported {
		t.Fatalf("Decode unknown: %v", err)
	}
	if root, ok := s.GitRoot("/repo/sub/dir"); !ok || root != "/repo" {
		t.Fatalf("GitRoot = %q %v", root, ok)
	}
	if _, ok := s.GitRoot("/elsewhere"); ok {
		t.Fatal("GitRoot found a root outside")
	}
	if b, err := s.GitBranch("/repo"); err != nil || b != "main" {
		t.Fatalf("GitBranch = %q %v", b, err)
	}
	if _, err := s.GitBranch("/nope"); err == nil {
		t.Fatal("GitBranch of a non-repository")
	}
}

func TestFakeHostBuffersAndResources(t *testing.T) {
	f := plugintest.NewFakeHost()
	a := f.AddBuffer("/src/a.go", "one\ntwo\nthree")
	b := f.AddBuffer("b.txt", "")
	if f.ActiveBuffer() != a || f.FindBuffer("b.txt") != b || f.FindBuffer("none") != 0 {
		t.Fatalf("buffers: active %d find %d", f.ActiveBuffer(), f.FindBuffer("b.txt"))
	}
	info, ok := f.Buffer(a)
	if !ok || info.Name != "a.go" || info.LineCount != 3 || !info.Active || info.Version.IsZero() {
		t.Fatalf("info = %+v", info)
	}
	lines, err := f.Lines(a, 1, 1)
	if err != nil || !reflect.DeepEqual(lines, []string{"two"}) {
		t.Fatalf("Lines = %v %v", lines, err)
	}
	if lines, _ := f.Lines(a, 1, -1); len(lines) != 2 {
		t.Fatalf("Lines to the end = %v", lines)
	}
	if lines, _ := f.Lines(a, 99, 5); len(lines) != 0 {
		t.Fatalf("Lines past the end = %v", lines)
	}
	if _, err := f.Lines(99, 0, 1); pluginapi.CodeOf(err) != pluginapi.CodeClosed {
		t.Fatalf("Lines of a closed buffer: %v", err)
	}
	f.SetActive(b)
	if i, _ := f.Buffer(a); i.Active {
		t.Fatal("two active buffers")
	}
	f.RemoveBuffer(b)
	if f.ActiveBuffer() != a || len(f.ListBuffers()) != 1 {
		t.Fatalf("after closing the active buffer: active %d", f.ActiveBuffer())
	}

	f.SetCommands("p", []pluginapi.CommandInfo{{ID: "p.x"}, {ID: "p.y"}})
	f.SetBindings("p", []pluginapi.Binding{{Chord: "ctrl+k"}})
	f.SetModes("p", []pluginapi.ModeInfo{{Name: "normal"}})
	f.SetContext("p", "on", true)
	f.SetContext("p", "off", false)
	f.SetStatusItems("p", []pluginapi.StatusItem{{ID: "s"}})
	_ = f.SetInputMode("p", "normal")
	f.SetCommands("q", []pluginapi.CommandInfo{{ID: "q.z"}})
	want := []string{"binding:p/ctrl+k", "command:p/p.x", "command:p/p.y", "command:q/q.z", "context:p/on", "input_mode:p/normal", "mode:p/normal", "status:p/s"}
	if got := f.Resources(); !reflect.DeepEqual(got, want) {
		t.Fatalf("Resources = %v", got)
	}
	f.OwnerGone("p", 3)
	if got := f.Resources(); !reflect.DeepEqual(got, []string{"command:q/q.z"}) {
		t.Fatalf("after OwnerGone: %v", got)
	}
	if len(f.Gone) != 1 || f.Gone[0] != (plugintest.Gone{Plugin: "p", Gen: 3}) {
		t.Fatalf("Gone = %+v", f.Gone)
	}
	if f.CallCount("SetCommands") != 2 || len(f.Calls()) == 0 {
		t.Fatalf("calls = %v", f.Calls())
	}
	if w, h := f.ScreenSize(); w != 80 || h != 24 {
		t.Fatalf("screen %dx%d", w, h)
	}
}

func TestHarnessGroupsFilesIntoPlugins(t *testing.T) {
	h := plugintest.New(t, plugintest.Files{
		"zeta.lua":           `T_ = 1`,
		"alpha/init.lua":     `require "util"`,
		"alpha/lua/util.lua": `return {}`,
		"mid.lua":            ``,
	}, plugintest.Options{})
	var names []string
	for _, s := range h.Sources {
		names = append(names, s.Name)
	}
	if !reflect.DeepEqual(names, []string{"alpha", "mid", "zeta"}) {
		t.Fatalf("sources = %v", names)
	}
	if h.Sources[0].Dir == "" || h.Sources[1].Dir != "" {
		t.Fatalf("dir plugins have a directory, single files do not: %+v %+v", h.Sources[0], h.Sources[1])
	}
	if len(h.Sources[0].Compiled) != 2 {
		t.Fatalf("compiled = %v", h.Sources[0].Compiled)
	}
	if h.CompileOnUIGoroutine {
		t.Fatal("compiling must run off the UI goroutine")
	}
}

func TestHarnessTimersOnTheManualClock(t *testing.T) {
	h := plugintest.New(t, plugintest.Files{}, plugintest.Options{})
	h.Start()
	t0 := h.Now()
	if !h.RunWork([]pluginapi.Work{{Token: 7, After: time.Second}, {Token: 8, After: 3 * time.Second}}) {
		t.Fatal("timers did not register")
	}
	if h.PendingTimers() != 2 {
		t.Fatalf("pending = %d", h.PendingTimers())
	}
	h.Advance(500 * time.Millisecond)
	if h.PendingTimers() != 2 {
		t.Fatalf("pending after 0.5 s = %d", h.PendingTimers())
	}
	h.Advance(600 * time.Millisecond)
	if h.PendingTimers() != 1 {
		t.Fatalf("pending after 1.1 s = %d", h.PendingTimers())
	}
	h.Advance(2 * time.Second)
	if h.PendingTimers() != 0 {
		t.Fatalf("pending after 3.1 s = %d", h.PendingTimers())
	}
	if got := h.Now().Sub(t0); got != 3100*time.Millisecond {
		t.Fatalf("the clock moved %v", got)
	}
}

func goid() string {
	var buf [64]byte
	return string(buf[:runtime.Stack(buf[:], false)])[:20]
}

func TestHarnessRunsWorkOffTheCallerGoroutine(t *testing.T) {
	h := plugintest.New(t, plugintest.Files{}, plugintest.Options{})
	h.Start()
	var inWork string
	h.RunWork([]pluginapi.Work{{Token: 1, Run: func(ctx context.Context) pluginapi.Completion {
		inWork = goid()
		return pluginapi.Completion{Token: 1}
	}}})
	if inWork == "" || inWork == goid() {
		t.Fatalf("Work ran on the test goroutine (%q): it must run like a tea.Cmd", inWork)
	}
}
