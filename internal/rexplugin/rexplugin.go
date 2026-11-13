// Package rexplugin installs gvim's Rex integration: a Rex configuration plugin
// that opens gvim in a floating layer over the session, at the working
// directory of the pane it was called from.
//
// The installer owns two paths: a Lua file under Rex's config directory and the
// managed block in Rex's init.lua that loads it. Both carry ownMarker, and a
// file without it is never replaced or removed.
package rexplugin

import (
	"bytes"
	_ "embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/tbxark/gvim/atomicfile"
)

// rexSource is the Rex plugin, which starts with ownMarker.

//go:embed rex.lua
var rexSource []byte

const (
	beginMarker = "-- >>> gvim >>>"
	endMarker   = "-- <<< gvim <<<"
	// ownMarker starts the installed Lua file. A file without it was not
	// written by this package.
	ownMarker = "-- Installed by `gvim rex install`"
)

// Installer writes and removes the integration. The zero value uses the rex
// found for this user, the user's config directories, and the running gvim.
type Installer struct {
	// Rex runs the rex command with args and returns its output. Nil runs the
	// rex found on PATH, or the one in REX_BIN_DIR.
	Rex func(args ...string) (string, error)
	// Bin is the gvim path recorded in the Rex plugin. Empty uses the running
	// binary.
	Bin string
	// RexConfig overrides Rex's init.lua. Empty resolves it from the
	// environment.
	RexConfig string
}

// Report names what Install and Uninstall touched.
type Report struct {
	RexConfig string
	RexPlugin string
	// Changed is false when every file already held what it would hold.
	Changed bool
	// Reloaded is true when `rex config reload` accepted the new file.
	Reloaded bool
}

// Install writes the plugin and the managed block that loads it. It asks rex to
// check the config and restores init.lua when the check fails.
func (i Installer) Install() (Report, error) {
	rep, err := i.paths()
	if err != nil {
		return rep, err
	}
	bin := i.Bin
	if bin == "" {
		exe, err := os.Executable()
		if err != nil {
			return rep, fmt.Errorf("gvim rex: finding this binary: %w", err)
		}
		bin = exe
	}
	previous, existed, err := readFile(rep.RexConfig)
	if err != nil {
		return rep, err
	}
	// Refuse a file this package did not write before writing anything, so a
	// refused install changes nothing.
	if err := checkOwned(rep.RexPlugin); err != nil {
		return rep, err
	}
	next, err := withBlock(previous, loader(rep.RexPlugin, bin))
	if err != nil {
		return rep, err
	}
	changed, err := writeOwned(rep.RexPlugin, rexSource)
	if err != nil {
		return rep, err
	}
	rep.Changed = changed
	if next != previous {
		if err := writeFile(rep.RexConfig, next); err != nil {
			return rep, err
		}
		rep.Changed = true
	}
	if _, err := i.rex("config", "check", rep.RexConfig); err != nil {
		restore(rep.RexConfig, previous, existed)
		return rep, fmt.Errorf("gvim rex: rex rejected the new init.lua: %w", err)
	}
	if rep.Changed {
		_, err := i.rex("config", "reload")
		rep.Reloaded = err == nil
	}
	return rep, nil
}

// Uninstall removes the managed block and the plugin, then checks and reloads
// Rex's config. A file it did not write is left alone.
func (i Installer) Uninstall() (Report, error) {
	rep, err := i.paths()
	if err != nil {
		return rep, err
	}
	previous, existed, err := readFile(rep.RexConfig)
	if err != nil {
		return rep, err
	}
	next, err := withoutBlock(previous)
	if err != nil {
		return rep, err
	}
	if next != previous {
		if err := writeFile(rep.RexConfig, next); err != nil {
			return rep, err
		}
		rep.Changed = true
		if _, err := i.rex("config", "check", rep.RexConfig); err != nil {
			restore(rep.RexConfig, previous, existed)
			return rep, fmt.Errorf("gvim rex: rex rejected the new init.lua: %w", err)
		}
	}
	if err := removeOwned(rep.RexPlugin); err != nil {
		return rep, err
	}
	// Its folder too, unless something else was put there.
	_ = os.Remove(filepath.Dir(rep.RexPlugin))
	if rep.Changed {
		_, err := i.rex("config", "reload")
		rep.Reloaded = err == nil
	}
	return rep, nil
}

// paths resolves the two paths the installer writes.
func (i Installer) paths() (Report, error) {
	configPath := i.RexConfig
	if configPath == "" {
		p, err := rexConfigPath()
		if err != nil {
			return Report{}, err
		}
		configPath = p
	}
	return Report{
		RexConfig: configPath,
		RexPlugin: filepath.Join(filepath.Dir(configPath), "plugins", "gvim", "init.lua"),
	}, nil
}

// rex runs one rex command and returns its trimmed output.
func (i Installer) rex(args ...string) (string, error) {
	if i.Rex != nil {
		return i.Rex(args...)
	}
	bin := rexBinary()
	if bin == "" {
		return "", errors.New("rex is not on PATH; install Rex, or put rex on PATH")
	}
	out, err := exec.Command(bin, args...).CombinedOutput()
	text := strings.TrimSpace(string(out))
	if err != nil {
		if text != "" {
			return text, fmt.Errorf("%s: %w", text, err)
		}
		return "", err
	}
	return text, nil
}

// rexConfigPath is Rex's init.lua: REX_CONFIG, else the account's config
// directory, as `rex config` describes it.
func rexConfigPath() (string, error) {
	if p := os.Getenv("REX_CONFIG"); p != "" {
		return p, nil
	}
	if runtime.GOOS == "windows" {
		base, err := os.UserConfigDir()
		if err != nil {
			return "", fmt.Errorf("gvim rex: user config dir: %w", err)
		}
		return filepath.Join(base, "rex", "init.lua"), nil
	}
	if base := os.Getenv("XDG_CONFIG_HOME"); filepath.IsAbs(base) {
		return filepath.Join(base, "rex", "init.lua"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("gvim rex: home directory: %w", err)
	}
	return filepath.Join(home, ".config", "rex", "init.lua"), nil
}

// rexBinary is the rex of PATH, or the one beside the running Rex server.
func rexBinary() string {
	if p, err := exec.LookPath("rex"); err == nil {
		return p
	}
	if dir := os.Getenv("REX_BIN_DIR"); dir != "" {
		p := filepath.Join(dir, "rex")
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// loader is the managed block: comments, then the call that loads the plugin.
func loader(pluginPath, bin string) string {
	return strings.Join([]string{
		beginMarker,
		"-- Managed by `gvim rex install`; `gvim rex uninstall` removes it.",
		"dofile(" + luaString(pluginPath) + ").setup({",
		"  bin = { " + luaString(bin) + " },",
		`  keys = { open = "cmd+shift+e" },`,
		"})",
		endMarker,
	}, "\n") + "\n"
}

// withBlock puts block where the managed block is, or after the content. An
// empty block removes it. It fails, leaving the content to the caller untouched,
// when the markers are not one begin followed by one end.
func withBlock(content, block string) (string, error) {
	start, end, ok, err := blockBounds(content)
	if err != nil {
		return "", err
	}
	if ok {
		return content[:start] + block + strings.TrimPrefix(content[end:], "\n"), nil
	}
	if block == "" {
		return content, nil
	}
	if strings.TrimSpace(content) == "" {
		return block, nil
	}
	return strings.TrimRight(content, "\n") + "\n\n" + block, nil
}

// withoutBlock is withBlock with no block: the managed block is removed,
// along with the blank line withBlock put before it.
func withoutBlock(content string) (string, error) {
	start, end, ok, err := blockBounds(content)
	if err != nil || !ok {
		return content, err
	}
	before := content[:start]
	if strings.HasSuffix(before, "\n\n") {
		before = before[:len(before)-1]
	}
	return before + strings.TrimPrefix(content[end:], "\n"), nil
}

// blockBounds returns the offsets of the managed block, endMarker included. A
// content with no marker has no block. Markers that are not exactly one begin
// followed by one end are an error: replacing from a stray begin would swallow
// the text the user wrote around it.
func blockBounds(content string) (start, end int, ok bool, err error) {
	begins, ends := strings.Count(content, beginMarker), strings.Count(content, endMarker)
	if begins == 0 && ends == 0 {
		return 0, 0, false, nil
	}
	start, stop := strings.Index(content, beginMarker), strings.Index(content, endMarker)
	if begins != 1 || ends != 1 || stop < start {
		return 0, 0, false, errors.New("gvim rex: the Rex config has unbalanced \"" + beginMarker + "\" / \"" + endMarker + "\" markers; fix or remove them first")
	}
	return start, stop + len(endMarker), true, nil
}

// luaString quotes s for a Lua string literal.
func luaString(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return `"` + s + `"`
}

// readFile returns the file's text and whether it exists.
func readFile(path string) (string, bool, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return string(data), true, nil
}

// writeFile writes data, and does nothing when the file already holds it.
func writeFile(path, data string) error {
	if old, err := os.ReadFile(path); err == nil && string(old) == data {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	_, err := atomicfile.WriteFile(path, []byte(data))
	return err
}

// restore puts back what readFile returned, removing a file that was absent.
func restore(path, previous string, existed bool) {
	if existed {
		_ = writeFile(path, previous)
		return
	}
	_ = os.Remove(path)
}

// checkOwned fails when path exists and this package did not write it.
func checkOwned(path string) error {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !bytes.HasPrefix(data, []byte(ownMarker)) {
		return errNotOwned(path)
	}
	return nil
}

// errNotOwned is the refusal to replace a file this package did not write.
func errNotOwned(path string) error {
	return fmt.Errorf("gvim rex: %s exists and was not installed by this command; move it first", path)
}

// writeOwned writes a Lua source, refusing a file this package did not write,
// and reports whether the file changed.
func writeOwned(path string, data []byte) (bool, error) {
	old, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return false, err
	}
	if err == nil {
		if !bytes.HasPrefix(old, []byte(ownMarker)) {
			return false, errNotOwned(path)
		}
		if bytes.Equal(old, data) {
			return false, nil
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}
	_, err = atomicfile.WriteFile(path, data)
	return err == nil, err
}

// removeOwned deletes a file this package wrote, and leaves any other alone.
func removeOwned(path string) error {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !bytes.HasPrefix(data, []byte(ownMarker)) {
		return nil
	}
	return os.Remove(path)
}
