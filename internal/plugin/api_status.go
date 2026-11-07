package plugin

import (
	"math"
	"strconv"

	lua "github.com/yuin/gopher-lua"

	"github.com/tbxark/gvim/internal/pluginapi"
)

// gvim.ui.statusbar.item and the StatusItem handle (stage S14). The items of a plugin are
// published to the host as a whole set (Host.SetStatusItems), at most QuotaStatusItems of
// them. A click on an item runs its on_click function as a hidden command named by
// pluginapi.StatusClickAction, so the host needs no handle on the function.
//
// gvim.ui itself belongs to the prompts stage (api_ui.go); this file only adds the
// statusbar table to it, whichever installer creates the table first.

const (
	kindStatusItem = "StatusItem"

	statusClickPrefix = "__status_"
	maxStatusPriority = 1_000_000
)

// statusItem is one status item of a plugin.
type statusItem struct {
	id      string
	text    string
	right   bool
	prio    int
	click   bool // on_click is registered
	hidden  bool
	removed bool
}

// statusHandle is the payload of a StatusItem handle.
type statusHandle struct {
	k    *keyState
	item *statusItem
}

func (h *statusHandle) handleString() string { return h.item.id }

// publishStatus replaces the plugin's status items in the host. Hidden items are not sent.
func (k *keyState) publishStatus() {
	p := k.p
	if p.dead || p.L == nil {
		return
	}
	out := make([]pluginapi.StatusItem, 0, len(k.items))
	for _, it := range k.items {
		if it.hidden {
			continue
		}
		out = append(out, pluginapi.StatusItem{
			ID: it.id, Plugin: p.name, Text: it.text, Right: it.right, Priority: it.prio, Clickable: it.click,
		})
	}
	p.r.host.SetStatusItems(p.name, out)
}

// statusText makes text fit one status cell run: one line of at most maxStatusText cells.
func (p *plugin) statusText(text string) string {
	return p.r.svc.Truncate(oneLine(clipMsg(text, 4*maxStatusText)), maxStatusText, "…")
}

func installStatus(e *apiEnv) {
	p := e.p
	ui := e.area("ui")
	sb, _ := ui.RawGetString("statusbar").(*lua.LTable)
	if sb == nil {
		sb = e.L.NewTable()
		ui.RawSetString("statusbar", sb)
	}

	live := func(L *lua.LState) (*statusHandle, bool) {
		h := checkObject(L, 1, kindStatusItem).(*statusHandle)
		return h, !h.item.removed && !h.k.p.dead
	}
	closed := func(L *lua.LState, h *statusHandle) int {
		return bufRet(L, pluginapi.Errf(pluginapi.CodeClosed, "status item %q was removed", h.item.id))
	}
	methods := e.kind(kindStatusItem).methods
	e.set(methods, "set", func(L *lua.LState) int {
		h, ok := live(L)
		text := p.statusText(L.CheckString(2))
		if !ok {
			return closed(L, h)
		}
		if h.item.text != text {
			h.item.text = text
			h.k.publishStatus()
		}
		return bufTrue(L, nil)
	})
	for name, hide := range map[string]bool{"show": false, "hide": true} {
		e.set(methods, name, func(L *lua.LState) int {
			h, ok := live(L)
			if !ok {
				return closed(L, h)
			}
			if h.item.hidden != hide {
				h.item.hidden = hide
				h.k.publishStatus()
			}
			return bufTrue(L, nil)
		})
	}
	e.set(methods, "remove", func(L *lua.LState) int {
		h, ok := live(L)
		if !ok {
			return closed(L, h)
		}
		h.k.removeItem(h.item)
		return bufTrue(L, nil)
	})

	e.set(sb, "item", func(L *lua.LState) int {
		spec := L.CheckTable(1)
		field := func(name string, want lua.LValueType) lua.LValue {
			v := spec.RawGetString(name)
			if v != lua.LNil && v.Type() != want {
				L.RaiseError("gvim.ui.statusbar.item: field %q must be a %s, got %s", name, want, v.Type())
			}
			return v
		}
		k := p.keys()
		it := &statusItem{}
		if s, ok := field("id", lua.LTString).(lua.LString); ok {
			it.id = string(s)
			if !nameRE.MatchString(it.id) {
				return bufRet(L, pluginapi.Errf(pluginapi.CodeInvalid, "invalid status item id %q (want [a-z][a-z0-9_]*, at most 40 characters)", clipMsg(it.id, 48)))
			}
		} else {
			k.itemSeq++
			it.id = "item" + strconv.Itoa(k.itemSeq)
		}
		if s, ok := field("text", lua.LTString).(lua.LString); ok {
			it.text = p.statusText(string(s))
		}
		switch a, _ := field("align", lua.LTString).(lua.LString); a {
		case "", "left":
		case "right":
			it.right = true
		default:
			return bufRet(L, pluginapi.Errf(pluginapi.CodeInvalid, `align must be "left" or "right"`))
		}
		if n, ok := field("priority", lua.LTNumber).(lua.LNumber); ok {
			if math.IsNaN(float64(n)) {
				return bufRet(L, pluginapi.Errf(pluginapi.CodeInvalid, "priority must be a number"))
			}
			it.prio = int(math.Max(-maxStatusPriority, math.Min(maxStatusPriority, float64(n))))
		}
		click, _ := field("on_click", lua.LTFunction).(*lua.LFunction)
		for _, o := range k.items {
			if o.id == it.id {
				return bufRet(L, pluginapi.Errf(pluginapi.CodeConflict, "status item %q already exists", it.id))
			}
		}
		if len(k.items) >= QuotaStatusItems {
			return bufRet(L, pluginapi.Errf(pluginapi.CodeLimit, "status item quota (%d)", QuotaStatusItems))
		}
		if click != nil {
			err := p.addCommand(&command{
				id: pluginapi.StatusClickAction(p.name, it.id), name: statusClickPrefix + it.id, fn: click,
				info: pluginapi.CommandInfo{ID: pluginapi.StatusClickAction(p.name, it.id), Title: it.id, Plugin: p.name, Hidden: true},
			})
			if err != nil {
				return bufRet(L, err)
			}
			it.click = true
		}
		k.items = append(k.items, it)
		k.publishStatus()
		L.Push(p.newObject(kindStatusItem, &statusHandle{k: k, item: it}))
		return 1
	})
}

// removeItem drops a status item and its click command.
func (k *keyState) removeItem(it *statusItem) {
	for i, o := range k.items {
		if o == it {
			k.items = append(k.items[:i:i], k.items[i+1:]...)
			break
		}
	}
	it.removed = true
	if it.click {
		k.p.removeCommand(statusClickPrefix + it.id)
	}
	k.publishStatus()
}
