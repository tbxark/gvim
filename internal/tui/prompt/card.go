package prompt

// The shared card helpers of the three prompts. A prompt is a centered card, the
// same frame the dialog package paints: the box geometry is space.CardWidth and
// space.CardBox, the buttons are style.Overlay.PaintButton.

import "github.com/tbxark/gvim/i18n"

// cardTitle returns the title of a prompt card: t when the opener gave one, the
// fallback otherwise.
func cardTitle(t, key, fallback string) string {
	if t != "" {
		return t
	}
	return i18n.TOr("", key, fallback, nil)
}

// cardGap is the gap between two buttons of a card.
const cardGap = 2
