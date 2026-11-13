package editor

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

// viewFlag is one per-document flag action: the field it sets, the default that
// applies with no override and no value, and the status text it leaves.
type viewFlag struct {
	field func(*ViewSettings) **bool
	def   func(*Editor) bool
	Label string
}

// constDefault is the default of a flag that does not depend on the options.
func constDefault(v bool) func(*Editor) bool {
	return func(*Editor) bool { return v }
}

// viewFlags lists the flag actions that set one override. A flag that has a UI
// global (line numbers, wrap, current line, occurrences, whitespace) is sent with
// its value, so the default here matters only for a call without one.
var viewFlags = map[Action]viewFlag{
	"toggle_line_numbers_current_buffer": {
		func(s *ViewSettings) **bool { return &s.LineNumbers }, constDefault(true), "Line numbers",
	},
	"toggle_line_wrap_current_buffer": {
		func(s *ViewSettings) **bool { return &s.LineWrap }, constDefault(true), "Line wrap",
	},
	"toggle_current_line_highlight_current_buffer": {
		func(s *ViewSettings) **bool { return &s.CurrentLine }, constDefault(true), "Current line highlight",
	},
	"toggle_occurrence_highlight_current_buffer": {
		func(s *ViewSettings) **bool { return &s.Occurrences }, constDefault(false), "Occurrence highlight",
	},
	"toggle_whitespace_indicators": {
		func(s *ViewSettings) **bool { return &s.Whitespace }, constDefault(false), "Whitespace indicators",
	},
	"toggle_indentation_guide_current_buffer": {
		func(s *ViewSettings) **bool { return &s.IndentGuides }, constDefault(false), "Indentation guides",
	},
	"toggle_fold_indicators_current_buffer": {
		func(s *ViewSettings) **bool { return &s.FoldIndicators }, constDefault(true), "Fold indicators",
	},
	"toggle_tab_indicators": {
		func(s *ViewSettings) **bool { return &s.TabIndicators }, constDefault(true), "Tab indicators",
	},
	"toggle_virtual_space_current_buffer": {
		func(s *ViewSettings) **bool { return &s.VirtualSpace }, constDefault(false),
		"Virtual space",
	},
	"toggle_indentation_style": {
		func(s *ViewSettings) **bool { return &s.IndentTabs }, func(e *Editor) bool { return e.opts.UseTabs },
		"Indentation with tabs",
	},
}

// registerViewHandlers adds the per-document view actions to h. The init below adds
// them to the handler table once handlers.go has built it, so guardDocument does not
// wrap them: none of them changes text.
func registerViewHandlers(h map[Action]handler) {
	for name, f := range viewFlags {
		h[name] = toggleViewFlag(f)
	}
	h["toggle_page_view"] = docAction(togglePageView)
	h["add_ruler"] = docAction(addRuler)
	h["remove_ruler"] = docAction(removeRuler)
	h["set_tab_size"] = docAction(setTabSize)
	h["set_page_width"] = docAction(setPageWidth)
	h["set_language"] = docAction(setLanguage)
}

func init() {
	registerViewHandlers(handlers)
}

// toggleViewFlag sets one flag. A "value" bool argument sets it; without one the
// flag flips from its current value.
func toggleViewFlag(f viewFlag) handler {
	return docAction(func(e *Editor, d *Document, args map[string]any) error {
		value, err := boolArg(args, "value", !f.current(e, d))
		if err != nil {
			return err
		}
		v := d.View()
		p := f.field(v)
		*p = &value
		e.setStatus(f.Label + ": " + onOff(value) + " for this buffer")
		return nil
	})
}

// current reads the flag as the document has it now.
func (f viewFlag) current(e *Editor, d *Document) bool {
	return OverrideOr(*f.field(d.View()), f.def(e))
}

// togglePageView sets the page view flag of the buffer. The page view is drawn
// from the per-document view state (docs/markdown.md): the text is centered in a
// page and, for Markdown, the inline marks are composed.
func togglePageView(e *Editor, d *Document, args map[string]any) error {
	current, err := boolArg(args, "value", !OverrideOr(d.View().PageView, false))
	if err != nil {
		return err
	}
	d.View().PageView = &current
	e.setStatus("Page view: " + onOff(current) + " for this buffer")
	return nil
}

// addRuler adds a ruler at the column given by "column". A ruler already there
// stays once.
func addRuler(e *Editor, d *Document, args map[string]any) error {
	col, err := intArg(args, "column", 1, MaxViewColumn)
	if err != nil {
		return err
	}
	if !slices.Contains(d.View().Rulers, col) {
		d.View().Rulers = append(slices.Clone(d.View().Rulers), col)
	}
	e.setStatus(fmt.Sprintf("Ruler added at column %d", col))
	return nil
}

// removeRuler removes the ruler at the column given by "column". It is an error
// when no ruler is there.
func removeRuler(e *Editor, d *Document, args map[string]any) error {
	col, err := intArg(args, "column", 1, MaxViewColumn)
	if err != nil {
		return err
	}
	i := slices.Index(d.View().Rulers, col)
	if i < 0 {
		return fmt.Errorf("no ruler at column %d", col)
	}
	d.View().Rulers = slices.Concat(d.View().Rulers[:i], d.View().Rulers[i+1:])
	e.setStatus(fmt.Sprintf("Ruler removed at column %d", col))
	return nil
}

// setTabSize sets the columns per tab stop of the document, from "size".
func setTabSize(e *Editor, d *Document, args map[string]any) error {
	size, err := intArg(args, "size", 1, MaxViewTabSize)
	if err != nil {
		return err
	}
	d.View().TabSize = size
	e.setStatus(fmt.Sprintf("Tab size %d for this buffer", size))
	return nil
}

// setPageWidth sets the page view width of the document, from "width". The page
// view draws the text in a page of that many columns; see docs/markdown.md.
func setPageWidth(e *Editor, d *Document, args map[string]any) error {
	width, err := intArg(args, "width", 1, MaxViewColumn)
	if err != nil {
		return err
	}
	d.View().PageWidth = width
	e.setStatus(fmt.Sprintf("Page width %d for this buffer", width))
	return nil
}

// setLanguage names the language of the document, from "name". The name is kept
// as the tab's language; an empty name clears it.
func setLanguage(e *Editor, d *Document, args map[string]any) error {
	name, err := stringArg(args, "name")
	if err != nil {
		return err
	}
	d.SetLanguage(strings.TrimSpace(name))
	e.setStatus("Language: " + orString(d.Language(), "Text"))
	return nil
}

// orString returns s, or fallback when s is empty.
func orString(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

// onOff is "on" or "off".
func onOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

// errBadArg is the error a view action returns for an argument it cannot use.
var errBadArg = errors.New("view action argument")

// boolArg returns args[key] as a bool, or def when the key is absent. A value of
// another type is an error.
func boolArg(args map[string]any, key string, def bool) (bool, error) {
	v, ok := args[key]
	if !ok {
		return def, nil
	}
	b, ok := v.(bool)
	if !ok {
		return false, fmt.Errorf("%w: %s must be true or false", errBadArg, key)
	}
	return b, nil
}

// intArg returns args[key] as an integer from lo to hi. JSON numbers arrive as
// float64, and both forms are accepted.
func intArg(args map[string]any, key string, lo, hi int) (int, error) {
	var n int
	switch v := args[key].(type) {
	case int:
		n = v
	case float64:
		n = int(v)
		if float64(n) != v {
			return 0, fmt.Errorf("%w: %s must be a whole number", errBadArg, key)
		}
	default:
		return 0, fmt.Errorf("%w: %s must be a number from %d to %d", errBadArg, key, lo, hi)
	}
	if n < lo || n > hi {
		return 0, fmt.Errorf("%w: %s must be from %d to %d, got %d", errBadArg, key, lo, hi, n)
	}
	return n, nil
}

// stringArg returns args[key] as a string.
func stringArg(args map[string]any, key string) (string, error) {
	s, ok := args[key].(string)
	if !ok {
		return "", fmt.Errorf("%w: %s must be a string", errBadArg, key)
	}
	return s, nil
}
