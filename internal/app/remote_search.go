package app

// Remote project search and git grep for a remote workspace (task-105). The commands run
// on the remote host through the workspace authority's one-shot surface (which applies the
// spawn/trust gate); nothing here reads the local file system, and there is no local
// fallback. Results are bounded, streamed to the finder panel as whole-file batches, and
// cancellation stops the remote process.

import (
	"context"
	"errors"
	"fmt"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/tbxark/gvim/authority"
	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/internal/tui"
	"github.com/tbxark/gvim/internal/tui/searchpanel"
	"github.com/tbxark/gvim/plugin/hostos"
	"github.com/tbxark/gvim/search"
)

const (
	// remoteSearchTimeout bounds one remote project search run.
	remoteSearchTimeout = 20 * time.Second
	// remoteToolProbeTimeout bounds the remote search tool probe.
	remoteToolProbeTimeout = 5 * time.Second
	// remoteSearchMaxOutput is the most remote output kept per stream.
	remoteSearchMaxOutput = 8 << 20
)

// remoteGrepSkipDirs are the directory names the grep fallback never descends into, matching
// the finder's local skip list.
var remoteGrepSkipDirs = []string{".git", "node_modules", "target", "__pycache__", ".hg", ".svn", ".DS_Store"}

// remoteSearch is the project-wide search over a remote workspace: a searchpanel.Searcher
// and BatchSearcher that runs ripgrep (or grep) through the authority's one-shot surface
// with the remote workspace as cwd. The local project search is never consulted.
type remoteSearch struct {
	one      authority.OneShot
	files    authority.FileSystem // for the preview head; nil in tests that do not preview
	root     string               // absolute remote workspace root
	timeout  time.Duration        // bounds one search run (remoteSearchTimeout unless a test says otherwise)
	spawnEnv func() []string
}

// newRemoteProjectSearch returns the remote project search over root.
func newRemoteProjectSearch(one authority.OneShot, root string, spawnEnv ...func() []string) *remoteSearch {
	s := &remoteSearch{one: one, root: root, timeout: remoteSearchTimeout}
	if len(spawnEnv) > 0 {
		s.spawnEnv = spawnEnv[0]
	}
	return s
}

// withFiles installs the authority file system the preview column reads through.
func (s *remoteSearch) withFiles(files authority.FileSystem) *remoteSearch {
	s.files = files
	return s
}

// ProjectSearchBatches starts one remote search and returns the channel its whole-file
// batches arrive on. Closing cancel asks the remote process to stop and closes the channel
// without a Done batch (a superseded search).
func (s *remoteSearch) ProjectSearchBatches(req searchpanel.SearchRequest, cancel <-chan struct{}) <-chan searchpanel.SearchBatch {
	out := make(chan searchpanel.SearchBatch)
	go s.searchBatches(req, cancel, out)
	return out
}

// ProjectSearch runs the whole remote search and returns one result, draining the batches
// (used when the panel does not take the streaming path).
func (s *remoteSearch) ProjectSearch(req searchpanel.SearchRequest) (searchpanel.SearchResult, error) {
	var out searchpanel.SearchResult
	for batch := range s.ProjectSearchBatches(req, make(chan struct{})) {
		out.Files = append(out.Files, batch.Files...)
		out.Truncated = out.Truncated || batch.Truncated
		out.SkippedCount, out.Skipped = batch.SkippedCount, batch.Skipped
		if batch.Done && batch.Err != "" {
			return out, errors.New(batch.Err)
		}
	}
	return out, nil
}

// searchBatches probes the remote search tool, runs it and streams whole-file batches. A
// cancelled search closes the channel without a Done batch.
func (s *remoteSearch) searchBatches(req searchpanel.SearchRequest, cancel <-chan struct{}, out chan<- searchpanel.SearchBatch) {
	defer close(out)
	cancelled := func() bool {
		select {
		case <-cancel:
			return true
		default:
			return false
		}
	}
	send := func(batch searchpanel.SearchBatch) bool {
		select {
		case <-cancel:
			return false
		case out <- batch:
			return true
		}
	}
	if req.Query == "" {
		send(searchpanel.SearchBatch{Done: true})
		return
	}
	q := search.Query{
		Text:          req.Query,
		CaseSensitive: req.Options.CaseSensitive,
		WholeWord:     req.Options.WholeWord,
		Regex:         req.Options.Regex,
	}
	if err := validateQuery(q); err != nil {
		send(searchpanel.SearchBatch{Done: true, Err: err.Error()})
		return
	}
	limit := req.Limit
	if limit <= 0 {
		limit = searchpanel.DefaultLimit
	}
	// The current-file and open-buffers scopes search the editor's remote document
	// snapshots (the authority loaded them; unsaved text included): no local disk is read
	// and no remote process is started.
	if req.Scope == searchpanel.CurrentFile || req.Scope == searchpanel.OpenBuffers {
		s.searchSnapshots(req, q, limit, cancelled, send)
		return
	}
	// The panel hands the file index snapshot (already filtered by its glob), so results
	// outside it are skipped; an empty snapshot (index not ready) searches the whole remote
	// tree with the tool's own ignore rules.
	allowed := remoteAllowedFiles(req.Files)
	ctx, stop := context.WithTimeout(context.Background(), s.timeout)
	defer stop()
	done := make(chan struct{})
	defer close(done)
	if cancel != nil {
		go func() {
			select {
			case <-cancel:
				stop()
			case <-done:
			}
		}()
	}
	tool, err := s.probeTool(ctx)
	if err != nil {
		if cancelled() {
			return
		}
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			send(searchpanel.SearchBatch{Done: true, Err: "remote search tool probe timed out"})
			return
		}
		send(searchpanel.SearchBatch{Done: true, Err: err.Error()})
		return
	}
	// The grep fallback has no ignore rules of its own (and grep would descend hidden
	// directories): narrow its output to Fresh's project-file scope. ripgrep already does
	// that itself, and an index-backed search arrives already filtered by the finder.
	// Both tools use Fresh's project-file scope on the whole-tree path: one candidate
	// listing decides what is searched, which is what makes a tracked file that matches
	// .gitignore searchable (ripgrep's own ignore rules omit it). An index-backed search
	// arrives already filtered by the finder.
	var scope *remoteProjectResultScope
	if allowed == nil {
		scope = s.newProjectResultScope(ctx)
	}
	res, err := s.run(ctx, s.root, remoteSearchArgv(tool, req, scope))
	if err != nil {
		if cancelled() {
			return
		}
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			send(searchpanel.SearchBatch{Done: true, Err: "remote search timed out"})
			return
		}
		send(searchpanel.SearchBatch{Done: true, Err: "remote search failed: " + err.Error()})
		return
	}
	if cancelled() {
		return
	}
	if res.Code > 1 {
		send(searchpanel.SearchBatch{Done: true, Err: fmt.Sprintf("%s failed on the remote host (exit %d)", tool, res.Code)})
		return
	}
	truncated := res.Truncated
	pending := make([]searchpanel.FileMatches, 0, searchBatchFiles)
	pendingMatches := 0
	totalMatches := 0
	flush := func(done bool) bool {
		batch := searchpanel.SearchBatch{Files: pending, Truncated: truncated, Done: done}
		if !send(batch) {
			return false
		}
		pending, pendingMatches = nil, 0
		return true
	}
	var current *searchpanel.FileMatches
	currentRel := ""
	flushCurrent := func() bool {
		if current == nil {
			return true
		}
		pending = append(pending, *current)
		pendingMatches += len(current.Matches)
		current = nil
		if len(pending) >= searchBatchFiles || pendingMatches >= searchBatchMatches {
			return flush(false)
		}
		return true
	}
	for _, line := range strings.Split(string(res.Stdout), "\n") {
		if line == "" || cancelled() {
			break
		}
		rel, lineNo, lineText, byteCol, ok := parseRemoteGrepLine(tool, line)
		if !ok {
			continue
		}
		if scope != nil && !scope.allows(rel) {
			continue
		}
		if allowed != nil && !allowed[rel] {
			continue
		}
		if rel != currentRel {
			if !flushCurrent() {
				return
			}
			currentRel = rel
			current = &searchpanel.FileMatches{Name: rel, Path: joinRemotePath(s.root, rel)}
		}
		matches := remoteLineMatches(q, lineNo, lineText, byteCol, totalMatches+len(current.Matches), limit)
		current.Matches = append(current.Matches, matches...)
		totalMatches += len(matches)
		if totalMatches >= limit {
			truncated = true
			break
		}
	}
	if !flushCurrent() {
		return
	}
	flush(true)
}

// remoteProjectResultScope narrows the whole-tree remote project search to Fresh's
// project-file scope. Fresh
// lists a git work tree with `git ls-files --cached --others --exclude-standard`: tracked
// files are listed even when they match .gitignore, untracked ignored files are left out,
// and nested repositories/submodules are listed only by name. The scope asks the remote
// host for the same listing once and accepts only results in it. Outside a git work tree,
// or when the listing fails/times out/is truncated, it degrades to hidden-only filtering
// (Fresh's walk leaves hidden entries out) and the search continues, never reading local
// files.
type remoteProjectResultScope struct {
	// listed is true only when the git candidate list is complete and membership is
	// authoritative; tracked entries (including tracked dotfiles) are accepted, untracked
	// dotfiles are not.
	listed    bool
	tracked   map[string]bool
	untracked map[string]bool
	// prefixes are nested repositories/submodules git lists only by name. Their contents
	// are accepted when their own path is not hidden; the inner repository's own ignore
	// rules are not applied by this single git call.
	prefixes []string
}

// newProjectResultScope probes the remote workspace and, in a git work tree, reads the same
// bounded candidate list Fresh's project_files uses. Any failure (missing git, not a
// repository, error, truncation) leaves listed false, so the scope only drops hidden
// entries: an unavailable candidate list must not be guessed at, because guessing with the
// .gitignore rules alone would wrongly drop tracked files that match an ignore pattern.
func (s *remoteSearch) newProjectResultScope(ctx context.Context) *remoteProjectResultScope {
	scope := &remoteProjectResultScope{}
	if s.files == nil {
		return scope
	}
	res, err := s.run(ctx, s.root, []string{"git", "rev-parse", "--is-inside-work-tree"})
	if err != nil || res.Code != 0 || strings.TrimSpace(string(res.Stdout)) != "true" {
		return scope
	}
	files, err := s.run(ctx, s.root, []string{"git", "-c", "core.quotePath=false", "ls-files", "-z", "-s", "--cached", "--others", "--exclude-standard"})
	if err != nil || files.Code != 0 || files.Truncated {
		return scope
	}
	scope.listed = true
	scope.tracked = make(map[string]bool)
	scope.untracked = make(map[string]bool)
	for _, raw := range strings.Split(string(files.Stdout), "\x00") {
		if raw == "" {
			continue
		}
		pathPart, tracked, submodule := parseGitLsFilesEntry(raw)
		if pathPart == "" || pathPart == ".git" || strings.HasPrefix(pathPart, ".git/") {
			continue
		}
		if !utf8.ValidString(pathPart) {
			continue // Fresh drops non-UTF-8 names from project_files too.
		}
		switch {
		case submodule:
			scope.prefixes = append(scope.prefixes, strings.TrimSuffix(pathPart, "/")+"/")
		case tracked:
			scope.tracked[pathPart] = true
		case strings.HasSuffix(pathPart, "/"):
			scope.prefixes = append(scope.prefixes, pathPart)
		default:
			scope.untracked[pathPart] = true
		}
	}
	return scope
}

// parseGitLsFilesEntry splits one NUL field of `git ls-files -z -s`: a tracked entry is
// "<mode> <object> <stage>\t<path>" (mode 160000 is a submodule), an untracked one is the
// bare path (a nested repository path ends with '/').
func parseGitLsFilesEntry(entry string) (pathPart string, tracked, submodule bool) {
	if tab := strings.IndexByte(entry, '\t'); tab >= 0 {
		header, pathPart := entry[:tab], entry[tab+1:]
		fields := strings.Fields(header)
		if len(fields) > 0 && fields[0] == "160000" {
			return pathPart, true, true
		}
		return pathPart, true, false
	}
	return entry, false, false
}

// allows reports whether rel (project-relative, slash separated) is in the grep fallback's
// scope. With a complete git listing, membership decides: a tracked file is always
// accepted (Fresh lists tracked files even when they match .gitignore, and tracked dotfiles
// are listed), an untracked dotfile is dropped, and entries below a nested repository are
// accepted unless the remaining path is hidden. Without a listing only hidden entries are
// dropped. Paths that escape the workspace or are absolute/empty are refused.
func (sc *remoteProjectResultScope) allows(rel string) bool {
	clean := path.Clean(rel)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || strings.HasPrefix(clean, "/") {
		return false
	}
	hidden := func(p string) bool {
		for _, seg := range strings.Split(p, "/") {
			if strings.HasPrefix(seg, ".") {
				return true
			}
		}
		return false
	}
	if !sc.listed {
		return !hidden(clean)
	}
	if sc.tracked[clean] {
		return true
	}
	if sc.untracked[clean] {
		return !hidden(clean)
	}
	for _, prefix := range sc.prefixes {
		if strings.HasPrefix(clean, prefix) {
			return !hidden(strings.TrimPrefix(clean, prefix))
		}
	}
	return false
}

// remoteAllowedFiles turns the panel's project-relative file snapshot into a membership set
// for filtering the remote tool's output, or nil when the snapshot is empty (the panel then
// searches the whole remote tree).
func remoteAllowedFiles(files []string) map[string]bool {
	if len(files) == 0 {
		return nil
	}
	out := make(map[string]bool, len(files))
	for _, f := range files {
		out[strings.TrimPrefix(f, "./")] = true
	}
	return out
}

// probeTool finds which search tool the remote host has: ripgrep first, grep second. The
// probe is one bounded POSIX sh command (constant script, no user text) so both tools and
// their absence are distinguished reliably.
func (s *remoteSearch) probeTool(ctx context.Context) (string, error) {
	probeCtx, stop := context.WithTimeout(ctx, remoteToolProbeTimeout)
	defer stop()
	const script = "if command -v rg >/dev/null 2>&1; then printf rg; elif command -v grep >/dev/null 2>&1; then printf grep; else printf none; fi"
	res, err := s.run(probeCtx, s.root, []string{"sh", "-c", script})
	if err != nil {
		return "", fmt.Errorf("remote search tool probe failed: %w", err)
	}
	switch strings.TrimSpace(string(res.Stdout)) {
	case "rg":
		return "rg", nil
	case "grep":
		return "grep", nil
	default:
		return "", errors.New("no search tool on the remote host: install ripgrep (rg) or grep")
	}
}

// remoteSearchArgv builds the remote search command line: no shell, the query is a single
// -e argument. rg uses its vimgrep output (file:line:byte-column:text); grep falls back to
// -r -n -I and the column is recovered from the line text.
func remoteSearchArgv(tool string, req searchpanel.SearchRequest, scope *remoteProjectResultScope) []string {
	switch tool {
	case "rg":
		argv := []string{"rg", "--vimgrep", "--no-heading", "--color", "never", "--no-messages"}
		if !req.Options.CaseSensitive {
			argv = append(argv, "-i")
		}
		if req.Options.WholeWord {
			argv = append(argv, "-w")
		}
		if !req.Options.Regex {
			argv = append(argv, "-F")
		}
		if scope != nil && scope.listed {
			// The complete Fresh candidate list is available: search ignored and hidden
			// entries too and let the scope decide, so tracked files that match
			// .gitignore are found (rg's default ignore rules would omit them). .git is
			// never in the candidate list. Without a complete listing this stays out:
			// guessing would both miss tracked-ignored files and widen the scope wrongly.
			argv = append(argv, "--no-ignore", "--hidden", "-g", "!.git")
		}
		return append(argv, "-e", req.Query, ".")
	default:
		argv := []string{"grep", "-r", "-n", "-I", "--color=never"}
		for _, dir := range remoteGrepSkipDirs {
			argv = append(argv, "--exclude-dir="+dir)
		}
		if !req.Options.CaseSensitive {
			argv = append(argv, "-i")
		}
		if req.Options.WholeWord {
			argv = append(argv, "-w")
		}
		if req.Options.Regex {
			argv = append(argv, "-E")
		} else {
			argv = append(argv, "-F")
		}
		return append(argv, "-e", req.Query, ".")
	}
}

// remoteGrepLine matches both rg's file:line:byte-column:text and grep's file:line:text,
// where the path may itself hold colons.
var (
	remoteRGLine       = regexp.MustCompile("^(.+?):([0-9]+):([0-9]+):(.*)$")
	remoteGrepTextLine = regexp.MustCompile("^(.+?):([0-9]+):(.*)$")
)

// parseRemoteGrepLine splits one output line. byteCol is 0 for grep (no column printed).
func parseRemoteGrepLine(tool, line string) (rel string, lineNo int, text string, byteCol int, ok bool) {
	if tool == "rg" {
		m := remoteRGLine.FindStringSubmatch(line)
		if m == nil {
			return "", 0, "", 0, false
		}
		n, err := strconv.Atoi(m[2])
		if err != nil {
			return "", 0, "", 0, false
		}
		col, err := strconv.Atoi(m[3])
		if err != nil {
			return "", 0, "", 0, false
		}
		return strings.TrimPrefix(m[1], "./"), n, m[4], col, true
	}
	m := remoteGrepTextLine.FindStringSubmatch(line)
	if m == nil {
		return "", 0, "", 0, false
	}
	n, err := strconv.Atoi(m[2])
	if err != nil {
		return "", 0, "", 0, false
	}
	return strings.TrimPrefix(m[1], "./"), n, m[3], 0, true
}

// remoteLineMatches finds the matches on one output line with the same search engine the
// local project search uses (no IO: the line text came from the remote process). When the
// engine finds nothing (for example a regex the line split broke), a synthetic match at
// the reported byte column keeps the hit visible. It returns at most limit-total matches.
func remoteLineMatches(q search.Query, lineNo int, line string, byteCol, total, limit int) []searchpanel.Match {
	line = strings.TrimRight(line, "\r")
	remaining := limit - total
	if remaining <= 0 {
		return nil
	}
	b := buffer.New(line)
	matches, err := search.Find(b, q)
	var out []searchpanel.Match
	if err == nil {
		for _, m := range matches {
			if len(out) >= remaining {
				break
			}
			out = append(out, searchpanel.Match{
				Line:    lineNo,
				Col:     m.Start.Col + 1,
				Len:     matchRunes(b, m),
				Text:    line,
				EndLine: m.End.Line + 1,
				EndCol:  m.End.Col + 1,
			})
		}
	}
	if len(out) == 0 {
		col := 1
		if byteCol > 1 && byteCol-1 <= len(line) {
			col = utf8.RuneCountInString(line[:byteCol-1]) + 1
		}
		out = append(out, searchpanel.Match{Line: lineNo, Col: col, Len: len([]rune(q.Text)), Text: line})
	}
	return out
}

// run performs one remote command bounded by the search's deadline and output cap.
func (s *remoteSearch) run(ctx context.Context, cwd string, argv []string) (hostos.ProcResult, error) {
	var envEntries []string
	if s.spawnEnv != nil {
		// The active remote environment's captured delta: PATH/tool dirs and the rest of
		// what the recipe changed on the remote host, so project search runs in the same
		// environment as the terminal, LSP and git grep.
		envEntries = s.spawnEnv()
	}
	return s.one.Run(ctx, argv, hostos.ProcOptions{Cwd: cwd, Env: remoteEnvMap(envEntries), MaxOutput: remoteSearchMaxOutput})
}

// installRemoteSearchPorts wires the remote project search, git grep and project-replace
// adapters into the root ports and keeps the local preview out of the remote panel. The
// replace adapter reads and write each file through the authority file system (atomic
// remote writes), never through the local disk; gate is the workspace trust gate.
func installRemoteSearchPorts(ports *tui.RootPorts, one authority.OneShot, files authority.FileSystem, gate func(ctx context.Context, argv []string, cwd string) error, root, label string, spawnEnv func() []string) {
	ports.ProjectSearch = newRemoteProjectSearch(one, root, spawnEnv).withFiles(files)
	ports.GitGrep = newRemoteGitGrep(one, root, spawnEnv)
	ports.ProjectReplace = newRemoteProjectReplacer(files, gate, root, label)
}

// remoteGitGrep runs git grep on the remote host: the repository root is resolved remotely
// (git rev-parse --show-toplevel through the same one-shot surface), then git grep runs
// there. Result paths are absolute remote paths, so selecting a hit opens it through the
// document file access; the local git state is never consulted.
type remoteGitGrep struct {
	one      authority.OneShot
	root     string // absolute remote workspace root
	spawnEnv func() []string
}

// newRemoteGitGrep returns the remote git grep over root.
func newRemoteGitGrep(one authority.OneShot, root string, spawnEnv func() []string) *remoteGitGrep {
	return &remoteGitGrep{one: one, root: root, spawnEnv: spawnEnv}
}

// GitGrep implements searchpanel.GitGrepper.
func (g *remoteGitGrep) GitGrep(req searchpanel.GitRequest) (searchpanel.GitResult, error) {
	var out searchpanel.GitResult
	if req.Query == "" {
		return out, nil
	}
	var extraEnv []string
	if g.spawnEnv != nil {
		extraEnv = g.spawnEnv()
	}
	top, code, err := runGitGrepOneShot(g.one, g.root,
		[]string{"git", "-c", "core.quotePath=false", "rev-parse", "--show-toplevel"}, extraEnv, gitGrepTimeout)
	if err != nil {
		return out, err
	}
	repo := strings.TrimSpace(top)
	if code != 0 || repo == "" {
		return out, fmt.Errorf("not a git repository: %s", g.root)
	}
	limit := req.Limit
	if limit <= 0 {
		limit = 100
	}
	text, code, err := runGitGrepOneShot(g.one, repo, gitGrepArgv(req), extraEnv, gitGrepTimeout)
	if err != nil {
		return out, err
	}
	if code > 1 {
		return out, fmt.Errorf("git grep failed (exit %d)", code)
	}
	if code == 1 || text == "" {
		return out, nil
	}
	return parseGitGrepOutput(text, limit, len([]rune(req.Query)), func(rel string) string {
		return joinRemotePath(repo, rel)
	}), nil
}

// joinRemotePath joins a slash-separated remote relative path onto a remote directory,
// keeping remote (POSIX) separators and an absolute path absolute.
func joinRemotePath(root, rel string) string {
	rel = strings.TrimPrefix(rel, "./")
	if strings.HasPrefix(rel, "/") {
		return rel
	}
	if root == "" || root == "/" {
		return "/" + rel
	}
	return strings.TrimSuffix(root, "/") + "/" + rel
}
