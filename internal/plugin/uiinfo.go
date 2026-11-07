package plugin

// InitFile is the path of init.lua, the user script the loader runs as the plugin "init".
// It is "" when there is no configuration directory, or no loader. The plugin manager's
// init commands (Edit init.lua, Save Macro to init.lua) read it; it is not part of
// pluginapi.Runtime because only the ui's optional port asks for it.
func (r *Runtime) InitFile() string {
	if r.ld.opt == nil {
		return ""
	}
	return r.ld.opt.InitFile
}

// NoPlugins reports whether --no-plugins switched discovery off: only the --plugin paths
// load then. The plugin manager says so in its title.
func (r *Runtime) NoPlugins() bool {
	return r.ld.opt != nil && r.ld.opt.Disabled
}
