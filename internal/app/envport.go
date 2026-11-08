package app

import (
	"errors"
	"strings"

	"github.com/tbxark/gvim/config"
	"github.com/tbxark/gvim/env"
	"github.com/tbxark/gvim/pluginapi"
	"github.com/tbxark/gvim/trust"
)

// envPort adapts the workspace env.Provider to tui.EnvPort: the plugin-visible
// detection/activation seam. The provider itself owns the trust gate and the
// bounded capture; the port only converts types and error codes.
type envPort struct {
	provider workspaceEnv
	level    func() string
}

var _ interface {
	DetectedEnv() (pluginapi.EnvInfo, bool)
	ActiveEnv() (pluginapi.EnvInfo, bool)
	TrustLevel() string
	SetEnv(snippet, dir, kind string) error
	ClearEnv() error
} = envPort{}

func envInfo(info env.Info) pluginapi.EnvInfo {
	return pluginapi.EnvInfo{Name: info.Name, Kind: string(info.Kind), Snippet: info.Snippet}
}

func (e envPort) DetectedEnv() (pluginapi.EnvInfo, bool) {
	det, ok := e.provider.Detected()
	if !ok {
		return pluginapi.EnvInfo{}, false
	}
	return pluginapi.EnvInfo{Name: det.Name, Kind: string(det.Kind), Snippet: det.Snippet}, true
}

func (e envPort) ActiveEnv() (pluginapi.EnvInfo, bool) {
	info, ok := e.provider.Active()
	if !ok {
		return pluginapi.EnvInfo{}, false
	}
	return envInfo(info), true
}

func (e envPort) TrustLevel() string {
	if e.level == nil {
		return string(trust.Restricted)
	}
	return e.level()
}

func (e envPort) SetEnv(snippet, dir, kind string) error {
	kv := config.EnvKind(kind)
	if kv == "" {
		kv = config.EnvKindShell
	}
	if kv != config.EnvKindPathOnly && kv != config.EnvKindShell {
		return pluginapi.Errf(pluginapi.CodeInvalid, "kind must be %q or %q, got %q", config.EnvKindPathOnly, config.EnvKindShell, kind)
	}
	err := e.provider.Set(snippet, dir, kv)
	if errors.Is(err, env.ErrUntrusted) {
		return pluginapi.Errf(pluginapi.CodeInvalid, "%s", strings.TrimPrefix(err.Error(), "untrusted: "))
	}
	return err
}

func (e envPort) ClearEnv() error {
	e.provider.Clear()
	return nil
}
