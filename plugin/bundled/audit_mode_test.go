package bundled_test

import (
	"context"
	"fmt"
	"io/fs"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tbxark/gvim/plugin/bundled"
	"github.com/tbxark/gvim/plugin/hostos"
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
	want := []string{auditPlugin + ".close", auditPlugin + ".next_hunk", auditPlugin + ".prev_hunk", auditPlugin + ".refresh", auditPlugin + ".review"}
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

func TestAuditModeNextPreviousHunk(t *testing.T) {
	t.Parallel()
	host, os := auditHost(t)
	os.Rescript(gitArgv("--no-optional-locks", "status", "--porcelain", "-z", "-uall"), plugintest.ProcScript{Stdout: " M a.go\x00"})
	os.Rescript(gitArgv("diff", "--no-ext-diff", "--no-textconv", "--no-color", "--unified=3", "--", "a.go"), plugintest.ProcScript{Stdout: "diff --git a/a.go b/a.go\n--- a/a.go\n+++ b/a.go\n@@ -1 +1 @@\n-old\n+new\n@@ -10 +10 @@\n-old2\n+new2\n"})
	h := startAudit(t, host, os)
	if err := h.Run(auditPlugin+".review", nil); err != nil {
		t.Fatal(err)
	}
	h.Advance(0)
	o := h.Host.UI().Last("picker")
	idx := -1
	for i, it := range o.Picker.Items {
		if strings.Contains(it.Text, "a.go") {
			idx = i
		}
	}
	if idx < 0 {
		t.Fatalf("no a.go row: %+v", o.Picker.Items)
	}
	h.Resolve(pluginapi.Completion{Token: o.Token, Values: []any{idx}})
	var diffID pluginapi.BufferID
	for _, b := range host.Buffers {
		if b.Info.Virtual && strings.Contains(b.Info.Name, "Review Diff: a.go") {
			diffID = b.Info.ID
		}
	}
	if diffID == 0 {
		t.Fatal("no diff buffer")
	}
	cursorLine := func() int {
		cursors, _ := host.Cursors(diffID)
		if len(cursors) == 0 {
			t.Fatal("no diff cursor")
		}
		return cursors[0].Pos.Line
	}
	// Switching to another tab first: the old diff buffer is still valid, but the command must
	// refuse to move its hidden cursor.
	host.SetActive(host.Buffers[0].Info.ID)
	if err := h.Run(auditPlugin+".next_hunk", nil); err != nil {
		t.Fatal(err)
	}
	if got := cursorLine(); got != 0 {
		t.Fatalf("hidden diff cursor moved to %d", got)
	}
	if got := strings.Join(h.NoticeTexts(), "|"); !strings.Contains(got, "switch to the open diff view") {
		t.Fatalf("hidden-diff notice = %q", got)
	}
	host.SetActive(diffID)

	// @@ headers are rows 3 and 6 (0-based); the commands move the diff cursor.
	if err := h.Run(auditPlugin+".next_hunk", nil); err != nil {
		t.Fatal(err)
	}
	if got := cursorLine(); got != 3 {
		t.Fatalf("first next_hunk line = %d, want 3", got)
	}
	if err := h.Run(auditPlugin+".next_hunk", nil); err != nil {
		t.Fatal(err)
	}
	if got := cursorLine(); got != 6 {
		t.Fatalf("second next_hunk line = %d, want 6", got)
	}
	if err := h.Run(auditPlugin+".next_hunk", nil); err != nil {
		t.Fatal(err)
	}
	if got := cursorLine(); got != 6 {
		t.Fatalf("next at the last hunk moved to %d", got)
	}
	if got := strings.Join(h.NoticeTexts(), "|"); !strings.Contains(got, "last hunk") {
		t.Fatalf("last-hunk notice = %q", got)
	}
	if err := h.Run(auditPlugin+".prev_hunk", nil); err != nil {
		t.Fatal(err)
	}
	if got := cursorLine(); got != 3 {
		t.Fatalf("prev_hunk line = %d, want 3", got)
	}
	if err := h.Run(auditPlugin+".prev_hunk", nil); err != nil {
		t.Fatal(err)
	}
	if got := cursorLine(); got != 3 {
		t.Fatalf("prev at the first hunk moved to %d", got)
	}
	if got := strings.Join(h.NoticeTexts(), "|"); !strings.Contains(got, "first hunk") {
		t.Fatalf("first-hunk notice = %q", got)
	}
	if err := h.Run(auditPlugin+".close", nil); err != nil {
		t.Fatal(err)
	}
	if err := h.Run(auditPlugin+".next_hunk", nil); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(h.NoticeTexts(), "|"); !strings.Contains(got, "open a file diff first") {
		t.Fatalf("closed-diff notice = %q", got)
	}
}

// openAuditDiff opens Review Diff and picks a.go from the picker, returning the diff buffer.
func openAuditDiff(t *testing.T, h *plugintest.Harness, host *plugintest.FakeHost) pluginapi.BufferID {
	t.Helper()
	if err := h.Run(auditPlugin+".review", nil); err != nil {
		t.Fatal(err)
	}
	h.Advance(0)
	o := h.Host.UI().Last("picker")
	if o == nil {
		t.Fatal("no review picker")
	}
	idx := -1
	for i, it := range o.Picker.Items {
		if strings.Contains(it.Text, "a.go") {
			idx = i
		}
	}
	if idx < 0 {
		t.Fatalf("no a.go row: %+v", o.Picker.Items)
	}
	h.Resolve(pluginapi.Completion{Token: o.Token, Values: []any{idx}})
	for _, b := range host.Buffers {
		if b.Info.Virtual && strings.Contains(b.Info.Name, "Review Diff: a.go") {
			return b.Info.ID
		}
	}
	t.Fatalf("no diff buffer (notices %v log %v buffers %+v)", h.NoticeTexts(), h.LogText(""), host.Buffers)
	return 0
}

func TestAuditModeHunkNavigationAfterCloseAndReopen(t *testing.T) {
	t.Parallel()
	host, os := auditHost(t)
	os.Rescript(gitArgv("--no-optional-locks", "status", "--porcelain", "-z", "-uall"), plugintest.ProcScript{Stdout: " M a.go\x00"})
	h := startAudit(t, host, os)
	first := openAuditDiff(t, h, host)
	if err := h.Run(auditPlugin+".next_hunk", nil); err != nil {
		t.Fatal(err)
	}
	if cursors, _ := host.Cursors(first); len(cursors) == 0 || cursors[0].Pos.Line != 3 {
		t.Fatalf("first diff cursor = %+v, want row 3", cursors)
	}
	if err := h.Run(auditPlugin+".close", nil); err != nil {
		t.Fatal(err)
	}
	for _, b := range host.Buffers {
		if b.Info.ID == first {
			t.Fatal("close left the old diff buffer open")
		}
	}

	// Reopen with a different (one-hunk) diff; hunk state must be replaced, not carried over.
	os.Rescript(gitArgv("diff", "--no-ext-diff", "--no-textconv", "--no-color", "--unified=3", "--", "a.go"), plugintest.ProcScript{Stdout: "diff --git a/a.go b/a.go\n--- a/a.go\n+++ b/a.go\n@@ -1 +1 @@\n-old\n+new\n"})
	second := openAuditDiff(t, h, host)
	if second == first {
		t.Fatal("reopen reused the closed diff buffer")
	}
	if err := h.Run(auditPlugin+".next_hunk", nil); err != nil {
		t.Fatal(err)
	}
	if cursors, _ := host.Cursors(second); len(cursors) == 0 || cursors[0].Pos.Line != 3 {
		t.Fatalf("reopened diff cursor = %+v, want row 3", cursors)
	}
	if err := h.Run(auditPlugin+".next_hunk", nil); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(h.NoticeTexts(), "|"); !strings.Contains(got, "last hunk") {
		t.Fatalf("reopened diff should have one hunk, notices = %q", got)
	}
}

// slowDiffOS blocks git diff Unified Work so a tracked diff can be closed while it is in
// flight; status/numstat and other git commands still complete normally.
type slowDiffOS struct {
	*plugintest.FakeOS
	started chan struct{}
	release chan struct{}
	match   string
	once    sync.Once
}

func (s *slowDiffOS) Run(ctx context.Context, argv []string, o hostos.ProcOptions) (hostos.ProcResult, error) {
	joined := strings.Join(argv, " ")
	if strings.Contains(joined, "--unified=3") && (s.match == "" || strings.Contains(joined, s.match)) {
		s.once.Do(func() { close(s.started) })
		select {
		case <-s.release:
		case <-ctx.Done():
			return hostos.ProcResult{}, ctx.Err()
		}
	}
	return s.FakeOS.Run(ctx, argv, o)
}

func TestAuditModeHunkNavigationIgnoresStaleDiffLoad(t *testing.T) {
	t.Parallel()
	host, plain := auditHost(t)
	slow := &slowDiffOS{FakeOS: plain, started: make(chan struct{}), release: make(chan struct{})}
	h := plugintest.New(t, auditFiles(t), plugintest.Options{Host: host, OS: slow, Timeout: 5 * time.Second, EventBudget: 5 * time.Second})
	h.Start()
	if err := h.Run(auditPlugin+".review", nil); err != nil {
		t.Fatal(err)
	}
	h.Advance(0)
	picker := h.Host.UI().Last("picker")
	if picker == nil {
		t.Fatal("no picker")
	}
	idx := -1
	for i, it := range picker.Picker.Items {
		if strings.Contains(it.Text, "a.go") {
			idx = i
		}
	}
	if idx < 0 {
		t.Fatalf("no a.go row: %+v", picker.Picker.Items)
	}
	h.RT.Resolve(pluginapi.Completion{Token: picker.Token, Values: []any{idx}})
	diffWork := runnableWork(h)
	if diffWork == nil {
		t.Fatal("no tracked diff Work")
	}
	diffDone := make(chan pluginapi.Completion, 1)
	go func() { diffDone <- diffWork.Run(diffWork.Ctx) }()
	<-slow.started
	// Close while the tracked diff is still running; release it and let the stale completion
	// arrive. It must not create/focus a diff buffer or seed hunk navigation.
	if err := h.RT.Run(auditPlugin+".close", nil, pluginapi.SrcAPI); err != nil {
		t.Fatal(err)
	}
	drainRuntime(h)
	close(slow.release)
	h.RT.Resolve(<-diffDone)
	drainRuntime(h)
	for _, b := range host.Buffers {
		if b.Info.Virtual && strings.Contains(b.Info.Name, "Review Diff: a.go") {
			t.Fatalf("stale tracked diff opened a buffer after close: %+v", b.Info)
		}
	}
	if err := h.Run(auditPlugin+".next_hunk", nil); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(h.NoticeTexts(), "|"); !strings.Contains(got, "open a file diff first") {
		t.Fatalf("stale-diff navigation notice = %q", got)
	}
}

func findAuditDiff(t *testing.T, host *plugintest.FakeHost, name string) pluginapi.BufferID {
	t.Helper()
	for _, b := range host.Buffers {
		if b.Info.Virtual && strings.Contains(b.Info.Name, "Review Diff: "+name) {
			return b.Info.ID
		}
	}
	t.Fatalf("no Review Diff: %s buffer", name)
	return 0
}

func auditDiffLine(t *testing.T, host *plugintest.FakeHost, id pluginapi.BufferID) int {
	t.Helper()
	cursors, _ := host.Cursors(id)
	if len(cursors) == 0 {
		t.Fatalf("no cursor for diff %d", id)
	}
	return cursors[0].Pos.Line
}

func TestAuditModeCrossFileHunkNavigation(t *testing.T) {
	t.Parallel()
	host, os := auditHost(t)
	os.SetFile("/w/b.go", "package b\n")
	os.Rescript(gitArgv("--no-optional-locks", "status", "--porcelain", "-z", "-uall"), plugintest.ProcScript{Stdout: " M a.go\x00 M b.go\x00"})
	os.Rescript(gitArgv("diff", "--no-ext-diff", "--no-textconv", "--no-color", "--numstat", "--"), plugintest.ProcScript{Stdout: "1\t0\ta.go\n2\t0\tb.go\n"})
	os.Rescript(gitArgv("diff", "--no-ext-diff", "--no-textconv", "--no-color", "--cached", "--numstat", "--"), plugintest.ProcScript{Stdout: ""})
	os.Script(gitArgv("diff", "--no-ext-diff", "--no-textconv", "--no-color", "--unified=3", "--", "b.go"), plugintest.ProcScript{Stdout: "diff --git a/b.go b/b.go\n--- a/b.go\n+++ b/b.go\n@@ -1 +1 @@\n-oldb\n+newb\n@@ -10 +10 @@\n-oldb2\n+newb2\n"})
	h := startAudit(t, host, os)
	aID := openAuditDiff(t, h, host)
	if got := auditDiffLine(t, host, aID); got != 0 {
		t.Fatalf("initial a cursor = %d", got)
	}
	// a's single hunk, then cross to b's first hunk.
	if err := h.Run(auditPlugin+".next_hunk", nil); err != nil {
		t.Fatal(err)
	}
	if got := auditDiffLine(t, host, aID); got != 3 {
		t.Fatalf("a hunk cursor = %d, want 3", got)
	}
	if err := h.Run(auditPlugin+".next_hunk", nil); err != nil {
		t.Fatal(err)
	}
	bID := findAuditDiff(t, host, "b.go")
	if got := auditDiffLine(t, host, bID); got != 3 {
		t.Fatalf("cross-file next cursor = %d, want b first hunk 3", got)
	}
	if err := h.Run(auditPlugin+".next_hunk", nil); err != nil {
		t.Fatal(err)
	}
	if got := auditDiffLine(t, host, bID); got != 6 {
		t.Fatalf("b second hunk cursor = %d, want 6", got)
	}
	if err := h.Run(auditPlugin+".prev_hunk", nil); err != nil {
		t.Fatal(err)
	}
	if got := auditDiffLine(t, host, bID); got != 3 {
		t.Fatalf("b prev cursor = %d, want 3", got)
	}
	// Cross back to a's last hunk, then the global first-hunk boundary.
	if err := h.Run(auditPlugin+".prev_hunk", nil); err != nil {
		t.Fatal(err)
	}
	aID = findAuditDiff(t, host, "a.go")
	if got := auditDiffLine(t, host, aID); got != 3 {
		t.Fatalf("cross-file prev cursor = %d, want a hunk 3", got)
	}
	if err := h.Run(auditPlugin+".prev_hunk", nil); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(h.NoticeTexts(), "|"); !strings.Contains(got, "first hunk") {
		t.Fatalf("global first-hunk notice = %q", got)
	}
	// Cross forward again and hit the global last-hunk boundary.
	if err := h.Run(auditPlugin+".next_hunk", nil); err != nil {
		t.Fatal(err)
	}
	if err := h.Run(auditPlugin+".next_hunk", nil); err != nil {
		t.Fatal(err)
	}
	if err := h.Run(auditPlugin+".next_hunk", nil); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(h.NoticeTexts(), "|"); !strings.Contains(got, "last hunk") {
		t.Fatalf("global last-hunk notice = %q", got)
	}
}

func TestAuditModeCrossFileHunkNavigationDiscardsStaleTarget(t *testing.T) {
	t.Parallel()
	host, plain := auditHost(t)
	plain.SetFile("/w/b.go", "package b\n")
	plain.Rescript(gitArgv("--no-optional-locks", "status", "--porcelain", "-z", "-uall"), plugintest.ProcScript{Stdout: " M a.go\x00 M b.go\x00"})
	plain.Rescript(gitArgv("diff", "--no-ext-diff", "--no-textconv", "--no-color", "--numstat", "--"), plugintest.ProcScript{Stdout: "1\t0\ta.go\n1\t0\tb.go\n"})
	plain.Rescript(gitArgv("diff", "--no-ext-diff", "--no-textconv", "--no-color", "--cached", "--numstat", "--"), plugintest.ProcScript{Stdout: ""})
	plain.Script(gitArgv("diff", "--no-ext-diff", "--no-textconv", "--no-color", "--unified=3", "--", "b.go"), plugintest.ProcScript{Stdout: "diff --git a/b.go b/b.go\n--- a/b.go\n+++ b/b.go\n@@ -1 +1 @@\n-old\n+new\n"})
	slow := &slowDiffOS{FakeOS: plain, started: make(chan struct{}), release: make(chan struct{}), match: "-- b.go"}
	h := plugintest.New(t, auditFiles(t), plugintest.Options{Host: host, OS: slow, Timeout: 5 * time.Second, EventBudget: 5 * time.Second})
	h.Start()
	if err := h.Run(auditPlugin+".review", nil); err != nil {
		t.Fatal(err)
	}
	h.Advance(0)
	picker := h.Host.UI().Last("picker")
	idx := -1
	for i, it := range picker.Picker.Items {
		if strings.Contains(it.Text, "a.go") {
			idx = i
		}
	}
	if idx < 0 {
		t.Fatalf("no a.go row: %+v", picker.Picker.Items)
	}
	h.Resolve(pluginapi.Completion{Token: picker.Token, Values: []any{idx}})
	if findAuditDiff(t, host, "a.go") == 0 {
		t.Fatal("no a diff")
	}
	// At a's last hunk, next starts loading b. Close the review while b's diff is blocked;
	// the stale cross-file target must not be opened.
	if err := h.Run(auditPlugin+".next_hunk", nil); err != nil {
		t.Fatal(err)
	}
	if err := h.RT.Run(auditPlugin+".next_hunk", nil, pluginapi.SrcAPI); err != nil {
		t.Fatal(err)
	}
	work := runnableWork(h)
	if work == nil {
		t.Fatal("no cross-file diff Work")
	}
	done := make(chan pluginapi.Completion, 1)
	go func() { done <- work.Run(work.Ctx) }()
	<-slow.started
	if err := h.RT.Run(auditPlugin+".close", nil, pluginapi.SrcAPI); err != nil {
		t.Fatal(err)
	}
	drainRuntime(h)
	close(slow.release)
	h.RT.Resolve(<-done)
	drainRuntime(h)
	for _, b := range host.Buffers {
		if b.Info.Virtual && strings.Contains(b.Info.Name, "Review Diff: b.go") {
			t.Fatalf("stale cross-file target opened after close: %+v", b.Info)
		}
	}
}

func TestAuditModeCrossFileTruncatedCandidateNotices(t *testing.T) {
	t.Parallel()
	host, os := auditHost(t)
	os.SetFile("/w/b.go", "package b\n")
	os.Rescript(gitArgv("--no-optional-locks", "status", "--porcelain", "-z", "-uall"), plugintest.ProcScript{Stdout: " M a.go\x00 M b.go\x00"})
	os.Rescript(gitArgv("diff", "--no-ext-diff", "--no-textconv", "--no-color", "--numstat", "--"), plugintest.ProcScript{Stdout: "1\t0\ta.go\n1\t0\tb.go\n"})
	os.Rescript(gitArgv("diff", "--no-ext-diff", "--no-textconv", "--no-color", "--cached", "--numstat", "--"), plugintest.ProcScript{Stdout: ""})
	os.Script(gitArgv("diff", "--no-ext-diff", "--no-textconv", "--no-color", "--unified=3", "--", "b.go"), plugintest.ProcScript{Stdout: "diff --git a/b.go b/b.go\n--- a/b.go\n+++ b/b.go\n@@ -1 +1 @@\n-old\n+new\n", Truncated: true})
	h := startAudit(t, host, os)
	openAuditDiff(t, h, host)
	if err := h.Run(auditPlugin+".next_hunk", nil); err != nil {
		t.Fatal(err)
	}
	if err := h.Run(auditPlugin+".next_hunk", nil); err != nil {
		t.Fatal(err)
	}
	if findAuditDiff(t, host, "b.go") == 0 {
		t.Fatal("truncated b diff did not open")
	}
	if got := strings.Join(h.NoticeTexts(), "|"); !strings.Contains(got, "diff for b.go was truncated") {
		t.Fatalf("truncation notice = %q", got)
	}
}

func TestAuditModeCrossFileReadFailureNotBoundary(t *testing.T) {
	t.Parallel()
	host, os := auditHost(t)
	os.SetFile("/w/b.go", "package b\n")
	os.Rescript(gitArgv("--no-optional-locks", "status", "--porcelain", "-z", "-uall"), plugintest.ProcScript{Stdout: " M a.go\x00 M b.go\x00"})
	os.Rescript(gitArgv("diff", "--no-ext-diff", "--no-textconv", "--no-color", "--numstat", "--"), plugintest.ProcScript{Stdout: "1\t0\ta.go\n1\t0\tb.go\n"})
	os.Rescript(gitArgv("diff", "--no-ext-diff", "--no-textconv", "--no-color", "--cached", "--numstat", "--"), plugintest.ProcScript{Stdout: ""})
	os.Script(gitArgv("diff", "--no-ext-diff", "--no-textconv", "--no-color", "--unified=3", "--", "b.go"), plugintest.ProcScript{Err: pluginapi.Errf(pluginapi.CodeExec, "cannot read b.go")})
	h := startAudit(t, host, os)
	openAuditDiff(t, h, host)
	if err := h.Run(auditPlugin+".next_hunk", nil); err != nil {
		t.Fatal(err)
	}
	if err := h.Run(auditPlugin+".next_hunk", nil); err != nil {
		t.Fatal(err)
	}
	if findAuditDiff(t, host, "a.go") == 0 {
		t.Fatal("a diff closed on failed cross load")
	}
	got := strings.Join(h.NoticeTexts(), "|")
	if !strings.Contains(got, "could not read a candidate diff") || strings.Contains(got, "last hunk") {
		t.Fatalf("failure notices = %q", got)
	}
}

func TestAuditModeCrossFileLimitNotBoundary(t *testing.T) {
	t.Parallel()
	host, os := auditHost(t)
	var b strings.Builder
	b.WriteString(" M a.go\x00")
	for i := 1; i <= 201; i++ {
		fmt.Fprintf(&b, " M z%03d.go\x00", i)
	}
	os.Rescript(gitArgv("--no-optional-locks", "status", "--porcelain", "-z", "-uall"), plugintest.ProcScript{Stdout: b.String()})
	os.Rescript(gitArgv("diff", "--no-ext-diff", "--no-textconv", "--no-color", "--numstat", "--"), plugintest.ProcScript{Stdout: "1\t0\ta.go\n"})
	os.Rescript(gitArgv("diff", "--no-ext-diff", "--no-textconv", "--no-color", "--cached", "--numstat", "--"), plugintest.ProcScript{Stdout: ""})
	// A generic empty diff for every candidate after a.go; a.go's specific script wins first.
	os.Script(gitArgv("diff", "--no-ext-diff", "--no-textconv", "--no-color", "--unified=3", "--"), plugintest.ProcScript{Stdout: ""})
	h := startAudit(t, host, os)
	openAuditDiff(t, h, host)
	if err := h.Run(auditPlugin+".next_hunk", nil); err != nil {
		t.Fatal(err)
	}
	if err := h.Run(auditPlugin+".next_hunk", nil); err != nil {
		t.Fatal(err)
	}
	got := strings.Join(h.NoticeTexts(), "|")
	if !strings.Contains(got, "stopped after 200 files") || strings.Contains(got, "last hunk") {
		t.Fatalf("limit notices = %q", got)
	}
}

func TestAuditModePickerEscInvalidatesCrossFileLoad(t *testing.T) {
	t.Parallel()
	host, plain := auditHost(t)
	plain.SetFile("/w/b.go", "package b\n")
	plain.Rescript(gitArgv("--no-optional-locks", "status", "--porcelain", "-z", "-uall"), plugintest.ProcScript{Stdout: " M a.go\x00 M b.go\x00"})
	plain.Rescript(gitArgv("diff", "--no-ext-diff", "--no-textconv", "--no-color", "--numstat", "--"), plugintest.ProcScript{Stdout: "1\t0\ta.go\n1\t0\tb.go\n"})
	plain.Rescript(gitArgv("diff", "--no-ext-diff", "--no-textconv", "--no-color", "--cached", "--numstat", "--"), plugintest.ProcScript{Stdout: ""})
	plain.Script(gitArgv("diff", "--no-ext-diff", "--no-textconv", "--no-color", "--unified=3", "--", "b.go"), plugintest.ProcScript{Stdout: "diff --git a/b.go b/b.go\n--- a/b.go\n+++ b/b.go\n@@ -1 +1 @@\n-old\n+new\n"})
	slow := &slowDiffOS{FakeOS: plain, started: make(chan struct{}), release: make(chan struct{}), match: "-- b.go"}
	h := plugintest.New(t, auditFiles(t), plugintest.Options{Host: host, OS: slow, Timeout: 5 * time.Second, EventBudget: 5 * time.Second})
	h.Start()
	openAuditDiff(t, h, host)
	if err := h.Run(auditPlugin+".next_hunk", nil); err != nil {
		t.Fatal(err)
	}
	if err := h.RT.Run(auditPlugin+".next_hunk", nil, pluginapi.SrcAPI); err != nil {
		t.Fatal(err)
	}
	work := runnableWork(h)
	if work == nil {
		t.Fatal("no cross-file Work")
	}
	done := make(chan pluginapi.Completion, 1)
	go func() { done <- work.Run(work.Ctx) }()
	<-slow.started
	// Esc on the review picker must invalidate the in-flight cross-file target.
	if err := h.Run(auditPlugin+".review", nil); err != nil {
		t.Fatal(err)
	}
	picker := h.Host.UI().Last("picker")
	if picker == nil {
		t.Fatal("no review picker")
	}
	h.RT.Resolve(pluginapi.Completion{Token: picker.Token})
	drainRuntime(h)
	close(slow.release)
	h.RT.Resolve(<-done)
	drainRuntime(h)
	if b := findAuditDiffMaybe(host, "b.go"); b != 0 {
		t.Fatalf("Esc on picker allowed stale cross-file diff %d", b)
	}
}

func findAuditDiffMaybe(host *plugintest.FakeHost, name string) pluginapi.BufferID {
	for _, b := range host.Buffers {
		if b.Info.Virtual && strings.Contains(b.Info.Name, "Review Diff: "+name) {
			return b.Info.ID
		}
	}
	return 0
}

// scriptedDiffOS blocks the first b.go diff and returns a different b diff on the second,
// so two overlapping navigations can be told apart by which result won.
type scriptedDiffOS struct {
	*plugintest.FakeOS
	mu      sync.Mutex
	calls   int
	started chan struct{}
	release chan struct{}
	first   string
	second  string
}

func (s *scriptedDiffOS) Run(ctx context.Context, argv []string, o hostos.ProcOptions) (hostos.ProcResult, error) {
	if strings.Contains(strings.Join(argv, " "), "-- b.go") {
		s.mu.Lock()
		s.calls++
		call := s.calls
		s.mu.Unlock()
		if call == 1 {
			close(s.started)
			select {
			case <-s.release:
			case <-ctx.Done():
				return hostos.ProcResult{}, ctx.Err()
			}
			return hostos.ProcResult{Stdout: []byte(s.first)}, nil
		}
		return hostos.ProcResult{Stdout: []byte(s.second)}, nil
	}
	return s.FakeOS.Run(ctx, argv, o)
}

func TestAuditModeOverlappingNextOnlyAppliesLatest(t *testing.T) {
	t.Parallel()
	host, plain := auditHost(t)
	plain.SetFile("/w/b.go", "package b\n")
	plain.Rescript(gitArgv("--no-optional-locks", "status", "--porcelain", "-z", "-uall"), plugintest.ProcScript{Stdout: " M a.go\x00 M b.go\x00"})
	plain.Rescript(gitArgv("diff", "--no-ext-diff", "--no-textconv", "--no-color", "--numstat", "--"), plugintest.ProcScript{Stdout: "1\t0\ta.go\n1\t0\tb.go\n"})
	plain.Rescript(gitArgv("diff", "--no-ext-diff", "--no-textconv", "--no-color", "--cached", "--numstat", "--"), plugintest.ProcScript{Stdout: ""})
	os := &scriptedDiffOS{
		FakeOS: plain, started: make(chan struct{}), release: make(chan struct{}),
		first:  "diff --git a/b.go b/b.go\n--- a/b.go\n+++ b/b.go\n@@ -1 +1 @@\n-old\n+stale\n",
		second: "diff --git a/b.go b/b.go\n--- a/b.go\n+++ b/b.go\n@@ -1 +1 @@\n-old\n+fresh\n",
	}
	h := plugintest.New(t, auditFiles(t), plugintest.Options{Host: host, OS: os, Timeout: 5 * time.Second, EventBudget: 5 * time.Second})
	h.Start()
	openAuditDiff(t, h, host)
	if err := h.Run(auditPlugin+".next_hunk", nil); err != nil {
		t.Fatal(err)
	}

	// First next starts loading b and blocks. A second next starts while it is in flight and
	// completes with the fresh result first.
	if err := h.RT.Run(auditPlugin+".next_hunk", nil, pluginapi.SrcAPI); err != nil {
		t.Fatal(err)
	}
	firstWork := runnableWork(h)
	if firstWork == nil {
		t.Fatal("no first cross-file Work")
	}
	firstDone := make(chan pluginapi.Completion, 1)
	go func() { firstDone <- firstWork.Run(firstWork.Ctx) }()
	<-os.started
	if err := h.RT.Run(auditPlugin+".next_hunk", nil, pluginapi.SrcAPI); err != nil {
		t.Fatal(err)
	}
	secondWork := runnableWork(h)
	if secondWork == nil {
		t.Fatal("no second cross-file Work")
	}
	secondCompletion := secondWork.Run(secondWork.Ctx)
	h.RT.Resolve(secondCompletion)
	drainRuntime(h)
	close(os.release)
	h.RT.Resolve(<-firstDone)
	drainRuntime(h)

	bID := findAuditDiff(t, host, "b.go")
	var text string
	for _, b := range host.Buffers {
		if b.Info.ID == bID {
			text = strings.Join(b.Lines, "\n")
		}
	}
	if !strings.Contains(text, "+fresh") || strings.Contains(text, "+stale") {
		t.Fatalf("overlapping next final diff = %q, want fresh only", text)
	}
}

func TestAuditModeRefreshInvalidatesInFlightNavigation(t *testing.T) {
	t.Parallel()
	host, plain := auditHost(t)
	plain.SetFile("/w/b.go", "package b\n")
	plain.Rescript(gitArgv("--no-optional-locks", "status", "--porcelain", "-z", "-uall"), plugintest.ProcScript{Stdout: " M a.go\x00 M b.go\x00"})
	plain.Rescript(gitArgv("diff", "--no-ext-diff", "--no-textconv", "--no-color", "--numstat", "--"), plugintest.ProcScript{Stdout: "1\t0\ta.go\n1\t0\tb.go\n"})
	plain.Rescript(gitArgv("diff", "--no-ext-diff", "--no-textconv", "--no-color", "--cached", "--numstat", "--"), plugintest.ProcScript{Stdout: ""})
	plain.Script(gitArgv("diff", "--no-ext-diff", "--no-textconv", "--no-color", "--unified=3", "--", "b.go"), plugintest.ProcScript{Stdout: "diff --git a/b.go b/b.go\n--- a/b.go\n+++ b/b.go\n@@ -1 +1 @@\n-old\n+new\n"})
	slow := &slowDiffOS{FakeOS: plain, started: make(chan struct{}), release: make(chan struct{}), match: "-- b.go"}
	h := plugintest.New(t, auditFiles(t), plugintest.Options{Host: host, OS: slow, Timeout: 5 * time.Second, EventBudget: 5 * time.Second})
	h.Start()
	openAuditDiff(t, h, host)
	if err := h.Run(auditPlugin+".next_hunk", nil); err != nil {
		t.Fatal(err)
	}
	if err := h.RT.Run(auditPlugin+".next_hunk", nil, pluginapi.SrcAPI); err != nil {
		t.Fatal(err)
	}
	work := runnableWork(h)
	if work == nil {
		t.Fatal("no cross-file Work")
	}
	done := make(chan pluginapi.Completion, 1)
	go func() { done <- work.Run(work.Ctx) }()
	<-slow.started
	// A refresh while the candidate is in flight replaces files/repo and must invalidate it.
	plain.Rescript(gitArgv("--no-optional-locks", "status", "--porcelain", "-z", "-uall"), plugintest.ProcScript{Stdout: " M a.go\x00"})
	if err := h.Run(auditPlugin+".refresh", nil); err != nil {
		t.Fatal(err)
	}
	close(slow.release)
	h.RT.Resolve(<-done)
	drainRuntime(h)
	if b := findAuditDiffMaybe(host, "b.go"); b != 0 {
		t.Fatalf("refresh allowed stale navigation to open b.go diff %d", b)
	}
}

// scriptedStatusOS makes the second status call block (the first refresh) and returns a
// different status on later calls (the newer refresh). It can make the blocked old refresh
// fail, to prove a stale error is ignored too.
type scriptedStatusOS struct {
	*plugintest.FakeOS
	mu         sync.Mutex
	calls      int
	started    chan struct{}
	startOnce  sync.Once
	release    chan struct{}
	first      []byte
	old        []byte
	fresh      []byte
	oldFailure bool
}

func (s *scriptedStatusOS) Run(ctx context.Context, argv []string, o hostos.ProcOptions) (hostos.ProcResult, error) {
	joined := strings.Join(argv, " ")
	if strings.Contains(joined, "status") && strings.Contains(joined, "--porcelain") {
		s.mu.Lock()
		s.calls++
		call := s.calls
		s.mu.Unlock()
		switch call {
		case 1:
			return hostos.ProcResult{Stdout: append([]byte(nil), s.first...)}, nil
		case 2:
			s.startOnce.Do(func() { close(s.started) })
			select {
			case <-s.release:
			case <-ctx.Done():
				return hostos.ProcResult{}, ctx.Err()
			}
			if s.oldFailure {
				return hostos.ProcResult{Code: 1, Stderr: []byte("old refresh failed")}, nil
			}
			return hostos.ProcResult{Stdout: append([]byte(nil), s.old...)}, nil
		default:
			return hostos.ProcResult{Stdout: append([]byte(nil), s.fresh...)}, nil
		}
	}
	return s.FakeOS.Run(ctx, argv, o)
}

func TestAuditModeOverlappingRefreshOnlyNewestApplies(t *testing.T) {
	t.Parallel()
	for _, oldFailure := range []bool{false, true} {
		t.Run(fmt.Sprintf("old_failure_%v", oldFailure), func(t *testing.T) {
			host, plain := auditHost(t)
			os := &scriptedStatusOS{
				FakeOS: plain, started: make(chan struct{}), release: make(chan struct{}),
				first:      []byte(" M a.go\x00"),
				old:        []byte(" M old.go\x00"),
				fresh:      []byte(" M a.go\x00 M b.go\x00"),
				oldFailure: oldFailure,
			}
			h := plugintest.New(t, auditFiles(t), plugintest.Options{Host: host, OS: os, Timeout: 5 * time.Second, EventBudget: 5 * time.Second})
			h.Start()
			if err := h.Run(auditPlugin+".review", nil); err != nil {
				t.Fatal(err)
			}
			h.Advance(0)
			picker := h.Host.UI().Last("picker")
			if picker == nil {
				t.Fatal("no initial picker")
			}

			// First refresh: run its repo-resolution Work and let the status Work block.
			if err := h.RT.Run(auditPlugin+".refresh", nil, pluginapi.SrcAPI); err != nil {
				t.Fatal(err)
			}
			var firstDone chan pluginapi.Completion
			for i := 0; i < 10; i++ {
				w := runnableWork(h)
				if w == nil {
					t.Fatal("no first refresh Work")
				}
				done := make(chan pluginapi.Completion, 1)
				go func(w *pluginapi.Work) { done <- w.Run(w.Ctx) }(w)
				select {
				case <-os.started:
					firstDone = done
				case c := <-done:
					h.RT.Resolve(c)
				case <-time.After(2 * time.Second):
					t.Fatal("first refresh did not finish/block")
				}
				if firstDone != nil {
					break
				}
			}
			if firstDone == nil {
				t.Fatal("first refresh never reached status")
			}

			// Second refresh returns the fresh status first.
			if err := h.RT.Run(auditPlugin+".refresh", nil, pluginapi.SrcAPI); err != nil {
				t.Fatal(err)
			}
			for i := 0; i < 10; i++ {
				w := runnableWork(h)
				if w == nil {
					break
				}
				h.RT.Resolve(w.Run(w.Ctx))
			}
			drainRuntime(h)
			if !hasPickerRow(t, picker, "b.go") {
				t.Fatalf("fresh refresh did not apply: %+v", picker.Picker.Items)
			}

			// Release the old refresh. Whether it succeeds or fails, it must not overwrite the
			// fresh picker or add an error notice.
			close(os.release)
			h.RT.Resolve(<-firstDone)
			drainRuntime(h)
			if !hasPickerRow(t, picker, "b.go") || hasPickerRow(t, picker, "old.go") {
				t.Fatalf("stale refresh changed rows: %+v", picker.Picker.Items)
			}
			if oldFailure {
				if got := strings.Join(h.NoticeTexts(), "|"); strings.Contains(got, "old refresh failed") {
					t.Fatalf("stale refresh error notice = %q", got)
				}
			}
		})
	}
}

func hasPickerRow(t *testing.T, picker *plugintest.FakeOverlay, substr string) bool {
	t.Helper()
	for _, it := range picker.Picker.Items {
		if strings.Contains(it.Text, substr) {
			return true
		}
	}
	return false
}

func TestAuditModeSaveRefreshesOpenPicker(t *testing.T) {
	t.Parallel()
	host, os := auditHost(t)
	os.SetFile("/w/b.go", "package b\n")
	os.Rescript(gitArgv("--no-optional-locks", "status", "--porcelain", "-z", "-uall"), plugintest.ProcScript{Stdout: " M a.go\x00"})
	h := startAudit(t, host, os)
	if err := h.Run(auditPlugin+".review", nil); err != nil {
		t.Fatal(err)
	}
	h.Advance(0)
	picker := h.Host.UI().Last("picker")
	if picker == nil {
		t.Fatal("no review picker")
	}
	if hasPickerRow(t, picker, "b.go") {
		t.Fatal("b.go present before save")
	}

	os.Rescript(gitArgv("--no-optional-locks", "status", "--porcelain", "-z", "-uall"), plugintest.ProcScript{Stdout: " M a.go\x00 M b.go\x00"})
	h.Emit(pluginapi.Event{Name: "after_file_save", Data: map[string]any{"path": "/w/a.go"}})
	if !hasPickerRow(t, picker, "b.go") {
		t.Fatalf("save did not refresh picker: %+v", picker.Picker.Items)
	}

	os.Rescript(gitArgv("--no-optional-locks", "status", "--porcelain", "-z", "-uall"), plugintest.ProcScript{Stdout: " M a.go\x00"})
	h.Emit(pluginapi.Event{Name: "after_file_revert", Data: map[string]any{"path": "/w/a.go"}})
	if hasPickerRow(t, picker, "b.go") {
		t.Fatalf("revert did not refresh picker: %+v", picker.Picker.Items)
	}
}

func TestAuditModeSaveReloadsOpenDiffInPlace(t *testing.T) {
	t.Parallel()
	host, os := auditHost(t)
	os.Rescript(gitArgv("--no-optional-locks", "status", "--porcelain", "-z", "-uall"), plugintest.ProcScript{Stdout: " M a.go\x00"})
	h := startAudit(t, host, os)
	diffID := openAuditDiff(t, h, host)
	if err := h.Run(auditPlugin+".next_hunk", nil); err != nil {
		t.Fatal(err)
	}
	if got := auditDiffLine(t, host, diffID); got != 3 {
		t.Fatalf("pre-save cursor = %d, want 3", got)
	}

	os.Rescript(gitArgv("diff", "--no-ext-diff", "--no-textconv", "--no-color", "--unified=3", "--", "a.go"), plugintest.ProcScript{Stdout: "diff --git a/a.go b/a.go\n--- a/a.go\n+++ b/a.go\n@@ -1 +1 @@\n-old\n+newer\n@@ -10 +10 @@\n-old2\n+newer2\n"})
	h.Emit(pluginapi.Event{Name: "after_file_save", Data: map[string]any{"path": "/w/a.go"}})
	if got := host.ActiveBuffer(); got != diffID {
		t.Fatalf("save refresh moved focus to %d, want diff %d", got, diffID)
	}
	if got := auditDiffLine(t, host, diffID); got != 3 {
		t.Fatalf("post-save cursor = %d, want hunk row 3", got)
	}
	var text string
	for _, b := range host.Buffers {
		if b.Info.ID == diffID {
			text = strings.Join(b.Lines, "\n")
		}
	}
	if !strings.Contains(text, "+newer2") {
		t.Fatalf("diff was not reloaded in place: %q", text)
	}
	if targets := host.LineTargets(diffID); len(targets) == 0 {
		t.Fatal("reloaded diff lost line targets")
	}
}

// blockingReloadOS blocks only the second a.go diff (the save-triggered reload), leaving
// the initial picker-open diff unblocked.
type blockingReloadOS struct {
	*plugintest.FakeOS
	mu      sync.Mutex
	calls   int
	started chan struct{}
	release chan struct{}
}

func (s *blockingReloadOS) Run(ctx context.Context, argv []string, o hostos.ProcOptions) (hostos.ProcResult, error) {
	joined := strings.Join(argv, " ")
	if strings.Contains(joined, "-- a.go") && strings.Contains(joined, "--unified=3") {
		s.mu.Lock()
		s.calls++
		call := s.calls
		s.mu.Unlock()
		if call == 2 {
			close(s.started)
			select {
			case <-s.release:
			case <-ctx.Done():
				return hostos.ProcResult{}, ctx.Err()
			}
		}
	}
	return s.FakeOS.Run(ctx, argv, o)
}

func TestAuditModeSaveReloadDiscardedAfterClose(t *testing.T) {
	t.Parallel()
	host, plain := auditHost(t)
	plain.Rescript(gitArgv("--no-optional-locks", "status", "--porcelain", "-z", "-uall"), plugintest.ProcScript{Stdout: " M a.go\x00"})
	slow := &blockingReloadOS{FakeOS: plain, started: make(chan struct{}), release: make(chan struct{})}
	h := plugintest.New(t, auditFiles(t), plugintest.Options{Host: host, OS: slow, Timeout: 5 * time.Second, EventBudget: 5 * time.Second})
	h.Start()
	openAuditDiff(t, h, host)
	if err := h.Run(auditPlugin+".next_hunk", nil); err != nil {
		t.Fatal(err)
	}

	h.RT.Emit(pluginapi.Event{Name: "after_file_save", Data: map[string]any{"path": "/w/a.go"}})
	work := runnableWork(h)
	if work == nil {
		t.Fatal("no save-triggered reload Work")
	}
	done := make(chan pluginapi.Completion, 1)
	go func() { done <- work.Run(work.Ctx) }()
	<-slow.started
	if err := h.RT.Run(auditPlugin+".close", nil, pluginapi.SrcAPI); err != nil {
		t.Fatal(err)
	}
	drainRuntime(h)
	close(slow.release)
	h.RT.Resolve(<-done)
	drainRuntime(h)
	if b := findAuditDiffMaybe(host, "a.go"); b != 0 {
		t.Fatalf("late save reload recreated closed diff %d", b)
	}
}

// scriptedReloadOS blocks the second a.go diff and returns firstDiff; later reload calls
// return secondDiff, so two overlapping save refreshes can be told apart.
type scriptedReloadOS struct {
	*plugintest.FakeOS
	mu      sync.Mutex
	calls   int
	started chan struct{}
	release chan struct{}
	first   string
	second  string
}

func (s *scriptedReloadOS) Run(ctx context.Context, argv []string, o hostos.ProcOptions) (hostos.ProcResult, error) {
	joined := strings.Join(argv, " ")
	if strings.Contains(joined, "-- a.go") && strings.Contains(joined, "--unified=3") {
		s.mu.Lock()
		s.calls++
		call := s.calls
		s.mu.Unlock()
		switch call {
		case 1:
			break
		case 2:
			close(s.started)
			select {
			case <-s.release:
			case <-ctx.Done():
				return hostos.ProcResult{}, ctx.Err()
			}
			return hostos.ProcResult{Stdout: []byte(s.first)}, nil
		default:
			return hostos.ProcResult{Stdout: []byte(s.second)}, nil
		}
	}
	return s.FakeOS.Run(ctx, argv, o)
}

func TestAuditModeOverlappingSaveReloadOnlyNewestApplies(t *testing.T) {
	t.Parallel()
	host, plain := auditHost(t)
	plain.Rescript(gitArgv("--no-optional-locks", "status", "--porcelain", "-z", "-uall"), plugintest.ProcScript{Stdout: " M a.go\x00"})
	os := &scriptedReloadOS{
		FakeOS: plain, started: make(chan struct{}), release: make(chan struct{}),
		first:  "diff --git a/a.go b/a.go\n--- a/a.go\n+++ b/a.go\n@@ -1 +1 @@\n-old\n+stale\n",
		second: "diff --git a/a.go b/a.go\n--- a/a.go\n+++ b/a.go\n@@ -1 +1 @@\n-old\n+fresh\n",
	}
	h := plugintest.New(t, auditFiles(t), plugintest.Options{Host: host, OS: os, Timeout: 5 * time.Second, EventBudget: 5 * time.Second})
	h.Start()
	diffID := openAuditDiff(t, h, host)
	if err := h.Run(auditPlugin+".next_hunk", nil); err != nil {
		t.Fatal(err)
	}

	h.RT.Emit(pluginapi.Event{Name: "after_file_save", Data: map[string]any{"path": "/w/a.go"}})
	firstWork := runnableWork(h)
	if firstWork == nil {
		t.Fatal("no first save reload Work")
	}
	firstDone := make(chan pluginapi.Completion, 1)
	go func() { firstDone <- firstWork.Run(firstWork.Ctx) }()
	<-os.started

	h.RT.Emit(pluginapi.Event{Name: "after_file_save", Data: map[string]any{"path": "/w/a.go"}})
	secondWork := runnableWork(h)
	if secondWork == nil {
		t.Fatal("no second save reload Work")
	}
	h.RT.Resolve(secondWork.Run(secondWork.Ctx))
	drainRuntime(h)
	close(os.release)
	h.RT.Resolve(<-firstDone)
	drainRuntime(h)

	var text string
	for _, b := range host.Buffers {
		if b.Info.ID == diffID {
			text = strings.Join(b.Lines, "\n")
		}
	}
	if !strings.Contains(text, "+fresh") || strings.Contains(text, "+stale") {
		t.Fatalf("overlapping save reload final diff = %q, want fresh only", text)
	}
}

func TestAuditModeSaveRefreshesPickerAndOpenDiff(t *testing.T) {
	t.Parallel()
	host, os := auditHost(t)
	os.SetFile("/w/b.go", "package b\n")
	os.Rescript(gitArgv("--no-optional-locks", "status", "--porcelain", "-z", "-uall"), plugintest.ProcScript{Stdout: " M a.go\x00"})
	h := startAudit(t, host, os)
	diffID := openAuditDiff(t, h, host)
	if err := h.Run(auditPlugin+".next_hunk", nil); err != nil {
		t.Fatal(err)
	}
	// Reopen the review picker while the diff stays open.
	if err := h.Run(auditPlugin+".review", nil); err != nil {
		t.Fatal(err)
	}
	h.Advance(0)
	picker := h.Host.UI().Last("picker")
	if picker == nil || !hasPickerRow(t, picker, "a.go") {
		t.Fatalf("picker not open over diff: %+v", picker)
	}

	os.Rescript(gitArgv("--no-optional-locks", "status", "--porcelain", "-z", "-uall"), plugintest.ProcScript{Stdout: " M a.go\x00 M b.go\x00"})
	os.Rescript(gitArgv("diff", "--no-ext-diff", "--no-textconv", "--no-color", "--unified=3", "--", "a.go"), plugintest.ProcScript{Stdout: "diff --git a/a.go b/a.go\n--- a/a.go\n+++ b/a.go\n@@ -1 +1 @@\n-old\n+saved\n"})
	h.Emit(pluginapi.Event{Name: "after_file_save", Data: map[string]any{"path": "/w/a.go"}})
	if !hasPickerRow(t, picker, "b.go") {
		t.Fatalf("save did not refresh picker over diff: %+v", picker.Picker.Items)
	}
	if got := auditDiffLine(t, host, diffID); got != 3 {
		t.Fatalf("save reload cursor = %d, want 3", got)
	}
	var text string
	for _, b := range host.Buffers {
		if b.Info.ID == diffID {
			text = strings.Join(b.Lines, "\n")
		}
	}
	if !strings.Contains(text, "+saved") {
		t.Fatalf("open diff stayed stale while picker refreshed: %q", text)
	}

	// Revert clears both the new list row and the diff content.
	os.Rescript(gitArgv("--no-optional-locks", "status", "--porcelain", "-z", "-uall"), plugintest.ProcScript{Stdout: " M a.go\x00"})
	os.Rescript(gitArgv("diff", "--no-ext-diff", "--no-textconv", "--no-color", "--unified=3", "--", "a.go"), plugintest.ProcScript{Stdout: "diff --git a/a.go b/a.go\n--- a/a.go\n+++ b/a.go\n@@ -1 +1 @@\n-old\n+reverted\n"})
	h.Emit(pluginapi.Event{Name: "after_file_revert", Data: map[string]any{"path": "/w/a.go"}})
	if hasPickerRow(t, picker, "b.go") {
		t.Fatalf("revert did not refresh picker over diff: %+v", picker.Picker.Items)
	}
	text = ""
	for _, b := range host.Buffers {
		if b.Info.ID == diffID {
			text = strings.Join(b.Lines, "\n")
		}
	}
	if !strings.Contains(text, "+reverted") {
		t.Fatalf("revert did not reload open diff: %q", text)
	}
}

func TestAuditModeSaveReloadSurvivesUnrelatedRefresh(t *testing.T) {
	t.Parallel()
	host, plain := auditHost(t)
	plain.Rescript(gitArgv("--no-optional-locks", "status", "--porcelain", "-z", "-uall"), plugintest.ProcScript{Stdout: " M a.go\x00"})
	slow := &blockingReloadOS{FakeOS: plain, started: make(chan struct{}), release: make(chan struct{})}
	h := plugintest.New(t, auditFiles(t), plugintest.Options{Host: host, OS: slow, Timeout: 5 * time.Second, EventBudget: 5 * time.Second})
	h.Start()
	diffID := openAuditDiff(t, h, host)
	if err := h.Run(auditPlugin+".next_hunk", nil); err != nil {
		t.Fatal(err)
	}
	if err := h.Run(auditPlugin+".review", nil); err != nil {
		t.Fatal(err)
	}
	h.Advance(0)
	picker := h.Host.UI().Last("picker")
	if picker == nil {
		t.Fatal("no picker over diff")
	}

	plain.Rescript(gitArgv("--no-optional-locks", "status", "--porcelain", "-z", "-uall"), plugintest.ProcScript{Stdout: " M a.go\x00 M b.go\x00"})
	plain.Rescript(gitArgv("diff", "--no-ext-diff", "--no-textconv", "--no-color", "--unified=3", "--", "a.go"), plugintest.ProcScript{Stdout: "diff --git a/a.go b/a.go\n--- a/a.go\n+++ b/a.go\n@@ -1 +1 @@\n-old\n+saved\n"})

	// a's save starts a blocked reload and a list refresh.
	h.RT.Emit(pluginapi.Event{Name: "after_file_save", Data: map[string]any{"path": "/w/a.go"}})
	pending := drainRunnableWorks(t, h)
	var aDone chan pluginapi.Completion
	for aDone == nil {
		if len(pending) == 0 {
			pending = drainRunnableWorks(t, h)
		}
		w := pending[0]
		pending = pending[1:]
		done := make(chan pluginapi.Completion, 1)
		go func(w *pluginapi.Work) { done <- w.Run(w.Ctx) }(w)
		select {
		case <-slow.started:
			aDone = done
		case c := <-done:
			h.RT.Resolve(c)
		case <-time.After(2 * time.Second):
			t.Fatal("a reload did not block")
		}
	}

	// b's save makes a new refresh win, while a's independent reload is still blocked.
	h.RT.Emit(pluginapi.Event{Name: "after_file_save", Data: map[string]any{"path": "/w/b.go"}})
	for len(pending) > 0 {
		w := pending[0]
		pending = pending[1:]
		h.RT.Resolve(w.Run(w.Ctx))
	}
	runAllWork(h)
	if !hasPickerRow(t, picker, "b.go") {
		t.Fatalf("winning b refresh did not apply: %+v", picker.Picker.Items)
	}

	close(slow.release)
	h.RT.Resolve(<-aDone)
	drainRuntime(h)
	var text string
	for _, b := range host.Buffers {
		if b.Info.ID == diffID {
			text = strings.Join(b.Lines, "\n")
		}
	}
	if !strings.Contains(text, "+saved") {
		t.Fatalf("a reload was lost when b refresh won: %q", text)
	}
}

// blockingSaveOS blocks the third status call (the save-event list refresh) and the second
// a.go diff call (the save-event reload), leaving initial review setup unblocked.
type blockingSaveOS struct {
	*plugintest.FakeOS
	mu            sync.Mutex
	statusCalls   int
	diffCalls     int
	statusStarted chan struct{}
	diffStarted   chan struct{}
	statusRelease chan struct{}
	diffRelease   chan struct{}
}

func (s *blockingSaveOS) Run(ctx context.Context, argv []string, o hostos.ProcOptions) (hostos.ProcResult, error) {
	joined := strings.Join(argv, " ")
	if strings.Contains(joined, "status") && strings.Contains(joined, "--porcelain") {
		s.mu.Lock()
		s.statusCalls++
		call := s.statusCalls
		s.mu.Unlock()
		if call == 3 {
			close(s.statusStarted)
			select {
			case <-s.statusRelease:
			case <-ctx.Done():
				return hostos.ProcResult{}, ctx.Err()
			}
		}
	}
	if strings.Contains(joined, "-- a.go") && strings.Contains(joined, "--unified=3") {
		s.mu.Lock()
		s.diffCalls++
		call := s.diffCalls
		s.mu.Unlock()
		if call == 2 {
			close(s.diffStarted)
			select {
			case <-s.diffRelease:
			case <-ctx.Done():
				return hostos.ProcResult{}, ctx.Err()
			}
		}
	}
	return s.FakeOS.Run(ctx, argv, o)
}

func TestAuditModeSaveReloadSurvivesPickerCloseDuringRefresh(t *testing.T) {
	t.Parallel()
	host, plain := auditHost(t)
	plain.Rescript(gitArgv("--no-optional-locks", "status", "--porcelain", "-z", "-uall"), plugintest.ProcScript{Stdout: " M a.go\x00"})
	os := &blockingSaveOS{
		FakeOS: plain, statusStarted: make(chan struct{}), diffStarted: make(chan struct{}),
		statusRelease: make(chan struct{}), diffRelease: make(chan struct{}),
	}
	h := plugintest.New(t, auditFiles(t), plugintest.Options{Host: host, OS: os, Timeout: 5 * time.Second, EventBudget: 5 * time.Second})
	h.Start()
	diffID := openAuditDiff(t, h, host)
	if err := h.Run(auditPlugin+".next_hunk", nil); err != nil {
		t.Fatal(err)
	}
	if err := h.Run(auditPlugin+".review", nil); err != nil {
		t.Fatal(err)
	}
	h.Advance(0)
	picker := h.Host.UI().Last("picker")
	if picker == nil {
		t.Fatal("no picker over diff")
	}

	plain.Rescript(gitArgv("diff", "--no-ext-diff", "--no-textconv", "--no-color", "--unified=3", "--", "a.go"), plugintest.ProcScript{Stdout: "diff --git a/a.go b/a.go\n--- a/a.go\n+++ b/a.go\n@@ -1 +1 @@\n-old\n+saved\n"})
	h.RT.Emit(pluginapi.Event{Name: "after_file_save", Data: map[string]any{"path": "/w/a.go"}})

	pending := drainRunnableWorks(t, h)
	var diffDone, statusDone chan pluginapi.Completion
	for diffDone == nil || statusDone == nil {
		if len(pending) == 0 {
			pending = drainRunnableWorks(t, h)
		}
		w := pending[0]
		pending = pending[1:]
		done := make(chan pluginapi.Completion, 1)
		go func(w *pluginapi.Work) { done <- w.Run(w.Ctx) }(w)
		var diffCh, statusCh <-chan struct{}
		if diffDone == nil {
			diffCh = os.diffStarted
		}
		if statusDone == nil {
			statusCh = os.statusStarted
		}
		select {
		case <-diffCh:
			diffDone = done
		case <-statusCh:
			statusDone = done
		case c := <-done:
			h.RT.Resolve(c)
		case <-time.After(2 * time.Second):
			t.Fatal("save reload and refresh did not both block")
		}
	}

	// Esc closes the picker while its refresh is still blocked; the independent reload must
	// still complete and update the open diff.
	h.RT.Resolve(pluginapi.Completion{Token: picker.Token})
	drainRuntime(h)
	close(os.statusRelease)
	close(os.diffRelease)
	h.RT.Resolve(<-statusDone)
	h.RT.Resolve(<-diffDone)
	drainRuntime(h)

	var text string
	for _, b := range host.Buffers {
		if b.Info.ID == diffID {
			text = strings.Join(b.Lines, "\n")
		}
	}
	if !strings.Contains(text, "+saved") {
		t.Fatalf("open diff stayed stale after picker closed during refresh: %q", text)
	}
}

func drainRunnableWorks(t *testing.T, h *plugintest.Harness) []*pluginapi.Work {
	t.Helper()
	for i := 0; i < 100; i++ {
		work, _ := h.RT.Drain(50 * time.Millisecond)
		var out []*pluginapi.Work
		for j := range work {
			if work[j].Run != nil {
				out = append(out, &work[j])
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	t.Fatal("no runnable Work after pumping turns")
	return nil
}

func runAllWork(h *plugintest.Harness) {
	idle := 0
	for i := 0; i < 100; i++ {
		work, more := h.RT.Drain(50 * time.Millisecond)
		ran := false
		for j := range work {
			if work[j].Run != nil {
				h.RT.Resolve(work[j].Run(work[j].Ctx))
				ran = true
			}
		}
		if ran {
			idle = 0
			continue
		}
		idle++
		if !more && idle >= 5 {
			return
		}
	}
}

func TestAuditModeSaveReloadSetContentErrorVisible(t *testing.T) {
	t.Parallel()
	host, os := auditHost(t)
	os.Rescript(gitArgv("--no-optional-locks", "status", "--porcelain", "-z", "-uall"), plugintest.ProcScript{Stdout: " M a.go\x00"})
	h := startAudit(t, host, os)
	openAuditDiff(t, h, host)
	if err := h.Run(auditPlugin+".next_hunk", nil); err != nil {
		t.Fatal(err)
	}
	host.SetContentErr = pluginapi.Errf(pluginapi.CodeIO, "setcontent boom")
	os.Rescript(gitArgv("diff", "--no-ext-diff", "--no-textconv", "--no-color", "--unified=3", "--", "a.go"), plugintest.ProcScript{Stdout: "diff --git a/a.go b/a.go\n--- a/a.go\n+++ b/a.go\n@@ -1 +1 @@\n-old\n+newer\n"})
	h.Emit(pluginapi.Event{Name: "after_file_save", Data: map[string]any{"path": "/w/a.go"}})
	got := strings.Join(h.NoticeTexts(), "|")
	if !strings.Contains(got, "could not update the diff for a.go") || !strings.Contains(got, "setcontent boom") {
		t.Fatalf("set_content failure notice = %q", got)
	}
}
