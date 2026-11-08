package plugin

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	lua "github.com/yuin/gopher-lua"
	"github.com/yuin/gopher-lua/parse"
)

// Readable error messages. gopher-lua words its errors in its own way (a syntax error is
// "chunk line:5(column:3) near 'end':   syntax error", a call of a nil value names nothing, a
// stack overflow has no position and a trace of identical frames). Everything here works from
// what the library exports, so the library stays as it is: the messages are rewritten when we
// report them.

// readableCompileError turns an error of parse.Parse or lua.Compile into
// "chunk:line:column: message near 'token'", the shape every other tool uses.
func readableCompileError(chunk, src string, err error) error {
	var pe *parse.Error
	var ce *lua.CompileError
	switch {
	case errors.As(err, &pe):
		src := chunk
		if pe.Pos.Source != "" {
			src = pe.Pos.Source
		}
		msg := strings.TrimSpace(pe.Message)
		if pe.Pos.Line == parse.EOF {
			// the error is at the end of the source: its last line is the line the file ends on
			return fmt.Errorf("%s:%d: %s near <eof>", src, sourceLines(src), msg)
		}
		near := ""
		if pe.Token != "" {
			near = " near '" + pe.Token + "'"
		}
		return fmt.Errorf("%s:%d:%d: %s%s", src, pe.Pos.Line, pe.Pos.Column, msg, near)
	case errors.As(err, &ce):
		return fmt.Errorf("%s:%d: %s", chunk, ce.Line, ce.Message)
	}
	return err
}

// sourceLines is the line the source ends on, where the end of the file is: after its last line
// break, as Lua counts it.
func sourceLines(src string) int {
	return strings.Count(src, "\n") + 1
}

// maxTraceLines bounds a traceback: a runaway recursion has hundreds of frames.
const maxTraceLines = 14

// compactTrace shortens a traceback of one frame per line: runs of identical frames collapse
// to one with a count, and a long trace keeps its first and last frames.
func compactTrace(trace string) string {
	if trace == "" {
		return ""
	}
	lines := strings.Split(strings.TrimRight(trace, "\n"), "\n")
	var out []string
	for i := 0; i < len(lines); {
		j := i
		for j < len(lines) && lines[j] == lines[i] {
			j++
		}
		out = append(out, lines[i])
		if n := j - i; n > 1 {
			out = append(out, fmt.Sprintf("\t... the line above repeats %d more times", n-1))
		}
		i = j
	}
	if len(out) > maxTraceLines {
		head, tail := maxTraceLines/2, maxTraceLines/2
		skipped := len(out) - head - tail
		out = append(append(out[:head:head], fmt.Sprintf("\t... %d more frames", skipped)), out[len(out)-tail:]...)
	}
	return strings.Join(out, "\n")
}

var (
	callErrRE = regexp.MustCompile(`^(.+?):(\d+): attempt to call a non-function object$`)
	posRE     = regexp.MustCompile(`^\s*(\S+?):(\d+): `)
)

// nameCallError adds what the call was to "chunk:line: attempt to call a non-function
// object", as reference Lua does for a nil value. gopher-lua keeps the name of every call it
// compiled (Proto.DbgCalls), and the call is known when the line has only one. The frame that
// raised the error is gone by the time we report it, so the main chunk of a plugin is left
// out once it has finished (inMain false): its calls ran before the callback that failed. It
// leaves msg alone when it cannot tell.
func nameCallError(msg string, protos []*lua.FunctionProto, inMain bool) string {
	m := callErrRE.FindStringSubmatch(firstLine(msg))
	if m == nil {
		return msg
	}
	line, _ := strconv.Atoi(m[2])
	var found []string
	for _, root := range protos {
		walkProtos(root, func(fp *lua.FunctionProto) {
			if fp.SourceName != m[1] || (fp.LineDefined == 0 && !inMain) {
				return
			}
			for _, call := range fp.DbgCalls {
				if call.Pc >= 0 && call.Pc < len(fp.DbgSourcePositions) && fp.DbgSourcePositions[call.Pc] == line {
					found = append(found, callKind(fp, call.Pc, call.Name))
				}
			}
		})
	}
	if len(found) != 1 || found[0] == "" {
		return msg
	}
	first := firstLine(msg)
	return first + " (" + found[0] + ")" + msg[len(first):]
}

func walkProtos(fp *lua.FunctionProto, f func(*lua.FunctionProto)) {
	f(fp)
	for _, c := range fp.FunctionPrototypes {
		walkProtos(c, f)
	}
}

// callKind describes what a call instruction called, like reference Lua: "global 'x'",
// "field 'x'", "method 'x'", "local 'x'" or "upvalue 'x'". name is what the compiler
// recorded; "?" and "(anonymous)" give "".
func callKind(fp *lua.FunctionProto, pc int, name string) string {
	if name == "" || name == "?" || name == "(anonymous)" {
		return ""
	}
	// The call instruction's function register was loaded by the last instruction before it
	// that wrote the register: GETGLOBAL, GETTABLE(KS), SELF, GETUPVAL or MOVE (a local).
	if pc <= 0 || pc >= len(fp.Code) {
		return "'" + name + "'"
	}
	reg := opA(fp.Code[pc])
	for i := pc - 1; i >= 0 && i >= pc-64; i-- {
		in := fp.Code[i]
		if opA(in) != reg {
			continue
		}
		switch opCode(in) {
		case lua.OP_GETGLOBAL:
			return "global '" + name + "'"
		case lua.OP_GETTABLE, lua.OP_GETTABLEKS:
			return "field '" + name + "'"
		case lua.OP_SELF:
			return "method '" + name + "'"
		case lua.OP_GETUPVAL:
			return "upvalue '" + name + "'"
		case lua.OP_MOVE:
			return "local '" + name + "'"
		}
		break
	}
	return "'" + name + "'"
}

// The instruction layout of gopher-lua (opcode.go): 6 bits of opcode on top, then A in 8 bits.
// Both are checked by TestCallKindDecodesInstructions against what the compiler produced, so a
// library update that moves the fields fails a test instead of mislabelling errors.
func opCode(in uint32) int { return int(in >> 26) }
func opA(in uint32) int    { return int(in>>18) & 0xff }

// withPosition puts the position of the first frame of trace in front of msg when msg has none
// ("lua callstack overflow" is raised without one).
func withPosition(msg, trace string) string {
	if !strings.Contains(msg, "callstack overflow") || posRE.MatchString(msg) || trace == "" {
		return msg
	}
	if m := posRE.FindStringSubmatch(strings.TrimLeft(firstLine(trace), "\t ")); m != nil {
		return m[1] + ":" + m[2] + ": " + msg
	}
	return msg
}

// protos returns the compiled files of p, which nameCallError searches.
func (p *plugin) protos() []*lua.FunctionProto {
	out := make([]*lua.FunctionProto, 0, len(p.files))
	for _, c := range p.files {
		if c != nil && c.Proto != nil {
			out = append(out, c.Proto)
		}
	}
	return out
}

var locRE = regexp.MustCompile(`^(\S+?:\d+(?::\d+)?): (.+)$`)

// statusText is the first line of an error for the status row. The row is cut at its width, and
// "file:line: " in front would take the room of what went wrong, so the position goes to the end:
// "attempt to call a nil value (t/init.lua:4)". The log keeps the usual form.
func statusText(msg string) string {
	first := firstLine(msg)
	if m := locRE.FindStringSubmatch(first); m != nil {
		return m[2] + " (" + m[1] + ")"
	}
	return first
}
