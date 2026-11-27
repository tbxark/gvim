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
	"sync"
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

	reviewGitTimeout         = 10 * time.Second
	reviewMaxStatusBytes     = 4 << 20 // 4 MiB cap for name-status/numstat/ls-files
	reviewMaxFileBytes       = 2 << 20 // 2 MiB cap per file side
	reviewMaxLines           = 50000
	reviewMaxFiles           = 2000
	reviewMaxUntrackedStats  = 64
	reviewUntrackedStatBytes = 512 << 10
	reviewBinaryProbeBytes   = 8192
)

// ReviewGit implements tui.ReviewGitPort over a workspace authority.Slot so
// Review Diff Mode works identically on local and SSH remote workspaces. When
// Slot is nil (e.g. standalone tests), it runs local git and os.ReadFile.
type ReviewGit struct {
	Slot   authority.Slot
	Remote bool
}

var _ tui.ReviewGitPort = ReviewGit{}

// Changes lists the differences for scope. The working-tree scope compares
// against baseRef, while staged and unstaged use the index as one side.
func (g ReviewGit) Changes(ctx context.Context, dir, baseRef string, scope reviewdiff.Scope) (tui.ReviewChangesResult, error) {
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

	var operands []string
	switch scope {
	case reviewdiff.ScopeStaged:
		operands = []string{"--cached"}
	case reviewdiff.ScopeWorkingTree:
		effectiveBase, _, err := g.resolveBase(ctx, repoRoot, baseRef)
		if err != nil {
			return tui.ReviewChangesResult{RepoRoot: repoRoot, Branch: branch, BaseRef: baseRef}, err
		}
		operands = []string{effectiveBase}
	}
	names := append([]string{"diff", "--no-color", "--no-ext-diff", "--no-textconv", "-M", "--name-status", "-z"}, operands...)
	statsArgs := append([]string{"diff", "--no-color", "--no-ext-diff", "--no-textconv", "-M", "--numstat", "-z"}, operands...)

	// 1. Tracked changes relative to effectiveBase (working tree vs base commit).
	nsRes, err := g.runGit(ctx, repoRoot, reviewMaxStatusBytes, append(names, "--")...)
	if err != nil {
		return tui.ReviewChangesResult{}, err
	}
	if nsRes.Code != 0 {
		return tui.ReviewChangesResult{RepoRoot: repoRoot, Branch: branch, BaseRef: baseRef}, gitErr("diff --name-status", nsRes)
	}
	entries := parseNameStatusZ(nsRes.Stdout)
	if scope != reviewdiff.ScopeWorkingTree {
		// An unmerged index can appear as both U and M for the same path.
		conflicted := make(map[string]bool)
		for _, entry := range entries {
			if entry.Status == reviewdiff.StatusConflicted {
				conflicted[entry.Path] = true
			}
		}
		entries = slices.DeleteFunc(entries, func(entry reviewdiff.FileEntry) bool {
			return conflicted[entry.Path] && entry.Status != reviewdiff.StatusConflicted
		})
	}

	// 2. Per-file line stats (+add/-del and binary flag).
	statRes, err := g.runGit(ctx, repoRoot, reviewMaxStatusBytes, append(statsArgs, "--")...)
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
	if scope != reviewdiff.ScopeStaged {
		lsRes, err := g.runGit(ctx, repoRoot, reviewMaxStatusBytes, "ls-files", "--others", "--exclude-standard", "-z")
		if err == nil && lsRes.Code == 0 {
			untracked := parseNullList(lsRes.Stdout)
			first := len(entries)
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
				if statCount < reviewMaxUntrackedStats {
					statCount++
				} else {
					fe.StatOnly = true
				}
				entries = append(entries, fe)
				if len(entries) >= reviewMaxFiles {
					break
				}
			}
			g.statUntracked(ctx, repoRoot, entries[first:])
		}
	}

	if len(entries) > reviewMaxFiles {
		entries = entries[:reviewMaxFiles]
	}
	for i := range entries {
		entries[i].Scope = scope
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

// reviewStatWorkers bounds the untracked-file reads of one Changes call that are in flight at
// once; over SSH each is a round trip.
const reviewStatWorkers = 8

// statUntracked fills the line count or binary flag of the untracked entries that are not
// already StatOnly, reading their first bytes with at most reviewStatWorkers reads at once.
// A file that cannot be read becomes StatOnly.
func (g ReviewGit) statUntracked(ctx context.Context, repoRoot string, entries []reviewdiff.FileEntry) {
	var wg sync.WaitGroup
	sem := make(chan struct{}, reviewStatWorkers)
	for i := range entries {
		if entries[i].StatOnly {
			continue
		}
		sem <- struct{}{}
		wg.Add(1)
		go func() {
			defer func() { <-sem; wg.Done() }()
			fe := &entries[i]
			raw, _, err := g.readWorkFile(ctx, repoRoot, fe.Path, reviewUntrackedStatBytes)
			raw = trimAtLineBoundary(raw, reviewUntrackedStatBytes)
			switch {
			case err != nil:
				fe.StatOnly = true
			case looksBinary(raw):
				fe.Binary = true
			default:
				fe.Added = countLines(raw)
			}
		}()
	}
	wg.Wait()
}

// FileDiff loads the old and new content of entry relative to baseRef and
// computes its line hunks off the UI goroutine.
func (g ReviewGit) FileDiff(ctx context.Context, repoRoot, baseRef string, entry reviewdiff.FileEntry) (*reviewdiff.FileDiff, error) {
	baseRef = defaultBase(baseRef)
	out := &reviewdiff.FileDiff{
		Path: entry.Path, OldPath: entry.OldPath, Status: entry.Status,
		BaseRef: baseRef, Scope: entry.Scope, Binary: entry.Binary,
	}
	if entry.Status == reviewdiff.StatusConflicted && entry.Scope != reviewdiff.ScopeWorkingTree {
		out.Binary = true
		out.Message = "Unmerged file — resolve the conflict before reviewing the index"
		return out, nil
	}
	imageFile := tui.ImagePreviewPath(entry.Path) || tui.ImagePreviewPath(entry.OldPath)
	if entry.Binary && !imageFile {
		out.Message = "Binary file changed"
		return out, nil
	}

	oldRef := ":"
	unborn := false
	if entry.Scope != reviewdiff.ScopeUnstaged {
		ref := baseRef
		if entry.Scope == reviewdiff.ScopeStaged {
			ref = "HEAD"
		}
		resolved, empty, err := g.resolveBase(ctx, repoRoot, ref)
		if err != nil {
			return nil, err
		}
		oldRef, unborn = resolved+":", empty
	}

	oldPath := entry.Path
	if entry.OldPath != "" {
		oldPath = entry.OldPath
	}
	hasOld := !unborn && entry.Status != reviewdiff.StatusUntracked && entry.Status != reviewdiff.StatusAdded
	hasNew := entry.Status != reviewdiff.StatusDeleted
	newRef := ""
	if entry.Scope == reviewdiff.ScopeStaged {
		newRef = ":"
	}

	limit := reviewMaxFileBytes
	if imageFile {
		limit = tui.ImagePreviewMaxBytes
	}
	var data [2][]byte
	var cut [2]bool
	var readErr [2]error
	for i, side := range [2]struct {
		path, ref string
		present   bool
	}{{oldPath, oldRef, hasOld}, {entry.Path, newRef, hasNew}} {
		if side.present {
			data[i], cut[i], readErr[i] = g.readDiffSide(ctx, repoRoot, side.path, side.ref, limit)
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if imageFile {
		var images [2]reviewdiff.ImageSide
		var hasImage bool
		for i, raw := range data {
			var err error
			switch {
			case readErr[i] != nil:
				err = readErr[i]
			case cut[i]:
				images[i].Error = "Image exceeds the preview limit"
			case raw != nil:
				images[i].Pixels, images[i].Size, err = tui.DecodeImagePreview(raw)
			}
			if err != nil {
				images[i].Error = err.Error()
			}
			hasImage = hasImage || images[i].Pixels != nil
		}
		if hasImage || entry.Binary {
			out.Binary, out.Images = true, &images
			out.Message = "Image file changed"
			return out, nil
		}
	}
	for _, err := range readErr {
		if err != nil {
			return nil, err
		}
	}
	oldRaw, newRaw := data[0], data[1]
	if looksBinary(oldRaw) || looksBinary(newRaw) {
		out.Binary = true
		out.Message = "Binary file changed"
		return out, nil
	}
	truncated := cut[0] || cut[1] || len(oldRaw) > reviewMaxFileBytes || len(newRaw) > reviewMaxFileBytes
	oldRaw = trimAtLineBoundary(oldRaw, reviewMaxFileBytes)
	newRaw = trimAtLineBoundary(newRaw, reviewMaxFileBytes)

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

// readDiffSide returns raw bytes; callers applying text limits trim to a line boundary.
func (g ReviewGit) readDiffSide(ctx context.Context, root, file, ref string, limit int) ([]byte, bool, error) {
	if ref == "" {
		data, cut, err := g.readWorkFile(ctx, root, file, limit)
		if errors.Is(err, os.ErrNotExist) {
			return nil, false, nil
		}
		return data, cut, err
	}
	res, err := g.runGit(ctx, root, limit+1, "show", ref+file)
	if err != nil {
		return nil, false, err
	}
	if res.Code != 0 {
		return nil, false, gitErr("show", res)
	}
	return res.Stdout, res.Truncated || len(res.Stdout) > limit, nil
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
	res, err := g.runGit(ctx, repoRoot, 4096, "rev-parse", "--verify", "--quiet", "--end-of-options", baseRef+"^{commit}")
	if err != nil {
		return "", false, err
	}
	if res.Code == 0 {
		return strings.TrimSpace(string(res.Stdout)), false, nil
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
			return raw, true, nil
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
		return raw, true, nil
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
