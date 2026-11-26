// Package configview backs the `gvim config` and `gvim grammar` commands: it
// renders the effective configuration for printing and reports the config file
// locations. It is private to the executable shell: internal/cli imports it,
// and it imports config, keybindings and keymap, never app or the TUI.
package configview

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/tbxark/gvim/config"
	"github.com/tbxark/gvim/internal/keybindings"
	"github.com/tbxark/gvim/keymap"
	"github.com/tbxark/gvim/theme"
)

// ConfigFile is one config file location and whether a file is there.
type ConfigFile struct {
	// Label names the layer: "user" or "project".
	Label string
	// Path is the file location. For a project with no file, it is the
	// location a project file would take under the root.
	Path   string
	Exists bool
}

// ConfigView is the effective configuration, ready to print.
type ConfigView struct {
	// JSON is the effective settings as indented JSON, using the exact schema
	// of config.json so it can be loaded back without diagnostics. It ends
	// with a newline.
	JSON []byte
	// Warnings describe each file or key that was skipped while loading. They
	// do not stop loading.
	Warnings []string
}

// Options are the command-line flags that change the effective configuration in
// a view. They are the config-relevant fields of app.Options; the zero value
// uses the config files as they are.
type Options struct {
	// Config is the user config file path. Empty means the default location.
	Config string
	// Keymap and Theme override the names the config files set.
	Keymap string
	Theme  string
	// Trusted tells Load whether the workspace's recorded trust level allows the
	// project config layer (docs/trust.md). The caller resolves it read-only; nil
	// (no trust information) keeps the project layer out, the conservative choice.
	Trusted *bool
}

// KeymapNames lists the built-in keymaps, for help text and completion. Names are
// checked by ValidateFlags.
func KeymapNames() []string { return keymap.BuiltinNames() }

// validate reports the first option that cannot be used.
func (o Options) validate() error { return ValidateFlags(o.Keymap, o.Theme) }

// ValidateFlags reports whether a --keymap and a --theme value can be used: a built-in
// keymap, and a theme in a valid form that exists when it is a plain name. An empty
// value is not checked. app.Options.Validate gives the same errors, so `gvim config
// show` rejects a bad flag with the same text as `gvim`.
func ValidateFlags(keymapName, themeName string) error {
	if keymapName != "" {
		if _, err := keymap.ByName(keymapName); err != nil {
			names := keymap.BuiltinNames()
			allowed := strings.Join(names[:len(names)-1], ", ") + " or " + names[len(names)-1]
			return fmt.Errorf("--keymap: unknown keymap %q (want %s)", keymapName, allowed)
		}
	}
	if themeName != "" {
		if err := theme.ValidateForm(themeName); err != nil {
			return fmt.Errorf("--theme: %w", err)
		}
		if theme.IsNamedForm(themeName) && !slices.Contains(theme.Names(), themeName) {
			dir, dirErr := config.Dir()
			if dirErr == nil {
				registry := theme.NewRegistry(filepath.Join(dir, "themes"))
				if _, err := registry.Load(themeName); err != nil {
					return fmt.Errorf("--theme: unknown theme %q (want one of the bundled themes, a theme in %s, or a theme path)", themeName, filepath.Join(dir, "themes"))
				}
			}
		}
	}
	return nil
}

// effectiveSettings is the JSON shape of ConfigView.JSON. Every field mirrors
// the nested schema of config.json so the output reloads cleanly with zero
// diagnostics.
type effectiveSettings struct {
	Theme              string                        `json:"theme"`
	Keymap             string                        `json:"keymap"`
	Locale             string                        `json:"locale,omitempty"`
	UIIcons            string                        `json:"ui_icons"`
	UIIconsFont        string                        `json:"ui_icons_font,omitempty"`
	ImageProtocol      string                        `json:"image_protocol"`
	MouseCapture       bool                          `json:"mouse_capture"`
	ScrollSync         bool                          `json:"scroll_sync"`
	LSPEnabled         bool                          `json:"lsp_enabled"`
	AutoAttach         bool                          `json:"auto_attach"`
	PluginTimeoutMS    int                           `json:"plugin_timeout_ms"`
	DefaultLanguage    string                        `json:"default_language,omitempty"`
	CheckForUpdates    bool                          `json:"check_for_updates"`
	UpdateCheckURL     string                        `json:"update_check_url,omitempty"`
	UpdateCommand      string                        `json:"update_command,omitempty"`
	FileScanExclusions []string                      `json:"file_scan_exclusions"`
	FileScanInclusions []string                      `json:"file_scan_inclusions,omitempty"`
	Editor             effectiveEditor               `json:"editor"`
	FileExplorer       effectiveFileExplorer         `json:"file_explorer"`
	Contents           effectiveContents             `json:"contents"`
	Terminal           effectiveTerminal             `json:"terminal"`
	Clipboard          effectiveClipboard            `json:"clipboard"`
	Daemon             effectiveDaemon               `json:"daemon"`
	Env                effectiveEnv                  `json:"env"`
	SSH                effectiveSSH                  `json:"ssh"`
	Keybindings        json.RawMessage               `json:"keybindings,omitempty"`
	Formatters         map[string]string             `json:"formatters,omitempty"`
	LSP                map[string][]config.LSPServer `json:"lsp,omitempty"`
	UniversalLSP       map[string][]config.LSPServer `json:"universal_lsp,omitempty"`
	Languages          map[string]effectiveLanguage  `json:"languages,omitempty"`
	Plugins            map[string]effectivePlugin    `json:"plugins,omitempty"`
}

type effectiveStatusBar struct {
	Left      []string `json:"left,omitempty"`
	Right     []string `json:"right,omitempty"`
	Separator string   `json:"separator,omitempty"`
}

type effectiveSearch struct {
	CaseSensitive bool `json:"case_sensitive"`
	WholeWord     bool `json:"whole_word"`
	Regex         bool `json:"regex"`
	ConfirmEach   bool `json:"confirm_each"`
}

type effectiveEditor struct {
	TabSize                                int                `json:"tab_size"`
	UseTabs                                bool               `json:"use_tabs"`
	DefaultLineEnding                      string             `json:"default_line_ending"`
	TrimTrailingWhitespaceOnSave           bool               `json:"trim_trailing_whitespace_on_save"`
	EnsureFinalNewlineOnSave               bool               `json:"ensure_final_newline_on_save"`
	AutoIndent                             bool               `json:"auto_indent"`
	AutoClose                              bool               `json:"auto_close"`
	AutoSurround                           bool               `json:"auto_surround"`
	VirtualSpace                           string             `json:"virtual_space"`
	ScrollOffset                           int                `json:"scroll_offset"`
	AutoReadOnly                           bool               `json:"auto_read_only"`
	LineNumbers                            bool               `json:"line_numbers"`
	RelativeLineNumbers                    bool               `json:"relative_line_numbers"`
	HighlightCurrentLine                   bool               `json:"highlight_current_line"`
	HighlightOccurrences                   bool               `json:"highlight_occurrences"`
	HideCurrentLineOnSelection             bool               `json:"hide_current_line_on_selection"`
	HighlightCurrentColumn                 bool               `json:"highlight_current_column"`
	LineWrap                               bool               `json:"line_wrap"`
	WrapIndent                             bool               `json:"wrap_indent"`
	WrapColumn                             int                `json:"wrap_column"`
	PageWidth                              int                `json:"page_width"`
	SyntaxHighlighting                     bool               `json:"syntax_highlighting"`
	ShowMenuBar                            bool               `json:"show_menu_bar"`
	MenuBarMnemonics                       bool               `json:"menu_bar_mnemonics"`
	ShowTabBar                             bool               `json:"show_tab_bar"`
	ShowStatusBar                          bool               `json:"show_status_bar"`
	ShowVerticalScrollbar                  bool               `json:"show_vertical_scrollbar"`
	ShowHorizontalScrollbar                bool               `json:"show_horizontal_scrollbar"`
	ShowTilde                              bool               `json:"show_tilde"`
	NerdFontIcons                          bool               `json:"nerd_font_icons"`
	SetWindowTitle                         bool               `json:"set_window_title"`
	CursorStyle                            string             `json:"cursor_style"`
	Rulers                                 []int              `json:"rulers,omitempty"`
	IndentationGuide                       string             `json:"indentation_guide"`
	IndentationGuideGlyph                  string             `json:"indentation_guide_glyph"`
	RainbowIndentation                     bool               `json:"rainbow_indentation"`
	WhitespaceShow                         bool               `json:"whitespace_show"`
	WhitespaceSpacesLeading                bool               `json:"whitespace_spaces_leading"`
	WhitespaceSpacesInner                  bool               `json:"whitespace_spaces_inner"`
	WhitespaceSpacesTrailing               bool               `json:"whitespace_spaces_trailing"`
	WhitespaceTabsLeading                  bool               `json:"whitespace_tabs_leading"`
	WhitespaceTabsInner                    bool               `json:"whitespace_tabs_inner"`
	WhitespaceTabsTrailing                 bool               `json:"whitespace_tabs_trailing"`
	WhitespaceNewlines                     bool               `json:"whitespace_newlines"`
	WhitespaceCarriageReturns              bool               `json:"whitespace_carriage_returns"`
	WhitespaceInSelection                  bool               `json:"whitespace_in_selection"`
	HighlightMatchingBrackets              bool               `json:"highlight_matching_brackets"`
	RainbowBrackets                        bool               `json:"rainbow_brackets"`
	CompletionPopupAutoShow                bool               `json:"completion_popup_auto_show"`
	QuickSuggestions                       bool               `json:"quick_suggestions"`
	QuickSuggestionsDelayMS                int                `json:"quick_suggestions_delay_ms"`
	SuggestOnTriggerCharacters             bool               `json:"suggest_on_trigger_characters"`
	EnableInlayHints                       bool               `json:"enable_inlay_hints"`
	DiagnosticsInlineText                  bool               `json:"diagnostics_inline_text"`
	MouseWheelScrollLines                  int                `json:"mouse_wheel_scroll_lines"`
	MouseHoverEnabled                      bool               `json:"mouse_hover_enabled"`
	MouseHoverDelayMS                      int                `json:"mouse_hover_delay_ms"`
	DoubleClickTimeMS                      int                `json:"double_click_time_ms"`
	AutoSaveEnabled                        bool               `json:"auto_save_enabled"`
	AutoSaveIntervalSecs                   int                `json:"auto_save_interval_secs"`
	HotExit                                bool               `json:"hot_exit"`
	RecoveryEnabled                        bool               `json:"recovery_enabled"`
	AutoRecoverySaveIntervalSecs           int                `json:"auto_recovery_save_interval_secs"`
	AutoRevertPollIntervalMS               int                `json:"auto_revert_poll_interval_ms"`
	ConfirmQuit                            bool               `json:"confirm_quit"`
	SaveSession                            bool               `json:"save_session"`
	RestorePreviousSession                 bool               `json:"restore_previous_session"`
	SkipSessionRestoreWhenFilesPassed      bool               `json:"skip_session_restore_when_files_passed"`
	EphemeralFilePatterns                  []string           `json:"ephemeral_file_patterns"`
	AutoCreateEmptyBufferOnLastBufferClose bool               `json:"auto_create_empty_buffer_on_last_buffer_close"`
	HighlightContextBytes                  int                `json:"highlight_context_bytes"`
	LargeFileThresholdBytes                int                `json:"large_file_threshold_bytes"`
	EstimatedLineLength                    int                `json:"estimated_line_length"`
	StatusBar                              effectiveStatusBar `json:"status_bar"`
	Search                                 effectiveSearch    `json:"search"`
}

type effectiveFileExplorer struct {
	ShowHidden                bool     `json:"show_hidden"`
	HideGitignore             bool     `json:"hide_gitignore"`
	RespectGitignore          bool     `json:"respect_gitignore"`
	Width                     string   `json:"width"`
	Side                      string   `json:"side"`
	FollowActiveBuffer        bool     `json:"follow_active_buffer"`
	AutoOpenOnLastBufferClose bool     `json:"auto_open_on_last_buffer_close"`
	CompactDirectories        bool     `json:"compact_directories"`
	PreviewTabs               bool     `json:"preview_tabs"`
	CustomIgnorePatterns      []string `json:"custom_ignore_patterns,omitempty"`
}

type effectiveContents struct {
	Enabled    bool   `json:"enabled"`
	FoldBuffer bool   `json:"fold_buffer"`
	Follow     string `json:"follow"`
	AutoOpen   bool   `json:"auto_open"`
	Rows       int    `json:"rows"`
	MaxDepth   int    `json:"max_depth"`
}

type effectiveTerminalShell struct {
	Command string   `json:"command,omitempty"`
	Args    []string `json:"args,omitempty"`
}

type effectiveTerminalAgent struct {
	Name       string   `json:"name"`
	Command    string   `json:"command"`
	Args       []string `json:"args,omitempty"`
	ResumeArgs []string `json:"resume_args,omitempty"`
}

type effectiveTerminal struct {
	Shell                effectiveTerminalShell   `json:"shell"`
	CloseOnExit          bool                     `json:"close_on_exit"`
	ScrollbackLines      int                      `json:"scrollback_lines"`
	MouseForwarding      string                   `json:"mouse_forwarding"`
	JumpToEndOnOutput    bool                     `json:"jump_to_end_on_output"`
	MouseDragSelects     bool                     `json:"mouse_drag_selects"`
	PassCtrlVWithoutText bool                     `json:"pass_ctrl_v_without_text"`
	ResumeAgents         bool                     `json:"resume_agents"`
	RestoreLive          bool                     `json:"restore_live"`
	Agents               []effectiveTerminalAgent `json:"agents,omitempty"`
}

type effectiveClipboard struct {
	UseOSC52   bool `json:"use_osc52"`
	OSC52Paste bool `json:"osc52_paste"`
}

type effectiveDaemon struct {
	MaterializedWorkspaces int `json:"materialized_workspaces"`
}

type effectiveEnv struct {
	Detectors []config.EnvDetector `json:"detectors"`
}

type effectiveSSH struct {
	KnownHostsPolicy  string   `json:"known_hosts_policy"`
	HostKeyAlgorithms []string `json:"host_key_algorithms,omitempty"`
	Keyring           bool     `json:"keyring"`
}

type effectiveLanguage struct {
	Extensions         []string              `json:"extensions,omitempty"`
	Filenames          []string              `json:"filenames,omitempty"`
	CommentPrefix      *string               `json:"comment_prefix,omitempty"`
	AutoIndent         *bool                 `json:"auto_indent,omitempty"`
	AutoClose          *bool                 `json:"auto_close,omitempty"`
	AutoSurround       *bool                 `json:"auto_surround,omitempty"`
	ShowWhitespaceTabs *bool                 `json:"show_whitespace_tabs,omitempty"`
	LineWrap           *bool                 `json:"line_wrap,omitempty"`
	WrapColumn         *int                  `json:"wrap_column,omitempty"`
	PageView           *bool                 `json:"page_view,omitempty"`
	PageWidth          *int                  `json:"page_width,omitempty"`
	UseTabs            *bool                 `json:"use_tabs,omitempty"`
	TabSize            *int                  `json:"tab_size,omitempty"`
	Formatter          *config.Formatter     `json:"formatter,omitempty"`
	FormatOnSave       *bool                 `json:"format_on_save,omitempty"`
	OnSave             []config.OnSaveAction `json:"on_save,omitempty"`
	WordCharacters     *string               `json:"word_characters,omitempty"`
	Indent             *config.IndentRules   `json:"indent,omitempty"`
	IndentationGuide   *bool                 `json:"indentation_guide,omitempty"`
}

type effectivePlugin struct {
	Enabled  *bool          `json:"enabled,omitempty"`
	Settings map[string]any `json:"settings,omitempty"`
}

func formatExplorerWidth(w config.ExplorerWidth) string {
	if w.Percent {
		return strconv.Itoa(w.Value) + "%"
	}
	return strconv.Itoa(w.Value)
}

func buildEffectiveSettings(cfg config.Config, themeName, keymapName string) effectiveSettings {
	var agents []effectiveTerminalAgent
	for _, a := range cfg.Terminal.Agents {
		agents = append(agents, effectiveTerminalAgent{
			Name:       a.Name,
			Command:    a.Command,
			Args:       a.Args,
			ResumeArgs: a.ResumeArgs,
		})
	}
	var langs map[string]effectiveLanguage
	if len(cfg.Languages) > 0 {
		langs = make(map[string]effectiveLanguage, len(cfg.Languages))
		for id, l := range cfg.Languages {
			langs[id] = effectiveLanguage{
				Extensions:         l.Extensions,
				Filenames:          l.Filenames,
				CommentPrefix:      l.CommentPrefix,
				AutoIndent:         l.AutoIndent,
				AutoClose:          l.AutoClose,
				AutoSurround:       l.AutoSurround,
				ShowWhitespaceTabs: l.ShowWhitespaceTabs,
				LineWrap:           l.LineWrap,
				WrapColumn:         l.WrapColumn,
				PageView:           l.PageView,
				PageWidth:          l.PageWidth,
				UseTabs:            l.UseTabs,
				TabSize:            l.TabSize,
				Formatter:          l.Formatter,
				FormatOnSave:       l.FormatOnSave,
				OnSave:             l.OnSave,
				WordCharacters:     l.WordCharacters,
				Indent:             l.Indent,
				IndentationGuide:   l.IndentationGuide,
			}
		}
	}
	var plugins map[string]effectivePlugin
	if len(cfg.Plugins) > 0 {
		plugins = make(map[string]effectivePlugin, len(cfg.Plugins))
		for name, p := range cfg.Plugins {
			plugins[name] = effectivePlugin{
				Enabled:  p.Enabled,
				Settings: p.Settings,
			}
		}
	}
	var lspMap map[string][]config.LSPServer
	if len(cfg.LSP) > 0 {
		lspMap = cfg.LSP
	}
	return effectiveSettings{
		Theme:              themeName,
		Keymap:             keymapName,
		Locale:             cfg.Locale,
		UIIcons:            cfg.UIIcons,
		UIIconsFont:        cfg.UIIconsFont,
		ImageProtocol:      cfg.UIImageProtocol,
		MouseCapture:       cfg.MouseCapture,
		ScrollSync:         cfg.ScrollSync,
		LSPEnabled:         cfg.LSPEnabled,
		AutoAttach:         cfg.AutoAttach,
		PluginTimeoutMS:    int(cfg.PluginTimeout / time.Millisecond),
		DefaultLanguage:    cfg.DefaultLanguage,
		CheckForUpdates:    cfg.Update.Check,
		UpdateCheckURL:     cfg.Update.URL,
		UpdateCommand:      cfg.Update.Command,
		FileScanExclusions: cfg.FileScanExclusions,
		FileScanInclusions: cfg.FileScanInclusions,
		Editor: effectiveEditor{
			TabSize:                                cfg.TabSize,
			UseTabs:                                !cfg.InsertSpaces,
			DefaultLineEnding:                      cfg.LineEnding,
			TrimTrailingWhitespaceOnSave:           cfg.TrimTrailingWhitespace,
			EnsureFinalNewlineOnSave:               cfg.EnsureFinalNewline,
			AutoIndent:                             cfg.AutoIndent,
			AutoClose:                              cfg.AutoClose,
			AutoSurround:                           cfg.AutoSurround,
			VirtualSpace:                           cfg.VirtualSpace,
			ScrollOffset:                           cfg.ScrollOffset,
			AutoReadOnly:                           cfg.AutoReadOnly,
			LineNumbers:                            cfg.LineNumbers,
			RelativeLineNumbers:                    cfg.RelativeLineNumbers,
			HighlightCurrentLine:                   cfg.HighlightCurrentLine,
			HighlightOccurrences:                   cfg.HighlightOccurrences,
			HideCurrentLineOnSelection:             cfg.HideCurrentLineOnSelection,
			HighlightCurrentColumn:                 cfg.HighlightCurrentColumn,
			LineWrap:                               cfg.LineWrap,
			WrapIndent:                             cfg.WrapIndent,
			WrapColumn:                             cfg.WrapColumn,
			PageWidth:                              cfg.PageWidth,
			SyntaxHighlighting:                     cfg.SyntaxHighlighting,
			ShowMenuBar:                            cfg.ShowMenuBar,
			MenuBarMnemonics:                       cfg.MenuBarMnemonics,
			ShowTabBar:                             cfg.ShowTabBar,
			ShowStatusBar:                          cfg.ShowStatusBar,
			ShowVerticalScrollbar:                  cfg.ShowVerticalScrollbar,
			ShowHorizontalScrollbar:                cfg.ShowHorizontalScrollbar,
			ShowTilde:                              cfg.ShowTilde,
			NerdFontIcons:                          cfg.NerdFontIcons,
			SetWindowTitle:                         cfg.SetWindowTitle,
			CursorStyle:                            cfg.CursorStyle,
			Rulers:                                 cfg.Rulers,
			IndentationGuide:                       cfg.IndentationGuide,
			IndentationGuideGlyph:                  cfg.IndentationGuideGlyph,
			RainbowIndentation:                     cfg.RainbowIndentation,
			WhitespaceShow:                         cfg.ShowWhitespace,
			WhitespaceSpacesLeading:                cfg.WhitespaceSpacesLeading,
			WhitespaceSpacesInner:                  cfg.WhitespaceSpacesInner,
			WhitespaceSpacesTrailing:               cfg.WhitespaceSpacesTrailing,
			WhitespaceTabsLeading:                  cfg.WhitespaceTabsLeading,
			WhitespaceTabsInner:                    cfg.WhitespaceTabsInner,
			WhitespaceTabsTrailing:                 cfg.WhitespaceTabsTrailing,
			WhitespaceNewlines:                     cfg.WhitespaceNewlines,
			WhitespaceCarriageReturns:              cfg.WhitespaceCarriageReturns,
			WhitespaceInSelection:                  cfg.WhitespaceInSelection,
			HighlightMatchingBrackets:              cfg.HighlightMatchingBrackets,
			RainbowBrackets:                        cfg.RainbowBrackets,
			CompletionPopupAutoShow:                cfg.CompletionPopupAutoShow,
			QuickSuggestions:                       cfg.QuickSuggestions,
			QuickSuggestionsDelayMS:                cfg.QuickSuggestionsDelayMS,
			SuggestOnTriggerCharacters:             cfg.SuggestOnTriggerCharacters,
			EnableInlayHints:                       cfg.EnableInlayHints,
			DiagnosticsInlineText:                  cfg.DiagnosticsInlineText,
			MouseWheelScrollLines:                  cfg.MouseWheelScrollLines,
			MouseHoverEnabled:                      cfg.MouseHover,
			MouseHoverDelayMS:                      cfg.MouseHoverDelayMS,
			DoubleClickTimeMS:                      cfg.DoubleClickTimeMS,
			AutoSaveEnabled:                        cfg.AutoSaveEnabled,
			AutoSaveIntervalSecs:                   cfg.AutoSaveIntervalSecs,
			HotExit:                                cfg.HotExit,
			RecoveryEnabled:                        cfg.RecoveryEnabled,
			AutoRecoverySaveIntervalSecs:           cfg.AutoRecoverySaveIntervalSecs,
			AutoRevertPollIntervalMS:               cfg.AutoRevertPollIntervalMS,
			ConfirmQuit:                            cfg.ConfirmQuit,
			SaveSession:                            cfg.SaveSession,
			RestorePreviousSession:                 cfg.RestorePreviousSession,
			SkipSessionRestoreWhenFilesPassed:      cfg.SkipSessionRestoreWhenFilesPassed,
			EphemeralFilePatterns:                  cfg.EphemeralFilePatterns,
			AutoCreateEmptyBufferOnLastBufferClose: cfg.AutoCreateEmptyBufferOnLastBufferClose,
			HighlightContextBytes:                  cfg.HighlightContextBytes,
			LargeFileThresholdBytes:                cfg.LargeFileThresholdBytes,
			EstimatedLineLength:                    cfg.EstimatedLineLength,
			StatusBar: effectiveStatusBar{
				Left:      cfg.StatusBar.Left,
				Right:     cfg.StatusBar.Right,
				Separator: cfg.StatusBar.Separator,
			},
			Search: effectiveSearch{
				CaseSensitive: cfg.Search.CaseSensitive,
				WholeWord:     cfg.Search.WholeWord,
				Regex:         cfg.Search.Regex,
				ConfirmEach:   cfg.Search.ConfirmEach,
			},
		},
		FileExplorer: effectiveFileExplorer{
			ShowHidden:                cfg.ExplorerShowHidden,
			HideGitignore:             cfg.FileExplorer.HideGitignore,
			RespectGitignore:          cfg.FileExplorer.RespectGitignoreEnabled(),
			Width:                     formatExplorerWidth(cfg.FileExplorer.Width),
			Side:                      cfg.FileExplorer.Side,
			FollowActiveBuffer:        cfg.FileExplorer.FollowActiveBuffer,
			AutoOpenOnLastBufferClose: cfg.FileExplorer.AutoOpenOnLastBufferClose,
			CompactDirectories:        cfg.FileExplorer.CompactDirectories,
			PreviewTabs:               cfg.FileExplorer.PreviewTabsEnabled(),
			CustomIgnorePatterns:      cfg.FileExplorer.CustomIgnorePatterns,
		},
		Contents: effectiveContents{
			Enabled:    cfg.ContentsEnabled(),
			FoldBuffer: cfg.ContentsFoldBuffer(),
			Follow:     cfg.ContentsFollow(),
			AutoOpen:   cfg.ContentsAutoOpen(),
			Rows:       cfg.ContentsRows(),
			MaxDepth:   cfg.ContentsMaxDepth(),
		},
		Terminal: effectiveTerminal{
			Shell: effectiveTerminalShell{
				Command: cfg.Terminal.Shell.Command,
				Args:    cfg.Terminal.Shell.Args,
			},
			CloseOnExit:          cfg.Terminal.CloseOnExit,
			ScrollbackLines:      cfg.Terminal.ScrollbackLines,
			MouseForwarding:      cfg.Terminal.MouseForwarding,
			JumpToEndOnOutput:    cfg.Terminal.JumpToEndOnOutput,
			MouseDragSelects:     cfg.Terminal.MouseDragSelects,
			PassCtrlVWithoutText: cfg.Terminal.PassCtrlVWithoutText,
			ResumeAgents:         cfg.Terminal.ResumeAgents,
			RestoreLive:          cfg.Terminal.RestoreLive,
			Agents:               agents,
		},
		Clipboard: effectiveClipboard{
			UseOSC52:   cfg.Clipboard.UseOSC52,
			OSC52Paste: cfg.Clipboard.OSC52Paste,
		},
		Daemon: effectiveDaemon{
			MaterializedWorkspaces: cfg.Daemon.MaterializedWorkspaces,
		},
		Env: effectiveEnv{
			Detectors: cfg.EnvDetectors(),
		},
		SSH: effectiveSSH{
			KnownHostsPolicy:  cfg.SSH.KnownHostsPolicy,
			HostKeyAlgorithms: cfg.SSH.HostKeyAlgorithms,
			Keyring:           cfg.SSH.Keyring,
		},
		Keybindings:  cfg.Keybindings,
		Formatters:   cfg.Formatters,
		LSP:          lspMap,
		UniversalLSP: cfg.UniversalLSP,
		Languages:    langs,
		Plugins:      plugins,
	}
}

// Load merges the default, user and project config layers for root and returns
// the result as printable text. A non-empty opts.Config replaces the default
// user file, as the --config flag does. A non-empty opts.Keymap and opts.Theme
// replace the keymap and theme the files name; the defaults apply when neither
// is set. A root that is not absolute is made absolute first. The error is
// non-nil when opts is invalid, when the result cannot be built, or when
// opts.Config is set and does not exist.
//
// The project layer is trust-gated exactly as the editor's config load is
// (docs/trust.md): it is merged only when the workspace's recorded trust level
// allows project config, so an untrusted or undecided workspace shows the
// configuration the editor would actually run with. When a project file exists
// but the gate skips it, the view carries a warning naming the file.
func Load(root string, opts Options) (ConfigView, error) {
	if err := opts.validate(); err != nil {
		return ConfigView{}, err
	}
	if err := CheckFile(opts.Config); err != nil {
		return ConfigView{}, err
	}
	trusted := opts.Trusted != nil && *opts.Trusted
	projectPath, projectFound := projectConfigFile(root)
	cfg, diags, err := config.Load(config.Options{Root: root, UserFile: opts.Config, Untrusted: !trusted})
	if err != nil {
		return ConfigView{}, err
	}
	keymapName := opts.Keymap
	if keymapName == "" {
		keymapName = cfg.Keymap
	}
	if keymapName == "" {
		keymapName = keymap.DefaultName()
	}
	themeName := opts.Theme
	if themeName == "" {
		themeName = cfg.Theme
	}
	data, err := json.MarshalIndent(buildEffectiveSettings(cfg, themeName, keymapName), "", "  ")
	if err != nil {
		return ConfigView{}, err
	}
	view := ConfigView{JSON: append(data, '\n')}
	for _, d := range diags {
		view.Warnings = append(view.Warnings, d.String())
	}
	if !trusted && projectFound {
		view.Warnings = append(view.Warnings, fmt.Sprintf(
			"project config layer skipped: %s (the workspace is not trusted; start gvim in it to decide trust)", projectPath))
	}
	view.Warnings = append(view.Warnings, keybindings.Warnings(cfg)...)
	return view, nil
}

// projectConfigFile returns the project config file in root. The trust decision
// itself is the caller's (Options.Trusted): this package is a config/keybindings
// leaf and does not read the trust store.
func projectConfigFile(root string) (path string, found bool) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", false
	}
	path, found, err = config.ProjectFile(abs)
	if err != nil {
		return path, false
	}
	return path, found
}

// Paths returns the user config file and the project config file for root. A
// non-empty configPath stands in for the user file, as it does in Load. The
// project entry is .gvim/config.json under root (even when absent, with Exists
// false). The error is non-nil only when configPath is empty and the user config
// directory is unknown.
func Paths(root, configPath string) ([]ConfigFile, error) {
	user := configPath
	if user == "" {
		files, err := config.Paths()
		if err != nil {
			return nil, err
		}
		user = files.User
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	proj, found, err := config.ProjectFile(abs)
	if err != nil {
		return nil, err
	}
	if !found {
		proj = filepath.Join(abs, ".gvim", "config.json")
	}
	return []ConfigFile{
		{Label: "user", Path: user, Exists: isFile(user)},
		{Label: "project", Path: proj, Exists: found},
	}, nil
}

// CheckFile returns an error that names path when a config file was given with
// --config and does not exist. An empty path is the default user file,
// which may be absent, so it passes. Other stat failures are left to
// config.Load, which reports them as diagnostics.
func CheckFile(path string) error {
	if path == "" {
		return nil
	}
	if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("config file %s does not exist", path)
	}
	return nil
}

// isFile reports whether path is an existing regular file.
func isFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}
