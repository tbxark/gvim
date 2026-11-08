package plugin

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	lua "github.com/yuin/gopher-lua"
)

// gvim.i18n (plugin API tier 1a): the plugin's own translations.
//
//	gvim.i18n.t(key, vars?) -> string     the current locale, then English, then the key
//	gvim.i18n.translate(key, vars?)       the same function
//	gvim.i18n.locale() -> string          the editor's locale ("en" today)
//
// A plugin ships a <plugin>.i18n.json next to its entry: a JSON object of locale -> key ->
// text, keyed the way Fresh's plugin i18n files are ("cmd.*", "status.*", "prompt.*"), with
// %{name} placeholders filled from vars. When the editor's locale has no entry, English is
// used, and when English has none either, the key itself is returned (so a missing
// translation reads as a key, not as an empty label). Command titles and descriptions that
// start with % are translated when the command is registered ("%cmd.open").
//
// The editor is English-only today; the locale is a runtime setting so an app-wide i18n can
// pass the user's choice without changing a plugin API.

// maxI18nValue bounds one translated text.
const maxI18nValue = 4096

// installI18n installs gvim.i18n.
func installI18n(e *apiEnv) {
	p := e.p
	t := e.area("i18n")
	translate := func(L *lua.LState) int {
		key := L.CheckString(1)
		var vars map[string]any
		if v := L.Get(2); v != lua.LNil {
			table, ok := v.(*lua.LTable)
			if !ok {
				L.ArgError(2, "a table of values expected, got "+v.Type().String())
			}
			x, err := p.fromLua(table)
			if err != nil {
				L.ArgError(2, err.Error())
			}
			vars, _ = x.(map[string]any)
		}
		L.Push(lua.LString(p.translate(key, vars)))
		return 1
	}
	e.set(t, "t", translate)
	// translate is the same function value as t: the two names are aliases.
	t.RawSetString("translate", t.RawGetString("t"))
	e.set(t, "locale", func(L *lua.LState) int {
		L.Push(lua.LString(p.locale()))
		return 1
	})
}

// locale returns the locale a plugin translates for.
func (p *plugin) locale() string {
	if p.r != nil && p.r.locale != "" {
		return p.r.locale
	}
	return "en"
}

// translate returns the text of key for the plugin's locale, falling back to English and then
// to the key itself, with the %{name} placeholders of vars filled.
func (p *plugin) translate(key string, vars map[string]any) string {
	text, ok := p.lookup(key)
	if !ok {
		return key
	}
	return interpolate(text, vars)
}

// lookup returns the translation of key: the current locale's first, then English.
func (p *plugin) lookup(key string) (string, bool) {
	if p.i18n == nil {
		return "", false
	}
	if table := p.i18n[p.locale()]; table != nil {
		if s, ok := table[key]; ok {
			return s, true
		}
	}
	if table := p.i18n["en"]; table != nil {
		if s, ok := table[key]; ok {
			return s, true
		}
	}
	return "", false
}

// translateString resolves a string that starts with "%" as a translation key: the command
// titles and descriptions and the menu labels of the plugin. Any other string is returned
// unchanged.
func (p *plugin) translateString(s string) string {
	if !strings.HasPrefix(s, "%") || len(s) < 2 {
		return s
	}
	return p.translate(s[1:], nil)
}

// interpolate fills the %{name} placeholders of text from vars. A placeholder with no value
// is left as it is, so a missing variable shows in the text instead of vanishing.
func interpolate(text string, vars map[string]any) string {
	if len(vars) == 0 || !strings.Contains(text, "%{") {
		return text
	}
	var b strings.Builder
	for {
		i := strings.Index(text, "%{")
		if i < 0 {
			b.WriteString(text)
			break
		}
		j := strings.IndexByte(text[i+2:], '}')
		if j < 0 {
			b.WriteString(text)
			break
		}
		b.WriteString(text[:i])
		name := text[i+2 : i+2+j]
		if v, ok := vars[name]; ok {
			b.WriteString(i18nValue(v))
		} else {
			b.WriteString(text[i : i+2+j+1])
		}
		text = text[i+2+j+1:]
	}
	return clipText(b.String(), maxI18nValue)
}

// i18nValue renders a placeholder value.
func i18nValue(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case []byte:
		return string(x)
	case bool:
		if x {
			return "true"
		}
		return "false"
	case int64:
		return strconv.FormatInt(x, 10)
	case int:
		return strconv.Itoa(x)
	case float64:
		return strconv.FormatFloat(x, 'g', -1, 64)
	default:
		return fmt.Sprint(x)
	}
}

// parseI18n reads a plugin's .i18n.json. A malformed file is a warning in the plugin log and
// leaves the plugin without translations: it must not stop the plugin from loading.
func parseI18n(r *Runtime, plugin, data string) map[string]map[string]string {
	var raw map[string]map[string]string
	if err := json.Unmarshal([]byte(data), &raw); err != nil {
		r.logf(plugin, levelWarn, "i18n: "+firstLine(err.Error()))
		return nil
	}
	out := make(map[string]map[string]string, len(raw))
	for locale, table := range raw {
		if locale == "" || len(table) == 0 {
			continue
		}
		clean := make(map[string]string, len(table))
		for key, text := range table {
			if key == "" {
				continue
			}
			clean[key] = clipText(text, maxI18nValue)
		}
		out[locale] = clean
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
