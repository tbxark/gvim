package app

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

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

// gitGrepLine is one line of git grep's output with -n --column: path, line, column and
// the line's text, where the path may itself hold colons.
var gitGrepLine = regexp.MustCompile(`^(.+?):([0-9]+):([0-9]+):(.*)$`)

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
	argv := gitGrepArgv(req)
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
	return parseGitGrepOutput(text, limit, len([]rune(req.Query)), func(rel string) string {
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

// parseGitGrepOutput parses git grep -n --column output into whole-file matches, at most
// limit matches. abs maps a repo-relative path (as git prints it) to the path the editor
// opens; Name stays the repo-relative path. Both the local and remote adapters share it.
func parseGitGrepOutput(text string, limit, matchLen int, abs func(rel string) string) searchpanel.GitResult {
	var out searchpanel.GitResult
	byPath := map[string]int{}
	total := 0
	for _, line := range strings.Split(text, "\n") {
		if total >= limit {
			break
		}
		if line == "" {
			continue
		}
		m := gitGrepLine.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		lineNo, errLine := strconv.Atoi(m[2])
		col, errCol := strconv.Atoi(m[3])
		if errLine != nil || errCol != nil {
			continue
		}
		rel := m[1]
		idx, ok := byPath[rel]
		if !ok {
			idx = len(out.Files)
			byPath[rel] = idx
			out.Files = append(out.Files, searchpanel.FileMatches{Path: abs(rel), Name: rel})
		}
		content := strings.TrimRight(m[4], "\r")
		out.Files[idx].Matches = append(out.Files[idx].Matches, searchpanel.Match{
			Line: lineNo,
			Col:  col,
			Len:  matchLen,
			Text: content,
		})
		total++
	}
	return out
}
