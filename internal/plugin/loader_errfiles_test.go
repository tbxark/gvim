package plugin_test

import (
	"strings"
	"testing"
)

// The chunk names in a traceback are relative; the log says where the files of a user
// plugin are.
func TestErrorLogSaysWhereThePluginFilesAre(t *testing.T) {
	e := newEnv(t)
	e.user("rterr/init.lua", "T.command('go', function()\nlocal x = nil\nreturn x.y\nend)")
	e.start()
	e.run("rterr.go")
	got := strings.Join(e.h.LogText("rterr"), "\n")
	if !strings.Contains(got, "plugin files: "+userDir+"/rterr") {
		t.Errorf("log = %q, want the directory of the plugin", got)
	}
}
