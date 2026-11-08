package agentdiscover

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// historyHome is a synthetic home with the given Claude Code and Codex stores.
type historyHome struct {
	home     string
	codexEnv string // CODEX_HOME, or "" for the default store
	now      time.Time
}

func newHistoryHome(t *testing.T) *historyHome {
	t.Helper()
	return &historyHome{home: t.TempDir(), now: time.Unix(1_700_000_000, 0)}
}

// getenv reads CODEX_HOME from the fixture, nothing else.
func (h *historyHome) getenv(key string) string {
	if key == "CODEX_HOME" {
		return h.codexEnv
	}
	return ""
}

// claudeFile writes one Claude transcript at
// home/.claude/projects/<bucket>/<stem>.jsonl and returns its path.
func (h *historyHome) claudeFile(t *testing.T, bucket, stem string, records ...string) string {
	t.Helper()
	dir := filepath.Join(h.home, ".claude", "projects", bucket)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	return h.writeFile(t, filepath.Join(dir, stem+".jsonl"), records...)
}

// codexFile writes one Codex rollout at
// <codexHome>/sessions/<date>/rollout-<name>.jsonl and returns its path.
func (h *historyHome) codexFile(t *testing.T, date, name string, records ...string) string {
	t.Helper()
	codexHome := h.codexEnv
	if codexHome == "" {
		codexHome = filepath.Join(h.home, ".codex")
	}
	dir := filepath.Join(codexHome, "sessions", filepath.FromSlash(date))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	return h.writeFile(t, filepath.Join(dir, "rollout-"+name+".jsonl"), records...)
}

func (h *historyHome) writeFile(t *testing.T, path string, records ...string) string {
	t.Helper()
	if err := os.WriteFile(path, []byte(strings.Join(records, newline)+newline), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func (h *historyHome) setModTime(t *testing.T, path string) {
	t.Helper()
	h.now = h.now.Add(time.Minute)
	if err := os.Chtimes(path, h.now, h.now); err != nil {
		t.Fatal(err)
	}
}

func scanFixture(t *testing.T, h *historyHome, mut func(*ExternalOptions)) ExternalReport {
	t.Helper()
	opts := ExternalOptions{HomeDir: h.home, Getenv: h.getenv}
	if mut != nil {
		mut(&opts)
	}
	return ScanExternal(context.Background(), opts)
}

func TestScanExternalWithoutStores(t *testing.T) {
	t.Parallel()
	h := newHistoryHome(t)
	report := scanFixture(t, h, nil)
	if report.Installed || len(report.Sessions) != 0 || len(report.Problems) != 0 {
		t.Fatalf("report = %+v, want nothing installed", report)
	}
	if len(report.Tools) != 2 || report.Tools[0].Installed || report.Tools[1].Installed {
		t.Fatalf("tools = %+v, want both absent", report.Tools)
	}
}

func TestScanExternalClaudeSessions(t *testing.T) {
	t.Parallel()
	h := newHistoryHome(t)
	older := h.claudeFile(t, "-tmp-proj", "11111111-aaaa-bbbb-cccc-000000000001",
		`{"type":"queue-operation","sessionId":"11111111-aaaa-bbbb-cccc-000000000001"}`,
		`{"type":"user","cwd":"/home/u/older","gitBranch":"main","sessionId":"11111111-aaaa-bbbb-cccc-000000000001"}`)
	newer := h.claudeFile(t, "-tmp-new", "22222222-aaaa-bbbb-cccc-000000000002",
		`{"type":"user","cwd":"/home/u/newer","gitBranch":"feature/x","sessionId":"22222222-aaaa-bbbb-cccc-000000000002"}`)
	// A sub-agent transcript is deeper than a bucket: never listed.
	subDir := filepath.Join(h.home, ".claude", "projects", "-tmp-new", "22222222-aaaa-bbbb-cccc-000000000002", "subagents")
	if err := os.MkdirAll(subDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(subDir, "child.jsonl"), []byte("{}"+newline), 0o600); err != nil {
		t.Fatal(err)
	}
	h.setModTime(t, older)
	h.setModTime(t, newer)
	report := scanFixture(t, h, nil)
	if !report.Installed || len(report.Sessions) != 2 || len(report.Problems) != 0 {
		t.Fatalf("report = %+v, want two Claude sessions", report)
	}
	// Newest first.
	first := report.Sessions[0]
	if first.Tool != ExternalClaude || first.Agent != "claude" || !first.Exact {
		t.Fatalf("first session = %+v", first)
	}
	if first.ID != "22222222-aaaa-bbbb-cccc-000000000002" || first.CWD != "/home/u/newer" || first.Title != "newer" {
		t.Fatalf("first identity = %+v", first)
	}
	if !slices.Equal(first.ResumeArgv, []string{"claude", "--resume", "22222222-aaaa-bbbb-cccc-000000000002"}) {
		t.Fatalf("resume argv = %v", first.ResumeArgv)
	}
	if first.Path != newer {
		t.Fatalf("path = %q, want %q", first.Path, newer)
	}
	second := report.Sessions[1]
	if second.ID != "11111111-aaaa-bbbb-cccc-000000000001" || second.Title != "older" {
		t.Fatalf("second session = %+v", second)
	}
	if report.Tools[0].ID != ExternalClaude || report.Tools[0].Count != 2 || !report.Tools[0].Installed {
		t.Fatalf("Claude tool = %+v", report.Tools[0])
	}
}

func TestScanExternalClaudeUntrustedIDFallsBackOrSkips(t *testing.T) {
	t.Parallel()
	h := newHistoryHome(t)
	// The recorded id carries shell bytes: it must not reach argv; the safe file
	// stem is the fallback.
	h.claudeFile(t, "bucket", "safe-stem-0001",
		`{"type":"user","cwd":"/w","sessionId":"bad id; rm -rf /"}`)
	// No safe fallback either: the row offers no resumable identity and is skipped.
	h.claudeFile(t, "bucket", "bad id 0002",
		`{"type":"user","cwd":"/w2","sessionId":"bad/id"}`)
	report := scanFixture(t, h, nil)
	if len(report.Sessions) != 1 {
		t.Fatalf("sessions = %+v, want only the safely identified one", report.Sessions)
	}
	row := report.Sessions[0]
	if row.ID != "safe-stem-0001" {
		t.Fatalf("id = %q, want the safe stem fallback", row.ID)
	}
	if !slices.Equal(row.ResumeArgv, []string{"claude", "--resume", "safe-stem-0001"}) {
		t.Fatalf("resume argv = %v", row.ResumeArgv)
	}
	for _, arg := range row.ResumeArgv {
		if strings.ContainsAny(arg, " ;/()") {
			t.Fatalf("unsafe argv token %q", arg)
		}
	}
}

func TestScanExternalClaudeNewestSessionsBound(t *testing.T) {
	t.Parallel()
	h := newHistoryHome(t)
	for i := 0; i < 4; i++ {
		name := "00000000-0000-0000-0000-00000000000" + itoa(i)
		path := h.claudeFile(t, "bucket", name, `{"type":"user","cwd":"/w","sessionId":"`+name+`"}`)
		h.setModTime(t, path)
	}
	report := scanFixture(t, h, func(o *ExternalOptions) { o.MaxSessions = 2 })
	if len(report.Sessions) != 2 || !report.Truncated {
		t.Fatalf("sessions = %d truncated %v, want 2 and truncated", len(report.Sessions), report.Truncated)
	}
	if len(report.Problems) == 0 || !strings.Contains(report.Problems[0], "newest 2 of 4") {
		t.Fatalf("problems = %v, want the newest-of note", report.Problems)
	}
}

func TestScanExternalCodexStore(t *testing.T) {
	t.Parallel()
	h := newHistoryHome(t)
	// The first record is the session_meta envelope: cwd/id are under payload.
	cli := h.codexFile(t, "2026/01/02", "2026-01-02T03-04-05-abcdef01-2345-6789-abcd-ef0123456789",
		`{"type":"session_meta","payload":{"id":"abcdef01-2345-6789-abcd-ef0123456789","cwd":"/home/u/codexproj","originator":"codex_cli_rs"}}`)
	h.setModTime(t, cli)
	// The ChatGPT desktop app shares the store; its conversations are not offered.
	h.codexFile(t, "2026/01/02", "2026-01-02T04-05-06-11111111-2222-3333-4444-555555555555",
		`{"type":"session_meta","payload":{"id":"11111111-2222-3333-4444-555555555555","cwd":"/home/u/desk","originator":"chatgpt_desktop"}}`)
	report := scanFixture(t, h, nil)
	if !report.Installed || len(report.Sessions) != 1 {
		t.Fatalf("report = %+v, want the CLI rollout only", report)
	}
	row := report.Sessions[0]
	if row.Tool != ExternalCodex || row.Agent != "codex" || row.Exact {
		t.Fatalf("codex row = %+v, want an inexact codex row", row)
	}
	if row.ID != "abcdef01-2345-6789-abcd-ef0123456789" || row.CWD != "/home/u/codexproj" || row.Title != "codexproj" {
		t.Fatalf("codex identity = %+v", row)
	}
	if !slices.Equal(row.ResumeArgv, []string{"codex", "resume", "--last"}) {
		t.Fatalf("codex resume = %v", row.ResumeArgv)
	}
	tool := report.Tools[1]
	if tool.ID != ExternalCodex || !tool.Installed || tool.Count != 1 {
		t.Fatalf("Codex tool = %+v", tool)
	}
}

func TestScanExternalCodexHomeEnv(t *testing.T) {
	t.Parallel()
	h := newHistoryHome(t)
	h.codexEnv = t.TempDir()
	path := h.codexFile(t, "2026/02/03", "2026-02-03T00-00-00-99999999-8888-7777-6666-555555555555",
		`{"type":"session_meta","payload":{"id":"99999999-8888-7777-6666-555555555555","cwd":"/w","originator":"codex_cli_rs"}}`)
	h.setModTime(t, path)
	report := scanFixture(t, h, nil)
	if len(report.Sessions) != 1 {
		t.Fatalf("CODEX_HOME sessions = %+v", report.Sessions)
	}
	if got := report.Sessions[0].Path; got != path {
		t.Fatalf("path = %q, want the CODEX_HOME rollout %q", got, path)
	}
}

func TestScanExternalMalformedHeadersAreSkipped(t *testing.T) {
	t.Parallel()
	h := newHistoryHome(t)
	h.claudeFile(t, "bucket", "33333333-aaaa-bbbb-cccc-000000000003",
		"not json",
		`{"type":"user","cwd":"/w","sessionId":"33333333-aaaa-bbbb-cccc-000000000003"}`,
		`{"truncated":`)
	report := scanFixture(t, h, nil)
	if len(report.Sessions) != 1 {
		t.Fatalf("sessions = %+v, want the one readable session", report.Sessions)
	}
	if report.Sessions[0].CWD != "/w" {
		t.Fatalf("cwd = %q", report.Sessions[0].CWD)
	}
}

func TestScanExternalCancelsBetweenFiles(t *testing.T) {
	t.Parallel()
	h := newHistoryHome(t)
	h.claudeFile(t, "bucket", "44444444-aaaa-bbbb-cccc-000000000004",
		`{"type":"user","cwd":"/w","sessionId":"44444444-aaaa-bbbb-cccc-000000000004"}`)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	report := ScanExternal(ctx, ExternalOptions{HomeDir: h.home, Getenv: h.getenv})
	if len(report.Sessions) != 0 {
		t.Fatalf("a cancelled scan read sessions: %+v", report.Sessions)
	}
}

func TestSafeSessionID(t *testing.T) {
	t.Parallel()
	for _, id := range []string{"abc", "A-b_c.d", "0123456789abcdef"} {
		if !safeSessionID(id) {
			t.Fatalf("safeSessionID(%q) = false", id)
		}
	}
	for _, id := range []string{"", "with space", "semi;colon", "slash/child", "dollar$", "$(x)", "line" + newline + "break", strings.Repeat("a", maxExternalIDBytes+1)} {
		if safeSessionID(id) {
			t.Fatalf("safeSessionID(%q) = true", id)
		}
	}
}
