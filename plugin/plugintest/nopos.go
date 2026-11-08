package plugintest

import "github.com/tbxark/gvim/plugin"

// NopOS refuses every file and process operation with CodeUnsupported. A harness uses a
// FakeOS (fake_os.go) unless Options.OS says otherwise.
type NopOS = plugin.NopOS
