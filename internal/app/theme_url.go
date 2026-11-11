package app

// URL themes: a theme value may be an http(s) URL pointing at a theme JSON file.
// Fresh's URL themes are packages installed by its package manager; gvim's slice
// fetches one JSON file at startup, under the update check's transport policy (per
// updatecheck.ValidateURL: https, or http only on the loopback interface), with a
// bounded request and response, validates it through the same theme parser and
// resolver user themes use, and caches it under the user cache dir keyed by the
// URL hash. A fresh cache avoids the request; a failed fetch uses the cached copy
// (last-known-good) with a warning. The runtime Settings theme path never fetches:
// theme.Registry.Load(URL) reads the cache only, so no Update ever blocks on the
// network. A URL-packaged repository theme (Fresh's ".../repo#name" form) is
// rejected in theme.ValidateForm and remains a recorded residual (no package
// manager / marketplace, docs/themes.md).

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/tbxark/gvim/atomicfile"
	"github.com/tbxark/gvim/config"
	"github.com/tbxark/gvim/theme"
	"github.com/tbxark/gvim/updatecheck"
)

const (
	// themeURLTimeout bounds one theme fetch, like the update check's request.
	themeURLTimeout = 8 * time.Second
	// maxThemeURLBytes bounds the theme body. Theme JSON is kilobytes; anything
	// larger is not a theme file and would only grow the cache.
	maxThemeURLBytes = 256 << 10
	// themeURLCacheTTL is how long a cached URL theme is used without asking the
	// server again. A startup with a fresh cache makes no request; a stale one is
	// refreshed, falling back to the same cached copy on failure.
	themeURLCacheTTL = 24 * time.Hour
)

// themeURLClient fetches URL themes; a test replaces it to observe requests or
// simulate a transport failure. It is never used from the Update loop: only the
// startup theme resolution and runtime cache reads exist.
var themeURLClient = &http.Client{Timeout: themeURLTimeout}

// themeURLCacheDir is where URL themes are cached for this launch:
// <user cache dir>/gvim/theme-cache. Move old entries individually so existing
// cache entries do not hide other themes in the old directory.
func themeURLCacheDir() (string, error) {
	dir, err := config.CacheDir()
	if err != nil {
		return "", fmt.Errorf("theme: user cache dir: %w", err)
	}
	dest := theme.URLCacheDir(dir)
	if old, err := config.Dir(); err == nil {
		old = theme.URLCacheDir(old)
		if entries, err := os.ReadDir(old); err == nil {
			for _, entry := range entries {
				if entry.Type().IsRegular() {
					_ = config.MoveLegacyFile(filepath.Join(old, entry.Name()), filepath.Join(dest, entry.Name()))
				}
			}
		}
	}
	return dest, nil
}

// themeURLPolicy applies the update check's transport policy to a theme URL and
// returns it cleaned for the request and the cache key.
func themeURLPolicy(ref string) (string, error) {
	clean, err := updatecheck.ValidateURL(strings.TrimSpace(ref))
	if err != nil {
		return "", fmt.Errorf("theme: %s", strings.TrimPrefix(err.Error(), "update check: "))
	}
	return clean, nil
}

// effectiveThemeRef is the theme value this launch resolves: the flag, else the
// config theme, else the default.
func effectiveThemeRef(flag string, cfg config.Config) string {
	if ref := strings.TrimSpace(flag); ref != "" {
		return ref
	}
	if ref := strings.TrimSpace(cfg.Theme); ref != "" {
		return ref
	}
	return "dark"
}

// fetchURLTheme resolves a URL theme at startup. It validates the transport
// policy, then: a cached body younger than themeURLCacheTTL is used without a
// request; otherwise the URL is fetched (bounded size and time), validated through
// the theme parser/resolver and written to the cache atomically; on failure the
// cached copy is the last-known-good fallback and the returned warning says so. No
// cache and no fetch is an error the caller turns into a clear notice and the
// default theme.
func fetchURLTheme(ctx context.Context, ref string, reg *theme.Registry) (theme.Theme, string, error) {
	clean, err := themeURLPolicy(ref)
	if err != nil {
		return theme.Theme{}, "", err
	}
	cacheDir, err := themeURLCacheDir()
	if err != nil {
		return theme.Theme{}, "", err
	}
	reg.SetURLCacheDir(cacheDir)
	path := theme.URLCachePath(cacheDir, clean)

	cached, cachedOK, cachedFresh := readCachedURLTheme(reg, clean, path)
	if cachedOK && cachedFresh {
		return cached, "", nil
	}
	body, fetchErr := fetchThemeURLBody(ctx, clean)
	if fetchErr == nil {
		if th, verr := reg.LoadURLBody(clean, body); verr == nil {
			warning := ""
			if err := writeThemeURLCache(path, body); err != nil {
				warning = fmt.Sprintf("theme: %s: using the fetched theme; the cache was not written: %v", clean, err)
			}
			return th, warning, nil
		} else {
			fetchErr = verr
		}
	}
	if cachedOK {
		return cached, fmt.Sprintf("theme: %s: %v; using the cached copy", clean, fetchErr), nil
	}
	return theme.Theme{}, "", fmt.Errorf("theme: %s: %w", clean, fetchErr)
}

// readCachedURLTheme loads the cached URL theme, reporting whether it exists and
// parses, and whether it is young enough to skip the request.
func readCachedURLTheme(reg *theme.Registry, ref, path string) (theme.Theme, bool, bool) {
	fi, err := os.Stat(path)
	if err != nil {
		return theme.Theme{}, false, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return theme.Theme{}, false, false
	}
	th, err := reg.LoadURLBody(ref, data)
	if err != nil {
		return theme.Theme{}, false, false
	}
	return th, true, time.Since(fi.ModTime()) < themeURLCacheTTL
}

// fetchThemeURLBody is the one bounded request of a URL theme fetch.
func fetchThemeURLBody(ctx context.Context, ref string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ref, nil)
	if err != nil {
		return nil, fmt.Errorf("theme: %s: %w", ref, err)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := themeURLClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("theme: %s: %w", ref, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("theme: %s: HTTP %s", ref, resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxThemeURLBytes+1))
	if err != nil {
		return nil, fmt.Errorf("theme: %s: read: %w", ref, err)
	}
	if len(data) > maxThemeURLBytes {
		return nil, fmt.Errorf("theme: %s: the theme body is larger than %d bytes", ref, maxThemeURLBytes)
	}
	return data, nil
}

// writeThemeURLCache writes a fetched theme body atomically under the cache dir.
func writeThemeURLCache(path string, body []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("theme cache: %w", err)
	}
	if _, err := atomicfile.WriteFileMode(path, body, 0o600); err != nil {
		return fmt.Errorf("theme cache: %w", err)
	}
	return nil
}

var _ = errors.Is
