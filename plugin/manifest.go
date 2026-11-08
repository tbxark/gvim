package plugin

import (
	"encoding/json"
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/tailscale/hujson"
)

// MaxManifest is the largest plugin.json the loader reads.
const MaxManifest = 1 << 20

// manifest is plugin.json (JSONC). Every field is optional.
type manifest struct {
	Name        string
	Version     string
	Description string
	API         int
	Main        string
	// DefaultEnabled applies to bundled plugins only; nil means enabled.
	DefaultEnabled *bool
	// Requires are hard dependencies: a plugin is not loaded unless they loaded, and it
	// starts after their main chunks finished. After only orders.
	Requires []string
	After    []string
	// Unknown are the keys the loader does not know (logged at debug level, ignored).
	Unknown []string
}

// parseManifest reads plugin.json. A field of the wrong type or an invalid value is an
// error that names the field; unknown fields are collected in Unknown.
func parseManifest(data []byte) (manifest, error) {
	var m manifest
	std, err := hujson.Standardize(append([]byte(nil), data...))
	if err != nil {
		return m, fmt.Errorf("plugin.json: %v", err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(std, &raw); err != nil {
		return m, fmt.Errorf("plugin.json: want a JSON object (%v)", err)
	}
	keys := make([]string, 0, len(raw))
	for k := range raw {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		v := raw[k]
		var err error
		switch k {
		case "name":
			err = json.Unmarshal(v, &m.Name)
		case "version":
			err = json.Unmarshal(v, &m.Version)
		case "description":
			err = json.Unmarshal(v, &m.Description)
		case "api":
			var f float64
			if err = json.Unmarshal(v, &f); err == nil {
				if f < 1 || f != float64(int(f)) {
					return m, fmt.Errorf(`plugin.json: "api" must be a positive integer`)
				}
				m.API = int(f)
			}
		case "main":
			err = json.Unmarshal(v, &m.Main)
		case "default_enabled":
			var b bool
			if err = json.Unmarshal(v, &b); err == nil {
				m.DefaultEnabled = &b
			}
		case "requires":
			err = json.Unmarshal(v, &m.Requires)
		case "after":
			err = json.Unmarshal(v, &m.After)
		default:
			m.Unknown = append(m.Unknown, k)
		}
		if err != nil {
			return m, fmt.Errorf("plugin.json: %q has the wrong type (%v)", k, err)
		}
	}
	if m.Name != "" && !nameRE.MatchString(m.Name) {
		return m, fmt.Errorf("plugin.json: invalid name %q (want [a-z][a-z0-9_]*, at most 40 characters)", m.Name)
	}
	if m.Main != "" {
		main, err := cleanMain(m.Main)
		if err != nil {
			return m, err
		}
		m.Main = main
	}
	if m.API > APIVersion {
		return m, fmt.Errorf("plugin.json: needs plugin API %d, this gvim provides %d", m.API, APIVersion)
	}
	if m.Requires, err = nameList("requires", m.Requires, true); err != nil {
		return m, err
	}
	m.After, _ = nameList("after", m.After, false)
	return m, nil
}

// cleanMain checks that main stays inside the plugin directory and names a .lua file.
func cleanMain(main string) (string, error) {
	c := path.Clean(strings.ReplaceAll(main, `\`, "/"))
	if path.IsAbs(c) || c == ".." || strings.HasPrefix(c, "../") || !strings.HasSuffix(c, ".lua") {
		return "", fmt.Errorf(`plugin.json: "main" must be a .lua file inside the plugin directory, got %q`, main)
	}
	return c, nil
}

// nameList validates a list of plugin names and removes duplicates. A bad name is an
// error when strict and dropped otherwise.
func nameList(field string, in []string, strict bool) ([]string, error) {
	var out []string
	seen := map[string]bool{}
	for _, n := range in {
		if !nameRE.MatchString(n) {
			if strict {
				return nil, fmt.Errorf("plugin.json: %q lists the invalid plugin name %q", field, n)
			}
			continue
		}
		if !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	return out, nil
}
