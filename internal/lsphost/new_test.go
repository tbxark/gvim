package lsphost

import (
	"github.com/tbxark/gvim/config"
	"github.com/tbxark/gvim/internal/tui"
	"github.com/tbxark/gvim/lsp/manager"
)

// New returns the language-server port for the servers of the config, or nil when
// no server is configured, so that no LSP feature is on. It is the explicit-server
// form the tests use: each server is one that starts on its own. NewFor is the
// config form, with the built-in table and the universal servers.
func New(servers map[string]config.LSPServer) tui.LSPSession {
	if len(servers) == 0 {
		return nil
	}
	plan := lspPlan{
		servers:   make(map[string][]manager.ServerConfig),
		info:      make(map[string]tui.LSPServerInfo),
		auto:      make(map[string]bool),
		startable: make(map[string]bool),
	}
	for lang, s := range servers {
		cfg := serverConfig(s, manager.Spec{})
		cfg.AutoStart = true
		plan.servers[lang] = []manager.ServerConfig{cfg}
		plan.info[lang] = tui.LSPServerInfo{State: tui.LSPServerOn, Language: lang, Command: cfg.Command[0]}
		plan.auto[lang] = true
		plan.startable[lang] = true
	}
	return newPort(plan, true, nil)
}
