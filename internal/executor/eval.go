package executor

import (
	"fmt"
	"regexp"
	"strings"

	"atlasDB/internal/catalog"
	"atlasDB/internal/common"
	"atlasDB/internal/parser"
	"atlasDB/internal/planner"
)

// EvalExpr evaluates an AST expression against a row tuple.
func EvalExpr(expr parser.Expr, row *Row, tbl *catalog.TableDef) (common.Value, error) {
	if expr == nil {
		return common.BoolValue(true), nil
	}

	switch e := expr.(type) {
	case *parser.LiteralExpr:
		return e.Val, nil

	case *parser.ColumnRef:
		idx, _, found := tbl.FindColumn(e.Name)
		if !found {
			return common.NullValue(), fmt.Errorf("%w: %s in expression", common.ErrColumnNotFound, e.Name)
		}
		if idx < len(row.Values) {
			return row.Values[idx], nil
		}
		return common.NullValue(), nil

	case *parser.IsNullExpr:
		val, err := EvalExpr(e.Expr, row, tbl)
		if err != nil {
			return common.NullValue(), err
		}
		isNull := val.IsNull()
		if e.Not {
			return common.BoolValue(!isNull), nil
		}
		return common.BoolValue(isNull), nil

	case *parser.LikeExpr:
		val, err := EvalExpr(e.Expr, row, tbl)
		if err != nil {
			return common.NullValue(), err
		}
		if val.IsNull() || val.Type != common.TypeText {
			return common.BoolValue(false), nil
		}
		matched := matchLike(val.Text, e.Pattern)
		if e.Not {
			matched = !matched
		}
		return common.BoolValue(matched), nil

	case *parser.InExpr:
		targetVal, err := EvalExpr(e.Expr, row, tbl)
		if err != nil {
			return common.NullValue(), err
		}
		found := false
		for _, item := range e.List {
			itemVal, err := EvalExpr(item, row, tbl)
			if err == nil && targetVal.Compare(itemVal) == 0 {
				found = true
				break
			}
		}
		if e.Not {
			found = !found
		}
		return common.BoolValue(found), nil

	case *parser.BetweenExpr:
		val, err := EvalExpr(e.Expr, row, tbl)
		if err != nil {
			return common.NullValue(), err
		}
		lowVal, err := EvalExpr(e.Low, row, tbl)
		if err != nil {
			return common.NullValue(), err
		}
		highVal, err := EvalExpr(e.High, row, tbl)
		if err != nil {
			return common.NullValue(), err
		}
		inRange := (val.Compare(lowVal) >= 0) && (val.Compare(highVal) <= 0)
		if e.Not {
			inRange = !inRange
		}
		return common.BoolValue(inRange), nil

	case *parser.BinaryExpr:
		left, err := EvalExpr(e.Left, row, tbl)
		if err != nil {
			return common.NullValue(), err
		}
		right, err := EvalExpr(e.Right, row, tbl)
		if err != nil {
			return common.NullValue(), err
		}

		switch e.Op {
		case "AND":
			return common.BoolValue(isTruthy(left) && isTruthy(right)), nil
		case "OR":
			return common.BoolValue(isTruthy(left) || isTruthy(right)), nil
		case "=":
			return common.BoolValue(left.Compare(right) == 0), nil
		case "!=":
			return common.BoolValue(left.Compare(right) != 0), nil
		case "<":
			return common.BoolValue(left.Compare(right) < 0), nil
		case "<=":
			return common.BoolValue(left.Compare(right) <= 0), nil
		case ">":
			return common.BoolValue(left.Compare(right) > 0), nil
		case ">=":
			return common.BoolValue(left.Compare(right) >= 0), nil
		default:
			return common.NullValue(), fmt.Errorf("unsupported operator: %s", e.Op)
		}

	default:
		return common.NullValue(), fmt.Errorf("unsupported expression type: %T", expr)
	}
}

// EvalAggExpr evaluates expressions in HAVING clauses against aggregated result rows.
func EvalAggExpr(expr parser.Expr, row *Row, plan *planner.AggregatePlan) (common.Value, error) {
	if expr == nil {
		return common.BoolValue(true), nil
	}

	switch e := expr.(type) {
	case *parser.LiteralExpr:
		return e.Val, nil

	case *parser.AggregateExpr:
		argName := "*"
		if _, isStar := e.Arg.(*parser.StarExpr); !isStar && e.Arg != nil {
			if cr, ok := e.Arg.(*parser.ColumnRef); ok {
				argName = cr.Name
			}
		}
		targetName := fmt.Sprintf("%s(%s)", e.Func, argName)
		for i, name := range plan.OutputNames {
			if name == targetName && i < len(row.Values) {
				return row.Values[i], nil
			}
		}
		return common.NullValue(), fmt.Errorf("aggregate %s not found in outputs", targetName)

	case *parser.ColumnRef:
		for i, name := range plan.OutputNames {
			if name == e.Name && i < len(row.Values) {
				return row.Values[i], nil
			}
		}
		return common.NullValue(), fmt.Errorf("column %s not found in aggregated row", e.Name)

	case *parser.BinaryExpr:
		left, err := EvalAggExpr(e.Left, row, plan)
		if err != nil {
			return common.NullValue(), err
		}
		right, err := EvalAggExpr(e.Right, row, plan)
		if err != nil {
			return common.NullValue(), err
		}
		switch e.Op {
		case "AND":
			return common.BoolValue(isTruthy(left) && isTruthy(right)), nil
		case "OR":
			return common.BoolValue(isTruthy(left) || isTruthy(right)), nil
		case "=":
			return common.BoolValue(left.Compare(right) == 0), nil
		case "!=":
			return common.BoolValue(left.Compare(right) != 0), nil
		case "<":
			return common.BoolValue(left.Compare(right) < 0), nil
		case "<=":
			return common.BoolValue(left.Compare(right) <= 0), nil
		case ">":
			return common.BoolValue(left.Compare(right) > 0), nil
		case ">=":
			return common.BoolValue(left.Compare(right) >= 0), nil
		default:
			return common.NullValue(), fmt.Errorf("unsupported operator: %s", e.Op)
		}

	default:
		return common.NullValue(), fmt.Errorf("unsupported expression in HAVING: %T", expr)
	}
}

func matchLike(text, pattern string) bool {
	var sb strings.Builder
	sb.WriteString("^")
	for i := 0; i < len(pattern); i++ {
		ch := pattern[i]
		switch ch {
		case '%':
			sb.WriteString(".*")
		case '_':
			sb.WriteString(".")
		case '.', '*', '+', '?', '^', '$', '{', '}', '(', ')', '|', '[', ']', '\\':
			sb.WriteString("\\")
			sb.WriteByte(ch)
		default:
			sb.WriteByte(ch)
		}
	}
	sb.WriteString("$")

	re, err := regexp.Compile(sb.String())
	if err != nil {
		return false
	}
	return re.MatchString(text)
}

func isTruthy(val common.Value) bool {
	if val.IsNull() {
		return false
	}
	if val.Type == common.TypeBool {
		return val.Bool
	}
	if val.Type == common.TypeInt {
		return val.Int != 0
	}
	return true
}
