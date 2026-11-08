// Package bundled embeds the Lua plugins that ship inside the gvim binary
// (plugins/<name>/{plugin.json,init.lua}) and the Lua modules the runtime loads on
// demand (lib/<name>.lua). Discovery ignores entries that are not directories with a
// plugin.json or .lua file.
package bundled

import (
	"embed"
	"io/fs"
	"strings"
)

// FS holds the embedded plugin and library files. Paths start with "plugins/" or "lib/".
//
//go:embed all:plugins all:lib
var FS embed.FS

// Lib returns the source of the bundled Lua module lib/<name>.lua, which the runtime's
// require loads for "gvim.<name>". The name must be a single path element: an empty
// name, one with a separator, a dot or a ".lua" suffix is not found.
func Lib(name string) (string, bool) { return lib(FS, name) }

func lib(fsys fs.FS, name string) (string, bool) {
	if name == "" || strings.ContainsAny(name, `/\.`) {
		return "", false
	}
	data, err := fs.ReadFile(fsys, "lib/"+name+".lua")
	if err != nil {
		return "", false
	}
	return string(data), true
}
