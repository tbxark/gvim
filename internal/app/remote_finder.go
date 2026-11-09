package app

// Remote workspace file finding for the quick-open palette (task-99, task-201): the
// finder lists the remote workspace through the authority. In a git work tree it uses the
// same candidate list as Fresh's project files — one bounded remote
// `git ls-files --cached --others --exclude-standard` run through the one-shot surface —
// so tracked files (including tracked-ignored files) and untracked non-ignored files are
// listed without a recursive ReadDir for every directory and without a client-side ignore
// reader. Outside a work tree, when git cannot list, or when the listing holds nested
// repositories/submodules listed by name only (their contents are not in that output), it
// falls back to the previous recursive authority walk. Nothing here touches the local
// disk: the authority is the only source, and the local project opener is replaced so a
// project switch cannot start a local enumeration.

import (
	"context"
	"errors"
	"fmt"
	"path"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/tbxark/gvim/authority"
	"github.com/tbxark/gvim/finder"
	"github.com/tbxark/gvim/internal/tui"
	"github.com/tbxark/gvim/plugin/hostos"
)

// Bounds of the remote finder's git listing: one probe and one bounded listing, so a
// remote host cannot hang the index or return an unbounded path list. The file cap
// (finder.MaxFiles), depth and cancellation stay in the finder Index; the listing is
// filtered by depth before it is emitted.
const (
	remoteFinderGitProbeTimeout = 5 * time.Second
	remoteFinderGitListTimeout  = 20 * time.Second
	remoteFinderGitMaxBytes     = 8 << 20
)

// remoteFinderFS adapts the authority's remote file system to finder.RemoteFS for the
// fallback walk. The file system is resolved per call, so after a reconnect (a new carrier
// behind the same slot) a finder index restart enumerates the restored host, never the dead
// one.
type remoteFinderFS struct {
	files func() authority.FileSystem
	// ignore filters entries matched by the remote workspace's .gitignore files; the
	// batch lives for one index lifetime (an index restart picks up edited ignore files).
	ignore *ignoreBatch
}

// ReadDir implements finder.RemoteFS.
func (r remoteFinderFS) ReadDir(ctx context.Context, path string, hidden bool) ([]finder.RemoteEntry, error) {
	fs := r.files()
	if fs == nil {
		return nil, fmt.Errorf("remote workspace has no file system")
	}
	entries, err := fs.ReadDir(ctx, path, hidden)
	if err != nil {
		return nil, err
	}
	out := make([]finder.RemoteEntry, 0, len(entries))
	for _, e := range entries {
		isDir := e.Kind == "dir" || (e.Kind == "symlink" && e.TargetKind == "dir")
		if r.ignore != nil && r.ignore.ignored(ctx, path, e.Name, isDir) {
			continue
		}
		out = append(out, finder.RemoteEntry{Name: e.Name, Kind: e.Kind})
	}
	return out, nil
}

// newRemoteFileIndex starts the remote workspace's file index and returns it with the
// root the palette should join relative rows against. A ~ root is expanded remotely
// through the authority's shell (the remote shell expands ~, not the client); when that
// fails the unexpanded root is kept and the enumeration error is surfaced by the index,
// never by falling back to local files. The enumeration function runs once per index run,
// so a Restart/refresh re-reads the remote git listing.
func newRemoteFileIndex(ctx context.Context, slot authority.Slot, root string) (*finder.Index, string) {
	expanded := expandRemoteRoot(ctx, slot, root)
	files := func() authority.FileSystem { return slot.Files() }
	enum := func(ctx context.Context, emit func(string) bool, notify func(string)) error {
		candidates, ok, notice := remoteFinderGitCandidates(ctx, slot, expanded)
		if ok {
			for _, rel := range candidates {
				if err := ctx.Err(); err != nil {
					return err
				}
				if !emit(rel) {
					return nil
				}
			}
			return nil
		}
		if notice != "" {
			notify(notice)
		}
		return finder.EnumerateRemote(ctx, remoteFinderFS{files: files, ignore: newIgnoreBatch(files, expanded)}, expanded, finder.Options{}, emit)
	}
	x := finder.StartFuncNotify(ctx, enum)
	// The literal-path probe is authority-only too: it resolves the candidate through
	// the remote realpath capability and stats it there. There is no local os path, and
	// a backend that cannot prove containment yields no probe result.
	x.SetProbe(newRemotePrefixProbe(slot, expanded))
	return x, expanded
}

// newRemotePrefixProbe is the remote literal-path probe, bound to the expanded
// remote root. It is deliberately the exact-path slice only: Fresh's parent/directory
// branches need a bounded directory reader, while the authority's ReadDir shell glob
// materializes the whole directory before any client-side cap, so they are not used
// here. RealPath is required: without it containment cannot be proven, so the probe
// refuses rather than falling back to the local filesystem. The exact candidate must
// resolve inside the resolved remote root and stat as a regular file.
func newRemotePrefixProbe(slot authority.Slot, root string) finder.ProbeFunc {
	return func(ctx context.Context, query string) ([]string, error) {
		rel, ok := finder.ProbePath(query)
		if !ok {
			return nil, nil
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if slot == nil {
			return nil, nil
		}
		fs := slot.Files()
		if fs == nil {
			return nil, nil
		}
		resolver, ok := authority.RealPathResolver(fs)
		if !ok {
			return nil, nil
		}
		realRoot, err := resolver.RealPath(ctx, root)
		if err != nil || !strings.HasPrefix(realRoot, "/") {
			return nil, nil
		}
		realCandidate, err := resolver.RealPath(ctx, remoteFinderJoin(root, rel))
		if err != nil {
			return nil, nil
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !remoteFinderContained(realRoot, realCandidate) {
			return nil, nil
		}
		st, err := fs.Stat(ctx, realCandidate)
		if err != nil || st.Kind != "file" {
			return nil, nil
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return []string{rel}, nil
	}
}

// remoteFinderJoin joins a slash-separated relative path to a remote root.
func remoteFinderJoin(root, rel string) string {
	return path.Join(root, rel)
}

// remoteFinderContained reports whether the resolved remote candidate is below the
// resolved remote root. Both paths are absolute POSIX paths from RealPath.
func remoteFinderContained(root, candidate string) bool {
	if !strings.HasPrefix(root, "/") || !strings.HasPrefix(candidate, "/") {
		return false
	}
	root = path.Clean(root)
	candidate = path.Clean(candidate)
	return candidate == root || strings.HasPrefix(candidate, root+"/")
}

// remoteFinderGitCandidates returns the remote git work tree's candidate files, or ok
// false when the recursive walk must run instead (not a work tree, git cannot list, the
// answer was truncated, or the listing contains nested repositories/submodules whose
// contents git names only). A failed probe/listing carries a non-fatal notice the finder
// exposes to the palette, so the fallback is not silent; a plain non-git workspace is not
// a degradation and has no notice. The paths are filtered like the quick-open walk: no
// .git internals, no hidden segment, no skip directory, no non-UTF-8 name, no invalid path
// segment and no depth beyond the finder's remote depth bound. The file cap and
// cancellation stay in the finder Index.
func remoteFinderGitCandidates(ctx context.Context, slot authority.Slot, root string) ([]string, bool, string) {
	var (
		gitUnavailable = appText("gvim.notice.remote_git_unavailable", "Remote quick-open: git could not run on the remote host; listing the workspace with a directory walk (ignore filtering follows the walk).")
		gitListFailed  = appText("gvim.notice.remote_git_list_failed", "Remote quick-open: the remote git file list failed or was truncated; listing the workspace with a directory walk (ignore filtering may differ).")
	)
	if slot == nil {
		return nil, false, ""
	}
	one := slot.OneShot()
	if one == nil {
		return nil, false, ""
	}
	probeCtx, stop := context.WithTimeout(ctx, remoteFinderGitProbeTimeout)
	probe, err := one.Run(probeCtx, []string{"git", "rev-parse", "--is-inside-work-tree"}, hostos.ProcOptions{Cwd: root})
	stop()
	if err != nil {
		if ctx.Err() != nil {
			return nil, false, ""
		}
		return nil, false, gitUnavailable
	}
	if probe.Code != 0 || strings.TrimSpace(string(probe.Stdout)) != "true" {
		return nil, false, "" // not a git work tree: the Fresh-style walk is the scope
	}
	listCtx, stop := context.WithTimeout(ctx, remoteFinderGitListTimeout)
	list, err := one.Run(listCtx, []string{"git", "-c", "core.quotePath=false", "ls-files", "-z", "-s", "--cached", "--others", "--exclude-standard"}, hostos.ProcOptions{Cwd: root, MaxOutput: remoteFinderGitMaxBytes})
	stop()
	if err != nil || list.Code != 0 || list.Truncated {
		if ctx.Err() != nil {
			return nil, false, ""
		}
		return nil, false, gitListFailed
	}
	var out []string
	for _, raw := range strings.Split(string(list.Stdout), "\x00") {
		if raw == "" {
			continue
		}
		rel, _, submodule := parseGitLsFilesEntry(raw)
		if submodule || (!strings.ContainsRune(raw, '\t') && strings.HasSuffix(rel, "/")) {
			// A nested repository/submodule is listed by name only: its contents are not
			// in this output, so the recursive walk must take over rather than silently
			// dropping them. This is not a failure, so it has no degradation notice.
			return nil, false, ""
		}
		if remoteFinderCandidate(rel) {
			out = append(out, rel)
		}
	}
	return out, true, ""
}

// remoteFinderCandidate accepts one slash-relative git listing path in the quick-open
// scope: it rejects .git internals, hidden segments, skip directories, invalid names,
// non-UTF-8 names and paths deeper than the finder's remote walk bound.
func remoteFinderCandidate(rel string) bool {
	if rel == "" || rel == ".git" || strings.HasPrefix(rel, ".git/") || !utf8.ValidString(rel) {
		return false
	}
	segs := strings.Split(rel, "/")
	if len(segs) > finder.MaxRemoteDepth {
		return false
	}
	for i, seg := range segs {
		if seg == "" || seg == "." || seg == ".." || strings.ContainsAny(seg, "\\\x00") {
			return false
		}
		if strings.HasPrefix(seg, ".") {
			return false
		}
		if i < len(segs)-1 && remoteFinderSkipDir(seg) {
			return false
		}
	}
	return true
}

// remoteFinderSkipDir is the walk's skip-dir rule, used on the git listing's intermediate
// segments (a file whose own name is "target" is not skipped).
func remoteFinderSkipDir(name string) bool {
	for _, skip := range remoteGrepSkipDirs {
		if name == skip {
			return true
		}
	}
	return false
}

// expandRemoteRoot turns a remote ~/... or ~user/... workspace root into the remote
// absolute path, using one bounded remote shell command. Other roots are returned as is.
func expandRemoteRoot(ctx context.Context, slot authority.Slot, root string) string {
	if root == "" || !strings.HasPrefix(root, "~") {
		return root
	}
	script := "cd " + authority.QuoteDir(root) + " && pwd"
	res, err := slot.OneShot().Run(ctx, []string{"sh", "-c", script}, hostos.ProcOptions{})
	if err != nil || res.Code != 0 {
		return root
	}
	if pwd := strings.TrimSpace(string(res.Stdout)); pwd != "" {
		return pwd
	}
	return root
}

// remoteProjects refuses a project switch in a remote workspace: the local project
// opener would build a local tree and start a local file index at a remote path.
type remoteProjects struct{}

// Open implements tui.Projects.
func (remoteProjects) Open(string) (tui.Project, error) {
	return tui.Project{}, errors.New("project switching is not available in a remote workspace")
}

// installRemoteDataPorts installs the authority-backed data ports of a remote workspace:
// the git-status marks for the remote explorer (returned so the reconnect hook can
// invalidate it) and the quick-open preview reader. Both read only through the slot.
func installRemoteDataPorts(ports *tui.RootPorts, slot authority.Slot, root string, spawnEnv func() []string) *remoteGitStatus {
	git := newRemoteGitStatus(slot.OneShot(), root, spawnEnv)
	ports.Git = git
	ports.Preview = newRemotePreviewer(slot)
	return git
}

// remoteRootPorts turns off the root ports that read the local workspace in a remote
// launch: the local project opener (a switch would enumerate the local disk), the local
// file preview (remote rows must not read the local file system) and the local git status
// adapter (remote paths would be looked up in local repositories).
func remoteRootPorts(ports *tui.RootPorts) {
	ports.Projects = remoteProjects{}
	// Preview and Git are cleared here; the remote branch installs the authority-backed
	// previewer and git-status port after this call (the local adapters must never read
	// local files for remote paths).
	ports.Preview = nil
	ports.Git = nil
	// The project search/replace and git grep ports read and write the LOCAL file system
	// (git grep may run through the authority, but with the local tree root), so a remote
	// launch must not offer them: the TUI additionally refuses these entry points with a
	// remote-specific notice, and these nil ports are the second line of defense.
	ports.ProjectSearch = nil
	ports.ProjectReplace = nil
	ports.GitGrep = nil
	// Live Grep must not keep the local Grepper either: the remote branch installs the
	// authority-backed Grep port after this call, so the local file reader can never see a
	// remote-looking row in the meantime.
	ports.Grep = nil
}
