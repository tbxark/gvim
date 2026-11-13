//go:build darwin || freebsd || netbsd || openbsd || dragonfly

package pluginhost

// kqueueDirLimit is the most entries a watched directory may have. kqueue watches a
// directory by opening a descriptor for every entry in it, so a big directory would eat the
// descriptors of the editor.
const kqueueDirLimit = 512
