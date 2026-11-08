package tui

// WrapDialog returns d as the root's Overlay. A dialog implements the contract
// directly now, so this is the identity; the app adapters call it to keep their
// own surface unchanged.
func WrapDialog(d Overlay) Overlay { return d }
