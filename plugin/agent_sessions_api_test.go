package plugin_test

import (
	"testing"
	"time"

	"github.com/tbxark/gvim/plugin/plugintest"
	"github.com/tbxark/gvim/pluginapi"
)

func TestAgentSessionsDiscover(t *testing.T) {
	svc := &plugintest.FakeServices{AgentDiscovery: pluginapi.AgentReport{
		Supported: true, Installed: true, Truncated: true,
		Sessions: []pluginapi.AgentSession{
			{Kind: "tmux", Tool: "tmux", ID: "s/work", Title: "work", Path: "/w", Dir: "/w", Agents: []string{"claude"}, Attached: true},
			{Kind: "history", Tool: "claude-code", ID: "abc", Title: "proj", Path: "/home/u/.claude/p/a.jsonl", Dir: "/w", Agent: "claude", Exact: true, ModTime: 42},
		},
		Problems: []string{"one problem"},
	}}
	h := start(t, gv+`
gvim.command.register{ name = "go", run = function()
  local r = gvim.agent_sessions.discover()
  T.spy(r.supported, r.installed, r.truncated, #r.sessions, #r.problems)
  T.spy(r.sessions[1].kind, r.sessions[1].title, r.sessions[1].attached, table.concat(r.sessions[1].agents, ","))
  T.spy(r.sessions[2].tool, r.sessions[2].path, r.sessions[2].exact, r.sessions[2].mod_time)
end }`, plugintest.Options{Services: svc, Timeout: 5 * time.Second})
	if err := h.Run("t.go", nil); err != nil {
		t.Fatal(err)
	}
	if got := spyText(h); got != "true true true 2 1|tmux work true claude|claude-code /home/u/.claude/p/a.jsonl true 42" {
		t.Fatalf("spy = %q (log %v)", got, h.LogText(""))
	}
}

func TestAgentSessionsUnsupportedWithoutService(t *testing.T) {
	h := start(t, gv+`
gvim.command.register{ name = "go", run = function()
  local r = gvim.agent_sessions.discover()
  T.spy(r.supported, r.installed, #r.sessions)
end }`)
	if err := h.Run("t.go", nil); err != nil {
		t.Fatal(err)
	}
	if got := spyText(h); got != "false false 0" {
		t.Fatalf("spy = %q (log %v)", got, h.LogText(""))
	}
}
