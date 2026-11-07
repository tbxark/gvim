package plugin

import (
	"context"

	lua "github.com/yuin/gopher-lua"

	"github.com/tbxark/gvim/internal/pluginapi"
)

// gvim.workspace (stage S9):
//
//	gvim.workspace.root() -> string
//	gvim.workspace.files(opts?) -> {files = {...}, loading = bool}   finder index snapshot, no IO; opts {limit = 50000}
//	gvim.workspace.grep(query, opts?) ⏳ -> { {path, line, col, text}, ... } | nil, err
//	      opts {regex = false, case_sensitive = false, max = 2000}; line and col are 0-based, col a
//	      byte offset into text (the app converts live grep's 1-based rune values, AMENDMENTS C24)
//	gvim.workspace.git_root(dir) ⏳ -> root | nil          Services.GitRoot, used by gvim.git when the binary is missing
//	gvim.workspace.git_branch(root) ⏳ -> name | nil, err  Services.GitBranch, same use (AMENDMENTS C28)
const (
	defaultFilesLimit = 50_000
	defaultGrepMax    = 2000
	maxGrepMax        = 20_000
)

func init() {
	registerAwait("workspace_grep", awaitGrep)
	registerAwait("workspace_git_root", awaitRepoRoot)
	registerAwait("workspace_git_branch", awaitRepoBranch)
}

func installWorkspace(e *apiEnv) {
	r := e.r
	t := e.area("workspace")
	e.set(t, "root", func(L *lua.LState) int {
		L.Push(lua.LString(r.guard.Root()))
		return 1
	})
	e.set(t, "files", func(L *lua.LState) int {
		limit := defaultFilesLimit
		if o := L.OptTable(1, nil); o != nil {
			if n, ok := o.RawGetString("limit").(lua.LNumber); ok {
				limit = max(1, min(clampInt(float64(n)), defaultFilesLimit))
			}
		}
		files, loading := r.guard.Files(limit)
		if len(files) > limit {
			files = files[:limit]
		}
		out := L.CreateTable(0, 2)
		out.RawSetString("files", e.p.toLua(files))
		out.RawSetString("loading", lua.LBool(loading))
		L.Push(out)
		return 1
	})
	t.RawSetString("grep", e.async("workspace_grep"))
	t.RawSetString("git_root", e.async("workspace_git_root"))
	t.RawSetString("git_branch", e.async("workspace_git_branch"))
}

// awaitRepoRoot finds the work tree that holds a directory without the external binary.
func awaitRepoRoot(c *awaitCall) {
	dir, ok := stringArg(c.args, 0)
	if !ok || dir == "" {
		c.Bad(1, "git_root", "directory expected")
		return
	}
	if err := c.CheckPark(true); err != nil {
		c.Fail(err)
		return
	}
	svc := c.r.svc
	c.Park(&parkSpec{Work: &pluginapi.Work{Run: func(context.Context) pluginapi.Completion {
		root, found := svc.GitRoot(dir)
		if !found {
			return pluginapi.Completion{Values: []any{nil}}
		}
		return pluginapi.Completion{Values: []any{root}}
	}}})
}

// awaitRepoBranch reads the checked-out branch of a work tree without the external binary.
func awaitRepoBranch(c *awaitCall) {
	root, ok := stringArg(c.args, 0)
	if !ok || root == "" {
		c.Bad(1, "git_branch", "work tree root expected")
		return
	}
	if err := c.CheckPark(true); err != nil {
		c.Fail(err)
		return
	}
	svc := c.r.svc
	c.Park(&parkSpec{Work: &pluginapi.Work{Run: func(context.Context) pluginapi.Completion {
		name, err := svc.GitBranch(root)
		if err != nil {
			return pluginapi.Completion{Err: errString(err)}
		}
		return pluginapi.Completion{Values: []any{name}}
	}}})
}

func awaitGrep(c *awaitCall) {
	if len(c.args) == 0 {
		c.Bad(1, "grep", "query expected")
		return
	}
	q, ok := c.args[0].(lua.LString)
	if !ok {
		c.Bad(1, "grep", "string expected")
		return
	}
	if q == "" {
		c.Fail(invalidErr("grep: the query is empty"))
		return
	}
	req := pluginapi.GrepRequest{Root: c.r.guard.Root(), Query: string(q), Max: defaultGrepMax}
	if len(c.args) > 1 {
		if o, ok := c.args[1].(*lua.LTable); ok {
			req.Regex = lua.LVAsBool(o.RawGetString("regex"))
			req.CaseSensitive = lua.LVAsBool(o.RawGetString("case_sensitive"))
			if n, ok := o.RawGetString("max").(lua.LNumber); ok {
				req.Max = max(1, min(clampInt(float64(n)), maxGrepMax))
			}
		}
	}
	if err := c.CheckPark(true); err != nil {
		c.Fail(err)
		return
	}
	svc := c.r.svc
	c.Park(&parkSpec{Work: &pluginapi.Work{Run: func(ctx context.Context) pluginapi.Completion {
		hits, err := svc.Grep(ctx, req)
		if err != nil {
			return pluginapi.Completion{Err: errString(err)}
		}
		if len(hits) > req.Max {
			hits = hits[:req.Max]
		}
		return pluginapi.Completion{Values: []any{hits}}
	}}})
}
