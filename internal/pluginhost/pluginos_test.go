package pluginhost

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	goruntime "runtime"
	"strings"
	"testing"
	"time"

	"github.com/tbxark/gvim/plugin/hostos"
	"github.com/tbxark/gvim/pluginapi"
)

// newTestOS returns a pluginOS whose directories are inside a temporary one.
func newTestOS(t *testing.T) (*pluginOS, string) {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	o := newPluginOS(func() hostos.Dirs {
		return hostos.Dirs{Config: filepath.Join(dir, "cfg"), Home: filepath.Join(dir, "home"), Temp: dir, Cwd: dir, Root: dir}
	})
	t.Cleanup(o.Shutdown)
	return o, dir
}

func writePluginTree(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func codeOf(err error) pluginapi.Code {
	if err == nil {
		return ""
	}
	return pluginapi.CodeOf(err)
}

func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestPluginOSReadFile(t *testing.T) {
	o, dir := newTestOS(t)
	writePluginTree(t, dir, map[string]string{"a.txt": "hello", "sub/b.txt": "x"})
	ctx := context.Background()
	rows := []struct {
		name string
		path string
		max  int64
		want string
		code pluginapi.Code
	}{
		{"absolute", filepath.Join(dir, "a.txt"), 100, "hello", ""},
		{"relative to the cwd", "sub/b.txt", 100, "x", ""},
		{"exactly the limit", "a.txt", 5, "hello", ""},
		{"over the limit", "a.txt", 4, "", pluginapi.CodeTooLarge},
		{"missing", "nope.txt", 100, "", pluginapi.CodeNotFound},
		{"directory", "sub", 100, "", pluginapi.CodeIO},
	}
	for _, r := range rows {
		t.Run(r.name, func(t *testing.T) {
			data, err := o.ReadFile(ctx, r.path, r.max)
			if codeOf(err) != r.code || string(data) != r.want {
				t.Fatalf("ReadFile = %q, %v; want %q, code %q", data, err, r.want, r.code)
			}
		})
	}
	t.Run("cancelled", func(t *testing.T) {
		c, cancel := context.WithCancel(ctx)
		cancel()
		if _, err := o.ReadFile(c, "a.txt", 10); codeOf(err) != pluginapi.CodeCancelled {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestPluginOSWriteFile(t *testing.T) {
	o, dir := newTestOS(t)
	ctx := context.Background()
	read := func(name string) string {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	leftovers := func() []string {
		var out []string
		ents, _ := os.ReadDir(dir)
		for _, e := range ents {
			if strings.HasPrefix(e.Name(), ".gvim-") {
				out = append(out, e.Name())
			}
		}
		return out
	}

	t.Run("atomic leaves no temporary file", func(t *testing.T) {
		if err := o.WriteFile(ctx, "a.txt", []byte("one"), hostos.WriteOptions{Atomic: true}); err != nil {
			t.Fatal(err)
		}
		if err := o.WriteFile(ctx, "a.txt", []byte("two"), hostos.WriteOptions{Atomic: true}); err != nil {
			t.Fatal(err)
		}
		if read("a.txt") != "two" || len(leftovers()) != 0 {
			t.Fatalf("content %q, leftovers %v", read("a.txt"), leftovers())
		}
	})
	t.Run("a new file gets 0644, a replaced one keeps its mode", func(t *testing.T) {
		if goruntime.GOOS == "windows" {
			t.Skip("no unix modes")
		}
		if err := o.WriteFile(ctx, "m.txt", []byte("1"), hostos.WriteOptions{Atomic: true}); err != nil {
			t.Fatal(err)
		}
		if st, _ := os.Stat(filepath.Join(dir, "m.txt")); st.Mode().Perm() != 0o644 {
			t.Fatalf("mode = %v", st.Mode())
		}
		if err := os.Chmod(filepath.Join(dir, "m.txt"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := o.WriteFile(ctx, "m.txt", []byte("2"), hostos.WriteOptions{Atomic: true}); err != nil {
			t.Fatal(err)
		}
		if st, _ := os.Stat(filepath.Join(dir, "m.txt")); st.Mode().Perm() != 0o755 {
			t.Fatalf("mode after replace = %v", st.Mode())
		}
		if err := o.WriteFile(ctx, "m.txt", []byte("3"), hostos.WriteOptions{Atomic: true, Mode: 0o600}); err != nil {
			t.Fatal(err)
		}
		if st, _ := os.Stat(filepath.Join(dir, "m.txt")); st.Mode().Perm() != 0o600 {
			t.Fatalf("explicit mode = %v", st.Mode())
		}
	})
	t.Run("a symbolic link is written through", func(t *testing.T) {
		if goruntime.GOOS == "windows" {
			t.Skip("symlinks need privileges")
		}
		writePluginTree(t, dir, map[string]string{"real.txt": "old"})
		if err := os.Symlink("real.txt", filepath.Join(dir, "link.txt")); err != nil {
			t.Fatal(err)
		}
		if err := o.WriteFile(ctx, "link.txt", []byte("new"), hostos.WriteOptions{Atomic: true}); err != nil {
			t.Fatal(err)
		}
		if fi, _ := os.Lstat(filepath.Join(dir, "link.txt")); fi.Mode()&os.ModeSymlink == 0 {
			t.Fatal("the link was replaced")
		}
		if read("real.txt") != "new" {
			t.Fatalf("target = %q", read("real.txt"))
		}
	})
	t.Run("plain and append", func(t *testing.T) {
		if err := o.WriteFile(ctx, "p.txt", []byte("ab"), hostos.WriteOptions{}); err != nil {
			t.Fatal(err)
		}
		if err := o.WriteFile(ctx, "p.txt", []byte("cd"), hostos.WriteOptions{Append: true}); err != nil {
			t.Fatal(err)
		}
		if read("p.txt") != "abcd" {
			t.Fatalf("content %q", read("p.txt"))
		}
	})
	t.Run("parents", func(t *testing.T) {
		err := o.WriteFile(ctx, "x/y/z.txt", []byte("1"), hostos.WriteOptions{})
		if codeOf(err) != pluginapi.CodeNotFound {
			t.Fatalf("err = %v", err)
		}
		if err := o.WriteFile(ctx, "x/y/z.txt", []byte("1"), hostos.WriteOptions{Parents: true, Atomic: true}); err != nil {
			t.Fatal(err)
		}
		if read("x/y/z.txt") != "1" {
			t.Fatal("not written")
		}
	})
	t.Run("a directory is refused", func(t *testing.T) {
		if err := o.WriteFile(ctx, "x", []byte("1"), hostos.WriteOptions{Atomic: true}); codeOf(err) != pluginapi.CodeIO {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestPluginOSDirectories(t *testing.T) {
	o, dir := newTestOS(t)
	ctx := context.Background()
	writePluginTree(t, dir, map[string]string{"a.txt": "abc", ".hidden": "h", "sub/inner.txt": "i"})
	if goruntime.GOOS != "windows" {
		if err := os.Symlink("a.txt", filepath.Join(dir, "link")); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("missing", filepath.Join(dir, "broken")); err != nil {
			t.Fatal(err)
		}
	}

	t.Run("list", func(t *testing.T) {
		ents, err := o.ReadDir(ctx, ".", false)
		if err != nil {
			t.Fatal(err)
		}
		got := map[string]string{}
		for _, e := range ents {
			got[e.Name] = e.Kind + "/" + e.TargetKind
		}
		want := map[string]string{"a.txt": "file/", "sub": "dir/"}
		if goruntime.GOOS != "windows" {
			want["link"], want["broken"] = "symlink/file", "symlink/"
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("entries = %v, want %v", got, want)
		}
		all, _ := o.ReadDir(ctx, ".", true)
		if len(all) != len(ents)+1 {
			t.Fatalf("hidden entries not listed: %d vs %d", len(all), len(ents))
		}
	})
	t.Run("stat", func(t *testing.T) {
		st, err := o.Stat(ctx, "a.txt")
		if err != nil || st.Kind != "file" || st.Size != 3 {
			t.Fatalf("Stat = %+v, %v", st, err)
		}
		if st, _ := o.Stat(ctx, "sub"); st.Kind != "dir" {
			t.Fatalf("kind = %q", st.Kind)
		}
		if _, err := o.Stat(ctx, "nope"); codeOf(err) != pluginapi.CodeNotFound {
			t.Fatalf("err = %v", err)
		}
		if goruntime.GOOS != "windows" {
			if st, _ := o.Stat(ctx, "link"); st.Kind != "file" {
				t.Fatalf("a link to a file is %q", st.Kind)
			}
			if st, err := o.Stat(ctx, "broken"); err != nil || st.Kind != "symlink" {
				t.Fatalf("a broken link is %+v, %v", st, err)
			}
		}
	})
	t.Run("mkdir", func(t *testing.T) {
		if err := o.Mkdir(ctx, "p/q/r", false); codeOf(err) != pluginapi.CodeNotFound {
			t.Fatalf("err = %v", err)
		}
		if err := o.Mkdir(ctx, "p/q/r", true); err != nil {
			t.Fatal(err)
		}
		if err := o.Mkdir(ctx, "p/q/r", true); err != nil {
			t.Fatalf("an existing directory with parents: %v", err)
		}
		if err := o.Mkdir(ctx, "p", false); codeOf(err) != pluginapi.CodeConflict {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("realpath", func(t *testing.T) {
		if got, err := o.RealPath(ctx, "sub/../sub"); err != nil || got != filepath.Join(dir, "sub") {
			t.Fatalf("RealPath = %q, %v", got, err)
		}
		if _, err := o.RealPath(ctx, "nope"); codeOf(err) != pluginapi.CodeNotFound {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestPluginOSRemove(t *testing.T) {
	o, dir := newTestOS(t)
	ctx := context.Background()
	writePluginTree(t, dir, map[string]string{"d/a.txt": "1", "d/e/b.txt": "2", "f.txt": "3"})
	if err := o.Remove(ctx, "d", false); err == nil {
		t.Fatal("removed a directory with content without recursive")
	}
	if err := o.Remove(ctx, "d", true); err != nil {
		t.Fatal(err)
	}
	if err := o.Remove(ctx, "f.txt", false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "d")); !os.IsNotExist(err) {
		t.Fatalf("d survived: %v", err)
	}
	if err := o.Remove(ctx, "f.txt", false); codeOf(err) != pluginapi.CodeNotFound {
		t.Fatalf("err = %v", err)
	}
	if err := o.Remove(ctx, string(filepath.Separator), true); codeOf(err) != pluginapi.CodeInvalid {
		t.Fatalf("the filesystem root: %v", err)
	}
	t.Run("a recursive remove does not follow links", func(t *testing.T) {
		if goruntime.GOOS == "windows" {
			t.Skip("symlinks need privileges")
		}
		outside := t.TempDir()
		writePluginTree(t, outside, map[string]string{"keep.txt": "precious"})
		writePluginTree(t, dir, map[string]string{"victim/a.txt": "1"})
		if err := os.Symlink(outside, filepath.Join(dir, "victim", "zz_escape")); err != nil {
			t.Fatal(err)
		}
		if err := o.Remove(ctx, "victim", true); err != nil {
			t.Fatal(err)
		}
		if b, err := os.ReadFile(filepath.Join(outside, "keep.txt")); err != nil || string(b) != "precious" {
			t.Fatalf("the target of the link was touched: %q, %v", b, err)
		}
	})
}

func TestPluginOSRenameCopy(t *testing.T) {
	o, dir := newTestOS(t)
	ctx := context.Background()
	writePluginTree(t, dir, map[string]string{"a.txt": "A", "tree/x.txt": "X", "tree/deep/y.txt": "Y"})
	if goruntime.GOOS != "windows" {
		if err := os.Symlink("x.txt", filepath.Join(dir, "tree", "ln")); err != nil {
			t.Fatal(err)
		}
	}
	t.Run("rename", func(t *testing.T) {
		if err := o.Rename(ctx, "a.txt", "b.txt"); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(filepath.Join(dir, "a.txt")); !os.IsNotExist(err) {
			t.Fatal("source survived")
		}
		if err := o.Rename(ctx, "a.txt", "c.txt"); codeOf(err) != pluginapi.CodeNotFound {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("copy a file", func(t *testing.T) {
		if err := o.Copy(ctx, "b.txt", "copy.txt"); err != nil {
			t.Fatal(err)
		}
		if b, _ := os.ReadFile(filepath.Join(dir, "copy.txt")); string(b) != "A" {
			t.Fatalf("copy = %q", b)
		}
	})
	t.Run("copy a tree keeps links as links", func(t *testing.T) {
		if err := o.Copy(ctx, "tree", "tree2"); err != nil {
			t.Fatal(err)
		}
		if b, _ := os.ReadFile(filepath.Join(dir, "tree2", "deep", "y.txt")); string(b) != "Y" {
			t.Fatalf("copy = %q", b)
		}
		if goruntime.GOOS != "windows" {
			target, err := os.Readlink(filepath.Join(dir, "tree2", "ln"))
			if err != nil || target != "x.txt" {
				t.Fatalf("link = %q, %v", target, err)
			}
		}
	})
	t.Run("copy refusals", func(t *testing.T) {
		if err := o.Copy(ctx, "tree", "tree/inner"); codeOf(err) != pluginapi.CodeInvalid {
			t.Fatalf("into itself: %v", err)
		}
		if err := o.Copy(ctx, "b.txt", "tree"); codeOf(err) != pluginapi.CodeConflict {
			t.Fatalf("over a directory: %v", err)
		}
		if err := o.Copy(ctx, "none", "x"); codeOf(err) != pluginapi.CodeNotFound {
			t.Fatalf("missing: %v", err)
		}
	})
}

func TestPluginOSWalk(t *testing.T) {
	o, dir := newTestOS(t)
	ctx := context.Background()
	writePluginTree(t, dir, map[string]string{
		"README.md": "r", "src/a.go": "1", "src/deep/b.go": "2", ".git/config": "c", "node_modules/x/y.js": "3", ".dot": "d",
	})
	if goruntime.GOOS != "windows" {
		outside := t.TempDir()
		writePluginTree(t, outside, map[string]string{"secret.txt": "s"})
		if err := os.Symlink(outside, filepath.Join(dir, "zz_escape")); err != nil {
			t.Fatal(err)
		}
	}
	rels := func(res hostos.WalkResult) []string {
		var out []string
		for _, e := range res.Entries {
			out = append(out, e.Rel+":"+e.Kind)
		}
		return out
	}
	rows := []struct {
		name  string
		opts  hostos.WalkOptions
		want  []string
		trunc bool
	}{
		{"skips and hides", hostos.WalkOptions{SkipDirs: []string{"node_modules"}},
			[]string{"README.md:file", "src:dir", "src/a.go:file", "src/deep:dir", "src/deep/b.go:file"}, false},
		{"hidden", hostos.WalkOptions{SkipDirs: []string{"node_modules"}, Hidden: true},
			[]string{".dot:file", ".git:dir", ".git/config:file", "README.md:file", "src:dir", "src/a.go:file", "src/deep:dir", "src/deep/b.go:file"}, false},
		{"depth", hostos.WalkOptions{SkipDirs: []string{"node_modules"}, MaxDepth: 1}, []string{"README.md:file", "src:dir"}, false},
		{"entry limit", hostos.WalkOptions{SkipDirs: []string{"node_modules"}, MaxEntries: 2}, []string{"README.md:file", "src:dir"}, true},
	}
	for _, r := range rows {
		t.Run(r.name, func(t *testing.T) {
			res, err := o.Walk(ctx, ".", r.opts)
			if err != nil {
				t.Fatal(err)
			}
			got := rels(res)
			// the link to a directory outside is listed as a link and never entered
			if goruntime.GOOS != "windows" {
				var kept []string
				for _, g := range got {
					if g == "zz_escape:symlink" {
						continue
					}
					kept = append(kept, g)
				}
				got = kept
			}
			if !reflect.DeepEqual(got, r.want) || res.Truncated != r.trunc {
				t.Fatalf("entries = %v truncated = %v\nwant %v %v", got, res.Truncated, r.want, r.trunc)
			}
		})
	}
	t.Run("never enters a symbolic link", func(t *testing.T) {
		res, err := o.Walk(ctx, ".", hostos.WalkOptions{SkipDirs: []string{"node_modules"}})
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range res.Entries {
			if strings.Contains(e.Rel, "secret.txt") {
				t.Fatalf("walked through a link: %s", e.Rel)
			}
			if !filepath.IsAbs(e.Path) {
				t.Fatalf("path %q is not absolute", e.Path)
			}
		}
	})
	t.Run("missing root", func(t *testing.T) {
		if _, err := o.Walk(ctx, "nope", hostos.WalkOptions{}); codeOf(err) != pluginapi.CodeNotFound {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestPluginOSEnvironment(t *testing.T) {
	o, dir := newTestOS(t)
	t.Setenv("GVIM_PLUGIN_OS_TEST", "yes")
	if v, ok := o.Getenv("GVIM_PLUGIN_OS_TEST"); !ok || v != "yes" {
		t.Fatalf("Getenv = %q, %v", v, ok)
	}
	if _, ok := o.Getenv("GVIM_PLUGIN_OS_NO_SUCH"); ok {
		t.Fatal("found an unset variable")
	}
	if d := o.Dirs(); d.Cwd != dir || d.Root != dir {
		t.Fatalf("Dirs = %+v", d)
	}
	if _, ok := o.LookPath("gvim-no-such-program"); ok {
		t.Fatal("LookPath found nothing")
	}
	if time.Since(o.Now()) > time.Minute {
		t.Fatal("Now")
	}
}
