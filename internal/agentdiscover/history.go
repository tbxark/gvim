package agentdiscover

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// External agent-session stores: the local transcripts Claude Code and the Codex
// CLI write for every conversation. Fresh's agent-sessions plugins scan them the
// same way (docs/internal/orchestrator-sessions.md, plugins/agent_sessions_*.ts):
// read a bounded header of the newest transcript files, take the cwd and session
// id from the records, and offer the agent's resume command. gvim's version is
// deliberately read-only and local: it opens no command, runs no shell and never
// uses a transcript's bytes as an executable argv — the only thing a transcript
// can contribute to a command is a session id that had to pass a strict token
// check, placed into the fixed resume template below.
//
// It does not replace the tmux scanner: a transcript says a conversation exists,
// not that it runs now. The two findings are merged by the UI.

// Tool ids and display names of the external stores.
const (
	ExternalClaude = "claude-code"
	ExternalCodex  = "codex-cli"
)

// The fixed resume templates of Fresh's agent registry (lib/agent_claude.ts,
// lib/agent_codex.ts). Claude resumes by id; Codex has no id-addressed resume,
// only "the newest session in this cwd" (resume --last), so its resume is
// inexact and the UI says so.
const (
	claudeResumeCommand = "claude"
	codexResumeCommand  = "codex"
)

// Defaults bound one external scan, mirroring Fresh's plugin bounds.
const (
	defaultExternalMaxSessions    = 50
	defaultClaudeHeaderBytes      = 256 << 10
	defaultCodexHeaderBytes       = 64 << 10
	defaultExternalMaxEntries     = 5001
	defaultExternalMaxProblems    = 20
	defaultExternalMaxRecordLines = 400
	maxExternalIDBytes            = 128
	maxExternalTitleBytes         = 200
)

// ExternalOptions are one external scan's inputs.
type ExternalOptions struct {
	// HomeDir is the user's home; empty means os.UserHomeDir.
	HomeDir string
	// Getenv reads CODEX_HOME; nil means os.Getenv.
	Getenv func(string) string
	// MaxSessions caps the rows kept per tool, newest first, before any header is
	// read. Zero means 50.
	MaxSessions int
	// ClaudeHeaderBytes and CodexHeaderBytes bound one transcript read.
	ClaudeHeaderBytes int
	CodexHeaderBytes  int
	// MaxEntries bounds each store walk. Zero means 5001.
	MaxEntries int
}

// ExternalSession is one discovered transcript: a conversation that exists on
// disk, not necessarily a process. ResumeArgv is built from the fixed registry
// template; Exact is false for Codex, whose resume means "the newest session in
// this directory", not this row.
type ExternalSession struct {
	Tool       string // ExternalClaude or ExternalCodex
	ID         string // the agent's session id (validated; never raw transcript bytes)
	Title      string // the session's directory basename, else the bucket
	CWD        string
	Path       string // the transcript file
	ModTime    time.Time
	Agent      string // the argv0 basename: "claude" or "codex"
	ResumeArgv []string
	Exact      bool
}

// ExternalTool is one store's status for the picker's summary line.
type ExternalTool struct {
	ID        string
	Name      string
	Installed bool
	Count     int
}

// ExternalReport is one external scan's result. It never returns an error: a
// missing store is Installed false, and a read failure is a bounded problem.
type ExternalReport struct {
	Installed bool // at least one store was found
	Sessions  []ExternalSession
	Problems  []string
	Truncated bool
	Tools     []ExternalTool
}

// ScanExternal discovers local Claude Code and Codex CLI transcripts. It reads
// files only: no process is started, no command is run, and nothing outside the
// two stores (plus the bounded file headers) is touched. It runs on the caller's
// goroutine, normally inside a UI effect command.
func ScanExternal(ctx context.Context, opts ExternalOptions) ExternalReport {
	home := strings.TrimSpace(opts.HomeDir)
	if home == "" {
		if h, err := os.UserHomeDir(); err == nil {
			home = h
		}
	}
	getenv := opts.Getenv
	if getenv == nil {
		getenv = os.Getenv
	}
	maxSessions := opts.MaxSessions
	if maxSessions <= 0 {
		maxSessions = defaultExternalMaxSessions
	}
	claudeBytes := opts.ClaudeHeaderBytes
	if claudeBytes <= 0 {
		claudeBytes = defaultClaudeHeaderBytes
	}
	codexBytes := opts.CodexHeaderBytes
	if codexBytes <= 0 {
		codexBytes = defaultCodexHeaderBytes
	}
	maxEntries := opts.MaxEntries
	if maxEntries <= 0 {
		maxEntries = defaultExternalMaxEntries
	}
	report := ExternalReport{Tools: make([]ExternalTool, 0, 2)}
	if home == "" {
		report.Problems = append(report.Problems, "no home directory: local agent sessions cannot be found")
		report.Tools = append(report.Tools,
			ExternalTool{ID: ExternalClaude, Name: "Claude Code"},
			ExternalTool{ID: ExternalCodex, Name: "Codex CLI"})
		return report
	}
	claude := scanClaudeStore(ctx, home, maxSessions, claudeBytes, maxEntries)
	codex := scanCodexStore(ctx, home, getenv, maxSessions, codexBytes, maxEntries)
	report.Tools = append(report.Tools,
		ExternalTool{ID: ExternalClaude, Name: "Claude Code", Installed: claude.installed, Count: len(claude.sessions)},
		ExternalTool{ID: ExternalCodex, Name: "Codex CLI", Installed: codex.installed, Count: len(codex.sessions)})
	report.Installed = claude.installed || codex.installed
	report.Sessions = append(claude.sessions, codex.sessions...)
	// Newest first across both tools, so the picker's first row is the freshest
	// conversation whichever tool wrote it.
	sort.SliceStable(report.Sessions, func(i, j int) bool {
		return report.Sessions[i].ModTime.After(report.Sessions[j].ModTime)
	})
	report.Problems = append(report.Problems, claude.problems...)
	report.Problems = append(report.Problems, codex.problems...)
	report.Truncated = claude.truncated || codex.truncated
	report.Problems = boundProblems(report.Problems)
	return report
}

// storeResult is one store scanner's outcome.
type storeResult struct {
	installed bool
	sessions  []ExternalSession
	problems  []string
	truncated bool
}

// transcriptFile is one candidate transcript found by a store walk.
type transcriptFile struct {
	path    string
	rel     string // slash-separated path under the store root
	modTime time.Time
}

// scanClaudeStore reads ~/.claude/projects/<bucket>/<session>.jsonl. Only depth
// 2 is listed: a deeper transcript is a sub-agent's, shares the parent's id and
// cannot be resumed on its own (Fresh skips them the same way).
func scanClaudeStore(ctx context.Context, home string, maxSessions, headerBytes, maxEntries int) storeResult {
	var out storeResult
	projects := filepath.Join(home, ".claude", "projects")
	buckets, err := os.ReadDir(projects)
	if err != nil {
		if !os.IsNotExist(err) {
			out.problems = append(out.problems, projects+": "+err.Error())
		}
		return out
	}
	out.installed = len(buckets) > 0
	files := make([]transcriptFile, 0, 16)
	entries := 0
walk:
	for _, bucket := range buckets {
		if ctx.Err() != nil {
			break
		}
		if !bucket.IsDir() {
			continue
		}
		bucketPath := filepath.Join(projects, bucket.Name())
		inner, err := os.ReadDir(bucketPath)
		if err != nil {
			out.problems = append(out.problems, bucketPath+": "+err.Error())
			continue
		}
		for _, entry := range inner {
			if ctx.Err() != nil {
				break walk
			}
			entries++
			if entries > maxEntries {
				out.truncated = true
				break walk
			}
			if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".jsonl") {
				continue // sub-agent transcripts live in directories
			}
			info, err := entry.Info()
			if err != nil {
				continue
			}
			files = append(files, transcriptFile{
				path:    filepath.Join(bucketPath, entry.Name()),
				rel:     bucket.Name() + "/" + entry.Name(),
				modTime: info.ModTime(),
			})
		}
	}
	sortNewest(files)
	if len(files) > maxSessions {
		out.problems = append(out.problems, "Claude Code: reporting the newest "+strconv.Itoa(maxSessions)+" of "+strconv.Itoa(len(files))+" transcripts")
		files = files[:maxSessions]
		out.truncated = true
	}
	for _, file := range files {
		if ctx.Err() != nil {
			break
		}
		session, problem := readClaudeTranscript(file, headerBytes)
		if problem != "" {
			out.problems = append(out.problems, problem)
		}
		if session != nil {
			out.sessions = append(out.sessions, *session)
		}
	}
	return out
}

// readClaudeTranscript reads one bounded header and builds a session row.
func readClaudeTranscript(file transcriptFile, headerBytes int) (*ExternalSession, string) {
	text, err := readFilePrefix(file.path, headerBytes)
	if err != nil {
		return nil, file.path + ": " + err.Error()
	}
	records := jsonRecords(text)
	parts := strings.Split(file.rel, "/")
	bucket := parts[0]
	stem := strings.TrimSuffix(filepath.Base(file.rel), ".jsonl")
	cwd := firstStringField(records, "cwd")
	id := firstStringField(records, "sessionId")
	if !safeSessionID(id) {
		id = ""
	}
	if id == "" && safeSessionID(stem) {
		id = stem
	}
	if id == "" {
		return nil, "" // no resumable identity: listing it would offer no action
	}
	resume := []string{claudeResumeCommand, "--resume", id}
	return &ExternalSession{
		Tool: ExternalClaude, ID: id, Title: titleFor(cwd, bucket),
		CWD: clipText(cwd, maxExternalTitleBytes), Path: file.path, ModTime: file.modTime,
		Agent: "claude", ResumeArgv: resume, Exact: true,
	}, ""
}

// scanCodexStore reads $CODEX_HOME/sessions/YYYY/MM/DD/rollout-*.jsonl. The
// ChatGPT desktop app shares the store; its sessions are not offered here (they
// are a different product, and their resume command is not known to gvim), so a
// transcript written by the desktop app is skipped.
func scanCodexStore(ctx context.Context, home string, getenv func(string) string, maxSessions, headerBytes, maxEntries int) storeResult {
	var out storeResult
	codexHome := strings.TrimSpace(getenv("CODEX_HOME"))
	if codexHome == "" {
		codexHome = filepath.Join(home, ".codex")
	}
	sessionsDir := filepath.Join(codexHome, "sessions")
	if info, err := os.Stat(sessionsDir); err != nil || !info.IsDir() {
		if err != nil && !os.IsNotExist(err) {
			out.problems = append(out.problems, sessionsDir+": "+err.Error())
		}
		return out
	}
	out.installed = true
	files := make([]transcriptFile, 0, 16)
	entries := 0
	var walk func(dir, rel string, depth int) bool
	walk = func(dir, rel string, depth int) bool {
		if depth > 4 {
			return false
		}
		items, err := os.ReadDir(dir)
		if err != nil {
			out.problems = append(out.problems, dir+": "+err.Error())
			return false
		}
		// The store is date-sharded (YYYY/MM/DD), so name order is time order: walk
		// newest first, so the entry bound drops the oldest days, not the newest.
		for i := len(items) - 1; i >= 0; i-- {
			entry := items[i]
			if ctx.Err() != nil {
				return true
			}
			entries++
			if entries > maxEntries {
				out.truncated = true
				return true
			}
			name := entry.Name()
			childRel := name
			if rel != "" {
				childRel = rel + "/" + name
			}
			if entry.IsDir() {
				if walk(filepath.Join(dir, name), childRel, depth+1) {
					return true
				}
				continue
			}
			if !strings.HasPrefix(strings.ToLower(name), "rollout-") || !strings.HasSuffix(strings.ToLower(name), ".jsonl") {
				continue
			}
			info, err := entry.Info()
			if err != nil {
				continue
			}
			files = append(files, transcriptFile{
				path: filepath.Join(dir, name), rel: childRel, modTime: info.ModTime(),
			})
		}
		return false
	}
	walk(sessionsDir, "", 1)
	sortNewest(files)
	if len(files) > maxSessions {
		out.problems = append(out.problems, "Codex CLI: reporting the newest "+strconv.Itoa(maxSessions)+" of "+strconv.Itoa(len(files))+" rollouts")
		files = files[:maxSessions]
		out.truncated = true
	}
	for _, file := range files {
		if ctx.Err() != nil {
			break
		}
		session, problem := readCodexTranscript(file, headerBytes)
		if problem != "" {
			out.problems = append(out.problems, problem)
		}
		if session != nil {
			out.sessions = append(out.sessions, *session)
		}
	}
	return out
}

// readCodexTranscript reads one rollout header. It skips ChatGPT desktop-app
// transcripts; the Codex CLI's resume is "the newest session in this cwd", so the
// row says Exact false.
func readCodexTranscript(file transcriptFile, headerBytes int) (*ExternalSession, string) {
	text, err := readFilePrefix(file.path, headerBytes)
	if err != nil {
		return nil, file.path + ": " + err.Error()
	}
	records := jsonRecords(text)
	originator := strings.ToLower(firstStringField(records, "originator"))
	if strings.Contains(originator, "desktop") || strings.Contains(originator, "chatgpt") || strings.Contains(originator, "codex_app") {
		return nil, "" // the desktop app's conversation: not offered by the CLI scanner
	}
	cwd := firstStringField(records, "cwd", "workdir", "working_directory")
	id := firstStringField(records, "id", "session_id")
	if !safeSessionID(id) {
		id = ""
	}
	stem := strings.TrimSuffix(filepath.Base(file.rel), ".jsonl")
	if id == "" {
		// The uuid is the last five dash-separated groups; the timestamp's dashes
		// come first.
		groups := strings.Split(stem, "-")
		if len(groups) >= 5 {
			id = strings.Join(groups[len(groups)-5:], "-")
		} else {
			id = stem
		}
	}
	if !safeSessionID(id) {
		id = clipText(stem, maxExternalIDBytes)
	}
	if id == "" {
		return nil, ""
	}
	return &ExternalSession{
		Tool: ExternalCodex, ID: id, Title: titleFor(cwd, "Codex session"),
		CWD: clipText(cwd, maxExternalTitleBytes), Path: file.path, ModTime: file.modTime,
		Agent: "codex", ResumeArgv: []string{codexResumeCommand, "resume", "--last"}, Exact: false,
	}, ""
}

// sortNewest orders transcripts newest first.
func sortNewest(files []transcriptFile) {
	sort.SliceStable(files, func(i, j int) bool { return files[i].modTime.After(files[j].modTime) })
}

// readFilePrefix reads at most maxBytes of path.
func readFilePrefix(path string, maxBytes int) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, int64(maxBytes)))
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// jsonRecords parses the leading JSON records of a transcript header. Malformed
// and incomplete lines are skipped: a truncated header must not fail the scan.
func jsonRecords(text string) []map[string]any {
	records := make([]map[string]any, 0, 8)
	count := 0
	forEachLine(text, func(line string) {
		if count >= defaultExternalMaxRecordLines {
			return
		}
		line = strings.TrimSpace(line)
		if line == "" || line[0] != '{' {
			return
		}
		count++
		var record map[string]any
		if json.Unmarshal([]byte(line), &record) == nil {
			records = append(records, record)
		}
	})
	return records
}

// firstStringField returns the first non-empty string for one of keys, looking in
// the record and in a Codex session_meta envelope's payload.
func firstStringField(records []map[string]any, keys ...string) string {
	for _, record := range records {
		for _, key := range keys {
			if value, ok := record[key].(string); ok && strings.TrimSpace(value) != "" {
				return value
			}
			if payload, ok := record["payload"].(map[string]any); ok {
				if value, ok := payload[key].(string); ok && strings.TrimSpace(value) != "" {
					return value
				}
			}
		}
	}
	return ""
}

// safeSessionID reports whether a value is a safe command argument: a short
// alphanumeric token with -. _ separators that does not start with "-" (it would
// read as a flag). A transcript's ID is placed in the fixed resume template, so
// this check is what keeps arbitrary transcript bytes out of argv.
func safeSessionID(id string) bool {
	if id == "" || len(id) > maxExternalIDBytes || id[0] == '-' {
		return false
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_' || c == '.' {
			continue
		}
		return false
	}
	return true
}

// titleFor is a session row's title: the cwd basename, else the store's bucket or
// a neutral fallback.
func titleFor(cwd, fallback string) string {
	if base := filepath.Base(strings.TrimSpace(cwd)); base != "." && base != string(filepath.Separator) && base != "" {
		return clipText(base, maxExternalTitleBytes)
	}
	return clipText(fallback, maxExternalTitleBytes)
}

// boundProblems caps the problem list at the package's bound.
func boundProblems(problems []string) []string {
	if len(problems) <= defaultExternalMaxProblems {
		return problems
	}
	return problems[:defaultExternalMaxProblems]
}
