package luapat

// Generated once from the row spec; wants checked against LuaJIT 2.1 and, where it
// deviates from PUC Lua 5.1, hand-set (see TestConformance comments).
var conformanceRows = []convRow{
	{op: "find", s: "hello world", p: "wor", want: "7 9"},                                                    // 0
	{op: "find", s: "hello world", p: "o", want: "5 5"},                                                      // 1
	{op: "find", s: "hello world", p: "o", init: 6, want: "8 8"},                                             // 2
	{op: "find", s: "hello world", p: "o", init: -3, want: "nil"},                                            // 3
	{op: "find", s: "hello world", p: "o", init: -100, want: "5 5"},                                          // 4
	{op: "find", s: "hello world", p: "world", init: 7, want: "7 11"},                                        // 5
	{op: "find", s: "hello world", p: "world", init: 8, want: "nil"},                                         // 6
	{op: "find", s: "hello world", p: "", init: 1, want: "1 0"},                                              // 7
	{op: "find", s: "hello world", p: "", init: 5, want: "5 4"},                                              // 8
	{op: "find", s: "hello world", p: "", init: 12, want: "12 11"},                                           // 9
	{op: "find", s: "hello world", p: "", init: 11, want: "11 10"},                                           // 10
	{op: "find", s: "hello world", p: "", init: 100, want: "12 11"},                                          // 11
	{op: "find", s: "hello", p: "l", init: 100, want: "nil"},                                                 // 12
	{op: "find", s: "a.b", p: "a.b", plain: true, want: "1 3"},                                               // 13
	{op: "find", s: "axb", p: "a.b", plain: true, want: "nil"},                                               // 14
	{op: "find", s: "a.b", p: ".", plain: true, want: "2 2"},                                                 // 15
	{op: "find", s: "a+b", p: "+", plain: true, want: "2 2"},                                                 // 16
	{op: "find", s: "a%b", p: "%", plain: true, want: "2 2"},                                                 // 17
	{op: "find", s: "abc", p: "", plain: true, want: "1 0"},                                                  // 18
	{op: "find", s: "abc", p: "c", init: 3, plain: true, want: "3 3"},                                        // 19
	{op: "find", s: "abc", p: "c", init: 4, plain: true, want: "nil"},                                        // 20
	{op: "find", s: "abc", p: "a", init: 2, plain: true, want: "nil"},                                        // 21
	{op: "find", s: "abc", p: "d", want: "nil"},                                                              // 22
	{op: "find", s: "", p: "", want: "1 0"},                                                                  // 23
	{op: "find", s: "", p: "a", want: "nil"},                                                                 // 24
	{op: "find", s: "", p: "x*", want: "1 0"},                                                                // 25
	{op: "find", s: "", p: "^", want: "1 0"},                                                                 // 26
	{op: "find", s: "", p: "$", want: "1 0"},                                                                 // 27
	{op: "find", s: "abc", p: "$", want: "4 3"},                                                              // 28
	{op: "find", s: "abc", p: "^", want: "1 0"},                                                              // 29
	{op: "find", s: "abc", p: "^abc", want: "1 3"},                                                           // 30
	{op: "find", s: "abc", p: "^bc", want: "nil"},                                                            // 31
	{op: "find", s: "abc", p: "abc$", want: "1 3"},                                                           // 32
	{op: "find", s: "abcabc", p: "abc$", want: "4 6"},                                                        // 33
	{op: "find", s: "abc$def", p: "c$d", want: "3 5"},                                                        // 34
	{op: "find", s: "a^b", p: "a^b", want: "1 3"},                                                            // 35
	{op: "find", s: "a^b", p: "^a^b", want: "1 3"},                                                           // 36
	{op: "find", s: "abc123", p: "%d+", want: "4 6"},                                                         // 37
	{op: "find", s: "abc123", p: "%D+", want: "1 3"},                                                         // 38
	{op: "find", s: "abc 123", p: "%s", want: "4 4"},                                                         // 39
	{op: "find", s: "abc\t123", p: "%s", want: "4 4"},                                                        // 40
	{op: "find", s: "abc 123", p: "%S+", want: "1 3"},                                                        // 41
	{op: "find", s: "Hello World", p: "%u", want: "1 1"},                                                     // 42
	{op: "find", s: "Hello World", p: "%l+", want: "2 5"},                                                    // 43
	{op: "find", s: "Hello World", p: "%L+", want: "1 1"},                                                    // 44
	{op: "find", s: "hello_world", p: "%w+", want: "1 5"},                                                    // 45
	{op: "find", s: "hello_world", p: "%W", want: "6 6"},                                                     // 46
	{op: "find", s: "hello, world!", p: "%p", want: "6 6"},                                                   // 47
	{op: "find", s: "hello, world!", p: "%P+", want: "1 5"},                                                  // 48
	{op: "find", s: "ab\x01cd", p: "%c", want: "3 3"},                                                        // 49
	{op: "find", s: "ab cd", p: "%C+", want: "1 5"},                                                          // 50
	{op: "find", s: "xyz 0Fa9 zz", p: "%x+", want: "5 8"},                                                    // 51
	{op: "find", s: "xyz 0Fa9 zz", p: "%X+", want: "1 4"},                                                    // 52
	{op: "find", s: "abc123", p: "%a+", want: "1 3"},                                                         // 53
	{op: "find", s: "abc123", p: "%A+", want: "4 6"},                                                         // 54
	{op: "find", s: "a.b", p: "%.", want: "2 2"},                                                             // 55
	{op: "find", s: "a.b", p: "a%.b", want: "1 3"},                                                           // 56
	{op: "find", s: "a-b", p: "%-", want: "2 2"},                                                             // 57
	{op: "find", s: "a%b", p: "%%", want: "2 2"},                                                             // 58
	{op: "find", s: "a]b", p: "%]", want: "2 2"},                                                             // 59
	{op: "find", s: "a[b", p: "%[", want: "2 2"},                                                             // 60
	{op: "find", s: "a(b", p: "%(", want: "2 2"},                                                             // 61
	{op: "find", s: "a)b", p: "%)", want: "2 2"},                                                             // 62
	{op: "find", s: "a*b", p: "%*", want: "2 2"},                                                             // 63
	{op: "find", s: "a?b", p: "%?", want: "2 2"},                                                             // 64
	{op: "find", s: "a$b", p: "%$", want: "2 2"},                                                             // 65
	{op: "find", s: "a^b", p: "%^", want: "2 2"},                                                             // 66
	{op: "find", s: "x", p: "%z", want: "nil"},                                                               // 67
	{op: "find", s: "axb", p: "a.b", want: "1 3"},                                                            // 68
	{op: "find", s: "a\nb", p: "a.b", want: "1 3"},                                                           // 69
	{op: "find", s: "hello", p: "[aeiou]", want: "2 2"},                                                      // 70
	{op: "find", s: "hello", p: "[^aeiou]+", want: "1 1"},                                                    // 71
	{op: "find", s: "hello world", p: "[a-m]+", want: "1 4"},                                                 // 72
	{op: "find", s: "hello world", p: "[%s]", want: "6 6"},                                                   // 73
	{op: "find", s: "hello world", p: "[%w_]+", want: "1 5"},                                                 // 74
	{op: "find", s: "a-b", p: "[a%-z]+", want: "1 2"},                                                        // 75
	{op: "find", s: "a-b", p: "[a-]+", want: "1 2"},                                                          // 76
	{op: "find", s: "a-b", p: "[-a]+", want: "1 2"},                                                          // 77
	{op: "find", s: "a]b", p: "[]]", want: "2 2"},                                                            // 78
	{op: "find", s: "a]b", p: "[^]]", want: "1 1"},                                                           // 79
	{op: "find", s: "a]b", p: "[a%]]+", want: "1 2"},                                                         // 80
	{op: "find", s: "2024-01-02", p: "[0-9][0-9][0-9][0-9]", want: "1 4"},                                    // 81
	{op: "find", s: "x^y", p: "[x^]+", want: "1 2"},                                                          // 82
	{op: "find", s: "abc", p: "[%a%d]+", want: "1 3"},                                                        // 83
	{op: "find", s: "a.b", p: "[.]", want: "2 2"},                                                            // 84
	{op: "find", s: "a%b", p: "[%%]", want: "2 2"},                                                           // 85
	{op: "find", s: "A1b2", p: "[A-Z0-9]+", want: "1 2"},                                                     // 86
	{op: "find", s: "zzz", p: "[a-y]", want: "nil"},                                                          // 87
	{op: "find", s: "m", p: "[a-z]", want: "1 1"},                                                            // 88
	{op: "find", s: "hello", p: "[%l%u]+", want: "1 5"},                                                      // 89
	{op: "find", s: "hello", p: "[^%l]", want: "nil"},                                                        // 90
	{op: "find", s: "a b", p: "[^%S]", want: "2 2"},                                                          // 91
	{op: "find", s: "aaab", p: "a*", want: "1 3"},                                                            // 92
	{op: "find", s: "baaab", p: "a*", want: "1 0"},                                                           // 93
	{op: "find", s: "baaab", p: "a+", want: "2 4"},                                                           // 94
	{op: "find", s: "baaab", p: "a-b", want: "1 1"},                                                          // 95
	{op: "find", s: "baaab", p: "a?b", want: "1 1"},                                                          // 96
	{op: "find", s: "aaab", p: "a-", want: "1 0"},                                                            // 97
	{op: "find", s: "aaab", p: "a?", want: "1 1"},                                                            // 98
	{op: "find", s: "aaa", p: "a-$", want: "1 3"},                                                            // 99
	{op: "find", s: "abc", p: "b*", want: "1 0"},                                                             // 100
	{op: "find", s: "abc", p: "x*", want: "1 0"},                                                             // 101
	{op: "find", s: "aXb", p: "a.-b", want: "1 3"},                                                           // 102
	{op: "find", s: "a b  c", p: "%s+", want: "2 2"},                                                         // 103
	{op: "find", s: "key = value", p: "(%w+)%s*=%s*(%w+)", want: "1 11 \"key\" \"value\""},                   // 104
	{op: "find", s: "<a><b>", p: "<.->", want: "1 3"},                                                        // 105
	{op: "find", s: "<a><b>", p: "<.*>", want: "1 6"},                                                        // 106
	{op: "find", s: "<a><b>", p: "<.+>", want: "1 6"},                                                        // 107
	{op: "find", s: "ab", p: "a?b?c?", want: "1 2"},                                                          // 108
	{op: "find", s: "aaa", p: "a-a", want: "1 1"},                                                            // 109
	{op: "find", s: "aaa", p: "^a-$", want: "1 3"},                                                           // 110
	{op: "find", s: "abc", p: "^(.-)b", want: "1 2 \"a\""},                                                   // 111
	{op: "find", s: "hello world", p: "(o)", want: "5 5 \"o\""},                                              // 112
	{op: "find", s: "hello world", p: "(h)(e)(l)", want: "1 3 \"h\" \"e\" \"l\""},                            // 113
	{op: "find", s: "hello world", p: "()ll()", want: "3 4 3 5"},                                             // 114
	{op: "find", s: "hello world", p: "(l)()", want: "3 3 \"l\" 4"},                                          // 115
	{op: "find", s: "key=val", p: "(%w+)=(%w+)", want: "1 7 \"key\" \"val\""},                                // 116
	{op: "find", s: "abc", p: "((a)(b))", want: "1 2 \"ab\" \"a\" \"b\""},                                    // 117
	{op: "find", s: "abc", p: "(a)(b)(c)(d?)", want: "1 3 \"a\" \"b\" \"c\" \"\""},                           // 118
	{op: "find", s: "x", p: "()", want: "1 0 1"},                                                             // 119
	{op: "find", s: "abc", p: "()()()", want: "1 0 1 1 1"},                                                   // 120
	{op: "find", s: "hello hello", p: "(%w+) %1", want: "1 11 \"hello\""},                                    // 121
	{op: "find", s: "hello world", p: "(%w+) %1", want: "nil"},                                               // 122
	{op: "find", s: "abcabc", p: "(abc)%1", want: "1 6 \"abc\""},                                             // 123
	{op: "find", s: "abab", p: "(a)(b)%1%2", want: "1 4 \"a\" \"b\""},                                        // 124
	{op: "find", s: "aa", p: "(a)%1", want: "1 2 \"a\""},                                                     // 125
	{op: "find", s: "ab", p: "(a)%1", want: "nil"},                                                           // 126
	{op: "find", s: "abcabc", p: "(a)(b)(c)%1%2%3", want: "1 6 \"a\" \"b\" \"c\""},                           // 127
	{op: "find", s: "xyzxyz", p: "(x(y)z)%2", want: "nil"},                                                   // 128
	{op: "find", s: "aaaa", p: "(a+)%1", want: "1 4 \"aa\""},                                                 // 129
	{op: "find", s: "a.b", p: "(.)%1", want: "nil"},                                                          // 130
	{op: "find", s: "hello", p: "(l)(l)", want: "3 4 \"l\" \"l\""},                                           // 131
	{op: "find", s: "abc", p: "(a)(.)(c)", init: 1, want: "1 3 \"a\" \"b\" \"c\""},                           // 132
	{op: "find", s: "  trim  ", p: "^%s*(.-)%s*$", want: "1 8 \"trim\""},                                     // 133
	{op: "find", s: "THE (quick) fox", p: "%f[%a]%a+", want: "1 3"},                                          // 134
	{op: "find", s: "THE (quick) fox", p: "%f[%a]%a+", init: 2, want: "6 10"},                                // 135
	{op: "find", s: "THE (quick) fox", p: "%f[%a]%a+", init: 5, want: "6 10"},                                // 136
	{op: "find", s: "THE (quick) fox", p: "%f[%l]%a+", want: "6 10"},                                         // 137
	{op: "find", s: "hello world", p: "%f[%w]%w+", init: 2, want: "7 11"},                                    // 138
	{op: "find", s: "hello world", p: "%f[%W]", want: "6 5"},                                                 // 139
	{op: "find", s: "hello", p: "%f[%W]", want: "6 5"},                                                       // 140
	{op: "find", s: "hello", p: "%f[%w]", want: "1 0"},                                                       // 141
	{op: "find", s: "", p: "%f[%w]", want: "nil"},                                                            // 142
	{op: "find", s: "", p: "%f[%W]", want: "nil"},                                                            // 143
	{op: "find", s: "foo.bar", p: "%f[%a]bar", want: "5 7"},                                                  // 144
	{op: "find", s: "xbar bar", p: "%f[%a]bar", want: "6 8"},                                                 // 145
	{op: "find", s: "foo bar", p: "%f[%S]", want: "5 4"},                                                     // 146
	{op: "find", s: "  a", p: "%f[%S]", want: "3 2"},                                                         // 147
	{op: "find", s: "ab", p: "%f[a-b]", want: "1 0"},                                                         // 148
	{op: "find", s: "ab", p: "%f[^a]", want: "2 1"},                                                          // 149
	{op: "find", s: "aXb", p: "%f[%u]", want: "2 1"},                                                         // 150
	{op: "find", s: "THE END", p: "%f[%a]%a+%f[%A]", want: "1 3"},                                            // 151
	{op: "find", s: "a1", p: "%f[%d]%d", want: "2 2"},                                                        // 152
	{op: "find", s: "1", p: "%f[%d]%d", want: "1 1"},                                                         // 153
	{op: "find", s: "a", p: "%f[%z]", want: "2 1"},                                                           // 154
	{op: "find", s: "one two", p: "%f[%a]%a*%f[%A]", want: "1 3"},                                            // 155
	{op: "find", s: "f(a(b)c)d", p: "%b()", want: "2 8"},                                                     // 156
	{op: "find", s: "f(a(b)c", p: "%b()", want: "4 6"},                                                       // 157
	{op: "find", s: "(unbalanced", p: "%b()", want: "nil"},                                                   // 158
	{op: "find", s: "x[a[b]c]y", p: "%b[]", want: "2 8"},                                                     // 159
	{op: "find", s: "if (a and (b or c)) then", p: "%b()", want: "4 19"},                                     // 160
	{op: "find", s: "{{}}", p: "%b{}", want: "1 4"},                                                          // 161
	{op: "find", s: "abc", p: "%bxy", want: "nil"},                                                           // 162
	{op: "find", s: "x(y)z", p: "%((%w)%)", want: "2 4 \"y\""},                                               // 163
	{op: "find", s: "a<b>c", p: "%b<>", want: "2 4"},                                                         // 164
	{op: "find", s: "))((", p: "%b)(", want: "1 4"},                                                          // 165
	{op: "find", s: "abc\x00def", p: "\x00", want: "4 4"},                                                    // 166
	{op: "find", s: "abc\x00def", p: "c%z", want: "3 4"},                                                     // 167
	{op: "find", s: "a\x00b", p: "a.b", want: "1 3"},                                                         // 168
	{op: "find", s: "\xad\xac", p: "[\xac\xad]+", want: "1 2"},                                               // 169
	{op: "find", s: "caf\x0195\x0e9", p: "[\n8-\xad]+", want: "1 3"},                                         // 170
	{op: "find", s: "one;two", p: "^(.-);", want: "1 4 \"one\""},                                             // 171
	{op: "find", s: "one;two", p: "^(.-);(.*)$", want: "1 7 \"one\" \"two\""},                                // 172
	{op: "find", s: "   ", p: "^%s*$", want: "1 3"},                                                          // 173
	{op: "find", s: "  x", p: "^%s*$", want: "nil"},                                                          // 174
	{op: "find", s: "2024-01-02", p: "(%d+)-(%d+)-(%d+)", want: "1 10 \"2024\" \"01\" \"02\""},               // 175
	{op: "find", s: "ab12", p: "^%a+%d+$", want: "1 4"},                                                      // 176
	{op: "find", s: "ab12x", p: "^%a+%d+$", want: "nil"},                                                     // 177
	{op: "find", s: "foo.lua", p: "%.lua$", want: "4 7"},                                                     // 178
	{op: "find", s: "foo.luax", p: "%.lua$", want: "nil"},                                                    // 179
	{op: "find", s: "path/to/file.txt", p: "[^/]+$", want: "9 16"},                                           // 180
	{op: "find", s: "path/to/file.txt", p: "([^/]*)%.(%w+)$", want: "9 16 \"file\" \"txt\""},                 // 181
	{op: "find", s: "a,b,,c", p: ",,", want: "4 5"},                                                          // 182
	{op: "find", s: "TODO: fix", p: "TODO", want: "1 4"},                                                     // 183
	{op: "find", s: "x = 10", p: "^(%w+)%s*=%s*(%d+)$", want: "1 6 \"x\" \"10\""},                            // 184
	{op: "find", s: "aaa", p: "a*?", want: "nil"},                                                            // 185
	{op: "match", s: "hello world", p: "%w+", want: "\"hello\""},                                             // 186
	{op: "match", s: "hello world", p: "(%w+) (%w+)", want: "\"hello\" \"world\""},                           // 187
	{op: "match", s: "hello world", p: "()", want: "1"},                                                      // 188
	{op: "match", s: "hello world", p: "()ll()", want: "3 5"},                                                // 189
	{op: "match", s: "hello world", p: "%w+", init: 7, want: "\"world\""},                                    // 190
	{op: "match", s: "hello world", p: "%w+", init: -5, want: "\"world\""},                                   // 191
	{op: "match", s: "hello world", p: "^%w+", init: 7, want: "\"world\""},                                   // 192
	{op: "match", s: "hello world", p: "^world", init: 7, want: "\"world\""},                                 // 193
	{op: "match", s: "key=val", p: "(%w+)=(%w+)", want: "\"key\" \"val\""},                                   // 194
	{op: "match", s: "abc", p: "x", want: "nil"},                                                             // 195
	{op: "match", s: "abc", p: "", want: "\"\""},                                                             // 196
	{op: "match", s: "abc", p: "", init: 4, want: "\"\""},                                                    // 197
	{op: "match", s: "abc", p: "", init: 9, want: "\"\""},                                                    // 198
	{op: "match", s: "2024-01-02", p: "(%d+)-(%d+)-(%d+)", want: "\"2024\" \"01\" \"02\""},                   // 199
	{op: "match", s: "  hi  ", p: "^%s*(.-)%s*$", want: "\"hi\""},                                            // 200
	{op: "match", s: "abc", p: "(a)(b)(c)", want: "\"a\" \"b\" \"c\""},                                       // 201
	{op: "match", s: "abc", p: "a(b)c", want: "\"b\""},                                                       // 202
	{op: "match", s: "abc", p: "(b)(x?)", want: "\"b\" \"\""},                                                // 203
	{op: "match", s: "[tag]", p: "%[(.-)%]", want: "\"tag\""},                                                // 204
	{op: "match", s: "a=1, b=2", p: "(%w+)=(%w+)", want: "\"a\" \"1\""},                                      // 205
	{op: "match", s: "a=1, b=2", p: "(%w+)=(%w+)", init: 5, want: "\"b\" \"2\""},                             // 206
	{op: "match", s: "x", p: "(x)(y?)", want: "\"x\" \"\""},                                                  // 207
	{op: "match", s: "ab", p: "^(a)(b)$", want: "\"a\" \"b\""},                                               // 208
	{op: "match", s: "12 apples", p: "^(%d+)%s+(%a+)$", want: "\"12\" \"apples\""},                           // 209
	{op: "match", s: "  x", p: "^%s*$", want: "nil"},                                                         // 210
	{op: "match", s: "hello", p: "(h)(e)(l)(l)(o)", want: "\"h\" \"e\" \"l\" \"l\" \"o\""},                   // 211
	{op: "match", s: "THE (quick) fox", p: "%f[%a](%a+)", init: 5, want: "\"quick\""},                        // 212
	{op: "match", s: "foo(bar)", p: "%((.-)%)", want: "\"bar\""},                                             // 213
	{op: "match", s: "name: Bob", p: "name:%s*(.+)", want: "\"Bob\""},                                        // 214
	{op: "match", s: "1.5e3", p: "^[+-]?%d+%.?%d*[eE]?[+-]?%d*$", want: "\"1.5e3\""},                         // 215
	{op: "gmatch", s: "one two three", p: "%a+", want: "[\"one\"][\"two\"][\"three\"]"},                      // 216
	{op: "gmatch", s: "one two three", p: "(%a)(%a*)", want: "[\"o\" \"ne\"][\"t\" \"wo\"][\"t\" \"hree\"]"}, // 217
	{op: "gmatch", s: "a=1, b=2, c=3", p: "(%w+)=(%w+)", want: "[\"a\" \"1\"][\"b\" \"2\"][\"c\" \"3\"]"},    // 218
	{op: "gmatch", s: "abc", p: "", want: "[\"\"][\"\"][\"\"][\"\"]"},                                        // 219
	{op: "gmatch", s: "abc", p: "%a*", want: "[\"abc\"][\"\"]"},                                              // 220
	{op: "gmatch", s: "abc", p: "x*", want: "[\"\"][\"\"][\"\"][\"\"]"},                                      // 221
	{op: "gmatch", s: "a,b,,c", p: ",", want: "[\",\"][\",\"][\",\"]"},                                       // 222
	{op: "gmatch", s: "a,b,,c", p: "([^,]*)", want: "[\"a\"][\"\"][\"b\"][\"\"][\"\"][\"c\"][\"\"]"},         // 223
	{op: "gmatch", s: "hello", p: "()", want: "[1][2][3][4][5][6]"},                                          // 224
	{op: "gmatch", s: "hello", p: "l", want: "[\"l\"][\"l\"]"},                                               // 225
	{op: "gmatch", s: "hello", p: "ll", want: "[\"ll\"]"},                                                    // 226
	{op: "gmatch", s: "abc", p: "%a", want: "[\"a\"][\"b\"][\"c\"]"},                                         // 227
	{op: "gmatch", s: "abc", p: "(%a)()", want: "[\"a\" 2][\"b\" 3][\"c\" 4]"},                               // 228
	{op: "gmatch", s: "a1b22c333", p: "%d+", want: "[\"1\"][\"22\"][\"333\"]"},                               // 229
	{op: "gmatch", s: "THE (quick) fox", p: "%f[%a]%a+", want: "[\"THE\"][\"quick\"][\"fox\"]"},              // 230
	{op: "gmatch", s: "THE (quick) fox", p: "%f[%a]%a+%f[%A]", want: "[\"THE\"][\"quick\"][\"fox\"]"},        // 231
	{op: "gmatch", s: "aaa", p: "a-", want: "[\"\"][\"\"][\"\"][\"\"]"},                                      // 232
	{op: "gmatch", s: "aaa", p: "a", want: "[\"a\"][\"a\"][\"a\"]"},                                          // 233
	{op: "gmatch", s: "aaa", p: "aa", want: "[\"aa\"]"},                                                      // 234
	{op: "gmatch", s: "x", p: "(x)(y?)", want: "[\"x\" \"\"]"},                                               // 235
	{op: "gmatch", s: "", p: "a*", want: "[\"\"]"},                                                           // 236
	{op: "gmatch", s: "abc", p: "b*", want: "[\"\"][\"b\"][\"\"][\"\"]"},                                     // 237
	{op: "gmatch", s: "  a  b ", p: "%S+", want: "[\"a\"][\"b\"]"},                                           // 238
	{op: "gmatch", s: "k1=v1;k2=v2", p: "(%w+)=(%w+)", want: "[\"k1\" \"v1\"][\"k2\" \"v2\"]"},               // 239
	{op: "gmatch", s: "f(a) g(b)", p: "%b()", want: "[\"(a)\"][\"(b)\"]"},                                    // 240
	{op: "gmatch", s: "aXbXc", p: "[^X]+", want: "[\"a\"][\"b\"][\"c\"]"},                                    // 241
	{op: "gmatch", s: "abab", p: "(ab)%1", want: "[\"ab\"]"},                                                 // 242
	{op: "gmatch", s: "ababab", p: "(ab)%1", want: "[\"ab\"]"},                                               // 243
	{op: "gsub", s: "hello world", p: "o", repl: "0", want: "\"hell0 w0rld\" 2"},                             // 244
	{op: "gsub", s: "hello world", p: "o", repl: "0", max: 1, want: "\"hell0 world\" 1"},                     // 245
	{op: "gsub", s: "hello world", p: "o", repl: "0", max: -1, want: "\"hello world\" 0"},                    // 246
	{op: "gsub", s: "hello world", p: "o", repl: "0", max: 5, want: "\"hell0 w0rld\" 2"},                     // 247
	{op: "gsub", s: "hello world", p: "(o)", repl: "[%1]", want: "\"hell[o] w[o]rld\" 2"},                    // 248
	{op: "gsub", s: "hello world", p: "o", repl: "[%0]", want: "\"hell[o] w[o]rld\" 2"},                      // 249
	{op: "gsub", s: "hello world", p: "(h)(e)", repl: "%2%1", want: "\"ehllo world\" 1"},                     // 250
	{op: "gsub", s: "hello world", p: "l+", repl: "%0%0", want: "\"hellllo worlld\" 2"},                      // 251
	{op: "gsub", s: "hello world", p: "", repl: "-", want: "\"-h-e-l-l-o- -w-o-r-l-d-\" 12"},                 // 252
	{op: "gsub", s: "abc", p: "", repl: "-", want: "\"-a-b-c-\" 4"},                                          // 253
	{op: "gsub", s: "abc", p: "x*", repl: "-", want: "\"-a-b-c-\" 4"},                                        // 254
	{op: "gsub", s: "abc", p: "b*", repl: "-", want: "\"-a--c-\" 4"},                                         // 255
	{op: "gsub", s: "abc", p: "%w", repl: "%0%0", want: "\"aabbcc\" 3"},                                      // 256
	{op: "gsub", s: "abc", p: "%w", repl: "%%", want: "\"%%%\" 3"},                                           // 257
	{op: "gsub", s: "abc", p: "b", repl: "%%%0%%", want: "\"a%b%c\" 1"},                                      // 258
	{op: "gsub", s: "abc", p: "b", repl: "%1", want: "\"abc\" 1"},                                            // 259
	{op: "gsub", s: "abc", p: "(b)", repl: "%1%1", want: "\"abbc\" 1"},                                       // 260
	{op: "gsub", s: "abc", p: "()b()", repl: "%1,%2", want: "\"a2,3c\" 1"},                                   // 261
	{op: "gsub", s: "hello world", p: "(%w+)", repl: "<%1>", want: "\"<hello> <world>\" 2"},                  // 262
	{op: "gsub", s: "hello world", p: "%w+", repl: "<%0>", want: "\"<hello> <world>\" 2"},                    // 263
	{op: "gsub", s: "hello world", p: "%f[%w]%w+", repl: "<%0>", want: "\"<hello> <world>\" 2"},              // 264
	{op: "gsub", s: "THE (quick) fox", p: "%f[%a]%a+", repl: "<%0>", want: "\"<THE> (<quick>) <fox>\" 3"},    // 265
	{op: "gsub", s: "hello world", p: "^hello", repl: "bye", want: "\"bye world\" 1"},                        // 266
	{op: "gsub", s: "hello hello", p: "^hello", repl: "bye", want: "\"bye hello\" 1"},                        // 267
	{op: "gsub", s: "hello world", p: "world$", repl: "there", want: "\"hello there\" 1"},                    // 268
	{op: "gsub", s: "hello world", p: "^", repl: "> ", want: "\"> hello world\" 1"},                          // 269
	{op: "gsub", s: "hello world", p: "$", repl: " <", want: "\"hello world <\" 1"},                          // 270
	{op: "gsub", s: "  trim  ", p: "^%s+", repl: "", want: "\"trim  \" 1"},                                   // 271
	{op: "gsub", s: "  trim  ", p: "%s+$", repl: "", want: "\"  trim\" 1"},                                   // 272
	{op: "gsub", s: "a b  c", p: "%s+", repl: " ", want: "\"a b c\" 2"},                                      // 273
	{op: "gsub", s: "a.b.c", p: "%.", repl: "/", want: "\"a/b/c\" 2"},                                        // 274
	{op: "gsub", s: "a.b.c", p: ".", repl: "x", want: "\"xxxxx\" 5"},                                         // 275
	{op: "gsub", s: "a.b.c", p: "%.", repl: "%%", want: "\"a%b%c\" 2"},                                       // 276
	{op: "gsub", s: "abc", p: "(a)(b)(c)", repl: "%3%2%1", want: "\"cba\" 1"},                                // 277
	{op: "gsub", s: "abc", p: "(a)(b)(c)", repl: "%0%3", want: "\"abcc\" 1"},                                 // 278
	{op: "gsub", s: "abc", p: "b", repl: "", want: "\"ac\" 1"},                                               // 279
	{op: "gsub", s: "abc", p: "", repl: "", want: "\"abc\" 4"},                                               // 280
	{op: "gsub", s: "aaa", p: "a", repl: "b", want: "\"bbb\" 3"},                                             // 281
	{op: "gsub", s: "aaa", p: "a", repl: "bb", max: 2, want: "\"bbbba\" 2"},                                  // 282
	{op: "gsub", s: "aaa", p: "^a", repl: "b", want: "\"baa\" 1"},                                            // 283
	{op: "gsub", s: "aaa", p: "a*", repl: "x", want: "\"xx\" 2"},                                             // 284
	{op: "gsub", s: "aaa", p: "a-", repl: "x", want: "\"xaxaxax\" 4"},                                        // 285
	{op: "gsub", s: "abc", p: "%w*", repl: "x", want: "\"xx\" 2"},                                            // 286
	{op: "gsub", s: "abc def", p: "%w*", repl: "x", want: "\"xx xx\" 4"},                                     // 287
	{op: "gsub", s: "abc", p: "[ac]", repl: "%0%0", want: "\"aabcc\" 2"},                                     // 288
	{op: "gsub", s: "x=1,y=2", p: "(%w+)=(%w+)", repl: "%2=%1", want: "\"1=x,2=y\" 2"},                       // 289
	{op: "gsub", s: "hello", p: "l", repl: "L", max: 1, want: "\"heLlo\" 1"},                                 // 290
	{op: "gsub", s: "hello", p: "", repl: "", max: 3, want: "\"hello\" 3"},                                   // 291
	{op: "gsub", s: "abc", p: "b", repl: "%a", want: "\"aac\" 1"},                                            // 292
	{op: "gsub", s: "abc", p: "b", repl: "%z", want: "\"azc\" 1"},                                            // 293
	{op: "gsub", s: "f(a(b)c)d", p: "%b()", repl: "[]", want: "\"f[]d\" 1"},                                  // 294
	{op: "gsub", s: "abab", p: "(ab)%1", repl: "X", want: "\"X\" 1"},                                         // 295
	{op: "gsub", s: "ababab", p: "(ab)%1", repl: "X", want: "\"Xab\" 1"},                                     // 296
	{op: "gsub", s: "a\nb", p: "\n", repl: "\\n", want: "\"a\\\\nb\" 1"},                                     // 297
	{op: "gsub", s: "hello world", p: "o", repl: "%1", want: "\"hello world\" 2"},                            // 298
	{op: "gsub", s: "path/to/file", p: "/", repl: "\\", want: "\"path\\\\to\\\\file\" 2"},                    // 299
	{op: "gsub", s: "one two", p: "(%w+) (%w+)", repl: "%2 %1", want: "\"two one\" 1"},                       // 300
	{op: "gsub", s: "aaa", p: "(a)", repl: "%1%1", max: 2, want: "\"aaaaa\" 2"},                              // 301
	{op: "gsub", s: "\x86\x7f\x86\x0f8", p: "[\n8-\xad]+", repl: "?", want: "\"?\\x0f?\" 2"},                 // 302
	{op: "gsub", s: "hello world", p: "%w+", fn: "upper", want: "\"HELLO WORLD\" 2"},                         // 303
	{op: "gsub", s: "hello world", p: "(%w)(%w*)", fn: "upper", want: "\"H W\" 2"},                           // 304
	{op: "gsub", s: "hello world", p: "(%w+) (%w+)", fn: "all", want: "\"<hello,world>\" 1"},                 // 305
	{op: "gsub", s: "hello world", p: "%w+", fn: "all", want: "\"<hello> <world>\" 2"},                       // 306
	{op: "gsub", s: "hello world", p: "()o", fn: "all", want: "\"hell<5> w<8>rld\" 2"},                       // 307
	{op: "gsub", s: "hello world", p: "o", fn: "nil", want: "\"hello world\" 2"},                             // 308
	{op: "gsub", s: "hello world", p: "o", fn: "false", want: "\"hello world\" 2"},                           // 309
	{op: "gsub", s: "hello world", p: "o", fn: "num", want: "\"hell42 w42rld\" 2"},                           // 310
	{op: "gsub", s: "hello world", p: "l", fn: "all", max: 2, want: "\"he<l><l>o world\" 2"},                 // 311
	{op: "gsub", s: "abc", p: "", fn: "all", want: "\"<>a<>b<>c<>\" 4"},                                      // 312
	{op: "gsub", s: "abc", p: "x*", fn: "all", want: "\"<>a<>b<>c<>\" 4"},                                    // 313
	{op: "gsub", s: "abc", p: "b", fn: "upper", want: "\"aBc\" 1"},                                           // 314
	{op: "gsub", s: "abc", p: "(a)(b)(c)", fn: "all", want: "\"<a,b,c>\" 1"},                                 // 315
	{op: "gsub", s: "a b a", p: "%w", fn: "tbl", want: "\"[A] b [A]\" 3"},                                    // 316
	{op: "gsub", s: "a b a", p: "(%w)", fn: "tbl", want: "\"[A] b [A]\" 3"},                                  // 317
	{op: "gsub", s: "a b a", p: "%w+", fn: "tbl", want: "\"[A] b [A]\" 3"},                                   // 318
	{op: "gsub", s: "12 a", p: "()", fn: "tbl", want: "\"12 a\" 5"},                                          // 319
	{op: "gsub", s: "12 a", p: "%d", fn: "tbl", want: "\"12 a\" 2"},                                          // 320
	{op: "gsub", s: "hello world", p: "(%w+)", fn: "tbl", want: "\"HI world\" 2"},                            // 321
	{op: "find", s: "a", p: "(", want: "error"},                                                              // 322
	{op: "find", s: "a", p: ")", want: "nil"},                                                                // 323
	{op: "find", s: "a", p: "%", want: "error"},                                                              // 324
	{op: "find", s: "a", p: "[a", want: "error"},                                                             // 325
	{op: "find", s: "a", p: "[", want: "error"},                                                              // 326
	{op: "find", s: "a", p: "[^", want: "error"},                                                             // 327
	{op: "find", s: "a", p: "%f", want: "error"},                                                             // 328
	{op: "find", s: "a", p: "%fa", want: "error"},                                                            // 329
	{op: "find", s: "a", p: "%b", want: "error"},                                                             // 330
	{op: "find", s: "a", p: "%bx", want: "error"},                                                            // 331
	{op: "find", s: "a", p: "(()", want: "error"},                                                            // 332
	{op: "find", s: "a", p: "%1", want: "error"},                                                             // 333
	{op: "find", s: "a", p: "(a)%2", want: "error"},                                                          // 334
	{op: "find", s: "a", p: "(a%1)", want: "error"},                                                          // 335
	{op: "match", s: "a", p: "(", want: "error"},                                                             // 336
	{op: "match", s: "a", p: "%", want: "error"},                                                             // 337
	{op: "match", s: "a", p: "[a", want: "error"},                                                            // 338
	{op: "gmatch", s: "a", p: "(", want: "error"},                                                            // 339
	{op: "gmatch", s: "a", p: "%", want: "error"},                                                            // 340
	{op: "gsub", s: "a", p: "(", repl: "x", want: "\"xax\" 2"},                                               // 341
	{op: "gsub", s: "a", p: "a", repl: "%2", want: "error"},                                                  // 342
	{op: "gsub", s: "a", p: "a", repl: "%1", want: "\"a\" 1"},                                                // 343
	{op: "gsub", s: "ab", p: "(a", repl: "x", want: "\"xb\" 1"},                                              // 344
	{op: "gsub", s: "a", p: "%", repl: "x", want: "error"},                                                   // 345
	{op: "find", s: "a", p: "a%", want: "error"},                                                             // 346
	{op: "match", s: "ab", p: "(a", want: "error"},                                                           // 347
	{op: "match", s: "ab", p: "a)", want: "error"},                                                           // 348
	{op: "match", s: "ab", p: "(a))", want: "error"},                                                         // 349
	{op: "match", s: "ab", p: "((((((((((((((((((((((((((((((((a))))))))))))))))))))))))))))))))", want: "\"a\" \"a\" \"a\" \"a\" \"a\" \"a\" \"a\" \"a\" \"a\" \"a\" \"a\" \"a\" \"a\" \"a\" \"a\" \"a\" \"a\" \"a\" \"a\" \"a\" \"a\" \"a\" \"a\" \"a\" \"a\" \"a\" \"a\" \"a\" \"a\" \"a\" \"a\" \"a\""}, // 350
	{op: "match", s: "ab", p: "(((((((((((((((((((((((((((((((a)))))))))))))))))))))))))))))))", want: "\"a\" \"a\" \"a\" \"a\" \"a\" \"a\" \"a\" \"a\" \"a\" \"a\" \"a\" \"a\" \"a\" \"a\" \"a\" \"a\" \"a\" \"a\" \"a\" \"a\" \"a\" \"a\" \"a\" \"a\" \"a\" \"a\" \"a\" \"a\" \"a\" \"a\" \"a\""},         // 351
	{op: "match", s: "ab", p: "()()()()()()()()()()()()()()()()()()()()()()()()()()()()()()()()", want: "1 1 1 1 1 1 1 1 1 1 1 1 1 1 1 1 1 1 1 1 1 1 1 1 1 1 1 1 1 1 1 1"},                                                                                                                                  // 352
	{op: "match", s: "ab", p: "()()()()()()()()()()()()()()()()()()()()()()()()()()()()()()()", want: "1 1 1 1 1 1 1 1 1 1 1 1 1 1 1 1 1 1 1 1 1 1 1 1 1 1 1 1 1 1 1"},                                                                                                                                      // 353
	{op: "find", s: "b", p: "a%", want: "nil"},     // 354
	{op: "find", s: "b", p: "a(", want: "nil"},     // 355
	{op: "find", s: "b", p: "[a", want: "error"},   // 356
	{op: "find", s: "b", p: "a)", want: "nil"},     // 357
	{op: "find", s: "b", p: "(a)%2x", want: "nil"}, // 358
}
