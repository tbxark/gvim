package app

import (
	"slices"

	"github.com/tbxark/gvim/config"
	"github.com/tbxark/gvim/explorer"
	"github.com/tbxark/gvim/finder"
)

// finderOptions are the file index options of cfg: the ignore and scan settings a
// local root is enumerated with.
func finderOptions(cfg config.Config) finder.Options {
	return finder.Options{
		RespectGitignore:     cfg.FileExplorer.RespectGitignore,
		CustomIgnorePatterns: cfg.FileExplorer.CustomIgnorePatterns,
		ScanExclusions:       cfg.FileScanExclusions,
		ScanInclusions:       cfg.FileScanInclusions,
	}
}

// explorerOptions converts the file_explorer config section plus the top-level
// file_scan_* lists to the TUI's plain explorer options. The width keeps its
// configured form (percent or columns), so a grip drag can preserve it, as
// Fresh's ExplorerWidth does.
func explorerOptions(cfg config.Config) explorer.Options {
	fe := cfg.FileExplorer
	var width explorer.Width
	if fe.Width.Percent {
		width = explorer.PercentWidth(fe.Width.Value)
	} else {
		width = explorer.ColumnsWidth(fe.Width.Value)
	}
	side := explorer.SideLeft
	if fe.Side == "right" {
		side = explorer.SideRight
	}
	return explorer.Options{
		Width:                     width,
		Side:                      side,
		FollowActiveBuffer:        fe.FollowActiveBuffer,
		CompactDirectories:        fe.CompactDirectories,
		AutoOpenOnLastBufferClose: fe.AutoOpenOnLastBufferClose,
		RespectGitignore:          fe.RespectGitignore,
		HideGitignore:             fe.HideGitignore,
		CustomIgnorePatterns:      slices.Clone(fe.CustomIgnorePatterns),
		PreviewTabs:               fe.PreviewTabs,
		ScanExclusions:            slices.Clone(cfg.FileScanExclusions),
		ScanInclusions:            slices.Clone(cfg.FileScanInclusions),
	}
}
