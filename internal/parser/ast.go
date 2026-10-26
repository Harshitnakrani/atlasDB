package parser

import (
	"atlasDB/internal/common"
)

type Statement interface {
	statementNode()
}

type Expr interface {
	exprNode()
}

// Expressions
type BinaryExpr struct {
	Left  Expr
	Op    string // "=", "!=", "<", "<=", ">", ">=", "AND", "OR"
	Right Expr
}
func (e *BinaryExpr) exprNode() {}

type ColumnRef struct {
	Table string // optional (e.g. "users" in "users.id")
	Name  string
}
func (e *ColumnRef) exprNode() {}

type LiteralExpr struct {
	Val common.Value
}
func (e *LiteralExpr) exprNode() {}

type StarExpr struct {
	Table string // optional (e.g. "users.*")
}
func (e *StarExpr) exprNode() {}

type AggregateExpr struct {
	Func     string // "COUNT", "SUM", "AVG", "MIN", "MAX"
	Arg      Expr
	Distinct bool
}
func (e *AggregateExpr) exprNode() {}

type InExpr struct {
	Expr Expr
	List []Expr
	Not  bool
}
func (e *InExpr) exprNode() {}

type BetweenExpr struct {
	Expr Expr
	Low  Expr
	High Expr
	Not  bool
}
func (e *BetweenExpr) exprNode() {}

type LikeExpr struct {
	Expr    Expr
	Pattern string
	Not     bool
}
func (e *LikeExpr) exprNode() {}

type IsNullExpr struct {
	Expr Expr
	Not  bool
}
func (e *IsNullExpr) exprNode() {}

// Clauses & Joins
type JoinType int

const (
	JoinInner JoinType = iota
	JoinLeft
	JoinRight
	JoinCross
)

type JoinClause struct {
	Type  JoinType
	Table string
	On    Expr
}

type OrderByClause struct {
	Table  string
	Column string
	Desc   bool
}

type ColumnElement struct {
	Name       string
	Type       common.DataType
	PrimaryKey bool
}

type Assignment struct {
	Column string
	Value  Expr
}

// Statements
type SelectStmt struct {
	Distinct bool
	Columns  []Expr
	Table    string
	Joins    []JoinClause
	Where    Expr
	GroupBy  []Expr
	Having   Expr
	OrderBy  *OrderByClause
	Limit    int
	Offset   int
}
func (s *SelectStmt) statementNode() {}

type InsertStmt struct {
	Table   string
	Columns []string
	Values  [][]Expr
}
func (s *InsertStmt) statementNode() {}

type CreateTableStmt struct {
	Table       string
	Columns     []ColumnElement
	IfNotExists bool
}
func (s *CreateTableStmt) statementNode() {}

type CreateIndexStmt struct {
	IndexName string
	Table     string
	Column    string
	Unique    bool
}
func (s *CreateIndexStmt) statementNode() {}

type UpdateStmt struct {
	Table       string
	Assignments []Assignment
	Where       Expr
}
func (s *UpdateStmt) statementNode() {}

type DeleteStmt struct {
	Table string
	Where Expr
}
func (s *DeleteStmt) statementNode() {}

type DropTableStmt struct {
	Table    string
	IfExists bool
}
func (s *DropTableStmt) statementNode() {}

type DropIndexStmt struct {
	IndexName string
	IfExists  bool
}
func (s *DropIndexStmt) statementNode() {}

type TruncateTableStmt struct {
	Table string
}
func (s *TruncateTableStmt) statementNode() {}

type AlterTableStmt struct {
	Table     string
	AddColumn *ColumnElement
}
func (s *AlterTableStmt) statementNode() {}

type BeginStmt struct{}
func (s *BeginStmt) statementNode() {}

type CommitStmt struct{}
func (s *CommitStmt) statementNode() {}

type RollbackStmt struct{}
func (s *RollbackStmt) statementNode() {}

type ExplainStmt struct {
	Stmt Statement
}
func (s *ExplainStmt) statementNode() {}

type TraceStmt struct {
	Stmt Statement
}
func (s *TraceStmt) statementNode() {}
