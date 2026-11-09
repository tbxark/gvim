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

// remoteNestedMaxRepos bounds how many nested repositories/submodules one search resolves
// with their own bounded git listings. More than this keeps the widened walk fallback (with
// the non-fatal notice), never a guessed/incomplete candidate set.
const remoteNestedMaxRepos = 8

// remoteSearch is the project-wide search over a remote workspace: a searchpanel.Searcher
// and BatchSearcher that runs ripgrep (or grep) through the authority's one-shot surface
// with the remote workspace as cwd. The local project search is never consulted.
type remoteSearch struct {
	one      authority.OneShot
	files    authority.FileSystem // for the preview head; nil in tests that do not preview
	root     string               // absolute remote workspace root
	timeout  time.Duration        // bounds one search run (remoteSearchTimeout unless a test says otherwise)
	spawnEnv func() []string
	// previewCache caches the syntax highlighters of preview windows (local and remote
	// heads); syntax colour is CPU-only, bounded by the shared preview caps.
	previewCache previewHighlighterCache
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
		if batch.Notice != "" {
			out.Notice = batch.Notice
		}
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
	// Fresh's project-file scope is the search scope. When the complete git candidate
	// list is available, pass its files explicitly to the tool in bounded batches: the
	// tool never scans ignored or hidden trees (so ignored matches cannot consume the
	// output cap before candidates) and the existing regex engine is unchanged. When the
	// list is unavailable, or too large to pass explicitly, fall back to the whole-tree
	// tool with the candidate filter and carry a non-fatal degradation notice to the panel.
	var scope *remoteProjectResultScope
	if allowed == nil {
		scope = s.newProjectResultScope(ctx)
	}
	widen := false
	var res hostos.ProcResult
	if scope != nil && scope.listed {
		batches, ok := batchSearchPaths(scope.paths)
		switch {
		case len(scope.prefixes) > 0:
			// The nested repositories/submodules that could not be resolved into their own
			// candidate lists are searched in place: the widened run's roots are exactly
			// those prefixes, not the whole tree, so unrelated ignored entries cannot
			// consume the output cap before them. The prefix filter still decides what is
			// in scope; a cap hit is reported as incomplete below.
			scope.notice = "remote search scanned the nested repositories/submodules in place because their candidate lists were unavailable; ignored entries may be scanned and results may be capped"
			widen = true
			res, err = s.run(ctx, s.root, remoteSearchArgv(tool, req, widen, scope.prefixes))
		case !ok:
			scope.notice = "remote search scanned ignored entries because the candidate file list was too large to pass explicitly; results may be capped before every candidate file is searched"
			widen = true
			res, err = s.run(ctx, s.root, remoteSearchArgv(tool, req, widen, nil))
		case len(scope.paths) == 0:
			// An empty candidate list has no files to search: no tool run and no paths.
		default:
			res, err = s.runBatched(ctx, tool, req, batches)
		}
	} else {
		res, err = s.run(ctx, s.root, remoteSearchArgv(tool, req, widen, nil))
	}
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
	if widen && res.Truncated && scope != nil {
		// A widened run consumed the remote output cap: candidate files beyond the cap
		// were neither searched nor reported, so the result is explicitly incomplete. The
		// generic degradation notice is kept, with the cap consequence spelled out.
		capNotice := "remote search result is incomplete: the widened fallback scan hit the remote output cap before all candidate files were reported; narrow the scope or refine the query"
		if scope.notice != "" {
			scope.notice += "; " + capNotice
		} else {
			scope.notice = capNotice
		}
	}
	pending := make([]searchpanel.FileMatches, 0, searchBatchFiles)
	pendingMatches := 0
	totalMatches := 0
	flush := func(done bool) bool {
		batch := searchpanel.SearchBatch{Files: pending, Truncated: truncated, Done: done}
		if done && scope != nil {
			batch.Notice = scope.notice
		}
		if !send(batch) {
			return false
		}
		pending, pendingMatches = nil, 0
		return true
	}
	// Open buffers are searched from the editor's snapshot, exactly as the local AllFiles
	// scope does and as Fresh's project_files/open-buffer search does: the remote tool's
	// disk line must not win for a file whose buffer text (possibly unsaved) is already in
	// hand. The current buffer's snapshot goes first; the other open buffers follow the
	// tool results. No remote read is added for this path.
	openSources, openByRel, openOrder := remoteOpenSources(req, s.root, allowed)
	emittedOpen := map[string]bool{}
	addOpenSource := func(src searchpanel.SourceFile, rel string) bool {
		emittedOpen[rel] = true
		if cancelled() || totalMatches >= limit {
			return true
		}
		matches := remoteSourceMatches(src.Text, q)
		if len(matches) == 0 {
			return true
		}
		fm := searchpanel.FileMatches{Path: joinRemotePath(s.root, rel), Name: rel, Hash: hashText(src.Text), Dirty: src.Dirty}
		for _, m := range matches {
			fm.Matches = append(fm.Matches, m)
			if totalMatches+len(fm.Matches) >= limit {
				truncated = true
				break
			}
		}
		if len(fm.Matches) > 0 {
			totalMatches += len(fm.Matches)
			pending = append(pending, fm)
			pendingMatches += len(fm.Matches)
		}
		if len(pending) >= searchBatchFiles || pendingMatches >= searchBatchMatches {
			return flush(false)
		}
		return true
	}
	currentRel := ""
	if req.Current != nil {
		if rel, ok := remoteRelPath(req.Current.Path, s.root); ok {
			if src, in := openSources[rel]; in {
				currentRel = rel
				if !addOpenSource(src, rel) {
					return
				}
			}
		}
	}
	var current *searchpanel.FileMatches
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
		if _, isOpen := openByRel[rel]; isOpen {
			// The buffer snapshot above owns this file; the disk line is stale or would
			// duplicate it.
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
	for _, rel := range openOrder {
		if emittedOpen[rel] || totalMatches >= limit {
			continue
		}
		if !addOpenSource(openSources[rel], rel) {
			return
		}
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
	// prefixes are nested repositories/submodules git lists only by name. When every
	// prefix resolves to its own complete bounded git listing (nestedListed), the prefix
	// list is replaced by those explicit candidate files and the inner repository's
	// .gitignore applies. Otherwise the prefixes are a walk fallback (hidden/IGNORED_DIRS
	// only, inner .gitignore not applied) with a degradation notice.
	prefixes []string
	// nestedListed is true when all prefixes were resolved into complete per-repository
	// candidate lists: no prefix walk remains, so the explicit batch path is used.
	nestedListed bool
	// paths is the ordered candidate file list (tracked files, then untracked non-hidden
	// files) used to pass explicit paths to the search tool in bounded batches.
	paths []string
	// gitRepo reports that the workspace is a git work tree (the candidate list was the
	// expected scope); notice is the non-fatal degradation when that list is missing or
	// the batches would be too many.
	gitRepo bool
	notice  string
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
	if err != nil {
		scope.notice = "remote ignore scope unavailable: git could not run on the remote host; ignored files may be searched and tracked files that match .gitignore may be omitted"
		return scope
	}
	if res.Code != 0 {
		if strings.Contains(strings.ToLower(string(res.Stderr)), "not a git repository") {
			return scope // a plain non-git workspace: Fresh's walk applies, no degradation
		}
		scope.notice = "remote ignore scope unavailable: git could not run on the remote host; ignored files may be searched and tracked files that match .gitignore may be omitted"
		return scope
	}
	if strings.TrimSpace(string(res.Stdout)) != "true" {
		return scope
	}
	scope.gitRepo = true
	files, err := s.run(ctx, s.root, []string{"git", "-c", "core.quotePath=false", "ls-files", "-z", "-s", "--cached", "--others", "--exclude-standard"})
	if err != nil || files.Code != 0 || files.Truncated {
		scope.notice = "remote ignore scope unavailable: the remote git candidate listing failed, timed out or truncated; ignored files may be searched and tracked files that match .gitignore may be omitted"
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
			if walkedRemotePathAllowed(strings.TrimSuffix(pathPart, "/")) {
				scope.prefixes = append(scope.prefixes, strings.TrimSuffix(pathPart, "/")+"/")
			}
		case tracked:
			scope.tracked[pathPart] = true
			scope.paths = append(scope.paths, pathPart)
		case strings.HasSuffix(pathPart, "/"):
			if walkedRemotePathAllowed(strings.TrimSuffix(pathPart, "/")) {
				scope.prefixes = append(scope.prefixes, pathPart)
			}
		default:
			scope.untracked[pathPart] = true
			if !hiddenRemotePath(pathPart) {
				scope.paths = append(scope.paths, pathPart)
			}
		}
	}
	s.resolveNestedPrefixes(ctx, scope)
	return scope
}

// resolveNestedPrefixes replaces each nested repository/submodule prefix with the
// candidates of its own bounded git ls-files --cached --others --exclude-standard listing,
// so its .gitignore is applied exactly as git defines it (tracked-ignored files included,
// untracked ignored files left out). This is all-or-nothing: more than
// remoteNestedMaxRepos prefixes, a failed/truncated/non-UTF-8 listing, a path that escapes
// the workspace, or another nested repository inside one of them leaves every prefix in
// place so the existing widened walk fallback (with its notice) runs instead -- an
// incomplete nested candidate set is never presented as complete. Each command is bounded
// by the search context, so cancellation/timeouts stop between prefixes and inside a
// command.
func (s *remoteSearch) resolveNestedPrefixes(ctx context.Context, scope *remoteProjectResultScope) {
	if len(scope.prefixes) == 0 || len(scope.prefixes) > remoteNestedMaxRepos {
		return
	}
	type nestedListing struct {
		prefix    string
		paths     []string
		tracked   map[string]bool
		untracked map[string]bool
	}
	var listings []nestedListing
	for _, prefix := range scope.prefixes {
		if err := ctx.Err(); err != nil {
			return
		}
		relPrefix := strings.TrimSuffix(prefix, "/")
		res, err := s.run(ctx, s.root, []string{"git", "-C", relPrefix, "-c", "core.quotePath=false", "ls-files", "-z", "-s", "--cached", "--others", "--exclude-standard"})
		if err != nil || res.Code != 0 || res.Truncated {
			return
		}
		entry := nestedListing{prefix: prefix, tracked: map[string]bool{}, untracked: map[string]bool{}}
		for _, raw := range strings.Split(string(res.Stdout), "\x00") {
			if raw == "" {
				continue
			}
			pathPart, tracked, submodule := parseGitLsFilesEntry(raw)
			if pathPart == "" || pathPart == ".git" || strings.HasPrefix(pathPart, ".git/") || submodule || strings.HasSuffix(pathPart, "/") {
				// Another nested repository/submodule inside this one cannot be resolved by
				// this slice; keep the whole fallback instead of dropping its contents.
				return
			}
			if !utf8.ValidString(pathPart) {
				return
			}
			full := path.Join(prefix, pathPart)
			if !strings.HasPrefix(full, prefix) {
				return
			}
			if tracked {
				entry.tracked[full] = true
				entry.paths = append(entry.paths, full)
			} else {
				entry.untracked[full] = true
				if !hiddenRemotePath(pathPart) {
					entry.paths = append(entry.paths, full)
				}
			}
		}
		listings = append(listings, entry)
	}
	// Every prefix resolved completely: swap the walk prefixes for explicit candidates.
	scope.prefixes = nil
	scope.nestedListed = true
	for _, entry := range listings {
		for full := range entry.tracked {
			scope.tracked[full] = true
		}
		for full := range entry.untracked {
			scope.untracked[full] = true
		}
		scope.paths = append(scope.paths, entry.paths...)
	}
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
	if !sc.listed {
		// A walk scope (no complete git candidate list): Fresh's walk never enters a
		// hidden directory or one of the IGNORED_DIRS (node_modules, target, ...).
		return walkedRemotePathAllowed(clean)
	}
	if sc.tracked[clean] {
		return true
	}
	if sc.untracked[clean] {
		return !hiddenRemotePath(clean)
	}
	for _, prefix := range sc.prefixes {
		if strings.HasPrefix(clean, prefix) {
			// The nested repository is walked by Fresh's walk rules: hidden directories
			// and IGNORED_DIRS inside it are left out (its own .gitignore still cannot be
			// applied by this single listing; that remains a recorded residual).
			return walkedRemotePathAllowed(strings.TrimPrefix(clean, prefix))
		}
	}
	return false
}

// hiddenRemotePath reports a dot-named path segment (the hidden-file rule).
func hiddenRemotePath(p string) bool {
	for _, seg := range strings.Split(p, "/") {
		if strings.HasPrefix(seg, ".") {
			return true
		}
	}
	return false
}

// skippedRemotePath reports a path segment Fresh's directory walk never enters
// (IGNORED_DIRS: .git, node_modules, target, __pycache__, .hg, .svn, .DS_Store).
func skippedRemotePath(p string) bool {
	for _, seg := range strings.Split(p, "/") {
		for _, skip := range remoteGrepSkipDirs {
			if seg == skip {
				return true
			}
		}
	}
	return false
}

// walkedRemotePathAllowed is the Fresh walk's project-file scope for a path reached by
// walking (no complete git candidate membership): not hidden and not inside a skip dir.
func walkedRemotePathAllowed(p string) bool {
	return !hiddenRemotePath(p) && !skippedRemotePath(p)
}

// remoteRelPath turns an absolute remote buffer path into a project-relative slash path,
// rejecting paths outside root and the root itself.
func remoteRelPath(abs, root string) (string, bool) {
	clean := path.Clean(abs)
	cleanRoot := path.Clean(root)
	if cleanRoot == "/" {
		if !strings.HasPrefix(clean, "/") || clean == "/" {
			return "", false
		}
		return strings.TrimPrefix(clean, "/"), true
	}
	if clean == cleanRoot || !strings.HasPrefix(clean, cleanRoot+"/") {
		return "", false
	}
	return strings.TrimPrefix(clean, cleanRoot+"/"), true
}

// remoteOpenSources collects the open-buffer snapshots that belong to the AllFiles scope:
// an under-root path that the panel's file snapshot (already glob-filtered) contains when
// there is one, or any under-root open buffer when the file index is not ready. It returns
// the snapshots by project-relative path, that map, and the display order.
func remoteOpenSources(req searchpanel.SearchRequest, root string, allowed map[string]bool) (map[string]searchpanel.SourceFile, map[string]searchpanel.SourceFile, []string) {
	byRel := make(map[string]searchpanel.SourceFile, len(req.OpenFiles))
	var order []string
	add := func(src searchpanel.SourceFile) {
		rel, ok := remoteRelPath(src.Path, root)
		if !ok {
			return
		}
		if allowed != nil && !allowed[rel] {
			return
		}
		if _, seen := byRel[rel]; seen {
			return
		}
		byRel[rel] = src
		order = append(order, rel)
	}
	for _, src := range req.OpenFiles {
		add(src)
	}
	if req.Current != nil {
		add(*req.Current)
	}
	return byRel, byRel, order
}

// remoteSourceMatches finds a buffer snapshot's matches with the same local search engine as
// the local project search; it does no IO and does not consult the remote file.
func remoteSourceMatches(text string, q search.Query) []searchpanel.Match {
	b := buffer.New(text)
	found, err := search.Find(b, q)
	if err != nil {
		return nil
	}
	out := make([]searchpanel.Match, 0, len(found))
	for _, m := range found {
		out = append(out, searchpanel.Match{
			Line:    m.Start.Line + 1,
			Col:     m.Start.Col + 1,
			Len:     matchRunes(b, m),
			Text:    strings.TrimRight(b.Line(m.Start.Line), "\r"),
			EndLine: m.End.Line + 1,
			EndCol:  m.End.Col + 1,
		})
	}
	return out
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

const (
	// searchScopeBatchFiles caps how many candidate paths one tool command receives.
	searchScopeBatchFiles = 400
	// searchScopeBatchBytes caps the path bytes of one command, so the argv stays well
	// under the remote command-size limit even with long paths.
	searchScopeBatchBytes = 48 << 10
	// searchScopeMaxBatches bounds the tool commands one search may start. A candidate
	// list needing more falls back to the whole-tree scan, with a degradation notice.
	searchScopeMaxBatches = 64
)

// batchSearchPaths splits the ordered candidate paths into bounded batches. It returns
// false when the list needs more than searchScopeMaxBatches commands: the caller then keeps
// the whole-tree fallback and tells the user that ignored entries were scanned.
func batchSearchPaths(paths []string) ([][]string, bool) {
	var batches [][]string
	var current []string
	bytes := 0
	seen := make(map[string]bool, len(paths))
	for _, p := range paths {
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		if len(current) > 0 && (len(current) >= searchScopeBatchFiles || bytes+len(p)+1 > searchScopeBatchBytes) {
			batches = append(batches, current)
			current, bytes = nil, 0
			if len(batches) >= searchScopeMaxBatches {
				return nil, false
			}
		}
		current = append(current, p)
		bytes += len(p) + 1
	}
	if len(current) > 0 {
		batches = append(batches, current)
	}
	if len(batches) > searchScopeMaxBatches {
		return nil, false
	}
	return batches, true
}

// runBatched runs the search tool once per candidate batch and merges the output. The
// tool never scans ignored/hidden trees this way; the caller's context bounds every
// command (so timeout and cancellation stop between batches too), and the merged output is
// bounded by remoteSearchMaxOutput with truncation flagged. A batch failure is an error
// (never a silent partial success).
func (s *remoteSearch) runBatched(ctx context.Context, tool string, req searchpanel.SearchRequest, batches [][]string) (hostos.ProcResult, error) {
	var out hostos.ProcResult
	var stdout []byte
	for _, batch := range batches {
		if err := ctx.Err(); err != nil {
			return hostos.ProcResult{}, err
		}
		res, err := s.run(ctx, s.root, remoteSearchArgvWithPaths(tool, req, batch))
		if err != nil {
			return hostos.ProcResult{}, err
		}
		if res.Code > 1 {
			return hostos.ProcResult{}, fmt.Errorf("%s failed on the remote host (exit %d)", tool, res.Code)
		}
		out.Truncated = out.Truncated || res.Truncated
		if room := remoteSearchMaxOutput - len(stdout); room > 0 {
			add := res.Stdout
			if len(add) > room {
				add = add[:room]
				out.Truncated = true
			}
			stdout = append(stdout, add...)
			if len(stdout) > 0 && stdout[len(stdout)-1] != '\n' {
				stdout = append(stdout, '\n')
			}
		} else {
			out.Truncated = true
			break
		}
	}
	out.Stdout = stdout
	return out, nil
}

// remoteSearchArgvWithPaths builds one tool command over explicit candidate paths. The
// paths follow a "--" so a name starting with a dash is a path, not an option; the query
// stays a single -e argument (no shell).
func remoteSearchArgvWithPaths(tool string, req searchpanel.SearchRequest, paths []string) []string {
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
		argv = append(argv, "-e", req.Query, "--")
		return append(argv, paths...)
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
		argv = append(argv, "-e", req.Query, "--")
		return append(argv, paths...)
	}
}

// remoteSearchArgv builds the remote search command line: no shell, the query is a single
// -e argument.
// A widened fallback run (widen) scans ignored and hidden entries for the caller's
// candidate filter; roots narrows it to the unresolved nested-repository prefixes when
// there are any, otherwise the whole tree is scanned. A cap hit is made explicit as an
// incomplete result by the caller. The normal path passes explicit candidate files. rg
// uses its vimgrep output (file:line:byte-column:text); grep falls back to -r -n -I and the
// column is recovered from the line text.
func remoteSearchArgv(tool string, req searchpanel.SearchRequest, widen bool, roots []string) []string {
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
		if widen {
			// Fallback only (the candidate list was too large to pass explicitly): scan
			// the whole tree and let the candidate filter decide. The panel carries a
			// notice that ignored entries were scanned.
			argv = append(argv, "--no-ignore", "--hidden", "-g", "!.git")
		}
		argv = append(argv, "-e", req.Query)
		return remoteSearchRoots(argv, roots)
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
		argv = append(argv, "-e", req.Query)
		return remoteSearchRoots(argv, roots)
	}
}

// remoteSearchRoots appends the widened run's roots after -- when there are narrower
// roots (nested repositories searched in place); otherwise the caller leaves the "."
// default so the ordinary whole-tree fallback is unchanged.
func remoteSearchRoots(argv []string, roots []string) []string {
	if len(roots) == 0 {
		return append(argv, ".")
	}
	argv = append(argv, "--")
	for _, root := range roots {
		argv = append(argv, strings.TrimSuffix(root, "/"))
	}
	return argv
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
