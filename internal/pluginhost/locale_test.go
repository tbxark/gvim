package pluginhost

import (
	"testing"

	"github.com/tbxark/gvim/config"
	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/i18n"
)

// TestLocaleFlowsIntoThePluginRuntime checks the app-to-plugin locale wiring: the runtime
// the host builds translates for the editor's active locale.
func TestLocaleFlowsIntoThePluginRuntime(t *testing.T) {
	t.Cleanup(func() { i18n.SetLocale("en") })
	i18n.SetLocale("zh-CN")
	w := New(config.Defaults(), editor.New(editor.Options{}), func(*editor.Document) {}, Options{})
	if got := w.Runtime.Locale(); got != "zh-CN" {
		t.Fatalf("plugin runtime locale = %q, want the editor locale zh-CN", got)
	}
}
