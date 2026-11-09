package plugintest

// OpenURL records gvim.url.open calls. The fake host does not enforce the real TUI
// host's scheme policy or confirmation; tests for those live in internal/tui, while this
// method lets plugin API tests prove the Lua surface reaches the host. An error set in
// Ext["urlopen_err"] is returned.
func (f *FakeHost) OpenURL(raw string) error {
	f.rec("OpenURL", raw)
	urls, _ := f.Ext["urls"].([]string)
	f.Ext["urls"] = append(urls, raw)
	if err, ok := f.Ext["urlopen_err"].(error); ok {
		return err
	}
	return nil
}

// URLs returns the URLs gvim.url.open passed to the host.
func (f *FakeHost) URLs() []string {
	urls, _ := f.Ext["urls"].([]string)
	return append([]string(nil), urls...)
}
