package plugintest_test

import (
	"errors"
	"reflect"
	"testing"

	"github.com/tbxark/gvim/internal/plugin/plugintest"
	"github.com/tbxark/gvim/internal/pluginapi"
)

func TestFakeHostCommands(t *testing.T) {
	f := plugintest.NewFakeHost()

	// Without an action list every action is accepted; counts below 1 mean 1.
	if err := f.Do("anything", map[string]any{"k": 1}, 0); err != nil {
		t.Fatal(err)
	}
	f.ActionList = []string{"undo", "redo"}
	if err := f.Do("nope", nil, 1); pluginapi.CodeOf(err) != pluginapi.CodeUnsupported {
		t.Fatalf("unknown action: %v", err)
	}
	boom := errors.New("boom")
	f.Cmd().Errors["redo"] = boom
	if err := f.Do("redo", nil, 2); !errors.Is(err, boom) {
		t.Fatalf("scripted error: %v", err)
	}

	// DoAll numbers its group, records until the first error and stops there.
	err := f.DoAll([]pluginapi.Step{{Action: "undo", Count: 3}, {Action: "redo"}, {Action: "undo"}})
	if !errors.Is(err, boom) {
		t.Fatalf("DoAll: %v", err)
	}
	if err := f.DoAll([]pluginapi.Step{{Action: "undo"}}); err != nil {
		t.Fatal(err)
	}
	want := []plugintest.DoCall{
		{Action: "anything", Args: map[string]any{"k": 1}, Count: 1},
		{Action: "redo", Count: 2},
		{Action: "undo", Count: 3, Group: 1},
		{Action: "redo", Count: 1, Group: 1},
		{Action: "undo", Count: 1, Group: 2},
	}
	if got := f.Cmd().Calls; !reflect.DeepEqual(got, want) {
		t.Fatalf("calls = %+v\nwant   %+v", got, want)
	}
	if got := f.Cmd().Ran(); !reflect.DeepEqual(got, []string{"anything", "redo", "undo", "redo", "undo"}) {
		t.Fatalf("Ran = %v", got)
	}

	// The hook sees every action and may refuse it.
	f.Cmd().Hook = func(action string, _ map[string]any, count int) error {
		if action == "undo" && count == 7 {
			return boom
		}
		return nil
	}
	if err := f.Do("undo", nil, 7); !errors.Is(err, boom) {
		t.Fatalf("hook error: %v", err)
	}

	f.Cmd().Labels["undo"] = "ctrl+z"
	if f.KeyLabel("undo") != "ctrl+z" || f.KeyLabel("redo") != "" {
		t.Fatal("KeyLabel does not follow Labels")
	}
	f.OpenPalette("> go")
	f.OpenPalette("")
	if got := f.Cmd().Palette; !reflect.DeepEqual(got, []string{"> go", ""}) {
		t.Fatalf("palette = %q", got)
	}
	if f.CallCount("Do") != 4 || f.CallCount("DoAll") != 2 || f.CallCount("OpenPalette") != 2 {
		t.Fatalf("recorded calls: %v", f.Calls())
	}
}
