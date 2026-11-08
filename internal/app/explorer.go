package app

import (
	"github.com/tbxark/gvim/config"
	"github.com/tbxark/gvim/explorer"
)

// explorerOptions converts the file_explorer config section to the TUI's plain
// explorer options. The width keeps its configured form (percent or columns), so a
// grip drag can preserve it, as Fresh's ExplorerWidth does.
func explorerOptions(fe config.FileExplorerConfig) explorer.Options {
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
	}
}
