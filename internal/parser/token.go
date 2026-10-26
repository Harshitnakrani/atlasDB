package parser

type TokenType int

const (
	TokenEOF TokenType = iota
	TokenIllegal

	// Identifiers and literals
	TokenIdent
	TokenIntLit
	TokenFloatLit
	TokenStringLit
	TokenTrue
	TokenFalse
	TokenNull

	// Operators
	TokenEq  // =
	TokenNeq // != or <>
	TokenLt  // <
	TokenLte // <=
	TokenGt  // >
	TokenGte // >=

	// Punctuation
	TokenComma     // ,
	TokenSemicolon // ;
	TokenLParen    // (
	TokenRParen    // )
	TokenStar      // *
	TokenDot       // .

	// Core DDL/DML Keywords
	TokenCreate
	TokenTable
	TokenIndex
	TokenOn
	TokenInsert
	TokenInto
	TokenValues
	TokenSelect
	TokenFrom
	TokenWhere
	TokenOrder
	TokenBy
	TokenAsc
	TokenDesc
	TokenLimit
	TokenOffset
	TokenUpdate
	TokenSet
	TokenDelete
	TokenBegin
	TokenCommit
	TokenRollback
	TokenExplain
	TokenTrace

	// Additional MySQL-compatible Keywords
	TokenDrop
	TokenTruncate
	TokenAlter
	TokenAdd
	TokenColumn

	// Join Keywords
	TokenJoin
	TokenInner
	TokenLeft
	TokenRight
	TokenCross

	// Aggregation & Grouping
	TokenGroup
	TokenHaving
	TokenDistinct
	TokenCount
	TokenSum
	TokenAvg
	TokenMin
	TokenMax

	// Extended Predicates
	TokenLike
	TokenIn
	TokenBetween
	TokenIs
	TokenNot

	// Types & Constraints
	TokenInteger
	TokenReal
	TokenText
	TokenBoolean
	TokenBlob
	TokenPrimary
	TokenKey
	TokenAnd
	TokenOr
)

type Token struct {
	Type    TokenType
	Literal string
	Pos     int
}
