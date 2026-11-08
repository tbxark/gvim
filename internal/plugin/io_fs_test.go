package plugin_test

import (
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/tbxark/gvim/internal/plugin/plugintest"
)

// osCmd runs a command whose body gets a FakeOS.
func osCmd(t *testing.T, fos *plugintest.FakeOS, prelude, body string) *plugintest.Harness {
	t.Helper()
	return cmd(t, prelude, body, plugintest.Options{OS: fos})
}

func TestPathHelpers(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("rows use slash-separated paths")
	}
	rows := []struct{ name, body, want string }{
		{"sep", `T.spy(gvim.path.sep)`, "/"},
		{"join cleans", `T.spy(gvim.path.join("a", "b", "../c", "d.txt"))`, "a/c/d.txt"},
		{"join absolute", `T.spy(gvim.path.join("/a/", "/b"))`, "/a/b"},
		{"join nothing", `T.spy(gvim.path.join())`, ""},
		{"dirname", `T.spy(gvim.path.dirname("/a/b/c.go"), gvim.path.dirname("c.go"), gvim.path.dirname("/"))`, "/a/b . /"},
		{"basename", `T.spy(gvim.path.basename("/a/b/c.go"), gvim.path.basename("/a/b/"), gvim.path.basename("c"))`, "c.go b c"},
		{"extname", `T.spy(gvim.path.extname("a/b.tar.gz"), "[" .. gvim.path.extname(".bashrc") .. "]", "[" .. gvim.path.extname("noext") .. "]", gvim.path.extname("dir.d/file"))`, ".gz [] [] "},
		{"is_absolute", `T.spy(gvim.path.is_absolute("/a"), gvim.path.is_absolute("a/b"), gvim.path.is_absolute(""))`, "true false false"},
		{"normalize", `T.spy(gvim.path.normalize("/a/./b/../c//d"), gvim.path.normalize(""))`, "/a/c/d ."},
		{"relative", `T.spy(gvim.path.relative("/a/b", "/a/c/d"), gvim.path.relative("/a", "/a"))`, "../c/d ."},
		{"relative of unrelated forms", `local v, e = gvim.path.relative("a", "/b") T.spy(tostring(v), gvim.err_code(e))`, "nil invalid"},
		{"split", `local d, f = gvim.path.split("/x/y/z.lua") T.spy(d, f)`, "/x/y z.lua"},
		{"to_uri", `T.spy(gvim.path.to_uri("/a b/c#d.txt"))`, "file:///a%20b/c%23d.txt"},
		{"to_uri relative is refused", `local v, e = gvim.path.to_uri("a/b") T.spy(tostring(v), gvim.err_code(e))`, "nil invalid"},
		{"from_uri", `T.spy(gvim.path.from_uri("file:///a%20b/c%23d.txt"))`, "/a b/c#d.txt"},
		{"from_uri localhost", `T.spy(gvim.path.from_uri("file://localhost/etc/x"))`, "/etc/x"},
		{"from_uri other scheme", `local v, e = gvim.path.from_uri("https://example.com/x") T.spy(tostring(v), gvim.err_code(e))`, "nil invalid"},
		{"from_uri other host", `local v, e = gvim.path.from_uri("file://server/share/x") T.spy(tostring(v), gvim.err_code(e))`, "nil invalid"},
		{"wrong argument type raises", `local ok, e = pcall(function() return gvim.path.dirname({}) end) T.spy(ok, e:match("bad argument.*$"))`, "false bad argument #1 to dirname (string expected, got table)"},
	}
	for _, r := range rows {
		t.Run(r.name, func(t *testing.T) {
			h := cmd(t, "", r.body)
			if got := spyText(h); got != r.want {
				t.Fatalf("spy = %q, want %q\nlog: %v", got, r.want, h.LogText(""))
			}
		})
	}
}

func TestFS(t *testing.T) {
	bigPath := "/work/" + strings.Repeat("x", 5000)
	rows := []struct {
		name  string
		setup func(*plugintest.FakeOS)
		body  string
		want  string
		check func(*testing.T, *plugintest.FakeOS)
	}{
		{name: "read relative to cwd", setup: func(f *plugintest.FakeOS) { f.SetFile("/work/a.txt", "hello") },
			body: `T.spy(gvim.fs.read("a.txt"))`, want: "hello"},
		{name: "read missing is not_found", body: `local v, e = gvim.fs.read("/work/none") T.spy(tostring(v), gvim.err_code(e))`, want: "nil not_found"},
		{name: "read over max is too_large", setup: func(f *plugintest.FakeOS) { f.SetFile("/work/big", strings.Repeat("z", 100)) },
			body: `local v, e = gvim.fs.read("/work/big", {max = 10}) T.spy(tostring(v), gvim.err_code(e))`, want: "nil too_large"},
		{name: "read of a directory is io", body: `local v, e = gvim.fs.read("/work") T.spy(tostring(v), gvim.err_code(e))`, want: "nil io"},
		{name: "write creates", body: `T.spy(gvim.fs.write("/work/o.txt", "data"))`, want: "true",
			check: func(t *testing.T, f *plugintest.FakeOS) {
				if got, _ := f.File("/work/o.txt"); got != "data" {
					t.Fatalf("file = %q", got)
				}
			}},
		{name: "write without parents fails", body: `local v, e = gvim.fs.write("/work/x/y.txt", "d") T.spy(tostring(v), gvim.err_code(e))`, want: "nil not_found"},
		{name: "write with parents", body: `T.spy(gvim.fs.write("/work/x/y.txt", "d", {parents = true}))`, want: "true",
			check: func(t *testing.T, f *plugintest.FakeOS) {
				if got, _ := f.File("/work/x/y.txt"); got != "d" {
					t.Fatalf("file = %q", got)
				}
			}},
		{name: "append", setup: func(f *plugintest.FakeOS) { f.SetFile("/work/log", "ab") },
			body: `T.spy(gvim.fs.write("/work/log", "cd", {append = true}))`, want: "true",
			check: func(t *testing.T, f *plugintest.FakeOS) {
				if got, _ := f.File("/work/log"); got != "abcd" {
					t.Fatalf("file = %q", got)
				}
			}},
		{name: "list hides dotfiles", setup: func(f *plugintest.FakeOS) {
			f.SetFile("/work/a.txt", "1")
			f.SetFile("/work/.hidden", "2")
			f.SetDir("/work/sub")
		}, body: `for _, e in ipairs(gvim.fs.list("/work")) do T.spy(e.name, e.kind, e.size) end`, want: "a.txt file 1|sub dir 0"},
		{name: "list hidden", setup: func(f *plugintest.FakeOS) {
			f.SetFile("/work/a.txt", "1")
			f.SetFile("/work/.hidden", "2")
		}, body: `for _, e in ipairs(gvim.fs.list("/work", {hidden = true})) do T.spy(e.name) end`, want: ".hidden|a.txt"},
		{name: "stat", setup: func(f *plugintest.FakeOS) { f.SetFile("/work/a.txt", "hello") },
			body: `local s = gvim.fs.stat("/work/a.txt") T.spy(s.kind, s.size, s.mode, s.mtime > 0)`, want: "file 5 420 true"},
		{name: "exists", setup: func(f *plugintest.FakeOS) { f.SetFile("/work/a.txt", "hello") },
			body: `T.spy(gvim.fs.exists("/work/a.txt"), gvim.fs.exists("/work/none"))`, want: "true false"},
		{name: "mkdir makes parents by default", body: `T.spy(gvim.fs.mkdir("/work/a/b/c"), gvim.fs.exists("/work/a/b/c"))`, want: "true true"},
		{name: "mkdir without parents", body: `local v, e = gvim.fs.mkdir("/work/a/b", {parents = false}) T.spy(tostring(v), gvim.err_code(e))`, want: "nil not_found"},
		{name: "remove a file", setup: func(f *plugintest.FakeOS) { f.SetFile("/work/a.txt", "1") },
			body: `T.spy(gvim.fs.remove("a.txt"), gvim.fs.exists("a.txt"))`, want: "true false"},
		{name: "remove of a full directory needs recursive", setup: func(f *plugintest.FakeOS) { f.SetFile("/work/d/a.txt", "1") },
			body: `local v, e = gvim.fs.remove("/work/d") T.spy(tostring(v), gvim.err_code(e), gvim.fs.remove("/work/d", {recursive = true}), gvim.fs.exists("/work/d"))`, want: "nil io true false"},
		{name: "rename", setup: func(f *plugintest.FakeOS) { f.SetFile("/work/a.txt", "1") },
			body: `T.spy(gvim.fs.rename("/work/a.txt", "/work/b.txt"), gvim.fs.exists("/work/a.txt"), gvim.fs.exists("/work/b.txt"))`, want: "true false true"},
		{name: "copy", setup: func(f *plugintest.FakeOS) { f.SetFile("/work/a.txt", "1") },
			body: `T.spy(gvim.fs.copy("/work/a.txt", "/work/b.txt"), gvim.fs.exists("/work/a.txt"), gvim.fs.exists("/work/b.txt"))`, want: "true true true"},
		{name: "realpath", body: `T.spy(gvim.fs.realpath("/work/../work/"))`, want: "/work"},
		{name: "walk skips, hides and reports", setup: func(f *plugintest.FakeOS) {
			f.SetFile("/work/src/a.go", "1")
			f.SetFile("/work/README.md", "1")
			f.SetFile("/work/.git/config", "1")
			f.SetFile("/work/node_modules/x/y.js", "1")
		}, body: `local w = gvim.fs.walk("/work", {skip_dirs = {"node_modules"}})
for _, e in ipairs(w.entries) do T.spy(e.rel, e.kind) end
T.spy(w.truncated)`, want: "README.md file|src dir|src/a.go file|false"},
		{name: "walk truncates", setup: func(f *plugintest.FakeOS) {
			f.SetFile("/work/a", "1")
			f.SetFile("/work/b", "1")
			f.SetFile("/work/c", "1")
		}, body: `local w = gvim.fs.walk("/work", {max_entries = 2}) T.spy(#w.entries, w.truncated)`, want: "2 true"},
		{name: "walk depth", setup: func(f *plugintest.FakeOS) { f.SetFile("/work/a/b/c.txt", "1") },
			body: `local w = gvim.fs.walk("/work", {max_depth = 2}) T.spy(#w.entries)`, want: "2"},

		{name: "table instead of a path raises", body: `local ok, e = pcall(gvim.fs.read, {}) T.spy(ok, e:match("bad argument.*$"))`, want: "false bad argument #1 to 'read' (string expected)"},
		{name: "second path raises too", body: `local ok, e = pcall(gvim.fs.rename, "/a") T.spy(ok, e:match("bad argument.*$"))`, want: "false bad argument #2 to 'rename' (string expected)"},
		{name: "options must be a table", body: `local ok, e = pcall(gvim.fs.read, "/x", 5) T.spy(ok, e:match("bad argument.*$"))`, want: "false bad argument #2 to 'read' (table expected)"},
		{name: "option types are checked", body: `local ok, e = pcall(gvim.fs.read, "/x", {max = "x"}) T.spy(ok, e:match("bad argument.*$"))`, want: "false bad argument #2 to 'read' (option max must be a number)"},
		{name: "empty path", body: `local v, e = gvim.fs.read("") T.spy(tostring(v), gvim.err_code(e))`, want: "nil invalid"},
		{name: "NUL in path", body: `local v, e = gvim.fs.stat("a\0b") T.spy(tostring(v), gvim.err_code(e))`, want: "nil invalid"},
		{name: "path too long", body: `local v, e = gvim.fs.stat("` + bigPath + `") T.spy(tostring(v), gvim.err_code(e))`, want: "nil invalid"},
	}
	for _, r := range rows {
		t.Run(r.name, func(t *testing.T) {
			fos := plugintest.NewFakeOS()
			if r.setup != nil {
				r.setup(fos)
			}
			h := osCmd(t, fos, "", r.body)
			if got := spyText(h); got != r.want {
				t.Fatalf("spy = %q, want %q\nlog: %v", got, r.want, h.LogText(""))
			}
			if r.check != nil {
				r.check(t, fos)
			}
		})
	}
}

// A write of more than 64 MiB never reaches the OS: gvim.fs.write refuses the data first. Stock
// gopher-lua does not cap the concatenation operator, so the command builds the 64 MiB+1 string
// and the size check of the write API is what stops it.
func TestFSWriteTooLargeIsRefusedBeforeTheOS(t *testing.T) {
	fos := plugintest.NewFakeOS()
	// Building a 64 MiB string is real work: the command gets a budget that a busy machine
	// cannot exhaust (this test is about the size check, not about the time budget).
	h := cmd(t, "", `
local half = string.rep("x", 33554432)
local ok, err = pcall(function() return half .. half .. "x" end)
T.spy(tostring(ok), #err)
local wok, werr = pcall(function() local _, e = gvim.fs.write("/work/huge", err) return e end)
T.spy(tostring(wok), tostring(werr):match("data is larger than %d+ bytes") or tostring(werr))`, plugintest.Options{OS: fos, Timeout: 20 * time.Second})
	if got := spyText(h); got != "true 67108865|true data is larger than 67108864 bytes" {
		t.Fatalf("spy = %q log=%v", got, h.LogText(""))
	}
	if fos.Exists("/work/huge") {
		t.Fatal("the OS saw the write")
	}
}

// TestFSRemoveRefusesProtectedDirectories is the security rule of fs.remove: the root, the
// home, the workspace, the config directory and everything that contains one of them stay,
// even recursively, and the OS is never asked.
func TestFSRemoveRefusesProtectedDirectories(t *testing.T) {
	for _, p := range []string{"/", "/home/u", "/work", "/home/u/.config/gvim", "/home", "/home/u/.config", ".", "..", "/work/../home/u", "/home/u/.config/gvim/"} {
		for _, rec := range []string{"true", "false"} {
			t.Run(p+"/"+rec, func(t *testing.T) {
				fos := plugintest.NewFakeOS()
				h := osCmd(t, fos, "", `local v, e = gvim.fs.remove("`+p+`", {recursive = `+rec+`}) T.spy(tostring(v), gvim.err_code(e))`)
				if got := spyText(h); got != "nil invalid" {
					t.Fatalf("spy = %q log=%v", got, h.LogText(""))
				}
				for _, c := range fos.Calls() {
					if strings.HasPrefix(c, "Remove ") {
						t.Fatalf("the OS was asked to remove: %v", fos.Calls())
					}
				}
			})
		}
	}
	t.Run("a sibling is fine", func(t *testing.T) {
		fos := plugintest.NewFakeOS()
		fos.SetFile("/work/sub/a", "1")
		fos.SetFile("/home/u/other/b", "1")
		h := osCmd(t, fos, "", `T.spy(gvim.fs.remove("sub", {recursive = true}), gvim.fs.remove("/home/u/other", {recursive = true}))`)
		if got := spyText(h); got != "true true" {
			t.Fatalf("spy = %q log=%v", got, h.LogText(""))
		}
	})
}

// TestIOHappensOnWorkGoroutines: no file, process or watcher call of the plugin API reaches
// the OS on the goroutine that runs Lua (D2: Update does no IO).
func TestIOHappensOnWorkGoroutines(t *testing.T) {
	fos := plugintest.NewFakeOS()
	fos.SetFile("/work/a.txt", "1")
	fos.Script([]string{"tool"}, plugintest.ProcScript{Stdout: "x"})
	h := osCmd(t, fos, "", `
gvim.fs.read("/work/a.txt") gvim.fs.write("/work/b.txt", "2") gvim.fs.list("/work") gvim.fs.stat("/work/a.txt")
gvim.fs.exists("/work/a.txt") gvim.fs.mkdir("/work/d") gvim.fs.rename("/work/b.txt", "/work/c.txt")
gvim.fs.copy("/work/c.txt", "/work/e.txt") gvim.fs.realpath("/work") gvim.fs.walk("/work") gvim.fs.remove("/work/e.txt")
gvim.proc.run({"tool"})
local p = gvim.proc.spawn({"tool"}) p:wait()
gvim.proc.which("tool")
local w = gvim.fs.watch("/work", function() end)
gvim.timer.sleep(1)
w:stop()
T.spy("done")`)
	h.Advance(5 * ms)
	if got := spyText(h); got != "done" {
		t.Fatalf("spy = %q log=%v", got, h.LogText(""))
	}
	if n := fos.UICalls(); n != 0 {
		t.Fatalf("%d OS calls on the Lua goroutine: %v", n, fos.Calls())
	}
	if len(fos.Calls()) < 12 {
		t.Fatalf("the plugin did not reach the OS: %v", fos.Calls())
	}
}

func TestIOWithoutAnOSIsUnsupported(t *testing.T) {
	h := cmd(t, "", `
local function code(v, e) return tostring(v) .. " " .. tostring(gvim.err_code(e)) end
T.spy(code(gvim.fs.read("/x")))
T.spy(code(gvim.fs.write("/x", "d")))
T.spy(code(gvim.proc.run({"tool"})))
T.spy(tostring(gvim.proc.which("tool")))
local p = gvim.proc.spawn({"tool"}, {on_exit = function(r) T.spy("exit", tostring(r.code), gvim.err_code(r.err)) end})
T.spy(code(p:wait()))
gvim.fs.watch("/x", function(ev) T.spy("watch", ev.kind, gvim.err_code(ev.err)) end)
T.spy(gvim.env.get("PATH") ~= nil, gvim.dirs.config() == "")`, plugintest.Options{OS: plugintest.NopOS{}})
	want := []string{"nil unsupported", "nil unsupported", "nil unsupported", "nil", "nil unsupported", "exit nil unsupported", "watch error unsupported", "true true"}
	if got := spyText(h); !sameLines(got, strings.Join(want, "|")) {
		t.Fatalf("spy = %q log=%v", got, h.LogText(""))
	}
}

func TestEnvAndDirs(t *testing.T) {
	fos := plugintest.NewFakeOS()
	fos.Env["GVIM_TEST"] = "yes"
	h := osCmd(t, fos, "", `
T.spy(gvim.env.get("GVIM_TEST"), tostring(gvim.env.get("GVIM_NOPE")))
T.spy(gvim.dirs.config(), gvim.dirs.data(), gvim.dirs.root(), gvim.dirs.cwd(), gvim.dirs.home(), gvim.dirs.temp(), "[" .. gvim.dirs.plugin() .. "]")`)
	want := "yes nil|/home/u/.config/gvim /home/u/.config/gvim/plugin-data/t /work /work /home/u /tmp []"
	if got := spyText(h); got != want {
		t.Fatalf("spy = %q, want %q", got, want)
	}
}
