package app

// slang-synth external locations. Fresh's slang-lsp plugin answers builtin-module
// go-to-definition targets of the form slang-synth://<module>/<path> by running
// "slangd --print-builtin-module <module>" and opening the generated .slang source at the
// LSP position, read-only. gvim does that natively here with four hard boundaries:
//
//   - URI validation: only the slang-synth scheme; the authority must be one bounded module
//     name. The path is never turned into a filesystem path.
//   - trust/authority: local authority only (a remote host is refused explicitly, never a
//     local fallback). The fixed slangd command runs through authority.OneShot under the
//     workspace trust gate; nothing is spawned with os/exec.
//   - process bounds: 5 s, 4 MiB stdout cap; truncation/timeout/signal/non-zero are errors.
//   - read-only result: the dump is written atomically under the cache directory and the
//     TUI opens it with its generated/read-only marker.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/tbxark/gvim/authority"
	"github.com/tbxark/gvim/internal/lsphost"
	"github.com/tbxark/gvim/plugin/hostos"
)

const (
	slangSynthScheme    = "slang-synth"
	slangSynthTimeout   = 5 * time.Second
	slangSynthMaxOutput = 4 << 20
	slangSynthMaxModule = 64

	// Cache maintenance. Content-addressed files are kept for a bounded age and the
	// directory is capped best-effort at a bounded number of entries. Cleanup runs in a
	// throttled background goroutine, never on the resolver path, and a grace window
	// keeps a file a just-returned goto is about to read.
	slangCacheCleanupInterval = time.Minute
	slangCacheMaxAge          = 7 * 24 * time.Hour
	slangCacheGrace           = time.Hour
	slangCacheMaxEntries      = 128
	slangCacheScanBatch       = 128
)

var slangCacheCleanupMu sync.Mutex
var slangCacheCleanupLast = map[string]time.Time{}

// scheduleSlangCacheCleanup starts at most one cleanup per interval for dir. It never
// waits for the cleanup.
func scheduleSlangCacheCleanup(dir string) {
	now := time.Now()
	slangCacheCleanupMu.Lock()
	if last, ok := slangCacheCleanupLast[dir]; ok && now.Sub(last) < slangCacheCleanupInterval {
		slangCacheCleanupMu.Unlock()
		return
	}
	slangCacheCleanupLast[dir] = now
	slangCacheCleanupMu.Unlock()
	go cleanupSlangCache(dir)
}

// cleanupSlangCache prunes the content-addressed cache in bounded batches: entries older
// than slangCacheMaxAge are removed, and the newest slangCacheMaxEntries are kept when
// older-than-grace entries exceed that cap. Errors are ignored (a missing/locked cache is
// not a resolver failure); a fresh file younger than slangCacheGrace is never deleted so a
// goto that just received its path can still open it.
func cleanupSlangCache(dir string) {
	f, err := os.Open(dir)
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }()
	now := time.Now()
	type entry struct {
		name string
		mod  time.Time
	}
	newest := make([]entry, 0, slangCacheMaxEntries)
	for {
		batch, readErr := f.ReadDir(slangCacheScanBatch)
		for _, de := range batch {
			name := de.Name()
			if !strings.HasSuffix(name, ".slang") && !strings.Contains(name, ".tmp-") {
				continue
			}
			info, err := de.Info()
			if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
				continue
			}
			age := now.Sub(info.ModTime())
			if age > slangCacheMaxAge && age > slangCacheGrace {
				_ = os.Remove(filepath.Join(dir, name))
				continue
			}
			newest = append(newest, entry{name: name, mod: info.ModTime()})
			sort.Slice(newest, func(i, j int) bool { return newest[i].mod.After(newest[j].mod) })
			for len(newest) > slangCacheMaxEntries {
				oldest := newest[len(newest)-1]
				newest = newest[:len(newest)-1]
				if now.Sub(oldest.mod) > slangCacheGrace {
					_ = os.Remove(filepath.Join(dir, oldest.name))
				}
			}
		}
		if readErr != nil {
			break
		}
	}
}

// newSlangSynthOpener returns the native external-location resolver for the app. remote
// is true for a remote/SSH workspace, where this local native flow is deliberately refused.
func newSlangSynthOpener(remote bool, slot authority.Slot, gate authority.Gate, env func() []string, root string) lsphost.ExternalLocationOpener {
	return func(ctx context.Context, req lsphost.ExternalLocationRequest) (string, error) {
		if remote {
			return "", fmt.Errorf("slang-synth: opening builtin modules is only supported on the local authority")
		}
		module, err := slangSynthModule(req.URI)
		if err != nil {
			return "", err
		}
		one := slot.OneShot()
		if one == nil {
			return "", fmt.Errorf("slang-synth: no authority one-shot")
		}
		// Use the binary/environment of the server that answered, not a hard-coded
		// PATH slangd. req.Program is the absolute executable the manager passed to
		// lsp.Start; without it, the configured command spelling is the best available
		// (a documented residual when a wrapper/bare PATH lookup differs).
		program := strings.TrimSpace(req.Program)
		if program == "" && len(req.Command) > 0 {
			program = strings.TrimSpace(req.Command[0])
		}
		if program == "" {
			return "", fmt.Errorf("slang-synth: no answering server program to run")
		}
		cwd := strings.TrimSpace(req.Root)
		if cwd == "" {
			cwd = root
		}
		argv := []string{program, "--print-builtin-module", module}
		if gate != nil {
			if err := gate(ctx, argv, cwd); err != nil {
				return "", fmt.Errorf("slang-synth: %w", err)
			}
		}
		opts := hostos.ProcOptions{Cwd: cwd, MaxOutput: slangSynthMaxOutput}
		envEntries := req.Env
		if len(envEntries) == 0 && env != nil {
			envEntries = env()
		}
		if len(envEntries) > 0 {
			opts.Env = remoteEnvMap(envEntries)
		}
		runCtx, cancel := context.WithTimeout(ctx, slangSynthTimeout)
		defer cancel()
		res, err := one.Run(runCtx, argv, opts)
		if err != nil {
			return "", fmt.Errorf("slang-synth: running slangd: %w", err)
		}
		if res.TimedOut || res.Signal != "" || res.StartErr != "" || res.Cancel != "" || res.Truncated {
			return "", fmt.Errorf("slang-synth: slangd did not produce a bounded complete module: %s", slangSynthResultReason(res))
		}
		if res.Code != 0 {
			return "", fmt.Errorf("slang-synth: slangd --print-builtin-module %s exited %d: %s", module, res.Code, slangSynthFirstLine(res.Stderr))
		}
		if len(res.Stdout) == 0 {
			return "", fmt.Errorf("slang-synth: slangd printed no source for module %q", module)
		}
		return writeSlangBuiltin(module, res.Stdout)
	}
}

// slangSynthModule parses slang-synth://<module>/<anything>. The authority is the only
// input used; a path, userinfo, port, query or fragment is rejected instead of guessed.
func slangSynthModule(rawURI string) (string, error) {
	u, err := url.Parse(rawURI)
	if err != nil {
		return "", fmt.Errorf("slang-synth: parse %q: %w", rawURI, err)
	}
	if !strings.EqualFold(u.Scheme, slangSynthScheme) {
		return "", fmt.Errorf("slang-synth: unsupported URI scheme %q", u.Scheme)
	}
	if u.User != nil || u.Port() != "" || u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("slang-synth: %q carries userinfo, port, query or fragment", rawURI)
	}
	for _, seg := range strings.Split(u.Path, "/") {
		if seg == ".." || seg == "." || strings.ContainsRune(seg, '\\') {
			return "", fmt.Errorf("slang-synth: %q has an unsafe path", rawURI)
		}
	}
	module := u.Host
	if module == "" || module == "." || module == ".." || len(module) > slangSynthMaxModule {
		return "", fmt.Errorf("slang-synth: %q has no valid module authority", rawURI)
	}
	for i, r := range module {
		allowed := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '_' || r == '-'
		if !allowed {
			return "", fmt.Errorf("slang-synth: module %q has an invalid character", module)
		}
		if i == 0 && (r == '.' || r == '-') {
			return "", fmt.Errorf("slang-synth: module %q must start with a letter, digit or underscore", module)
		}
	}
	return module, nil
}

// ensureSlangCacheDir creates <base>/gvim/slang-builtin and refuses to traverse a
// symlink at either component. The directory is private (0700); the content-addressed
// files inside are read-only. This keeps a hostile or accidentally linked cache path
// from redirecting the generated-source write to an arbitrary directory.
func ensureSlangCacheDir(base string) (string, error) {
	if err := os.MkdirAll(base, 0o700); err != nil {
		return "", fmt.Errorf("slang-synth: create cache root %q: %w", base, err)
	}
	for _, path := range []string{filepath.Join(base, "gvim"), filepath.Join(base, "gvim", "slang-builtin")} {
		st, err := os.Lstat(path)
		switch {
		case err == nil:
			if st.Mode()&os.ModeSymlink != 0 {
				return "", fmt.Errorf("slang-synth: cache directory %q is a symlink", path)
			}
			if !st.IsDir() {
				return "", fmt.Errorf("slang-synth: cache path %q is not a directory", path)
			}
			if err := os.Chmod(path, 0o700); err != nil {
				return "", fmt.Errorf("slang-synth: secure cache directory %q: %w", path, err)
			}
		case errors.Is(err, fs.ErrNotExist):
			if err := os.Mkdir(path, 0o700); err != nil && !errors.Is(err, fs.ErrExist) {
				return "", fmt.Errorf("slang-synth: create cache directory %q: %w", path, err)
			}
			st, err := os.Lstat(path)
			if err != nil {
				return "", fmt.Errorf("slang-synth: cache directory %q: %w", path, err)
			}
			if st.Mode()&os.ModeSymlink != 0 {
				return "", fmt.Errorf("slang-synth: cache directory %q is a symlink", path)
			}
			if !st.IsDir() {
				return "", fmt.Errorf("slang-synth: cache path %q is not a directory", path)
			}
			if err := os.Chmod(path, 0o700); err != nil {
				return "", fmt.Errorf("slang-synth: secure cache directory %q: %w", path, err)
			}
		default:
			return "", fmt.Errorf("slang-synth: cache directory %q: %w", path, err)
		}
	}
	return filepath.Join(base, "gvim", "slang-builtin"), nil
}

// writeSlangBuiltin writes the dump atomically under the user cache and makes the file
// read-only. It always regenerates (no version-keyed stale content): the line numbers of the
// target URI belong to the slangd that just answered the LSP request.
func writeSlangBuiltin(module string, src []byte) (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("slang-synth: cache directory: %w", err)
	}
	dir, err := ensureSlangCacheDir(base)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(src)
	// Content-addressed: two concurrent resolutions of the same module with different
	// source (different slangd versions/roots) get distinct paths, so neither can
	// replace the file a goto is about to read and an already-open Document is reused
	// only for identical bytes.
	target := filepath.Join(dir, module+"-"+hex.EncodeToString(sum[:])+".slang")
	if st, err := os.Lstat(target); err == nil {
		if st.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("slang-synth: cache target %q is a symlink", target)
		}
		if !st.Mode().IsRegular() {
			return "", fmt.Errorf("slang-synth: cache target %q is not a regular file", target)
		}
		scheduleSlangCacheCleanup(dir)
		return target, nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return "", fmt.Errorf("slang-synth: cache target: %w", err)
	}
	f, err := os.CreateTemp(dir, "."+module+".tmp-*")
	if err != nil {
		return "", fmt.Errorf("slang-synth: create cache file: %w", err)
	}
	tmp := f.Name()
	cleanup := func() {
		_ = f.Close()
		_ = os.Remove(tmp)
	}
	if _, err := f.Write(src); err != nil {
		cleanup()
		return "", fmt.Errorf("slang-synth: write cache file: %w", err)
	}
	if err := f.Chmod(0o444); err != nil {
		cleanup()
		return "", fmt.Errorf("slang-synth: make cache file read-only: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return "", fmt.Errorf("slang-synth: close cache file: %w", err)
	}
	if err := os.Rename(tmp, target); err != nil {
		_ = os.Remove(tmp)
		return "", fmt.Errorf("slang-synth: install cache file: %w", err)
	}
	scheduleSlangCacheCleanup(dir)
	return target, nil
}

func slangSynthResultReason(res hostos.ProcResult) string {
	switch {
	case res.TimedOut:
		return "timeout"
	case res.Signal != "":
		return "signal " + res.Signal
	case res.StartErr != "":
		return "start: " + res.StartErr
	case res.Cancel != "":
		return "cancel: " + res.Cancel
	case res.Truncated:
		return "output over the cap"
	}
	return "unknown"
}

func slangSynthFirstLine(b []byte) string {
	line := strings.TrimSpace(string(b))
	if i := strings.IndexByte(line, '\n'); i >= 0 {
		line = line[:i]
	}
	if len(line) > 240 {
		line = line[:240]
	}
	if line == "" {
		return "(no stderr)"
	}
	return line
}
