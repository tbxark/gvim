package bundled_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/tbxark/gvim/internal/plugin/plugintest"
	"github.com/tbxark/gvim/internal/pluginapi"
)

// probe runs the body of a Lua command in a plugin "probe" that has the gvim table and a
// function say(...) that collects its arguments, joined by commas, one line per call. It
// returns the lines.
func probe(t *testing.T, o rigOpts, body string) (*rig, []string) {
	t.Helper()
	o.probe = `
local gvim = require "gvim"
local git = gvim.git
local lines = {}
local function say(...)
  local parts = {}
  for i = 1, select("#", ...) do
    -- notify makes one line of its text: show the characters it would turn into spaces
    parts[i] = (tostring((select(i, ...))):gsub("[\r\n%z]", { ["\r"] = "\\r", ["\n"] = "\\n", ["\0"] = "\\0" }))
  end
  lines[#lines + 1] = table.concat(parts, ",")
end
gvim.command.register{ name = "go", run = function()
` + body + `
  gvim.notify(table.concat(lines, "§"))
end }`
	r := startRig(t, o)
	if err := r.h.Run("probe.go", nil); err != nil {
		t.Fatal(err)
	}
	notices := r.h.NoticeTexts()
	if len(notices) != 1 {
		t.Fatalf("notices = %v (log %v)", notices, r.h.LogText(""))
	}
	return r, strings.Split(notices[0], "§")
}

// anyGit makes every git command succeed with no output.
func anyGit(o *plugintest.FakeOS) { o.Script(gitHead, plugintest.ProcScript{}) }

// TestGitArgvHardened is binding (AMENDMENTS C28): every call is the fixed prefix, the
// extra flags of the subcommand and the arguments of the caller, with the fixed
// environment, and the caller cannot undo the environment.
func TestGitArgvHardened(t *testing.T) {
	tests := []struct {
		name string
		lua  string
		want []string
	}{
		{"show", `git.run({"show", "HEAD:x"})`, gitArgv("show", "--no-ext-diff", "--no-textconv", "HEAD:x")},
		{"diff", `git.run({"diff", "--stat"})`, gitArgv("diff", "--no-ext-diff", "--no-textconv", "--stat")},
		{"log", `git.run({"log", "-1"})`, gitArgv("log", "--no-ext-diff", "--no-textconv", "-1")},
		{"blame", `git.run({"blame", "x"})`, gitArgv("blame", "--no-textconv", "x")},
		{"rev-parse", `git.run({"rev-parse", "HEAD"})`, gitArgv("rev-parse", "HEAD")},
		{"ls-files", `git.run({"ls-files", "-z"})`, gitArgv("ls-files", "-z")},
		{"show of a file", `git.show("/w/dir/a.txt")`, gitArgv("show", "--no-ext-diff", "--no-textconv", "HEAD:./a.txt")},
		{"show at a revision", `git.show("/w/dir/a.txt", {rev = "v1.0"})`, gitArgv("show", "--no-ext-diff", "--no-textconv", "v1.0:./a.txt")},
		{"blame of lines", `git.blame("/w/dir/a.txt", {from = 4, to = 9})`, gitArgv("blame", "--no-textconv", "--porcelain", "-L5,9", "--", "a.txt")},
		{"blame of unsaved text", `git.blame("/w/dir/a.txt", {from = 0, to = 1, contents = "x"})`,
			gitArgv("blame", "--no-textconv", "--porcelain", "-L1,1", "--contents", "-", "--", "a.txt")},
		{"commit", `git.commit("/w/dir", "abc1234")`, gitArgv("show", "--no-ext-diff", "--no-textconv", "--stat", "--format=fuller", "abc1234")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, _ := probe(t, rigOpts{script: anyGit}, tt.lua)
			runs := r.os.Runs()
			// git.blame first lists the filter drivers of the repository (S19-1): a second
			// git command with the same fixed prefix and environment.
			if strings.Contains(tt.lua, "git.blame(") {
				if len(runs) != 2 || !slices.Equal(runs[0].Argv, filtersArgv) || runs[0].Opts.Env["GIT_CONFIG_NOSYSTEM"] != "1" {
					t.Fatalf("runs = %v, want the filter listing first", runs)
				}
				runs = runs[1:]
			}
			if len(runs) != 1 || !slices.Equal(runs[0].Argv, tt.want) {
				t.Fatalf("argv = %v\nwant   %v", runs, tt.want)
			}
		})
	}

	t.Run("environment", func(t *testing.T) {
		r, _ := probe(t, rigOpts{script: anyGit}, `git.run({"rev-parse", "HEAD"}, {cwd = "/w", env = {GIT_OPTIONAL_LOCKS = "1", GIT_DIR = "/x", FOO = "bar"}})`)
		runs := r.os.Runs()
		if len(runs) != 1 {
			t.Fatalf("runs = %v", runs)
		}
		o := runs[0].Opts
		want := map[string]string{"GIT_OPTIONAL_LOCKS": "0", "GIT_TERMINAL_PROMPT": "0", "GIT_CONFIG_NOSYSTEM": "1", "GIT_DIR": "/x", "FOO": "bar"}
		for k, v := range want {
			if o.Env[k] != v {
				t.Errorf("env %s = %q, want %q", k, o.Env[k], v)
			}
		}
		if o.Cwd != "/w" || o.Timeout == 0 {
			t.Errorf("options = %+v", o)
		}
	})
	t.Run("show runs in the directory of the file", func(t *testing.T) {
		r, _ := probe(t, rigOpts{script: anyGit}, `git.show("/w/dir/a.txt")`)
		if got := r.os.Runs()[0].Opts.Cwd; got != "/w/dir" {
			t.Errorf("cwd = %q", got)
		}
	})
	t.Run("a revision cannot be an option", func(t *testing.T) {
		r, out := probe(t, rigOpts{script: anyGit}, `
say(git.show("/w/a.txt", {rev = "--output=/tmp/x"}))
say(git.show("/w/a.txt", {rev = ""}))
say(git.show("/w/a.txt", {rev = "a\nb"}))
say(git.blame("/w/a.txt", {rev = "-p"}))
say(git.commit("/w", "--help"))
say(git.commit("/w", "abc"))`)
		want := []string{
			"nil,invalid: bad revision", "nil,invalid: bad revision", "nil,invalid: bad revision",
			"nil,invalid: bad revision", "nil,invalid: bad commit id", "nil,invalid: bad commit id",
		}
		if !slices.Equal(out, want) || len(r.os.Runs()) != 0 {
			t.Errorf("out = %q, runs %v", out, r.os.Runs())
		}
	})
}

// TestGitRootBranchFallbackWithoutBinary is binding (AMENDMENTS C28).
func TestGitRootBranchFallbackWithoutBinary(t *testing.T) {
	svc := &plugintest.FakeServices{GitRoots: map[string]string{"/repo": "/repo"}, Branches: map[string]string{"/repo": "main"}}
	r, out := probe(t, rigOpts{noGit: true, services: svc}, `
say("has", gvim.has("git"), git.available())
say("root", git.root("/repo/sub/f.txt"))
say("root of a stranger", git.root("/elsewhere/f.txt"))
local repo = git.repo("/repo/sub/f.txt")
say("repo", repo.root, repo.head)
say("branch", git.branch("/repo"))
say("branch of a stranger", git.branch("/elsewhere"))
say("show", git.show("/repo/f.txt"))
say("ls_files", git.ls_files("/repo"))
say("blame", git.blame("/repo/f.txt"))
say("run", git.run({"status"}))
say("commit", git.commit("/repo", "abc1234"))`)
	want := []string{
		"has,false,false",
		"root,/repo",
		"root of a stranger,nil,nil",
		"repo,/repo,/repo/.git/HEAD",
		"branch,main",
		"branch of a stranger,nil,not_found: not a repository: /elsewhere",
		"show,nil,unsupported: git binary not found",
		"ls_files,nil,unsupported: git binary not found",
		"blame,nil,unsupported: git binary not found",
		"run,nil,unsupported: git binary not found",
		"commit,nil,unsupported: git binary not found",
	}
	if !slices.Equal(out, want) {
		t.Errorf("out =\n%s\nwant\n%s", strings.Join(out, "\n"), strings.Join(want, "\n"))
	}
	if runs := r.os.Runs(); len(runs) != 0 {
		t.Errorf("processes started without a git binary: %v", runs)
	}
}

func TestGitRepo(t *testing.T) {
	tests := []struct {
		name   string
		script plugintest.ProcScript
		want   string
	}{
		{"relative HEAD path is resolved against the directory git ran in", plugintest.ProcScript{Stdout: "/r\n../.git/HEAD\n"}, "repo,/r,/r/.git/HEAD"},
		{"absolute HEAD path of a worktree", plugintest.ProcScript{Stdout: "/r\n/main/.git/worktrees/w/HEAD\n"}, "repo,/r,/main/.git/worktrees/w/HEAD"},
		{"not a repository", plugintest.ProcScript{Code: 128, Stderr: "fatal: not a git repository\n"}, "repo,nil,nil"},
		{"no top level (a bare repository)", plugintest.ProcScript{Stdout: "\n."}, "repo,nil,nil"},
		{"git cannot start", plugintest.ProcScript{Err: pluginapi.Errf(pluginapi.CodeExec, "not executable")}, "repo,nil,exec: not executable"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, out := probe(t, rigOpts{script: func(o *plugintest.FakeOS) {
				o.Script(gitArgv("rev-parse", "--show-toplevel", "--git-path", "HEAD"), tt.script)
			}}, `
local repo, err = git.repo("/r/sub/f.txt")
if repo then say("repo", repo.root, repo.head) else say("repo", repo, err) end`)
			if len(out) != 1 || out[0] != tt.want {
				t.Errorf("out = %q, want %q", out, tt.want)
			}
			if got := r.os.Runs()[0].Opts.Cwd; got != "/r/sub" {
				t.Errorf("cwd = %q, want the directory of the file", got)
			}
		})
	}
}

func TestGitBranch(t *testing.T) {
	sym := gitArgv("symbolic-ref", "--short", "-q", "HEAD")
	short := gitArgv("rev-parse", "--short=7", "HEAD")
	tests := []struct {
		name   string
		script func(o *plugintest.FakeOS)
		want   string
	}{
		{"a branch", func(o *plugintest.FakeOS) {
			o.Script(sym, plugintest.ProcScript{Stdout: "feature/x\n"})
		}, "feature/x"},
		{"detached HEAD: the short commit id", func(o *plugintest.FakeOS) {
			o.Script(sym, plugintest.ProcScript{Code: 1})
			o.Script(short, plugintest.ProcScript{Stdout: "abc1234\n"})
		}, "abc1234,true"},
		{"not a repository", func(o *plugintest.FakeOS) {
			o.Script(sym, plugintest.ProcScript{Code: 128, Stderr: "fatal: not a git repository\n"})
		}, "nil"},
		{"cannot start", func(o *plugintest.FakeOS) {
			o.Script(sym, plugintest.ProcScript{Err: pluginapi.Errf(pluginapi.CodeExec, "boom")})
		}, "nil,exec: boom"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, out := probe(t, rigOpts{script: tt.script}, `say(git.branch("/r"))`)
			if len(out) != 1 || out[0] != tt.want {
				t.Errorf("out = %q, want %q", out, tt.want)
			}
		})
	}
}

// TestGitShowForBufferDecodes is binding (AMENDMENTS C22): the blob is decoded with the
// encoding of the buffer, and its line ends are made LF.
func TestGitShowForBufferDecodes(t *testing.T) {
	tests := []struct {
		name, encoding, blob string
		forBuffer            bool
		want                 string
	}{
		{"latin-1 file with CRLF", "latin-1", "caf\xe9\r\nna\xefve\r\n", true, "café\\nnaïve\\n"},
		{"utf-8 with lone CR", "utf-8", "a\rb\r\nc", true, "a\\nb\\nc"},
		{"no buffer: the bytes as they are", "latin-1", "caf\xe9\r\n", false, "caf\xe9\\r\\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lua := `say(git.show("/w/a.txt"))`
			if tt.forBuffer {
				lua = `say(git.show("/w/a.txt", {for_buffer = gvim.buf.current()}))`
			}
			_, out := probe(t, rigOpts{
				buffers:  []rigBuffer{{"/w/a.txt", "x"}},
				encoding: tt.encoding,
				script: func(o *plugintest.FakeOS) {
					o.Script(showArgv("a.txt"), plugintest.ProcScript{Stdout: tt.blob})
				},
			}, lua)
			if len(out) != 1 || out[0] != tt.want {
				t.Errorf("out = %q, want %q", out, tt.want)
			}
		})
	}
}

func TestGitShowErrors(t *testing.T) {
	tests := []struct {
		name   string
		script plugintest.ProcScript
		want   string
	}{
		{"not in HEAD", plugintest.ProcScript{Code: 128, Stderr: "fatal: path 'a.txt' exists on disk, but not in 'HEAD'\nmore\n"}, "nil,not_found: fatal: path 'a.txt' exists on disk, but not in 'HEAD'"},
		{"too large for the output limit", plugintest.ProcScript{Stdout: "x", Truncated: true}, "nil,too_large: the file is larger than 8388608 bytes"},
		{"cannot start", plugintest.ProcScript{Err: pluginapi.Errf(pluginapi.CodeExec, "boom")}, "nil,exec: boom"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, out := probe(t, rigOpts{script: func(o *plugintest.FakeOS) {
				o.Script(showArgv("a.txt"), tt.script)
			}}, `say(git.show("/w/a.txt"))`)
			if len(out) != 1 || out[0] != tt.want {
				t.Errorf("out = %q, want %q", out, tt.want)
			}
		})
	}
}

func TestGitLsFiles(t *testing.T) {
	ls := gitArgv("ls-files", "-z", "--full-name")
	tests := []struct {
		name   string
		script plugintest.ProcScript
		want   string
	}{
		{"names with spaces and newlines", plugintest.ProcScript{Stdout: "a b.txt\x00dir/c\nd.go\x00"}, "2,false,a b.txt,dir/c\\nd.go"},
		{"nothing tracked", plugintest.ProcScript{}, "0,false,"},
		{"cut output: the last name may be partial", plugintest.ProcScript{Stdout: "a\x00b\x00par", Truncated: true}, "2,true,a,b"},
		{"not a repository", plugintest.ProcScript{Code: 128, Stderr: "fatal: not a git repository\n"}, "nil,not_found: fatal: not a git repository"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, out := probe(t, rigOpts{script: func(o *plugintest.FakeOS) { o.Script(ls, tt.script) }}, `
local files, truncated = git.ls_files("/r")
if files then say(#files, truncated, table.concat(files, ",")) else say(files, truncated) end`)
			if got := strings.Join(out, "|"); got != tt.want {
				t.Errorf("out = %q, want %q", got, tt.want)
			}
		})
	}
}

var blameOut = strings.ReplaceAll(`1111111111111111111111111111111111111111 1 1 2
author Ann Author
author-mail <ann@example.com>
author-time 1700000000
author-tz +0000
committer Ann Author
summary first commit
filename a.txt
\tline one
1111111111111111111111111111111111111111 2 2
\tline two
2222222222222222222222222222222222222222 3 3 1
author Bob B
author-time 1700001000
summary fix: the "third" line
previous 1111111111111111111111111111111111111111 a.txt
filename a.txt
\tline three
0000000000000000000000000000000000000000 4 4 1
author Not Committed Yet
author-time 1700002000
summary Version of a.txt from a.txt
filename a.txt
\tline four
`, `\t`, "\t")

func TestGitBlameParsesPorcelain(t *testing.T) {
	_, out := probe(t, rigOpts{script: func(o *plugintest.FakeOS) {
		o.Script(gitArgv("blame", "--no-textconv", "--porcelain", "--", "a.txt"), plugintest.ProcScript{Stdout: blameOut})
	}}, `
local entries = git.blame("/w/a.txt")
for _, e in ipairs(entries) do say(e.line, e.commit:sub(1, 2), e.author, e.time, e.summary, e.uncommitted) end`)
	want := []string{
		"0,11,Ann Author,1700000000,first commit,false",
		"1,11,Ann Author,1700000000,first commit,false",
		"2,22,Bob B,1700001000,fix: the \"third\" line,false",
		"3,00,Not Committed Yet,1700002000,Version of a.txt from a.txt,true",
	}
	if !slices.Equal(out, want) {
		t.Errorf("entries =\n%s\nwant\n%s", strings.Join(out, "\n"), strings.Join(want, "\n"))
	}
}

func TestGitBlameErrors(t *testing.T) {
	blame := gitArgv("blame", "--no-textconv", "--porcelain", "--", "a.txt")
	tests := []struct {
		name   string
		script plugintest.ProcScript
		want   string
	}{
		{"not tracked", plugintest.ProcScript{Code: 128, Stderr: "fatal: no such path a.txt in HEAD\n"}, "nil,not_found: fatal: no such path a.txt in HEAD"},
		{"huge output", plugintest.ProcScript{Truncated: true}, "nil,too_large: the blame output is larger than 4194304 bytes"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, out := probe(t, rigOpts{script: func(o *plugintest.FakeOS) { o.Script(blame, tt.script) }}, `say(git.blame("/w/a.txt"))`)
			if len(out) != 1 || out[0] != tt.want {
				t.Errorf("out = %q, want %q", out, tt.want)
			}
		})
	}
}

// TestGitBlameNeutralisesRepositoryFilters is the regression test of S19-1: `git blame` reads
// the work tree file (or the --contents text) through the clean filters that .gitattributes
// assigns, and their commands come from the repository's config, so every filter the
// config names is switched off on the command line.
func TestGitBlameNeutralisesRepositoryFilters(t *testing.T) {
	off := func(name string) []string {
		return []string{
			"-c", "filter." + name + ".clean=", "-c", "filter." + name + ".smudge=",
			"-c", "filter." + name + ".process=", "-c", "filter." + name + ".required=false",
		}
	}
	// blameWith is the blame command line with extra settings between the fixed prefix and "blame".
	blameWith := func(extra []string, args ...string) []string {
		return append(append(slices.Clone(gitHead), extra...), append([]string{"blame", "--no-textconv", "--porcelain"}, args...)...)
	}
	tests := []struct {
		name   string
		config string // stdout of the key listing
		code   int
		lua    string
		want   []string // the blame argv, nil when blame must not run
		out    string
	}{
		{"no filters", "", 1, `git.blame("/w/a.txt")`, blameWith(nil, "--", "a.txt"), ""},
		{"clean filter", "filter.evil.clean\x00filter.evil.required\x00", 0, `git.blame("/w/a.txt")`,
			blameWith(off("evil"), "--", "a.txt"), ""},
		{"filters with unsaved text", "filter.evil.clean\x00", 0, `git.blame("/w/a.txt", {contents = "x"})`,
			blameWith(off("evil"), "--contents", "-", "--", "a.txt"), ""},
		{"process filter, dotted name, two filters", "filter.a.b.process\x00filter.lfs.smudge\x00filter.a.b.clean\x00", 0, `git.blame("/w/a.txt")`,
			blameWith(slices.Concat(off("a.b"), off("lfs")), "--", "a.txt"), ""},
		{"name that -c cannot carry", "filter.x=y.clean\x00", 0, `say(git.blame("/w/a.txt"))`, nil,
			"nil,unsupported: the repository defines filters that cannot be switched off"},
		{"the listing fails", "", 128, `say(git.blame("/w/a.txt"))`, nil,
			"nil,unsupported: cannot list the filters of the repository"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, out := probe(t, rigOpts{script: func(o *plugintest.FakeOS) {
				o.Script(filtersArgv, plugintest.ProcScript{Stdout: tt.config, Code: tt.code})
				anyGit(o)
			}}, tt.lua)
			var blames [][]string
			for _, run := range r.os.Runs() {
				if len(run.Argv) > len(gitHead) && slices.Contains(run.Argv, "blame") {
					blames = append(blames, run.Argv)
				}
			}
			if tt.want == nil {
				if len(blames) != 0 {
					t.Fatalf("blame ran: %v", blames)
				}
				if out[0] != tt.out {
					t.Errorf("out = %q, want %q", out[0], tt.out)
				}
				return
			}
			if len(blames) != 1 || !slices.Equal(blames[0], tt.want) {
				t.Fatalf("blame argv = %v\nwant        %v", blames, tt.want)
			}
		})
	}
}
