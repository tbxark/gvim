package sidebar

// Plugin sections (plugin API tier 1a). A plugin can mount a declarative widget panel as a
// section of the column; it sits under the native explorer and Contents sections and shares
// the column's rows through the same Layout. The kind has a reserved value far from the
// native kinds, so adding native kinds later never renumbers it.
//
// The root model owns the plugin sections: it appends their Specs in sidebarSpecs and paints
// their bodies, because the widget panel state lives in the plugin host. This package only
// gives the kind and the optional title and close flag the header needs.
const (
	// KindPlugin is a section whose body is a plugin widget panel.
	KindPlugin Kind = 100
)
