package planner

import (
	"fmt"

	"atlasDB/internal/catalog"
	"atlasDB/internal/common"
	"atlasDB/internal/parser"
)

type Planner struct {
	cat *catalog.Catalog
}

func NewPlanner(cat *catalog.Catalog) *Planner {
	return &Planner{cat: cat}
}

// Plan converts a parsed AST statement into an executable physical plan.
func (p *Planner) Plan(stmt parser.Statement, tc *common.TraceContext) (Plan, error) {
	switch s := stmt.(type) {
	case *parser.CreateTableStmt:
		return p.planCreateTable(s)
	case *parser.CreateIndexStmt:
		return p.planCreateIndex(s)
	case *parser.DropTableStmt:
		return &DropTablePlan{Table: s.Table, IfExists: s.IfExists}, nil
	case *parser.DropIndexStmt:
		return &DropIndexPlan{IndexName: s.IndexName, IfExists: s.IfExists}, nil
	case *parser.TruncateTableStmt:
		return &TruncateTablePlan{Table: s.Table}, nil
	case *parser.AlterTableStmt:
		return &AlterTablePlan{
			Table: s.Table,
			AddColumn: catalog.ColumnDef{
				Name:     s.AddColumn.Name,
				Type:     s.AddColumn.Type,
				Nullable: true,
			},
		}, nil
	case *parser.InsertStmt:
		return p.planInsert(s)
	case *parser.SelectStmt:
		return p.planSelect(s, tc)
	case *parser.UpdateStmt:
		return p.planUpdate(s, tc)
	case *parser.DeleteStmt:
		return p.planDelete(s, tc)
	case *parser.BeginStmt:
		return &BeginPlan{}, nil
	case *parser.CommitStmt:
		return &CommitPlan{}, nil
	case *parser.RollbackStmt:
		return &RollbackPlan{}, nil
	case *parser.ExplainStmt:
		return p.Plan(s.Stmt, tc)
	case *parser.TraceStmt:
		return p.Plan(s.Stmt, tc)
	default:
		return nil, fmt.Errorf("unsupported statement type for planning: %T", stmt)
	}
}

func (p *Planner) planCreateTable(s *parser.CreateTableStmt) (Plan, error) {
	cols := make([]catalog.ColumnDef, len(s.Columns))
	for i, c := range s.Columns {
		cols[i] = catalog.ColumnDef{
			Name:       c.Name,
			Type:       c.Type,
			PrimaryKey: c.PrimaryKey,
			Nullable:   !c.PrimaryKey,
		}
	}
	return &CreateTablePlan{
		Table:       s.Table,
		Columns:     cols,
		IfNotExists: s.IfNotExists,
	}, nil
}

func (p *Planner) planCreateIndex(s *parser.CreateIndexStmt) (Plan, error) {
	tbl, exists := p.cat.GetTable(s.Table)
	if !exists {
		return nil, common.ErrTableNotFound
	}
	if _, _, ok := tbl.FindColumn(s.Column); !ok {
		return nil, common.ErrColumnNotFound
	}
	return &CreateIndexPlan{
		IndexName: s.IndexName,
		Table:     s.Table,
		Column:    s.Column,
		Unique:    s.Unique,
	}, nil
}

func (p *Planner) planInsert(s *parser.InsertStmt) (Plan, error) {
	tbl, exists := p.cat.GetTable(s.Table)
	if !exists {
		return nil, common.ErrTableNotFound
	}

	cols := s.Columns
	if len(cols) == 0 {
		cols = make([]string, len(tbl.Columns))
		for i, c := range tbl.Columns {
			cols[i] = c.Name
		}
	}

	for _, cName := range cols {
		if _, _, ok := tbl.FindColumn(cName); !ok {
			return nil, fmt.Errorf("%w: %s in insert", common.ErrColumnNotFound, cName)
		}
	}

	allRowsValues := make([][]common.Value, len(s.Values))
	for rowIdx, rowExprs := range s.Values {
		if len(rowExprs) != len(cols) {
			return nil, fmt.Errorf("column count %d does not match value count %d", len(cols), len(rowExprs))
		}

		rowValues := make([]common.Value, len(tbl.Columns))
		for i := range rowValues {
			rowValues[i] = common.NullValue()
		}

		for valIdx, expr := range rowExprs {
			lit, ok := expr.(*parser.LiteralExpr)
			if !ok {
				return nil, fmt.Errorf("only literal expressions supported in INSERT")
			}
			colName := cols[valIdx]
			colPos, colDef, _ := tbl.FindColumn(colName)

			val := lit.Val
			if !val.IsNull() && val.Type != colDef.Type {
				if colDef.Type == common.TypeFloat && val.Type == common.TypeInt {
					val = common.FloatValue(float64(val.Int))
				} else {
					return nil, fmt.Errorf("%w for column %s: expected %v, got %v",
						common.ErrTypeMismatch, colName, colDef.Type, val.Type)
				}
			}

			rowValues[colPos] = val
		}

		allRowsValues[rowIdx] = rowValues
	}

	return &InsertPlan{
		Table:   tbl,
		Columns: cols,
		Values:  allRowsValues,
	}, nil
}

func (p *Planner) planSelect(s *parser.SelectStmt, tc *common.TraceContext) (Plan, error) {
	tbl, exists := p.cat.GetTable(s.Table)
	if !exists {
		return nil, common.ErrTableNotFound
	}

	var rootPlan Plan

	// 1. Check for JOINs
	if len(s.Joins) > 0 {
		rootPlan = &TableScanPlan{Table: tbl}
		tc.Add("Planner", "-> %s", rootPlan.String())

		for _, j := range s.Joins {
			rightTbl, rightExists := p.cat.GetTable(j.Table)
			if !rightExists {
				return nil, fmt.Errorf("join table %s not found", j.Table)
			}
			rightScan := &TableScanPlan{Table: rightTbl}
			rootPlan = &JoinPlan{
				Left:       rootPlan,
				Right:      rightScan,
				JoinType:   j.Type,
				LeftTable:  tbl,
				RightTable: rightTbl,
				On:         j.On,
			}
			tc.Add("Planner", "-> %s", rootPlan.String())
		}

		if s.Where != nil {
			rootPlan = &FilterPlan{
				Child: rootPlan,
				Where: s.Where,
				Table: tbl,
			}
		}
	} else {
		// Single-table query: check if WHERE predicate allows an IndexScan or PointLookup
		optIndexScan := p.tryIndexScan(tbl, s.Where)
		if optIndexScan != nil {
			rootPlan = optIndexScan
			tc.Add("Planner", "-> %s", optIndexScan.String())
		} else {
			rootPlan = &TableScanPlan{Table: tbl}
			tc.Add("Planner", "-> %s", rootPlan.String())
			if s.Where != nil {
				rootPlan = &FilterPlan{
					Child: rootPlan,
					Where: s.Where,
					Table: tbl,
				}
			}
		}
	}

	// 2. Check for Aggregations & Group By
	hasAggregates := false
	for _, col := range s.Columns {
		if _, isAgg := col.(*parser.AggregateExpr); isAgg {
			hasAggregates = true
			break
		}
	}

	if hasAggregates || len(s.GroupBy) > 0 {
		var aggs []AggregateItem
		var outNames []string
		var groupIndices []int
		var groupCols []string

		for _, grpExpr := range s.GroupBy {
			if colRef, ok := grpExpr.(*parser.ColumnRef); ok {
				idx, _, found := tbl.FindColumn(colRef.Name)
				if !found {
					return nil, fmt.Errorf("%w: %s in GROUP BY", common.ErrColumnNotFound, colRef.Name)
				}
				groupIndices = append(groupIndices, idx)
				groupCols = append(groupCols, colRef.Name)
			}
		}

		for _, expr := range s.Columns {
			switch e := expr.(type) {
			case *parser.AggregateExpr:
				colIdx := -1
				colName := "*"
				if _, isStar := e.Arg.(*parser.StarExpr); !isStar && e.Arg != nil {
					if colRef, ok := e.Arg.(*parser.ColumnRef); ok {
						idx, _, found := tbl.FindColumn(colRef.Name)
						if !found {
							return nil, fmt.Errorf("%w: %s in %s()", common.ErrColumnNotFound, colRef.Name, e.Func)
						}
						colIdx = idx
						colName = colRef.Name
					}
				}
				outName := fmt.Sprintf("%s(%s)", e.Func, colName)
				aggs = append(aggs, AggregateItem{
					Func:       e.Func,
					ColIdx:     colIdx,
					ColName:    colName,
					OutputName: outName,
				})
				outNames = append(outNames, outName)

			case *parser.ColumnRef:
				idx, _, found := tbl.FindColumn(e.Name)
				if !found {
					return nil, fmt.Errorf("%w: %s in select list with aggregation", common.ErrColumnNotFound, e.Name)
				}
				// Pass group column through
				aggs = append(aggs, AggregateItem{
					Func:       "GROUP",
					ColIdx:     idx,
					ColName:    e.Name,
					OutputName: e.Name,
				})
				outNames = append(outNames, e.Name)

			default:
				return nil, fmt.Errorf("expression not allowed in aggregation query")
			}
		}

		rootPlan = &AggregatePlan{
			Child:       rootPlan,
			Table:       tbl,
			Aggs:        aggs,
			GroupBy:     groupIndices,
			GroupCols:   groupCols,
			Having:      s.Having,
			OutputNames: outNames,
		}
	} else {
		// Normal Projections
		if s.OrderBy != nil {
			idx, _, found := tbl.FindColumn(s.OrderBy.Column)
			if !found {
				return nil, fmt.Errorf("%w: %s in ORDER BY", common.ErrColumnNotFound, s.OrderBy.Column)
			}
			rootPlan = &OrderByPlan{
				Child:   rootPlan,
				Table:   tbl,
				ColIdx:  idx,
				ColName: s.OrderBy.Column,
				Desc:    s.OrderBy.Desc,
			}
		}

		var colIndices []int
		var outNames []string
		if len(s.Columns) == 1 {
			if _, isStar := s.Columns[0].(*parser.StarExpr); isStar {
				colIndices = make([]int, len(tbl.Columns))
				outNames = make([]string, len(tbl.Columns))
				for i, c := range tbl.Columns {
					colIndices[i] = i
					outNames[i] = c.Name
				}
			}
		}

		if len(colIndices) == 0 {
			colIndices = make([]int, len(s.Columns))
			outNames = make([]string, len(s.Columns))
			for i, expr := range s.Columns {
				colRef, ok := expr.(*parser.ColumnRef)
				if !ok {
					return nil, fmt.Errorf("only column references or * supported in projection")
				}
				idx := -1
				outName := colRef.Name

				if len(s.Joins) > 0 {
					// Multi-table join column resolution
					if colRef.Table == "" || colRef.Table == tbl.Name {
						if cIdx, _, found := tbl.FindColumn(colRef.Name); found {
							idx = cIdx
							if colRef.Table != "" {
								outName = fmt.Sprintf("%s.%s", colRef.Table, colRef.Name)
							}
						}
					}
					if idx == -1 {
						colOffset := len(tbl.Columns)
						for _, j := range s.Joins {
							jTbl, _ := p.cat.GetTable(j.Table)
							if jTbl != nil && (colRef.Table == "" || colRef.Table == j.Table) {
								if cIdx, _, found := jTbl.FindColumn(colRef.Name); found {
									idx = colOffset + cIdx
									outName = fmt.Sprintf("%s.%s", j.Table, colRef.Name)
									break
								}
							}
							if jTbl != nil {
								colOffset += len(jTbl.Columns)
							}
						}
					}
				} else {
					cIdx, _, found := tbl.FindColumn(colRef.Name)
					if !found {
						return nil, fmt.Errorf("%w: %s in SELECT projection", common.ErrColumnNotFound, colRef.Name)
					}
					idx = cIdx
				}

				if idx == -1 {
					return nil, fmt.Errorf("%w: %s in SELECT projection", common.ErrColumnNotFound, colRef.Name)
				}
				colIndices[i] = idx
				outNames[i] = outName
			}
		}

		rootPlan = &ProjectPlan{
			Child:       rootPlan,
			Table:       tbl,
			ColIndices:  colIndices,
			OutputNames: outNames,
		}
	}

	// 3. Distinct
	if s.Distinct {
		rootPlan = &DistinctPlan{Child: rootPlan}
	}

	// 4. Limit & Offset
	if s.Limit >= 0 || s.Offset > 0 {
		rootPlan = &LimitPlan{
			Child:  rootPlan,
			Limit:  s.Limit,
			Offset: s.Offset,
		}
	}

	return rootPlan, nil
}

func (p *Planner) tryIndexScan(tbl *catalog.TableDef, where parser.Expr) *IndexScanPlan {
	if where == nil {
		return nil
	}

	bin, ok := where.(*parser.BinaryExpr)
	if !ok || bin.Op != "=" {
		return nil
	}

	var colRef *parser.ColumnRef
	var lit *parser.LiteralExpr

	if cr, ok := bin.Left.(*parser.ColumnRef); ok {
		if l, ok := bin.Right.(*parser.LiteralExpr); ok {
			colRef = cr
			lit = l
		}
	} else if cr, ok := bin.Right.(*parser.ColumnRef); ok {
		if l, ok := bin.Left.(*parser.LiteralExpr); ok {
			colRef = cr
			lit = l
		}
	}

	if colRef == nil || lit == nil {
		return nil
	}

	colIdx, colDef, found := tbl.FindColumn(colRef.Name)
	if !found {
		return nil
	}

	if colDef.PrimaryKey && lit.Val.Type == common.TypeInt {
		return &IndexScanPlan{
			Table:     tbl,
			ColName:   colDef.Name,
			TargetVal: lit.Val,
			IsPK:      true,
		}
	}

	indexes := p.cat.GetTableIndexes(tbl.Name)
	for _, idx := range indexes {
		if idx.ColumnName == colRef.Name {
			_ = colIdx
			return &IndexScanPlan{
				Table:     tbl,
				Index:     idx,
				ColName:   colDef.Name,
				TargetVal: lit.Val,
				IsPK:      false,
			}
		}
	}

	return nil
}

func (p *Planner) planUpdate(s *parser.UpdateStmt, tc *common.TraceContext) (Plan, error) {
	tbl, exists := p.cat.GetTable(s.Table)
	if !exists {
		return nil, common.ErrTableNotFound
	}

	assignments := make(map[int]common.Value, len(s.Assignments))
	for _, a := range s.Assignments {
		idx, colDef, found := tbl.FindColumn(a.Column)
		if !found {
			return nil, fmt.Errorf("%w: %s in SET", common.ErrColumnNotFound, a.Column)
		}
		lit, ok := a.Value.(*parser.LiteralExpr)
		if !ok {
			return nil, fmt.Errorf("only literal assignments supported in UPDATE")
		}
		assignments[idx] = lit.Val
		_ = colDef
	}

	scan := p.tryIndexScan(tbl, s.Where)
	var basePlan Plan = scan
	if basePlan == nil {
		basePlan = &TableScanPlan{Table: tbl}
		if s.Where != nil {
			basePlan = &FilterPlan{Child: basePlan, Where: s.Where, Table: tbl}
		}
	}

	return &UpdatePlan{
		Table:       tbl,
		Scan:        basePlan,
		Assignments: assignments,
	}, nil
}

func (p *Planner) planDelete(s *parser.DeleteStmt, tc *common.TraceContext) (Plan, error) {
	tbl, exists := p.cat.GetTable(s.Table)
	if !exists {
		return nil, common.ErrTableNotFound
	}

	scan := p.tryIndexScan(tbl, s.Where)
	var basePlan Plan = scan
	if basePlan == nil {
		basePlan = &TableScanPlan{Table: tbl}
		if s.Where != nil {
			basePlan = &FilterPlan{Child: basePlan, Where: s.Where, Table: tbl}
		}
	}

	return &DeletePlan{
		Table: tbl,
		Scan:  basePlan,
	}, nil
}
