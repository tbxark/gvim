package tui

import (
	"strings"
	"testing"

	"github.com/tbxark/gvim/i18n"
)

// TestLSPRemoteRootNoticeCatalogue: the remote root_markers degradation notice is
// rendered by the TUI catalogue in English and falls back to the English text in Chinese
// instead of leaking a raw key or an English string from the port.
func TestLSPRemoteRootNoticeCatalogue(t *testing.T) {
	notice := LSPNotice{
		Key:     "gvim.notice.lsp.remote_root_failed",
		English: "Remote LSP root_markers scan failed: %{error}; using the workspace root",
		Vars:    map[string]any{"error": "permission denied"},
	}
	i18n.SetLocale("en")
	if got := lspNoticeText(notice); !strings.Contains(got, "permission denied") || !strings.Contains(got, "using the workspace root") {
		t.Fatalf("English notice = %q", got)
	}
	i18n.SetLocale("zh-CN")
	t.Cleanup(func() { i18n.SetLocale("en") })
	if got := lspNoticeText(notice); got != "Remote LSP root_markers scan failed: permission denied; using the workspace root" {
		t.Fatalf("Chinese fallback notice = %q", got)
	}
}
