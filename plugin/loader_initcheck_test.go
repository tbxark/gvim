package plugin_test

import (
	"strings"
	"testing"

	"github.com/tbxark/gvim/pluginapi"
)

func TestInitCheckOfTheOpenBuffer(t *testing.T) {
	check := func(e *env, text string) pluginapi.Notice {
		e.t.Helper()
		n := e.h.Control(pluginapi.Control{Op: "init_check", Name: "buffer", Text: text})
		if len(n) == 0 {
			ns := e.h.Host.Notices
			if len(ns) == 0 {
				e.t.Fatal("no notice")
			}
			last := ns[len(ns)-1]
			return pluginapi.Notice{Text: last.Msg, Warn: last.Level != pluginapi.LevelInfo}
		}
		return n[len(n)-1]
	}
	t.Run("a broken buffer is a failure, whatever the disk has", func(t *testing.T) {
		e := newEnv(t)
		e.disk.put(initFile, `T.spy("fine on disk")`)
		e.start()
		n := check(e, "T.spy('x')\nlocal x = = 1")
		if !n.Warn || strings.Contains(n.Text, "syntax OK") {
			t.Fatalf("notice = %+v, want a warning that is no 'syntax OK'", n)
		}
		for _, want := range []string{"init.lua:2:11", "syntax error"} {
			if !strings.Contains(n.Text, want) {
				t.Errorf("notice %q lacks %q", n.Text, want)
			}
		}
		if len(n.Text) > 70 {
			t.Errorf("notice %q is %d bytes: too long for the status row", n.Text, len(n.Text))
		}
	})
	t.Run("a good buffer passes, whatever the disk has", func(t *testing.T) {
		e := newEnv(t)
		e.disk.put(initFile, "local x = = 1")
		e.start()
		n := check(e, `T.spy("ok")`)
		if n.Warn || !strings.Contains(n.Text, "syntax OK") || !strings.Contains(n.Text, "unsaved buffer") {
			t.Fatalf("notice = %+v", n)
		}
	})
	t.Run("the file on disk is reported at file:line", func(t *testing.T) {
		e := newEnv(t)
		e.disk.put(initFile, "T.spy('a')\nlocal x = = 1")
		e.start()
		n := e.h.Control(pluginapi.Control{Op: "init_check"})
		ns := e.h.Host.Notices
		text := ""
		if len(n) > 0 {
			text = n[len(n)-1].Text
		} else if len(ns) > 0 {
			text = ns[len(ns)-1].Msg
		}
		if !strings.Contains(text, "init.lua:2:11") || strings.Contains(text, "line:") {
			t.Errorf("notice = %q", text)
		}
	})
}
