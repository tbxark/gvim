package plugin

import (
	"context"

	"github.com/tbxark/gvim/pluginapi"
)

// gvim.agent_sessions: the bounded, read-only local agent-session discovery
// the native "Discover Agent Sessions" picker uses. The app owns the scan and its trust
// gate; this API only exposes the finished, bounded rows. It never attaches to or resumes
// a process and carries no attach/resume argv.
//
//	gvim.agent_sessions.discover() ⏳ -> { supported=, installed=, truncated=,
//	    sessions={ {kind=, tool=, id=, title=, path=, dir=, agent=, agents={...},
//	                attached=, socket_only=, exact=, mod_time=}, ... },
//	    problems={...} }
//
// A runtime without an agent-discovery service answers supported=false, so a plugin can
// state the missing capability instead of presenting an empty result as a real scan.

func init() {
	registerAwait("agent_sessions_discover", awaitAgentSessionsDiscover)
}

func installAgentSessions(e *apiEnv) {
	t := e.area("agent_sessions")
	t.RawSetString("discover", e.async("agent_sessions_discover"))
}

func awaitAgentSessionsDiscover(c *awaitCall) {
	if err := c.CheckPark(true); err != nil {
		c.Fail(err)
		return
	}
	svc := c.r.svc
	c.Park(&parkSpec{Work: &pluginapi.Work{Run: func(ctx context.Context) pluginapi.Completion {
		report, err := svc.DiscoverAgents(ctx)
		if err != nil {
			return pluginapi.Completion{Err: errString(err)}
		}
		return pluginapi.Completion{Values: []any{report}}
	}}})
}
