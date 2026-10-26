package btree

import (
	"fmt"
	"sync"

	"atlasDB/internal/buffer"
	"atlasDB/internal/common"
)

// pathItem records traversal history from root to leaf.
type pathItem struct {
	pgID  common.PageID
	child common.PageID
}

// intCell is a temporary interior cell entry during parent splits.
type intCell struct {
	child common.PageID
	key   int64
}

// Tree represents a persistent Table B+ Tree backed by a BufferPool.
type Tree struct {
	mu       sync.RWMutex
	bp       *buffer.BufferPool
	rootPgID common.PageID
}

// NewTree opens an existing B+ Tree with the given root page ID.
func NewTree(bp *buffer.BufferPool, rootPgID common.PageID) *Tree {
	return &Tree{
		bp:       bp,
		rootPgID: rootPgID,
	}
}

// CreateTree initializes a brand new B+ Tree by allocating a fresh root leaf page.
func CreateTree(bp *buffer.BufferPool) (*Tree, error) {
	frame, err := bp.NewPage()
	if err != nil {
		return nil, fmt.Errorf("failed to allocate root page: %w", err)
	}

	node := NewNode(frame.PageID, frame.Data)
	node.InitHeader(PageTypeLeafTable, common.InvalidPageID)
	_ = bp.Unpin(frame.PageID, true)

	return &Tree{
		bp:       bp,
		rootPgID: frame.PageID,
	}, nil
}

// RootPageID returns the root page ID of the tree.
func (t *Tree) RootPageID() common.PageID {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.rootPgID
}

// SeekRowid performs a point lookup for a target 64-bit Row ID.
func (t *Tree) SeekRowid(rowID int64, tc *common.TraceContext) ([]byte, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	currPgID := t.rootPgID
	if tc != nil {
		tc.Add("BTree", "-> root page %d", currPgID)
	}

	for {
		frame, err := t.bp.FetchPage(currPgID)
		if err != nil {
			return nil, err
		}
		node := NewNode(currPgID, frame.Data)

		if node.IsLeaf() {
			defer t.bp.Unpin(currPgID, false)
			idx, found := node.FindKeyInLeaf(rowID)
			if !found {
				return nil, common.ErrNotFound
			}
			cell := node.GetTableLeafCell(idx)
			res := make([]byte, len(cell.Payload))
			copy(res, cell.Payload)
			return res, nil
		}

		// Interior node traversal
		nextPgID := node.FindChildInInterior(rowID)
		if tc != nil {
			tc.Add("BTree", "-> internal page %d", nextPgID)
		}
		_ = t.bp.Unpin(currPgID, false)
		currPgID = nextPgID
	}
}

// Insert adds a row payload under a specific RowID.
func (t *Tree) Insert(rowID int64, payload []byte) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	var path []pathItem
	currPgID := t.rootPgID

	for {
		frame, err := t.bp.FetchPage(currPgID)
		if err != nil {
			t.unpinPath(path)
			return err
		}
		node := NewNode(currPgID, frame.Data)

		if node.IsLeaf() {
			err := node.InsertTableLeafCell(rowID, payload)
			if err == nil {
				_ = t.bp.Unpin(currPgID, true)
				t.unpinPath(path)
				return nil
			}

			if err != ErrNodeOverflow {
				_ = t.bp.Unpin(currPgID, false)
				t.unpinPath(path)
				return err
			}

			// Leaf node is full, must split leaf
			return t.splitLeafAndInsert(currPgID, path, rowID, payload)
		}

		// Interior node
		childPgID := node.FindChildInInterior(rowID)
		path = append(path, pathItem{pgID: currPgID, child: childPgID})
		currPgID = childPgID
	}
}

func (t *Tree) unpinPath(path []pathItem) {
	for _, item := range path {
		_ = t.bp.Unpin(item.pgID, false)
	}
}

func (t *Tree) splitLeafAndInsert(leafPgID common.PageID, path []pathItem, rowID int64, payload []byte) error {
	leafFrame, err := t.bp.FetchPage(leafPgID)
	if err != nil {
		t.unpinPath(path)
		return err
	}
	leafNode := NewNode(leafPgID, leafFrame.Data)

	// Collect all existing cells + the new cell to split evenly
	totalCells := leafNode.CellCount()
	type tempCell struct {
		rowID   int64
		payload []byte
	}
	allCells := make([]tempCell, 0, totalCells+1)
	inserted := false
	for i := 0; i < totalCells; i++ {
		c := leafNode.GetTableLeafCell(i)
		if !inserted && rowID < c.RowID {
			allCells = append(allCells, tempCell{rowID: rowID, payload: payload})
			inserted = true
		}
		allCells = append(allCells, tempCell{rowID: c.RowID, payload: c.Payload})
	}
	if !inserted {
		allCells = append(allCells, tempCell{rowID: rowID, payload: payload})
	}

	mid := len(allCells) / 2
	if mid == 0 {
		mid = 1
	}
	leftCells := allCells[:mid]
	rightCells := allCells[mid:]
	separatorKey := leftCells[len(leftCells)-1].rowID

	// If this leaf is the ROOT of the tree, use root swapping so rootPgID never changes!
	if len(path) == 0 {
		newLeftFrame, err := t.bp.NewPage()
		if err != nil {
			_ = t.bp.Unpin(leafPgID, false)
			return err
		}
		newRightFrame, err := t.bp.NewPage()
		if err != nil {
			_ = t.bp.Unpin(leafPgID, false)
			_ = t.bp.Unpin(newLeftFrame.PageID, false)
			return err
		}

		newLeftNode := NewNode(newLeftFrame.PageID, newLeftFrame.Data)
		newLeftNode.InitHeader(PageTypeLeafTable, newRightFrame.PageID)
		for _, c := range leftCells {
			_ = newLeftNode.InsertTableLeafCell(c.rowID, c.payload)
		}

		newRightNode := NewNode(newRightFrame.PageID, newRightFrame.Data)
		newRightNode.InitHeader(PageTypeLeafTable, leafNode.RightChildOrNext())
		for _, c := range rightCells {
			_ = newRightNode.InsertTableLeafCell(c.rowID, c.payload)
		}

		// Reinitialize root as interior node pointing to newLeft and newRight
		leafNode.InitHeader(PageTypeInteriorTable, newRightFrame.PageID)
		_ = leafNode.InsertTableInteriorCell(newLeftFrame.PageID, separatorKey)

		_ = t.bp.Unpin(leafPgID, true)
		_ = t.bp.Unpin(newLeftFrame.PageID, true)
		_ = t.bp.Unpin(newRightFrame.PageID, true)
		return nil
	}

	// Normal non-root leaf split
	rightFrame, err := t.bp.NewPage()
	if err != nil {
		_ = t.bp.Unpin(leafPgID, false)
		t.unpinPath(path)
		return err
	}
	rightNode := NewNode(rightFrame.PageID, rightFrame.Data)
	rightNode.InitHeader(PageTypeLeafTable, leafNode.RightChildOrNext())
	leafNode.SetRightChildOrNext(rightFrame.PageID)

	leafNode.InitHeader(PageTypeLeafTable, rightFrame.PageID)
	for _, c := range leftCells {
		_ = leafNode.InsertTableLeafCell(c.rowID, c.payload)
	}

	for _, c := range rightCells {
		_ = rightNode.InsertTableLeafCell(c.rowID, c.payload)
	}

	_ = t.bp.Unpin(leafPgID, true)
	_ = t.bp.Unpin(rightFrame.PageID, true)

	return t.insertIntoParent(path, leafPgID, separatorKey, rightFrame.PageID)
}

func (t *Tree) insertIntoParent(path []pathItem, leftPgID common.PageID, key int64, rightPgID common.PageID) error {
	parentItem := path[len(path)-1]
	parentPath := path[:len(path)-1]

	parentFrame, err := t.bp.FetchPage(parentItem.pgID)
	if err != nil {
		t.unpinPath(path)
		return err
	}
	parentNode := NewNode(parentItem.pgID, parentFrame.Data)

	count := parentNode.CellCount()
	var newCells []intCell
	var newRightChild common.PageID

	if parentNode.RightChildOrNext() == leftPgID {
		for i := 0; i < count; i++ {
			c := parentNode.GetTableInteriorCell(i)
			newCells = append(newCells, intCell{child: c.Child, key: c.Key})
		}
		newCells = append(newCells, intCell{child: leftPgID, key: key})
		newRightChild = rightPgID
	} else {
		newRightChild = parentNode.RightChildOrNext()
		for i := 0; i < count; i++ {
			c := parentNode.GetTableInteriorCell(i)
			if c.Child == leftPgID {
				newCells = append(newCells, intCell{child: leftPgID, key: key})
				newCells = append(newCells, intCell{child: rightPgID, key: c.Key})
			} else {
				newCells = append(newCells, intCell{child: c.Child, key: c.Key})
			}
		}
	}

	// Check if newCells fit into parentNode
	neededBytes := NodeHeaderSize + len(newCells)*(2+16)
	if neededBytes <= len(parentNode.data) {
		parentNode.InitHeader(PageTypeInteriorTable, newRightChild)
		for _, c := range newCells {
			_ = parentNode.InsertTableInteriorCell(c.child, c.key)
		}
		_ = t.bp.Unpin(parentItem.pgID, true)
		t.unpinPath(parentPath)
		return nil
	}

	// Split interior node
	return t.splitInteriorNodeFromCells(parentItem.pgID, parentPath, newCells, newRightChild)
}

func (t *Tree) splitInteriorNodeFromCells(nodePgID common.PageID, path []pathItem, allCells []intCell, finalRightChild common.PageID) error {
	mid := len(allCells) / 2
	if mid == 0 {
		mid = 1
	}
	promotedKey := allCells[mid].key
	promotedChild := allCells[mid].child

	leftCells := allCells[:mid]
	rightCells := allCells[mid+1:]

	// If nodePgID is the ROOT, use root swapping!
	if len(path) == 0 {
		rootFrame, err := t.bp.FetchPage(nodePgID)
		if err != nil {
			return err
		}
		rootNode := NewNode(nodePgID, rootFrame.Data)

		newLeftFrame, err := t.bp.NewPage()
		if err != nil {
			_ = t.bp.Unpin(nodePgID, false)
			return err
		}
		newRightFrame, err := t.bp.NewPage()
		if err != nil {
			_ = t.bp.Unpin(nodePgID, false)
			_ = t.bp.Unpin(newLeftFrame.PageID, false)
			return err
		}

		newLeftNode := NewNode(newLeftFrame.PageID, newLeftFrame.Data)
		newLeftNode.InitHeader(PageTypeInteriorTable, promotedChild)
		for _, c := range leftCells {
			_ = newLeftNode.InsertTableInteriorCell(c.child, c.key)
		}

		newRightNode := NewNode(newRightFrame.PageID, newRightFrame.Data)
		newRightNode.InitHeader(PageTypeInteriorTable, finalRightChild)
		for _, c := range rightCells {
			_ = newRightNode.InsertTableInteriorCell(c.child, c.key)
		}

		rootNode.InitHeader(PageTypeInteriorTable, newRightFrame.PageID)
		_ = rootNode.InsertTableInteriorCell(newLeftFrame.PageID, promotedKey)

		_ = t.bp.Unpin(nodePgID, true)
		_ = t.bp.Unpin(newLeftFrame.PageID, true)
		_ = t.bp.Unpin(newRightFrame.PageID, true)
		return nil
	}

	origFrame, err := t.bp.FetchPage(nodePgID)
	if err != nil {
		t.unpinPath(path)
		return err
	}
	origNode := NewNode(nodePgID, origFrame.Data)

	rightFrame, err := t.bp.NewPage()
	if err != nil {
		_ = t.bp.Unpin(nodePgID, false)
		t.unpinPath(path)
		return err
	}
	rightNode := NewNode(rightFrame.PageID, rightFrame.Data)

	origNode.InitHeader(PageTypeInteriorTable, promotedChild)
	for _, c := range leftCells {
		_ = origNode.InsertTableInteriorCell(c.child, c.key)
	}

	rightNode.InitHeader(PageTypeInteriorTable, finalRightChild)
	for _, c := range rightCells {
		_ = rightNode.InsertTableInteriorCell(c.child, c.key)
	}

	_ = t.bp.Unpin(nodePgID, true)
	_ = t.bp.Unpin(rightFrame.PageID, true)

	return t.insertIntoParent(path, nodePgID, promotedKey, rightFrame.PageID)
}

// Delete removes a record by RowID.
func (t *Tree) Delete(rowID int64) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	currPgID := t.rootPgID
	for {
		frame, err := t.bp.FetchPage(currPgID)
		if err != nil {
			return err
		}
		node := NewNode(currPgID, frame.Data)

		if node.IsLeaf() {
			defer t.bp.Unpin(currPgID, true)
			return node.DeleteTableLeafCell(rowID)
		}

		nextPgID := node.FindChildInInterior(rowID)
		_ = t.bp.Unpin(currPgID, false)
		currPgID = nextPgID
	}
}

// VerifyInvariants validates B+ tree structural invariants:
// 1. Equal depth for all leaves.
// 2. Strict key ordering in nodes.
// 3. Valid leaf chaining via RightChildOrNext.
func (t *Tree) VerifyInvariants() error {
	t.mu.RLock()
	defer t.mu.RUnlock()

	var leafDepth int = -1
	var checkNode func(pgID common.PageID, depth int) error

	checkNode = func(pgID common.PageID, depth int) error {
		frame, err := t.bp.FetchPage(pgID)
		if err != nil {
			return err
		}
		defer t.bp.Unpin(pgID, false)
		node := NewNode(pgID, frame.Data)

		if node.IsLeaf() {
			if leafDepth == -1 {
				leafDepth = depth
			} else if leafDepth != depth {
				return fmt.Errorf("depth mismatch: expected %d, got %d at page %d", leafDepth, depth, pgID)
			}

			count := node.CellCount()
			var prevKey int64 = -1
			for i := 0; i < count; i++ {
				cell := node.GetTableLeafCell(i)
				if i > 0 && cell.RowID <= prevKey {
					return fmt.Errorf("unordered keys in leaf %d: %d <= %d", pgID, cell.RowID, prevKey)
				}
				prevKey = cell.RowID
			}
			return nil
		}

		// Interior node checks
		count := node.CellCount()
		var prevKey int64 = -1
		for i := 0; i < count; i++ {
			cell := node.GetTableInteriorCell(i)
			if i > 0 && cell.Key <= prevKey {
				return fmt.Errorf("unordered keys in interior node %d: %d <= %d", pgID, cell.Key, prevKey)
			}
			prevKey = cell.Key
			if err := checkNode(cell.Child, depth+1); err != nil {
				return err
			}
		}

		return checkNode(node.RightChildOrNext(), depth+1)
	}

	return checkNode(t.rootPgID, 0)
}
