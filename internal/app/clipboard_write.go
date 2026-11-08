package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	goruntime "runtime"
	"unicode/utf16"

	"github.com/tbxark/gvim/internal/tui"
)

// The write half of the system clipboard, which copy and cut use in addition to OSC 52.
var _ tui.ClipboardWriter = systemClipboard{}

// Write puts text on the system clipboard with the platform's own tool. It fails when no
// tool is installed or every installed tool fails. A tool that fails (wl-copy under X11,
// say) does not stop the next one from being tried. The editor has already written the text to the terminal with OSC 52,
// so a failure here costs only the terminals that ignore it.
func (systemClipboard) Write(text string) error {
	return writeClipboard(clipboardWriteCommands(goruntime.GOOS), text)
}

// writeClipboard feeds text to the first of cmds that is installed and succeeds.
func writeClipboard(cmds [][]string, text string) error {
	tools := make([]clipboardTool, len(cmds))
	for i, argv := range cmds {
		tools[i] = clipboardTool{argv: argv, input: clipboardInput(argv[0], text)}
	}
	return runClipboardTools(tools, "no clipboard tool found; install pbcopy, wl-copy, xclip or xsel")
}

// clipboardTool is a command and what it reads on stdin.
type clipboardTool struct {
	argv  []string
	input []byte
}

// runClipboardTools runs the first of tools that is installed and succeeds.
func runClipboardTools(tools []clipboardTool, none string) error {
	var last error
	for _, tool := range tools {
		argv := tool.argv
		path, err := exec.LookPath(argv[0])
		if err != nil {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), clipboardReadTimeout)
		// Stdout and stderr stay unset: xclip and wl-copy fork a process that serves the
		// selection, and an attached pipe would keep Run waiting for it.
		cmd := exec.CommandContext(ctx, path, argv[1:]...)
		cmd.Stdin = bytes.NewReader(tool.input)
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
	return errors.New(none)
}

// The rich-text half of the system clipboard, which copy with formatting uses.
var _ tui.HTMLClipboardWriter = systemClipboard{}

// WriteHTML puts html on the system clipboard, with text as its plain-text form where the
// platform's tool can hold both. It fails when no tool that takes HTML is installed.
func (systemClipboard) WriteHTML(html, text string) error {
	tools := clipboardHTMLTools(goruntime.GOOS, html, text)
	if len(tools) == 0 {
		return errors.New("this platform has no rich-text clipboard tool")
	}
	return runClipboardTools(tools, "no rich-text clipboard tool found; install osascript, wl-copy or xclip")
}

// clipboardHTMLTools lists the commands that put html on the clipboard on goos, in the
// order they are tried. macOS sets the HTML and the plain text together through
// osascript, with both as hex data so no quoting is needed. wl-copy and xclip own one
// type at a time, so they hold only the HTML. Windows has no tool that takes HTML.
func clipboardHTMLTools(goos, html, text string) []clipboardTool {
	switch goos {
	case "darwin":
		script := fmt.Sprintf("set the clipboard to {«class utf8»:«data utf8%x», «class HTML»:«data HTML%x»}\n", text, html)
		return []clipboardTool{{argv: []string{"osascript", "-"}, input: []byte(script)}}
	case "linux":
		return []clipboardTool{
			{argv: []string{"wl-copy", "--type", "text/html"}, input: []byte(html)},
			{argv: []string{"xclip", "-selection", "clipboard", "-t", "text/html", "-in"}, input: []byte(html)},
		}
	}
	return nil
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
