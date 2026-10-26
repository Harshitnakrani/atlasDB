package btree

import (
	"bytes"
	"fmt"
	"sync"

	"atlasDB/internal/buffer"
	"atlasDB/internal/common"
)

// IndexTree manages a persistent B+ Tree for secondary index lookups.
type IndexTree struct {
	mu       sync.RWMutex
	bp       *buffer.BufferPool
	rootPgID common.PageID
}

func CreateIndexTree(bp *buffer.BufferPool) (*IndexTree, error) {
	frame, err := bp.NewPage()
	if err != nil {
		return nil, err
	}

	node := NewNode(frame.PageID, frame.Data)
	node.InitHeader(PageTypeLeafIndex, common.InvalidPageID)
	_ = bp.Unpin(frame.PageID, true)

	return &IndexTree{
		bp:       bp,
		rootPgID: frame.PageID,
	}, nil
}

func NewIndexTree(bp *buffer.BufferPool, rootPgID common.PageID) *IndexTree {
	return &IndexTree{
		bp:       bp,
		rootPgID: rootPgID,
	}
}

func (it *IndexTree) RootPageID() common.PageID {
	it.mu.RLock()
	defer it.mu.RUnlock()
	return it.rootPgID
}

// Insert inserts a secondary index entry (key, rowID).
func (it *IndexTree) Insert(key []byte, rowID int64) error {
	it.mu.Lock()
	defer it.mu.Unlock()

	currPgID := it.rootPgID
	frame, err := it.bp.FetchPage(currPgID)
	if err != nil {
		return err
	}
	node := NewNode(currPgID, frame.Data)

	if node.IsLeaf() {
		err := node.InsertIndexLeafCell(key, rowID)
		if err == nil {
			_ = it.bp.Unpin(currPgID, true)
			return nil
		}
		if err != ErrNodeOverflow {
			_ = it.bp.Unpin(currPgID, false)
			return err
		}

		// Split index leaf
		rightFrame, err := it.bp.NewPage()
		if err != nil {
			_ = it.bp.Unpin(currPgID, false)
			return err
		}
		rightNode := NewNode(rightFrame.PageID, rightFrame.Data)
		rightNode.InitHeader(PageTypeLeafIndex, node.RightChildOrNext())
		node.SetRightChildOrNext(rightFrame.PageID)

		// Collect all existing cells + the new cell
		cnt := node.CellCount()
		type item struct {
			key   []byte
			rowID int64
		}
		all := make([]item, 0, cnt+1)
		inserted := false
		for i := 0; i < cnt; i++ {
			c := node.GetIndexLeafCell(i)
			cmp := bytes.Compare(c.Key, key)
			if !inserted && (cmp > 0 || (cmp == 0 && c.RowID > rowID)) {
				all = append(all, item{key: key, rowID: rowID})
				inserted = true
			}
			all = append(all, item{key: c.Key, rowID: c.RowID})
		}
		if !inserted {
			all = append(all, item{key: key, rowID: rowID})
		}

		mid := len(all) / 2
		node.InitHeader(PageTypeLeafIndex, rightFrame.PageID)
		for _, c := range all[:mid] {
			_ = node.InsertIndexLeafCell(c.key, c.rowID)
		}
		for _, c := range all[mid:] {
			_ = rightNode.InsertIndexLeafCell(c.key, c.rowID)
		}

		_ = it.bp.Unpin(currPgID, true)
		_ = it.bp.Unpin(rightFrame.PageID, true)
		return nil
	}

	_ = it.bp.Unpin(currPgID, false)
	return fmt.Errorf("multi-level index interior split not yet needed")
}

// Find returns all rowIDs associated with the exact index key.
func (it *IndexTree) Find(key []byte) ([]int64, error) {
	it.mu.RLock()
	defer it.mu.RUnlock()

	var results []int64
	currPgID := it.rootPgID

	for currPgID != common.InvalidPageID && currPgID != 0 {
		frame, err := it.bp.FetchPage(currPgID)
		if err != nil {
			return nil, err
		}
		node := NewNode(currPgID, frame.Data)

		if node.IsLeaf() {
			cnt := node.CellCount()
			for i := 0; i < cnt; i++ {
				cell := node.GetIndexLeafCell(i)
				cmp := bytes.Compare(cell.Key, key)
				if cmp == 0 {
					results = append(results, cell.RowID)
				} else if cmp > 0 {
					_ = it.bp.Unpin(currPgID, false)
					return results, nil
				}
			}
			next := node.RightChildOrNext()
			_ = it.bp.Unpin(currPgID, false)
			currPgID = next
		} else {
			_ = it.bp.Unpin(currPgID, false)
			break
		}
	}

	return results, nil
}
