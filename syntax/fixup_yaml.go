package syntax

import (
	"regexp"
	"strings"

	"github.com/alecthomas/chroma/v2"
)

// yamlNumberRe is a YAML scalar that is a number. The lexer knows plain integers and
// floats, and not the hex and octal forms or the infinities.
var yamlNumberRe = regexp.MustCompile(`^(?:[-+]?(?:0x[0-9a-fA-F_]+|0o[0-7_]+|\d[\d_]*(?:\.\d*)?(?:[eE][-+]?\d+)?|\.\d+(?:[eE][-+]?\d+)?)|[-+]?\.(?:inf|Inf|INF)|\.(?:nan|NaN|NAN))$`)

// yamlFlow repairs flow collections, "{a: 1, b: [x, y]}", which the lexer reads as one
// run of keys and text, and the plain scalars that are numbers or constants.
func yamlFlow(toks []chroma.Token) []chroma.Token {
	out := make([]chroma.Token, 0, len(toks))
	for i := 0; i < len(toks); i++ {
		t := toks[i]
		if (t.Value == "{" || t.Value == "[") && (t.Type == chroma.Text || t.Type == chroma.Punctuation) {
			if region, last, rest := flowRegion(toks[i:]); region != "" {
				out = append(out, lexFlow(region)...)
				if rest != "" {
					out = append(out, chroma.Token{Type: toks[i+last].Type, Value: rest})
				}
				i += last
				continue
			}
		}
		if t.Type == chroma.Literal && yamlNumberRe.MatchString(t.Value) {
			t.Type = numberType(strings.TrimLeft(t.Value, "+-"))
			if strings.Contains(strings.ToLower(t.Value), "inf") || strings.Contains(strings.ToLower(t.Value), "nan") {
				t.Type = chroma.LiteralNumberFloat
			}
		}
		if t.Type == chroma.Literal && t.Value == "~" {
			t.Type = chroma.KeywordConstant
		}
		out = append(out, t)
	}
	return out
}

// maxFlowBytes bounds a flow collection that yamlFlow reads.
const maxFlowBytes = 4096

// flowRegion returns the text of the flow collection that begins with the first token of
// toks, the index of the token it ends in, and the part of that token after it. The
// region is "" when the collection does not close within maxFlowBytes.
func flowRegion(toks []chroma.Token) (region string, last int, rest string) {
	depth := 0
	var b strings.Builder
	for k, t := range toks {
		quote := byte(0)
		for p := 0; p < len(t.Value); p++ {
			c := t.Value[p]
			switch {
			case quote != 0:
				if c == '\\' && quote == '"' {
					p++
				} else if c == quote {
					quote = 0
				}
			case c == '"' || c == '\'':
				quote = c
			case c == '{' || c == '[':
				depth++
			case c == '}' || c == ']':
				depth--
				if depth == 0 {
					b.WriteString(t.Value[:p+1])
					return b.String(), k, t.Value[p+1:]
				}
			}
		}
		b.WriteString(t.Value)
		if b.Len() > maxFlowBytes {
			break
		}
	}
	return "", 0, ""
}

// lexFlow reads the text of a flow collection: punctuation, quoted strings, keys, and
// scalars that are numbers, constants or plain text.
func lexFlow(s string) []chroma.Token {
	var out []chroma.Token
	var stack []byte
	add := func(t chroma.TokenType, v string) {
		if v != "" {
			out = append(out, chroma.Token{Type: t, Value: v})
		}
	}
	for i := 0; i < len(s); {
		c := s[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			j := i
			for j < len(s) && strings.IndexByte(" \t\r\n", s[j]) >= 0 {
				j++
			}
			add(chroma.Text, s[i:j])
			i = j
		case c == '{' || c == '[':
			stack = append(stack, c)
			add(chroma.Punctuation, s[i:i+1])
			i++
		case c == '}' || c == ']':
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
			add(chroma.Punctuation, s[i:i+1])
			i++
		case c == ',':
			add(chroma.Punctuation, ",")
			i++
		case c == '"' || c == '\'':
			j := i + 1
			for j < len(s) && s[j] != c {
				if s[j] == '\\' && c == '"' {
					j++
				}
				j++
			}
			j = min(j+1, len(s))
			typ := chroma.LiteralStringDouble
			if c == '\'' {
				typ = chroma.LiteralStringSingle
			}
			scalarEnd := j
			add(typ, s[i:scalarEnd])
			i = scalarEnd
		case c == ':' && (i+1 >= len(s) || strings.IndexByte(" \t\r\n,]}", s[i+1]) >= 0):
			add(chroma.Punctuation, ":")
			i++
		default:
			j := i
			for j < len(s) {
				d := s[j]
				if d == ',' || d == ']' || d == '}' || d == '\n' ||
					d == ':' && (j+1 >= len(s) || strings.IndexByte(" \t\r\n,]}", s[j+1]) >= 0) {
					break
				}
				j++
			}
			word := strings.TrimRight(s[i:j], " \t\r")
			end := i + len(word)
			rest := strings.TrimLeft(s[end:], " \t")
			isKey := len(stack) > 0 && stack[len(stack)-1] == '{' && strings.HasPrefix(rest, ":")
			switch {
			case isKey:
				add(chroma.NameTag, word)
			case word == "true" || word == "false" || word == "null" || word == "~" || word == "True" || word == "False" || word == "Null":
				add(chroma.KeywordConstant, word)
			case yamlNumberRe.MatchString(word):
				add(numberType(strings.TrimLeft(word, "+-")), word)
			default:
				add(chroma.Literal, word)
			}
			i = end
			if i == j && i < len(s) && word == "" {
				i++ // no progress: never loop
			}
		}
	}
	return out
}
