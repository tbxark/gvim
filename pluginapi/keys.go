package pluginapi

// StatusClickAction is the action id the ui runs when the user clicks the status item id of
// plugin. The runtime registers the item's on_click function as a hidden command of this
// name, so the ui needs no handle on the function: it runs the id with Runtime.Run.
func StatusClickAction(plugin, id string) string { return plugin + ".__status_" + id }

// ModeContext is the when-clause name that holds while the input mode name of plugin is the
// active editor mode ("mode:<plugin>:<name>"). Host.Holds answers it, which is how a plugin
// reads the current mode without keeping a copy that could go stale when the ui clears the
// mode on its own (the panic chord).
func ModeContext(plugin, name string) string { return "mode:" + plugin + ":" + name }
