package plugin

import (
	"testing"

	lua "github.com/yuin/gopher-lua"
)

// A compiled chunk keeps its slices at exact length, nested functions included: the
// compiler's append-grown backing arrays cost about 17 KiB per function otherwise.
func TestCompileSourceTrimsProtos(t *testing.T) {
	src := "local a = 1\nlocal function f(x) local y = x + a; return function() return y end end\nreturn f(2)"
	proto, err := CompileSource("t/init.lua", src)
	if err != nil {
		t.Fatal(err)
	}
	var walk func(p *lua.FunctionProto, depth int) int
	walk = func(p *lua.FunctionProto, depth int) int {
		if cap(p.Code) != len(p.Code) || cap(p.Constants) != len(p.Constants) ||
			cap(p.FunctionPrototypes) != len(p.FunctionPrototypes) ||
			cap(p.DbgSourcePositions) != len(p.DbgSourcePositions) ||
			cap(p.DbgLocals) != len(p.DbgLocals) || cap(p.DbgCalls) != len(p.DbgCalls) ||
			cap(p.DbgUpvalues) != len(p.DbgUpvalues) {
			t.Errorf("depth %d: proto slices have spare capacity", depth)
		}
		n := 1
		for _, f := range p.FunctionPrototypes {
			n += walk(f, depth+1)
		}
		return n
	}
	if n := walk(proto, 0); n != 3 {
		t.Fatalf("walked %d protos, want 3", n)
	}
	L := lua.NewState()
	defer L.Close()
	L.Push(L.NewFunctionFromProto(proto))
	L.Call(0, 1)
	if _, ok := L.Get(-1).(*lua.LFunction); !ok {
		t.Fatalf("chunk returned %v, want a function", L.Get(-1))
	}
}
