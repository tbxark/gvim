package plugin

// gvim.dashboard: third-party dashboard sections (the gvim counterpart of Fresh's
// dashboard registerSection API).
//
//	gvim.dashboard.register_section(name, refresh, opts?) -> id |//	gvim.dashboard.set_auto_open(enabled) -> true
//	gvim.dashboard.remove_section(id_or_name) -> boolean
//	gvim.dashboard.sections() -> { {id, plugin, name, title, key, ttl_ms, timeout_ms, priority}, ... }
//	gvim.dashboard.set_auto_open(enabled) -> true
//	gvim.dashboard.auto_open() -> boolean | nil  (nil: dashboard plugin setting applies)
//
// The refresh callback is retained in the registering plugin's state. The dashboard
// plugin discovers registered sections through sections(), then calls the exported proxy
// function under the generated key and receives bounded plain-data rows; no callback
// crosses plugin states. The host only validates registration, ownership and caps; the
// dashboard plugin applies the per-section timeout, rejects oversized/invalid rows, keeps
// the last good rows on failure and paints one section at a time.
//
// opts: {title = string, ttl_ms = 30000, timeout_ms = 8000, priority = 0}
// Rows are {kind = "kv"|"text"|"blank", label=?, value=?, text=?, color=?}; the dashboard
// plugin owns rendering/clipping and validates them.

import (
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	lua "github.com/yuin/gopher-lua"
)

const (
	maxDashboardSections      = 16
	maxDashboardResultNodes   = 4096
	maxDashboardResultDepth   = 8
	maxDashboardResultString  = 4 << 10
	maxDashboardSectionsTotal = 64
	maxDashboardName          = 64
	maxDashboardTitle         = 120
	defaultDashboardTTL       = 30 * time.Second
	minDashboardTTL           = time.Second
	maxDashboardTTL           = 10 * time.Minute
	defaultDashboardTimeout   = 8 * time.Second
	minDashboardTimeout       = 100 * time.Millisecond
	maxDashboardTimeout       = 30 * time.Second
)

// dashboardSection is one registered section. The function stays in the plugin's Lua
// state; the generated key is what the dashboard plugin asks the cross-plugin proxy for.
type dashboardSection struct {
	id       int
	name     string
	title    string
	key      string
	ttl      time.Duration
	timeout  time.Duration
	priority int
	fn       *lua.LFunction
}

func installDashboard(e *apiEnv) {
	p, r := e.p, e.r
	area := e.area("dashboard")

	e.set(area, "register_section", func(L *lua.LState) int {
		name := L.CheckString(1)
		fn := L.CheckFunction(2)
		if name == "" || utf8.RuneCountInString(name) > maxDashboardName || strings.ContainsRune(name, 0) {
			return bufRet(L, invalidErr("dashboard section name must be 1..%d characters and contain no NUL", maxDashboardName))
		}
		o := optionsArg(argsFrom(L, 1), 2)
		title := clipText(o.str("title"), maxDashboardTitle)
		ttlMS := o.number("ttl_ms", float64(defaultDashboardTTL.Milliseconds()), float64(minDashboardTTL.Milliseconds()), float64(maxDashboardTTL.Milliseconds()))
		timeoutMS := o.number("timeout_ms", float64(defaultDashboardTimeout.Milliseconds()), float64(minDashboardTimeout.Milliseconds()), float64(maxDashboardTimeout.Milliseconds()))
		priority := o.number("priority", 0, -100, 100)
		if o.bad != "" {
			L.ArgError(3, o.bad)
			return 0
		}
		if title == "" {
			title = name
		}
		if p.dashboard == nil {
			p.dashboard = map[string]*dashboardSection{}
			p.addCleanup(func() { p.dashboard = nil })
		}
		if _, exists := p.dashboard[name]; exists {
			return bufRet(L, invalidErr("dashboard section %q is already registered by this plugin", name))
		}
		if len(p.dashboard) >= maxDashboardSections {
			return bufRet(L, limitErr("too many dashboard sections for plugin %q (max %d)", p.name, maxDashboardSections))
		}
		if r.dashboardSectionCount() >= maxDashboardSectionsTotal {
			return bufRet(L, limitErr("too many dashboard sections in total (max %d)", maxDashboardSectionsTotal))
		}
		p.dashboardSeq++
		id := p.dashboardSeq
		s := &dashboardSection{
			id: id, name: name, title: title,
			key:      "__gvim_dashboard_" + p.name + "_" + strconv.Itoa(id),
			ttl:      time.Duration(ttlMS) * time.Millisecond,
			timeout:  time.Duration(timeoutMS) * time.Millisecond,
			priority: int(math.Round(priority)),
			fn:       fn,
		}
		p.dashboard[name] = s
		L.Push(lua.LNumber(id))
		return 1
	})

	e.set(area, "remove_section", func(L *lua.LState) int {
		if p.dashboard == nil {
			L.Push(lua.LFalse)
			return 1
		}
		var name string
		switch v := L.Get(1).(type) {
		case lua.LString:
			name = string(v)
		case lua.LNumber:
			id := int(v)
			for key, s := range p.dashboard {
				if s.id == id {
					name = key
					break
				}
			}
		}
		if name == "" {
			L.Push(lua.LFalse)
			return 1
		}
		if _, ok := p.dashboard[name]; !ok {
			L.Push(lua.LFalse)
			return 1
		}
		delete(p.dashboard, name)
		L.Push(lua.LTrue)
		return 1
	})

	e.set(area, "set_auto_open", func(L *lua.LState) int {
		r.setDashboardAutoOpen(L.CheckBool(1))
		L.Push(lua.LTrue)
		return 1
	})

	e.set(area, "auto_open", func(L *lua.LState) int {
		if enabled, ok := r.dashboardAutoOpenOverride(); ok {
			L.Push(lua.LBool(enabled))
			return 1
		}
		L.Push(lua.LNil)
		return 1
	})

	e.set(area, "sections", func(L *lua.LState) int {
		rows := r.dashboardSections()
		out := L.CreateTable(len(rows), 0)
		consumer := isDashboardConsumer(p)
		for _, row := range rows {
			t := L.CreateTable(0, 8)
			t.RawSetString("id", lua.LNumber(row.id))
			t.RawSetString("plugin", lua.LString(row.plugin))
			t.RawSetString("name", lua.LString(row.name))
			t.RawSetString("title", lua.LString(row.title))
			key := row.key
			if !consumer {
				key = ""
			}
			t.RawSetString("key", lua.LString(key))
			t.RawSetString("ttl_ms", lua.LNumber(row.ttl.Milliseconds()))
			t.RawSetString("timeout_ms", lua.LNumber(row.timeout.Milliseconds()))
			t.RawSetString("priority", lua.LNumber(row.priority))
			out.Append(t)
		}
		L.Push(out)
		return 1
	})
}

// validateDashboardResult bounds the plain data returned by a registered section callback
// before it is copied into the dashboard state: the dashboard would validate rows anyway,
// but the host must cap nodes/depth/strings first so a provider cannot make the caller's
// state materialize an unbounded result. The row schema itself is dashboard-owned.
func validateDashboardResult(v any) error {
	nodes := 0
	var walk func(any, int) error
	walk = func(x any, depth int) error {
		if depth > maxDashboardResultDepth {
			return limitErr("dashboard section result is nested deeper than %d", maxDashboardResultDepth)
		}
		if nodes++; nodes > maxDashboardResultNodes {
			return limitErr("dashboard section result has more than %d values", maxDashboardResultNodes)
		}
		switch val := x.(type) {
		case nil, bool, float64, int, int64:
			return nil
		case string:
			if len(val) > maxDashboardResultString {
				return limitErr("dashboard section result string is longer than %d bytes", maxDashboardResultString)
			}
			return nil
		case []any:
			for _, item := range val {
				if err := walk(item, depth+1); err != nil {
					return err
				}
			}
			return nil
		case map[string]any:
			for _, item := range val {
				if err := walk(item, depth+1); err != nil {
					return err
				}
			}
			return nil
		default:
			return invalidErr("dashboard section result contains an unsupported value")
		}
	}
	return walk(v, 0)
}

// dashboardConsumer is the one authorized caller of a section's generated proxy key: the
// bundled dashboard plugin. A user plugin that happens to be named dashboard is not the
// bundled one and must not receive the key. The key is also hidden from sections() callers
// other than the dashboard.
func isDashboardConsumer(p *plugin) bool {
	return p != nil && p.name == "dashboard" && p.kindStr == SourceBundled
}

// dashboard auto-open override values stored in Runtime.dashboardAutoOpen.
const (
	dashboardAutoUnset int8 = iota
	dashboardAutoOff
	dashboardAutoOn
)

// setDashboardAutoOpen stores the session-scoped -- not config-persisted -- auto-open
// override. The bundled dashboard plugin reads it via gvim.dashboard.auto_open(); the
// user setting show_on_empty is the fallback when nothing overrides it.
func (r *Runtime) setDashboardAutoOpen(enabled bool) {
	if enabled {
		r.dashboardAutoOpen = dashboardAutoOn
		return
	}
	r.dashboardAutoOpen = dashboardAutoOff
}

func (r *Runtime) dashboardAutoOpenOverride() (bool, bool) {
	switch r.dashboardAutoOpen {
	case dashboardAutoOn:
		return true, true
	case dashboardAutoOff:
		return false, true
	}
	return false, false
}

// dashboardSectionRow is the plain-data metadata list returned to Lua.
type dashboardSectionRow struct {
	id       int
	plugin   string
	name     string
	title    string
	key      string
	ttl      time.Duration
	timeout  time.Duration
	priority int
}

func (r *Runtime) dashboardSectionCount() int {
	n := 0
	for _, p := range r.order {
		n += len(p.dashboard)
	}
	return n
}

// dashboardSections lists live registrations in priority/name order. The total is already
// bounded by registration; the result is plain data only (the callback is never exposed).
func (r *Runtime) dashboardSections() []dashboardSectionRow {
	var out []dashboardSectionRow
	for _, p := range r.order {
		if p == nil || p.dead || p.L == nil || len(p.dashboard) == 0 {
			continue
		}
		names := make([]string, 0, len(p.dashboard))
		for name := range p.dashboard {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			s := p.dashboard[name]
			out = append(out, dashboardSectionRow{
				id: s.id, plugin: p.name, name: s.name, title: s.title, key: s.key,
				ttl: s.ttl, timeout: s.timeout, priority: s.priority,
			})
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].priority != out[j].priority {
			return out[i].priority > out[j].priority
		}
		if out[i].plugin != out[j].plugin {
			return out[i].plugin < out[j].plugin
		}
		return out[i].name < out[j].name
	})
	if len(out) > maxDashboardSectionsTotal {
		out = out[:maxDashboardSectionsTotal]
	}
	return out
}

// dashboardFunction returns the retained callback for a generated section key, or nil.
func (p *plugin) dashboardFunction(key string) *lua.LFunction {
	for _, s := range p.dashboard {
		if s.key == key {
			return s.fn
		}
	}
	return nil
}

// exportedFunction resolves a cross-plugin proxy function from the ordinary exports or a
// registered dashboard section callback.
func (p *plugin) exportedFunction(key string) *lua.LFunction {
	if fn, ok := p.data.exports[key].(*lua.LFunction); ok {
		return fn
	}
	return p.dashboardFunction(key)
}
