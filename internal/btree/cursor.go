package btree

import (
	"atlasDB/internal/buffer"
	"atlasDB/internal/common"
)

// Cursor provides sequential range scanning over a Table B+ Tree.
type Cursor struct {
	tree      *Tree
	bp        *buffer.BufferPool
	currPgID  common.PageID
	currIndex int
	currFrame *buffer.Frame
	eof       bool
}

// NewCursor creates a cursor positioned at the first record with rowID >= startRowID.
func NewCursor(tree *Tree, startRowID int64) (*Cursor, error) {
	bp := tree.bp
	currPgID := tree.RootPageID()

	for {
		frame, err := bp.FetchPage(currPgID)
		if err != nil {
			return nil, err
		}
		node := NewNode(currPgID, frame.Data)

		if node.IsLeaf() {
			idx, _ := node.FindKeyInLeaf(startRowID)
			c := &Cursor{
				tree:      tree,
				bp:        bp,
				currPgID:  currPgID,
				currIndex: idx,
				currFrame: frame,
			}
			// If index is past the end of this page, advance to next page if available
			if idx >= node.CellCount() {
				nextPg := node.RightChildOrNext()
				if nextPg == common.InvalidPageID || nextPg == 0 {
					c.eof = true
				} else {
					_ = bp.Unpin(currPgID, false)
					nextFrame, err := bp.FetchPage(nextPg)
					if err != nil {
						c.eof = true
						return c, nil
					}
					c.currPgID = nextPg
					c.currIndex = 0
					c.currFrame = nextFrame
				}
			}
			return c, nil
		}

		nextPgID := node.FindChildInInterior(startRowID)
		_ = bp.Unpin(currPgID, false)
		currPgID = nextPgID
	}
}

// Next yields the next TableLeafCell in the scan.
func (c *Cursor) Next() (int64, []byte, bool, error) {
	if c.eof || c.currFrame == nil {
		return 0, nil, false, nil
	}

	node := NewNode(c.currPgID, c.currFrame.Data)
	if c.currIndex >= node.CellCount() {
		// Advance to next leaf page
		nextPg := node.RightChildOrNext()
		if nextPg == common.InvalidPageID || nextPg == 0 {
			c.eof = true
			_ = c.Close()
			return 0, nil, false, nil
		}

		_ = c.bp.Unpin(c.currPgID, false)
		nextFrame, err := c.bp.FetchPage(nextPg)
		if err != nil {
			c.eof = true
			return 0, nil, false, err
		}
		c.currPgID = nextPg
		c.currIndex = 0
		c.currFrame = nextFrame
		node = NewNode(c.currPgID, c.currFrame.Data)
		if node.CellCount() == 0 {
			c.eof = true
			_ = c.Close()
			return 0, nil, false, nil
		}
	}

	cell := node.GetTableLeafCell(c.currIndex)
	c.currIndex++
	return cell.RowID, cell.Payload, true, nil
}

// Close releases any pinned buffer frames held by the cursor.
func (c *Cursor) Close() error {
	if c.currFrame != nil {
		_ = c.bp.Unpin(c.currPgID, false)
		c.currFrame = nil
	}
	return nil
}
