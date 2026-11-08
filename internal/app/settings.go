package app

import (
	"time"

	"github.com/tbxark/gvim/config"
	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/internal/tui"
)

// languageSettings returns the resolver that gives the editor and the UI the effective
// per-language settings of a document. It reads cfg once per call, so it keeps the
// config alive but changes nothing in it.
func languageSettings(cfg config.Config) editor.SettingsResolver {
	return func(language, path string) editor.LanguageSettings {
		return languageSettingsOf(cfg.EditorFor(language, path))
	}
}

// languageSettingsOf converts the config result to the editor's plain type.
func languageSettingsOf(s config.EditorSettings) editor.LanguageSettings {
	out := editor.LanguageSettings{
		Language:               s.Language,
		TabSize:                s.TabSize,
		UseTabs:                s.UseTabs,
		AutoIndent:             s.AutoIndent,
		AutoClose:              s.AutoClose,
		AutoSurround:           s.AutoSurround,
		LineWrap:               s.LineWrap,
		WrapColumn:             s.WrapColumn,
		PageView:               s.PageView,
		PageWidth:              s.PageWidth,
		WhitespaceTabsLeading:  s.Whitespace.TabsLeading,
		WhitespaceTabsInner:    s.Whitespace.TabsInner,
		WhitespaceTabsTrailing: s.Whitespace.TabsTrailing,
		IndentationGuide:       s.IndentationGuide,
		CommentPrefix:          s.CommentPrefix,
		WordCharacters:         s.WordCharacters,
		FormatOnSave:           s.FormatOnSave,
	}
	if f := s.Formatter; f != nil {
		out.Formatter = &editor.FormatterSpec{Command: f.Command, Args: f.Args, Stdin: f.Stdin, TimeoutMS: f.TimeoutMS}
	}
	for _, a := range s.OnSave {
		out.OnSave = append(out.OnSave, editor.OnSaveAction{
			Command: a.Command, Args: a.Args, WorkingDir: a.WorkingDir,
			Stdin: a.Stdin, TimeoutMS: a.TimeoutMS, Enabled: a.Enabled,
		})
	}
	if r := s.Indent; r != nil {
		out.Indent = &editor.IndentRules{
			IncreaseIndentPattern: r.IncreaseIndentPattern,
			DecreaseIndentPattern: r.DecreaseIndentPattern,
			IndentNextLinePattern: r.IndentNextLinePattern,
			DedentNextLinePattern: r.DedentNextLinePattern,
			SelfClosePattern:      r.SelfClosePattern,
		}
	}
	return out
}

// autoSaveInterval returns the auto-save interval. A non-zero flagged value (from
// Options.AutoSaveInterval) wins; otherwise editor.auto_save_enabled turns on
// editor.auto_save_interval_secs. Zero means auto-save is off.
func autoSaveInterval(flagged time.Duration, cfg config.Config) time.Duration {
	if flagged != 0 {
		return flagged
	}
	if cfg.AutoSaveEnabled && cfg.AutoSaveIntervalSecs > 0 {
		return time.Duration(cfg.AutoSaveIntervalSecs) * time.Second
	}
	return 0
}

// viewOptions returns the starting display options that cfg gives. icons is the
// resolved icon set, "plain" or "nerd".
func viewOptions(cfg config.Config, icons string) *tui.ViewOptions {
	return &tui.ViewOptions{
		ShowTabBar:              cfg.ShowTabBar,
		ShowStatusBar:           cfg.ShowStatusBar,
		ShowPromptLine:          cfg.ShowPromptLine,
		ShowVerticalScrollbar:   cfg.ShowVerticalScrollbar,
		ShowHorizontalScrollbar: cfg.ShowHorizontalScrollbar,
		LineWrap:                cfg.LineWrap,
		HighlightCurrentLine:    cfg.HighlightCurrentLine,
		HighlightOccurrences:    cfg.HighlightOccurrences,
		ShowWhitespace:          cfg.ShowWhitespace,
		CursorStyle:             cfg.CursorStyle,
		MouseCapture:            cfg.MouseCapture,
		MouseHover:              cfg.MouseHover,
		ScrollSync:              cfg.ScrollSync,
		UIIcons:                 icons,

		RelativeLineNumbers:        cfg.RelativeLineNumbers,
		HideCurrentLineOnSelection: cfg.HideCurrentLineOnSelection,
		HighlightCurrentColumn:     cfg.HighlightCurrentColumn,
		WrapIndent:                 cfg.WrapIndent,
		SyntaxHighlighting:         cfg.SyntaxHighlighting,
		ShowTilde:                  cfg.ShowTilde,
		IndentationGuideMode:       cfg.IndentationGuide,
		IndentationGuideGlyph:      cfg.IndentationGuideGlyph,
		RainbowIndentation:         cfg.RainbowIndentation,
		Whitespace: tui.WhitespaceOptions{
			SpacesLeading:  cfg.WhitespaceSpacesLeading,
			SpacesInner:    cfg.WhitespaceSpacesInner,
			SpacesTrailing: cfg.WhitespaceSpacesTrailing,
			Newlines:       cfg.WhitespaceNewlines,
			CarriageReturn: cfg.WhitespaceCarriageReturns,
			InSelection:    cfg.WhitespaceInSelection,
		},
		HighlightMatchingBrackets: cfg.HighlightMatchingBrackets,
		RainbowBrackets:           cfg.RainbowBrackets,
		Search: tui.SearchOptions{
			CaseSensitive: cfg.Search.CaseSensitive,
			WholeWord:     cfg.Search.WholeWord,
			Regex:         cfg.Search.Regex,
			ConfirmEach:   cfg.Search.ConfirmEach,
		},
		StatusBar: tui.StatusBarOptions{
			Left:      append([]string(nil), cfg.StatusBar.Left...),
			Right:     append([]string(nil), cfg.StatusBar.Right...),
			Separator: cfg.StatusBar.Separator,
		},
		MenuBarMnemonics:       cfg.MenuBarMnemonics,
		Animations:             cfg.Animations,
		CursorJumpAnimation:    cfg.CursorJumpAnimation,
		ViewportEdgeFade:       cfg.ViewportEdgeFade,
		SmoothScroll:           cfg.SmoothScroll,
		UseTerminalBG:          cfg.UseTerminalBG,
		SetWindowTitle:         cfg.SetWindowTitle,
		TerminalAutoTitle:      cfg.TerminalAutoTitle,
		ScreensaverEnabled:     cfg.ScreensaverEnabled,
		ScreensaverIdleMinutes: cfg.ScreensaverIdleMinutes,

		MouseWheelScrollLines: cfg.MouseWheelScrollLines,
		DoubleClickTimeMS:     cfg.DoubleClickTimeMS,
		MouseHoverDelayMS:     cfg.MouseHoverDelayMS,

		EnableInlayHints:           cfg.EnableInlayHints,
		EnableSemanticTokensFull:   cfg.EnableSemanticTokensFull,
		DiagnosticsInlineText:      cfg.DiagnosticsInlineText,
		CompletionPopupAutoShow:    cfg.CompletionPopupAutoShow,
		QuickSuggestions:           cfg.QuickSuggestions,
		QuickSuggestionsDelayMS:    cfg.QuickSuggestionsDelayMS,
		SuggestOnTriggerCharacters: cfg.SuggestOnTriggerCharacters,
	}
}

// autoRevertPoll is how often the root checks the open files for outside changes:
// editor.auto_revert_poll_interval_ms.
func autoRevertPoll(cfg config.Config) time.Duration {
	return time.Duration(cfg.AutoRevertPollIntervalMS) * time.Millisecond
}
