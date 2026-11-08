package i18n

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
)

//go:embed fresh_map.json
var freshMapJSON []byte

var (
	freshMapOnce sync.Once
	freshMap     map[string]string
)

// FreshMap returns gvim-key -> Fresh-key mappings as a copy: the explicit import
// table for the strings whose English wording/meaning matches Fresh's. The keys of
// the map are a subset of the English catalogue; the rest of the catalogue is
// gvim-only (English source text, no Fresh translation) and is absent from the
// translated locale files by design.
func FreshMap() map[string]string {
	freshMapOnce.Do(func() {
		if err := json.Unmarshal(freshMapJSON, &freshMap); err != nil {
			panic("i18n: embedded fresh_map.json: " + err.Error())
		}
	})
	out := make(map[string]string, len(freshMap))
	for k, v := range freshMap {
		out[k] = v
	}
	return out
}

// pluralCategories are the CLDR cardinal categories a catalogue key may name as a
// suffix. The allowed set for one locale is derived from PluralCategory, so
// validation and runtime selection cannot drift.
var pluralCategories = []string{"zero", "one", "two", "few", "many", "other"}

// allowedCategories is the set of plural categories PluralCategory can return for
// locale.
func allowedCategories(locale string) map[string]bool {
	allowed := map[string]bool{}
	// The runtime rule is an integer rule; 0..200 covers every residue class the
	// shipped languages distinguish.
	for n := 0; n <= 200; n++ {
		allowed[PluralCategory(locale, n)] = true
	}
	allowed["other"] = true
	return allowed
}

// pluralSuffix reports whether key ends in "." + a CLDR plural category, and which.
func pluralSuffix(key string) (base, category string, ok bool) {
	i := strings.LastIndexByte(key, '.')
	if i <= 0 {
		return "", "", false
	}
	last := key[i+1:]
	for _, cat := range pluralCategories {
		if last == cat {
			return key[:i], cat, true
		}
	}
	return "", "", false
}

// placeholders returns the %{name} names of text and an error for a malformed
// placeholder. The names allow letters, digits and underscores, which is the shape
// the catalogues (and the plugin runtime) use.
func placeholders(text string) ([]string, error) {
	var out []string
	for i := 0; i < len(text); {
		start := strings.Index(text[i:], "%{")
		if start < 0 {
			break
		}
		start += i
		end := strings.IndexByte(text[start+2:], '}')
		if end < 0 {
			return nil, fmt.Errorf("unterminated placeholder at %q", text[start:])
		}
		end += start + 2
		name := text[start+2 : end]
		if name == "" {
			return nil, fmt.Errorf("empty placeholder %q", text[start:end+1])
		}
		for _, r := range name {
			if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '_' {
				return nil, fmt.Errorf("placeholder name %q: %q is not allowed", name, r)
			}
		}
		out = append(out, name)
		i = end + 1
	}
	return out, nil
}

// sortedPlaceholders returns the names in a stable order for comparison.
func sortedPlaceholders(text string) ([]string, error) {
	names, err := placeholders(text)
	if err != nil {
		return nil, err
	}
	sort.Strings(names)
	return names, nil
}

// ValidateCatalogue validates one locale table against the English catalogue and the
// locale's CLDR plural rules. It returns each problem separately (unknown keys,
// malformed or mismatched %{name} placeholders, plural keys whose category the
// locale does not use or whose base English lacks, empty or oversized values, and an
// unknown locale code). It is the CI-ready check behind the catalogue tests; a
// translated file ships only when it returns no errors.
func ValidateCatalogue(locale string, table map[string]string) []error {
	ensureEnglish()
	en := catalogue(DefaultLocale)
	if _, ok := Find(locale); !ok && locale != DefaultLocale {
		return []error{fmt.Errorf("locale %q is not one of the shipped locales", locale)}
	}
	allowed := allowedCategories(locale)
	var errs []error
	keys := make([]string, 0, len(table))
	for key := range table {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		value := table[key]
		if key == "" {
			errs = append(errs, fmt.Errorf("%s: empty key", locale))
			continue
		}
		if value == "" {
			errs = append(errs, fmt.Errorf("%s: %s: empty value", locale, key))
			continue
		}
		if len(value) > maxValue {
			errs = append(errs, fmt.Errorf("%s: %s: value is %d bytes, over the %d limit", locale, key, len(value), maxValue))
			continue
		}
		english, known := en[key]
		if !known {
			errs = append(errs, fmt.Errorf("%s: %s: key is not in the English catalogue", locale, key))
			continue
		}
		if base, category, ok := pluralSuffix(key); ok {
			if !allowed[category] {
				errs = append(errs, fmt.Errorf("%s: %s: plural category %q is not used by this locale", locale, key, category))
			}
			if _, hasBase := en[base]; !hasBase && !hasPluralBase(en, base) {
				errs = append(errs, fmt.Errorf("%s: %s: plural key has no English base %q", locale, key, base))
			}
		}
		want, err := sortedPlaceholders(english)
		if err != nil {
			errs = append(errs, fmt.Errorf("en: %s: %v", key, err))
			continue
		}
		got, err := sortedPlaceholders(value)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %s: %v", locale, key, err))
			continue
		}
		if strings.Join(got, "|") != strings.Join(want, "|") {
			errs = append(errs, fmt.Errorf("%s: %s: placeholders %v do not match English %v", locale, key, got, want))
		}
	}
	return errs
}

// hasPluralBase reports whether the English catalogue has any plural form of base.
func hasPluralBase(en map[string]string, base string) bool {
	for key := range en {
		if strings.HasPrefix(key, base+".") {
			if _, _, ok := pluralSuffix(key); ok {
				return true
			}
		}
	}
	return false
}

// ValidateCatalogueJSON decodes one catalogue file, rejecting duplicate keys (which
// json.Unmarshal would silently collapse), then validates the table.
func ValidateCatalogueJSON(locale string, data []byte) []error {
	table := map[string]string{}
	dec := json.NewDecoder(bytes.NewReader(data))
	tok, err := dec.Token()
	if err != nil {
		return []error{fmt.Errorf("%s: %v", locale, err)}
	}
	if delim, ok := tok.(json.Delim); !ok || delim != '{' {
		return []error{fmt.Errorf("%s: catalogue must be a JSON object", locale)}
	}
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return []error{fmt.Errorf("%s: %v", locale, err)}
		}
		key, ok := keyTok.(string)
		if !ok {
			return []error{fmt.Errorf("%s: a non-string key", locale)}
		}
		if _, dup := table[key]; dup {
			return []error{fmt.Errorf("%s: duplicate key %q", locale, key)}
		}
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return []error{fmt.Errorf("%s: %s: %v", locale, key, err)}
		}
		var value string
		if err := json.Unmarshal(raw, &value); err != nil {
			return []error{fmt.Errorf("%s: %s: %v (the value must be a string)", locale, key, err)}
		}
		table[key] = value
	}
	if tok, err = dec.Token(); err != nil {
		return []error{fmt.Errorf("%s: %v", locale, err)}
	}
	if delim, ok := tok.(json.Delim); !ok || delim != '}' {
		return []error{fmt.Errorf("%s: catalogue object does not close", locale)}
	}
	return ValidateCatalogue(locale, table)
}

// Coverage is one locale's translation coverage of the English catalogue.
type Coverage struct {
	Locale     string
	Translated int      // English keys the locale file translates
	Total      int      // English keys in total (the extracted inventory)
	Missing    []string // the English keys the locale file lacks, sorted
}

// CoverageFor reports how much of the English catalogue locale translates. The
// untranslated keys fall back to English at runtime; the gvim-only keys are expected
// to be missing from translated files.
func CoverageFor(locale string) Coverage {
	ensureEnglish()
	en := catalogue(DefaultLocale)
	table := catalogue(locale)
	out := Coverage{Locale: locale, Total: len(en)}
	for key := range en {
		if _, ok := table[key]; ok {
			out.Translated++
		} else {
			out.Missing = append(out.Missing, key)
		}
	}
	sort.Strings(out.Missing)
	return out
}
