package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/tbxark/gvim/atomicfile"
	"github.com/tbxark/gvim/config"
	"github.com/tbxark/gvim/internal/tui"
	"github.com/tbxark/gvim/theme"
)

// userThemeDir is config.Dir()/themes: where user themes live and where the
// theme editor saves. An unknown config dir means bundled themes only.
func userThemeDir() string {
	dir, err := config.Dir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "themes")
}

// themeCatalog is internal/app's implementation of the TUI's ThemeCatalog over
// theme.Registry. The registry owns discovery, config forms, extends and
// fallbacks; the app owns the atomic file write, because the theme package is a
// leaf and may not import atomicfile.
type themeCatalog struct {
	reg *theme.Registry
}

var _ tui.ThemeCatalog = (*themeCatalog)(nil)

// newThemeCatalog returns a catalog over reg. A nil reg is an embedded-only
// registry.
func newThemeCatalog(reg *theme.Registry) *themeCatalog {
	if reg == nil {
		reg = theme.NewRegistry("")
	}
	return &themeCatalog{reg: reg}
}

func (c *themeCatalog) Infos() []tui.ThemeInfo {
	infos := c.reg.Infos()
	out := make([]tui.ThemeInfo, len(infos))
	for i, info := range infos {
		out[i] = tui.ThemeInfo{Name: info.Name, Ref: info.Ref, Builtin: info.Builtin}
	}
	return out
}

func (c *themeCatalog) Load(ref string) (theme.Theme, error) { return c.reg.Load(ref) }

func (c *themeCatalog) Canonical(ref string) (string, error) { return c.reg.Canonical(ref) }

func (c *themeCatalog) Reload() error { return c.reg.Reload() }

func (c *themeCatalog) Warnings() []string { return c.reg.Warnings() }

// SaveTheme writes th to the user theme dir as name.json and returns its
// portable ref. It is the theme editor's Save/Save As: one atomic write, then a
// registry rescan so every picker sees the file.
func (c *themeCatalog) SaveTheme(name string, th theme.Theme) (string, error) {
	path, err := c.reg.ThemePath(name)
	if err != nil {
		return "", err
	}
	base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	data, err := th.MarshalFile(base)
	if err != nil {
		return "", fmt.Errorf("theme: encode %s: %w", base, err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", fmt.Errorf("theme: make the theme dir: %w", err)
	}
	if _, err := atomicfile.WriteFile(path, data); err != nil {
		return "", fmt.Errorf("theme: write %s: %w", path, err)
	}
	if err := c.reg.Reload(); err != nil {
		return "", err
	}
	ref, err := c.reg.Canonical(path)
	if err != nil {
		return base + ".json", nil
	}
	return ref, nil
}

// DeleteTheme removes a user theme file and rescans.
func (c *themeCatalog) DeleteTheme(ref string) error { return c.reg.DeleteTheme(ref) }
