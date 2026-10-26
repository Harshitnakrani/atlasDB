package parser

import (
	"strings"
	"unicode"
)

type Lexer struct {
	input string
	pos   int
}

func NewLexer(input string) *Lexer {
	return &Lexer{input: input, pos: 0}
}

func (l *Lexer) NextToken() Token {
	l.skipWhitespace()

	if l.pos >= len(l.input) {
		return Token{Type: TokenEOF, Literal: "", Pos: l.pos}
	}

	start := l.pos
	ch := l.input[l.pos]

	switch ch {
	case ',':
		l.pos++
		return Token{Type: TokenComma, Literal: ",", Pos: start}
	case ';':
		l.pos++
		return Token{Type: TokenSemicolon, Literal: ";", Pos: start}
	case '.':
		l.pos++
		return Token{Type: TokenDot, Literal: ".", Pos: start}
	case '(':
		l.pos++
		return Token{Type: TokenLParen, Literal: "(", Pos: start}
	case ')':
		l.pos++
		return Token{Type: TokenRParen, Literal: ")", Pos: start}
	case '*':
		l.pos++
		return Token{Type: TokenStar, Literal: "*", Pos: start}
	case '=':
		l.pos++
		return Token{Type: TokenEq, Literal: "=", Pos: start}
	case '!':
		if l.peek() == '=' {
			l.pos += 2
			return Token{Type: TokenNeq, Literal: "!=", Pos: start}
		}
		l.pos++
		return Token{Type: TokenIllegal, Literal: "!", Pos: start}
	case '<':
		if l.peek() == '=' {
			l.pos += 2
			return Token{Type: TokenLte, Literal: "<=", Pos: start}
		} else if l.peek() == '>' {
			l.pos += 2
			return Token{Type: TokenNeq, Literal: "<>", Pos: start}
		}
		l.pos++
		return Token{Type: TokenLt, Literal: "<", Pos: start}
	case '>':
		if l.peek() == '=' {
			l.pos += 2
			return Token{Type: TokenGte, Literal: ">=", Pos: start}
		}
		l.pos++
		return Token{Type: TokenGt, Literal: ">", Pos: start}
	case '\'', '"', '`':
		return l.readString(ch)
	default:
		if isDigit(ch) || (ch == '-' && isDigit(l.peek())) {
			return l.readNumber()
		}
		if isIdentStart(ch) {
			return l.readIdentOrKeyword()
		}
		l.pos++
		return Token{Type: TokenIllegal, Literal: string(ch), Pos: start}
	}
}

func (l *Lexer) peek() byte {
	if l.pos+1 >= len(l.input) {
		return 0
	}
	return l.input[l.pos+1]
}

func (l *Lexer) skipWhitespace() {
	for l.pos < len(l.input) && unicode.IsSpace(rune(l.input[l.pos])) {
		l.pos++
	}
}

func (l *Lexer) readString(quote byte) Token {
	start := l.pos
	l.pos++ // skip opening quote
	valStart := l.pos
	for l.pos < len(l.input) && l.input[l.pos] != quote {
		l.pos++
	}
	val := l.input[valStart:l.pos]
	if l.pos < len(l.input) && l.input[l.pos] == quote {
		l.pos++ // skip closing quote
	}
	if quote == '`' {
		return Token{Type: TokenIdent, Literal: val, Pos: start}
	}
	return Token{Type: TokenStringLit, Literal: val, Pos: start}
}

func (l *Lexer) readNumber() Token {
	start := l.pos
	isFloat := false
	if l.input[l.pos] == '-' {
		l.pos++
	}
	for l.pos < len(l.input) && (isDigit(l.input[l.pos]) || l.input[l.pos] == '.') {
		if l.input[l.pos] == '.' {
			isFloat = true
		}
		l.pos++
	}
	lit := l.input[start:l.pos]
	if isFloat {
		return Token{Type: TokenFloatLit, Literal: lit, Pos: start}
	}
	return Token{Type: TokenIntLit, Literal: lit, Pos: start}
}

func (l *Lexer) readIdentOrKeyword() Token {
	start := l.pos
	for l.pos < len(l.input) && isIdentPart(l.input[l.pos]) {
		l.pos++
	}
	lit := l.input[start:l.pos]
	upper := strings.ToUpper(lit)

	keywords := map[string]TokenType{
		"CREATE":   TokenCreate,
		"TABLE":    TokenTable,
		"INDEX":    TokenIndex,
		"ON":       TokenOn,
		"INSERT":   TokenInsert,
		"INTO":     TokenInto,
		"VALUES":   TokenValues,
		"SELECT":   TokenSelect,
		"FROM":     TokenFrom,
		"WHERE":    TokenWhere,
		"ORDER":    TokenOrder,
		"BY":       TokenBy,
		"ASC":      TokenAsc,
		"DESC":     TokenDesc,
		"LIMIT":    TokenLimit,
		"OFFSET":   TokenOffset,
		"UPDATE":   TokenUpdate,
		"SET":      TokenSet,
		"DELETE":   TokenDelete,
		"BEGIN":    TokenBegin,
		"COMMIT":   TokenCommit,
		"ROLLBACK": TokenRollback,
		"EXPLAIN":  TokenExplain,
		"TRACE":    TokenTrace,

		// MySQL Extended Keywords
		"DROP":     TokenDrop,
		"TRUNCATE": TokenTruncate,
		"ALTER":    TokenAlter,
		"ADD":      TokenAdd,
		"COLUMN":   TokenColumn,

		// Join Keywords
		"JOIN":  TokenJoin,
		"INNER": TokenInner,
		"LEFT":  TokenLeft,
		"RIGHT": TokenRight,
		"CROSS": TokenCross,

		// Aggregation & Grouping
		"GROUP":    TokenGroup,
		"HAVING":   TokenHaving,
		"DISTINCT": TokenDistinct,
		"COUNT":    TokenCount,
		"SUM":      TokenSum,
		"AVG":      TokenAvg,
		"MIN":      TokenMin,
		"MAX":      TokenMax,

		// Operators & Predicates
		"LIKE":    TokenLike,
		"IN":      TokenIn,
		"BETWEEN": TokenBetween,
		"IS":      TokenIs,
		"NOT":     TokenNot,

		// Types & Logic
		"INTEGER": TokenInteger,
		"INT":     TokenInteger,
		"BIGINT":  TokenInteger,
		"REAL":    TokenReal,
		"FLOAT":   TokenReal,
		"DOUBLE":  TokenReal,
		"TEXT":    TokenText,
		"VARCHAR": TokenText,
		"CHAR":    TokenText,
		"BOOLEAN": TokenBoolean,
		"BOOL":    TokenBoolean,
		"BLOB":    TokenBlob,
		"PRIMARY": TokenPrimary,
		"KEY":     TokenKey,
		"AND":     TokenAnd,
		"OR":      TokenOr,
		"TRUE":    TokenTrue,
		"FALSE":   TokenFalse,
		"NULL":    TokenNull,
	}

	if tokType, ok := keywords[upper]; ok {
		return Token{Type: tokType, Literal: lit, Pos: start}
	}

	return Token{Type: TokenIdent, Literal: lit, Pos: start}
}

func isDigit(ch byte) bool {
	return ch >= '0' && ch <= '9'
}

func isIdentStart(ch byte) bool {
	return (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || ch == '_'
}

func isIdentPart(ch byte) bool {
	return isIdentStart(ch) || isDigit(ch)
}
