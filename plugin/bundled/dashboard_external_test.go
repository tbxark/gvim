package bundled_test

import (
	"strings"
	"testing"
	"time"
)

// exampleDashboardPlugin is the documented third-party example: register a section whose
// refresh callback returns bounded plain-data rows. The dashboard plugin discovers it
// through gvim.dashboard.sections and invokes the exported proxy callback.
const exampleDashboardPlugin = `
local gvim = require 'gvim'
local calls = 0
gvim.dashboard.register_section('demo', function(ctx)
  calls = calls + 1
  return {
    { kind = 'kv', label = 'demo', value = 'hello' },
    { kind = 'text', text = 'from a third-party plugin; width=' .. tostring(ctx.width) },
  }
end, { title = 'Demo Section', ttl_ms = 1000, timeout_ms = 1000 })
gvim.command.register {
  name = 'demo_calls',
  run = function()
    gvim.notify('demo calls ' .. tostring(calls))
  end,
}
`

func TestDashboardThirdPartySection(t *testing.T) {
	r := startRig(t, rigOpts{
		plugins: []string{dashboardPlugin},
		script:  dashboardRemoteScript("https://github.com/me/repo"),
		extra:   map[string]string{"demo/init.lua": exampleDashboardPlugin},
	})
	dashboardReady(t, r)
	text := dashboardSettle(t, r, "Demo Section")
	if !strings.Contains(text, "Demo Section") || !strings.Contains(text, "hello") || !strings.Contains(text, "third-party plugin") {
		t.Fatalf("dashboard text misses the external section: %s (log %v)", text, r.h.LogText(""))
	}
	// The provider's callback ran once for the initial refresh; the dashboard did not
	// block and the section is cached by its ttl.
	if err := r.h.Run("demo.demo_calls", nil); err != nil {
		t.Fatal(err)
	}
	r.h.Settle()
	if !strings.Contains(strings.Join(r.h.NoticeTexts(), "\n"), "demo calls 1") {
		t.Fatalf("provider notices = %v", r.h.NoticeTexts())
	}
}

const failingDashboardPlugin = `
local gvim = require 'gvim'
gvim.dashboard.register_section('good', function()
  return { { kind = 'kv', label = 'ok', value = 'still here' } }
end, { title = 'Good Section', ttl_ms = 1000 })
gvim.dashboard.register_section('bad', function()
  error('provider boom')
end, { title = 'Bad Section', ttl_ms = 1000, timeout_ms = 1000 })
`

func TestDashboardThirdPartySectionFailureIsolated(t *testing.T) {
	r := startRig(t, rigOpts{
		plugins: []string{dashboardPlugin},
		script:  dashboardRemoteScript("https://github.com/me/repo"),
		extra:   map[string]string{"demo/init.lua": failingDashboardPlugin},
	})
	dashboardReady(t, r)
	text := dashboardSettle(t, r, "Good Section")
	if !strings.Contains(text, "still here") {
		t.Fatalf("good section missing after a peer failed: %s", text)
	}
	if !strings.Contains(text, "Bad Section") || !strings.Contains(text, "unavailable") {
		t.Fatalf("bad section did not degrade to unavailable: %s", text)
	}
}

const timeoutDashboardPlugin = `
local gvim = require 'gvim'
gvim.dashboard.register_section('slow', function()
  gvim.timer.sleep(500)
  return { { kind = 'text', text = 'late data' } }
end, { title = 'Slow Section', ttl_ms = 1000, timeout_ms = 100 })
`

func TestDashboardThirdPartySectionTimeoutIsolated(t *testing.T) {
	r := startRig(t, rigOpts{
		plugins: []string{dashboardPlugin},
		script:  dashboardRemoteScript("https://github.com/me/repo"),
		extra:   map[string]string{"demo/init.lua": timeoutDashboardPlugin},
	})
	dashboardReady(t, r)
	r.h.Advance(200 * time.Millisecond) // let the section timeout fire
	r.h.Settle()
	text := dashboardSettle(t, r, "Slow Section")
	if !strings.Contains(text, "Slow Section") || !strings.Contains(text, "unavailable") {
		t.Fatalf("timed-out section did not degrade: %s", text)
	}
	if strings.Contains(text, "late data") {
		t.Fatalf("timed-out section painted late data: %s", text)
	}
}

const cappedDashboardPlugin = `
local gvim = require 'gvim'
gvim.dashboard.register_section('many', function()
  local rows = {}
  for i = 1, 100 do
    rows[i] = { kind = 'kv', label = 'row' .. i, value = 'v' .. i }
  end
  return rows
end, { title = 'Capped Section', ttl_ms = 1000 })
`

func TestDashboardThirdPartySectionRowCap(t *testing.T) {
	r := startRig(t, rigOpts{
		plugins: []string{dashboardPlugin},
		script:  dashboardRemoteScript("https://github.com/me/repo"),
		extra:   map[string]string{"demo/init.lua": cappedDashboardPlugin},
	})
	dashboardReady(t, r)
	text := dashboardSettle(t, r, "Capped Section")
	if !strings.Contains(text, "row64") {
		t.Fatalf("expected capped rows through row64: %s", text)
	}
	if strings.Contains(text, "row65") {
		t.Fatalf("external row cap did not apply: %s", text)
	}
}
