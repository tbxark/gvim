package uiadapter

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/tbxark/gvim/authority"
	"github.com/tbxark/gvim/diff"
	"github.com/tbxark/gvim/internal/tui"
	"github.com/tbxark/gvim/internal/tui/reviewdiff"
	"github.com/tbxark/gvim/plugin/hostos"
)

const (
	// gitEmptyTreeHash is Git's well-known SHA-1 for an empty tree object, used
	// when diffing a brand-new repository whose HEAD has no commits yet.
	gitEmptyTreeHash = "4b825dc642cb6eb9a060e54bf8d69288fbee4904"

	reviewGitTimeout          = 10 * time.Second
	reviewMaxStatusBytes      = 4 << 20 // 4 MiB cap for name-status/numstat/ls-files
	reviewMaxFileBytes        = 2 << 20 // 2 MiB cap per file side
	reviewMaxLines            = 50000
	reviewMaxFiles            = 2000
	reviewMaxUntrackedStats   = 64
	reviewUntrackedStatBytes  = 512 << 10
	reviewBinaryProbeBytes    = 8192
)

// ReviewGit implements tui.ReviewGitPort over a workspace authority.Slot so
// Review Diff Mode works identically on local and SSH remote workspaces. When
// Slot is nil (e.g. standalone tests), it runs local git and os.ReadFile.
type ReviewGit struct {
	Slot   authority.Slot
	Remote bool
}

var _ tui.ReviewGitPort = ReviewGit{}

// Changes lists all working-tree changes relative to baseRef plus untracked
// files in a single flat list (each changed path appears once).
func (g ReviewGit) Changes(ctx context.Context, dir, baseRef string) (tui.ReviewChangesResult, error) {
	if dir == "" {
		return tui.ReviewChangesResult{NotRepo: true, BaseRef: defaultBase(baseRef)}, nil
	}
	baseRef = defaultBase(baseRef)

	topRes, err := g.runGit(ctx, dir, reviewMaxStatusBytes, "rev-parse", "--show-toplevel")
	if err != nil {
		return tui.ReviewChangesResult{}, err
	}
	if topRes.Code != 0 {
		if isNotRepoError(topRes.Stderr) {
			return tui.ReviewChangesResult{NotRepo: true, BaseRef: baseRef}, nil
		}
		return tui.ReviewChangesResult{}, gitErr("rev-parse", topRes)
	}
	repoRoot := strings.TrimSpace(string(topRes.Stdout))
	if repoRoot == "" {
		return tui.ReviewChangesResult{NotRepo: true, BaseRef: baseRef}, nil
	}
	if !g.Remote {
		repoRoot = filepath.Clean(repoRoot)
	}

	branch := g.readBranch(ctx, repoRoot)

	effectiveBase, unborn, err := g.resolveBase(ctx, repoRoot, baseRef)
	if err != nil {
		return tui.ReviewChangesResult{RepoRoot: repoRoot, Branch: branch, BaseRef: baseRef}, err
	}

	// 1. Tracked changes relative to effectiveBase (working tree vs base commit).
	nsRes, err := g.runGit(ctx, repoRoot, reviewMaxStatusBytes, "diff", "--no-color", "-M", "--name-status", "-z", effectiveBase, "--")
	if err != nil {
		return tui.ReviewChangesResult{}, err
	}
	if nsRes.Code != 0 {
		return tui.ReviewChangesResult{RepoRoot: repoRoot, Branch: branch, BaseRef: baseRef}, gitErr("diff --name-status", nsRes)
	}
	entries := parseNameStatusZ(nsRes.Stdout)

	// 2. Per-file line stats (+add/-del and binary flag).
	statRes, err := g.runGit(ctx, repoRoot, reviewMaxStatusBytes, "diff", "--no-color", "-M", "--numstat", "-z", effectiveBase, "--")
	if err == nil && statRes.Code == 0 {
		stats := parseNumstatZ(statRes.Stdout)
		for i := range entries {
			if st, ok := stats[entries[i].Path]; ok {
				entries[i].Added = st.added
				entries[i].Deleted = st.deleted
				entries[i].Binary = st.binary
			}
		}
	}

	// 3. Untracked non-ignored files.
	byPath := make(map[string]struct{}, len(entries))
	for _, e := range entries {
		byPath[e.Path] = struct{}{}
	}
	lsRes, err := g.runGit(ctx, repoRoot, reviewMaxStatusBytes, "ls-files", "--others", "--exclude-standard", "-z")
	if err == nil && lsRes.Code == 0 {
		untracked := parseNullList(lsRes.Stdout)
		statCount := 0
		for _, p := range untracked {
			if _, exists := byPath[p]; exists {
				continue
			}
			byPath[p] = struct{}{}
			fe := reviewdiff.FileEntry{
				Path:   p,
				Status: reviewdiff.StatusUntracked,
			}
			if unborn {
				fe.Status = reviewdiff.StatusUntracked
			}
			if statCount < reviewMaxUntrackedStats {
				statCount++
				if raw, _, rerr := g.readWorkFile(ctx, repoRoot, p, reviewUntrackedStatBytes); rerr == nil {
					if looksBinary(raw) {
						fe.Binary = true
					} else {
						fe.Added = countLines(raw)
					}
				} else {
					fe.StatOnly = true
				}
			} else {
				fe.StatOnly = true
			}
			entries = append(entries, fe)
			if len(entries) >= reviewMaxFiles {
				break
			}
		}
	}

	if len(entries) > reviewMaxFiles {
		entries = entries[:reviewMaxFiles]
	}
	slices.SortFunc(entries, func(a, b reviewdiff.FileEntry) int {
		return strings.Compare(a.Path, b.Path)
	})

	return tui.ReviewChangesResult{
		RepoRoot: repoRoot,
		Branch:   branch,
		BaseRef:  baseRef,
		Files:    entries,
	}, nil
}

// FileDiff loads the old and new content of entry relative to baseRef and
// computes its line hunks off the UI goroutine.
func (g ReviewGit) FileDiff(ctx context.Context, repoRoot, baseRef string, entry reviewdiff.FileEntry) (*reviewdiff.FileDiff, error) {
	baseRef = defaultBase(baseRef)
	out := &reviewdiff.FileDiff{
		Path:    entry.Path,
		OldPath: entry.OldPath,
		Status:  entry.Status,
		BaseRef: baseRef,
		Binary:  entry.Binary,
	}
	if entry.Binary {
		out.Message = "Binary file changed"
		return out, nil
	}

	effectiveBase, unborn, err := g.resolveBase(ctx, repoRoot, baseRef)
	if err != nil {
		return nil, err
	}

	var oldRaw, newRaw []byte
	var truncated bool

	// Read old side from git object store unless newly created/untracked/unborn.
	if !unborn && entry.Status != reviewdiff.StatusUntracked && entry.Status != reviewdiff.StatusAdded {
		oldPath := entry.Path
		if entry.OldPath != "" {
			oldPath = entry.OldPath
		}
		res, rerr := g.runGit(ctx, repoRoot, reviewMaxFileBytes+1, "show", effectiveBase+":"+oldPath)
		if rerr != nil {
			return nil, rerr
		}
		if res.Code == 0 {
			oldRaw = res.Stdout
			if res.Truncated || len(oldRaw) > reviewMaxFileBytes {
				oldRaw = trimAtLineBoundary(oldRaw, reviewMaxFileBytes)
				truncated = true
			}
		}
	}

	// Read new side from the working tree unless deleted.
	if entry.Status != reviewdiff.StatusDeleted {
		raw, trunc, rerr := g.readWorkFile(ctx, repoRoot, entry.Path, reviewMaxFileBytes)
		if rerr != nil {
			if !errors.Is(rerr, os.ErrNotExist) {
				return nil, rerr
			}
		} else {
			newRaw = raw
			if trunc {
				truncated = true
			}
		}
	}

	if looksBinary(oldRaw) || looksBinary(newRaw) {
		out.Binary = true
		out.Message = "Binary file changed"
		return out, nil
	}

	oldLines, oldCut := splitLinesBounded(oldRaw, reviewMaxLines)
	newLines, newCut := splitLinesBounded(newRaw, reviewMaxLines)
	if oldCut || newCut {
		truncated = true
	}
	out.OldLines = oldLines
	out.NewLines = newLines
	out.Truncated = truncated
	if truncated {
		out.Message = "File truncated to preview limit"
	}
	out.Hunks = diff.Lines(oldLines, newLines)
	return out, nil
}

// Refs lists HEAD followed by local branches, remote branches, and tags sorted
// by most recent committer date for the base-ref picker.
func (g ReviewGit) Refs(ctx context.Context, dir string) ([]string, error) {
	if dir == "" {
		return []string{"HEAD"}, nil
	}
	res, err := g.runGit(ctx, dir, reviewMaxStatusBytes,
		"for-each-ref", "--sort=-committerdate", "--count=60",
		"--format=%(refname:short)", "refs/heads", "refs/remotes", "refs/tags",
	)
	if err != nil {
		return []string{"HEAD"}, err
	}
	seen := map[string]struct{}{"HEAD": {}}
	out := []string{"HEAD"}
	if res.Code != 0 {
		return out, nil
	}
	var rest []string
	var mainRefs []string
	for line := range strings.SplitSeq(string(res.Stdout), "\n") {
		ref := strings.TrimSpace(line)
		if ref == "" || strings.HasSuffix(ref, "/HEAD") {
			continue
		}
		if _, ok := seen[ref]; ok {
			continue
		}
		seen[ref] = struct{}{}
		if ref == "main" || ref == "master" || ref == "origin/main" || ref == "origin/master" {
			mainRefs = append(mainRefs, ref)
		} else {
			rest = append(rest, ref)
		}
	}
	out = append(out, mainRefs...)
	out = append(out, rest...)
	return out, nil
}

func defaultBase(baseRef string) string {
	baseRef = strings.TrimSpace(baseRef)
	if baseRef == "" {
		return "HEAD"
	}
	return baseRef
}

func (g ReviewGit) readBranch(ctx context.Context, repoRoot string) string {
	if res, err := g.runGit(ctx, repoRoot, 4096, "symbolic-ref", "--short", "HEAD"); err == nil && res.Code == 0 {
		if b := strings.TrimSpace(string(res.Stdout)); b != "" {
			return b
		}
	}
	if res, err := g.runGit(ctx, repoRoot, 4096, "rev-parse", "--short", "HEAD"); err == nil && res.Code == 0 {
		if h := strings.TrimSpace(string(res.Stdout)); h != "" {
			return h
		}
	}
	return "(unborn)"
}

func (g ReviewGit) resolveBase(ctx context.Context, repoRoot, baseRef string) (resolved string, unborn bool, err error) {
	res, err := g.runGit(ctx, repoRoot, 4096, "rev-parse", "--verify", "--quiet", baseRef+"^{commit}")
	if err != nil {
		return "", false, err
	}
	if res.Code == 0 {
		return baseRef, false, nil
	}
	if baseRef == "HEAD" {
		// Brand-new repo with no commits yet: diff against empty tree.
		return gitEmptyTreeHash, true, nil
	}
	return "", false, fmt.Errorf("unknown git ref %q", baseRef)
}

func (g ReviewGit) runGit(ctx context.Context, cwd string, maxOutput int, args ...string) (hostos.ProcResult, error) {
	argv := make([]string, 0, len(args)+10)
	argv = append(argv,
		"git",
		"--no-optional-locks",
		"-c", "core.quotepath=false",
		"-c", "diff.external=",
		"-c", "diff.noprefix=false",
		"-c", "core.fsmonitor=false",
	)
	argv = append(argv, args...)

	runCtx, cancel := context.WithTimeout(ctx, reviewGitTimeout)
	defer cancel()

	env := map[string]string{
		"GIT_TERMINAL_PROMPT": "0",
		"LC_ALL":              "C",
	}
	if g.Slot != nil {
		return g.Slot.OneShot().Run(runCtx, argv, hostos.ProcOptions{
			Cwd:       cwd,
			Env:       env,
			Timeout:   reviewGitTimeout,
			MaxOutput: maxOutput,
		})
	}
	return runLocalGit(runCtx, cwd, maxOutput, env, argv)
}

func runLocalGit(ctx context.Context, cwd string, maxOutput int, extraEnv map[string]string, argv []string) (hostos.ProcResult, error) {
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir = cwd
	cmd.Env = os.Environ()
	for k, v := range extraEnv {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	var stdout, stderr limitedBuffer
	stdout.limit = maxOutput
	stderr.limit = 64 << 10
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	res := hostos.ProcResult{
		Stdout:    stdout.buf.Bytes(),
		Stderr:    stderr.buf.Bytes(),
		Truncated: stdout.truncated,
	}
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			res.Code = ee.ExitCode()
			return res, nil
		}
		return res, err
	}
	return res, nil
}

type limitedBuffer struct {
	buf       bytes.Buffer
	limit     int
	truncated bool
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if b.limit <= 0 {
		return b.buf.Write(p)
	}
	rem := b.limit - b.buf.Len()
	if rem <= 0 {
		b.truncated = true
		return len(p), nil
	}
	if len(p) > rem {
		_, _ = b.buf.Write(p[:rem])
		b.truncated = true
		return len(p), nil
	}
	return b.buf.Write(p)
}

func (g ReviewGit) readWorkFile(ctx context.Context, repoRoot, rel string, maxBytes int) ([]byte, bool, error) {
	abs := g.joinPath(repoRoot, rel)
	if g.Slot != nil {
		raw, err := g.Slot.Files().ReadFile(ctx, abs, int64(maxBytes+1))
		if err != nil {
			return nil, false, err
		}
		if len(raw) > maxBytes {
			return trimAtLineBoundary(raw, maxBytes), true, nil
		}
		return raw, false, nil
	}
	f, err := os.Open(abs)
	if err != nil {
		return nil, false, err
	}
	defer func() { _ = f.Close() }()
	raw, err := io.ReadAll(io.LimitReader(f, int64(maxBytes+1)))
	if err != nil {
		return nil, false, err
	}
	if len(raw) > maxBytes {
		return trimAtLineBoundary(raw, maxBytes), true, nil
	}
	return raw, false, nil
}

func (g ReviewGit) joinPath(repoRoot, rel string) string {
	if g.Remote {
		return path.Join(repoRoot, rel)
	}
	return filepath.Join(repoRoot, filepath.FromSlash(rel))
}

func isNotRepoError(stderr []byte) bool {
	s := strings.ToLower(string(stderr))
	return strings.Contains(s, "not a git repository") || strings.Contains(s, "must be run in a work tree")
}

func gitErr(op string, res hostos.ProcResult) error {
	msg := strings.TrimSpace(string(res.Stderr))
	if msg != "" {
		first, _, _ := strings.Cut(msg, "\n")
		return fmt.Errorf("git %s: %s", op, first)
	}
	return fmt.Errorf("git %s: exit status %d", op, res.Code)
}

func parseNameStatusZ(raw []byte) []reviewdiff.FileEntry {
	if len(raw) == 0 {
		return nil
	}
	parts := bytes.Split(raw, []byte{0})
	var out []reviewdiff.FileEntry
	for i := 0; i < len(parts); {
		code := string(parts[i])
		i++
		if code == "" || i >= len(parts) {
			break
		}
		switch code[0] {
		case 'R', 'C':
			if i+1 >= len(parts) {
				break
			}
			oldPath := string(parts[i])
			newPath := string(parts[i+1])
			i += 2
			if newPath == "" {
				continue
			}
			st := reviewdiff.StatusRenamed
			if code[0] == 'C' {
				st = reviewdiff.StatusAdded
			}
			out = append(out, reviewdiff.FileEntry{Path: newPath, OldPath: oldPath, Status: st})
		default:
			p := string(parts[i])
			i++
			if p == "" {
				continue
			}
			out = append(out, reviewdiff.FileEntry{Path: p, Status: mapStatusByte(code[0])})
		}
	}
	return out
}

func mapStatusByte(b byte) reviewdiff.FileStatus {
	switch b {
	case 'A':
		return reviewdiff.StatusAdded
	case 'D':
		return reviewdiff.StatusDeleted
	case 'R':
		return reviewdiff.StatusRenamed
	case 'U':
		return reviewdiff.StatusConflicted
	default:
		return reviewdiff.StatusModified
	}
}

type numstat struct {
	added   int
	deleted int
	binary  bool
}

func parseNumstatZ(raw []byte) map[string]numstat {
	out := make(map[string]numstat)
	if len(raw) == 0 {
		return out
	}
	parts := bytes.Split(raw, []byte{0})
	for i := 0; i < len(parts); {
		rec := string(parts[i])
		i++
		if rec == "" {
			continue
		}
		fields := strings.SplitN(rec, "\t", 3)
		if len(fields) < 3 {
			continue
		}
		st := numstat{}
		if fields[0] == "-" || fields[1] == "-" {
			st.binary = true
		} else {
			st.added, _ = strconv.Atoi(fields[0])
			st.deleted, _ = strconv.Atoi(fields[1])
		}
		p := fields[2]
		if p == "" {
			// Rename/copy record in `git diff --numstat -z`:
			// "<add>\t<del>\t\0<old>\0<new>\0"
			if i+1 >= len(parts) {
				break
			}
			p = string(parts[i+1])
			i += 2
		}
		if p != "" {
			out[p] = st
		}
	}
	return out
}

func parseNullList(raw []byte) []string {
	if len(raw) == 0 {
		return nil
	}
	parts := bytes.Split(raw, []byte{0})
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if len(p) > 0 {
			out = append(out, string(p))
		}
	}
	return out
}

func looksBinary(raw []byte) bool {
	if len(raw) == 0 {
		return false
	}
	sample := raw
	if len(sample) > reviewBinaryProbeBytes {
		sample = sample[:reviewBinaryProbeBytes]
	}
	return bytes.IndexByte(sample, 0) >= 0
}

func trimAtLineBoundary(raw []byte, limit int) []byte {
	if len(raw) <= limit {
		return raw
	}
	cut := raw[:limit]
	if idx := bytes.LastIndexByte(cut, '\n'); idx > 0 {
		return cut[:idx+1]
	}
	return cut
}

func countLines(raw []byte) int {
	if len(raw) == 0 {
		return 0
	}
	n := bytes.Count(raw, []byte{'\n'})
	if raw[len(raw)-1] != '\n' {
		n++
	}
	return n
}

func splitLinesBounded(raw []byte, maxLines int) ([]string, bool) {
	if len(raw) == 0 {
		return nil, false
	}
	norm := strings.ReplaceAll(string(raw), "\r\n", "\n")
	norm = strings.TrimSuffix(norm, "\n")
	lines := strings.Split(norm, "\n")
	if len(lines) > maxLines {
		return lines[:maxLines], true
	}
	return lines, false
}
