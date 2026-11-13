package plugin

import (
	lua "github.com/yuin/gopher-lua"

	"github.com/tbxark/gvim/pluginapi"
)

// dataState is what the data API keeps per plugin: the settings of the config files and
// the effective values, the table exported to other plugins, the compiled regular
// expressions and the markers of json.
type dataState struct {
	user, project map[string]any  // plugins.<name>.settings of the two config layers
	settings      *settingsState  // set by gvim.settings.define
	warned        map[string]bool // config keys already reported as unknown

	exports     map[string]lua.LValue // exported functions
	exportsData map[string]any        // exported plain data

	re      map[string]*reProg
	null    *lua.LUserData // json.null
	arrayMT *lua.LTable    // metatable that marks a table as a json array
}

// installData installs the data libraries: the globals json, re, utf8 and bit
// (also reachable as gvim.json, ...), gvim.text, gvim.time, gvim.theme, gvim.diff,
// gvim.settings and gvim.config.
func installData(e *apiEnv) {
	for _, lib := range []struct {
		name string
		t    *lua.LTable
	}{
		{"json", installJSON(e)},
		{"re", installRE(e)},
		{"utf8", installUTF8(e)},
		{"bit", installBit(e)},
	} {
		e.gvim.RawSetString(lib.name, lib.t)
		e.p.areas[lib.name] = lib.t
		e.L.SetGlobal(lib.name, lib.t)
	}
	installText(e)
	installTime(e)
	installTheme(e)
	installDiff(e)
	installSettings(e)
	installConfig(e)
}

// errReturn pushes `nil, "<code>: <msg>"` for err and returns the number of results.
func errReturn(L *lua.LState, err error) int {
	return bufRet(L, err)
}

// limitErr is a CodeLimit error.
func limitErr(format string, args ...any) error {
	return pluginapi.Errf(pluginapi.CodeLimit, format, args...)
}

// invalidErr is a CodeInvalid error.
func invalidErr(format string, args ...any) error {
	return pluginapi.Errf(pluginapi.CodeInvalid, format, args...)
}

// notFoundErr is a CodeNotFound error.
func notFoundErr(format string, args ...any) error {
	return pluginapi.Errf(pluginapi.CodeNotFound, format, args...)
}
