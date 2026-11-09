package plugin

import (
	lua "github.com/yuin/gopher-lua"

	"github.com/tbxark/gvim/pluginapi"
)

// gvim.url: open a URL in the user's browser.
//
//	gvim.url.open(url) -> true | nil, err
//
// Only absolute http/https URLs are accepted (no other schemes, no credentials, no
// control characters/whitespace, at most pluginapi.MaxOpenURLLen bytes). The host asks
// the user to confirm in a keyboard-reachable Yes/No dialog; the opener itself runs
// asynchronously with argv-only exec (never a shell). An unsafe URL is refused before
// any prompt. Opening is never automatic.
func installURL(e *apiEnv) {
	area := e.area("url")
	host := func() pluginapi.Host { return e.p.r.guard }
	e.set(area, "open", func(L *lua.LState) int {
		raw := L.CheckString(1)
		if len(raw) > pluginapi.MaxOpenURLLen {
			return bufRet(L, invalidErr("url longer than %d bytes", pluginapi.MaxOpenURLLen))
		}
		opener, ok := host().(pluginapi.URLOpenHost)
		if !ok {
			return bufRet(L, pluginapi.Errf(pluginapi.CodeUnsupported, "opening URLs is not supported in this host"))
		}
		return bufTrue(L, opener.OpenURL(raw))
	})
}
