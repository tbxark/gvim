package plugin

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/tbxark/gvim/pluginapi"
)

func warnN(format string, a ...any) []pluginapi.Notice {
	return []pluginapi.Notice{{Text: fmt.Sprintf(format, a...), Warn: true}}
}

func infoN(format string, a ...any) []pluginapi.Notice {
	return []pluginapi.Notice{{Text: fmt.Sprintf(format, a...)}}
}

// doControl runs a lifecycle request at depth 0. The sources it needs are
// read, and compiled, in Work; everything that changes plugins happens when that Work's
// completion is delivered, so a failing reload never touches the running version.
func (r *Runtime) doControl(c pluginapi.Control) ([]pluginapi.Work, []pluginapi.Notice) {
	l := &r.ld
	switch c.Op {
	case "reload":
		return r.ctlReload(c.Name)
	case "reload_all":
		if n := r.loaderUnavailable(); n != nil {
			return nil, n
		}
		return []pluginapi.Work{r.discover(&l.state, func(d *discovery) {
			r.applyDiscovery(d, false, func(b *batch) {
				if why := d.unread[SourceUser]; why != "" {
					// The warning of the scan must outlast this notice: say it here too.
					r.say(pluginapi.LevelWarn, fmt.Sprintf("%d plugin(s) loaded; %s (the user plugins keep running)", len(b.okNames()), why))
					return
				}
				if len(b.errs) > 0 {
					// The last notice is what stays on the status row: it must not read as a
					// success when a reload was aborted or a plugin failed. The first problem
					// comes first, because the row is cut at its width.
					more := ""
					if n := len(b.errs) - 1; n > 0 {
						more = fmt.Sprintf(" (+%d more)", n)
					}
					r.say(pluginapi.LevelWarn, fmt.Sprintf("%s%s; %d plugin(s) loaded (Plugin: Show Log)", firstLine(b.errs[0]), more, r.loadedCount()))
					return
				}
				r.say(pluginapi.LevelInfo, fmt.Sprintf("%d plugin(s) loaded", len(b.okNames())))
			})
		})}, nil
	case "enable":
		return r.ctlEnable(c.Name)
	case "disable":
		return nil, r.ctlDisable(c.Name)
	case "load_text":
		return r.ctlLoadText(c.Name, c.Text)
	case "init_check":
		return r.ctlInit(false, c.Text)
	case "init_reload":
		return r.ctlInit(true, "")
	}
	return nil, warnN("plugin control %q is unknown", c.Op)
}

// loaderUnavailable explains why discovery based operations cannot run.
func (r *Runtime) loaderUnavailable() []pluginapi.Notice {
	switch l := &r.ld; {
	case l.opt == nil:
		return warnN("no plugin loader is configured")
	case l.opt.Disabled:
		return warnN("plugins are disabled by --no-plugins")
	}
	return nil
}

// rescan reads one plugin again in Work and hands the compiled candidate to then. The
// result is dropped when the plugin was enabled or disabled meanwhile: the enablement the
// Work computed is out of date then.
func (r *Runtime) rescan(ref pluginRef, then func(*candidate)) pluginapi.Work {
	opts := *r.ld.opt
	st := r.ld.state.clone()
	osys := r.os
	cfgDir := osys.Dirs().Config
	seq := r.ld.toggles[ref.name]
	return r.newWork(func(ctx context.Context) (any, string) {
		return opts.scanOne(ctx, osys, cfgDir, ref, opts.Enabled, &st), ""
	}, func(v any, errText string) {
		if r.ld.toggles[ref.name] != seq {
			r.logf(ref.name, levelDebug, "dropped a reload that was overtaken by enable or disable")
			return
		}
		nc, _ := v.(*candidate)
		if nc == nil {
			r.say(pluginapi.LevelWarn, "reading plugin '"+ref.name+"' failed: "+errText)
			return
		}
		then(nc)
	})
}

// ---------- reload ----------

func (r *Runtime) ctlReload(name string) ([]pluginapi.Work, []pluginapi.Notice) {
	l := &r.ld
	c := l.byName[name]
	switch p := r.plugins[name]; {
	case c == nil && p == nil:
		return nil, warnN("plugin '%s' is not loaded", name)
	case c == nil && p.kindStr == SourceBuffer:
		return nil, warnN("plugin '%s' was loaded from a buffer: load it from the buffer again", name)
	case c == nil || l.opt == nil:
		return nil, warnN("plugin '%s' cannot be reloaded: it was not loaded from a file", name)
	case !c.enabled:
		return nil, warnN("plugin '%s' is disabled", name)
	}
	return []pluginapi.Work{r.rescan(c.ref, func(nc *candidate) { r.reloadScanned(name, nc, "reloaded") })}, nil
}

// reloadScanned replaces the running plugin name by the freshly read candidate nc.
// Nothing is touched unless the new source reads and compiles in every file: a syntax error anywhere keeps the running version and says which file.
func (r *Runtime) reloadScanned(name string, nc *candidate, verb string) {
	switch {
	case nc.name != name:
		r.abortReload(name, fmt.Sprintf("the plugin is now called %q; restart gvim", nc.name))
	case nc.err != "":
		r.abortReload(name, nc.err)
	case len(nc.problems) > 0:
		r.abortReload(name, nc.problems[0])
	case !nc.enabled:
		r.abortReload(name, "the plugin is disabled")
	default:
		why := r.replace(nc, func(b *batch) {
			if it := b.by[name]; it != nil && it.st == itemOK {
				text := fmt.Sprintf("Plugin '%s' %s", name, verb)
				if n := len(b.items) - 1; n > 0 {
					text += fmt.Sprintf(" (with %d dependant(s))", n)
				}
				r.say(pluginapi.LevelInfo, text)
			}
		})
		if why != "" {
			r.abortReload(name, why)
		}
	}
}

func (r *Runtime) abortReload(name, why string) {
	r.logf(name, levelError, "reload aborted: "+why)
	r.say(pluginapi.LevelWarn, fmt.Sprintf("plugin '%s': reload aborted: %s (the running version is kept)", name, statusText(why)))
}

// replace stops the dependants and the old version of nc.name, then loads nc and starts the
// dependants again, in dependency order. why is non-empty, and nothing has changed, when nc
// cannot load because of a dependency.
func (r *Runtime) replace(nc *candidate, done func(*batch)) (why string) {
	l := &r.ld
	dependants := r.loadedDependants(nc.name)
	set := []*candidate{nc}
	for _, n := range dependants {
		set = append(set, l.byName[n])
	}
	set = append(set, r.stoppedDependants(nc.name)...)
	if _, blocked := planOrder(set, r.depLookup); blocked[nc] != "" {
		return blocked[nc]
	}
	// Dependants stop before what they depend on, the last started first.
	names := slices.Clone(dependants)
	slices.Reverse(names)
	r.unloadNamed(append(names, nc.name), unloadOptions{})
	if nc.kind != SourceBuffer {
		l.install(nc)
	}
	b := r.newBatch(false, done)
	r.prepare(b, set)
	b.advance()
	return ""
}

// ---------- enable and disable ----------

// toggleable checks that the manager may change name; it returns the notice that explains
// a refusal.
func (r *Runtime) toggleable(c *candidate) []pluginapi.Notice {
	switch {
	case c.locked != "":
		return warnN("plugin '%s' is %s (plugins.%s.enabled); change it in config.json", c.name, c.locked, c.name)
	case c.kind != SourceBundled && c.kind != SourceUser:
		return warnN("plugin '%s' was given with %s and cannot be disabled", c.name, map[string]string{SourceCLI: "--plugin", SourceInit: "init.lua"}[c.kind])
	}
	return nil
}

func (r *Runtime) ctlEnable(name string) ([]pluginapi.Work, []pluginapi.Notice) {
	if n := r.loaderUnavailable(); n != nil {
		return nil, n
	}
	l := &r.ld
	c := l.byName[name]
	if c == nil {
		return nil, warnN("plugin '%s' is not installed", name)
	}
	if n := r.toggleable(c); n != nil {
		return nil, n
	}
	if p := r.plugins[name]; c.enabled && p != nil && !p.dead {
		return nil, infoN("plugin '%s' is already enabled", name)
	}
	l.toggled(name)
	l.state.set(name, true)
	r.saveState()
	return []pluginapi.Work{r.rescan(c.ref, func(nc *candidate) {
		if nc.err != "" {
			l.install(nc)
			nc.reason = nc.err
			r.say(pluginapi.LevelError, fmt.Sprintf("plugin '%s': %s", name, statusText(nc.err)))
			return
		}
		why := r.replace(nc, func(b *batch) {
			if it := b.by[name]; it != nil && it.st == itemOK {
				r.say(pluginapi.LevelInfo, fmt.Sprintf("Plugin '%s' enabled", name))
			}
		})
		if why != "" {
			l.install(nc)
			nc.reason = why
			r.say(pluginapi.LevelError, fmt.Sprintf("plugin '%s': %s", name, why))
		}
	})}, nil
}

func (r *Runtime) ctlDisable(name string) []pluginapi.Notice {
	l := &r.ld
	c, p := l.byName[name], r.plugins[name]
	switch {
	case c == nil && p == nil:
		return warnN("plugin '%s' is not loaded", name)
	case c == nil && p.kindStr == SourceBuffer:
		// Loaded from a buffer: there is nothing to keep, unload it.
		deps := r.loadedDependants(name)
		slices.Reverse(deps)
		r.unloadNamed(append(deps, name), unloadOptions{})
		return infoN("plugin '%s' unloaded", name)
	case c == nil:
		// Loaded without the loader: stop it and keep a disabled row.
		r.unload(p, unloadOptions{final: statusDisabled})
		return infoN("plugin '%s' disabled", name)
	}
	if n := r.toggleable(c); n != nil {
		return n
	}
	if !c.enabled {
		if !l.state.enabled[name] {
			return infoN("plugin '%s' is already disabled", name)
		}
		// An enable is still being read (or failed to read): cancel it.
		l.toggled(name)
		l.state.set(name, false)
		r.saveState()
		return infoN("plugin '%s' disabled", name)
	}
	l.toggled(name)
	deps := r.loadedDependants(name)
	why := fmt.Sprintf("requires %q (disabled)", name)
	for i := len(deps) - 1; i >= 0; i-- {
		if p := r.plugins[deps[i]]; p != nil && !p.dead {
			r.unload(p, unloadOptions{final: statusFailed, err: why})
		}
		if dc := l.byName[deps[i]]; dc != nil {
			dc.reason = why
		}
	}
	c.enabled = false
	l.state.set(name, false)
	r.saveState()
	if p != nil && !p.dead {
		r.unload(p, unloadOptions{final: statusDisabled})
	}
	if len(deps) > 0 {
		return infoN("plugin '%s' disabled (also stopped: %s)", name, strings.Join(deps, ", "))
	}
	return infoN("plugin '%s' disabled", name)
}

// ---------- load from buffer ----------

// sanitizeName turns a buffer's file stem into a plugin name.
func sanitizeName(stem string) string {
	var b strings.Builder
	for _, ch := range strings.ToLower(stem) {
		switch {
		case ch >= 'a' && ch <= 'z', ch >= '0' && ch <= '9', ch == '_':
			b.WriteRune(ch)
		default:
			b.WriteByte('_')
		}
	}
	name := strings.TrimLeft(b.String(), "0123456789_")
	if len(name) > 40 {
		name = name[:40]
	}
	if name == "" {
		return "buffer"
	}
	return name
}

func (r *Runtime) ctlLoadText(stem, text string) ([]pluginapi.Work, []pluginapi.Notice) {
	if len(text) > MaxLuaFile {
		return nil, warnN("%s: source too large (%d bytes, max %d)", pluginapi.CodeLimit, len(text), MaxLuaFile)
	}
	name := sanitizeName(stem)
	l := &r.ld
	p := r.plugins[name]
	switch c := l.byName[name]; {
	case name == "init":
		return nil, warnN(`"init" is reserved for init.lua: use Plugin: Reload init.lua`)
	case p != nil && !p.dead && p.kindStr != SourceBuffer:
		return nil, warnN("plugin '%s' is already loaded from %s: not replaced by the buffer", name, p.kindStr)
	case c != nil && c.kind != SourceBuffer:
		return nil, warnN("a %s plugin named '%s' is installed: not replaced by the buffer", c.kind, name)
	}
	verb := "loaded from buffer"
	if p != nil && !p.dead {
		verb = "reloaded from buffer"
	}
	return []pluginapi.Work{r.newWork(func(ctx context.Context) (any, string) {
		return compileText(ctx, name, text), ""
	}, func(v any, errText string) {
		nc, _ := v.(*candidate)
		if nc == nil {
			r.say(pluginapi.LevelWarn, "plugin '"+name+"': "+errText)
			return
		}
		if nc.err != "" {
			r.say(pluginapi.LevelWarn, fmt.Sprintf("plugin '%s': %s", name, statusText(nc.err)))
			return
		}
		r.reloadScanned(name, nc, verb)
	})}, nil
}

// ---------- init.lua ----------

// ctlInit is init_check (compile init.lua and report the first error) and init_reload
// (reload the plugin "init"). init_check with text checks that text, the unsaved buffer of
// init.lua, instead of the file on disk.
func (r *Runtime) ctlInit(reload bool, text string) ([]pluginapi.Work, []pluginapi.Notice) {
	if n := r.loaderUnavailable(); n != nil {
		return nil, n
	}
	opts := r.ld.opt
	if opts.InitFile == "" {
		return nil, warnN("there is no init.lua: no configuration directory")
	}
	ref := opts.initRef()
	where := filepath.Join(ref.dir, ref.file)
	if !reload && text != "" {
		return []pluginapi.Work{r.newWork(func(ctx context.Context) (any, string) {
			return compileText(ctx, "init", text), ""
		}, func(v any, errText string) {
			nc, _ := v.(*candidate)
			switch {
			case nc == nil:
				r.say(pluginapi.LevelWarn, "init.lua (unsaved buffer): could not be checked: "+errText)
			case nc.err != "":
				r.say(pluginapi.LevelWarn, "init.lua (unsaved buffer): "+statusText(nc.err))
			default:
				r.say(pluginapi.LevelInfo, "init.lua (unsaved buffer): syntax OK")
			}
		})}, nil
	}
	return []pluginapi.Work{r.rescan(ref, func(nc *candidate) {
		switch {
		case nc.err != "" && reload:
			r.abortReload("init", nc.err)
		case nc.err != "":
			r.say(pluginapi.LevelWarn, "init.lua: "+statusText(nc.err))
		case !reload:
			r.say(pluginapi.LevelInfo, "init.lua: syntax OK ("+where+")")
		default:
			r.reloadScanned("init", nc, "reloaded")
		}
	})}, nil
}

// loadedCount is the number of plugins that are loaded now.
func (r *Runtime) loadedCount() int {
	n := 0
	for _, p := range r.plugins {
		if p.status == statusLoaded && !p.dead {
			n++
		}
	}
	return n
}
