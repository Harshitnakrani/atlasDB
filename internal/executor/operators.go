package executor

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"atlasDB/internal/btree"
	"atlasDB/internal/buffer"
	"atlasDB/internal/catalog"
	"atlasDB/internal/common"
	"atlasDB/internal/parser"
	"atlasDB/internal/planner"
	"atlasDB/internal/record"
)

// TableScanExecutor iterates sequentially over all records in a table.
type TableScanExecutor struct {
	bp     *buffer.BufferPool
	tbl    *catalog.TableDef
	cursor *btree.Cursor
	tree   *btree.Tree
	tc     *common.TraceContext
}

func NewTableScanExecutor(bp *buffer.BufferPool, tbl *catalog.TableDef, tc *common.TraceContext) *TableScanExecutor {
	tree := btree.NewTree(bp, tbl.RootPageID)
	return &TableScanExecutor{
		bp:   bp,
		tbl:  tbl,
		tree: tree,
		tc:   tc,
	}
}

func (e *TableScanExecutor) Init() error {
	cursor, err := btree.NewCursor(e.tree, 1)
	if err != nil {
		return err
	}
	e.cursor = cursor
	return nil
}

func (e *TableScanExecutor) Next() (*Row, error) {
	if e.cursor == nil {
		return nil, nil
	}

	for {
		rowID, payload, ok, err := e.cursor.Next()
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, nil
		}

		values, err := record.Decode(payload)
		if err != nil {
			return nil, err
		}

		return &Row{
			RowID:  rowID,
			Values: values,
		}, nil
	}
}

func (e *TableScanExecutor) Close() error {
	if e.cursor != nil {
		err := e.cursor.Close()
		e.cursor = nil
		return err
	}
	return nil
}

// IndexScanExecutor performs direct point lookup via PK or secondary index.
type IndexScanExecutor struct {
	bp        *buffer.BufferPool
	tbl       *catalog.TableDef
	tree      *btree.Tree
	plan      *planner.IndexScanPlan
	tc        *common.TraceContext
	yielded   bool
	rowIDs    []int64
	currRowID int
}

func NewIndexScanExecutor(bp *buffer.BufferPool, plan *planner.IndexScanPlan, tc *common.TraceContext) *IndexScanExecutor {
	tree := btree.NewTree(bp, plan.Table.RootPageID)
	return &IndexScanExecutor{
		bp:   bp,
		tbl:  plan.Table,
		tree: tree,
		plan: plan,
		tc:   tc,
	}
}

func (e *IndexScanExecutor) Init() error {
	if e.plan.IsPK {
		e.yielded = false
		return nil
	}

	idxTree := btree.NewIndexTree(e.bp, e.plan.Index.RootPageID)
	keyBytes, err := record.EncodeValues([]common.Value{e.plan.TargetVal})
	if err != nil {
		return err
	}
	rowIDs, err := idxTree.Find(keyBytes)
	if err != nil {
		return err
	}
	e.rowIDs = rowIDs
	e.currRowID = 0
	return nil
}

func (e *IndexScanExecutor) Next() (*Row, error) {
	if e.plan.IsPK {
		if e.yielded {
			return nil, nil
		}
		e.yielded = true

		targetRowID := e.plan.TargetVal.Int
		payload, err := e.tree.SeekRowid(targetRowID, e.tc)
		if err != nil {
			if err == common.ErrNotFound {
				return nil, nil
			}
			return nil, err
		}

		values, err := record.Decode(payload)
		if err != nil {
			return nil, err
		}
		return &Row{RowID: targetRowID, Values: values}, nil
	}

	if e.currRowID >= len(e.rowIDs) {
		return nil, nil
	}

	targetRowID := e.rowIDs[e.currRowID]
	e.currRowID++

	payload, err := e.tree.SeekRowid(targetRowID, e.tc)
	if err != nil {
		if err == common.ErrNotFound {
			return e.Next()
		}
		return nil, err
	}

	values, err := record.Decode(payload)
	if err != nil {
		return nil, err
	}
	return &Row{RowID: targetRowID, Values: values}, nil
}

func (e *IndexScanExecutor) Close() error {
	return nil
}

// JoinExecutor implements Nested-Loop Join (INNER and LEFT).
type JoinExecutor struct {
	bp        *buffer.BufferPool
	plan      *planner.JoinPlan
	leftExec  Executor
	rightExec Executor
	leftRow   *Row
	tc        *common.TraceContext
	hasMatch  bool
}

func (e *JoinExecutor) Init() error {
	if err := e.leftExec.Init(); err != nil {
		return err
	}
	e.leftRow = nil
	e.hasMatch = false
	return nil
}

func (e *JoinExecutor) Next() (*Row, error) {
	for {
		if e.leftRow == nil {
			var err error
			e.leftRow, err = e.leftExec.Next()
			if err != nil || e.leftRow == nil {
				return nil, err
			}
			e.hasMatch = false
			if err := e.rightExec.Init(); err != nil {
				return nil, err
			}
		}

		rightRow, err := e.rightExec.Next()
		if err != nil {
			return nil, err
		}

		if rightRow != nil {
			// Combine columns
			combined := make([]common.Value, 0, len(e.leftRow.Values)+len(rightRow.Values))
			combined = append(combined, e.leftRow.Values...)
			combined = append(combined, rightRow.Values...)
			combinedRow := &Row{RowID: e.leftRow.RowID, Values: combined}

			matched := true
			if e.plan.On != nil {
				matched, err = e.evalJoinOn(combinedRow)
				if err != nil {
					return nil, err
				}
			}

			if matched {
				e.hasMatch = true
				return combinedRow, nil
			}
			continue
		}

		// Reached end of right table for current left row
		_ = e.rightExec.Close()
		needLeftNull := (e.plan.JoinType == parser.JoinLeft) && !e.hasMatch

		currLeft := e.leftRow
		e.leftRow = nil

		if needLeftNull {
			nulls := make([]common.Value, len(e.plan.RightTable.Columns))
			for i := range nulls {
				nulls[i] = common.NullValue()
			}
			combined := append(currLeft.Values, nulls...)
			return &Row{RowID: currLeft.RowID, Values: combined}, nil
		}
	}
}

func (e *JoinExecutor) evalJoinOn(row *Row) (bool, error) {
	bin, ok := e.plan.On.(*parser.BinaryExpr)
	if !ok || bin.Op != "=" {
		return true, nil
	}

	leftRef, okL := bin.Left.(*parser.ColumnRef)
	rightRef, okR := bin.Right.(*parser.ColumnRef)
	if !okL || !okR {
		return true, nil
	}

	// Resolve left val and right val
	leftIdx, _, _ := e.plan.LeftTable.FindColumn(leftRef.Name)
	rightIdx, _, _ := e.plan.RightTable.FindColumn(rightRef.Name)
	if leftIdx >= 0 && rightIdx >= 0 {
		actualRightIdx := len(e.plan.LeftTable.Columns) + rightIdx
		if leftIdx < len(row.Values) && actualRightIdx < len(row.Values) {
			return row.Values[leftIdx].Compare(row.Values[actualRightIdx]) == 0, nil
		}
	}
	return true, nil
}

func (e *JoinExecutor) Close() error {
	_ = e.leftExec.Close()
	_ = e.rightExec.Close()
	return nil
}

// AggregateExecutor implements aggregations (COUNT, SUM, AVG, MIN, MAX) with GROUP BY and HAVING.
type AggregateExecutor struct {
	child   Executor
	plan    *planner.AggregatePlan
	results []*Row
	currPos int
}

func (e *AggregateExecutor) Init() error {
	if err := e.child.Init(); err != nil {
		return err
	}
	defer e.child.Close()

	type aggGroup struct {
		groupVals []common.Value
		count     int64
		sums      []float64
		hasSum    []bool
		mins      []common.Value
		maxs      []common.Value
	}

	groups := make(map[string]*aggGroup)
	var groupKeys []string

	for {
		row, err := e.child.Next()
		if err != nil {
			return err
		}
		if row == nil {
			break
		}

		// Compute group key
		var sb strings.Builder
		var grpVals []common.Value
		for _, gIdx := range e.plan.GroupBy {
			if gIdx >= 0 && gIdx < len(row.Values) {
				v := row.Values[gIdx]
				sb.WriteString(v.String())
				sb.WriteString("|")
				grpVals = append(grpVals, v)
			}
		}
		key := sb.String()

		g, exists := groups[key]
		if !exists {
			g = &aggGroup{
				groupVals: grpVals,
				sums:      make([]float64, len(e.plan.Aggs)),
				hasSum:    make([]bool, len(e.plan.Aggs)),
				mins:      make([]common.Value, len(e.plan.Aggs)),
				maxs:      make([]common.Value, len(e.plan.Aggs)),
			}
			groups[key] = g
			groupKeys = append(groupKeys, key)
		}

		g.count++

		for aIdx, agg := range e.plan.Aggs {
			if agg.ColIdx < 0 || agg.ColIdx >= len(row.Values) {
				continue
			}
			val := row.Values[agg.ColIdx]
			if val.IsNull() {
				continue
			}

			// Numeric accumulators for SUM / AVG
			var numVal float64
			if val.Type == common.TypeInt {
				numVal = float64(val.Int)
				g.sums[aIdx] += numVal
				g.hasSum[aIdx] = true
			} else if val.Type == common.TypeFloat {
				numVal = val.Float
				g.sums[aIdx] += numVal
				g.hasSum[aIdx] = true
			}

			// MIN / MAX
			if g.mins[aIdx].IsNull() || val.Compare(g.mins[aIdx]) < 0 {
				g.mins[aIdx] = val
			}
			if g.maxs[aIdx].IsNull() || val.Compare(g.maxs[aIdx]) > 0 {
				g.maxs[aIdx] = val
			}
		}
	}

	// Handle empty table with scalar aggregate (e.g. COUNT(*) on empty returns 0)
	if len(groups) == 0 && len(e.plan.GroupBy) == 0 {
		rowVals := make([]common.Value, len(e.plan.Aggs))
		for aIdx, agg := range e.plan.Aggs {
			switch agg.Func {
			case "COUNT":
				rowVals[aIdx] = common.IntValue(0)
			default:
				rowVals[aIdx] = common.NullValue()
			}
		}
		e.results = []*Row{{RowID: 1, Values: rowVals}}
		e.currPos = 0
		return nil
	}

	// Build result rows
	var outRows []*Row
	for _, key := range groupKeys {
		g := groups[key]
		rowVals := make([]common.Value, len(e.plan.Aggs))
		grpValIdx := 0

		for aIdx, agg := range e.plan.Aggs {
			switch agg.Func {
			case "COUNT":
				rowVals[aIdx] = common.IntValue(g.count)
			case "SUM":
				if g.hasSum[aIdx] {
					rowVals[aIdx] = common.FloatValue(g.sums[aIdx])
				} else {
					rowVals[aIdx] = common.NullValue()
				}
			case "AVG":
				if g.count > 0 && g.hasSum[aIdx] {
					rowVals[aIdx] = common.FloatValue(g.sums[aIdx] / float64(g.count))
				} else {
					rowVals[aIdx] = common.NullValue()
				}
			case "MIN":
				rowVals[aIdx] = g.mins[aIdx]
			case "MAX":
				rowVals[aIdx] = g.maxs[aIdx]
			case "GROUP":
				if grpValIdx < len(g.groupVals) {
					rowVals[aIdx] = g.groupVals[grpValIdx]
					grpValIdx++
				}
			}
		}

		resRow := &Row{RowID: int64(len(outRows) + 1), Values: rowVals}

		// Apply HAVING filter if present
		if e.plan.Having != nil {
			match, err := EvalAggExpr(e.plan.Having, resRow, e.plan)
			if err != nil || !isTruthy(match) {
				continue
			}
		}

		outRows = append(outRows, resRow)
	}

	e.results = outRows
	e.currPos = 0
	return nil
}

func (e *AggregateExecutor) Next() (*Row, error) {
	if e.currPos >= len(e.results) {
		return nil, nil
	}
	r := e.results[e.currPos]
	e.currPos++
	return r, nil
}

func (e *AggregateExecutor) Close() error {
	e.results = nil
	return nil
}

// DistinctExecutor removes duplicate rows from the output.
type DistinctExecutor struct {
	child Executor
	seen  map[string]bool
}

func (e *DistinctExecutor) Init() error {
	e.seen = make(map[string]bool)
	return e.child.Init()
}

func (e *DistinctExecutor) Next() (*Row, error) {
	for {
		row, err := e.child.Next()
		if err != nil || row == nil {
			return nil, err
		}

		var sb strings.Builder
		for _, val := range row.Values {
			sb.WriteString(val.String())
			sb.WriteString("|")
		}
		sig := sb.String()

		if !e.seen[sig] {
			e.seen[sig] = true
			return row, nil
		}
	}
}

func (e *DistinctExecutor) Close() error {
	e.seen = nil
	return e.child.Close()
}

// FilterExecutor filters incoming rows against a predicate.
type FilterExecutor struct {
	child Executor
	where parser.Expr
	tbl   *catalog.TableDef
}

func (e *FilterExecutor) Init() error {
	return e.child.Init()
}

func (e *FilterExecutor) Next() (*Row, error) {
	for {
		row, err := e.child.Next()
		if err != nil || row == nil {
			return nil, err
		}

		resVal, err := EvalExpr(e.where, row, e.tbl)
		if err != nil {
			return nil, err
		}
		if isTruthy(resVal) {
			return row, nil
		}
	}
}

func (e *FilterExecutor) Close() error {
	return e.child.Close()
}

// ProjectExecutor projects selected columns.
type ProjectExecutor struct {
	child      Executor
	colIndices []int
}

func (e *ProjectExecutor) Init() error {
	return e.child.Init()
}

func (e *ProjectExecutor) Next() (*Row, error) {
	row, err := e.child.Next()
	if err != nil || row == nil {
		return nil, err
	}

	projected := make([]common.Value, len(e.colIndices))
	for i, idx := range e.colIndices {
		if idx >= 0 && idx < len(row.Values) {
			projected[i] = row.Values[idx]
		} else {
			projected[i] = common.NullValue()
		}
	}

	return &Row{
		RowID:  row.RowID,
		Values: projected,
	}, nil
}

func (e *ProjectExecutor) Close() error {
	return e.child.Close()
}

// OrderByExecutor sorts rows in memory before returning.
type OrderByExecutor struct {
	child   Executor
	colIdx  int
	desc    bool
	rows    []*Row
	currPos int
}

func (e *OrderByExecutor) Init() error {
	if err := e.child.Init(); err != nil {
		return err
	}
	defer e.child.Close()

	var allRows []*Row
	for {
		row, err := e.child.Next()
		if err != nil {
			return err
		}
		if row == nil {
			break
		}
		allRows = append(allRows, row)
	}

	sort.SliceStable(allRows, func(i, j int) bool {
		valA := allRows[i].Values[e.colIdx]
		valB := allRows[j].Values[e.colIdx]
		cmp := valA.Compare(valB)
		if e.desc {
			return cmp > 0
		}
		return cmp < 0
	})

	e.rows = allRows
	e.currPos = 0
	return nil
}

func (e *OrderByExecutor) Next() (*Row, error) {
	if e.currPos >= len(e.rows) {
		return nil, nil
	}
	r := e.rows[e.currPos]
	e.currPos++
	return r, nil
}

func (e *OrderByExecutor) Close() error {
	e.rows = nil
	return nil
}

// LimitExecutor limits the maximum number of rows returned, with OFFSET support.
type LimitExecutor struct {
	child    Executor
	limit    int
	offset   int
	skipped  int
	returned int
}

func (e *LimitExecutor) Init() error {
	e.skipped = 0
	e.returned = 0
	return e.child.Init()
}

func (e *LimitExecutor) Next() (*Row, error) {
	// Skip initial offset rows
	for e.skipped < e.offset {
		row, err := e.child.Next()
		if err != nil || row == nil {
			return nil, err
		}
		e.skipped++
	}

	if e.limit >= 0 && e.returned >= e.limit {
		return nil, nil
	}

	row, err := e.child.Next()
	if err != nil || row == nil {
		return nil, err
	}
	e.returned++
	return row, nil
}

func (e *LimitExecutor) Close() error {
	return e.child.Close()
}

// BuildQueryExecutor builds the execution pipeline from a planned SELECT.
func BuildQueryExecutor(plan planner.Plan, bp *buffer.BufferPool, tc *common.TraceContext) (Executor, []string, error) {
	switch p := plan.(type) {
	case *planner.TableScanPlan:
		return NewTableScanExecutor(bp, p.Table, tc), nil, nil

	case *planner.IndexScanPlan:
		return NewIndexScanExecutor(bp, p, tc), nil, nil

	case *planner.JoinPlan:
		leftExec, _, err := BuildQueryExecutor(p.Left, bp, tc)
		if err != nil {
			return nil, nil, err
		}
		rightExec, _, err := BuildQueryExecutor(p.Right, bp, tc)
		if err != nil {
			return nil, nil, err
		}
		var cols []string
		for _, c := range p.LeftTable.Columns {
			cols = append(cols, fmt.Sprintf("%s.%s", p.LeftTable.Name, c.Name))
		}
		for _, c := range p.RightTable.Columns {
			cols = append(cols, fmt.Sprintf("%s.%s", p.RightTable.Name, c.Name))
		}
		return &JoinExecutor{
			bp:        bp,
			plan:      p,
			leftExec:  leftExec,
			rightExec: rightExec,
			tc:        tc,
		}, cols, nil

	case *planner.FilterPlan:
		childExec, cols, err := BuildQueryExecutor(p.Child, bp, tc)
		if err != nil {
			return nil, nil, err
		}
		return &FilterExecutor{
			child: childExec,
			where: p.Where,
			tbl:   p.Table,
		}, cols, nil

	case *planner.AggregatePlan:
		childExec, _, err := BuildQueryExecutor(p.Child, bp, tc)
		if err != nil {
			return nil, nil, err
		}
		return &AggregateExecutor{
			child: childExec,
			plan:  p,
		}, p.OutputNames, nil

	case *planner.ProjectPlan:
		childExec, _, err := BuildQueryExecutor(p.Child, bp, tc)
		if err != nil {
			return nil, nil, err
		}
		return &ProjectExecutor{
			child:      childExec,
			colIndices: p.ColIndices,
		}, p.OutputNames, nil

	case *planner.DistinctPlan:
		childExec, cols, err := BuildQueryExecutor(p.Child, bp, tc)
		if err != nil {
			return nil, nil, err
		}
		return &DistinctExecutor{
			child: childExec,
		}, cols, nil

	case *planner.OrderByPlan:
		childExec, cols, err := BuildQueryExecutor(p.Child, bp, tc)
		if err != nil {
			return nil, nil, err
		}
		return &OrderByExecutor{
			child:  childExec,
			colIdx: p.ColIdx,
			desc:   p.Desc,
		}, cols, nil

	case *planner.LimitPlan:
		childExec, cols, err := BuildQueryExecutor(p.Child, bp, tc)
		if err != nil {
			return nil, nil, err
		}
		return &LimitExecutor{
			child:  childExec,
			limit:  p.Limit,
			offset: p.Offset,
		}, cols, nil

	default:
		return nil, nil, fmt.Errorf("unexpected plan type for query executor: %T", plan)
	}
}

var _ = math.Abs
