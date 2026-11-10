package bundled_test

import (
	"context"
	"io/fs"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tbxark/gvim/plugin/bundled"
	"github.com/tbxark/gvim/plugin/plugintest"
	"github.com/tbxark/gvim/pluginapi"
)

const auditPlugin = "audit_mode"

func auditFiles(t *testing.T) plugintest.Files {
	t.Helper()
	files := plugintest.Files{}
	for _, name := range []string{"init.lua", auditPlugin + ".i18n.json"} {
		data, err := fs.ReadFile(bundled.FS, "plugins/"+auditPlugin+"/"+name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		files[auditPlugin+"/"+name] = string(data)
	}
	return files
}

func auditHost(t *testing.T) (*plugintest.FakeHost, *plugintest.FakeOS) {
	t.Helper()
	host := plugintest.NewFakeHost()
	host.Workspace().RootDir = "/w"
	host.AddBuffer("/w/a.go", "package a\n")
	os := plugintest.NewFakeOS()
	os.Bin["git"] = "/usr/bin/git"
	for _, name := range []string{"a.go", "staged.go", "new.txt"} {
		os.SetFile("/w/"+name, "line\n")
	}
	os.Script(gitArgv("rev-parse", "--show-toplevel", "--git-path", "HEAD"), plugintest.ProcScript{Stdout: "/w\n.git/HEAD\n"})
	os.Script(gitArgv("--no-optional-locks", "status", "--porcelain", "-z", "-uall"), plugintest.ProcScript{Stdout: " M a.go\x00A  staged.go\x00?? new.txt\x00"})
	os.Script(gitArgv("diff", "--no-ext-diff", "--no-textconv", "--no-color", "--numstat", "--"), plugintest.ProcScript{Stdout: "2\t1\ta.go\n"})
	os.Script(gitArgv("diff", "--no-ext-diff", "--no-textconv", "--no-color", "--cached", "--numstat", "--"), plugintest.ProcScript{Stdout: "3\t0\tstaged.go\n"})
	os.Script(gitArgv("diff", "--no-ext-diff", "--no-textconv", "--no-color", "--unified=3", "--", "a.go"), plugintest.ProcScript{Stdout: "diff --git a/a.go b/a.go\n--- a/a.go\n+++ b/a.go\n@@ -1,2 +1,3 @@\n package a\n-old\n+new\n+extra\n"})
	os.Script(gitArgv("diff", "--no-ext-diff", "--no-textconv", "--no-color", "--cached", "--unified=3", "--", "staged.go"), plugintest.ProcScript{Stdout: "diff --git a/staged.go b/staged.go\n--- /dev/null\n+++ b/staged.go\n@@ -0,0 +1 @@\n+staged\n"})
	return host, os
}

func startAudit(t *testing.T, host *plugintest.FakeHost, os *plugintest.FakeOS) *plugintest.Harness {
	t.Helper()
	h := plugintest.New(t, auditFiles(t), plugintest.Options{Host: host, OS: os, Timeout: 2 * time.Second, EventBudget: 5 * time.Second})
	h.Start()
	return h
}

func TestAuditModeListsChangesAndOpensDiff(t *testing.T) {
	t.Parallel()
	host, os := auditHost(t)
	h := startAudit(t, host, os)
	if err := h.Run(auditPlugin+".review", nil); err != nil {
		t.Fatal(err)
	}
	h.Advance(0)
	o := h.Host.UI().Last("picker")
	if o == nil || !o.Open {
		t.Fatalf("no review picker (notices %v log %v)", h.NoticeTexts(), h.LogText(""))
	}
	var texts []string
	for _, it := range o.Picker.Items {
		texts = append(texts, it.Text)
	}
	joined := strings.Join(texts, "|")
	for _, want := range []string{"STAGED (1)", "UNSTAGED (1)", "UNTRACKED (1)", "A  staged.go", "M  a.go", "?  new.txt"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("picker rows = %q, missing %q", joined, want)
		}
	}
	// The description object carries the numstat; the host only shows the text.
	if !strings.Contains(o.Picker.Items[1].Detail, "+3 -0") && !strings.Contains(o.Picker.Items[2].Detail, "+2 -1") {
		t.Fatalf("numstat details = %+v", o.Picker.Items)
	}

	// Find the unstaged a.go row and choose it by its picker item index (0-based ID).
	idx := -1
	for i, it := range o.Picker.Items {
		if strings.Contains(it.Text, "M  a.go") {
			idx = i
			break
		}
	}
	if idx < 0 {
		t.Fatalf("no a.go row: %+v", o.Picker.Items)
	}
	h.Resolve(pluginapi.Completion{Token: o.Token, Values: []any{idx}})
	h.CompleteBuffers()

	var diffID pluginapi.BufferID
	for _, b := range host.Buffers {
		if b.Info.Virtual && strings.Contains(b.Info.Name, "Review Diff: a.go") {
			diffID = b.Info.ID
		}
	}
	if diffID == 0 {
		t.Fatalf("no review diff buffer (buffers %+v log %v)", host.Buffers, h.LogText(""))
	}
	var diffText strings.Builder
	for _, b := range host.Buffers {
		if b.Info.ID == diffID {
			diffText.WriteString(strings.Join(b.Lines, "\n"))
		}
	}
	if !strings.Contains(diffText.String(), "+new") || !strings.Contains(diffText.String(), "+extra") {
		t.Fatalf("diff = %q", diffText.String())
	}
	targets := host.LineTargets(diffID)
	if len(targets) == 0 {
		t.Fatalf("diff has no line targets")
	}
	found := false
	for _, target := range targets {
		if target.Path == "/w/a.go" && target.Target == 1 { // new "+new" is 0-based line 1
			found = true
		}
	}
	if !found {
		t.Fatalf("line targets = %+v, want /w/a.go target 1", targets)
	}
}

func TestAuditModeCleanAndNotRepo(t *testing.T) {
	t.Parallel()
	host, os := auditHost(t)
	os.Rescript(gitArgv("--no-optional-locks", "status", "--porcelain", "-z", "-uall"), plugintest.ProcScript{Stdout: ""})
	h := startAudit(t, host, os)
	if err := h.Run(auditPlugin+".review", nil); err != nil {
		t.Fatal(err)
	}
	h.Advance(0)
	o := h.Host.UI().Last("picker")
	if o == nil || len(o.Picker.Items) != 1 || !o.Picker.Items[0].Disabled || !strings.Contains(o.Picker.Items[0].Text, "clean") {
		t.Fatalf("clean picker = %+v", o)
	}

	// A failed git status/not-a-repository scan says so instead of showing a fake clean list.
	host2, os2 := auditHost(t)
	os2.Rescript(gitArgv("rev-parse", "--show-toplevel", "--git-path", "HEAD"), plugintest.ProcScript{Code: 128, Stderr: "fatal: not a git repository\n"})
	h2 := startAudit(t, host2, os2)
	if err := h2.Run(auditPlugin+".review", nil); err != nil {
		t.Fatal(err)
	}
	h2.Advance(0)
	p := h2.Host.UI().Last("picker")
	if p == nil || len(p.Picker.Items) != 1 || !strings.Contains(p.Picker.Items[0].Text, "git repository") {
		t.Fatalf("not-repo picker = %+v notices %v", p, h2.NoticeTexts())
	}
}

func TestAuditModeRegistersReadOnlyCommands(t *testing.T) {
	t.Parallel()
	host, os := auditHost(t)
	h := startAudit(t, host, os)
	var ids []string
	for _, c := range h.Host.Commands[auditPlugin] {
		if !c.Hidden {
			ids = append(ids, c.ID)
		}
	}
	want := []string{auditPlugin + ".close", auditPlugin + ".refresh", auditPlugin + ".review"}
	if strings.Join(ids, "|") != strings.Join(want, "|") {
		t.Fatalf("commands = %v, want %v", ids, want)
	}
	for _, id := range ids {
		for _, write := range []string{"stage", "unstage", "discard", "comment", "export"} {
			if strings.Contains(id, write) {
				t.Fatalf("read-only port registered %q", id)
			}
		}
	}
}

func TestAuditModeShowsUntrackedFileAsNewFile(t *testing.T) {
	t.Parallel()
	host, os := auditHost(t)
	os.SetFile("/w/new.txt", "line one\nline two\n")
	h := startAudit(t, host, os)
	if err := h.Run(auditPlugin+".review", nil); err != nil {
		t.Fatal(err)
	}
	h.Advance(0)
	o := h.Host.UI().Last("picker")
	if o == nil {
		t.Fatal("no picker")
	}
	idx := -1
	for i, it := range o.Picker.Items {
		if strings.Contains(it.Text, "new.txt") {
			idx = i
		}
	}
	if idx < 0 {
		t.Fatalf("no untracked row: %+v", o.Picker.Items)
	}
	h.Resolve(pluginapi.Completion{Token: o.Token, Values: []any{idx}})
	h.CompleteBuffers()
	var diffID pluginapi.BufferID
	for _, b := range host.Buffers {
		if b.Info.Virtual && strings.Contains(b.Info.Name, "Review Diff: new.txt") {
			diffID = b.Info.ID
		}
	}
	if diffID == 0 {
		t.Fatalf("no untracked diff buffer")
	}
	var text string
	for _, b := range host.Buffers {
		if b.Info.ID == diffID {
			text = strings.Join(b.Lines, "\n")
		}
	}
	if !strings.Contains(text, "+line one") || !strings.Contains(text, "+line two") {
		t.Fatalf("untracked view = %q", text)
	}
	targets := host.LineTargets(diffID)
	if len(targets) == 0 || targets[0].Path != "/w/new.txt" || targets[0].Target != 0 {
		t.Fatalf("untracked targets = %+v", targets)
	}
}

// slowReadFakeOS blocks ReadFile until the test releases it, so an untracked review can be
// closed while its working-tree read is still in flight.
type slowReadFakeOS struct {
	*plugintest.FakeOS
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (s *slowReadFakeOS) ReadFile(ctx context.Context, path string, max int64) ([]byte, error) {
	s.once.Do(func() { close(s.started) })
	select {
	case <-s.release:
		return s.FakeOS.ReadFile(ctx, path, max)
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func TestAuditModeClosesWhileUntrackedReadIsInFlight(t *testing.T) {
	t.Parallel()
	host, plain := auditHost(t)
	slow := &slowReadFakeOS{FakeOS: plain, started: make(chan struct{}), release: make(chan struct{})}
	h := plugintest.New(t, auditFiles(t), plugintest.Options{Host: host, OS: slow, Timeout: 5 * time.Second, EventBudget: 5 * time.Second})
	h.Start()
	if err := h.Run(auditPlugin+".review", nil); err != nil {
		t.Fatal(err)
	}
	h.Advance(0)
	o := h.Host.UI().Last("picker")
	if o == nil {
		t.Fatal("no picker")
	}
	idx := -1
	for i, it := range o.Picker.Items {
		if strings.Contains(it.Text, "new.txt") {
			idx = i
		}
	}
	if idx < 0 {
		t.Fatalf("no untracked row: %+v", o.Picker.Items)
	}
	// Select without Settle: the on_select task starts a slow ReadFile Work, which the test
	// holds open while it closes the review.
	h.RT.Resolve(pluginapi.Completion{Token: o.Token, Values: []any{idx}})
	readWork := runnableWork(h)
	if readWork == nil {
		t.Fatal("no untracked ReadFile Work")
	}
	readDone := make(chan pluginapi.Completion, 1)
	go func() { readDone <- readWork.Run(readWork.Ctx) }()
	<-slow.started
	if err := h.RT.Run(auditPlugin+".close", nil, pluginapi.SrcAPI); err != nil {
		t.Fatal(err)
	}
	drainRuntime(h)
	close(slow.release)
	h.RT.Resolve(<-readDone)
	drainRuntime(h)
	for _, b := range host.Buffers {
		if b.Info.Virtual && strings.Contains(b.Info.Name, "Review Diff: new.txt") {
			t.Fatalf("stale untracked read opened a diff after close: %+v", b.Info)
		}
	}
}

func TestAuditModeRefreshClearsTruncationFooter(t *testing.T) {
	t.Parallel()
	host, os := auditHost(t)
	os.Rescript(gitArgv("--no-optional-locks", "status", "--porcelain", "-z", "-uall"), plugintest.ProcScript{Stdout: " M a.go\x00", Truncated: true})
	h := startAudit(t, host, os)
	if err := h.Run(auditPlugin+".review", nil); err != nil {
		t.Fatal(err)
	}
	h.Advance(0)
	o := h.Host.UI().Last("picker")
	if o == nil || !strings.Contains(o.Picker.Footer, "truncated") {
		t.Fatalf("truncated footer = %q, picker %+v", o.Picker.Footer, o)
	}
	os.Rescript(gitArgv("--no-optional-locks", "status", "--porcelain", "-z", "-uall"), plugintest.ProcScript{Stdout: ""})
	if err := h.Run(auditPlugin+".refresh", nil); err != nil {
		t.Fatal(err)
	}
	h.Advance(0)
	if o.Picker.Footer != "" {
		t.Fatalf("footer after clean refresh = %q, want reset", o.Picker.Footer)
	}
	if len(o.Picker.Items) != 1 || !strings.Contains(o.Picker.Items[0].Text, "clean") {
		t.Fatalf("clean rows = %+v", o.Picker.Items)
	}
}

func TestAuditModeSeparatesStagedAndUnstagedNumstat(t *testing.T) {
	t.Parallel()
	host, os := auditHost(t)
	os.Rescript(gitArgv("--no-optional-locks", "status", "--porcelain", "-z", "-uall"), plugintest.ProcScript{Stdout: "MM a.go\x00"})
	os.Rescript(gitArgv("diff", "--no-ext-diff", "--no-textconv", "--no-color", "--numstat", "--"), plugintest.ProcScript{Stdout: "1\t0\ta.go\n"})
	os.Rescript(gitArgv("diff", "--no-ext-diff", "--no-textconv", "--no-color", "--cached", "--numstat", "--"), plugintest.ProcScript{Stdout: "2\t0\ta.go\n"})
	h := startAudit(t, host, os)
	if err := h.Run(auditPlugin+".review", nil); err != nil {
		t.Fatal(err)
	}
	h.Advance(0)
	o := h.Host.UI().Last("picker")
	if o == nil {
		t.Fatal("no picker")
	}
	var aRows []pluginapi.PickItem
	for _, it := range o.Picker.Items {
		if strings.Contains(it.Text, "a.go") {
			aRows = append(aRows, it)
		}
	}
	if len(aRows) != 2 {
		t.Fatalf("MM rows = %+v, want staged and unstaged", o.Picker.Items)
	}
	seen := map[string]bool{}
	for _, it := range aRows {
		seen[it.Detail] = true
	}
	if !seen["+2 -0"] || !seen["+1 -0"] {
		t.Fatalf("MM details = %+v, want separate staged/unstaged stats", aRows)
	}
	var status string
	for _, u := range o.Updates {
		if u.Status != nil {
			status = *u.Status
		}
	}
	if !strings.Contains(status, "+3 -0") || strings.Contains(status, "+4 -0") {
		t.Fatalf("review status = %q, want total +3 -0", status)
	}
}
