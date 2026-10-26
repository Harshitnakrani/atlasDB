package btree

import (
	"bytes"
	"encoding/binary"
	"errors"

	"atlasDB/internal/common"
)

// B-Tree Page Types matching research paper and SQLite specification.
const (
	PageTypeInteriorIndex byte = 0x02
	PageTypeInteriorTable byte = 0x05
	PageTypeLeafIndex     byte = 0x0A
	PageTypeLeafTable     byte = 0x0D

	NodeHeaderSize = 16
)

var (
	ErrNodeOverflow  = errors.New("atlas btree: node free space exhausted")
	ErrDuplicateKey  = errors.New("atlas btree: duplicate key")
	ErrCorruptNode   = errors.New("atlas btree: corrupt node page")
)

// TableLeafCell represents a row stored in a leaf table page.
type TableLeafCell struct {
	RowID   int64
	Payload []byte
}

// TableInteriorCell represents a routing entry in an internal table page.
type TableInteriorCell struct {
	Child common.PageID
	Key   int64 // Upper bound key for this child
}

// Node wraps a raw page byte slice and provides slotted-page manipulation.
type Node struct {
	pageID common.PageID
	data   []byte
}

func NewNode(pageID common.PageID, data []byte) *Node {
	return &Node{
		pageID: pageID,
		data:   data,
	}
}

// InitHeader initializes a fresh slotted node header.
func (n *Node) InitHeader(pageType byte, rightChildOrNext common.PageID) {
	pageSize := len(n.data)
	n.data[0] = pageType
	binary.BigEndian.PutUint16(n.data[1:3], 0)               // CellCount = 0
	binary.BigEndian.PutUint16(n.data[3:5], uint16(pageSize)) // CellStart = pageSize
	free := uint16(pageSize - NodeHeaderSize)
	binary.BigEndian.PutUint16(n.data[5:7], free)           // FreeBytes
	binary.BigEndian.PutUint64(n.data[8:16], uint64(rightChildOrNext))
}

func (n *Node) PageType() byte {
	return n.data[0]
}

func (n *Node) SetPageType(t byte) {
	n.data[0] = t
}

func (n *Node) IsLeaf() bool {
	return n.data[0] == PageTypeLeafTable || n.data[0] == PageTypeLeafIndex
}

func (n *Node) CellCount() int {
	return int(binary.BigEndian.Uint16(n.data[1:3]))
}

func (n *Node) setCellCount(cnt int) {
	binary.BigEndian.PutUint16(n.data[1:3], uint16(cnt))
}

func (n *Node) CellStart() uint16 {
	return binary.BigEndian.Uint16(n.data[3:5])
}

func (n *Node) setCellStart(offset uint16) {
	binary.BigEndian.PutUint16(n.data[3:5], offset)
}

func (n *Node) FreeBytes() uint16 {
	return binary.BigEndian.Uint16(n.data[5:7])
}

func (n *Node) setFreeBytes(free uint16) {
	binary.BigEndian.PutUint16(n.data[5:7], free)
}

func (n *Node) RightChildOrNext() common.PageID {
	return common.PageID(binary.BigEndian.Uint64(n.data[8:16]))
}

func (n *Node) SetRightChildOrNext(pgID common.PageID) {
	binary.BigEndian.PutUint64(n.data[8:16], uint64(pgID))
}

// Pointer array helpers
func (n *Node) getCellOffset(index int) uint16 {
	ptrPos := NodeHeaderSize + index*2
	return binary.BigEndian.Uint16(n.data[ptrPos : ptrPos+2])
}

func (n *Node) setCellOffset(index int, offset uint16) {
	ptrPos := NodeHeaderSize + index*2
	binary.BigEndian.PutUint16(n.data[ptrPos:ptrPos+2], offset)
}

// Table Leaf Cell operations
func (n *Node) GetTableLeafCell(index int) TableLeafCell {
	offset := n.getCellOffset(index)
	rowID := int64(binary.BigEndian.Uint64(n.data[offset : offset+8]))
	payloadLen := binary.BigEndian.Uint32(n.data[offset+8 : offset+12])
	payload := n.data[offset+12 : offset+12+uint16(payloadLen)]
	return TableLeafCell{
		RowID:   rowID,
		Payload: payload,
	}
}

// Table Interior Cell operations
func (n *Node) GetTableInteriorCell(index int) TableInteriorCell {
	offset := n.getCellOffset(index)
	child := common.PageID(binary.BigEndian.Uint64(n.data[offset : offset+8]))
	key := int64(binary.BigEndian.Uint64(n.data[offset+8 : offset+16]))
	return TableInteriorCell{
		Child: child,
		Key:   key,
	}
}

// FindKeyInLeaf performs binary search over leaf table cells for rowID.
// Returns (index, true) if found; (insertIndex, false) if not found.
func (n *Node) FindKeyInLeaf(rowID int64) (int, bool) {
	count := n.CellCount()
	low, high := 0, count-1
	for low <= high {
		mid := (low + high) / 2
		cell := n.GetTableLeafCell(mid)
		if cell.RowID == rowID {
			return mid, true
		} else if cell.RowID < rowID {
			low = mid + 1
		} else {
			high = mid - 1
		}
	}
	return low, false
}

// FindChildInInterior finds which child page pointer to follow for the given rowID.
// In interior table nodes: keys are separators. If rowID <= cell.Key, follow cell.Child.
// If greater than all cell keys, follow RightChild.
func (n *Node) FindChildInInterior(rowID int64) common.PageID {
	count := n.CellCount()
	for i := 0; i < count; i++ {
		cell := n.GetTableInteriorCell(i)
		if rowID <= cell.Key {
			return cell.Child
		}
	}
	return n.RightChildOrNext()
}

// AvailableSpace returns contiguous bytes available for inserting a new cell + pointer.
func (n *Node) AvailableSpace() int {
	headerUsed := NodeHeaderSize + n.CellCount()*2
	cellStart := int(n.CellStart())
	if cellStart < headerUsed {
		return 0
	}
	return cellStart - headerUsed
}

// InsertTableLeafCell inserts a TableLeafCell maintaining sorted RowID order.
func (n *Node) InsertTableLeafCell(rowID int64, payload []byte) error {
	needed := 2 + 8 + 4 + len(payload) // 2-byte pointer + 8-byte rowid + 4-byte len + payload
	if n.AvailableSpace() < needed {
		n.Compact()
		if n.AvailableSpace() < needed {
			return ErrNodeOverflow
		}
	}

	idx, found := n.FindKeyInLeaf(rowID)
	if found {
		return ErrDuplicateKey
	}

	// Allocate cell at bottom of page
	newCellStart := n.CellStart() - uint16(8+4+len(payload))
	n.setCellStart(newCellStart)

	// Write cell data
	binary.BigEndian.PutUint64(n.data[newCellStart:newCellStart+8], uint64(rowID))
	binary.BigEndian.PutUint32(n.data[newCellStart+8:newCellStart+12], uint32(len(payload)))
	copy(n.data[newCellStart+12:], payload)

	// Shift cell pointers to make room at idx
	count := n.CellCount()
	for i := count; i > idx; i-- {
		n.setCellOffset(i, n.getCellOffset(i-1))
	}
	n.setCellOffset(idx, newCellStart)

	n.setCellCount(count + 1)
	n.setFreeBytes(uint16(n.AvailableSpace()))
	return nil
}

// InsertTableInteriorCell inserts an interior routing cell maintaining sorted key order.
func (n *Node) InsertTableInteriorCell(child common.PageID, key int64) error {
	needed := 2 + 8 + 8 // 2-byte pointer + 8-byte child + 8-byte key = 18 bytes
	if n.AvailableSpace() < needed {
		n.Compact()
		if n.AvailableSpace() < needed {
			return ErrNodeOverflow
		}
	}

	count := n.CellCount()
	idx := 0
	for idx < count {
		cell := n.GetTableInteriorCell(idx)
		if key <= cell.Key {
			break
		}
		idx++
	}

	newCellStart := n.CellStart() - 16
	n.setCellStart(newCellStart)

	binary.BigEndian.PutUint64(n.data[newCellStart:newCellStart+8], uint64(child))
	binary.BigEndian.PutUint64(n.data[newCellStart+8:newCellStart+16], uint64(key))

	for i := count; i > idx; i-- {
		n.setCellOffset(i, n.getCellOffset(i-1))
	}
	n.setCellOffset(idx, newCellStart)

	n.setCellCount(count + 1)
	n.setFreeBytes(uint16(n.AvailableSpace()))
	return nil
}

// DeleteTableLeafCell removes the cell with the given rowID and compacts pointers.
func (n *Node) DeleteTableLeafCell(rowID int64) error {
	idx, found := n.FindKeyInLeaf(rowID)
	if !found {
		return common.ErrNotFound
	}

	count := n.CellCount()
	for i := idx; i < count-1; i++ {
		n.setCellOffset(i, n.getCellOffset(i+1))
	}
	n.setCellCount(count - 1)
	n.setFreeBytes(uint16(n.AvailableSpace()))
	// Defragmentation / compaction can be performed when space is needed
	return nil
}

// Compact defragments cells in the page, recovering unused space from deletions.
func (n *Node) Compact() {
	count := n.CellCount()
	if count == 0 {
		n.setCellStart(uint16(len(n.data)))
		n.setFreeBytes(uint16(len(n.data) - NodeHeaderSize))
		return
	}

	if n.PageType() == PageTypeLeafTable {
		cells := make([]TableLeafCell, count)
		for i := 0; i < count; i++ {
			cells[i] = n.GetTableLeafCell(i)
		}
		n.InitHeader(PageTypeLeafTable, n.RightChildOrNext())
		for _, cell := range cells {
			_ = n.InsertTableLeafCell(cell.RowID, cell.Payload)
		}
	} else if n.PageType() == PageTypeInteriorTable {
		cells := make([]TableInteriorCell, count)
		for i := 0; i < count; i++ {
			cells[i] = n.GetTableInteriorCell(i)
		}
		n.InitHeader(PageTypeInteriorTable, n.RightChildOrNext())
		for _, cell := range cells {
			_ = n.InsertTableInteriorCell(cell.Child, cell.Key)
		}
	}
}

// Index Cells for Secondary Indexes
type IndexLeafCell struct {
	Key   []byte
	RowID int64
}

type IndexInteriorCell struct {
	Child common.PageID
	Key   []byte
}

func (n *Node) GetIndexLeafCell(index int) IndexLeafCell {
	offset := n.getCellOffset(index)
	rowID := int64(binary.BigEndian.Uint64(n.data[offset : offset+8]))
	keyLen := binary.BigEndian.Uint16(n.data[offset+8 : offset+10])
	key := n.data[offset+10 : offset+10+keyLen]
	return IndexLeafCell{
		RowID: rowID,
		Key:   key,
	}
}

func (n *Node) FindIndexKeyInLeaf(key []byte, rowID int64) (int, bool) {
	count := n.CellCount()
	low, high := 0, count-1
	for low <= high {
		mid := (low + high) / 2
		cell := n.GetIndexLeafCell(mid)
		cmp := bytes.Compare(cell.Key, key)
		if cmp == 0 {
			if cell.RowID == rowID {
				return mid, true
			} else if cell.RowID < rowID {
				low = mid + 1
			} else {
				high = mid - 1
			}
		} else if cmp < 0 {
			low = mid + 1
		} else {
			high = mid - 1
		}
	}
	return low, false
}

func (n *Node) InsertIndexLeafCell(key []byte, rowID int64) error {
	needed := 2 + 8 + 2 + len(key)
	if n.AvailableSpace() < needed {
		return ErrNodeOverflow
	}

	idx, found := n.FindIndexKeyInLeaf(key, rowID)
	if found {
		return ErrDuplicateKey
	}

	newCellStart := n.CellStart() - uint16(8+2+len(key))
	n.setCellStart(newCellStart)

	binary.BigEndian.PutUint64(n.data[newCellStart:newCellStart+8], uint64(rowID))
	binary.BigEndian.PutUint16(n.data[newCellStart+8:newCellStart+10], uint16(len(key)))
	copy(n.data[newCellStart+10:], key)

	count := n.CellCount()
	for i := count; i > idx; i-- {
		n.setCellOffset(i, n.getCellOffset(i-1))
	}
	n.setCellOffset(idx, newCellStart)

	n.setCellCount(count + 1)
	n.setFreeBytes(uint16(n.AvailableSpace()))
	return nil
}
