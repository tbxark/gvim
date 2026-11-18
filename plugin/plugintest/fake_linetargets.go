package plugintest

import (
	"fmt"

	"github.com/tbxark/gvim/pluginapi"
)

// Line targets of the fake host (tier-1c remainder).

type lineTargetEntry struct {
	owner   string
	targets []pluginapi.LineTarget
}

// SetLineTargets records the buffer's clickable lines.
func (f *FakeHost) SetLineTargets(plugin string, id pluginapi.BufferID, targets []pluginapi.LineTarget) error {
	if f.find(id) == nil {
		return bufClosedErr(id)
	}
	f.rec("SetLineTargets", plugin, id, len(targets))
	if f.Ext == nil {
		f.Ext = map[string]any{}
	}
	sets, _ := f.Ext["linetargets"].(map[pluginapi.BufferID]lineTargetEntry)
	if sets == nil {
		sets = map[pluginapi.BufferID]lineTargetEntry{}
		f.Ext["linetargets"] = sets
	}
	if len(targets) == 0 {
		delete(sets, id)
		return nil
	}
	sets[id] = lineTargetEntry{owner: plugin, targets: append([]pluginapi.LineTarget(nil), targets...)}
	return nil
}

// ClearLineTargets forgets a buffer's targets when owned by plugin.
func (f *FakeHost) ClearLineTargets(plugin string, id pluginapi.BufferID) {
	f.rec("ClearLineTargets", plugin, id)
	if sets, _ := f.Ext["linetargets"].(map[pluginapi.BufferID]lineTargetEntry); sets != nil {
		if entry, ok := sets[id]; ok && entry.owner == plugin {
			delete(sets, id)
		}
	}
}

// LineTargets returns the targets recorded for a buffer (a test helper).
func (f *FakeHost) LineTargets(id pluginapi.BufferID) []pluginapi.LineTarget {
	sets, _ := f.Ext["linetargets"].(map[pluginapi.BufferID]lineTargetEntry)
	return append([]pluginapi.LineTarget(nil), sets[id].targets...)
}

func init() {
	RegisterResourceKind(func(f *FakeHost) []string {
		sets, _ := f.Ext["linetargets"].(map[pluginapi.BufferID]lineTargetEntry)
		if len(sets) == 0 {
			return nil
		}
		out := make([]string, 0, len(sets))
		for id, entry := range sets {
			out = append(out, fmt.Sprintf("line_targets:%s@%d", entry.owner, id))
		}
		return out
	})
	OnOwnerGone(func(f *FakeHost, plugin string, _ uint64) {
		sets, _ := f.Ext["linetargets"].(map[pluginapi.BufferID]lineTargetEntry)
		for id, entry := range sets {
			if entry.owner == plugin {
				delete(sets, id)
			}
		}
	})
}
