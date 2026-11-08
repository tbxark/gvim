package plugintest

// Host features (the optional Feature(name) bool seam that plugin gvim.has reports).
// A feature not set here uses the fake's default: after the tier-1c paint surface landed
// "decor_virtual_paint" defaults to true, every other name to false. SetFeature lets a test
// drive both modes, e.g. SetFeature("decor_virtual_paint", false) for the reduced path.

// SetFeature overrides a host feature for this fake host.
func (f *FakeHost) SetFeature(name string, on bool) {
	m, _ := f.Ext["features"].(map[string]bool)
	if m == nil {
		m = map[string]bool{}
		f.Ext["features"] = m
	}
	m[name] = on
}

// Feature reports a host feature: the override when set, else the default.
func (f *FakeHost) Feature(name string) bool {
	if m, _ := f.Ext["features"].(map[string]bool); m != nil {
		if v, ok := m[name]; ok {
			return v
		}
	}
	return name == "decor_virtual_paint"
}
