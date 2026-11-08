package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/tbxark/gvim/config"
	"github.com/tbxark/gvim/internal/tui"
	"github.com/tbxark/gvim/lsp/manager"
)

// The Settings LSP page. Fresh keeps language servers in a per-language map of
// server arrays and edits them through its generic array-item dialogs. gvim's
// config.PatchBytes cannot address an element of an array, so the page shows each
// effective server (built-in table + user entries + universal_lsp) with its live
// manager state, and edits a language's whole server array as validated JSON (or
// runs the start/stop/restart actions). The project layer may only mute or enable
// servers: config.ValidateEdits applies the same setProjectLSP rules a config load
// does, so a project write that names a program is refused with its diagnostic.

// lspServerView is one effective server of a language: the config entry, where it
// comes from, and the running manager's state when it is configured there.
type lspServerView struct {
	Server   config.LSPServer
	Source   string // "builtin", "user" or "universal"
	Status   tui.LSPServerStatus
	HasState bool
}

// lspDisplayName is the name a server row shows: its configured name, else the
// program's base name.
func lspDisplayName(srv config.LSPServer) string {
	if srv.Name != "" {
		return srv.Name
	}
	if srv.Command != "" {
		return filepath.Base(srv.Command)
	}
	return "(no program)"
}

// lspCommandLine is the command as the row shows it: the program and its
// arguments, joined by spaces.
func lspCommandLine(srv config.LSPServer) string {
	if len(srv.Args) == 0 {
		return srv.Command
	}
	return srv.Command + " " + strings.Join(srv.Args, " ")
}

// lspStatuses groups the manager's server states per language; a universal
// server's Language is empty and is grouped under "".
func (s *settingsService) lspStatuses() map[string][]tui.LSPServerStatus {
	out := make(map[string][]tui.LSPServerStatus)
	if s.rt.lsp == nil {
		return out
	}
	for _, st := range s.rt.lsp.Servers() {
		out[st.Language] = append(out[st.Language], st)
	}
	return out
}

// matchLSPStatus finds the manager's row for one configured server: the name it
// reports, or the program its command starts.
func matchLSPStatus(statuses []tui.LSPServerStatus, srv config.LSPServer) (tui.LSPServerStatus, bool) {
	want := lspDisplayName(srv)
	for _, st := range statuses {
		if st.Name == want || (srv.Command != "" && st.Command != "" && strings.Contains(st.Command, filepath.Base(srv.Command))) {
			return st, true
		}
	}
	return tui.LSPServerStatus{}, false
}

// builtinServer converts the built-in table's entry for a language to the config
// shape the page shows.
func builtinServer(language string) (config.LSPServer, bool) {
	spec, ok := manager.DefaultFor(language)
	if !ok {
		return config.LSPServer{}, false
	}
	return config.LSPServer{Command: spec.Command, Args: slices.Clone(spec.Args), RootMarkers: slices.Clone(spec.RootMarkers)}, true
}

// effectiveLSPServers is the servers of one language as the page lists them: the
// user's entries when the language has any (the runtime plan lets them replace the
// built-in entry), else the built-in table's server.
func (s *settingsService) effectiveLSPServers(cfg config.Config, language string) []lspServerView {
	statuses := s.lspStatuses()[language]
	var out []lspServerView
	if list, ok := cfg.LSP[language]; ok && len(list) > 0 {
		for _, srv := range list {
			view := lspServerView{Server: srv, Source: "user"}
			view.Status, view.HasState = matchLSPStatus(statuses, srv)
			out = append(out, view)
		}
		return out
	}
	if srv, ok := builtinServer(language); ok {
		view := lspServerView{Server: srv, Source: "builtin"}
		view.Status, view.HasState = matchLSPStatus(statuses, srv)
		out = append(out, view)
	}
	return out
}

// lspStateText is a server's live state as the row's note: "running", "stopped",
// "starting", or the failure.
func lspStateText(st tui.LSPServerStatus) string {
	switch st.State {
	case tui.LSPServerRunning:
		return "running"
	case tui.LSPServerStarting:
		return "starting"
	case tui.LSPServerFailed:
		if st.Error != "" {
			return "failed: " + st.Error
		}
		return "failed"
	default:
		if st.AutoStart {
			return "stopped"
		}
		return "stopped (manual start)"
	}
}

// structureLSP is the "lsp" page: one row per language that has a server (the
// built-in table, the user's lsp entries, or a manager status), plus the universal
// servers. The project layer lists only what the layer's own file configures.
func (s *settingsService) structureLSP(cfg config.Config, object map[string]json.RawMessage, project bool) tui.SettingsStructure {
	st := tui.SettingsStructure{Key: "lsp", Title: "Language Servers", Kind: "map", AddLabel: "Add language server…"}
	st.Note = "Enter a language's page for its servers, live state and whole-array JSON; the project layer may only mute or enable servers."
	languages := make(map[string]bool)
	layer := map[string]json.RawMessage(nil)
	if !project {
		for _, spec := range manager.Defaults() {
			languages[spec.Language] = true
		}
		for lang := range cfg.LSP {
			languages[lang] = true
		}
	} else {
		layer, _ = layerObjectAt(object, []string{"lsp"})
		for lang := range layer {
			languages[lang] = true
		}
	}
	for _, lang := range sortedStrings(languages) {
		servers := s.effectiveLSPServers(cfg, lang)
		names := make([]string, 0, len(servers))
		states := make([]string, 0, len(servers))
		for _, view := range servers {
			names = append(names, lspDisplayName(view.Server))
			if view.HasState {
				states = append(states, lspStateText(view.Status))
			}
		}
		value := strings.Join(names, ", ")
		if project {
			value = "configured"
		}
		note := strings.Join(states, ", ")
		if note == "" {
			note = "not started"
		}
		if project && len(layer[lang]) > 0 {
			note = "project entry"
		}
		st.Rows = append(st.Rows, tui.SettingsStructureRow{
			Path: []string{"lsp", lang}, Name: lang, Value: value, Note: note,
			Child: "lsp." + lang,
		})
	}
	// Universal servers run for every language; their own page edits the array.
	universal := cfg.UniversalLSP
	if project {
		universal, _ = decodeUniversalAt(layerObjectRaw(object, "universal_lsp"))
	}
	universalStatuses := s.lspStatuses()[""]
	for _, name := range sortedStrings(universal) {
		servers := universal[name]
		names := make([]string, 0, len(servers))
		states := make([]string, 0, len(servers))
		for _, srv := range servers {
			names = append(names, lspDisplayName(srv))
			if st, ok := matchLSPStatus(universalStatuses, srv); ok {
				states = append(states, lspStateText(st))
			}
		}
		note := "all languages"
		if len(states) > 0 {
			note += " · " + strings.Join(states, ", ")
		}
		st.Rows = append(st.Rows, tui.SettingsStructureRow{
			Path: []string{"universal_lsp", name}, Name: "universal: " + name,
			Value: strings.Join(names, ", "), Note: note,
			Child: "lsp.universal." + name,
		})
	}
	return st
}

// structureLSPLanguage is one language's page: a read-only row per effective
// server with its live state, the start/stop/restart actions, and the editable
// whole-array JSON of the layer's lsp entry (the project layer's is locked: only
// mute/enable changes validate there).
func (s *settingsService) structureLSPLanguage(cfg config.Config, object map[string]json.RawMessage, language string, project bool) tui.SettingsStructure {
	st := tui.SettingsStructure{
		Key: "lsp." + language, Title: "Language Servers · " + language, Kind: "fields",
	}
	st.Note = "process_limits is accepted and shown, not applied (gvim does not limit servers)."
	servers := s.effectiveLSPServers(cfg, language)
	for _, view := range servers {
		note := view.Source
		if view.HasState {
			note += " · " + lspStateText(view.Status)
		}
		st.Rows = append(st.Rows, tui.SettingsStructureRow{
			Name: lspDisplayName(view.Server), Value: lspCommandLine(view.Server),
			Note: note, Locked: true, Path: []string{"lsp", language},
		})
	}
	// The actions run the managers' manual start/stop/restart, whatever the layer.
	if s.rt.lsp != nil {
		st.Rows = append(st.Rows,
			tui.SettingsStructureRow{Name: "Start " + language, Action: "act:lsp-start", Value: language, Note: "runs the configured servers by hand"},
			tui.SettingsStructureRow{Name: "Restart " + language, Action: "act:lsp-restart", Value: language, Note: "stops the named server and starts it again"},
			tui.SettingsStructureRow{Name: "Stop " + language, Action: "act:lsp-stop", Value: language, Note: "stops the language's servers"},
		)
	}
	raw := json.RawMessage("[]")
	if r, ok := layerRawAt(object, []string{"lsp", language}); ok {
		raw = compactJSON(r)
	} else if list := cfg.LSP[language]; len(list) > 0 && !project {
		raw = serversJSON(list)
	}
	row := tui.SettingsStructureRow{
		Path: []string{"lsp", language}, Name: "servers (JSON)", Kind: "json",
		Value: string(raw), Note: "whole array; Enter to edit",
	}
	if project {
		row.Locked = true
		row.Note = "the project layer may only mute or enable servers"
	}
	st.Rows = append(st.Rows, row)
	return st
}

// structureLSPUniversal is one universal server's page: the same read-only view
// and whole-array JSON as a language page, under universal_lsp.
func (s *settingsService) structureLSPUniversal(cfg config.Config, object map[string]json.RawMessage, name string, project bool) tui.SettingsStructure {
	st := tui.SettingsStructure{
		Key: "lsp.universal." + name, Title: "Universal Server · " + name, Kind: "fields",
	}
	st.Note = "A universal server runs for every language. process_limits is accepted and shown, not applied."
	all := cfg.UniversalLSP[name]
	if project {
		us, _ := decodeUniversalAt(layerObjectRaw(object, "universal_lsp"))
		all = us[name]
	}
	statuses := s.lspStatuses()[""]
	for _, srv := range all {
		note := "universal"
		if st, ok := matchLSPStatus(statuses, srv); ok {
			note += " · " + lspStateText(st)
		}
		st.Rows = append(st.Rows, tui.SettingsStructureRow{
			Name: lspDisplayName(srv), Value: lspCommandLine(srv),
			Note: note, Locked: true, Path: []string{"universal_lsp", name},
		})
	}
	raw := json.RawMessage("[]")
	if r, ok := layerRawAt(object, []string{"universal_lsp", name}); ok {
		raw = compactJSON(r)
	} else if len(all) > 0 && !project {
		raw = serversJSON(all)
	}
	row := tui.SettingsStructureRow{
		Path: []string{"universal_lsp", name}, Name: "servers (JSON)", Kind: "json",
		Value: string(raw), Note: "whole array; Enter to edit",
	}
	if project {
		row.Locked = true
		row.Note = "the project layer may only mute or enable servers"
	}
	st.Rows = append(st.Rows, row)
	return st
}

// serversJSON marshals config servers as the compact array the JSON row edits.
func serversJSON(list []config.LSPServer) json.RawMessage {
	raw, err := json.Marshal(list)
	if err != nil {
		return json.RawMessage("[]")
	}
	return raw
}

// compactJSON returns raw without insignificant whitespace.
func compactJSON(raw json.RawMessage) json.RawMessage {
	var buf bytes.Buffer
	if err := json.Compact(&buf, raw); err != nil {
		return raw
	}
	return json.RawMessage(buf.String())
}

// decodeUniversalAt decodes a layer's universal_lsp object.
func decodeUniversalAt(raw json.RawMessage) (map[string][]config.LSPServer, bool) {
	var out map[string][]config.LSPServer
	if len(raw) == 0 || json.Unmarshal(raw, &out) != nil {
		return nil, false
	}
	return out, true
}

// layerObjectRaw returns the raw JSON object at a top-level key of a layer, if it
// is an object.
func layerObjectRaw(object map[string]json.RawMessage, key string) json.RawMessage {
	return object[key]
}

// mapsKeys returns the keys of a set as a slice.
func mapsKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// sortedStrings returns the keys of a set in sorted order.
func sortedStrings[V any](m map[string]V) []string {
	out := mapsKeys(m)
	slices.Sort(out)
	return out
}

// lspStart/Stop/Restart run one LSP action for the page. The page sends the
// language, and optionally a server name, as "language\x00name".
func (s *settingsService) lspAct(action, value string) (string, error) {
	if s.rt.lsp == nil {
		return "", fmt.Errorf("no language-server port is wired")
	}
	language, name, _ := strings.Cut(value, "\x00")
	switch action {
	case "lsp-start":
		if err := s.rt.lsp.StartLanguage(language); err != nil {
			return "", err
		}
		return "LSP " + language + " started", nil
	case "lsp-restart":
		if err := s.rt.lsp.RestartServer(language, name); err != nil {
			return "", err
		}
		if language == "" {
			return "Universal server restarted", nil
		}
		return "LSP " + language + " restarted", nil
	case "lsp-stop":
		if err := s.rt.lsp.StopServer(language, name); err != nil {
			return "", err
		}
		if language == "" {
			return "Universal server stopped", nil
		}
		return "LSP " + language + " stopped", nil
	}
	return "", fmt.Errorf("unknown language-server action %q", action)
}
