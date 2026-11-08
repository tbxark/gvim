package app

// Remote workspace file finding for the quick-open palette (task-99): the finder walks
// the authority file system of the remote workspace (authority.Files()), streamed into
// the ordinary finder.Index. Nothing here touches the local disk: the adapter is the only
// source, and the local project opener is replaced so a project switch cannot start a
// local enumeration.

import (
	"context"
	"errors"
	"strings"

	"github.com/tbxark/gvim/authority"
	"github.com/tbxark/gvim/finder"
	"github.com/tbxark/gvim/internal/tui"
	"github.com/tbxark/gvim/plugin/hostos"
)

// remoteFinderFS adapts the authority's remote file system to finder.RemoteFS.
type remoteFinderFS struct{ fs authority.FileSystem }

// ReadDir implements finder.RemoteFS.
func (r remoteFinderFS) ReadDir(ctx context.Context, path string, hidden bool) ([]finder.RemoteEntry, error) {
	entries, err := r.fs.ReadDir(ctx, path, hidden)
	if err != nil {
		return nil, err
	}
	out := make([]finder.RemoteEntry, 0, len(entries))
	for _, e := range entries {
		out = append(out, finder.RemoteEntry{Name: e.Name, Kind: e.Kind})
	}
	return out, nil
}

// newRemoteFileIndex starts the remote workspace's file index and returns it with the
// root the palette should join relative rows against. A ~ root is expanded remotely
// through the authority's shell (the remote shell expands ~, not the client); when that
// fails the unexpanded root is kept and the enumeration error is surfaced by the index,
// never by falling back to local files.
func newRemoteFileIndex(ctx context.Context, slot authority.Slot, root string) (*finder.Index, string) {
	expanded := expandRemoteRoot(ctx, slot, root)
	return finder.StartRemote(ctx, remoteFinderFS{fs: slot.Files()}, expanded), expanded
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

// remoteGitStatus replaces the local git status adapter in a remote workspace: the local
// adapter would run git against a local repository for remote-looking paths (and show
// the local work tree's letters). Remote git status over the authority is not wired yet,
// so the remote session deliberately shows no git marks.
type remoteGitStatus struct{}

// Marks implements tui.GitStatus without touching the local disk.
func (remoteGitStatus) Marks(string) (map[string]rune, error) { return nil, nil }

// remoteRootPorts turns off the root ports that read the local workspace in a remote
// launch: the local project opener (a switch would enumerate the local disk), the local
// file preview (remote rows must not read the local file system) and the local git status
// adapter (remote paths would be looked up in local repositories).
func remoteRootPorts(ports *tui.RootPorts) {
	ports.Projects = remoteProjects{}
	ports.Preview = nil
	ports.Git = remoteGitStatus{}
	// The project search/replace and git grep ports read and write the LOCAL file system
	// (git grep may run through the authority, but with the local tree root), so a remote
	// launch must not offer them: the TUI additionally refuses these entry points with a
	// remote-specific notice, and these nil ports are the second line of defense.
	ports.ProjectSearch = nil
	ports.ProjectReplace = nil
	ports.GitGrep = nil
}
