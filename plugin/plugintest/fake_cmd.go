package plugintest

import (
	"slices"

	"github.com/tbxark/gvim/pluginapi"
)

// DoCall is one action run through Host.Do, or one step of Host.DoAll (Group > 0, the
// same number for the steps of one DoAll).
type DoCall struct {
	Action string
	Args   map[string]any
	Count  int
	Group  int
}

// FakeCmd is the state of the CommandHost fake: what plugins ran and asked for.
type FakeCmd struct {
	// Calls are the actions run, in order. Count is at least 1.
	Calls []DoCall
	// Palette are the queries of OpenPalette.
	Palette []string
	// Errors maps an action to the error Do and DoAll return for it (the call is still recorded).
	Errors map[string]error
	// Labels maps an action to its KeyLabel.
	Labels map[string]string
	// Hook runs for every action before it is recorded; its error is returned like Errors.
	Hook func(action string, args map[string]any, count int) error

	groups int
}

// Cmd returns the command state of the fake.
func (f *FakeHost) Cmd() *FakeCmd {
	c, ok := f.Ext["cmd"].(*FakeCmd)
	if !ok {
		c = &FakeCmd{Errors: map[string]error{}, Labels: map[string]string{}}
		f.Ext["cmd"] = c
	}
	return c
}

// Ran returns the names of the actions that were run, in order.
func (c *FakeCmd) Ran() []string {
	out := make([]string, len(c.Calls))
	for i, d := range c.Calls {
		out[i] = d.Action
	}
	return out
}

// runAction validates and records one action. With ActionList set, an action outside it is
// refused as the editor adapter does (unsupported).
func (f *FakeHost) runAction(action string, args map[string]any, count, group int) error {
	c := f.Cmd()
	if count < 1 {
		count = 1
	}
	if len(f.ActionList) > 0 && !slices.Contains(f.ActionList, action) {
		return pluginapi.Errf(pluginapi.CodeUnsupported, "unknown action %q", action)
	}
	c.Calls = append(c.Calls, DoCall{Action: action, Args: args, Count: count, Group: group})
	if c.Hook != nil {
		if err := c.Hook(action, args, count); err != nil {
			return err
		}
	}
	return c.Errors[action]
}

// Do records the action.
func (f *FakeHost) Do(action string, args map[string]any, count int) error {
	f.rec("Do", action, count)
	return f.runAction(action, args, count, 0)
}

// DoAll records the steps and stops at the first error.
func (f *FakeHost) DoAll(steps []pluginapi.Step) error {
	f.rec("DoAll", len(steps))
	c := f.Cmd()
	c.groups++
	for _, s := range steps {
		if err := f.runAction(s.Action, s.Args, s.Count, c.groups); err != nil {
			return err
		}
	}
	return nil
}

// KeyLabel returns the label set in Cmd().Labels.
func (f *FakeHost) KeyLabel(action string) string { return f.Cmd().Labels[action] }

// OpenPalette records the query.
func (f *FakeHost) OpenPalette(query string) {
	f.rec("OpenPalette", query)
	c := f.Cmd()
	c.Palette = append(c.Palette, query)
}
