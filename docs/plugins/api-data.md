# Data libraries and the standard library

## The standard library

A plugin state has these globals and nothing else (besides `gvim`, `json`, `re`, `utf8`, `bit` and
`require`):

| Library | What is there |
|---|---|
| base | `assert error getmetatable ipairs next pairs pcall print rawequal rawget rawset select setmetatable tonumber tostring type unpack xpcall _G _VERSION` |
| `string` | `byte char find format gmatch gsub len lower match rep reverse sub upper` and the `s:method()` forms |
| `table` | `concat getn insert maxn move pack remove sort unpack` |
| `math` | `abs acos asin atan atan2 ceil cos cosh deg exp floor fmod frexp huge ldexp log log10 max min modf pi rad random randomseed sin sinh sqrt tan tanh` |
| `os` | `time clock date difftime` only (no `execute`, `remove`, `rename`, `tmpname`, `exit`, `setenv`, `getenv`) |
| `coroutine` | `create resume running status wrap yield` (yield-safe, see [conventions.md](conventions.md#tasks-and-the--calls)) |

**Not available**: `io`, `os.execute` and friends, `debug`, `package`, `dofile`, `loadfile`, `load`,
`loadstring`, `module`, `newproxy`, `collectgarbage`, `getfenv`, `setfenv`, `string.dump`,
`string.gfind`, `math.mod`, `math.pow`. Files go through `gvim.fs`, processes through `gvim.proc`, the
environment through `gvim.env`. `require` is gvim's ([README.md](README.md#the-manifest-pluginjson)); the
string metatable is locked.

**Repaired in gvim**, because the stock gopher-lua versions are wrong or unbounded:

* `tonumber("1e2")` works, `math.huge` is infinity, `tostring` and `string.format("%s", v)` honour
  `__tostring`.
* `string.find`, `match`, `gmatch` and `gsub` run on **luapat**, gvim's Lua 5.1 pattern matcher
  (with `%f` and `%b`) that counts steps: a call gets 2 000 000 steps plus 8 per byte of the subject and
  then fails with `pattern too complex`, so a catastrophic pattern cannot hang the editor. A pattern is
  at most 256 bytes and a subject at most 16 MiB. A plain `find` (the fourth argument `true`, or a
  pattern without special characters) is a substring search with no budget. One difference from stock
  5.1: a lone `%` at the end of a `gsub` replacement string raises `invalid use of '%' in replacement
  string`.
* `string.rep`, `table.concat`, `string.format`, `table.sort` and `unpack` have size caps
  ([limits.md](limits.md#the-cap-table)); `string.format` takes `%d %i %u %c %x %X %o %e %E %f %g %G %q
  %s %%` with width and precision up to 99.
* `os.date` and `os.time` use the editor's clock; `os.date` understands the usual `strftime` letters.
* `table.sort` is a Go implementation: more than 500 000 elements raise `table too large to sort`, and
  a long default-comparator sort notices the time budget.

String concatenation (`s = s .. s`) cannot be bounded: a loop that doubles a string is stopped by the
heap guard (a slice that grows the heap by 256 MiB is cancelled and counts as a strike).

## gvim.json

```text
json.encode(v, opts?) -> string          -- opts {indent = 2, sort_keys = false}
json.decode(s, opts?) -> value | nil, "line:col: message"    -- opts {jsonc = false}
json.decode_jsonc(s) -> value | nil, err
json.null                                 -- what JSON null decodes to
json.array(t) -> t                        -- mark a table as an array
```

* A table with keys `1..n` is an array, any other table is an object; a **mixed** table raises. An
  empty table is the object `{}`; `json.array({})` encodes as `[]`, and holes in a marked array become
  `null`.
* `null` decodes to the sentinel `json.null`, so arrays stay contiguous.
* Caps: input and output at most 64 MiB, at most 1 000 000 nodes and 200 levels (checked while
  parsing, before tables are built; `json: too many nodes`, `json: too deep`). A cycle in `encode`
  raises.

## gvim.re

Go's RE2 regular expressions, linear time, for when Lua patterns are not enough. The syntax is
RE2's (https://github.com/google/re2/wiki/Syntax): no backreferences or lookaround.

```text
re.compile(pattern, flags?) -> Regex | nil, err          -- flags "i" "m" "s"
re.find(s, pattern, init?) -> start, end, captures... | nil     -- 1-based inclusive, like string.find
re.match(s, pattern, init?) -> captures... | nil       -- the whole match when there are no groups
re.test(s, pattern) -> boolean
re.gmatch(s, pattern) -> iterator
re.gsub(s, pattern, repl, n?) -> string, count          -- repl: string ($1, ${name}), table or function
re.split(s, pattern, n?) -> string[]
re.escape(s) -> string
-- a Regex has the same methods: rx:find(s, init) rx:match rx:test rx:gmatch rx:gsub rx:split
```

`pattern` may be a string or a compiled `Regex`. Positions are **1-based** like `string.find`. A group
that did not take part in the match is `false`. Failures return `nil, "<code>: <message>"`
(`invalid: ...` for a bad pattern, `limit: ...` for a cap); `re.gmatch` raises instead, because it
returns an iterator.

> **The regex work cap.** RE2 cannot be interrupted, so gvim refuses work it cannot bound. A
> pattern is compiled to a program, and a program of more than 2000 instructions is refused at compile
> time (`limit: pattern too complex`). Before every `find`, `match`, `test`, `gmatch`, `gsub` or
> `split`, `work = len(subject) * instructions` must be at most 40 000 000, otherwise the call returns
> `limit: subject too large for this pattern; use b:find_lines/b:find or scan line by line`. The cost
> is about 6 ns per unit, so the cap is about a quarter of a second at worst. A pattern that is a
> plain literal is a substring search and only the 16 MiB subject cap applies. The pattern may be at
> most 4 KiB and the subject 16 MiB. Do not search a whole 10 MiB buffer with a heavy regex: use
> `b:find` (host side, with a range) or `b:find_lines`, or scan line by line.

`re.gsub` replacement: `$1` or `${name}` expand groups, `$$` is a dollar sign; a table replaces by the
first capture; a function receives the captures. The result is capped at 64 MiB.

## gvim.text

```text
gvim.text.width(s) -> integer                   -- display cells: wide and combining characters aware
gvim.text.truncate(s, w, ellipsis?) -> string   -- to at most w cells
gvim.text.pad(s, w, align?) -> string           -- "left" (default) | "right" | "center"; w <= 8192
gvim.text.upper(s)  gvim.text.lower(s)  gvim.text.trim(s) -> string
gvim.text.split(s, sep, plain?) -> string[]     -- sep is a Lua pattern unless plain is true; at most 1 000 000 pieces
gvim.text.starts_with(s, prefix)  gvim.text.ends_with(s, suffix) -> boolean
gvim.text.fuzzy(query, items, opts?) ⏳ -> { {item, score, indexes}, ... } | nil, err
gvim.text.decode(data, encoding, opts?) -> string | nil, err
```

* `fuzzy` ranks with the editor's own fuzzy scorer in the background. opts `key` (a field of a table
  item to match on), `limit` (default 50, at most 1000); at most 50 000 items, a query of at most 256
  bytes (longer: `too_large`); only the first 4096 bytes of an item take part in the match. `indexes` are
  **1-based character positions** of the matched characters.
* `decode(data, encoding, {normalize = true})` converts bytes from an encoding to UTF-8; `encoding` is
  the label of `b:info().encoding` (`"UTF-8"`, `"UTF-16 LE"`, `"Latin-1"`, ...). `normalize` turns
  CRLF and lone CR into LF. At most 16 MiB.

## gvim.utf8

Lua 5.3 `utf8` semantics on 1-based byte positions, plus helpers. Also the global `utf8`.

```text
utf8.len(s, i?, j?) -> integer | nil, position     utf8.char(...) -> string
utf8.codepoint(s, i?, j?) -> integer...            utf8.offset(s, n, i?) -> integer | nil
utf8.codes(s) -> iterator                          utf8.charpattern
utf8.valid(s) -> boolean
utf8.sub(s, ci, cj?) -> string                     -- by character, not byte
utf8.byte_to_char(s, b) -> integer                 utf8.char_to_byte(s, c) -> integer
```

## gvim.bit

32-bit integer operations (Lua 5.1 has no bit operators): `bit.band`, `bit.bor`, `bit.bxor`, `bit.bnot`,
`bit.lshift`, `bit.rshift`. Also the global `bit`.

## gvim.time

```text
gvim.time.now_ms() -> integer                 -- wall clock, milliseconds since 1970
gvim.time.monotonic_ms() -> integer           -- since the plugin was loaded; never goes back
gvim.time.format(fmt, t_ms?) -> string        -- strftime subset; t_ms defaults to now; a leading "!" means UTC
```

`format` understands `%Y %m %d %H %M %S %j %a %A %b %B %p %Z %z %%`. A format over 1024 bytes raises.

## gvim.theme

```text
gvim.theme.name() -> string
gvim.theme.color(key) -> { r, g, b } | nil     -- key "section.key", for example "editor.selection_bg"
gvim.theme.is_dark() -> boolean
```

`gvim.theme.builtin_themes()` lists the theme names built into the editor (the same names `gvim.theme.name()` can return), for a theme chooser or the theme editor.

**Session colour overrides.** `gvim.theme.override{ ["editor.bg"] = "#112233", ["ui.accent_bg"] = "Blue" }`
layers colour overrides over the active theme for the rest of the session (at most 64 keys, key
and value 64 bytes): the values are `#RRGGBB`, Fresh's named colours (`"Red"`, `"DarkGray"`, ...) or
`"Default"`; attribute bundles are not accepted. `gvim.theme.clear()` drops them and
`gvim.theme.overrides()` returns the map in force. The editor rebuilds its styles at once and raises
`theme_changed`; a later theme change clears the overrides, as Fresh's does. A bad key or value
returns `nil, err` and keeps the current look. For fast animation loops prefer a
`gvim.timer.every` that calls `override` with a small map.

