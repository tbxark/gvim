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
	// The native section is user-layer only: a project file can disable a real plugin
	// (merged Plugins.Enabled), but it must not turn off the reader's Contents section.
	out.Enabled = cfg.ContentsEnabled()
	if pc, ok := cfg.Plugins["markdown_toc"]; ok {
		out.Settings = sidebar.ParseSettings(pc.Settings) // user settings only
	}
	// The native contents.* keys override the retired plugin's settings path when the
	// user file sets them (config's effective getters keep both compatible). Only the
	// user layer reaches either path, so a project cannot tune the section.
	out.Settings.FoldBuffer = cfg.ContentsFoldBuffer()
	out.Settings.Follow = sidebar.ParseFollow(cfg.ContentsFollow())
	out.Settings.AutoOpen = cfg.ContentsAutoOpen()
	out.Settings.Rows = cfg.ContentsRows()
	return &out
}
