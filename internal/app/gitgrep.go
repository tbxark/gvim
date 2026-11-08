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
	argv := []string{"git", "-c", "core.quotePath=false", "grep", "-n", "--column", "-I"}
	if !req.CaseSensitive {
		argv = append(argv, "-i")
	}
	argv = append(argv, "-e", req.Query)
	if len(req.Pathspecs) > 0 {
		// The patterns are git pathspecs, so they go after the separator and are never
		// read as options.
		argv = append(argv, "--")
		argv = append(argv, req.Pathspecs...)
	}
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
	byPath := map[string]int{}
	total := 0
	for _, line := range strings.Split(text, "\n") {
		if line == "" || total >= limit {
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
			out.Files = append(out.Files, searchpanel.FileMatches{
				Path: filepath.Join(root, filepath.FromSlash(rel)),
				Name: rel,
			})
		}
		content := strings.TrimRight(m[4], "\r")
		out.Files[idx].Matches = append(out.Files[idx].Matches, searchpanel.Match{
			Line: lineNo,
			Col:  col,
			Len:  len([]rune(req.Query)),
			Text: content,
		})
		total++
	}
	return out, nil
}
