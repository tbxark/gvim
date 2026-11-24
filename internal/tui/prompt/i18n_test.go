package prompt

import (
	"strings"
	"testing"

	"github.com/tbxark/gvim/i18n"
)

// TestPromptDefaultsFallBackToEnglish checks the extracted prompt strings: with Chinese
// active, gvim-only prompt keys show their English text (never the bare key), and the
// line-rejection message keeps its English wording. Not parallel: locale.
func TestPromptDefaultsFallBackToEnglish(t *testing.T) {
	i18n.SetLocale("zh-CN")
	t.Cleanup(func() { i18n.SetLocale("en") })
	c := NewConfirmWith(testStyles(), "Replace?", "tag", ConfirmOptions{})
	if c.yesText != "Yes" || c.noText != "No" {
		t.Errorf("Chinese default answers = %q/%q, want the English fallback", c.yesText, c.noText)
	}
	if err := ValidateLineCol("x"); err == nil || err.Error() != "Not a line number: x" {
		t.Errorf("rejection = %v, want the English fallback message", err)
	}
}

// TestFallbackTitleRendersInChinese checks the embedded zh-CN translation of the
// fallback prompt title paints on the card's top border. Not parallel: locale.
func TestFallbackTitleRendersInChinese(t *testing.T) {
	i18n.SetLocale("zh-CN")
	t.Cleanup(func() { i18n.SetLocale("en") })
	l := NewLine(testStyles(), LineOptions{})
	if l.title != "输入" {
		t.Fatalf("line title = %q, want 输入", l.title)
	}
	lay := l.layout(60, 12)
	if row := draw(l, 60, 12)[lay.box.Y]; !strings.Contains(row, "输入") {
		t.Fatalf("the card's title row %q misses 输入", row)
	}
}
