// Package plugin is the Lua plugin runtime: one gopher-lua state per plugin, a task
// scheduler with time and memory budgets, the sandbox, require, handles, and the gvim API
// modules. It implements pluginapi.Runtime for internal/tui and talks to the editor
// through pluginapi.Host.
//
// Everything here runs on the Bubble Tea Update goroutine. The only exceptions are the Work
// closures it hands out (plain Go data in, plain Go data out), the slice timers and the heap
// guard. The package never touches the filesystem or starts a process itself: that is
// hostos.OS behind Work (the one exception is discovery, which reads plugin directories
// inside its Work through LoaderOptions.Open).
//
// # File map
//
// The package is one unit on purpose: the files below share the runtime's kernel state
// directly, so a split would have to export most of it. The map is here to find them.
//
//   - runtime kernel: runtime.go, plugin.go, task.go, sandbox.go, slice.go,
//     handles.go, convert.go, limits.go, log.go, hostcall.go, ioruntime.go,
//     require.go, timerpace.go, errfmt.go, bindings.go, lint_bindings.go. The
//     Runtime and plugin state, Lua entry points and the yield protocol, tasks,
//     budgets/strikes and the heap guard, the handle registry, Go↔Lua conversion,
//     the sandbox, require, error formatting, key bindings.
//   - gvim.* API installers: api_*.go, one file per area, installed in the order of
//     api_modules.go; bindings.go holds the shared binding helpers. They stay here
//     because they call the kernel directly (about 120 symbols, most unexported).
//   - data libraries: data_*.go (gvim.json, gvim.re, gvim.settings, gvim.text,
//     gvim.diff, gvim.time, gvim.utf8) and stdlib*.go (the repaired Lua 5.1
//     stdlib: tonumber, table.*, string.*, os.*).
//   - loader: discover.go, manifest.go, order.go, lifecycle.go, control.go.
//     Discovery, plugin.json, dependency order, enable/disable/reload,
//     plugin-state.json.
//   - OS port: the hostos package is the file, process and watch interface behind
//     gvim.fs and gvim.proc; internal/pluginhost implements it for the editor and
//     plugin/plugintest for tests.
package plugin
