package plugin

// Per-workspace project plugin configuration. One daemon runs one plugin
// runtime, but a workspace can enable/disable plugins and set plugin settings in its own
// project layer of config.json. On a switch the daemon compares the entering workspace's
// plugin configuration with the runtime's current one and calls
// ApplyWorkspacePluginConfig with the new layer; only the plugins whose effective enabled
// state or settings changed are reloaded/unloaded/loaded, and the work reads the sources in
// Work (off the update loop). The editor-wide plugin-state toggles are never written for a
// project layer: the workspace config is a view, not a user toggle.

import (
	"fmt"
	"maps"
	"reflect"
	"sort"

	"github.com/tbxark/gvim/pluginapi"
)

// ApplyWorkspacePluginConfig replaces the runtime's plugins.<name>.enabled and
// plugins.<name>.settings config layer with one workspace's project config and schedules
// the affected plugins' reloads as Work. It must run on the UI goroutine (the runtime's
// single goroutine). The returned Work is already queued on the runtime; the caller only
// has to let the plugin pump run.
func (r *Runtime) ApplyWorkspacePluginConfig(enabled map[string]bool, settings map[string]PluginSettings) []pluginapi.Work {
	l := &r.ld
	if l.opt == nil || l.opt.Disabled {
		return nil
	}
	newEnabled := maps.Clone(enabled)
	if newEnabled == nil {
		newEnabled = map[string]bool{}
	}
	newSettings := maps.Clone(settings)
	if newSettings == nil {
		newSettings = map[string]PluginSettings{}
	}
	oldSettings := l.opt.Settings
	if oldSettings == nil {
		oldSettings = map[string]PluginSettings{}
	}
	l.opt.Enabled = newEnabled
	l.opt.Settings = newSettings

	// Every known candidate can be affected by setting or removing a config entry, so
	// decide all of them; only the ones whose answer or settings changed get work.
	names := make(map[string]bool, len(l.byName)+len(newEnabled)+len(newSettings))
	for name := range l.byName {
		names[name] = true
	}
	for name := range newEnabled {
		names[name] = true
	}
	for name := range newSettings {
		names[name] = true
	}
	order := make([]string, 0, len(names))
	for name := range names {
		order = append(order, name)
	}
	sort.Strings(order)

	var work []pluginapi.Work
	var enabledN, disabledN, reloadedN int
	for _, name := range order {
		c := l.byName[name]
		if c == nil || (c.kind != SourceBundled && c.kind != SourceUser) {
			continue
		}
		oldEnabled := c.enabled
		oldSt, newSt := oldSettings[name], newSettings[name]
		stChanged := !reflect.DeepEqual(oldSt, newSt)
		c.decide(newEnabled, &l.state)
		if stChanged {
			l.updateSettings(name, newSt)
		}
		running := r.plugins[name] != nil && !r.plugins[name].dead
		switch {
		case c.enabled && !oldEnabled:
			enabledN++
			if running {
				continue
			}
			n := name
			work = append(work, r.rescan(c.ref, func(nc *candidate) {
				if nc.err != "" {
					l.install(nc)
					nc.reason = nc.err
					r.say(pluginapi.LevelError, fmt.Sprintf("plugin '%s' (workspace config): %s", n, statusText(nc.err)))
					return
				}
				if why := r.replace(nc, func(b *batch) {
					if it := b.by[n]; it != nil && it.st == itemOK {
						r.say(pluginapi.LevelInfo, fmt.Sprintf("Plugin '%s' enabled by the workspace config", n))
					}
				}); why != "" {
					l.install(nc)
					nc.reason = why
					r.say(pluginapi.LevelError, fmt.Sprintf("plugin '%s' (workspace config): %s", n, statusText(why)))
				}
			}))
		case !c.enabled && oldEnabled:
			disabledN++
			// Disable like ctlDisable, minus the editor-wide toggle write: the project
			// layer is not a user choice.
			why := fmt.Sprintf("requires %q (disabled by the workspace config)", name)
			deps := r.loadedDependants(name)
			for i := len(deps) - 1; i >= 0; i-- {
				if p := r.plugins[deps[i]]; p != nil && !p.dead {
					r.unload(p, unloadOptions{final: statusFailed, err: why})
				}
				if dc := l.byName[deps[i]]; dc != nil {
					dc.reason = why
				}
			}
			if p := r.plugins[name]; p != nil && !p.dead {
				r.unload(p, unloadOptions{final: statusDisabled})
			}
			r.say(pluginapi.LevelInfo, fmt.Sprintf("Plugin '%s' disabled by the workspace config", name))
		case c.enabled && running && stChanged:
			reloadedN++
			n := name
			work = append(work, r.rescan(c.ref, func(nc *candidate) {
				r.reloadScanned(n, nc, "reloaded with the workspace config")
			}))
		}
	}
	if enabledN+disabledN+reloadedN > 0 {
		r.say(pluginapi.LevelInfo, fmt.Sprintf("workspace plugin config: %d enabled, %d disabled, %d reloaded", enabledN, disabledN, reloadedN))
	}
	return work
}
