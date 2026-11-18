package plugin

// OpenURL forwards to the wrapped Host, so gvim.url.open works through
// the same guarded host wrapper as every other API call.
func (g hostGuard) OpenURL(raw string) error {
	return g.Host.OpenURL(raw)
}
