# B+ Tree Architecture & Implementation Plan


## 1. Executive Summary & Core Concepts

A **B+ Tree** is a self-balancing $m$-way search tree optimized for systems that read and write large blocks of memory (such as disk pages). In `atlasDB`, the B+ Tree serves as the primary storage engine for all database tables and indexes.

### 1.1 Standard B-Tree vs. B+ Tree

| Feature | Standard B-Tree | B+ Tree (`atlasDB` Choice) |
| :--- | :--- | :--- |
| **Data / Payload Storage** | Stored in both internal and leaf nodes | Stored **exclusively in leaf nodes** |
| **Internal Nodes** | Contain keys and record payloads | Contain **only routing keys and child page pointers** |
| **Leaf Node Connections** | No pointers between leaves | Doubly/Singly linked list connecting leaf nodes |
| **Range Scan Performance** | Requires full tree traversal ($O(N \log N)$) | Sequential traversal via leaf links ($O(N)$) |
| **Fan-Out (Capacity/Page)** | Lower (payload consumes page space) | **Much Higher** (internal nodes only store keys + pointers) |

---

## 2. Disk Page Layout & Slotted Page Architecture

In `atlasDB`, disk space is managed in fixed-size pages (default: **4096 bytes**). Both internal and leaf nodes must fit within a single page frame managed by the **Pager** (`pkg/pager`).

To handle variable-length row records without fragmentation, `atlasDB` uses a **Slotted Page Layout**.

```
+-----------------------------------------------------------------------+
|                           4096-BYTE DISK PAGE                         |
+-----------------------------------------------------------------------+
| Page Header (Flags, Cell Count, Free Space Offset, Right Child/Next) |
+-----------------------------------------------------------------------+
| Cell Pointer Array [ offset_0 | offset_1 | offset_2 | ... ]           |
|   (Grows Forward ---->)                                               |
+-----------------------------------------------------------------------+
|                           UNALLOCATED SPACE                           |
|                                                                       |
+-----------------------------------------------------------------------+
|   (<---- Grows Backward)                                              |
| [ Cell 2 Payload ]  [ Cell 1 Payload ]  [ Cell 0 Payload ]             |
+-----------------------------------------------------------------------+
```

### 2.1 Page Header Fields (`12-16 bytes`)
- `PageType` (1 byte): `0x0D` = Leaf Table Page, `0x05` = Internal Table Page, `0x0A` = Leaf Index Page, `0x02` = Internal Index Page.
- `CellCount` (2 bytes): Number of cells/records in this page.
- `CellStart` (2 bytes): Byte offset to the start of the cell payload area.
- `FreeBytes` (2 bytes): Amount of free unallocated bytes on the page.
- `RightChild` / `NextLeaf` (4 bytes):
  - In internal nodes: Pointer to the rightmost child page.
  - In leaf nodes: Pointer (Page ID) to the next leaf page for sequential scans.

### 2.2 Internal Node vs. Leaf Node Payload

1. **Leaf Node Cell Layout**:
   - `RowID` (varint 1-9 bytes): 64-bit integer primary key.
   - `PayloadSize` (varint 1-9 bytes): Byte size of the record data.
   - `Payload` (bytes): Dynamic SQLite record (Header + Body columns).

2. **Internal Node Cell Layout**:
   - `ChildPageID` (4 bytesuint32): Page number of the left child branch.
   - `Key` (varint): Separator key value (`rowid`).

---

## 3. How `atlasDB` Uses B+ Trees

`atlasDB` relies on B+ Trees for two primary system responsibilities:

### 3.1 Table B+ Trees (Primary Data Storage)
- Every user table (e.g., `CREATE TABLE users ...`) is represented by a dedicated Table B+ Tree.
- **Key**: 64-bit signed integer `rowid` (either explicitly defined via `INTEGER PRIMARY KEY` or implicitly generated).
- **Value**: The full row tuple encoded in SQLite's binary record format.
- **Root Page**: Stored in system metadata table (`sqlite_schema`).

### 3.2 Index B-Trees (Secondary Indexes)
- Created via `CREATE INDEX` queries.
- **Key**: Composite payload of indexed column value(s) + the target row's `rowid`.
- **Value**: None (or empty). Searching the index yields the `rowid`, which is then used to probe the Table B+ Tree (or avoided if the query is a *covering index query*).

---

## 4. Key Operations & Algorithms

### 4.1 Point Search (`SeekRowid`)
1. Start at the Root Page.
2. Read the page header and binary-search the **Cell Pointer Array** for `targetRowID`.
3. If page is an **Internal Node**:
   - Find child pointer corresponding to the key range: $\text{Keys}[i] \le \text{targetRowID} < \text{Keys}[i+1]$.
   - Fetch child page from `Pager` and repeat step 2.
4. If page is a **Leaf Node**:
   - Search cell array. If `cell.RowID == targetRowID`, decode payload; otherwise return `ErrNotFound`.

### 4.2 Range Scans & Iteration
1. Perform Point Search for `startRowID` to locate starting Leaf Page and cell index.
2. Yield cell data to the Virtual Machine (VDBE).
3. Increment cell index. When reaching the end of the cell array, read `NextLeaf` page pointer in the header.
4. Fetch next Leaf Page from `Pager` and continue sequentially.

### 4.3 Node Insertion & Splitting
1. Traverse down to the target Leaf Page.
2. Encode cell payload and calculate required bytes (`CellPointer` + `CellPayload`).
3. If page has sufficient free space:
   - Insert payload at `CellStart - PayloadSize`, add offset to `CellPointer` array, update `CellCount` and `CellStart`.
4. If page is **Full (Overflow)**:
   - **Split Page**: Allocate a new page via `Pager`.
   - Distribute cells 50/50 between current page and new page.
   - Update `NextLeaf` pointers to preserve leaf link sequence.
   - **Promote Separator Key**: Insert the middle key and new `ChildPageID` into the parent internal node.
   - If parent is also full, split recursively up to the root. If root splits, create a new root page (increasing tree height by 1).

### 4.4 Deletion & Rebalancing
1. Locate cell in target Leaf Page.
2. Mark cell payload area as unallocated and compact offset pointers.
3. If page utilization drops below threshold ($\approx 50\%$ space):
   - **Borrow/Redistribute**: Shift cells from sibling page if sibling has surplus.
   - **Merge**: Combine page with sibling if combined size fits in a single 4096-byte page frame, and free the empty page via `Pager`.

---

## 5. Go Package & Struct Design (`pkg/btree`)

Below is the foundational Go blueprint for `atlasDB`'s B+ Tree package:

```go
package btree

import (
	"atlasDB/pkg/pager"
)

// Page Types
const (
	PageTypeInteriorIndex = 0x02
	PageTypeInteriorTable = 0x05
	PageTypeLeafIndex     = 0x0A
	PageTypeLeafTable     = 0x0D
)

// BTree represents a table or index B+ Tree instance
type BTree struct {
	pager    *pager.Pager
	rootPgID uint32
}

// BTreeNode represents an in-memory page node frame
type BTreeNode struct {
	PageID   uint32
	PageType byte
	Data     []byte // 4096 bytes disk payload
	Header   NodeHeader
}

// NodeHeader represents parsed slotted page header metadata
type NodeHeader struct {
	PageType   byte
	CellCount  uint16
	CellStart  uint16
	FreeBytes  uint16
	RightChild uint32 // Interior nodes: rightmost child PGID; Leaf nodes: Next Leaf PGID
}

// Cursor provides position-based scanning over a B+ Tree
type Cursor struct {
	tree      *BTree
	currPgID  uint32
	currIndex int // Current cell index on active page
}

// NewBTree initializes a B+ Tree instance
func NewBTree(p *pager.Pager, rootPageID uint32) *BTree {
	return &BTree{
		pager:    p,
		rootPgID: rootPageID,
	}
}

// SeekRowid performs a point lookup for a target 64-bit Row ID
func (t *BTree) SeekRowid(rowID int64) ([]byte, error) {
	// 1. Traverse from rootPgID to Leaf Page
	// 2. Binary search cell pointers
	// 3. Return binary tuple payload
	return nil, nil
}

// Insert adds a row payload under a specific rowID
func (t *BTree) Insert(rowID int64, payload []byte) error {
	// 1. Locate target leaf page
	// 2. Insert into slotted page frame
	// 3. Handle split if page overflows
	return nil
}
```

---

## 6. Verification & Testing Strategy

To ensure reliability matching SQLite standards:
1. **Unit Tests**: Test page slot allocation, varint encoding, cell pointer array compaction.
2. **Tree Invariant Invariant Checks**: Add a validator verifying:
   - All leaf nodes are at identical depth.
   - Keys in internal node $i$ strictly separate child $i$ and child $i+1$.
   - Leaf links form a non-cyclic, ordered chain.
3. **Crash & Recovery Simulation**: Ensure page operations integrate properly with WAL logging in `pkg/pager`.
