package prompt

import (
	"testing"

	"github.com/tbxark/gvim/i18n"
)

// TestPromptDefaultsFallBackToEnglish checks the extracted prompt strings: with German
// active, gvim-only prompt keys show their English text (never the bare key), and the
// line-rejection message keeps its English wording. Not parallel: locale.
func TestPromptDefaultsFallBackToEnglish(t *testing.T) {
	i18n.SetLocale("de")
	t.Cleanup(func() { i18n.SetLocale("en") })
	c := NewConfirmWith(testStyles(), "Replace?", "tag", ConfirmOptions{})
	if c.yesText != "Yes" || c.noText != "No" {
		t.Errorf("German default answers = %q/%q, want the English fallback", c.yesText, c.noText)
	}
	if err := ValidateLineCol("x"); err == nil || err.Error() != "Not a line number: x" {
		t.Errorf("rejection = %v, want the English fallback message", err)
	}
}
