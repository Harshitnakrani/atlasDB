package parser

import (
	"fmt"
	"strconv"
	"strings"

	"atlasDB/internal/common"
)

type Parser struct {
	lexer     *Lexer
	currToken Token
	peekToken Token
}

func NewParser(input string) *Parser {
	l := NewLexer(input)
	p := &Parser{lexer: l}
	p.nextToken()
	p.nextToken()
	return p
}

func (p *Parser) nextToken() {
	p.currToken = p.peekToken
	p.peekToken = p.lexer.NextToken()
}

func (p *Parser) expect(t TokenType) error {
	if p.currToken.Type != t {
		return fmt.Errorf("%w: expected token %v, got %v (%q) at pos %d",
			common.ErrInvalidSQL, t, p.currToken.Type, p.currToken.Literal, p.currToken.Pos)
	}
	p.nextToken()
	return nil
}

// Parse converts SQL string into a Statement AST node.
func (p *Parser) Parse() (Statement, error) {
	if p.currToken.Type == TokenEOF {
		return nil, fmt.Errorf("%w: empty query", common.ErrInvalidSQL)
	}

	switch p.currToken.Type {
	case TokenCreate:
		return p.parseCreate()
	case TokenDrop:
		return p.parseDrop()
	case TokenTruncate:
		return p.parseTruncate()
	case TokenAlter:
		return p.parseAlter()
	case TokenInsert:
		return p.parseInsert()
	case TokenSelect:
		return p.parseSelect()
	case TokenUpdate:
		return p.parseUpdate()
	case TokenDelete:
		return p.parseDelete()
	case TokenBegin:
		p.nextToken()
		return &BeginStmt{}, nil
	case TokenCommit:
		p.nextToken()
		return &CommitStmt{}, nil
	case TokenRollback:
		p.nextToken()
		return &RollbackStmt{}, nil
	case TokenExplain:
		p.nextToken()
		stmt, err := p.Parse()
		if err != nil {
			return nil, err
		}
		return &ExplainStmt{Stmt: stmt}, nil
	case TokenTrace:
		p.nextToken()
		stmt, err := p.Parse()
		if err != nil {
			return nil, err
		}
		return &TraceStmt{Stmt: stmt}, nil
	default:
		return nil, fmt.Errorf("%w: unexpected starting token %q", common.ErrInvalidSQL, p.currToken.Literal)
	}
}

func (p *Parser) parseCreate() (Statement, error) {
	p.nextToken() // consume CREATE
	switch p.currToken.Type {
	case TokenTable:
		return p.parseCreateTable()
	case TokenIndex:
		return p.parseCreateIndex()
	default:
		return nil, fmt.Errorf("%w: expected TABLE or INDEX after CREATE", common.ErrInvalidSQL)
	}
}

func (p *Parser) parseCreateTable() (Statement, error) {
	p.nextToken() // consume TABLE

	if p.currToken.Type != TokenIdent {
		return nil, fmt.Errorf("%w: expected table name", common.ErrInvalidSQL)
	}
	tableName := p.currToken.Literal
	p.nextToken()

	if err := p.expect(TokenLParen); err != nil {
		return nil, err
	}

	var columns []ColumnElement
	for p.currToken.Type != TokenRParen && p.currToken.Type != TokenEOF {
		if p.currToken.Type != TokenIdent {
			return nil, fmt.Errorf("%w: expected column name", common.ErrInvalidSQL)
		}
		colName := p.currToken.Literal
		p.nextToken()

		var colType common.DataType
		switch p.currToken.Type {
		case TokenInteger:
			colType = common.TypeInt
		case TokenReal:
			colType = common.TypeFloat
		case TokenText:
			colType = common.TypeText
		case TokenBoolean:
			colType = common.TypeBool
		case TokenBlob:
			colType = common.TypeBlob
		default:
			return nil, fmt.Errorf("%w: invalid column type %s", common.ErrInvalidSQL, p.currToken.Literal)
		}
		p.nextToken()

		isPK := false
		if p.currToken.Type == TokenPrimary {
			p.nextToken()
			if p.currToken.Type == TokenKey {
				p.nextToken()
				isPK = true
			}
		}

		columns = append(columns, ColumnElement{
			Name:       colName,
			Type:       colType,
			PrimaryKey: isPK,
		})

		if p.currToken.Type == TokenComma {
			p.nextToken()
		} else {
			break
		}
	}

	if err := p.expect(TokenRParen); err != nil {
		return nil, err
	}

	return &CreateTableStmt{
		Table:   tableName,
		Columns: columns,
	}, nil
}

func (p *Parser) parseCreateIndex() (Statement, error) {
	p.nextToken() // consume INDEX

	if p.currToken.Type != TokenIdent {
		return nil, fmt.Errorf("%w: expected index name", common.ErrInvalidSQL)
	}
	indexName := p.currToken.Literal
	p.nextToken()

	if err := p.expect(TokenOn); err != nil {
		return nil, err
	}

	if p.currToken.Type != TokenIdent {
		return nil, fmt.Errorf("%w: expected table name in CREATE INDEX", common.ErrInvalidSQL)
	}
	tableName := p.currToken.Literal
	p.nextToken()

	if err := p.expect(TokenLParen); err != nil {
		return nil, err
	}

	if p.currToken.Type != TokenIdent {
		return nil, fmt.Errorf("%w: expected column name in CREATE INDEX", common.ErrInvalidSQL)
	}
	colName := p.currToken.Literal
	p.nextToken()

	if err := p.expect(TokenRParen); err != nil {
		return nil, err
	}

	return &CreateIndexStmt{
		IndexName: indexName,
		Table:     tableName,
		Column:    colName,
		Unique:    false,
	}, nil
}

func (p *Parser) parseDrop() (Statement, error) {
	p.nextToken() // consume DROP
	switch p.currToken.Type {
	case TokenTable:
		p.nextToken()
		if p.currToken.Type != TokenIdent {
			return nil, fmt.Errorf("%w: expected table name in DROP TABLE", common.ErrInvalidSQL)
		}
		tableName := p.currToken.Literal
		p.nextToken()
		return &DropTableStmt{Table: tableName}, nil

	case TokenIndex:
		p.nextToken()
		if p.currToken.Type != TokenIdent {
			return nil, fmt.Errorf("%w: expected index name in DROP INDEX", common.ErrInvalidSQL)
		}
		indexName := p.currToken.Literal
		p.nextToken()
		return &DropIndexStmt{IndexName: indexName}, nil

	default:
		return nil, fmt.Errorf("%w: expected TABLE or INDEX after DROP", common.ErrInvalidSQL)
	}
}

func (p *Parser) parseTruncate() (Statement, error) {
	p.nextToken() // consume TRUNCATE
	if p.currToken.Type == TokenTable {
		p.nextToken()
	}
	if p.currToken.Type != TokenIdent {
		return nil, fmt.Errorf("%w: expected table name in TRUNCATE TABLE", common.ErrInvalidSQL)
	}
	tableName := p.currToken.Literal
	p.nextToken()
	return &TruncateTableStmt{Table: tableName}, nil
}

func (p *Parser) parseAlter() (Statement, error) {
	p.nextToken() // consume ALTER
	if err := p.expect(TokenTable); err != nil {
		return nil, err
	}
	if p.currToken.Type != TokenIdent {
		return nil, fmt.Errorf("%w: expected table name in ALTER TABLE", common.ErrInvalidSQL)
	}
	tableName := p.currToken.Literal
	p.nextToken()

	if err := p.expect(TokenAdd); err != nil {
		return nil, err
	}
	if p.currToken.Type == TokenColumn {
		p.nextToken()
	}

	if p.currToken.Type != TokenIdent {
		return nil, fmt.Errorf("%w: expected column name to add", common.ErrInvalidSQL)
	}
	colName := p.currToken.Literal
	p.nextToken()

	var colType common.DataType
	switch p.currToken.Type {
	case TokenInteger:
		colType = common.TypeInt
	case TokenReal:
		colType = common.TypeFloat
	case TokenText:
		colType = common.TypeText
	case TokenBoolean:
		colType = common.TypeBool
	case TokenBlob:
		colType = common.TypeBlob
	default:
		return nil, fmt.Errorf("%w: invalid column type in ALTER TABLE", common.ErrInvalidSQL)
	}
	p.nextToken()

	return &AlterTableStmt{
		Table: tableName,
		AddColumn: &ColumnElement{
			Name: colName,
			Type: colType,
		},
	}, nil
}

func (p *Parser) parseInsert() (Statement, error) {
	p.nextToken() // consume INSERT
	if err := p.expect(TokenInto); err != nil {
		return nil, err
	}

	if p.currToken.Type != TokenIdent {
		return nil, fmt.Errorf("%w: expected table name", common.ErrInvalidSQL)
	}
	tableName := p.currToken.Literal
	p.nextToken()

	var cols []string
	if p.currToken.Type == TokenLParen {
		p.nextToken()
		for p.currToken.Type != TokenRParen && p.currToken.Type != TokenEOF {
			if p.currToken.Type != TokenIdent {
				return nil, fmt.Errorf("%w: expected column name in column list", common.ErrInvalidSQL)
			}
			cols = append(cols, p.currToken.Literal)
			p.nextToken()
			if p.currToken.Type == TokenComma {
				p.nextToken()
			} else {
				break
			}
		}
		if err := p.expect(TokenRParen); err != nil {
			return nil, err
		}
	}

	if err := p.expect(TokenValues); err != nil {
		return nil, err
	}

	var allValues [][]Expr
	for {
		if err := p.expect(TokenLParen); err != nil {
			return nil, err
		}

		var rowValues []Expr
		for p.currToken.Type != TokenRParen && p.currToken.Type != TokenEOF {
			valExpr, err := p.parsePrimaryExpr()
			if err != nil {
				return nil, err
			}
			rowValues = append(rowValues, valExpr)
			if p.currToken.Type == TokenComma {
				p.nextToken()
			} else {
				break
			}
		}

		if err := p.expect(TokenRParen); err != nil {
			return nil, err
		}

		allValues = append(allValues, rowValues)

		if p.currToken.Type == TokenComma {
			p.nextToken()
		} else {
			break
		}
	}

	return &InsertStmt{
		Table:   tableName,
		Columns: cols,
		Values:  allValues,
	}, nil
}

func (p *Parser) parseSelect() (Statement, error) {
	p.nextToken() // consume SELECT

	distinct := false
	if p.currToken.Type == TokenDistinct {
		distinct = true
		p.nextToken()
	}

	var columns []Expr
	for {
		expr, err := p.parseExpression()
		if err != nil {
			return nil, err
		}
		columns = append(columns, expr)
		if p.currToken.Type == TokenComma {
			p.nextToken()
		} else {
			break
		}
	}

	if err := p.expect(TokenFrom); err != nil {
		return nil, err
	}

	if p.currToken.Type != TokenIdent {
		return nil, fmt.Errorf("%w: expected table name in FROM clause", common.ErrInvalidSQL)
	}
	tableName := p.currToken.Literal
	p.nextToken()

	// Parse optional JOINs
	var joins []JoinClause
	for {
		jType := JoinInner
		hasJoin := false

		if p.currToken.Type == TokenInner {
			p.nextToken()
			if err := p.expect(TokenJoin); err != nil {
				return nil, err
			}
			jType = JoinInner
			hasJoin = true
		} else if p.currToken.Type == TokenLeft {
			p.nextToken()
			if p.currToken.Type == TokenJoin {
				p.nextToken()
			}
			jType = JoinLeft
			hasJoin = true
		} else if p.currToken.Type == TokenCross {
			p.nextToken()
			if err := p.expect(TokenJoin); err != nil {
				return nil, err
			}
			jType = JoinCross
			hasJoin = true
		} else if p.currToken.Type == TokenJoin {
			p.nextToken()
			jType = JoinInner
			hasJoin = true
		}

		if !hasJoin {
			break
		}

		if p.currToken.Type != TokenIdent {
			return nil, fmt.Errorf("%w: expected table name after JOIN", common.ErrInvalidSQL)
		}
		joinTable := p.currToken.Literal
		p.nextToken()

		var onExpr Expr
		if jType != JoinCross {
			if err := p.expect(TokenOn); err != nil {
				return nil, err
			}
			var err error
			onExpr, err = p.parseExpression()
			if err != nil {
				return nil, err
			}
		}

		joins = append(joins, JoinClause{
			Type:  jType,
			Table: joinTable,
			On:    onExpr,
		})
	}

	var whereExpr Expr
	if p.currToken.Type == TokenWhere {
		p.nextToken()
		var err error
		whereExpr, err = p.parseExpression()
		if err != nil {
			return nil, err
		}
	}

	// GROUP BY
	var groupBy []Expr
	if p.currToken.Type == TokenGroup {
		p.nextToken()
		if err := p.expect(TokenBy); err != nil {
			return nil, err
		}
		for {
			grpExpr, err := p.parsePrimaryExpr()
			if err != nil {
				return nil, err
			}
			groupBy = append(groupBy, grpExpr)
			if p.currToken.Type == TokenComma {
				p.nextToken()
			} else {
				break
			}
		}
	}

	// HAVING
	var havingExpr Expr
	if p.currToken.Type == TokenHaving {
		p.nextToken()
		var err error
		havingExpr, err = p.parseExpression()
		if err != nil {
			return nil, err
		}
	}

	// ORDER BY
	var orderBy *OrderByClause
	if p.currToken.Type == TokenOrder {
		p.nextToken()
		if err := p.expect(TokenBy); err != nil {
			return nil, err
		}
		colExpr, err := p.parsePrimaryExpr()
		if err != nil {
			return nil, err
		}
		colRef, ok := colExpr.(*ColumnRef)
		if !ok {
			return nil, fmt.Errorf("%w: expected column name in ORDER BY", common.ErrInvalidSQL)
		}
		desc := false
		if p.currToken.Type == TokenDesc {
			desc = true
			p.nextToken()
		} else if p.currToken.Type == TokenAsc {
			p.nextToken()
		}
		orderBy = &OrderByClause{
			Table:  colRef.Table,
			Column: colRef.Name,
			Desc:   desc,
		}
	}

	limit := -1
	offset := 0
	if p.currToken.Type == TokenLimit {
		p.nextToken()
		if p.currToken.Type != TokenIntLit {
			return nil, fmt.Errorf("%w: expected integer in LIMIT clause", common.ErrInvalidSQL)
		}
		firstNum, err := strconv.Atoi(p.currToken.Literal)
		if err != nil {
			return nil, err
		}
		p.nextToken()

		// MySQL syntax: LIMIT offset, count OR LIMIT count OFFSET offset
		if p.currToken.Type == TokenComma {
			p.nextToken()
			if p.currToken.Type != TokenIntLit {
				return nil, fmt.Errorf("%w: expected count after offset in LIMIT", common.ErrInvalidSQL)
			}
			countNum, err := strconv.Atoi(p.currToken.Literal)
			if err != nil {
				return nil, err
			}
			offset = firstNum
			limit = countNum
			p.nextToken()
		} else {
			limit = firstNum
			if p.currToken.Type == TokenOffset {
				p.nextToken()
				if p.currToken.Type != TokenIntLit {
					return nil, fmt.Errorf("%w: expected integer in OFFSET clause", common.ErrInvalidSQL)
				}
				offNum, err := strconv.Atoi(p.currToken.Literal)
				if err != nil {
					return nil, err
				}
				offset = offNum
				p.nextToken()
			}
		}
	}

	return &SelectStmt{
		Distinct: distinct,
		Columns:  columns,
		Table:    tableName,
		Joins:    joins,
		Where:    whereExpr,
		GroupBy:  groupBy,
		Having:   havingExpr,
		OrderBy:  orderBy,
		Limit:    limit,
		Offset:   offset,
	}, nil
}

func (p *Parser) parseUpdate() (Statement, error) {
	p.nextToken() // consume UPDATE

	if p.currToken.Type != TokenIdent {
		return nil, fmt.Errorf("%w: expected table name in UPDATE", common.ErrInvalidSQL)
	}
	tableName := p.currToken.Literal
	p.nextToken()

	if err := p.expect(TokenSet); err != nil {
		return nil, err
	}

	var assignments []Assignment
	for {
		if p.currToken.Type != TokenIdent {
			return nil, fmt.Errorf("%w: expected column in SET assignment", common.ErrInvalidSQL)
		}
		col := p.currToken.Literal
		p.nextToken()

		if err := p.expect(TokenEq); err != nil {
			return nil, err
		}

		valExpr, err := p.parsePrimaryExpr()
		if err != nil {
			return nil, err
		}

		assignments = append(assignments, Assignment{
			Column: col,
			Value:  valExpr,
		})

		if p.currToken.Type == TokenComma {
			p.nextToken()
		} else {
			break
		}
	}

	var whereExpr Expr
	if p.currToken.Type == TokenWhere {
		p.nextToken()
		var err error
		whereExpr, err = p.parseExpression()
		if err != nil {
			return nil, err
		}
	}

	return &UpdateStmt{
		Table:       tableName,
		Assignments: assignments,
		Where:       whereExpr,
	}, nil
}

func (p *Parser) parseDelete() (Statement, error) {
	p.nextToken() // consume DELETE
	if err := p.expect(TokenFrom); err != nil {
		return nil, err
	}

	if p.currToken.Type != TokenIdent {
		return nil, fmt.Errorf("%w: expected table name in DELETE", common.ErrInvalidSQL)
	}
	tableName := p.currToken.Literal
	p.nextToken()

	var whereExpr Expr
	if p.currToken.Type == TokenWhere {
		p.nextToken()
		var err error
		whereExpr, err = p.parseExpression()
		if err != nil {
			return nil, err
		}
	}

	return &DeleteStmt{
		Table: tableName,
		Where: whereExpr,
	}, nil
}

func (p *Parser) parseExpression() (Expr, error) {
	return p.parseBinaryExpr(0)
}

func (p *Parser) parseBinaryExpr(minPrec int) (Expr, error) {
	left, err := p.parsePrimaryExpr()
	if err != nil {
		return nil, err
	}

	for {
		// Handle postfix / infix operators: IS [NOT] NULL, [NOT] LIKE, [NOT] IN, [NOT] BETWEEN
		if p.currToken.Type == TokenIs {
			p.nextToken()
			isNot := false
			if p.currToken.Type == TokenNot {
				isNot = true
				p.nextToken()
			}
			if err := p.expect(TokenNull); err != nil {
				return nil, err
			}
			left = &IsNullExpr{Expr: left, Not: isNot}
			continue
		}

		isNot := false
		if p.currToken.Type == TokenNot {
			isNot = true
			p.nextToken()
		}

		if p.currToken.Type == TokenLike {
			p.nextToken()
			patternExpr, err := p.parsePrimaryExpr()
			if err != nil {
				return nil, err
			}
			lit, ok := patternExpr.(*LiteralExpr)
			if !ok || lit.Val.Type != common.TypeText {
				return nil, fmt.Errorf("%w: LIKE requires string pattern", common.ErrInvalidSQL)
			}
			left = &LikeExpr{Expr: left, Pattern: lit.Val.Text, Not: isNot}
			continue
		}

		if p.currToken.Type == TokenIn {
			p.nextToken()
			if err := p.expect(TokenLParen); err != nil {
				return nil, err
			}
			var list []Expr
			for p.currToken.Type != TokenRParen && p.currToken.Type != TokenEOF {
				item, err := p.parsePrimaryExpr()
				if err != nil {
					return nil, err
				}
				list = append(list, item)
				if p.currToken.Type == TokenComma {
					p.nextToken()
				} else {
					break
				}
			}
			if err := p.expect(TokenRParen); err != nil {
				return nil, err
			}
			left = &InExpr{Expr: left, List: list, Not: isNot}
			continue
		}

		if p.currToken.Type == TokenBetween {
			p.nextToken()
			low, err := p.parsePrimaryExpr()
			if err != nil {
				return nil, err
			}
			if err := p.expect(TokenAnd); err != nil {
				return nil, err
			}
			high, err := p.parsePrimaryExpr()
			if err != nil {
				return nil, err
			}
			left = &BetweenExpr{Expr: left, Low: low, High: high, Not: isNot}
			continue
		}

		prec, op := p.currOpPrec()
		if prec < minPrec {
			break
		}
		p.nextToken()
		right, err := p.parseBinaryExpr(prec + 1)
		if err != nil {
			return nil, err
		}
		left = &BinaryExpr{
			Left:  left,
			Op:    op,
			Right: right,
		}
	}

	return left, nil
}

func (p *Parser) currOpPrec() (int, string) {
	switch p.currToken.Type {
	case TokenOr:
		return 1, "OR"
	case TokenAnd:
		return 2, "AND"
	case TokenEq:
		return 3, "="
	case TokenNeq:
		return 3, "!="
	case TokenLt:
		return 3, "<"
	case TokenLte:
		return 3, "<="
	case TokenGt:
		return 3, ">"
	case TokenGte:
		return 3, ">="
	default:
		return -1, ""
	}
}

func (p *Parser) parsePrimaryExpr() (Expr, error) {
	switch p.currToken.Type {
	case TokenIdent:
		lit := p.currToken.Literal
		p.nextToken()

		// Check for dot notation (table.column or table.*)
		if p.currToken.Type == TokenDot {
			p.nextToken()
			if p.currToken.Type == TokenStar {
				p.nextToken()
				return &StarExpr{Table: lit}, nil
			}
			if p.currToken.Type == TokenIdent {
				colName := p.currToken.Literal
				p.nextToken()
				return &ColumnRef{Table: lit, Name: colName}, nil
			}
			return nil, fmt.Errorf("%w: expected column name after '.'", common.ErrInvalidSQL)
		}
		return &ColumnRef{Name: lit}, nil

	case TokenStar:
		p.nextToken()
		return &StarExpr{}, nil

	case TokenCount, TokenSum, TokenAvg, TokenMin, TokenMax:
		fnName := p.currToken.Literal
		p.nextToken()
		if err := p.expect(TokenLParen); err != nil {
			return nil, err
		}

		distinct := false
		if p.currToken.Type == TokenDistinct {
			distinct = true
			p.nextToken()
		}

		var arg Expr
		if p.currToken.Type == TokenStar {
			arg = &StarExpr{}
			p.nextToken()
		} else {
			var err error
			arg, err = p.parsePrimaryExpr()
			if err != nil {
				return nil, err
			}
		}

		if err := p.expect(TokenRParen); err != nil {
			return nil, err
		}
		return &AggregateExpr{
			Func:     strings.ToUpper(fnName),
			Arg:      arg,
			Distinct: distinct,
		}, nil

	case TokenIntLit:
		val, err := strconv.ParseInt(p.currToken.Literal, 10, 64)
		if err != nil {
			return nil, err
		}
		p.nextToken()
		return &LiteralExpr{Val: common.IntValue(val)}, nil

	case TokenFloatLit:
		val, err := strconv.ParseFloat(p.currToken.Literal, 64)
		if err != nil {
			return nil, err
		}
		p.nextToken()
		return &LiteralExpr{Val: common.FloatValue(val)}, nil

	case TokenStringLit:
		str := p.currToken.Literal
		p.nextToken()
		return &LiteralExpr{Val: common.TextValue(str)}, nil

	case TokenTrue:
		p.nextToken()
		return &LiteralExpr{Val: common.BoolValue(true)}, nil

	case TokenFalse:
		p.nextToken()
		return &LiteralExpr{Val: common.BoolValue(false)}, nil

	case TokenNull:
		p.nextToken()
		return &LiteralExpr{Val: common.NullValue()}, nil

	case TokenLParen:
		p.nextToken()
		expr, err := p.parseExpression()
		if err != nil {
			return nil, err
		}
		if err := p.expect(TokenRParen); err != nil {
			return nil, err
		}
		return expr, nil

	default:
		return nil, fmt.Errorf("%w: unexpected expression token %q", common.ErrInvalidSQL, p.currToken.Literal)
	}
}

// ParseSQL is a convenience function to parse a raw SQL string into an AST.
func ParseSQL(sql string) (Statement, error) {
	trimmed := strings.TrimSpace(sql)
	trimmed = strings.TrimRight(trimmed, ";")
	p := NewParser(trimmed)
	return p.Parse()
}
