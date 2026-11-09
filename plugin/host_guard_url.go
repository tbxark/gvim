package plugin

import "github.com/tbxark/gvim/pluginapi"

// OpenURL forwards to the optional URL-opening host part, so gvim.url.open works through
// the same guarded host wrapper as every other API call. A host without it reports
// unsupported rather than silently ignoring the request.
func (g hostGuard) OpenURL(raw string) error {
	if u, ok := g.Host.(pluginapi.URLOpenHost); ok {
		return u.OpenURL(raw)
	}
	return pluginapi.Errf(pluginapi.CodeUnsupported, "opening URLs is not supported in this host")
}
