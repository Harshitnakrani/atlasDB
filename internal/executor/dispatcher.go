package executor

import (
	"atlasDB/internal/buffer"
	"atlasDB/internal/catalog"
	"atlasDB/internal/common"
	"atlasDB/internal/planner"
	"atlasDB/internal/transaction"
)

// Dispatch executes any physical plan against the database engine.
func Dispatch(plan planner.Plan, bp *buffer.BufferPool, cat *catalog.Catalog, tc *common.TraceContext, tx *transaction.Transaction) (*Result, error) {
	switch plan.Type() {
	case planner.PlanTypeCreateTable, planner.PlanTypeCreateIndex, planner.PlanTypeInsert,
		planner.PlanTypeUpdate, planner.PlanTypeDelete, planner.PlanTypeDropTable,
		planner.PlanTypeDropIndex, planner.PlanTypeTruncateTable, planner.PlanTypeAlterTable:
		res, err := ExecuteMutation(plan, bp, cat, tc, tx)
		if err != nil {
			return nil, err
		}
		if tc != nil {
			tc.Add("Executor", "-> %d rows affected", res.RowsAffected)
		}
		return res, nil

	default:
		// Query execution (TableScan, IndexScan, Project, Filter, OrderBy, Limit)
		exec, cols, err := BuildQueryExecutor(plan, bp, tc)
		if err != nil {
			return nil, err
		}
		if err := exec.Init(); err != nil {
			return nil, err
		}
		defer exec.Close()

		var rows [][]common.Value
		for {
			row, err := exec.Next()
			if err != nil {
				return nil, err
			}
			if row == nil {
				break
			}
			rows = append(rows, row.Values)
		}

		if tc != nil {
			if len(rows) == 1 {
				tc.Add("Executor", "-> 1 row")
			} else {
				tc.Add("Executor", "-> %d rows", len(rows))
			}
		}

		return &Result{
			Columns:      cols,
			Rows:         rows,
			RowsAffected: int64(len(rows)),
		}, nil
	}
}
