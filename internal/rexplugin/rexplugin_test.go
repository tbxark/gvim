package rexplugin

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yuin/gopher-lua/parse"
)

// isolate points the config directories at a temp dir and clears REX_CONFIG.
func isolate(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("REX_CONFIG", "")
	return home
}

// fakeRex records the rex commands and fails the ones named by failOn.
func fakeRex(calls *[][]string, failOn string) func(args ...string) (string, error) {
	return func(args ...string) (string, error) {
		*calls = append(*calls, args)
		if failOn != "" && strings.Join(args, " ") == failOn {
			return "boom", errors.New("boom")
		}
		return "", nil
	}
}

func readText(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return string(data)
}

func TestSourceParses(t *testing.T) {
	if !bytes.HasPrefix(rexSource, []byte(ownMarker)) {
		t.Errorf("rex.lua does not start with %q", ownMarker)
	}
	if _, err := parse.Parse(bytes.NewReader(rexSource), "rex.lua"); err != nil {
		t.Errorf("rex.lua does not parse: %v", err)
	}
}

func TestInstallWritesThePluginAndTheLoader(t *testing.T) {
	home := isolate(t)
	var calls [][]string
	installer := Installer{Rex: fakeRex(&calls, ""), Bin: "/opt/gvim/bin/gvim"}

	rep, err := installer.Install()
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if want := filepath.Join(home, ".config", "rex", "init.lua"); rep.RexConfig != want {
		t.Errorf("RexConfig = %q, want %q", rep.RexConfig, want)
	}
	if want := filepath.Join(home, ".config", "rex", "plugins", "gvim", "init.lua"); rep.RexPlugin != want {
		t.Errorf("RexPlugin = %q, want %q", rep.RexPlugin, want)
	}
	if !rep.Changed || !rep.Reloaded {
		t.Errorf("Changed = %v, Reloaded = %v, want both true", rep.Changed, rep.Reloaded)
	}
	if got := readText(t, rep.RexPlugin); got != string(rexSource) {
		t.Error("the plugin does not hold the embedded source")
	}
	config := readText(t, rep.RexConfig)
	for _, want := range []string{
		beginMarker,
		endMarker,
		`dofile("` + rep.RexPlugin + `").setup({`,
		`bin = { "/opt/gvim/bin/gvim" }`,
	} {
		if !strings.Contains(config, want) {
			t.Errorf("init.lua lacks %q:\n%s", want, config)
		}
	}
	if got := strings.Count(config, beginMarker); got != 1 {
		t.Errorf("init.lua has %d managed blocks, want 1", got)
	}
	if len(calls) != 2 || strings.Join(calls[0], " ") != "config check "+rep.RexConfig || strings.Join(calls[1], " ") != "config reload" {
		t.Errorf("rex calls = %q", calls)
	}
}

func TestInstallKeepsExistingConfigAndIsIdempotent(t *testing.T) {
	home := isolate(t)
	configPath := filepath.Join(home, ".config", "rex", "init.lua")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatal(err)
	}
	const original = "rex.bind(\"cmd+shift+g\", \"open_git\")\n"
	if err := os.WriteFile(configPath, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	var calls [][]string
	installer := Installer{Rex: fakeRex(&calls, ""), Bin: "/bin/gvim"}

	first, err := installer.Install()
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	second, err := installer.Install()
	if err != nil {
		t.Fatalf("second Install: %v", err)
	}
	if second.Changed {
		t.Error("a second install changed the config")
	}
	config := readText(t, configPath)
	if !strings.HasPrefix(config, original) {
		t.Errorf("install rewrote the existing config:\n%s", config)
	}
	if got := strings.Count(config, beginMarker); got != 1 {
		t.Errorf("init.lua has %d managed blocks, want 1", got)
	}
	if first.RexPlugin != second.RexPlugin {
		t.Error("the plugin paths differ between installs")
	}
}

// TestInstallReloadsWhenOnlyThePluginChanged: the managed block stays the same
// while the plugin source moves, and Rex still has to read the new source.
func TestInstallReloadsWhenOnlyThePluginChanged(t *testing.T) {
	isolate(t)
	var calls [][]string
	installer := Installer{Rex: fakeRex(&calls, ""), Bin: "/bin/gvim"}
	rep, err := installer.Install()
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if err := os.WriteFile(rep.RexPlugin, []byte(ownMarker+"\nold\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	calls = nil
	again, err := installer.Install()
	if err != nil {
		t.Fatalf("second Install: %v", err)
	}
	if !again.Changed || !again.Reloaded {
		t.Errorf("Changed = %v, Reloaded = %v, want both true", again.Changed, again.Reloaded)
	}
	if got := readText(t, rep.RexPlugin); got != string(rexSource) {
		t.Error("the plugin was not rewritten")
	}
	if len(calls) != 2 || strings.Join(calls[1], " ") != "config reload" {
		t.Errorf("rex calls = %q, want a reload", calls)
	}
}

func TestInstallRefusesAFileItDidNotWrite(t *testing.T) {
	home := isolate(t)
	plugin := filepath.Join(home, ".config", "rex", "plugins", "gvim", "init.lua")
	if err := os.MkdirAll(filepath.Dir(plugin), 0o755); err != nil {
		t.Fatal(err)
	}
	const mine = "print(\"mine\")\n"
	if err := os.WriteFile(plugin, []byte(mine), 0o644); err != nil {
		t.Fatal(err)
	}
	var calls [][]string
	_, err := (Installer{Rex: fakeRex(&calls, ""), Bin: "/bin/gvim"}).Install()
	if err == nil || !strings.Contains(err.Error(), "move it first") {
		t.Fatalf("Install error = %v, want a refusal", err)
	}
	if got := readText(t, plugin); got != mine {
		t.Error("the existing plugin was overwritten")
	}
	configPath := filepath.Join(home, ".config", "rex", "init.lua")
	if _, err := os.Stat(configPath); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a refused install wrote init.lua (err = %v)", err)
	}
}

func TestInstallRestoresTheConfigWhenRexRejectsIt(t *testing.T) {
	home := isolate(t)
	configPath := filepath.Join(home, ".config", "rex", "init.lua")
	var calls [][]string
	installer := Installer{Rex: fakeRex(&calls, "config check "+configPath), Bin: "/bin/gvim"}

	if _, err := installer.Install(); err == nil {
		t.Fatal("Install succeeded, want a config check failure")
	}
	if _, err := os.Stat(configPath); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("init.lua still exists after a failed check (err = %v)", err)
	}
}

func TestUninstallRemovesEverythingItWrote(t *testing.T) {
	home := isolate(t)
	configPath := filepath.Join(home, ".config", "rex", "init.lua")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatal(err)
	}
	const original = "rex.bind(\"cmd+shift+g\", \"open_git\")\n"
	if err := os.WriteFile(configPath, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	var calls [][]string
	installer := Installer{Rex: fakeRex(&calls, ""), Bin: "/bin/gvim"}
	rep, err := installer.Install()
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	after, err := installer.Uninstall()
	if err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	if !after.Changed || !after.Reloaded {
		t.Errorf("Changed = %v, Reloaded = %v, want both true", after.Changed, after.Reloaded)
	}
	if got := readText(t, configPath); got != original {
		t.Errorf("init.lua = %q, want the original %q", got, original)
	}
	if _, err := os.Stat(rep.RexPlugin); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("%s still exists (err = %v)", rep.RexPlugin, err)
	}
	if _, err := os.Stat(filepath.Dir(rep.RexPlugin)); !errors.Is(err, os.ErrNotExist) {
		t.Error("the Rex plugin folder still exists")
	}
}

func TestUninstallLeavesAFileItDidNotWrite(t *testing.T) {
	home := isolate(t)
	plugin := filepath.Join(home, ".config", "rex", "plugins", "gvim", "init.lua")
	if err := os.MkdirAll(filepath.Dir(plugin), 0o755); err != nil {
		t.Fatal(err)
	}
	const mine = "-- mine\n"
	if err := os.WriteFile(plugin, []byte(mine), 0o644); err != nil {
		t.Fatal(err)
	}
	var calls [][]string
	if _, err := (Installer{Rex: fakeRex(&calls, ""), Bin: "/bin/gvim"}).Uninstall(); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	if got := readText(t, plugin); got != mine {
		t.Error("Uninstall removed a file it did not write")
	}
}

func TestWithBlockReplacesAndRemoves(t *testing.T) {
	const block = beginMarker + "\nold\n" + endMarker + "\n"
	const next = beginMarker + "\nnew\n" + endMarker + "\n"
	if got := withBlock("keep\n"+block, next); got != "keep\n"+next {
		t.Errorf("withBlock replacement = %q", got)
	}
	if got := withBlock("", block); got != block {
		t.Errorf("withBlock on empty = %q", got)
	}
	if got := withBlock("keep\n", block); got != "keep\n\n"+block {
		t.Errorf("withBlock append = %q", got)
	}
	if got := withoutBlock("keep\n\n" + block + "after\n"); got != "keep\nafter\n" {
		t.Errorf("withoutBlock = %q", got)
	}
	if got := withoutBlock("keep\n"); got != "keep\n" {
		t.Errorf("withoutBlock without a block = %q", got)
	}
}
