package plugintest

import "github.com/tbxark/gvim/pluginapi"

// EnvSetCall is one SetEnv call of the fake host.
type EnvSetCall struct {
	Plugin  string
	Snippet string
	Dir     string
	Kind    string
}

func (f *FakeHost) DetectedEnv() (pluginapi.EnvInfo, bool) {
	return f.EnvDetected, f.HasEnvDetected
}

func (f *FakeHost) ActiveEnv() (pluginapi.EnvInfo, bool) {
	return f.EnvActiveInfo, f.HasEnvActive
}

func (f *FakeHost) EnvTrustLevel() string {
	if f.EnvTrust == "" {
		return "restricted"
	}
	return f.EnvTrust
}

func (f *FakeHost) SetEnv(plugin, snippet, dir, kind string) error {
	if f.EnvSetErr != nil {
		return f.EnvSetErr
	}
	f.EnvSets = append(f.EnvSets, EnvSetCall{Plugin: plugin, Snippet: snippet, Dir: dir, Kind: kind})
	return nil
}

func (f *FakeHost) ClearEnv(plugin string) error {
	if f.EnvClearErr != nil {
		return f.EnvClearErr
	}
	f.EnvClears++
	return nil
}
