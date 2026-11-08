package plugintest

import "github.com/tbxark/gvim/pluginapi"

// EnvSetCall is one SetEnv call of the fake host.
type EnvSetCall struct {
	Plugin  string
	Snippet string
	Dir     string
	Kind    string
}

func (h *FakeHost) DetectedEnv() (pluginapi.EnvInfo, bool) {
	return h.EnvDetected, h.HasEnvDetected
}

func (h *FakeHost) ActiveEnv() (pluginapi.EnvInfo, bool) {
	return h.EnvActiveInfo, h.HasEnvActive
}

func (h *FakeHost) EnvTrustLevel() string {
	if h.EnvTrust == "" {
		return "restricted"
	}
	return h.EnvTrust
}

func (h *FakeHost) SetEnv(plugin, snippet, dir, kind string) error {
	if h.EnvSetErr != nil {
		return h.EnvSetErr
	}
	h.EnvSets = append(h.EnvSets, EnvSetCall{Plugin: plugin, Snippet: snippet, Dir: dir, Kind: kind})
	return nil
}

func (h *FakeHost) ClearEnv(plugin string) error {
	if h.EnvClearErr != nil {
		return h.EnvClearErr
	}
	h.EnvClears++
	return nil
}
