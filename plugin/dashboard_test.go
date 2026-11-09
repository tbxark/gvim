package plugin_test

import (
	"strings"
	"testing"

	"github.com/tbxark/gvim/plugin/plugintest"
)

func TestDashboardSectionRegistrationAPI(t *testing.T) {
	h := plugintest.New(t, plugintest.Files{"t.lua": "\nlocal gvim = require 'gvim'\ngvim.command.register {\n  name = \"check\",\n  run = function()\n    local id, err = gvim.dashboard.register_section(\"good\", function() return { { kind = \"kv\", label = \"x\", value = \"y\" } } end, {\n      title = \"Good\", ttl_ms = 10, timeout_ms = 10, priority = 5,\n    })\n    gvim.notify(\"first:\" .. tostring(id == 1) .. \":\" .. tostring(err))\n    local dup, derr = gvim.dashboard.register_section(\"good\", function() end)\n    gvim.notify(\"dup:\" .. tostring(dup) .. \":\" .. tostring(derr ~= nil))\n    local list = gvim.dashboard.sections()\n    gvim.notify(\"list:\" .. tostring(#list) .. \":\" .. tostring(list[1].plugin) .. \":\" .. tostring(list[1].title) .. \":\" .. tostring(list[1].ttl_ms) .. \":\" .. tostring(list[1].timeout_ms) .. \":\" .. tostring(list[1].priority))\n    local bad, berr = gvim.dashboard.register_section(\"\", function() end)\n    gvim.notify(\"bad:\" .. tostring(bad) .. \":\" .. tostring(berr ~= nil))\n    local removed = gvim.dashboard.remove_section(\"good\")\n    gvim.notify(\"remove:\" .. tostring(removed) .. \":\" .. tostring(#gvim.dashboard.sections()))\n  end,\n}\n"}, plugintest.Options{})
	h.Start()
	if err := h.Run("t.check", nil); err != nil {
		t.Fatal(err)
	}
	got := strings.Join(h.NoticeTexts(), "|")
	for _, want := range []string{"first:true:nil", "dup:nil:true", "list:1:t:Good:1000:100:5", "bad:nil:true", "remove:true:0"} {
		if !strings.Contains(got, want) {
			t.Fatalf("notices = %q, missing %q", got, want)
		}
	}
}

func TestDashboardSectionCap(t *testing.T) {
	h := plugintest.New(t, plugintest.Files{"t.lua": "\nlocal gvim = require 'gvim'\ngvim.command.register {\n  name = \"cap\",\n  run = function()\n    local last, lastErr\n    for i = 1, 17 do\n      last, lastErr = gvim.dashboard.register_section(\"s\" .. i, function() return {} end)\n    end\n    gvim.notify(\"17:\" .. tostring(last) .. \":\" .. tostring(lastErr ~= nil))\n    gvim.notify(\"count:\" .. tostring(#gvim.dashboard.sections()))\n  end,\n}\n"}, plugintest.Options{})
	h.Start()
	if err := h.Run("t.cap", nil); err != nil {
		t.Fatal(err)
	}
	got := strings.Join(h.NoticeTexts(), "|")
	if !strings.Contains(got, "17:nil:true") || !strings.Contains(got, "count:16") {
		t.Fatalf("cap notices = %q, want nil/error and 16 sections", got)
	}
}

func TestDashboardSectionCallbackKeyIsHiddenFromOtherPlugins(t *testing.T) {
	h := plugintest.New(t, plugintest.Files{
		"p.lua": "\nlocal gvim = require 'gvim'\ngvim.dashboard.register_section('big', function()\n  return { { kind = 'kv', label = 'k', value = 'v' } }\nend, { title = 'Big' })\n",
		"c.lua": "\nlocal gvim = require 'gvim'\ngvim.command.register {\n  name = 'check',\n  run = function()\n    local sec\n    for _, row in ipairs(gvim.dashboard.sections()) do\n      if row.plugin == 'p' then sec = row end\n    end\n    gvim.notify('key:' .. tostring(sec.key))\n    local provider = gvim.plugins.get('p')\n    local guessed = provider['__gvim_dashboard_p_1']\n    gvim.notify('guessed:' .. tostring(guessed))\n  end,\n}\n",
	}, plugintest.Options{})
	h.Start()
	if err := h.Run("c.check", nil); err != nil {
		t.Fatal(err)
	}
	got := strings.Join(h.NoticeTexts(), "|")
	if !strings.Contains(got, "key:|") && !strings.HasSuffix(got, "key:") {
		t.Fatalf("sections() exposed the dashboard callback key to a non-dashboard plugin: %q", got)
	}
	if !strings.Contains(got, "guessed:nil") {
		t.Fatalf("proxy resolved a dashboard callback for a non-dashboard plugin: %q", got)
	}
}
