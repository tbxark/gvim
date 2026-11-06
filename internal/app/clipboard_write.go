package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	goruntime "runtime"
	"unicode/utf16"

	"github.com/tbxark/gvim/internal/ui"
)

// The write half of the system clipboard, which copy and cut use in addition to OSC 52.
var _ ui.ClipboardWriter = systemClipboard{}

// Write puts text on the system clipboard with the platform's own tool. It fails when no
// tool is installed or every installed tool fails. A tool that fails (wl-copy under X11,
// say) does not stop the next one from being tried. The editor has already written the text to the terminal with OSC 52,
// so a failure here costs only the terminals that ignore it.
func (systemClipboard) Write(text string) error {
	return writeClipboard(clipboardWriteCommands(goruntime.GOOS), text)
}

// writeClipboard feeds text to the first of cmds that is installed and succeeds.
func writeClipboard(cmds [][]string, text string) error {
	var last error
	for _, argv := range cmds {
		path, err := exec.LookPath(argv[0])
		if err != nil {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), clipboardReadTimeout)
		// Stdout and stderr stay unset: xclip and wl-copy fork a process that serves the
		// selection, and an attached pipe would keep Run waiting for it.
		cmd := exec.CommandContext(ctx, path, argv[1:]...)
		cmd.Stdin = bytes.NewReader(clipboardInput(argv[0], text))
		err = cmd.Run()
		cancel()
		if err == nil {
			return nil
		}
		last = fmt.Errorf("%s: %w", argv[0], err)
	}
	if last != nil {
		return last
	}
	return errors.New("no clipboard tool found; install pbcopy, wl-copy, xclip or xsel")
}

// clipboardInput is the bytes tool reads for text. clip.exe decodes its input with the
// system code page unless it starts with a byte order mark, so it gets UTF-16LE with one;
// the other tools take UTF-8.
func clipboardInput(tool, text string) []byte {
	if tool != "clip.exe" {
		return []byte(text)
	}
	units := utf16.Encode([]rune(text))
	out := make([]byte, 2, 2+2*len(units))
	out[0], out[1] = 0xFF, 0xFE
	for _, u := range units {
		out = append(out, byte(u), byte(u>>8))
	}
	return out
}

// clipboardWriteCommands lists the commands that read the clipboard from their input on
// goos, in the order they are tried. clip.exe is last on Linux for WSL.
func clipboardWriteCommands(goos string) [][]string {
	switch goos {
	case "darwin":
		return [][]string{{"pbcopy"}}
	case "linux":
		return [][]string{
			{"wl-copy"},
			{"xclip", "-selection", "clipboard", "-in"},
			{"xsel", "--clipboard", "--input"},
			{"clip.exe"},
		}
	case "windows":
		return [][]string{{"clip.exe"}}
	}
	return nil
}
