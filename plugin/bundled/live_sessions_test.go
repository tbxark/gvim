package bundled_test

import (
	"context"
	"io/fs"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tbxark/gvim/plugin/bundled"
	"github.com/tbxark/gvim/plugin/plugintest"
	"github.com/tbxark/gvim/pluginapi"
)

const liveSessionsPlugin = "live_sessions"

func liveSessionsFiles(t *testing.T) plugintest.Files {
	t.Helper()
	files := plugintest.Files{}
	for _, name := range []string{"init.lua", liveSessionsPlugin + ".i18n.json"} {
		data, err := fs.ReadFile(bundled.FS, "plugins/"+liveSessionsPlugin+"/"+name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		files[liveSessionsPlugin+"/"+name] = string(data)
	}
	return files
}

func liveSessionsHost(t *testing.T) (*plugintest.FakeHost, *plugintest.FakeServices) {
	t.Helper()
	host := plugintest.NewFakeHost()
	host.Workspace().RootDir = "/w"
	host.AddBuffer("/w/a.go", "package a\n")
	svc := &plugintest.FakeServices{AgentDiscovery: pluginapi.AgentReport{
		Supported: true, Installed: true,
		Sessions: []pluginapi.AgentSession{
			{Kind: "tmux", Tool: "tmux", ID: "s/work", Title: "work", Path: "/w", Dir: "/w", Agents: []string{"claude"}, Attached: true},
			{Kind: "history", Tool: "claude-code", ID: "abc", Title: "proj", Path: "/home/u/.claude/p/a.jsonl", Dir: "/w", Agent: "claude", Exact: true},
		},
	}}
	return host, svc
}

func startLiveSessions(t *testing.T, host *plugintest.FakeHost, svc *plugintest.FakeServices) *plugintest.Harness {
	t.Helper()
	h := plugintest.New(t, liveSessionsFiles(t), plugintest.Options{Host: host, Services: svc, Timeout: 2 * time.Second, EventBudget: 5 * time.Second})
	h.Start()
	return h
}

func TestLiveSessionsListsAndOpensHistory(t *testing.T) {
	t.Parallel()
	host, svc := liveSessionsHost(t)
	h := startLiveSessions(t, host, svc)
	if err := h.Run(liveSessionsPlugin+".show", nil); err != nil {
		t.Fatal(err)
	}
	h.Advance(0)
	o := h.Host.UI().Last("picker")
	if o == nil || !o.Open {
		t.Fatalf("no picker (notices %v log %v)", h.NoticeTexts(), h.LogText(""))
	}
	if len(o.Picker.Items) != 2 {
		t.Fatalf("picker items = %+v", o.Picker.Items)
	}
	if !strings.Contains(o.Picker.Items[0].Text, "tmux") || !strings.Contains(o.Picker.Items[0].Text, "claude") {
		t.Fatalf("tmux row = %+v", o.Picker.Items[0])
	}
	if !strings.Contains(o.Picker.Items[1].Text, "Claude Code") || !strings.Contains(o.Picker.Items[1].Detail, "/w") {
		t.Fatalf("history row = %+v", o.Picker.Items[1])
	}

	// Enter on the transcript row opens its file; it does not attach to a process.
	h.Resolve(pluginapi.Completion{Token: o.Token, Values: []any{1}})
	h.CompleteBuffers()
	var opened bool
	for _, c := range host.Calls() {
		if c.Method == "Open" {
			if req, ok := c.Args[1].(pluginapi.OpenRequest); ok && req.Path == "/home/u/.claude/p/a.jsonl" && req.Focus {
				opened = true
			}
		}
	}
	if !opened {
		t.Fatalf("no transcript open request (calls %v log %v)", host.Calls(), h.LogText(""))
	}
}

func TestLiveSessionsUnsupportedAndEmpty(t *testing.T) {
	t.Parallel()
	host, svc := liveSessionsHost(t)
	svc.AgentDiscovery = pluginapi.AgentReport{}
	h := startLiveSessions(t, host, svc)
	if err := h.Run(liveSessionsPlugin+".show", nil); err != nil {
		t.Fatal(err)
	}
	h.Advance(0)
	o := h.Host.UI().Last("picker")
	if o == nil || len(o.Picker.Items) != 1 || !o.Picker.Items[0].Disabled {
		t.Fatalf("unsupported picker = %+v", o)
	}
	if !strings.Contains(o.Picker.Items[0].Text, "not available") {
		t.Fatalf("unsupported row = %q", o.Picker.Items[0].Text)
	}

	// A supported scan with no rows has its own empty state, not the unsupported one.
	svc.AgentDiscovery = pluginapi.AgentReport{Supported: true}
	if err := h.Run(liveSessionsPlugin+".show", nil); err != nil {
		t.Fatal(err)
	}
	h.Advance(0)
	if last := h.Host.UI().Last("picker"); last == nil || len(last.Picker.Items) != 1 || !strings.Contains(last.Picker.Items[0].Text, "No ") {
		t.Fatalf("empty picker = %+v", last)
	}
}

func TestLiveSessionsPollsWhileOpen(t *testing.T) {
	t.Parallel()
	host, svc := liveSessionsHost(t)
	h := startLiveSessions(t, host, svc)
	if err := h.Run(liveSessionsPlugin+".show", nil); err != nil {
		t.Fatal(err)
	}
	h.Advance(0)
	o := h.Host.UI().Last("picker")
	if o == nil {
		t.Fatal("no picker")
	}
	svc.AgentDiscovery = pluginapi.AgentReport{Supported: true, Installed: true, Sessions: []pluginapi.AgentSession{
		{Kind: "history", Tool: "codex-cli", ID: "new", Title: "newer", Path: "/home/u/.codex/new.jsonl", Dir: "/w", Agent: "codex"},
	}}
	h.Advance(60 * time.Second)
	h.Advance(0)
	if len(o.Picker.Items) != 1 || !strings.Contains(o.Picker.Items[0].Text, "Codex CLI") {
		t.Fatalf("polled rows = %+v", o.Picker.Items)
	}
}

func TestLiveSessionsTmuxRowIsInformational(t *testing.T) {
	t.Parallel()
	host, svc := liveSessionsHost(t)
	h := startLiveSessions(t, host, svc)
	if err := h.Run(liveSessionsPlugin+".show", nil); err != nil {
		t.Fatal(err)
	}
	h.Advance(0)
	o := h.Host.UI().Last("picker")
	if o == nil {
		t.Fatal("no picker")
	}
	h.Resolve(pluginapi.Completion{Token: o.Token, Values: []any{0}})
	if got := strings.Join(h.NoticeTexts(), "|"); !strings.Contains(got, "attach is not available") || !strings.Contains(got, "/w") {
		t.Fatalf("notices = %q, want the tmux no-attach state", got)
	}
}

func TestLiveSessionsReopensDuringInFlightScan(t *testing.T) {
	t.Parallel()
	host, svc := liveSessionsHost(t)
	started := make(chan int, 2)
	release := make(chan pluginapi.AgentReport, 2)
	var calls atomic.Int32
	svc.AgentDiscoveryFn = func(ctx context.Context) (pluginapi.AgentReport, error) {
		n := int(calls.Add(1))
		started <- n
		select {
		case report := <-release:
			return report, nil
		case <-ctx.Done():
			return pluginapi.AgentReport{}, ctx.Err()
		}
	}
	h := startLiveSessions(t, host, svc)

	// Start the first show without settling its async scan: the old command task parks on
	// the discovery Work, which the test runs by hand so it can interleave a close/reopen.
	if err := h.RT.Run(liveSessionsPlugin+".show", nil, pluginapi.SrcAPI); err != nil {
		t.Fatal(err)
	}
	firstWork := runnableWork(h)
	if firstWork == nil {
		t.Fatal("no first discovery Work")
	}
	first := h.Host.UI().Last("picker")
	if first == nil {
		t.Fatal("first picker did not open")
	}
	firstResult := make(chan pluginapi.Completion, 1)
	go func() { firstResult <- firstWork.Run(firstWork.Ctx) }()
	if n := <-started; n != 1 {
		t.Fatalf("first scan call = %d, want 1", n)
	}

	// Close the picker and immediately reopen it while the first scan is still blocked.
	h.RT.Resolve(pluginapi.Completion{Token: first.Token})
	drainRuntime(h)
	if err := h.RT.Run(liveSessionsPlugin+".show", nil, pluginapi.SrcAPI); err != nil {
		t.Fatal(err)
	}
	drainRuntime(h)
	second := h.Host.UI().Last("picker")
	if second == nil || second == first {
		t.Fatalf("second picker = %+v, want a new picker", second)
	}
	if len(second.Picker.Items) != 1 || !strings.Contains(second.Picker.Items[0].Text, "Scanning") {
		t.Fatalf("reopened picker = %+v, want its scanning state", second.Picker.Items)
	}

	// The old scan finishes stale. It must consume the reopened picker's pending refresh and
	// start a new scan immediately, rather than leaving the picker to the 60-second tick.
	release <- pluginapi.AgentReport{Supported: true, Installed: true, Sessions: []pluginapi.AgentSession{
		{Kind: "history", Tool: "claude-code", ID: "old", Title: "old", Path: "/old.jsonl", Dir: "/w", Agent: "claude"},
	}}
	oldResult := <-firstResult
	h.RT.Resolve(oldResult)
	retryWork := runnableWork(h)
	if retryWork == nil {
		t.Fatal("no retry discovery Work")
	}
	retryResult := make(chan pluginapi.Completion, 1)
	go func() { retryResult <- retryWork.Run(retryWork.Ctx) }()
	if n := <-started; n != 2 {
		t.Fatalf("retry scan call = %d, want 2", n)
	}
	release <- pluginapi.AgentReport{Supported: true, Installed: true, Sessions: []pluginapi.AgentSession{
		{Kind: "history", Tool: "codex-cli", ID: "fresh", Title: "fresh", Path: "/fresh.jsonl", Dir: "/w", Agent: "codex"},
	}}
	h.RT.Resolve(<-retryResult)
	drainRuntime(h)
	if len(second.Picker.Items) != 1 || !strings.Contains(second.Picker.Items[0].Text, "Codex CLI · fresh") {
		t.Fatalf("reopened picker rows = %+v, want the fresh scan result", second.Picker.Items)
	}
}

// runnableWork drains until the runtime hands out a Work with a Run closure (the scan),
// ignoring queued timers. The test runs that Work by hand so it can interleave host
// completions around a close/reopen.
func runnableWork(h *plugintest.Harness) *pluginapi.Work {
	for i := 0; i < 20; i++ {
		work, more := h.RT.Drain(100 * time.Millisecond)
		for j := range work {
			if work[j].Run != nil {
				return &work[j]
			}
		}
		if !more {
			return nil
		}
	}
	return nil
}

// drainRuntime pumps the runtime without running any Work; async scans in these tests are
// started by hand so the test can interleave host completions.
func drainRuntime(h *plugintest.Harness) {
	for i := 0; i < 20; i++ {
		work, more := h.RT.Drain(100 * time.Millisecond)
		if len(work) == 0 && !more {
			return
		}
	}
}
