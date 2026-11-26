package app

import (
	"github.com/tbxark/gvim/config"
	"github.com/tbxark/gvim/internal/tui"
	"github.com/tbxark/gvim/internal/tui/sidebar"
)

// markdownTOCConfig reads the Contents sidebar section's configuration from the
// native user-layer contents.* keys.
func markdownTOCConfig(cfg config.Config) *tui.MarkdownTOC {
	out := tui.DefaultMarkdownTOC()
	out.Enabled = cfg.ContentsEnabled()
	out.Settings.FoldBuffer = cfg.ContentsFoldBuffer()
	out.Settings.Follow = sidebar.ParseFollow(cfg.ContentsFollow())
	out.Settings.AutoOpen = cfg.ContentsAutoOpen()
	out.Settings.Rows = cfg.ContentsRows()
	out.Settings.MaxDepth = cfg.ContentsMaxDepth()
	return &out
}
