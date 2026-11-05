package ui

import (
	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"

	"github.com/tbxark/gvim/internal/ui/style"
)

// Hint is one keybinding the footer shows: its chord text and what it does.
type Hint struct {
	Keys string // chord text, for example "Ctrl+S"
	Desc string // what the binding does, for example "save"
}

// HintSource lists the hints of a focus context in display order. The app
// adapter maps the keymap bindings of the context to hints. ui does not import
// keymap, so the mapping lives on the other side of this interface.
type HintSource interface {
	Hints(ctx KeyContext) []Hint
}

// footer shows the bindings of the focused context in the short form of
// bubbles/help. It takes no rows of its own in the current geometry; the
// layout phase that adds a footer band calls View.
type footer struct {
	help help.Model
	src  HintSource
}

// newFooter returns a footer that reads its hints from src and draws them in
// the status colors. A nil src shows nothing.
func newFooter(src HintSource, st style.Styles) footer {
	h := help.New()
	h.Styles = help.Styles{
		Ellipsis:       st.Status,
		ShortKey:       st.Status.Bold(true),
		ShortDesc:      st.Status,
		ShortSeparator: st.Status,
		FullKey:        st.Status.Bold(true),
		FullDesc:       st.Status,
		FullSeparator:  st.Status,
	}
	return footer{help: h, src: src}
}

// bindings returns the key bindings the footer shows for ctx. Hints without
// chord text are skipped.
func (f footer) bindings(ctx KeyContext) []key.Binding {
	if f.src == nil {
		return nil
	}
	hints := f.src.Hints(ctx)
	out := make([]key.Binding, 0, len(hints))
	for _, h := range hints {
		if h.Keys == "" {
			continue
		}
		out = append(out, key.NewBinding(key.WithKeys(h.Keys), key.WithHelp(h.Keys, h.Desc)))
	}
	return out
}

// View renders the short help for ctx on one line no wider than width. Hints that
// do not fit are dropped from the end, as bubbles/help does.
func (f footer) View(ctx KeyContext, width int) string {
	f.help.SetWidth(width)
	return f.help.ShortHelpView(f.bindings(ctx))
}
