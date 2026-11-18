package app

import (
	"fmt"
	"path/filepath"
	"time"
	"unicode/utf8"

	"github.com/tbxark/gvim/authority"
	"github.com/tbxark/gvim/gitstate"
	"github.com/tbxark/gvim/internal/tui/searchpanel"
	"github.com/tbxark/gvim/shellcmd"
)

// gitGrep runs git grep in the work tree of the workspace root for the panel's Git Grep
// sheet. It passes git its arguments directly, without a shell, so a query with spaces
// and quotes reaches git as it is.
type gitGrep struct {
	// spawnEnv is the active project environment's set-only overlay for the git
	// process; nil applies the inherited environment.
	spawnEnv func() []string
	// oneShot is the workspace authority's one-shot surface: git grep runs through
	// it, so a remote workspace greps the remote work tree. Nil keeps shellcmd.
	oneShot authority.OneShot
}

// gitGrepTimeout bounds one git grep run.
const gitGrepTimeout = 20 * time.Second

// GitGrep searches the tracked files of the work tree that holds req.Root. git grep's
// exit codes are honored: 0 means matches, 1 means none (a successful search), and 2 or
// more is an error. Only the workspace root's work tree is searched; a root outside a
// repository is an error.
func (g gitGrep) GitGrep(req searchpanel.GitRequest) (searchpanel.GitResult, error) {
	var out searchpanel.GitResult
	if req.Query == "" {
		return out, nil
	}
	root, ok := gitstate.FindRoot(req.Root)
	if !ok {
		return out, fmt.Errorf("not a git repository: %s", req.Root)
	}
	limit := req.Limit
	if limit <= 0 {
		limit = 100
	}
	argv := gitGrepNullArgv(req)
	var extraEnv []string
	if g.spawnEnv != nil {
		extraEnv = g.spawnEnv()
	}
	var text string
	var code int
	var err error
	if g.oneShot != nil {
		text, code, err = runGitGrepOneShot(g.oneShot, root, argv, extraEnv, gitGrepTimeout)
	} else {
		text, code, err = shellcmd.RunArgsEnv(root, argv, "", gitGrepTimeout, extraEnv)
	}
	if err != nil {
		return out, err
	}
	if code > 1 {
		return out, fmt.Errorf("git grep failed (exit %d)", code)
	}
	if code == 1 || text == "" {
		return out, nil
	}
	return parseGitGrepNullOutput(text, limit, utf8.RuneCountInString(cleanRemoteDisplayText(req.Query)), func(rel string) string {
		return filepath.Join(root, filepath.FromSlash(rel))
	}), nil
}

// gitGrepArgv builds the git grep command line for req: arguments are passed as argv (no
// shell), pathspecs go after "--" so they are never read as options.
func gitGrepArgv(req searchpanel.GitRequest) []string {
	argv := []string{"git", "-c", "core.quotePath=false", "grep", "-n", "--column", "-I"}
	if !req.CaseSensitive {
		argv = append(argv, "-i")
	}
	argv = append(argv, "-e", req.Query)
	if len(req.Pathspecs) > 0 {
		argv = append(argv, "--")
		argv = append(argv, req.Pathspecs...)
	}
	return argv
}
