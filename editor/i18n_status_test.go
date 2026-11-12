package editor

import (
	"testing"

	"github.com/tbxark/gvim/i18n"
)

// TestClipStatusUsesTheActiveLocale checks the extraction of the clipboard statuses: the
// English text is unchanged, and a registered catalogue's text is shown instead. It is not
// parallel because it changes the process locale; the other tests in the package are
// paused while it runs and it restores English before returning.
func TestClipStatusUsesTheActiveLocale(t *testing.T) {
	i18n.Register("xx", map[string]string{
		"clipboard.copied":      "kopia",
		"clipboard.copied_line": "kopia rad",
		"clipboard.pasted":      "inklistrad",
	})
	i18n.SetLocale("xx")
	t.Cleanup(func() {
		i18n.SetLocale("en")
		i18n.Register("xx", nil)
	})
	e := clipEditor(t, "one")
	e.Do("copy", nil)
	if got := e.Status(); got != "kopia rad" {
		t.Fatalf("copy status = %q, want the translated line copy", got)
	}
	e.Do("paste", map[string]any{"text": "x"})
	if got := e.Status(); got != "inklistrad" {
		t.Fatalf("paste status = %q, want the translated paste", got)
	}
	// The English catalogue keeps the original text.
	i18n.SetLocale("en")
	e.Do("copy", nil)
	if got := e.Status(); got != "Copied line" {
		t.Fatalf("English copy status = %q, want Copied line", got)
	}
}

// TestClipStatusUsesTheEmbeddedChineseCatalogue checks the shipped translation files:
// with the embedded zh-CN catalogue active, the extracted clipboard statuses show
// Fresh's Chinese text without any test registration. It is not parallel because it
// changes the process locale, and it restores English.
func TestClipStatusUsesTheEmbeddedChineseCatalogue(t *testing.T) {
	i18n.SetLocale("zh-CN")
	t.Cleanup(func() { i18n.SetLocale("en") })
	e := clipEditor(t, "one")
	e.Do("copy", nil)
	if got := e.Status(); got != "已复制行" {
		t.Fatalf("zh-CN copy status = %q, want the embedded Chinese line copy", got)
	}
	e.Do("paste", map[string]any{"text": "x"})
	if got := e.Status(); got != "已粘贴" {
		t.Fatalf("zh-CN paste status = %q, want the embedded Chinese paste", got)
	}
	// Switching back to English renders the source text again on the next action.
	i18n.SetLocale("en")
	e.Do("copy", nil)
	if got := e.Status(); got != "Copied line" {
		t.Fatalf("English copy status = %q, want Copied line", got)
	}
}
