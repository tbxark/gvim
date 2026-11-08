// Package i18n is gvim's translation layer: locale resolution, embedded catalogues,
// %{name} interpolation and CLDR plural forms. It is a TUI-free leaf package: only the
// standard library, so both the engine packages and the UI can translate through it.
//
// The catalogues are embedded from i18n/locales/*.json and keyed like Fresh's
// (refs/fresh/.../locales/en.json): "clipboard.copied", "terminal.no_terminal_open".
// Where gvim has a string of its own that Fresh does not name, the key lives under
// "gvim." in the same file. i18n/locales/en.json is the source of truth; the other
// locale files are generated from Fresh's translations by tools/i18n_import.py and
// contain only the keys whose English wording matches Fresh (the rest fall back to
// English at runtime). Register installs or replaces a locale table for tests and
// callers; embedded files are loaded lazily on first use.
//
// Lookup order is the active locale, each shorter tag of it (zh-Hant-CN -> zh-Hant -> zh),
// then English, then the key itself. A missing translation therefore reads as a key rather
// than as an empty label, which is easy to spot and to fix.
package i18n

import (
	"embed"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
)

// DefaultLocale is the fallback locale and the one catalogue embedded in the binary.
const DefaultLocale = "en"

// Language is one language the editor offers.
type Language struct {
	Code    string // the catalogue code, such as "pt-BR" or "zh-CN"
	English string // the English name, such as "Portuguese (Brazil)"
	Native  string // the language's own name, such as "Português (Brasil)"
}

// locales are the languages Fresh ships and gvim's Settings page offers. The names are
// Fresh's (fresh-editor/src/i18n.rs locale_display_name).
var locales = []Language{
	{"bg", "Bulgarian", "Български"},
	{"cs", "Czech", "Čeština"},
	{"de", "German", "Deutsch"},
	{"en", "English", "English"},
	{"es", "Spanish", "Español"},
	{"fr", "French", "Français"},
	{"it", "Italian", "Italiano"},
	{"ja", "Japanese", "日本語"},
	{"ko", "Korean", "한국어"},
	{"pt-BR", "Portuguese (Brazil)", "Português (Brasil)"},
	{"ru", "Russian", "Русский"},
	{"th", "Thai", "ไทย"},
	{"uk", "Ukrainian", "Українська"},
	{"vi", "Vietnamese", "Tiếng Việt"},
	{"zh-CN", "Chinese (Simplified)", "简体中文"},
}

// Locales returns the languages gvim offers, English first in Fresh's order.
func Locales() []Language { return append([]Language(nil), locales...) }

// Find returns the language for code (case-insensitive, "-" only) and whether it is known.
func Find(code string) (Language, bool) {
	for _, l := range locales {
		if strings.EqualFold(l.Code, code) {
			return l, true
		}
	}
	return Language{}, false
}

// Normalize turns an environment or config locale into a catalogue code: it strips an
// encoding or modifier suffix (pt_BR.UTF-8 -> pt-BR, de_DE@euro -> de), maps "_" to "-",
// matches a known locale, and otherwise falls back to the primary language subtag
// (en_US -> en). "" is returned for the C and POSIX locales and for an empty tag.
func Normalize(tag string) string {
	t := strings.TrimSpace(tag)
	if i := strings.IndexAny(t, ".@"); i >= 0 {
		t = t[:i]
	}
	t = strings.ReplaceAll(t, "_", "-")
	if t == "" {
		return ""
	}
	lower := strings.ToLower(t)
	if lower == "c" || lower == "posix" {
		return ""
	}
	for _, l := range locales {
		if strings.ToLower(l.Code) == lower {
			return l.Code
		}
	}
	lang := strings.ToLower(strings.SplitN(t, "-", 2)[0])
	if lang == "" || lang == "c" || lang == "posix" {
		return ""
	}
	for _, l := range locales {
		if strings.ToLower(strings.SplitN(l.Code, "-", 2)[0]) == lang {
			return l.Code
		}
	}
	return lang
}

// Resolve is the locale the editor starts in. A config locale other than "" or "auto" wins
// (an unknown one is kept so lookups fall back to English); otherwise LC_ALL, LC_MESSAGES
// and LANG are tried in that order; the last fallback is English. getenv is os.Getenv in
// the app, and a map lookup in tests.
func Resolve(configLocale string, getenv func(string) string) string {
	if c := strings.TrimSpace(configLocale); c != "" && !strings.EqualFold(c, "auto") {
		if n := Normalize(c); n != "" {
			return n
		}
		return DefaultLocale
	}
	if getenv != nil {
		for _, name := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
			if n := Normalize(getenv(name)); n != "" {
				return n
			}
		}
	}
	return DefaultLocale
}

var (
	currentMu sync.RWMutex
	current   = DefaultLocale
)

// SetLocale sets the active locale. Unknown codes are accepted: they resolve through the
// fallback chain. The app calls it at startup and when the Settings row is saved.
func SetLocale(code string) {
	currentMu.Lock()
	defer currentMu.Unlock()
	if n := Normalize(code); n != "" {
		current = n
		return
	}
	current = DefaultLocale
}

// Locale is the active locale.
func Locale() string {
	currentMu.RLock()
	defer currentMu.RUnlock()
	return current
}

var (
	catMu      sync.RWMutex
	catalogues = map[string]map[string]string{}
	loadOnce   sync.Once
)

//go:embed locales/*.json
var localeFiles embed.FS

// Register installs (or replaces) the catalogue of a locale. It is the seam for the
// translated Fresh JSON files and for tests; the English catalogue is embedded and
// registered lazily. Tables are copied, so later changes by the caller do not show.
func Register(locale string, table map[string]string) {
	code := Normalize(locale)
	if code == "" {
		code = locale
	}
	copyTable := make(map[string]string, len(table))
	for k, v := range table {
		if k != "" {
			copyTable[k] = v
		}
	}
	ensureEnglish()
	catMu.Lock()
	defer catMu.Unlock()
	catalogues[code] = copyTable
}

// ensureEnglish loads the embedded English catalogue once. It is the source of truth
// the other locales are validated against.
func ensureEnglish() {
	loadOnce.Do(func() {
		table := decodeEmbedded(DefaultLocale)
		catMu.Lock()
		catalogues[DefaultLocale] = table
		catMu.Unlock()
	})
}

// decodeEmbedded loads and decodes one embedded locale file. A malformed embedded
// file is a build-time bug, so it panics on first use; the catalogue tests catch it
// before a release.
func decodeEmbedded(code string) map[string]string {
	data, err := localeFiles.ReadFile("locales/" + code + ".json")
	if err != nil {
		panic("i18n: embedded locale " + code + ": " + err.Error())
	}
	var table map[string]string
	if err := json.Unmarshal(data, &table); err != nil {
		panic("i18n: embedded locale " + code + ": " + err.Error())
	}
	return table
}

// T translates key in locale, falling back to English and then to the key itself, and
// fills the %{name} placeholders from vars. An empty locale means the active one.
func T(locale, key string, vars map[string]any) string {
	text, ok := lookup(locale, key)
	if !ok {
		return key
	}
	return interpolate(text, vars)
}

// TOr translates key like T, but returns fallback when the catalogue has no entry for
// it (in the locale or in English) instead of returning the key. Display code that has
// an English source text uses it, so a catalogue gap can never show a bare key.
func TOr(locale, key, fallback string, vars map[string]any) string {
	if key == "" {
		return fallback
	}
	text, ok := lookup(locale, key)
	if !ok {
		return fallback
	}
	return interpolate(text, vars)
}

// TN translates the count-dependent message key for n. The catalogue holds one key per
// CLDR plural category (key + ".one", key + ".other", ...); the form is picked by the
// locale's own rule, and a missing form falls back to ".other" and then to English's. The
// %{count} placeholder is filled with n.
func TN(locale, key string, n int, vars map[string]any) string {
	count := strconv.Itoa(n)
	if vars == nil {
		vars = map[string]any{}
	} else {
		copied := make(map[string]any, len(vars)+1)
		for k, v := range vars {
			copied[k] = v
		}
		vars = copied
	}
	vars["count"] = count
	for _, loc := range chain(locale) {
		table := catalogue(loc)
		if table == nil {
			continue
		}
		form := key + "." + PluralCategory(loc, n)
		if text, ok := table[form]; ok {
			return interpolate(text, vars)
		}
		if text, ok := table[key+".other"]; ok {
			return interpolate(text, vars)
		}
	}
	return key
}

// PluralCategory is the CLDR cardinal category of the integer n in locale: English's
// one/other rule, Fresh's other language rules (ru/uk one/few/many, cs one/few, fr/pt one
// for 0 and 1, no grammatical number for ja/ko/zh/th/vi).
func PluralCategory(locale string, n int) string {
	lang := strings.ToLower(strings.SplitN(strings.ReplaceAll(locale, "_", "-"), "-", 2)[0])
	switch lang {
	case "ja", "ko", "zh", "th", "vi":
		return "other"
	case "fr", "pt":
		if n >= 0 && n <= 1 {
			return "one"
		}
		return "other"
	case "cs", "sk":
		switch {
		case n == 1:
			return "one"
		case n >= 2 && n <= 4:
			return "few"
		default:
			return "other"
		}
	case "ru", "uk", "be":
		m10, m100 := n%10, n%100
		switch {
		case m10 == 1 && m100 != 11:
			return "one"
		case m10 >= 2 && m10 <= 4 && (m100 < 12 || m100 > 14):
			return "few"
		default:
			return "many"
		}
	default:
		if n == 1 {
			return "one"
		}
		return "other"
	}
}

// chain is the locales a lookup consults: the locale itself, each shorter tag of it, then
// English. An empty locale means the active one.
func chain(locale string) []string {
	if locale == "" {
		locale = Locale()
	}
	var out []string
	cur := strings.ReplaceAll(locale, "_", "-")
	for cur != "" {
		out = append(out, cur)
		i := strings.LastIndex(cur, "-")
		if i < 0 {
			break
		}
		cur = strings.TrimSuffix(cur[:i], "-x")
	}
	out = append(out, DefaultLocale)
	return out
}

// catalogue returns the table of one locale code, or nil. An embedded locale file is
// loaded lazily the first time its code is consulted, so a session that never uses a
// translated locale pays only for English. Register tables win over the embedded file.
func catalogue(code string) map[string]string {
	ensureEnglish()
	if code == "" {
		return nil
	}
	catMu.RLock()
	table := catalogues[code]
	catMu.RUnlock()
	if table != nil {
		return table
	}
	if !hasEmbeddedLocale(code) {
		return nil
	}
	table = decodeEmbedded(code)
	catMu.Lock()
	defer catMu.Unlock()
	if existing := catalogues[code]; existing != nil {
		return existing // Register raced with the lazy load.
	}
	catalogues[code] = table
	return table
}

// hasEmbeddedLocale reports whether i18n/locales has a file for code.
func hasEmbeddedLocale(code string) bool {
	_, err := localeFiles.ReadFile("locales/" + code + ".json")
	return err == nil
}

// lookup resolves key through the fallback chain.
func lookup(locale, key string) (string, bool) {
	for _, loc := range chain(locale) {
		if table := catalogue(loc); table != nil {
			if text, ok := table[key]; ok {
				return text, true
			}
		}
	}
	return "", false
}

// maxValue bounds one translated text, as the plugin runtime's does.
const maxValue = 4096

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
			b.WriteString(valueText(v))
		} else {
			b.WriteString(text[i : i+2+j+1])
		}
		text = text[i+2+j+1:]
	}
	out := b.String()
	if len([]rune(out)) > maxValue {
		return string([]rune(out)[:maxValue])
	}
	return out
}

// valueText renders a placeholder value.
func valueText(v any) string {
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
	case int:
		return strconv.Itoa(x)
	case int64:
		return strconv.FormatInt(x, 10)
	case float64:
		return strconv.FormatFloat(x, 'g', -1, 64)
	default:
		return fmt.Sprint(x)
	}
}
