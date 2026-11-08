package app

import (
	"github.com/tbxark/gvim/config"
	"github.com/tbxark/gvim/internal/tui"
	"github.com/tbxark/gvim/internal/tui/sidebar"
)

// markdownTOCConfig reads the Contents sidebar section's configuration from the
// config's plugins.markdown_toc entry, so the Fresh-compatible keys
// plugins.markdown_toc.enabled and plugins.markdown_toc.settings keep working
// now that the outline is built into the TUI instead of a bundled plugin.
//
// Only the user layer is read. The project layer of a plugin's settings applies
// to a plugin key only when that plugin declares it with project = true, and
// markdown_toc declared none; a repository cannot turn the section on or tune
// it. The section is on with its defaults when the config has no entry, as the
// bundled plugin shipped enabled.
func markdownTOCConfig(cfg config.Config) *tui.MarkdownTOC {
	out := tui.DefaultMarkdownTOC()
	pc, ok := cfg.Plugins["markdown_toc"]
	if !ok {
		return &out
	}
	if pc.Enabled != nil {
		out.Enabled = *pc.Enabled
	}
	out.Settings = sidebar.ParseSettings(pc.Settings)
	return &out
}
