// Package pluginhost wires the Lua plugin runtime into the executable: the real
// hostos.OS (processes, files, watchers), the editor as plugins see it
// (pluginapi.BufferEditor), the plugin services (width, diff, fuzzy, grep,
// decode, git), virtual buffers and decorations. It is a host package used by
// internal/app.
package pluginhost
