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
	// retryWatch is the local detector-watcher recovery hook: an env-manager
	// activation/reload (gvim.env.set/clear) after a root-Add failure asks it to start a
	// fresh bounded watcher in the background. nil for remote launches or when local file
	// watching is off.
	retryWatch func()
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
	// Env: Reload starts the env-manager's reload with detected(); that read must also
	// recover a local watcher whose root Add failed (the command may return "no env"
	// before any set/clear action). Retry only when the provider says one is needed, so
	// ordinary detection reads do not churn fsnotify.
	if wn, ok := e.provider.(interface{ WatchRetryNeeded() bool }); ok && wn.WatchRetryNeeded() {
		e.retryWatchAsync()
	}
	if !ok {
		return pluginapi.EnvInfo{}, false
	}
	return pluginapi.EnvInfo{Name: det.Name, Kind: string(det.Kind), Snippet: det.Snippet}, true
}

// CachedEnvStatus is the side-effect-free read for the status row: it never triggers the
// watcher-retry recovery that DetectedEnv performs, so rendering cannot spawn IO. The
// snippets are deliberately not carried, only the name/kind a label needs.
func (e envPort) CachedEnvStatus() (active, detected pluginapi.EnvInfo, hasActive, hasDetected bool, trust string) {
	if det, ok := e.provider.Detected(); ok {
		detected, hasDetected = pluginapi.EnvInfo{Name: det.Name, Kind: string(det.Kind)}, true
	}
	if info, ok := e.provider.Active(); ok {
		active, hasActive = pluginapi.EnvInfo{Name: info.Name, Kind: string(info.Kind)}, true
	}
	return active, detected, hasActive, hasDetected, e.TrustLevel()
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
	if err == nil {
		e.retryWatchAsync()
	}
	return err
}

func (e envPort) ClearEnv() error {
	e.provider.Clear()
	e.retryWatchAsync()
	return nil
}

// retryWatchAsync runs the potentially stat-watching recovery hook off the caller: the
// plugin env action stays non-blocking and no fsnotify call ever lands on the UI update
// loop. A live watcher makes the hook a no-op.
func (e envPort) retryWatchAsync() {
	if e.retryWatch != nil {
		go e.retryWatch()
	}
}
