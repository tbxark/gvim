package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
)

// Values of editor.indentation_guide.
const (
	IndentationGuideNone   = "none"
	IndentationGuideAll    = "all"
	IndentationGuideActive = "active"
)

// Values of editor.virtual_space.
const (
	VirtualSpaceOff   = "off"
	VirtualSpaceBlock = "block"
	VirtualSpaceOn    = "on"
)

// DefaultIndentationGuideGlyph is the editor.indentation_guide_glyph default.
const DefaultIndentationGuideGlyph = "▏"

// StatusBarConfig is editor.status_bar: the elements of the status row, as Fresh names
// them ("{cursor}", "{language}", ...).
type StatusBarConfig struct {
	Left      []string
	Right     []string
	Separator string
}

// SearchDefaults is editor.search: the starting state of the find bar toggles.
type SearchDefaults struct {
	CaseSensitive bool
	WholeWord     bool
	Regex         bool
	ConfirmEach   bool // ask before each replacement
}

// EditorOptions are the editor.* options of Fresh's config that gvim parses in
// addition to the top-level settings. Config embeds it, so the fields read as
// cfg.RelativeLineNumbers. Every default is Fresh's. A zero or negative integer is
// not a "disabled" value unless the field says so.
//
// The fields that Fresh lets a language override are also in EditorSettings, the
// result of Config.EditorFor. A consumer that works on one document reads that
// result, not these fields.
type EditorOptions struct {
	// Display.

	RelativeLineNumbers        bool // relative_line_numbers, default false
	HideCurrentLineOnSelection bool // hide_current_line_on_selection, default false
	HighlightCurrentColumn     bool // highlight_current_column, default false
	WrapIndent                 bool // wrap_indent: wrapped rows keep the indent of their line, default true
	// WrapColumn is wrap_column: the text column at which lines wrap, gutter excluded.
	// Zero (or null) means the viewport width. Default 0. A language can override it.
	WrapColumn int
	// PageWidth is page_width: the width of the page view in columns. Zero (or null)
	// means the viewport width. Default 80. A language can override it.
	PageWidth          int
	SyntaxHighlighting bool            // syntax_highlighting, default true
	MenuBarMnemonics   bool            // menu_bar_mnemonics, default true
	StatusBar          StatusBarConfig // status_bar
	Search             SearchDefaults  // search
	ShowTilde          bool            // show_tilde: "~" on rows past the end of the file, default true
	NerdFontIcons      bool            // nerd_font_icons, default false
	SetWindowTitle     bool            // set_window_title, default true
	// Rulers is rulers: 1-based columns that carry a vertical ruler. Default none (nil).
	Rulers []int
	// IndentationGuide is indentation_guide: "none", "all" or "active". Default
	// "none". A language can switch the guides off (see EditorSettings.IndentationGuide).
	IndentationGuide      string
	IndentationGuideGlyph string // indentation_guide_glyph, default "▏"
	RainbowIndentation    bool   // rainbow_indentation, default false

	// Whitespace. ShowWhitespace (whitespace_show) is the master switch; the other
	// flags choose which marks show once it is on.

	WhitespaceSpacesLeading   bool // whitespace_spaces_leading, default false
	WhitespaceSpacesInner     bool // whitespace_spaces_inner, default false
	WhitespaceSpacesTrailing  bool // whitespace_spaces_trailing, default false
	WhitespaceTabsLeading     bool // whitespace_tabs_leading, default true
	WhitespaceTabsInner       bool // whitespace_tabs_inner, default true
	WhitespaceTabsTrailing    bool // whitespace_tabs_trailing, default true
	WhitespaceNewlines        bool // whitespace_newlines: mark line ends, default false
	WhitespaceCarriageReturns bool // whitespace_carriage_returns, default false
	WhitespaceInSelection     bool // whitespace_in_selection: marks show inside a selection, default true

	// Editing.

	AutoClose    bool // auto_close, default true. A language can override it.
	AutoSurround bool // auto_surround, default true. A language can override it.
	// VirtualSpace is virtual_space: "off", "block" or "on". Default "off".
	VirtualSpace string
	// ScrollOffset is scroll_offset: lines kept visible above and below the caret.
	// Default 3.
	ScrollOffset int
	AutoReadOnly bool // auto_read_only: open unwritable and library files read-only, default true

	// Bracket matching.

	HighlightMatchingBrackets bool // highlight_matching_brackets, default true
	RainbowBrackets           bool // rainbow_brackets, default true

	// Completion, LSP and diagnostics.

	CompletionPopupAutoShow    bool // completion_popup_auto_show, default false
	QuickSuggestions           bool // quick_suggestions, default true
	QuickSuggestionsDelayMS    int  // quick_suggestions_delay_ms, default 150
	SuggestOnTriggerCharacters bool // suggest_on_trigger_characters, default true
	EnableInlayHints           bool // enable_inlay_hints, default true
	DiagnosticsInlineText      bool // diagnostics_inline_text, default false

	// Mouse.

	MouseWheelScrollLines int // mouse_wheel_scroll_lines, default 3; 0 reads as 3
	MouseHoverDelayMS     int // mouse_hover_delay_ms, default 500
	DoubleClickTimeMS     int // double_click_time_ms, default 500

	// Recovery.

	AutoSaveEnabled              bool // auto_save_enabled, default false
	AutoSaveIntervalSecs         int  // auto_save_interval_secs, default 30
	HotExit                      bool // hot_exit: keep unsaved buffers across quit, default true
	RecoveryEnabled              bool // recovery_enabled, default true
	AutoRecoverySaveIntervalSecs int  // auto_recovery_save_interval_secs, default 2
	AutoRevertPollIntervalMS     int  // auto_revert_poll_interval_ms, default 2000

	// Startup and quit.

	ConfirmQuit                       bool // confirm_quit, default false
	SaveSession                       bool // save_session, default false
	RestorePreviousSession            bool // restore_previous_session, default true
	SkipSessionRestoreWhenFilesPassed bool // skip_session_restore_when_files_passed, default true
	// EphemeralFilePatterns is ephemeral_file_patterns: globs of files that are not
	// remembered in the session. Default ["**/.git/**"].
	EphemeralFilePatterns                  []string
	AutoCreateEmptyBufferOnLastBufferClose bool // auto_create_empty_buffer_on_last_buffer_close, default false

	// Performance.

	HighlightContextBytes   int // highlight_context_bytes, default 10000
	LargeFileThresholdBytes int // large_file_threshold_bytes, default 10485760: files of >= this size open in large file mode, 0 = off
	EstimatedLineLength     int // estimated_line_length, default 80
}

// defaultEditorOptions returns the defaults for EditorOptions. It builds new
// slices on each call.
func defaultEditorOptions() EditorOptions {
	return EditorOptions{
		WrapIndent:         true,
		PageWidth:          80,
		SyntaxHighlighting: true,

		MenuBarMnemonics: true,
		StatusBar: StatusBarConfig{
			Left:  []string{"{trust}", "{remote}", "{terminal_restart}", "{cursor}", "{diagnostics}", "{cursor_count}", "{messages}"},
			Right: []string{"{read_only}", "{line_ending}", "{encoding}", "{language}", "{lsp}", "{warnings}", "{update}", "{palette}"},
		},
		ShowTilde:             true,
		SetWindowTitle:        true,
		IndentationGuide:      IndentationGuideNone,
		IndentationGuideGlyph: DefaultIndentationGuideGlyph,

		WhitespaceTabsLeading:  true,
		WhitespaceTabsInner:    true,
		WhitespaceTabsTrailing: true,
		WhitespaceInSelection:  true,

		AutoClose:    true,
		AutoSurround: true,
		VirtualSpace: VirtualSpaceOff,
		ScrollOffset: 3,
		AutoReadOnly: true,

		HighlightMatchingBrackets: true,
		RainbowBrackets:           true,

		QuickSuggestions:           true,
		QuickSuggestionsDelayMS:    150,
		SuggestOnTriggerCharacters: true,
		EnableInlayHints:           true,

		MouseWheelScrollLines: 3,
		MouseHoverDelayMS:     500,
		DoubleClickTimeMS:     500,

		AutoSaveIntervalSecs:         30,
		HotExit:                      true,
		RecoveryEnabled:              true,
		AutoRecoverySaveIntervalSecs: 2,
		AutoRevertPollIntervalMS:     2000,

		RestorePreviousSession:            true,
		SkipSessionRestoreWhenFilesPassed: true,
		EphemeralFilePatterns:             []string{"**/.git/**"},

		HighlightContextBytes:   10000,
		LargeFileThresholdBytes: 10485760,
		EstimatedLineLength:     80,
	}
}

// option is one key of the "editor" object.
type option struct {
	key      string // Fresh's name, the key under "editor"
	nullable bool   // null is a valid value
	apply    func(c *Config, raw json.RawMessage) error
}

// set applies the option to c. On error c is unchanged.
func (o option) set(c *Config, raw json.RawMessage) error {
	if isNull(raw) && !o.nullable {
		return errors.New("null is not a valid value")
	}
	return o.apply(c, raw)
}

func boolOpt(key string, field func(*Config) *bool) option {
	return option{key: key, apply: func(c *Config, raw json.RawMessage) error {
		var v bool
		if err := json.Unmarshal(raw, &v); err != nil {
			return errors.New("must be true or false")
		}
		*field(c) = v
		return nil
	}}
}

// intOpt is an integer option in [lo, hi]. hi 0 means no upper bound.
func intOpt(key string, lo, hi int, field func(*Config) *int) option {
	return option{key: key, apply: func(c *Config, raw json.RawMessage) error {
		n, err := decodeInt(raw, lo, hi)
		if err != nil {
			return err
		}
		*field(c) = n
		return nil
	}}
}

// nullableIntOpt is intOpt that also accepts null, which stores 0.
func nullableIntOpt(key string, field func(*Config) *int) option {
	o := intOpt(key, 0, 0, field)
	o.nullable = true
	inner := o.apply
	o.apply = func(c *Config, raw json.RawMessage) error {
		if isNull(raw) {
			*field(c) = 0
			return nil
		}
		return inner(c, raw)
	}
	return o
}

// enumOpt is a string option that must be one of values. normalize maps a
// recognised spelling to the stored value; nil means the value is stored as written.
func enumOpt(key string, values []string, normalize func(string) (string, bool), field func(*Config) *string) option {
	return option{key: key, apply: func(c *Config, raw json.RawMessage) error {
		s, err := decodeString(raw)
		if err != nil {
			return err
		}
		if normalize != nil {
			if v, ok := normalize(s); ok {
				*field(c) = v
				return nil
			}
		} else if slices.Contains(values, s) {
			*field(c) = s
			return nil
		}
		return fmt.Errorf("must be %s, got %q", quotedList(values), s)
	}}
}

// quotedList formats values as "a", "b" or "c".
func quotedList(values []string) string {
	q := make([]string, len(values))
	for i, v := range values {
		q[i] = fmt.Sprintf("%q", v)
	}
	if len(q) < 2 {
		return strings.Join(q, "")
	}
	return strings.Join(q[:len(q)-1], ", ") + " or " + q[len(q)-1]
}

func decodeInt(raw json.RawMessage, lo, hi int) (int, error) {
	var n int
	if err := json.Unmarshal(raw, &n); err != nil {
		return 0, errors.New("must be an integer")
	}
	switch {
	case hi > 0 && (n < lo || n > hi):
		return 0, fmt.Errorf("must be between %d and %d, got %d", lo, hi, n)
	case n < lo:
		return 0, fmt.Errorf("must be at least %d, got %d", lo, n)
	}
	return n, nil
}

// cursorShapes maps each editor.cursor_style spelling to gvim's shape. The Fresh names
// carry a blink choice that gvim does not keep, and "default" is the terminal's
// own shape, which gvim draws as its default block.
var cursorShapes = map[string]string{
	CursorStyleBlock:     CursorStyleBlock,
	CursorStyleBar:       CursorStyleBar,
	CursorStyleUnderline: CursorStyleUnderline,
	"default":            CursorStyleBlock,
	"blinking_block":     CursorStyleBlock,
	"steady_block":       CursorStyleBlock,
	"blinking_bar":       CursorStyleBar,
	"steady_bar":         CursorStyleBar,
	"blinking_underline": CursorStyleUnderline,
	"steady_underline":   CursorStyleUnderline,
}

// editorOptions lists every key of the "editor" object that gvim knows.
var editorOptions = []option{
	// Display.
	boolOpt("line_numbers", func(c *Config) *bool { return &c.LineNumbers }),
	boolOpt("relative_line_numbers", func(c *Config) *bool { return &c.RelativeLineNumbers }),
	boolOpt("highlight_current_line", func(c *Config) *bool { return &c.HighlightCurrentLine }),
	boolOpt("highlight_occurrences", func(c *Config) *bool { return &c.HighlightOccurrences }),
	boolOpt("hide_current_line_on_selection", func(c *Config) *bool { return &c.HideCurrentLineOnSelection }),
	boolOpt("highlight_current_column", func(c *Config) *bool { return &c.HighlightCurrentColumn }),
	boolOpt("line_wrap", func(c *Config) *bool { return &c.LineWrap }),
	boolOpt("wrap_indent", func(c *Config) *bool { return &c.WrapIndent }),
	nullableIntOpt("wrap_column", func(c *Config) *int { return &c.WrapColumn }),
	nullableIntOpt("page_width", func(c *Config) *int { return &c.PageWidth }),
	boolOpt("syntax_highlighting", func(c *Config) *bool { return &c.SyntaxHighlighting }),
	boolOpt("show_menu_bar", func(c *Config) *bool { return &c.ShowMenuBar }),
	boolOpt("menu_bar_mnemonics", func(c *Config) *bool { return &c.MenuBarMnemonics }),
	boolOpt("show_tab_bar", func(c *Config) *bool { return &c.ShowTabBar }),
	boolOpt("show_status_bar", func(c *Config) *bool { return &c.ShowStatusBar }),
	{key: "status_bar", apply: applyStatusBar},
	{key: "search", apply: applySearch},
	boolOpt("show_vertical_scrollbar", func(c *Config) *bool { return &c.ShowVerticalScrollbar }),
	boolOpt("show_horizontal_scrollbar", func(c *Config) *bool { return &c.ShowHorizontalScrollbar }),
	boolOpt("show_tilde", func(c *Config) *bool { return &c.ShowTilde }),
	boolOpt("nerd_font_icons", func(c *Config) *bool { return &c.NerdFontIcons }),
	boolOpt("set_window_title", func(c *Config) *bool { return &c.SetWindowTitle }),
	enumOpt("cursor_style",
		[]string{CursorStyleBlock, CursorStyleBar, CursorStyleUnderline},
		func(s string) (string, bool) { v, ok := cursorShapes[s]; return v, ok },
		func(c *Config) *string { return &c.CursorStyle }),
	{key: "rulers", apply: applyRulers},
	enumOpt("indentation_guide",
		[]string{IndentationGuideNone, IndentationGuideAll, IndentationGuideActive}, nil,
		func(c *Config) *string { return &c.IndentationGuide }),
	{key: "indentation_guide_glyph", apply: applyGlyph},
	boolOpt("rainbow_indentation", func(c *Config) *bool { return &c.RainbowIndentation }),

	// Whitespace.
	boolOpt("whitespace_show", func(c *Config) *bool { return &c.ShowWhitespace }),
	boolOpt("whitespace_spaces_leading", func(c *Config) *bool { return &c.WhitespaceSpacesLeading }),
	boolOpt("whitespace_spaces_inner", func(c *Config) *bool { return &c.WhitespaceSpacesInner }),
	boolOpt("whitespace_spaces_trailing", func(c *Config) *bool { return &c.WhitespaceSpacesTrailing }),
	boolOpt("whitespace_tabs_leading", func(c *Config) *bool { return &c.WhitespaceTabsLeading }),
	boolOpt("whitespace_tabs_inner", func(c *Config) *bool { return &c.WhitespaceTabsInner }),
	boolOpt("whitespace_tabs_trailing", func(c *Config) *bool { return &c.WhitespaceTabsTrailing }),
	boolOpt("whitespace_newlines", func(c *Config) *bool { return &c.WhitespaceNewlines }),
	boolOpt("whitespace_carriage_returns", func(c *Config) *bool { return &c.WhitespaceCarriageReturns }),
	boolOpt("whitespace_in_selection", func(c *Config) *bool { return &c.WhitespaceInSelection }),

	// Editing.
	{key: "use_tabs", apply: func(c *Config, raw json.RawMessage) error {
		var v bool
		if err := json.Unmarshal(raw, &v); err != nil {
			return errors.New("must be true or false")
		}
		c.InsertSpaces = !v
		return nil
	}},
	intOpt("tab_size", MinTabSize, MaxTabSize, func(c *Config) *int { return &c.TabSize }),
	boolOpt("auto_indent", func(c *Config) *bool { return &c.AutoIndent }),
	boolOpt("auto_close", func(c *Config) *bool { return &c.AutoClose }),
	boolOpt("auto_surround", func(c *Config) *bool { return &c.AutoSurround }),
	enumOpt("virtual_space",
		[]string{VirtualSpaceOff, VirtualSpaceBlock, VirtualSpaceOn}, nil,
		func(c *Config) *string { return &c.VirtualSpace }),
	intOpt("scroll_offset", 0, 0, func(c *Config) *int { return &c.ScrollOffset }),
	enumOpt("default_line_ending",
		[]string{LineEndingAuto, LineEndingLF, LineEndingCRLF}, nil,
		func(c *Config) *string { return &c.LineEnding }),
	boolOpt("trim_trailing_whitespace_on_save", func(c *Config) *bool { return &c.TrimTrailingWhitespace }),
	boolOpt("ensure_final_newline_on_save", func(c *Config) *bool { return &c.EnsureFinalNewline }),
	boolOpt("auto_read_only", func(c *Config) *bool { return &c.AutoReadOnly }),

	// Bracket matching.
	boolOpt("highlight_matching_brackets", func(c *Config) *bool { return &c.HighlightMatchingBrackets }),
	boolOpt("rainbow_brackets", func(c *Config) *bool { return &c.RainbowBrackets }),

	// Completion, LSP, diagnostics.
	boolOpt("completion_popup_auto_show", func(c *Config) *bool { return &c.CompletionPopupAutoShow }),
	boolOpt("quick_suggestions", func(c *Config) *bool { return &c.QuickSuggestions }),
	intOpt("quick_suggestions_delay_ms", 0, 0, func(c *Config) *int { return &c.QuickSuggestionsDelayMS }),
	boolOpt("suggest_on_trigger_characters", func(c *Config) *bool { return &c.SuggestOnTriggerCharacters }),
	boolOpt("enable_inlay_hints", func(c *Config) *bool { return &c.EnableInlayHints }),
	boolOpt("diagnostics_inline_text", func(c *Config) *bool { return &c.DiagnosticsInlineText }),

	// Mouse.
	intOpt("mouse_wheel_scroll_lines", 0, 0, func(c *Config) *int { return &c.MouseWheelScrollLines }),
	boolOpt("mouse_hover_enabled", func(c *Config) *bool { return &c.MouseHover }),
	intOpt("mouse_hover_delay_ms", 0, 0, func(c *Config) *int { return &c.MouseHoverDelayMS }),
	intOpt("double_click_time_ms", 0, 0, func(c *Config) *int { return &c.DoubleClickTimeMS }),

	// Recovery.
	boolOpt("auto_save_enabled", func(c *Config) *bool { return &c.AutoSaveEnabled }),
	intOpt("auto_save_interval_secs", 0, 0, func(c *Config) *int { return &c.AutoSaveIntervalSecs }),
	boolOpt("hot_exit", func(c *Config) *bool { return &c.HotExit }),
	boolOpt("recovery_enabled", func(c *Config) *bool { return &c.RecoveryEnabled }),
	intOpt("auto_recovery_save_interval_secs", 0, 0, func(c *Config) *int { return &c.AutoRecoverySaveIntervalSecs }),
	intOpt("auto_revert_poll_interval_ms", 0, 0, func(c *Config) *int { return &c.AutoRevertPollIntervalMS }),

	// Startup and quit.
	boolOpt("confirm_quit", func(c *Config) *bool { return &c.ConfirmQuit }),
	boolOpt("save_session", func(c *Config) *bool { return &c.SaveSession }),
	boolOpt("restore_previous_session", func(c *Config) *bool { return &c.RestorePreviousSession }),
	boolOpt("skip_session_restore_when_files_passed", func(c *Config) *bool { return &c.SkipSessionRestoreWhenFilesPassed }),
	{key: "ephemeral_file_patterns", apply: applyEphemeral},
	boolOpt("auto_create_empty_buffer_on_last_buffer_close", func(c *Config) *bool { return &c.AutoCreateEmptyBufferOnLastBufferClose }),

	// Performance.
	intOpt("highlight_context_bytes", 0, 0, func(c *Config) *int { return &c.HighlightContextBytes }),
	intOpt("large_file_threshold_bytes", 0, 0, func(c *Config) *int { return &c.LargeFileThresholdBytes }),
	intOpt("estimated_line_length", 0, 0, func(c *Config) *int { return &c.EstimatedLineLength }),
}

// editorByKey indexes editorOptions by key.
var editorByKey = indexOptions()

func indexOptions() map[string]option {
	m := make(map[string]option, len(editorOptions))
	for _, o := range editorOptions {
		m[o.key] = o
	}
	return m
}

// statusBarElementNames are the elements editor.status_bar.left and .right accept,
// without their braces. They are Fresh's StatusBarElement names (fresh-editor-core
// config.rs TryFrom<String>). The list is repeated here because the config package
// cannot import internal/tui/frame, where the renderer parses the same names
// (frame.ParseStatusElement); a change to one belongs in both.
var statusBarElementNames = map[string]bool{
	"filename": true, "read_only": true, "cursor": true, "cursor:compact": true,
	"diagnostics": true, "cursor_count": true, "messages": true, "chord": true,
	"line_ending": true, "encoding": true, "language": true, "lsp": true,
	"warnings": true, "update": true, "terminal_restart": true, "palette": true,
	"clock": true, "remote": true, "trust": true,
}

// validStatusBarElement reports whether name is an element Fresh's status bar parses,
// with or without its braces. A name with a colon is a plugin token, which Fresh
// accepts too; gvim paints nothing for it (see docs/ui.md).
func validStatusBarElement(name string) bool {
	inner := strings.TrimSpace(name)
	if len(inner) >= 2 && strings.HasPrefix(inner, "{") && strings.HasSuffix(inner, "}") {
		inner = inner[1 : len(inner)-1]
	}
	if statusBarElementNames[inner] {
		return true
	}
	return strings.Contains(inner, ":")
}

// validateStatusBarSide checks one configured element list. Fresh's TryFrom fails on a
// name it does not know, so a typo is reported instead of silently painting nothing.
func validateStatusBarSide(side string, names []string) error {
	for _, name := range names {
		if !validStatusBarElement(name) {
			return fmt.Errorf("%s: unknown status bar element %q", side, name)
		}
	}
	return nil
}

func applyStatusBar(c *Config, raw json.RawMessage) error {
	var in struct {
		Left      *[]string `json:"left"`
		Right     *[]string `json:"right"`
		Separator *string   `json:"separator"`
	}
	if err := decodeStrict(raw, &in); err != nil {
		return err
	}
	if in.Left != nil {
		if err := validateStatusBarSide("left", *in.Left); err != nil {
			return err
		}
		c.StatusBar.Left = slices.Clone(*in.Left)
	}
	if in.Right != nil {
		if err := validateStatusBarSide("right", *in.Right); err != nil {
			return err
		}
		c.StatusBar.Right = slices.Clone(*in.Right)
	}
	if in.Separator != nil {
		c.StatusBar.Separator = *in.Separator
	}
	return nil
}

func applySearch(c *Config, raw json.RawMessage) error {
	var in struct {
		CaseSensitive *bool `json:"case_sensitive"`
		WholeWord     *bool `json:"whole_word"`
		Regex         *bool `json:"regex"`
		ConfirmEach   *bool `json:"confirm_each"`
	}
	if err := decodeStrict(raw, &in); err != nil {
		return err
	}
	set := func(dst *bool, v *bool) {
		if v != nil {
			*dst = *v
		}
	}
	set(&c.Search.CaseSensitive, in.CaseSensitive)
	set(&c.Search.WholeWord, in.WholeWord)
	set(&c.Search.Regex, in.Regex)
	set(&c.Search.ConfirmEach, in.ConfirmEach)
	return nil
}

func applyRulers(c *Config, raw json.RawMessage) error {
	var cols []int
	if err := json.Unmarshal(raw, &cols); err != nil {
		return errors.New("must be a list of column numbers")
	}
	for _, col := range cols {
		if col < 1 {
			return fmt.Errorf("columns must be at least 1, got %d", col)
		}
	}
	if len(cols) == 0 {
		cols = nil
	}
	c.Rulers = cols
	return nil
}

func applyGlyph(c *Config, raw json.RawMessage) error {
	s, err := decodeString(raw)
	if err != nil {
		return err
	}
	if s == "" {
		return errors.New("must not be empty")
	}
	c.IndentationGuideGlyph = s
	return nil
}

func applyEphemeral(c *Config, raw json.RawMessage) error {
	var patterns []string
	if err := json.Unmarshal(raw, &patterns); err != nil {
		return errors.New("must be a list of strings")
	}
	c.EphemeralFilePatterns = patterns
	return nil
}

// decodeStrict decodes the JSON object raw into dst and rejects unknown fields.
func decodeStrict(raw json.RawMessage, dst any) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return err
	}
	return nil
}

// sections are the top-level keys whose value is an object of settings. They are
// applied after the other top-level keys, in this order.
var sections = []string{"file_explorer", "editor", "languages", "clipboard"}

func isSection(key string) bool { return slices.Contains(sections, key) }

// sectionFailure is one rejected key inside a section, with its full dotted name.
type sectionFailure struct {
	key string
	err error
}

// setSection applies the object raw of the section ("editor", "file_explorer" or
// "languages"). It returns the dotted names of the keys that were applied and the
// ones that were rejected, in key order. A key that fails leaves its field unchanged.
func (c *Config) setSection(section string, raw json.RawMessage, l layer) (applied []string, failed []sectionFailure) {
	entries, err := decodeObject(raw)
	if err != nil {
		return nil, []sectionFailure{{section, err}}
	}
	if section == "languages" {
		return c.setLanguages(entries, l)
	}
	for _, k := range sortedKeys(entries) {
		full := section + "." + k
		if err := c.setSectionKey(section, k, entries[k]); err != nil {
			failed = append(failed, sectionFailure{full, err})
			continue
		}
		applied = append(applied, full)
	}
	return applied, failed
}

func (c *Config) setSectionKey(section, key string, raw json.RawMessage) error {
	if section == "file_explorer" {
		return c.setFileExplorer(key, raw)
	}
	if section == "clipboard" {
		return c.setClipboard(key, raw)
	}
	opt, ok := editorByKey[key]
	if !ok {
		return errors.New("unknown setting")
	}
	return opt.set(c, raw)
}

// setFileExplorer applies one key of "file_explorer".
func (c *Config) setFileExplorer(key string, raw json.RawMessage) error {
	switch key {
	case "show_hidden":
		return boolOpt(key, func(c *Config) *bool { return &c.ExplorerShowHidden }).set(c, raw)
	case "hide_gitignore":
		return setFileExplorerBool(raw, &c.FileExplorer.HideGitignore)
	case "width":
		return c.setFileExplorerWidth(raw)
	case "side":
		return c.setFileExplorerSide(raw)
	case "follow_active_buffer":
		return setFileExplorerBool(raw, &c.FileExplorer.FollowActiveBuffer)
	case "auto_open_on_last_buffer_close":
		return setFileExplorerBool(raw, &c.FileExplorer.AutoOpenOnLastBufferClose)
	case "compact_directories":
		return setFileExplorerBool(raw, &c.FileExplorer.CompactDirectories)
	case "respect_gitignore":
		return setFileExplorerBoolPtr(raw, &c.FileExplorer.RespectGitignore)
	case "custom_ignore_patterns":
		return setFileExplorerStringList(raw, &c.FileExplorer.CustomIgnorePatterns)
	case "preview_tabs":
		return setFileExplorerBoolPtr(raw, &c.FileExplorer.PreviewTabs)
	}
	return errors.New("unknown setting")
}

// decodeObject decodes raw as a JSON object of raw values.
func decodeObject(raw json.RawMessage) (map[string]json.RawMessage, error) {
	trimmed := bytes.TrimSpace(raw)
	var m map[string]json.RawMessage
	if len(trimmed) == 0 || trimmed[0] != '{' || json.Unmarshal(trimmed, &m) != nil {
		return nil, errors.New("must be a JSON object")
	}
	return m, nil
}

// lowerPluginTimeout applies plugin_timeout_ms from the project layer. The
// effective value is min(user, project): a repository can shorten the time a
// plugin may run but never extend it. A larger value is rejected and the value
// from the layers below stays.
func (c *Config) lowerPluginTimeout(raw json.RawMessage) error {
	before := c.PluginTimeout
	if err := c.set("plugin_timeout_ms", raw); err != nil {
		return err
	}
	if c.PluginTimeout > before {
		c.PluginTimeout = before
		return errors.New("project config cannot raise plugin_timeout_ms")
	}
	return nil
}

// setPlugins applies the "plugins" object of one layer: {"<name>": {"enabled": bool,
// "settings": {...}}}. It returns the dotted names of the keys that were applied and
// the rejected ones, in key order. A name that is not an object, or a key that is
// rejected, leaves that entry unchanged. The project layer cannot set enabled to
// true, so a repository can switch a plugin off or tune it but never turn code on.
func (c *Config) setPlugins(raw json.RawMessage, l layer) (applied []string, failed []sectionFailure) {
	entries, err := decodeObject(raw)
	if err != nil {
		return nil, []sectionFailure{{"plugins", err}}
	}
	for _, name := range sortedKeys(entries) {
		full := "plugins." + name
		fields, err := decodeObject(entries[name])
		if err != nil {
			failed = append(failed, sectionFailure{full, err})
			continue
		}
		for _, k := range sortedKeys(fields) {
			key := full + "." + k
			if err := c.setPluginField(name, k, fields[k], l); err != nil {
				failed = append(failed, sectionFailure{key, err})
				continue
			}
			applied = append(applied, key)
		}
	}
	return applied, failed
}

// setPluginField applies one key of plugins.<name>.
func (c *Config) setPluginField(name, key string, raw json.RawMessage, l layer) error {
	if isNull(raw) {
		return errors.New("null is not a valid value")
	}
	switch key {
	case "enabled":
		var v bool
		if err := json.Unmarshal(raw, &v); err != nil {
			return errors.New("must be true or false")
		}
		if l == layerProject && v {
			return errors.New("project config cannot enable a plugin")
		}
		pc := c.Plugins[name]
		pc.Enabled = &v
		c.putPlugin(name, pc)
	case "settings":
		var v map[string]any
		if trimmed := bytes.TrimSpace(raw); len(trimmed) == 0 || trimmed[0] != '{' || json.Unmarshal(trimmed, &v) != nil {
			return errors.New("must be a JSON object")
		}
		pc := c.Plugins[name]
		if l == layerProject {
			pc.ProjectSettings = v
		} else {
			pc.Settings = v
		}
		c.putPlugin(name, pc)
	default:
		return errors.New("unknown setting")
	}
	return nil
}

func (c *Config) putPlugin(name string, pc PluginConfig) {
	if c.Plugins == nil {
		c.Plugins = make(map[string]PluginConfig)
	}
	c.Plugins[name] = pc
}
