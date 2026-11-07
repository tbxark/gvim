package plugin

import (
	"net/url"
	"path/filepath"
	"runtime"
	"strings"

	lua "github.com/yuin/gopher-lua"

	"github.com/tbxark/gvim/internal/pluginapi"
)

// installPath installs the pure path helpers of gvim.path (FINAL 5.8). They follow the
// path syntax of the platform the editor runs on and never touch the filesystem.
func installPath(e *apiEnv) {
	t := e.area("path")
	t.RawSetString("sep", lua.LString(string(filepath.Separator)))

	e.set(t, "join", func(L *lua.LState) int {
		parts := make([]string, 0, L.GetTop())
		for i := 1; i <= L.GetTop(); i++ {
			parts = append(parts, L.CheckString(i))
		}
		L.Push(lua.LString(filepath.Join(parts...)))
		return 1
	})
	e.set(t, "dirname", func(L *lua.LState) int {
		L.Push(lua.LString(filepath.Dir(L.CheckString(1))))
		return 1
	})
	e.set(t, "basename", func(L *lua.LState) int {
		L.Push(lua.LString(filepath.Base(L.CheckString(1))))
		return 1
	})
	e.set(t, "extname", func(L *lua.LState) int {
		L.Push(lua.LString(extName(L.CheckString(1))))
		return 1
	})
	e.set(t, "is_absolute", func(L *lua.LState) int {
		L.Push(lua.LBool(filepath.IsAbs(L.CheckString(1))))
		return 1
	})
	e.set(t, "normalize", func(L *lua.LState) int {
		L.Push(lua.LString(filepath.Clean(L.CheckString(1))))
		return 1
	})
	e.set(t, "relative", func(L *lua.LState) int {
		rel, err := filepath.Rel(L.CheckString(1), L.CheckString(2))
		if err != nil {
			v, s := errTuple(pluginapi.Errf(pluginapi.CodeInvalid, "%v", err))
			L.Push(v)
			L.Push(s)
			return 2
		}
		L.Push(lua.LString(rel))
		return 1
	})
	e.set(t, "split", func(L *lua.LState) int {
		p := L.CheckString(1)
		L.Push(lua.LString(filepath.Dir(p)))
		L.Push(lua.LString(filepath.Base(p)))
		return 2
	})
	e.set(t, "to_uri", func(L *lua.LState) int {
		uri, err := pathToURI(L.CheckString(1))
		if err != nil {
			v, s := errTuple(err)
			L.Push(v)
			L.Push(s)
			return 2
		}
		L.Push(lua.LString(uri))
		return 1
	})
	e.set(t, "from_uri", func(L *lua.LState) int {
		p, err := uriToPath(L.CheckString(1))
		if err != nil {
			v, s := errTuple(err)
			L.Push(v)
			L.Push(s)
			return 2
		}
		L.Push(lua.LString(p))
		return 1
	})
}

// extName is the extension of the last element including the dot; a dotfile such as
// ".bashrc" has none.
func extName(p string) string {
	base := filepath.Base(p)
	i := strings.LastIndexByte(base, '.')
	if i <= 0 {
		return ""
	}
	return base[i:]
}

// pathToURI makes the file: URI of an absolute path.
func pathToURI(p string) (string, error) {
	if !filepath.IsAbs(p) {
		return "", pluginapi.Errf(pluginapi.CodeInvalid, "to_uri: path must be absolute")
	}
	s := filepath.ToSlash(filepath.Clean(p))
	if !strings.HasPrefix(s, "/") {
		s = "/" + s // C:/dir on Windows
	}
	return (&url.URL{Scheme: "file", Path: s}).String(), nil
}

// uriToPath is the inverse of pathToURI. Only file: URIs on the local host are accepted.
func uriToPath(uri string) (string, error) {
	u, err := url.Parse(uri)
	if err != nil {
		return "", pluginapi.Errf(pluginapi.CodeInvalid, "from_uri: %v", err)
	}
	if u.Scheme != "file" || (u.Host != "" && u.Host != "localhost") {
		return "", pluginapi.Errf(pluginapi.CodeInvalid, "from_uri: only file: URIs of this machine are supported")
	}
	p := u.Path
	if runtime.GOOS == "windows" && len(p) >= 3 && p[0] == '/' && p[2] == ':' {
		p = p[1:]
	}
	if p == "" || strings.ContainsRune(p, 0) {
		return "", pluginapi.Errf(pluginapi.CodeInvalid, "from_uri: no path")
	}
	return filepath.FromSlash(p), nil
}
