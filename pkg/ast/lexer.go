// Copyright (c) luaugo contributors. Licensed under the MIT License.
// Portions derived from Luau (https://github.com/luau-lang/luau),
// Copyright (c) 2019-2026 Roblox Corporation, MIT License.
// Portions derived from Lua 5.x, Copyright (c) 1994-2019 Lua.org, PUC-Rio, MIT License.

package ast

// lexer.go is a faithful port of Luau's Ast/src/Lexer.cpp. It produces the
// same set of Lexeme types described by upstream's Lexeme::Type enum and
// follows the same tokenization rules for numbers, quoted strings, long
// brackets, backtick interpolated strings, comments, attributes, and the
// Luau-specific keywords.
//
// The lexer is zero-copy where it can be: token Value slices alias the
// source buffer. Callers must not mutate the buffer while the lexer is
// in use.

// reserved keyword table; index = TokenAnd..TokenWhile mapped to lowercase.
var reservedKeywords = map[string]TokenKind{
	"and":      TokenAnd,
	"break":    TokenBreak,
	"do":       TokenDo,
	"else":     TokenElse,
	"elseif":   TokenElseif,
	"end":      TokenEnd,
	"false":    TokenFalse,
	"for":      TokenFor,
	"function": TokenFunction,
	"if":       TokenIf,
	"in":       TokenIn,
	"local":    TokenLocal,
	"nil":      TokenNil,
	"not":      TokenNot,
	"or":       TokenOr,
	"repeat":   TokenRepeat,
	"return":   TokenReturn,
	"then":     TokenThen,
	"true":     TokenTrue,
	"until":    TokenUntil,
	"while":    TokenWhile,
}

type braceType uint8

const (
	braceNormal braceType = iota
	braceInterpString
)

type luauLexer struct {
	source []byte
	off    int
	line   uint32
	// lineOffset is the offset at which the current line starts; Column = off - lineOffset.
	// We use a signed int to allow representing negative starting offsets when (rare) start
	// positions are used; here we always start at 0 so int is fine.
	lineOffset int

	cur  Token
	prev Location

	skipComments bool
	readNames    bool

	braceStack []braceType
}

func newLexer(_ string, source []byte) Lexer {
	l := &luauLexer{
		source:       source,
		off:          0,
		line:         0,
		lineOffset:   0,
		readNames:    true,
		skipComments: false,
	}
	// Prime: cur is left as zero-valued so Peek before Next returns EOF.
	// The parser calls Next() first.
	l.cur = Token{Kind: TokenEof, Location: Location{Begin: l.position(), End: l.position()}}
	return l
}

// ----------------------------------------------------------------------
// Interface methods
// ----------------------------------------------------------------------

func (l *luauLexer) Next() Token {
	// Skip whitespace
	for {
		for l.off < len(l.source) && isSpaceByte(l.source[l.off]) {
			l.consumeAny()
		}
		l.prev = l.cur.Location
		tok := l.readNext()
		l.cur = tok
		if l.skipComments && (tok.Kind == TokenComment || tok.Kind == TokenBlockComment) {
			continue
		}
		return tok
	}
}

func (l *luauLexer) Peek() Token {
	return l.cur
}

func (l *luauLexer) Done() bool {
	return l.cur.Kind == TokenEof
}

// lookahead returns the next token without consuming the current one or
// permanently advancing offsets. It is used by the parser via lookahead().
func (l *luauLexer) lookahead() Token {
	savedOff := l.off
	savedLine := l.line
	savedLineOffset := l.lineOffset
	savedCur := l.cur
	savedPrev := l.prev
	savedDepth := len(l.braceStack)
	var topBrace braceType
	if savedDepth > 0 {
		topBrace = l.braceStack[savedDepth-1]
	}

	tok := l.Next()

	l.off = savedOff
	l.line = savedLine
	l.lineOffset = savedLineOffset
	l.cur = savedCur
	l.prev = savedPrev
	// Restore brace stack
	if len(l.braceStack) < savedDepth {
		l.braceStack = append(l.braceStack, topBrace)
	} else if len(l.braceStack) > savedDepth {
		l.braceStack = l.braceStack[:savedDepth]
	}

	return tok
}

// ----------------------------------------------------------------------
// Helpers
// ----------------------------------------------------------------------

func isSpaceByte(b byte) bool {
	return b == ' ' || b == '\t' || b == '\r' || b == '\n' || b == '\v' || b == '\f'
}

func isAlphaByte(b byte) bool {
	return (b|' ')-'a' < 26
}

func isDigitByte(b byte) bool {
	return b-'0' < 10
}

func isHexDigitByte(b byte) bool {
	if isDigitByte(b) {
		return true
	}
	lc := b | ' '
	return lc-'a' < 6
}

func (l *luauLexer) peekch() byte {
	if l.off < len(l.source) {
		return l.source[l.off]
	}
	return 0
}

func (l *luauLexer) peekchAt(n int) byte {
	if l.off+n < len(l.source) {
		return l.source[l.off+n]
	}
	return 0
}

func (l *luauLexer) position() Position {
	return Position{Line: l.line, Column: uint32(l.off - l.lineOffset)}
}

func (l *luauLexer) consume() {
	// Assumes not a newline; matches upstream's `consume()`.
	l.off++
}

func (l *luauLexer) consumeAny() {
	if l.off < len(l.source) && l.source[l.off] == '\n' {
		l.line++
		l.lineOffset = l.off + 1
	}
	l.off++
}

// ----------------------------------------------------------------------
// readNext - main dispatch
// ----------------------------------------------------------------------

func (l *luauLexer) readNext() Token {
	start := l.position()

	if l.off >= len(l.source) {
		return Token{Kind: TokenEof, Location: Location{Begin: start, End: start}}
	}

	ch := l.source[l.off]
	switch ch {
	case '-':
		next := l.peekchAt(1)
		if next == '>' {
			l.consume()
			l.consume()
			return Token{Kind: TokenSkinnyArrow, Location: Location{Begin: start, End: l.position()}}
		}
		if next == '=' {
			l.consume()
			l.consume()
			return Token{Kind: TokenSubAssign, Location: Location{Begin: start, End: l.position()}}
		}
		if next == '-' {
			return l.readCommentBody(start)
		}
		l.consume()
		return Token{Kind: TokenKind('-'), Location: Location{Begin: start, End: l.position()}}

	case '[':
		sep := l.skipLongSeparator()
		if sep >= 0 {
			return l.readLongString(start, sep, TokenRawString, TokenBrokenString)
		}
		if sep == -1 {
			return Token{Kind: TokenKind('['), Location: Location{Begin: start, End: l.position()}}
		}
		return Token{Kind: TokenBrokenString, Location: Location{Begin: start, End: l.position()}}

	case '{':
		l.consume()
		if len(l.braceStack) > 0 {
			l.braceStack = append(l.braceStack, braceNormal)
		}
		return Token{Kind: TokenKind('{'), Location: Location{Begin: start, End: l.position()}}

	case '}':
		l.consume()
		if len(l.braceStack) == 0 {
			return Token{Kind: TokenKind('}'), Location: Location{Begin: start, End: l.position()}}
		}
		top := l.braceStack[len(l.braceStack)-1]
		l.braceStack = l.braceStack[:len(l.braceStack)-1]
		if top != braceInterpString {
			return Token{Kind: TokenKind('}'), Location: Location{Begin: start, End: l.position()}}
		}
		return l.readInterpolatedStringSection(start, TokenInterpStringMid, TokenInterpStringEnd)

	case '=':
		l.consume()
		if l.peekch() == '=' {
			l.consume()
			return Token{Kind: TokenEqual, Location: Location{Begin: start, End: l.position()}}
		}
		return Token{Kind: TokenKind('='), Location: Location{Begin: start, End: l.position()}}

	case '<':
		l.consume()
		if l.peekch() == '=' {
			l.consume()
			return Token{Kind: TokenLessEqual, Location: Location{Begin: start, End: l.position()}}
		}
		return Token{Kind: TokenKind('<'), Location: Location{Begin: start, End: l.position()}}

	case '>':
		l.consume()
		if l.peekch() == '=' {
			l.consume()
			return Token{Kind: TokenGreaterEqual, Location: Location{Begin: start, End: l.position()}}
		}
		return Token{Kind: TokenKind('>'), Location: Location{Begin: start, End: l.position()}}

	case '~':
		l.consume()
		if l.peekch() == '=' {
			l.consume()
			return Token{Kind: TokenNotEqual, Location: Location{Begin: start, End: l.position()}}
		}
		return Token{Kind: TokenKind('~'), Location: Location{Begin: start, End: l.position()}}

	case '"', '\'':
		return l.readQuotedString()

	case '`':
		return l.readInterpolatedStringBegin()

	case '.':
		l.consume()
		if l.peekch() == '.' {
			l.consume()
			if l.peekch() == '.' {
				l.consume()
				return Token{Kind: TokenDot3, Location: Location{Begin: start, End: l.position()}}
			}
			if l.peekch() == '=' {
				l.consume()
				return Token{Kind: TokenConcatAssign, Location: Location{Begin: start, End: l.position()}}
			}
			return Token{Kind: TokenDot2, Location: Location{Begin: start, End: l.position()}}
		}
		if isDigitByte(l.peekch()) {
			return l.readNumber(start, l.off-1)
		}
		return Token{Kind: TokenKind('.'), Location: Location{Begin: start, End: l.position()}}

	case '+':
		l.consume()
		if l.peekch() == '=' {
			l.consume()
			return Token{Kind: TokenAddAssign, Location: Location{Begin: start, End: l.position()}}
		}
		return Token{Kind: TokenKind('+'), Location: Location{Begin: start, End: l.position()}}

	case '/':
		l.consume()
		c := l.peekch()
		if c == '=' {
			l.consume()
			return Token{Kind: TokenDivAssign, Location: Location{Begin: start, End: l.position()}}
		}
		if c == '/' {
			l.consume()
			if l.peekch() == '=' {
				l.consume()
				return Token{Kind: TokenFloorDivAssign, Location: Location{Begin: start, End: l.position()}}
			}
			return Token{Kind: TokenFloorDiv, Location: Location{Begin: start, End: l.position()}}
		}
		return Token{Kind: TokenKind('/'), Location: Location{Begin: start, End: l.position()}}

	case '*':
		l.consume()
		if l.peekch() == '=' {
			l.consume()
			return Token{Kind: TokenMulAssign, Location: Location{Begin: start, End: l.position()}}
		}
		return Token{Kind: TokenKind('*'), Location: Location{Begin: start, End: l.position()}}

	case '%':
		l.consume()
		if l.peekch() == '=' {
			l.consume()
			return Token{Kind: TokenModAssign, Location: Location{Begin: start, End: l.position()}}
		}
		return Token{Kind: TokenKind('%'), Location: Location{Begin: start, End: l.position()}}

	case '^':
		l.consume()
		if l.peekch() == '=' {
			l.consume()
			return Token{Kind: TokenPowAssign, Location: Location{Begin: start, End: l.position()}}
		}
		return Token{Kind: TokenKind('^'), Location: Location{Begin: start, End: l.position()}}

	case ':':
		l.consume()
		if l.peekch() == ':' {
			l.consume()
			return Token{Kind: TokenDoubleColon, Location: Location{Begin: start, End: l.position()}}
		}
		return Token{Kind: TokenKind(':'), Location: Location{Begin: start, End: l.position()}}

	case '(', ')', ']', ';', ',', '#', '?', '&', '|':
		l.consume()
		return Token{Kind: TokenKind(ch), Location: Location{Begin: start, End: l.position()}}

	case '@':
		if l.peekchAt(1) == '[' {
			l.consume()
			l.consume()
			return Token{Kind: TokenAttributeOpen, Location: Location{Begin: start, End: l.position()}}
		}
		l.consume() // @
		if isAlphaByte(l.peekch()) || l.peekch() == '_' {
			nameStart := l.off
			for l.off < len(l.source) {
				c := l.source[l.off]
				if isAlphaByte(c) || isDigitByte(c) || c == '_' {
					l.consume()
				} else {
					break
				}
			}
			return Token{
				Kind:     TokenAttribute,
				Location: Location{Begin: start, End: l.position()},
				Value:    l.source[nameStart:l.off],
			}
		}
		return Token{Kind: TokenAttribute, Location: Location{Begin: start, End: l.position()}, Value: nil}

	default:
		if isDigitByte(ch) {
			return l.readNumber(start, l.off)
		}
		if isAlphaByte(ch) || ch == '_' {
			return l.readName(start)
		}
		if ch&0x80 != 0 {
			return l.readUtf8Error(start)
		}
		l.consume()
		return Token{Kind: TokenKind(ch), Location: Location{Begin: start, End: l.position()}}
	}
}

// ----------------------------------------------------------------------
// readCommentBody, skipLongSeparator, readLongString
// ----------------------------------------------------------------------

func (l *luauLexer) readCommentBody(start Position) Token {
	// We've seen "--"
	l.consume()
	l.consume()

	startOff := l.off

	if l.peekch() == '[' {
		sep := l.skipLongSeparator()
		if sep >= 0 {
			return l.readLongString(start, sep, TokenBlockComment, TokenBrokenComment)
		}
	}

	for l.off < len(l.source) {
		c := l.source[l.off]
		if c == 0 || c == '\r' || c == '\n' {
			break
		}
		l.consume()
	}

	return Token{
		Kind:     TokenComment,
		Location: Location{Begin: start, End: l.position()},
		Value:    l.source[startOff:l.off],
	}
}

// skipLongSeparator returns the number of '=' between brackets, or
// negative if the prefix doesn't match a long separator.
func (l *luauLexer) skipLongSeparator() int {
	startCh := l.peekch()
	// Caller guarantees this is '[' or ']'.
	l.consume()
	count := 0
	for l.peekch() == '=' {
		l.consume()
		count++
	}
	if l.peekch() == startCh {
		return count
	}
	return -count - 1
}

func (l *luauLexer) readLongString(start Position, sep int, okKind, brokenKind TokenKind) Token {
	// We've just consumed the leading [==[  up to (and including) the
	// second '['. Confirm we're on it and consume it.
	if l.peekch() != '[' {
		// Defensive: should not happen.
	} else {
		l.consume()
	}

	startOff := l.off

	for l.off < len(l.source) {
		c := l.source[l.off]
		if c == ']' {
			endCandidate := l.off
			if l.skipLongSeparator() == sep {
				if l.peekch() == ']' {
					l.consume()
					endOff := l.off - sep - 2
					if endOff < startOff {
						endOff = startOff
					}
					return Token{
						Kind:       okKind,
						Location:   Location{Begin: start, End: l.position()},
						Value:      l.source[startOff:endOff],
						BlockDepth: uint32(sep),
					}
				}
			}
			// not matching — but skipLongSeparator consumed some chars
			_ = endCandidate
		} else {
			l.consumeAny()
		}
	}

	return Token{Kind: brokenKind, Location: Location{Begin: start, End: l.position()}}
}

// ----------------------------------------------------------------------
// readQuotedString and friends
// ----------------------------------------------------------------------

func (l *luauLexer) readBackslashInString() {
	// peekch() == '\\'
	l.consume()
	c := l.peekch()
	switch c {
	case '\r':
		l.consume()
		if l.peekch() == '\n' {
			l.consumeAny()
		}
	case 0:
		// nothing
	case 'z':
		l.consume()
		for isSpaceByte(l.peekch()) {
			l.consumeAny()
		}
	default:
		l.consumeAny()
	}
}

func (l *luauLexer) readQuotedString() Token {
	start := l.position()
	delim := l.peekch()
	l.consume()
	startOff := l.off

	for l.off < len(l.source) {
		c := l.source[l.off]
		if c == delim {
			break
		}
		switch c {
		case 0, '\r', '\n':
			return Token{Kind: TokenBrokenString, Location: Location{Begin: start, End: l.position()}}
		case '\\':
			l.readBackslashInString()
		default:
			l.consume()
		}
	}

	if l.off >= len(l.source) || l.source[l.off] != delim {
		return Token{Kind: TokenBrokenString, Location: Location{Begin: start, End: l.position()}}
	}
	l.consume() // closing quote
	q := QuoteDouble
	if delim == '\'' {
		q = QuoteSingle
	}
	return Token{
		Kind:     TokenQuotedString,
		Location: Location{Begin: start, End: l.position()},
		Value:    l.source[startOff : l.off-1],
		Quote:    q,
	}
}

// ----------------------------------------------------------------------
// Interpolated strings
// ----------------------------------------------------------------------

func (l *luauLexer) readInterpolatedStringBegin() Token {
	start := l.position()
	l.consume() // `
	return l.readInterpolatedStringSection(start, TokenInterpStringBegin, TokenInterpStringSimple)
}

func (l *luauLexer) readInterpolatedStringSection(start Position, formatKind, endKind TokenKind) Token {
	startOff := l.off

	for l.off < len(l.source) {
		c := l.source[l.off]
		if c == '`' {
			break
		}
		switch c {
		case 0, '\r', '\n':
			return Token{Kind: TokenBrokenString, Location: Location{Begin: start, End: l.position()}}
		case '\\':
			// Allow for \u{}, otherwise consumed by looking for {
			if l.peekchAt(1) == 'u' && l.peekchAt(2) == '{' {
				l.consume() // \\
				l.consume() // u
				l.consume() // {
			} else {
				l.readBackslashInString()
			}
		case '{':
			l.braceStack = append(l.braceStack, braceInterpString)
			if l.peekchAt(1) == '{' {
				tok := Token{
					Kind:     TokenBrokenInterpDoubleBrace,
					Location: Location{Begin: start, End: l.position()},
					Value:    l.source[startOff:l.off],
				}
				l.consume()
				l.consume()
				return tok
			}
			l.consume()
			return Token{
				Kind:     formatKind,
				Location: Location{Begin: start, End: l.position()},
				Value:    l.source[startOff : l.off-1],
			}
		default:
			l.consume()
		}
	}

	if l.off >= len(l.source) || l.source[l.off] != '`' {
		return Token{Kind: TokenBrokenString, Location: Location{Begin: start, End: l.position()}}
	}
	l.consume()
	return Token{
		Kind:     endKind,
		Location: Location{Begin: start, End: l.position()},
		Value:    l.source[startOff : l.off-1],
	}
}

// ----------------------------------------------------------------------
// Numbers and names
// ----------------------------------------------------------------------

func (l *luauLexer) readNumber(start Position, startOff int) Token {
	// already on a digit
	for l.off < len(l.source) {
		c := l.source[l.off]
		if isDigitByte(c) || c == '.' || c == '_' {
			l.consume()
		} else {
			break
		}
	}
	if c := l.peekch(); c == 'e' || c == 'E' {
		l.consume()
		if d := l.peekch(); d == '+' || d == '-' {
			l.consume()
		}
	}
	for l.off < len(l.source) {
		c := l.source[l.off]
		if isAlphaByte(c) || isDigitByte(c) || c == '_' {
			l.consume()
		} else {
			break
		}
	}

	return Token{
		Kind:     TokenNumber,
		Location: Location{Begin: start, End: l.position()},
		Value:    l.source[startOff:l.off],
	}
}

func (l *luauLexer) readName(start Position) Token {
	startOff := l.off
	for l.off < len(l.source) {
		c := l.source[l.off]
		if isAlphaByte(c) || isDigitByte(c) || c == '_' {
			l.consume()
		} else {
			break
		}
	}
	name := l.source[startOff:l.off]
	kind := TokenName
	if l.readNames {
		if kw, ok := reservedKeywords[string(name)]; ok {
			kind = kw
		}
	}
	return Token{
		Kind:     kind,
		Location: Location{Begin: start, End: l.position()},
		Value:    name,
	}
}

// ----------------------------------------------------------------------
// UTF-8 error path (matches upstream readUtf8Error for diagnostic purposes)
// ----------------------------------------------------------------------

func (l *luauLexer) readUtf8Error(start Position) Token {
	b := l.peekch()
	var size int
	switch {
	case b&0x80 == 0:
		size = 1
	case b&0xE0 == 0xC0:
		size = 2
	case b&0xF0 == 0xE0:
		size = 3
	case b&0xF8 == 0xF0:
		size = 4
	default:
		l.consume()
		return Token{Kind: TokenBrokenUnicode, Location: Location{Begin: start, End: l.position()}}
	}
	l.consume()
	for i := 1; i < size; i++ {
		if l.peekch()&0xC0 != 0x80 {
			return Token{Kind: TokenBrokenUnicode, Location: Location{Begin: start, End: l.position()}}
		}
		l.consume()
	}
	return Token{Kind: TokenBrokenUnicode, Location: Location{Begin: start, End: l.position()}}
}

// ----------------------------------------------------------------------
// fixupQuotedString and fixupMultilineString (port of static helpers)
// ----------------------------------------------------------------------

// fixupQuotedString decodes Lua escape sequences in src into a freshly
// allocated byte slice. Returns nil, false if src contains a malformed
// escape.
func fixupQuotedString(src []byte) ([]byte, bool) {
	// Fast path: no backslashes -> return a copy
	hasEsc := false
	for _, b := range src {
		if b == '\\' {
			hasEsc = true
			break
		}
	}
	if !hasEsc {
		out := make([]byte, len(src))
		copy(out, src)
		return out, true
	}
	out := make([]byte, 0, len(src))
	i := 0
	for i < len(src) {
		c := src[i]
		if c != '\\' {
			out = append(out, c)
			i++
			continue
		}
		if i+1 >= len(src) {
			return nil, false
		}
		esc := src[i+1]
		i += 2
		switch esc {
		case '\n':
			out = append(out, '\n')
		case '\r':
			out = append(out, '\n')
			if i < len(src) && src[i] == '\n' {
				i++
			}
		case 0:
			return nil, false
		case 'x':
			if i+2 > len(src) {
				return nil, false
			}
			var code int
			for j := 0; j < 2; j++ {
				ch := src[i+j]
				if !isHexDigitByte(ch) {
					return nil, false
				}
				var v int
				if isDigitByte(ch) {
					v = int(ch - '0')
				} else {
					v = int((ch | ' ') - 'a' + 10)
				}
				code = code*16 + v
			}
			out = append(out, byte(code))
			i += 2
		case 'z':
			for i < len(src) && isSpaceByte(src[i]) {
				i++
			}
		case 'u':
			if i+3 > len(src) {
				return nil, false
			}
			if src[i] != '{' {
				return nil, false
			}
			i++
			if src[i] == '}' {
				return nil, false
			}
			code := 0
			for j := 0; j < 16; j++ {
				if i == len(src) {
					return nil, false
				}
				ch := src[i]
				if ch == '}' {
					break
				}
				if !isHexDigitByte(ch) {
					return nil, false
				}
				var v int
				if isDigitByte(ch) {
					v = int(ch - '0')
				} else {
					v = int((ch | ' ') - 'a' + 10)
				}
				code = code*16 + v
				i++
			}
			if i == len(src) || src[i] != '}' {
				return nil, false
			}
			i++
			buf := [4]byte{}
			n := encodeUtf8(buf[:], code)
			if n == 0 {
				return nil, false
			}
			out = append(out, buf[:n]...)
		default:
			if isDigitByte(esc) {
				code := int(esc - '0')
				for j := 0; j < 2; j++ {
					if i == len(src) || !isDigitByte(src[i]) {
						break
					}
					code = code*10 + int(src[i]-'0')
					i++
				}
				if code > 255 {
					return nil, false
				}
				out = append(out, byte(code))
			} else {
				out = append(out, unescape(esc))
			}
		}
	}
	return out, true
}

func unescape(c byte) byte {
	switch c {
	case 'a':
		return '\a'
	case 'b':
		return '\b'
	case 'f':
		return '\f'
	case 'n':
		return '\n'
	case 'r':
		return '\r'
	case 't':
		return '\t'
	case 'v':
		return '\v'
	default:
		return c
	}
}

func encodeUtf8(dst []byte, code int) int {
	switch {
	case code < 0x80:
		dst[0] = byte(code)
		return 1
	case code < 0x800:
		dst[0] = byte(0xC0 | code>>6)
		dst[1] = byte(0x80 | code&0x3F)
		return 2
	case code < 0x10000:
		dst[0] = byte(0xE0 | code>>12)
		dst[1] = byte(0x80 | (code>>6)&0x3F)
		dst[2] = byte(0x80 | code&0x3F)
		return 3
	case code < 0x110000:
		dst[0] = byte(0xF0 | code>>18)
		dst[1] = byte(0x80 | (code>>12)&0x3F)
		dst[2] = byte(0x80 | (code>>6)&0x3F)
		dst[3] = byte(0x80 | code&0x3F)
		return 4
	}
	return 0
}

// fixupMultilineString normalises newlines in a long string per upstream rules.
func fixupMultilineString(src []byte) []byte {
	if len(src) == 0 {
		return src
	}
	out := make([]byte, 0, len(src))
	i := 0
	// skip leading newline
	if i+1 < len(src) && src[i] == '\r' && src[i+1] == '\n' {
		i += 2
	} else if src[i] == '\n' {
		i++
	}
	for i < len(src) {
		if i+1 < len(src) && src[i] == '\r' && src[i+1] == '\n' {
			out = append(out, '\n')
			i += 2
		} else {
			out = append(out, src[i])
			i++
		}
	}
	return out
}
