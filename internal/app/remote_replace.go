package app

// Remote project replacement for a remote workspace (task-115). The replacement runs on
// the remote host through the workspace authority: every candidate file is read through
// authority.Files(), the same search.Query/search.PlanReplace semantics as the local
// replacer decide the edits, and the result is written back atomically (temp in the same
// remote directory + rename, mode preserved) through the same FileSystem. Nothing here
// reads or writes the local file system, and there is no local fallback.
//
// Safety rules: the workspace trust gate runs before the first remote operation; per-file
// size, total bytes, file count and wall-clock budgets bound one run; a file whose
// search-time hash no longer matches is skipped as stale; a file whose size or mtime
// changed between the read and the write is skipped as changed (never written at a stale
// snapshot); any read/write error aborts the run with a precise error naming the file and
// how many files were already written. There is no cross-file rollback and no undo
// history for the direct writes (recorded residual).

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"path"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/tbxark/gvim/authority"
	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/internal/tui/searchpanel"
	"github.com/tbxark/gvim/plugin/hostos"
	"github.com/tbxark/gvim/search"
)

const (
	// remoteReplaceMaxFile bounds one candidate file: larger files are skipped, not read.
	remoteReplaceMaxFile = 4 << 20
	// remoteReplaceMaxTotal bounds one run's reads/writes.
	remoteReplaceMaxTotal = 32 << 20
	// remoteReplaceMaxFiles bounds how many files one run writes.
	remoteReplaceMaxFiles = 512
	// remoteReplaceTimeout bounds one run.
	remoteReplaceTimeout = 20 * time.Second
)

// remoteReplacer implements searchpanel.RemoteReplacer over the authority file system.
type remoteReplacer struct {
	files   authority.FileSystem
	gate    func(ctx context.Context, argv []string, cwd string) error
	root    string // absolute remote workspace root
	label   string // status label, "SSH:user@host"
	timeout time.Duration
}

// newRemoteProjectReplacer returns the remote replacer of root over files. gate is the
// workspace trust gate (nil in tests that do not exercise trust).
func newRemoteProjectReplacer(files authority.FileSystem, gate func(ctx context.Context, argv []string, cwd string) error, root, label string) *remoteReplacer {
	return &remoteReplacer{files: files, gate: gate, root: root, label: label, timeout: remoteReplaceTimeout}
}

// RemoteTarget names the backend in the confirmation text.
func (r *remoteReplacer) RemoteTarget() string {
	if r.root == "" {
		return r.label
	}
	return r.label + ":" + r.root
}

// ProjectReplace is the local plan interface. A remote replacement must never be applied
// as a local edit plan, so this refuses; the root calls ProjectReplaceRemote instead.
func (r *remoteReplacer) ProjectReplace(searchpanel.ReplaceRequest) (searchpanel.ReplaceResult, error) {
	return searchpanel.ReplaceResult{}, errors.New("remote project replace writes on the remote host; use ProjectReplaceRemote")
}

// ProjectReplaceRemote applies one replacement over the remote workspace.
func (r *remoteReplacer) ProjectReplaceRemote(parent context.Context, req searchpanel.ReplaceRequest) (searchpanel.RemoteReplaceResult, error) {
	var out searchpanel.RemoteReplaceResult
	if req.Query == "" {
		return out, nil
	}
	q := search.Query{
		Text:          req.Query,
		CaseSensitive: req.Options.CaseSensitive,
		WholeWord:     req.Options.WholeWord,
		Regex:         req.Options.Regex,
	}
	if err := validateQuery(q); err != nil {
		return out, err
	}
	if r.files == nil {
		return out, errors.New("remote project replace: the workspace authority mounts no file system")
	}
	limit := req.Limit
	if limit <= 0 {
		limit = searchpanel.DefaultLimit
	}
	timeout := r.timeout
	if timeout <= 0 {
		timeout = remoteReplaceTimeout
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	if r.gate != nil {
		if err := r.gate(ctx, []string{"gvim-remote-replace", r.root}, r.root); err != nil {
			return out, fmt.Errorf("remote project replace: %w", err)
		}
	}
	editsLeft, total := limit, int64(0)
	for _, f := range req.Files {
		if err := ctx.Err(); err != nil {
			return out, err
		}
		if out.Files >= remoteReplaceMaxFiles {
			return out, fmt.Errorf("remote project replace: the run reached its %d-file budget; %d files were written", remoteReplaceMaxFiles, out.Files)
		}
		if editsLeft <= 0 {
			break
		}
		if f.Dirty {
			// An open buffer's text is not the remote file's text, and writing the file
			// under a live buffer is never safe: skipped as a recorded residual.
			out.Skipped++
			continue
		}
		if !r.inside(f.Path) {
			return out, fmt.Errorf("remote project replace: %s is outside the remote workspace %s", f.Path, r.root)
		}
		st, err := r.files.Stat(ctx, f.Path)
		if err != nil {
			return out, fmt.Errorf("remote project replace: stat %s: %w", f.Path, err)
		}
		if st.Kind != "file" {
			out.Skipped++
			continue
		}
		if st.Size > remoteReplaceMaxFile {
			out.Skipped++
			continue
		}
		if total+st.Size > remoteReplaceMaxTotal {
			return out, fmt.Errorf("remote project replace: the run reached its %d-byte budget; %d files were written", remoteReplaceMaxTotal, out.Files)
		}
		data, err := r.files.ReadFile(ctx, f.Path, st.Size+1)
		if err != nil {
			return out, fmt.Errorf("remote project replace: read %s: %w", f.Path, err)
		}
		if err := ctx.Err(); err != nil {
			return out, err
		}
		if int64(len(data)) != st.Size {
			out.Skipped++
			continue
		}
		total += int64(len(data))
		if !utf8.Valid(data) {
			out.Skipped++
			continue
		}
		probe := data
		if len(probe) > searchBinaryProbe {
			probe = probe[:searchBinaryProbe]
		}
		if bytes.IndexByte(probe, 0) >= 0 {
			out.Skipped++
			continue
		}
		text := string(data)
		if f.Hash != 0 && hashText(text) != f.Hash {
			out.Skipped++
			continue
		}
		plan, err := search.PlanReplace(buffer.New(text), q, req.Replacement)
		if err != nil {
			return out, fmt.Errorf("remote project replace: %s: %w", f.Path, err)
		}
		edits := selectedReplaceEdits(plan, f.Selected)
		if len(edits) == 0 {
			continue
		}
		if len(edits) > editsLeft {
			edits = edits[:editsLeft]
		}
		next, err := remoteReplaceText(text, edits)
		if err != nil {
			return out, fmt.Errorf("remote project replace: %s: %w", f.Path, err)
		}
		// Never write through a stale snapshot: the file must still have the size and
		// mtime it had when the run read it.
		after, err := r.files.Stat(ctx, f.Path)
		if err != nil {
			return out, fmt.Errorf("remote project replace: stat %s before writing: %w", f.Path, err)
		}
		if after.Kind != "file" || after.Size != st.Size || !after.MTime.Equal(st.MTime) {
			out.Skipped++
			continue
		}
		if err := ctx.Err(); err != nil {
			return out, err
		}
		if err := r.files.WriteFile(ctx, f.Path, []byte(next), hostos.WriteOptions{Atomic: true}); err != nil {
			return out, fmt.Errorf("remote project replace: write %s: %w (%d files already written)", f.Path, err, out.Files)
		}
		out.Files++
		editsLeft -= len(edits)
	}
	return out, nil
}

// inside reports whether p is the remote root or under it.
func (r *remoteReplacer) inside(p string) bool {
	if p == "" || r.root == "" {
		return false
	}
	clean := path.Clean(p)
	root := path.Clean(r.root)
	return clean == root || strings.HasPrefix(clean, root+"/")
}

// selectedReplaceEdits keeps the plan entries whose start the panel checked. An empty
// selection means every match of the file, as in the local replacer.
func selectedReplaceEdits(plan []search.Edit, selected []searchpanel.Position) []search.Edit {
	if len(selected) == 0 {
		return plan
	}
	want := make(map[[2]int]bool, len(selected))
	for _, p := range selected {
		want[[2]int{p.Line, p.Col}] = true
	}
	out := make([]search.Edit, 0, len(plan))
	for _, e := range plan {
		if want[[2]int{e.From.Line, e.From.Col}] {
			out = append(out, e)
		}
	}
	return out
}

// remoteReplaceText applies the plan's edits to text. Positions are rune columns of the
// remote file's text, the same as the local replacer's plan.
func remoteReplaceText(text string, edits []search.Edit) (string, error) {
	lines := strings.Split(text, "\n")
	runes := []rune(text)
	offset := func(p buffer.Pos) (int, error) {
		if p.Line < 0 || p.Line >= len(lines) || p.Col < 0 {
			return 0, fmt.Errorf("replacement position %+v is outside the file", p)
		}
		n := 0
		for i := 0; i < p.Line; i++ {
			n += len([]rune(lines[i])) + 1
		}
		n += p.Col
		if p.Col > len([]rune(lines[p.Line])) || n > len(runes) {
			return 0, fmt.Errorf("replacement position %+v is outside the file", p)
		}
		return n, nil
	}
	from := make([]int, len(edits))
	to := make([]int, len(edits))
	for i, e := range edits {
		var err error
		if from[i], err = offset(e.From); err != nil {
			return "", err
		}
		if to[i], err = offset(e.To); err != nil {
			return "", err
		}
		if from[i] > to[i] {
			return "", fmt.Errorf("replacement range %+v..%+v is inverted", e.From, e.To)
		}
	}
	var out []rune
	last := 0
	for i, e := range edits {
		if from[i] < last {
			return "", fmt.Errorf("overlapping replacement at %+v", e.From)
		}
		out = append(out, runes[last:from[i]]...)
		out = append(out, []rune(e.Text)...)
		last = to[i]
	}
	out = append(out, runes[last:]...)
	return string(out), nil
}
