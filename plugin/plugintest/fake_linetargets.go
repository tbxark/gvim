package plugintest

import "github.com/tbxark/gvim/pluginapi"

// Line targets of the fake host (tier-1c remainder).

// SetLineTargets records the buffer's clickable lines.
func (f *FakeHost) SetLineTargets(plugin string, id pluginapi.BufferID, targets []pluginapi.LineTarget) error {
	if f.find(id) == nil {
		return bufClosedErr(id)
	}
	f.rec("SetLineTargets", plugin, id, len(targets))
	if f.Ext == nil {
		f.Ext = map[string]any{}
	}
	sets, _ := f.Ext["linetargets"].(map[pluginapi.BufferID][]pluginapi.LineTarget)
	if sets == nil {
		sets = map[pluginapi.BufferID][]pluginapi.LineTarget{}
		f.Ext["linetargets"] = sets
	}
	if len(targets) == 0 {
		delete(sets, id)
		return nil
	}
	sets[id] = append([]pluginapi.LineTarget(nil), targets...)
	return nil
}

// ClearLineTargets forgets a buffer's targets.
func (f *FakeHost) ClearLineTargets(plugin string, id pluginapi.BufferID) {
	f.rec("ClearLineTargets", plugin, id)
	if sets, _ := f.Ext["linetargets"].(map[pluginapi.BufferID][]pluginapi.LineTarget); sets != nil {
		delete(sets, id)
	}
}

// LineTargets returns the targets recorded for a buffer (a test helper).
func (f *FakeHost) LineTargets(id pluginapi.BufferID) []pluginapi.LineTarget {
	sets, _ := f.Ext["linetargets"].(map[pluginapi.BufferID][]pluginapi.LineTarget)
	return append([]pluginapi.LineTarget(nil), sets[id]...)
}
