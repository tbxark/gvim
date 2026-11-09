package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tbxark/gvim/authority"
	"github.com/tbxark/gvim/authority/authoritytest"
	"github.com/tbxark/gvim/internal/lsphost"
	"github.com/tbxark/gvim/plugin/hostos"
)

// slangSlot returns a fake authority slot whose one-shot runs script.
func slangSlot(script func(context.Context, []string, hostos.ProcOptions) (hostos.ProcResult, error)) *authoritytest.Fake {
	slot := authoritytest.New()
	slot.OneShotter = &fakeOneShot{script: script}
	return slot
}

func slangTempHome(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
}

// TestSlangSynthOpenerSuccess checks the safe native flow: one bounded authority
// OneShot using the answering server's actual program and env, no local exec, and a
// generated read-only file.
func TestSlangSynthOpenerSuccess(t *testing.T) {
	slangTempHome(t)
	src := []byte("// core builtins\nfloat3 normalize(float3 x);\n")
	var mu sync.Mutex
	var argv []string
	var pathValue string
	slot := slangSlot(func(_ context.Context, a []string, o hostos.ProcOptions) (hostos.ProcResult, error) {
		mu.Lock()
		argv = append([]string(nil), a...)
		pathValue = o.Env["PATH"]
		mu.Unlock()
		if o.Cwd != "/work/proj" {
			t.Errorf("cwd = %q", o.Cwd)
		}
		return hostos.ProcResult{Code: 0, Stdout: src}, nil
	})
	gateCalls := 0
	opener := newSlangSynthOpener(false, slot, func(_ context.Context, a []string, cwd string) error {
		gateCalls++
		if cwd != "/work/proj" || len(a) < 2 || a[0] != "/opt/slangd" || a[1] != "--print-builtin-module" {
			t.Errorf("gate argv/cwd = %v / %q", a, cwd)
		}
		return nil
	}, nil, "/fallback")
	path, err := opener(context.Background(), lsphost.ExternalLocationRequest{
		URI:     "slang-synth://core/core.builtin",
		Root:    "/work/proj",
		Program: "/opt/slangd",
		Command: []string{"/ignored/wrapper", "lsp"},
		Env:     []string{"PATH=/server/bin"},
	})
	if err != nil {
		t.Fatalf("opener: %v", err)
	}
	if gateCalls != 1 {
		t.Fatalf("gate calls = %d, want 1", gateCalls)
	}
	mu.Lock()
	gotArgv, gotPath := append([]string(nil), argv...), pathValue
	mu.Unlock()
	if len(gotArgv) != 3 || gotArgv[0] != "/opt/slangd" || gotArgv[2] != "core" {
		t.Fatalf("slangd argv = %v, want the answering server's program", gotArgv)
	}
	if gotPath != "/server/bin" {
		t.Fatalf("PATH = %q, want the server's launch env", gotPath)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(src) {
		t.Fatalf("generated file = %q", got)
	}
	if st, err := os.Stat(path); err != nil || st.Mode().Perm()&0o222 != 0 {
		t.Fatalf("generated file is writable: %v", st)
	}
	if !strings.Contains(filepath.Base(path), "core-") || !strings.HasSuffix(path, ".slang") {
		t.Fatalf("cache path = %q, want module-content-hash.slang", path)
	}
	// A remote selector refuses before any authority process.
	remote := newSlangSynthOpener(true, slot, nil, nil, "/fallback")
	if _, err := remote(context.Background(), lsphost.ExternalLocationRequest{URI: "slang-synth://core/core.builtin"}); err == nil || !strings.Contains(err.Error(), "local authority") {
		t.Fatalf("remote opener error = %v, want an explicit local-authority refusal", err)
	}
}

// TestSlangSynthModuleValidation rejects schemes/module authorities that could be
// turned into file paths or extra arguments.
func TestSlangSynthModuleValidation(t *testing.T) {
	cases := map[string]bool{
		"slang-synth://core/core.builtin":     true,
		"slang-synth://core":                  true,
		"slang-synth://slang.core_1-x/core":   true,
		"slang-synth://../core":               false,
		"slang-synth://./core":                false,
		"slang-synth://core/../../etc/passwd": false,
		"slang-synth://core?x=1":              false,
		"slang-synth://core#frag":             false,
		"slang-synth://user@core/core":        false,
		"slang-synth://core:1234/core":        false,
		"slang-synth://":                      false,
		"file:///tmp/deno.json":               false,
		"http://example.com/core":             false,
	}
	for raw, ok := range cases {
		_, err := slangSynthModule(raw)
		if (err == nil) != ok {
			t.Errorf("slangSynthModule(%q) err = %v, want ok=%v", raw, err, ok)
		}
	}
}

// TestSlangSynthOpenerFailures checks trust refusal and every bounded/failed process
// result is an error, with no source file installed.
func TestSlangSynthOpenerFailures(t *testing.T) {
	slangTempHome(t)
	cases := map[string]struct {
		gate   authority.Gate
		result hostos.ProcResult
		want   string
	}{
		"trust gate":  {gate: func(context.Context, []string, string) error { return errors.New("untrusted") }, want: "untrusted"},
		"non-zero":    {result: hostos.ProcResult{Code: 2, Stderr: []byte("bad module")}, want: "exited 2"},
		"timeout":     {result: hostos.ProcResult{TimedOut: true}, want: "timeout"},
		"signal":      {result: hostos.ProcResult{Signal: "killed"}, want: "signal"},
		"truncated":   {result: hostos.ProcResult{Truncated: true}, want: "output over the cap"},
		"start error": {result: hostos.ProcResult{StartErr: "boom"}, want: "start"},
		"cancel":      {result: hostos.ProcResult{Cancel: "stopped"}, want: "cancel"},
		"empty":       {result: hostos.ProcResult{}, want: "printed no source"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			slot := slangSlot(func(context.Context, []string, hostos.ProcOptions) (hostos.ProcResult, error) {
				if tc.gate != nil {
					t.Fatal("the trust gate refusal still ran slangd")
				}
				return tc.result, nil
			})
			opener := newSlangSynthOpener(false, slot, tc.gate, nil, "/work")
			_, err := opener(context.Background(), lsphost.ExternalLocationRequest{
				URI: "slang-synth://core/core.builtin", Program: "/opt/slangd", Root: "/work",
			})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want %q", err, tc.want)
			}
		})
	}
}

// TestSlangBuiltinCacheContentAddressed checks concurrent same-module dumps with different
// content get different read-only paths, so neither can replace the file a goto reads.
func TestSlangBuiltinCacheContentAddressed(t *testing.T) {
	slangTempHome(t)
	type result struct {
		path string
		err  error
	}
	done := make(chan result, 2)
	for _, src := range [][]byte{[]byte("float a;\n"), []byte("float b;\n")} {
		src := src
		go func() {
			path, err := writeSlangBuiltin("core", src)
			done <- result{path: path, err: err}
		}()
	}
	first, second := <-done, <-done
	if first.err != nil || second.err != nil {
		t.Fatalf("write errors = %v / %v", first.err, second.err)
	}
	if first.path == second.path {
		t.Fatal("different module content shared one cache path")
	}
	for _, r := range []result{first, second} {
		data, err := os.ReadFile(r.path)
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != "float a;\n" && string(data) != "float b;\n" {
			t.Fatalf("cache file %q = %q", r.path, data)
		}
	}
}

// TestSlangBuiltinCacheRefusesSymlinkDir checks the Lstat protection: a symlinked
// slang-builtin directory is refused and its target is not written through.
func TestSlangBuiltinCacheRefusesSymlinkDir(t *testing.T) {
	if filepath.Separator == '\\' {
		t.Skip("symlink creation needs extra privileges on Windows")
	}
	slangTempHome(t)
	base, err := os.UserCacheDir()
	if err != nil {
		t.Fatal(err)
	}
	gvimDir := filepath.Join(base, "gvim")
	if err := os.MkdirAll(gvimDir, 0o700); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(gvimDir, "slang-builtin")); err != nil {
		t.Fatal(err)
	}
	if _, err := writeSlangBuiltin("core", []byte("float x;\n")); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("symlinked cache dir error = %v, want refusal", err)
	}
	if entries, err := os.ReadDir(outside); err != nil || len(entries) != 0 {
		t.Fatalf("wrote through cache symlink: %v %v", entries, err)
	}
}

// TestSlangSynthOpenerUsesConfiguredCommandWhenUnresolved checks the documented residual:
// when the manager did not resolve an absolute program, the configured command spelling is
// used (never a hard-coded PATH slangd), while argv beyond the program is not inherited.
func TestSlangSynthOpenerUsesConfiguredCommandWhenUnresolved(t *testing.T) {
	slangTempHome(t)
	var argv []string
	slot := slangSlot(func(_ context.Context, a []string, _ hostos.ProcOptions) (hostos.ProcResult, error) {
		argv = append([]string(nil), a...)
		return hostos.ProcResult{Code: 0, Stdout: []byte("float x;\n")}, nil
	})
	opener := newSlangSynthOpener(false, slot, nil, nil, "/work")
	if _, err := opener(context.Background(), lsphost.ExternalLocationRequest{
		URI:     "slang-synth://core/core.builtin",
		Root:    "/work",
		Command: []string{"configured-slangd", "--some-server-arg"},
	}); err != nil {
		t.Fatalf("opener: %v", err)
	}
	if len(argv) != 3 || argv[0] != "configured-slangd" || argv[2] != "core" {
		t.Fatalf("fallback argv = %v, want configured program + print flag only", argv)
	}
	for _, arg := range argv {
		if arg == "--some-server-arg" {
			t.Fatal("the server wrapper argv was inherited")
		}
	}
}

// TestSlangCacheDirRepairsPermissions checks old cache directories are chmod-ed 0700 on
// first use, so an old permissive cache cannot expose generated source to other users.
func TestSlangCacheDirRepairsPermissions(t *testing.T) {
	if filepath.Separator == '\\' {
		t.Skip("directory mode assertions are POSIX")
	}
	slangTempHome(t)
	base, err := os.UserCacheDir()
	if err != nil {
		t.Fatal(err)
	}
	gvimDir := filepath.Join(base, "gvim")
	inner := filepath.Join(gvimDir, "slang-builtin")
	if err := os.MkdirAll(inner, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(gvimDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(inner, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := writeSlangBuiltin("core", []byte("float x;\n")); err != nil {
		t.Fatalf("write: %v", err)
	}
	for _, dir := range []string{gvimDir, inner} {
		st, err := os.Lstat(dir)
		if err != nil {
			t.Fatal(err)
		}
		if perm := st.Mode().Perm(); perm != 0o700 {
			t.Fatalf("%s mode = %o, want 0700", dir, perm)
		}
	}
}

// TestSlangCacheCleanupAgeAndCapacity checks the bounded background-cleanable cache:
// aged entries are removed, and older-than-grace entries above the entry cap are pruned.
func TestSlangCacheCleanupAgeAndCapacity(t *testing.T) {
	slangTempHome(t)
	dir, err := ensureSlangCacheDir(mustUserCacheDir(t))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	// Age pruning.
	for i := range 4 {
		path := filepath.Join(dir, "aged-"+strconv.Itoa(i)+".slang")
		if err := os.WriteFile(path, []byte("old"), 0o444); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, now.Add(-slangCacheMaxAge-time.Hour), now.Add(-slangCacheMaxAge-time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	cleanupSlangCache(dir)
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("aged cache entries remain: %d", len(entries))
	}
	// Capacity pruning: more than the cap, older than grace but younger than max age.
	total := slangCacheMaxEntries + 20
	for i := range total {
		path := filepath.Join(dir, "cap-"+strconv.Itoa(i)+".slang")
		if err := os.WriteFile(path, []byte("x"), 0o444); err != nil {
			t.Fatal(err)
		}
		when := now.Add(-2 * slangCacheGrace)
		if err := os.Chtimes(path, when, when); err != nil {
			t.Fatal(err)
		}
	}
	cleanupSlangCache(dir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) > slangCacheMaxEntries {
		t.Fatalf("cache has %d entries, cap %d", len(entries), slangCacheMaxEntries)
	}
}

func mustUserCacheDir(t *testing.T) string {
	t.Helper()
	base, err := os.UserCacheDir()
	if err != nil {
		t.Fatal(err)
	}
	return base
}
