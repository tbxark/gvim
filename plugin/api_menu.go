package plugin

import (
	lua "github.com/yuin/gopher-lua"

	"github.com/tbxark/gvim/pluginapi"
)

// gvim.menu (plugin API tier 1a): rows a plugin adds to the generated menu bar.
//
//	gvim.menu.add_item{menu=, label=, action=, checkbox=, when=, after=, before=} -> true | nil, err
//	gvim.menu.remove_item{menu=, action=|label=} -> true | nil, err
//	gvim.menu.clear() -> true
//
// "menu" is a menu's stable id ("File", "View", ...) or its label. "action" is the action
// the row runs: normally your own command id ("<plugin>.<command>"), which the menu bar runs
// through the ordinary dispatch. "checkbox" names a context key whose state draws the row's
// checkmark (gvim.context.set); "when" gates it. "after"/"before" name a neighbouring row by
// its action or label to position the row; an unknown neighbour appends it. A label that
// starts with % is a translation key (plugin i18n).
//
// The set is whole-set replace per plugin: a reload or unload drops the rows of the old
// generation.

// installMenu installs gvim.menu.
func installMenu(e *apiEnv) {
	p := e.p
	t := e.area("menu")

	e.set(t, "add_item", func(L *lua.LState) int {
		opts, ok := L.Get(1).(*lua.LTable)
		if !ok {
			L.ArgError(1, "a table of menu item options expected")
		}
		o := bufTableOpts{t: opts}
		it := pluginapi.MenuItem{
			Menu:     clipText(optString(&o, "menu"), maxMenuMenu),
			Label:    clipText(p.translateString(optString(&o, "label")), maxMenuLabel),
			Action:   clipText(optString(&o, "action"), maxMenuAction),
			Checkbox: clipText(optString(&o, "checkbox"), maxMenuContext),
			When:     clipText(optString(&o, "when"), maxMenuContext),
			After:    clipText(optString(&o, "after"), maxMenuAction),
			Before:   clipText(optString(&o, "before"), maxMenuAction),
		}
		if o.err != "" {
			return bufRet(L, invalidErr("%s", o.err))
		}
		if it.Menu == "" || it.Label == "" || it.Action == "" {
			return bufRet(L, invalidErr("a menu item needs menu, label and action"))
		}
		if len(p.menuItems) >= QuotaMenuItems {
			return bufRet(L, pluginapi.Errf(pluginapi.CodeLimit, "menu item quota (%d)", QuotaMenuItems))
		}
		p.menuItems = append(p.menuItems, it)
		p.publishMenus()
		return bufTrue(L, nil)
	})

	e.set(t, "remove_item", func(L *lua.LState) int {
		opts, ok := L.Get(1).(*lua.LTable)
		if !ok {
			L.ArgError(1, "a table of menu item options expected")
		}
		o := bufTableOpts{t: opts}
		menu, action, label := optString(&o, "menu"), optString(&o, "action"), optString(&o, "label")
		if o.err != "" {
			return bufRet(L, invalidErr("%s", o.err))
		}
		kept := p.menuItems[:0:0]
		removed := 0
		for _, it := range p.menuItems {
			match := it.Menu == menu && (action != "" && it.Action == action || action == "" && label != "" && it.Label == label)
			if match {
				removed++
				continue
			}
			kept = append(kept, it)
		}
		if removed == 0 {
			return bufRet(L, pluginapi.Errf(pluginapi.CodeNotFound, "no menu item matches"))
		}
		p.menuItems = kept
		p.publishMenus()
		return bufTrue(L, nil)
	})

	e.set(t, "clear", func(L *lua.LState) int {
		if len(p.menuItems) > 0 {
			p.menuItems = nil
			p.publishMenus()
		}
		L.Push(lua.LTrue)
		return 1
	})
}

// Caps of one menu item.
const (
	maxMenuMenu    = 32
	maxMenuLabel   = 200
	maxMenuAction  = 128
	maxMenuContext = 64
)

// publishMenus replaces the plugin's rows in the host.
func (p *plugin) publishMenus() {
	if p.dead {
		return
	}
	p.r.host.SetMenuItems(p.name, append([]pluginapi.MenuItem(nil), p.menuItems...))
}
