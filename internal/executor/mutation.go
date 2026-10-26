package executor

import (
	"fmt"

	"atlasDB/internal/btree"
	"atlasDB/internal/buffer"
	"atlasDB/internal/catalog"
	"atlasDB/internal/common"
	"atlasDB/internal/planner"
	"atlasDB/internal/record"
	"atlasDB/internal/transaction"
)

// ExecuteMutation executes DDL and DML operations.
func ExecuteMutation(plan planner.Plan, bp *buffer.BufferPool, cat *catalog.Catalog, tc *common.TraceContext, tx *transaction.Transaction) (*Result, error) {
	switch p := plan.(type) {
	case *planner.CreateTablePlan:
		return executeCreateTable(p, bp, cat)
	case *planner.CreateIndexPlan:
		return executeCreateIndex(p, bp, cat)
	case *planner.DropTablePlan:
		return executeDropTable(p, bp, cat)
	case *planner.DropIndexPlan:
		return executeDropIndex(p, bp, cat)
	case *planner.TruncateTablePlan:
		return executeTruncateTable(p, bp, cat)
	case *planner.AlterTablePlan:
		return executeAlterTable(p, bp, cat)
	case *planner.InsertPlan:
		return executeInsert(p, bp, cat, tc, tx)
	case *planner.UpdatePlan:
		return executeUpdate(p, bp, cat, tc, tx)
	case *planner.DeletePlan:
		return executeDelete(p, bp, cat, tc, tx)
	default:
		return nil, fmt.Errorf("unsupported mutation plan: %T", plan)
	}
}

func executeDropTable(p *planner.DropTablePlan, bp *buffer.BufferPool, cat *catalog.Catalog) (*Result, error) {
	err := cat.DropTable(p.Table)
	if err != nil {
		if p.IfExists && err == common.ErrTableNotFound {
			return &Result{RowsAffected: 0}, nil
		}
		return nil, err
	}
	_ = cat.Save()
	return &Result{RowsAffected: 0}, nil
}

func executeDropIndex(p *planner.DropIndexPlan, bp *buffer.BufferPool, cat *catalog.Catalog) (*Result, error) {
	err := cat.DropIndex(p.IndexName)
	if err != nil {
		if p.IfExists && err == common.ErrIndexNotFound {
			return &Result{RowsAffected: 0}, nil
		}
		return nil, err
	}
	_ = cat.Save()
	return &Result{RowsAffected: 0}, nil
}

func executeTruncateTable(p *planner.TruncateTablePlan, bp *buffer.BufferPool, cat *catalog.Catalog) (*Result, error) {
	tbl, exists := cat.GetTable(p.Table)
	if !exists {
		return nil, common.ErrTableNotFound
	}

	// Reinitialize tree root as empty leaf
	frame, err := bp.FetchPage(tbl.RootPageID)
	if err != nil {
		return nil, err
	}
	node := btree.NewNode(tbl.RootPageID, frame.Data)
	node.InitHeader(btree.PageTypeLeafTable, common.InvalidPageID)
	_ = bp.Unpin(tbl.RootPageID, true)

	_, err = cat.TruncateTable(p.Table)
	if err != nil {
		return nil, err
	}
	_ = cat.Save()
	return &Result{RowsAffected: 0}, nil
}

func executeAlterTable(p *planner.AlterTablePlan, bp *buffer.BufferPool, cat *catalog.Catalog) (*Result, error) {
	err := cat.AddColumn(p.Table, p.AddColumn)
	if err != nil {
		return nil, err
	}
	_ = cat.Save()
	return &Result{RowsAffected: 0}, nil
}

func executeCreateTable(p *planner.CreateTablePlan, bp *buffer.BufferPool, cat *catalog.Catalog) (*Result, error) {
	tree, err := btree.CreateTree(bp)
	if err != nil {
		return nil, fmt.Errorf("failed to create table btree: %w", err)
	}

	_, err = cat.CreateTable(p.Table, p.Columns, tree.RootPageID())
	if err != nil {
		return nil, err
	}

	if err := cat.Save(); err != nil {
		return nil, err
	}

	return &Result{RowsAffected: 0}, nil
}

func executeCreateIndex(p *planner.CreateIndexPlan, bp *buffer.BufferPool, cat *catalog.Catalog) (*Result, error) {
	tbl, exists := cat.GetTable(p.Table)
	if !exists {
		return nil, common.ErrTableNotFound
	}
	colIdx, _, found := tbl.FindColumn(p.Column)
	if !found {
		return nil, common.ErrColumnNotFound
	}

	idxTree, err := btree.CreateIndexTree(bp)
	if err != nil {
		return nil, fmt.Errorf("failed to create index btree: %w", err)
	}

	// Backfill existing rows into index
	tblTree := btree.NewTree(bp, tbl.RootPageID)
	cursor, err := btree.NewCursor(tblTree, 1)
	if err == nil {
		defer cursor.Close()
		for {
			rowID, payload, ok, err := cursor.Next()
			if err != nil || !ok {
				break
			}
			values, err := record.Decode(payload)
			if err != nil || colIdx >= len(values) {
				continue
			}
			colVal := values[colIdx]
			keyBytes, err := record.EncodeValues([]common.Value{colVal})
			if err == nil {
				_ = idxTree.Insert(keyBytes, rowID)
			}
		}
	}

	_, err = cat.CreateIndex(p.IndexName, p.Table, p.Column, idxTree.RootPageID(), p.Unique)
	if err != nil {
		return nil, err
	}

	if err := cat.Save(); err != nil {
		return nil, err
	}

	return &Result{RowsAffected: 0}, nil
}

func executeInsert(p *planner.InsertPlan, bp *buffer.BufferPool, cat *catalog.Catalog, tc *common.TraceContext, tx *transaction.Transaction) (*Result, error) {
	tblTree := btree.NewTree(bp, p.Table.RootPageID)
	indexes := cat.GetTableIndexes(p.Table.Name)

	var pkIdx int = -1
	for i, c := range p.Table.Columns {
		if c.PrimaryKey {
			pkIdx = i
			break
		}
	}

	var affected int64 = 0

	for _, rowVals := range p.Values {
		var rowID int64
		if pkIdx >= 0 && rowVals[pkIdx].Type == common.TypeInt && rowVals[pkIdx].Int > 0 {
			rowID = rowVals[pkIdx].Int
		} else {
			var err error
			rowID, err = cat.NextRowID(p.Table.Name)
			if err != nil {
				return nil, err
			}
			if pkIdx >= 0 {
				rowVals[pkIdx] = common.IntValue(rowID)
			}
		}

		payload, err := record.EncodeValues(rowVals)
		if err != nil {
			return nil, err
		}

		if err := tblTree.Insert(rowID, payload); err != nil {
			return nil, err
		}

		if tx != nil {
			tx.RecordInsert(p.Table.Name, p.Table.RootPageID, rowID)
		}

		// Insert into secondary indexes
		for _, idxDef := range indexes {
			cIdx, _, found := p.Table.FindColumn(idxDef.ColumnName)
			if found && cIdx < len(rowVals) {
				idxVal := rowVals[cIdx]
				keyBytes, err := record.EncodeValues([]common.Value{idxVal})
				if err == nil {
					idxTree := btree.NewIndexTree(bp, idxDef.RootPageID)
					_ = idxTree.Insert(keyBytes, rowID)
				}
			}
		}

		affected++
	}

	_ = cat.Save()
	return &Result{RowsAffected: affected}, nil
}

func executeUpdate(p *planner.UpdatePlan, bp *buffer.BufferPool, cat *catalog.Catalog, tc *common.TraceContext, tx *transaction.Transaction) (*Result, error) {
	tblTree := btree.NewTree(bp, p.Table.RootPageID)
	exec, _, err := BuildQueryExecutor(p.Scan, bp, tc)
	if err != nil {
		return nil, err
	}
	if err := exec.Init(); err != nil {
		return nil, err
	}
	defer exec.Close()

	var toUpdate []*Row
	for {
		row, err := exec.Next()
		if err != nil {
			return nil, err
		}
		if row == nil {
			break
		}
		cloned := make([]common.Value, len(row.Values))
		copy(cloned, row.Values)
		for colIdx, newVal := range p.Assignments {
			if colIdx < len(cloned) {
				cloned[colIdx] = newVal
			}
		}
		toUpdate = append(toUpdate, &Row{RowID: row.RowID, Values: cloned})
	}

	var affected int64 = 0
	for _, r := range toUpdate {
		if tx != nil {
			oldPayload, _ := tblTree.SeekRowid(r.RowID, nil)
			tx.RecordUpdate(p.Table.Name, p.Table.RootPageID, r.RowID, oldPayload)
		}

		payload, err := record.EncodeValues(r.Values)
		if err != nil {
			return nil, err
		}
		_ = tblTree.Delete(r.RowID)
		if err := tblTree.Insert(r.RowID, payload); err != nil {
			return nil, err
		}
		affected++
	}

	return &Result{RowsAffected: affected}, nil
}

func executeDelete(p *planner.DeletePlan, bp *buffer.BufferPool, cat *catalog.Catalog, tc *common.TraceContext, tx *transaction.Transaction) (*Result, error) {
	tblTree := btree.NewTree(bp, p.Table.RootPageID)
	exec, _, err := BuildQueryExecutor(p.Scan, bp, tc)
	if err != nil {
		return nil, err
	}
	if err := exec.Init(); err != nil {
		return nil, err
	}
	defer exec.Close()

	var toDelete []int64
	for {
		row, err := exec.Next()
		if err != nil {
			return nil, err
		}
		if row == nil {
			break
		}
		toDelete = append(toDelete, row.RowID)
	}

	var affected int64 = 0
	for _, rowID := range toDelete {
		if tx != nil {
			oldPayload, _ := tblTree.SeekRowid(rowID, nil)
			tx.RecordDelete(p.Table.Name, p.Table.RootPageID, rowID, oldPayload)
		}

		if err := tblTree.Delete(rowID); err == nil {
			affected++
		}
	}

	return &Result{RowsAffected: affected}, nil
}
