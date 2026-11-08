package plugin_test

import (
	"testing"
	"time"

	"github.com/tbxark/gvim/plugin/plugintest"
)

func TestJSONEncode(t *testing.T) {
	luaRows(t, []row{
		{"array", `T.spy(json.encode({1, 2, 3}))`, "[1,2,3]"},
		{"object", `T.spy(json.encode({a = 1}))`, `{"a":1}`},
		{"empty table is an object", `T.spy(json.encode({}))`, "{}"},
		{"marked array", `T.spy(json.encode(json.array()), json.encode(json.array({})))`, "[] []"},
		{"marked sparse array", `local t = json.array({1, nil, 3}) T.spy(json.encode(t))`, "[1,null,3]"},
		{"nested", `T.spy(json.encode({a = {1, {b = true}}, c = "x"}, {sort_keys = true}))`, `{"a":[1,{"b":true}],"c":"x"}`},
		{"sorted keys", `T.spy(json.encode({b = 1, a = 2, c = 3}, {sort_keys = true}))`, `{"a":2,"b":1,"c":3}`},
		{"insertion order", `local t = {} t.z = 1 t.y = 2 t.x = 3 T.spy(json.encode(t))`, `{"z":1,"y":2,"x":3}`},
		{"string escapes", `T.spy(json.encode("a\"b\\c\n\t\1/"))`, `"a\"b\\c\n\t\u0001/"`},
		{"invalid utf-8", `T.spy(json.encode("a\255b"))`, "\"a\xef\xbf\xbdb\""},
		{"numbers", `T.spy(json.encode({1, 1.5, -0.25, 2^52, 1e21, 0}))`, "[1,1.5,-0.25,4503599627370496,1e+21,0]"},
		{"nan", `T.spy(pcall(json.encode, 0/0))`, "~cannot encode NaN or Infinity"},
		{"infinity", `T.spy(pcall(json.encode, {1/0}))`, "~cannot encode NaN or Infinity"},
		{"null", `T.spy(json.encode({json.null, 1}), json.encode(nil), json.encode(json.null))`, "[null,1] null null"},
		{"indent", `T.spy(json.encode({a = {1, 2}, b = {}, c = json.array()}, {indent = 2, sort_keys = true}))`,
			"{\n  \"a\": [\n    1,\n    2\n  ],\n  \"b\": {},\n  \"c\": []\n}"},
		{"mixed raises", `T.spy(pcall(json.encode, {1, 2, a = 3}))`, "~mixed array and object"},
		{"sparse raises", `T.spy(pcall(json.encode, {1, nil, 3}))`, "~sparse array"},
		{"function raises", `T.spy(pcall(json.encode, {print}))`, "~cannot encode function"},
		{"boolean key raises", `T.spy(pcall(json.encode, {[true] = 1}))`, "~unsupported table key type boolean"},
		{"fractional key raises", `T.spy(pcall(json.encode, {[1.5] = 1}))`, "~unsupported table key"},
		{"cycle", `local t = {} t[1] = t T.spy(pcall(json.encode, t))`, "~circular reference"},
		{"shared subtable is not a cycle", `local s = {1} T.spy(json.encode({s, s}))`, "[[1],[1]]"},
		{"depth 200 levels", `local t = {} local c = t for i = 1, 199 do c[1] = {} c = c[1] end T.spy(#json.encode(t))`, "400"},
		{"depth 201 levels", `local t = {} local c = t for i = 1, 200 do c[1] = {} c = c[1] end T.spy(pcall(json.encode, t))`, "~json: too deep"},
		{"too many nodes", `local t = {} for i = 1, 1000001 do t[i] = 1 end T.spy(pcall(json.encode, t))`, "~json: too many nodes"},
		{"bad indent", `T.spy(pcall(json.encode, {}, {indent = "x"}))`, "~indent must be a number"},
	}, plugintest.Options{Timeout: 5 * time.Second})
}

func TestJSONDecode(t *testing.T) {
	luaRows(t, []row{
		{"document", `local v = json.decode('{"a":[1,2,{"b":null}],"c":"x\\n\\u00e9\\ud83d\\ude00"}')
			T.spy(#v.a, v.a[3].b == json.null, v.c)`, "3 true x\né\U0001F600"},
		{"numbers", `T.spy(json.decode("-1.5e2"), json.decode("0"), json.decode("1E2"), json.decode("-0.5"))`, "-150 0 100 -0.5"},
		{"scalars", `T.spy(json.decode("true"), json.decode("false"), json.decode('"s"'), json.decode(" 7 "))`, "true false s 7"},
		{"null is a sentinel", `T.spy(json.decode("null") == json.null, json.decode("[null]")[1] == json.null, tostring(json.null))`, "true true json.null"},
		{"empty containers round trip", `T.spy(json.encode(json.decode("[]")), json.encode(json.decode("{}")), #json.decode("[]"))`, "[] {} 0"},
		{"round trip", `T.spy(json.encode(json.decode('{"b":[1,2],"a":{"x":"y"}}'), {sort_keys = true}))`, `{"a":{"x":"y"},"b":[1,2]}`},
		{"duplicate keys: last wins", `T.spy(json.decode('{"a":1,"a":2}').a)`, "2"},
		{"invalid utf-8 passes through", `T.spy(#json.decode('"a\255b"'))`, "3"},
		{"lone surrogate", `T.spy(json.decode('"\\ud800x"'))`, "\uFFFDx"},
		{"bom", `T.spy(json.decode("\239\187\191[1]")[1])`, "1"},
		{"trailing comma", `T.spy(json.decode('{"a":1,}'))`, "nil 1:8: expected a string key"},
		{"error line and column", `T.spy(json.decode("[1,\n 2,\n x]"))`, "nil 3:2: unexpected character \"x\""},
		{"trailing characters", `T.spy(json.decode("1 2"))`, "nil 1:3: unexpected character after the value"},
		{"leading zero", `T.spy(json.decode("01"))`, "nil 1:2: unexpected character after the value"},
		{"bad fraction", `T.spy(select(2, json.decode("1.")))`, "1:3: invalid number: digits expected after '.'"},
		{"bad exponent", `T.spy(select(2, json.decode("1e")))`, "1:3: invalid number: digits expected in exponent"},
		{"lone minus", `T.spy(select(2, json.decode("-")))`, "1:2: invalid number"},
		{"unterminated string", `T.spy(json.decode('"abc'))`, "nil 1:5: unterminated string"},
		{"control character", `T.spy(json.decode('"a\nb"'))`, "nil 1:3: control character in string"},
		{"bad escape", `T.spy(json.decode('"\\x"'))`, "nil 1:3: invalid escape '\\x'"},
		{"empty", `T.spy(json.decode(""))`, "nil 1:1: unexpected end of input"},
		{"unterminated array", `T.spy(json.decode("[1,2"))`, "nil 1:5: unexpected end of input in array"},
		{"number out of range", `T.spy(json.decode("1e999"))`, "nil 1:1: number out of range"},
		{"comments need the option", `T.spy(json.decode("[1] // x"))`, "nil 1:5: unexpected character after the value"},
		{"jsonc option", `local v = json.decode('{"a":1, // c\n "b":[1,2,], /* x */}', {jsonc = true}) T.spy(v.a, #v.b)`, "1 2"},
		{"decode_jsonc", `T.spy(json.decode_jsonc('[1, /* two */ 2,]')[2])`, "2"},
		{"unterminated comment", `T.spy(json.decode("[1] /* x", {jsonc = true}))`, "nil 1:9: unterminated comment"},
		{"deep arrays are fine", `T.spy(type(json.decode(("["):rep(150) .. ("]"):rep(150))))`, "table"},
		{"jsonc is per call", `json.decode("[1,]", {jsonc = true}) T.spy(json.decode("[1,]"))`, "~nil 1:4: unexpected character"},
	}, plugintest.Options{Timeout: 5 * time.Second})
}
