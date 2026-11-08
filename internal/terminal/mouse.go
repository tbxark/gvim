package terminal

import (
	uv "github.com/charmbracelet/ultraviolet"
)

// MouseKind is what a forwarded mouse event is.
type MouseKind int

// The kinds of a forwarded mouse event.
const (
	MousePress MouseKind = iota
	MouseRelease
	MouseMotion
	MouseWheel
)

// Mouse buttons, as xterm numbers them. A motion event carries MouseNone.
const (
	MouseNone       = int(uv.MouseNone)
	MouseLeft       = int(uv.MouseLeft)
	MouseMiddle     = int(uv.MouseMiddle)
	MouseRight      = int(uv.MouseRight)
	MouseWheelUp    = int(uv.MouseWheelUp)
	MouseWheelDown  = int(uv.MouseWheelDown)
	MouseWheelLeft  = int(uv.MouseWheelLeft)
	MouseWheelRight = int(uv.MouseWheelRight)
	MouseBackward   = int(uv.MouseBackward)
	MouseForward    = int(uv.MouseForward)
)

// MouseMods are the modifiers held for a forwarded mouse event.
type MouseMods struct {
	Shift, Alt, Ctrl bool
}

// MouseEvent is one mouse event forwarded to the child. X and Y are zero-based
// cells of the shell screen.
type MouseEvent struct {
	Kind   MouseKind
	X, Y   int
	Button int
	Mods   MouseMods
}

// SendMouse forwards a mouse event to the child. The emulator encodes it in the
// form the child's modes ask for (X10 or SGR), and sends nothing when the child
// has not turned mouse reporting on.
func (s *Session) SendMouse(ev MouseEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.emu == nil {
		return
	}
	m := uv.Mouse{X: ev.X, Y: ev.Y, Button: uv.MouseButton(ev.Button), Mod: mouseMod(ev.Mods)}
	switch ev.Kind {
	case MousePress:
		s.emu.SendMouse(uv.MouseClickEvent(m))
	case MouseRelease:
		s.emu.SendMouse(uv.MouseReleaseEvent(m))
	case MouseMotion:
		s.emu.SendMouse(uv.MouseMotionEvent(m))
	case MouseWheel:
		s.emu.SendMouse(uv.MouseWheelEvent(m))
	}
}

// mouseMod converts the modifiers of a forwarded event to the ultraviolet form.
func mouseMod(m MouseMods) uv.KeyMod {
	var mod uv.KeyMod
	if m.Shift {
		mod |= uv.ModShift
	}
	if m.Alt {
		mod |= uv.ModAlt
	}
	if m.Ctrl {
		mod |= uv.ModCtrl
	}
	return mod
}
