package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// FileExplorerConfig is the file_explorer section of Fresh's config: the options
// gvim's explorer honours. The flat explorer_show_hidden/explorer_show_ignored
// keys stay on Config as their own older spellings.
//
// Verified against refs/fresh/crates/fresh-editor-core/src/config.rs
// (FileExplorerConfig): width defaults to 30% of the terminal, side to the left,
// follow_active_buffer off, auto_open_on_last_buffer_close on and
// compact_directories on. respect_gitignore, custom_ignore_patterns, preview_tabs
// and the tree_indicator glyph keys are still accepted and ignored (documented in
// docs/sidebar.md).
type FileExplorerConfig struct {
	// Width is file_explorer.width: a percentage of the terminal ("30%") or a
	// column count ("24"); a plain number is a percentage. Default 30%.
	Width ExplorerWidth
	// Side is file_explorer.side: "left" (default) or "right".
	Side string
	// FollowActiveBuffer is file_explorer.follow_active_buffer, default false.
	FollowActiveBuffer bool
	// AutoOpenOnLastBufferClose is
	// file_explorer.auto_open_on_last_buffer_close, default true.
	AutoOpenOnLastBufferClose bool
	// CompactDirectories is file_explorer.compact_directories, default true.
	CompactDirectories bool
}

// DefaultFileExplorer returns Fresh's file_explorer defaults.
func DefaultFileExplorer() FileExplorerConfig {
	return FileExplorerConfig{
		Width:                     ExplorerWidth{Percent: true, Value: 30},
		Side:                      "left",
		AutoOpenOnLastBufferClose: true,
		CompactDirectories:        true,
	}
}

// ExplorerWidth is file_explorer.width. Percent says the value is a percentage of
// the terminal width; otherwise it is a count of columns. Fresh's ExplorerWidth
// has the same two forms, and a grip drag keeps the configured form (a percentage
// stays a percentage).
type ExplorerWidth struct {
	Percent bool
	Value   int
}

// Cols is the width in columns at a frame frameWidth columns wide, before the
// frame's own clamps: integer division for a percentage, as Fresh's to_cols.
func (w ExplorerWidth) Cols(frameWidth int) int {
	if w.Percent {
		return max(frameWidth, 0) * w.Value / 100
	}
	return w.Value
}

// parseExplorerWidth parses file_explorer.width: "30%" or "30 %" is a percentage,
// "24" a column count, an integer a percentage, and a float in [0, 1] the legacy
// fraction form (0.3 is 30%); any other float is a percentage. The bounds are
// Fresh's: a percentage is 0..100 and a column count is at least 0.
func parseExplorerWidth(raw json.RawMessage) (ExplorerWidth, error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return ExplorerWidth{}, errors.New("must be a percent (\"30%\") or a column count")
	}
	if trimmed[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return ExplorerWidth{}, errors.New("must be a percent (\"30%\") or a column count")
		}
		s = strings.ReplaceAll(strings.TrimSpace(s), " ", "")
		if pct, ok := strings.CutSuffix(s, "%"); ok {
			n, err := parseExplorerPercent(pct)
			if err != nil {
				return ExplorerWidth{}, err
			}
			return ExplorerWidth{Percent: true, Value: n}, nil
		}
		var n int
		if _, err := fmt.Sscanf(s, "%d", &n); err != nil || n < 0 {
			return ExplorerWidth{}, fmt.Errorf("invalid width %q: want a percent (\"30%%\") or a column count", s)
		}
		return ExplorerWidth{Value: n}, nil
	}
	var f float64
	if err := json.Unmarshal(raw, &f); err != nil {
		return ExplorerWidth{}, errors.New("must be a percent (\"30%\") or a column count")
	}
	if f >= 0 && f <= 1 && f != float64(int(f)) {
		// The legacy fraction form: 0.3 meant 30%.
		return ExplorerWidth{Percent: true, Value: int(f*100 + 0.5)}, nil
	}
	n := int(f)
	if n < 0 || n > 100 {
		return ExplorerWidth{}, fmt.Errorf("a percent must be between 0 and 100, got %v", f)
	}
	return ExplorerWidth{Percent: true, Value: n}, nil
}

// parseExplorerPercent parses the digits of a percentage. The result is 0..100.
func parseExplorerPercent(s string) (int, error) {
	var n int
	if _, err := fmt.Sscanf(strings.TrimSpace(s), "%d", &n); err != nil || n < 0 || n > 100 {
		return 0, fmt.Errorf("a percent must be between 0 and 100, got %q", s)
	}
	return n, nil
}

// setFileExplorerWidth applies file_explorer.width.
func (c *Config) setFileExplorerWidth(raw json.RawMessage) error {
	w, err := parseExplorerWidth(raw)
	if err != nil {
		return err
	}
	c.FileExplorer.Width = w
	return nil
}

// setFileExplorerSide applies file_explorer.side.
func (c *Config) setFileExplorerSide(raw json.RawMessage) error {
	var side string
	if err := json.Unmarshal(raw, &side); err != nil {
		return errors.New("must be \"left\" or \"right\"")
	}
	if side != "left" && side != "right" {
		return fmt.Errorf("must be \"left\" or \"right\", got %q", side)
	}
	c.FileExplorer.Side = side
	return nil
}

// setFileExplorerBool applies one boolean file_explorer key to dst.
func setFileExplorerBool(raw json.RawMessage, dst *bool) error {
	var v bool
	if err := json.Unmarshal(raw, &v); err != nil {
		return errors.New("must be true or false")
	}
	*dst = v
	return nil
}
