package transaction

import (
	"fmt"
	"sync"
	"sync/atomic"

	"atlasDB/internal/btree"
	"atlasDB/internal/buffer"
	"atlasDB/internal/common"
	"atlasDB/internal/wal"
)

type TxState int

const (
	StateActive TxState = iota
	StateCommitted
	StateRolledBack
)

type RowUndoType int

const (
	UndoInsert RowUndoType = iota
	UndoUpdate
	UndoDelete
)

type RowUndo struct {
	Type       RowUndoType
	Table      string
	RootPageID common.PageID
	RowID      int64
	OldPayload []byte
}

// Transaction tracks an individual ACID transaction.
type Transaction struct {
	ID       common.TxID
	State    TxState
	mu       sync.Mutex
	undoLogs []*wal.LogRecord
	rowUndos []RowUndo
}

func (tx *Transaction) RecordInsert(table string, rootPgID common.PageID, rowID int64) {
	if tx == nil {
		return
	}
	tx.mu.Lock()
	defer tx.mu.Unlock()
	tx.rowUndos = append(tx.rowUndos, RowUndo{
		Type:       UndoInsert,
		Table:      table,
		RootPageID: rootPgID,
		RowID:      rowID,
	})
}

func (tx *Transaction) RecordDelete(table string, rootPgID common.PageID, rowID int64, oldPayload []byte) {
	if tx == nil {
		return
	}
	tx.mu.Lock()
	defer tx.mu.Unlock()
	tx.rowUndos = append(tx.rowUndos, RowUndo{
		Type:       UndoDelete,
		Table:      table,
		RootPageID: rootPgID,
		RowID:      rowID,
		OldPayload: oldPayload,
	})
}

func (tx *Transaction) RecordUpdate(table string, rootPgID common.PageID, rowID int64, oldPayload []byte) {
	if tx == nil {
		return
	}
	tx.mu.Lock()
	defer tx.mu.Unlock()
	tx.rowUndos = append(tx.rowUndos, RowUndo{
		Type:       UndoUpdate,
		Table:      table,
		RootPageID: rootPgID,
		RowID:      rowID,
		OldPayload: oldPayload,
	})
}

// Manager coordinates ACID transactions, WAL logging, and buffer pool synchronization.
type Manager struct {
	mu       sync.Mutex
	wal      *wal.WAL
	bp       *buffer.BufferPool
	nextTxID uint64
	active   map[common.TxID]*Transaction
}

func NewManager(w *wal.WAL, bp *buffer.BufferPool) *Manager {
	return &Manager{
		wal:      w,
		bp:       bp,
		nextTxID: 1,
		active:   make(map[common.TxID]*Transaction),
	}
}

// SetNextTxID sets the base counter after recovery.
func (m *Manager) SetNextTxID(id common.TxID) {
	atomic.StoreUint64(&m.nextTxID, uint64(id))
}

// Begin initiates a new transaction.
func (m *Manager) Begin() (*Transaction, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	txID := common.TxID(atomic.AddUint64(&m.nextTxID, 1))
	tx := &Transaction{
		ID:    txID,
		State: StateActive,
	}

	if m.wal != nil {
		rec := &wal.LogRecord{
			Type: wal.RecordTypeBegin,
			TxID: txID,
		}
		if err := m.wal.Append(rec); err != nil {
			return nil, err
		}
	}

	m.active[txID] = tx
	return tx, nil
}

// LogUpdate records a page-level modification before applying to the buffer frame.
func (m *Manager) LogUpdate(tx *Transaction, pageID common.PageID, offset uint16, before, after []byte) error {
	if tx == nil || tx.State != StateActive {
		return common.ErrNoActiveTx
	}

	tx.mu.Lock()
	defer tx.mu.Unlock()

	rec := &wal.LogRecord{
		Type:   wal.RecordTypePageUpdate,
		TxID:   tx.ID,
		PageID: pageID,
		Offset: offset,
		Before: before,
		After:  after,
	}

	if m.wal != nil {
		if err := m.wal.Append(rec); err != nil {
			return err
		}
	}

	tx.undoLogs = append(tx.undoLogs, rec)
	return nil
}

// Commit finalizes the transaction and persists its WAL commit record.
func (m *Manager) Commit(tx *Transaction) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if tx.State != StateActive {
		return fmt.Errorf("transaction %d is not active", tx.ID)
	}

	if m.wal != nil {
		rec := &wal.LogRecord{
			Type: wal.RecordTypeCommit,
			TxID: tx.ID,
		}
		if err := m.wal.Append(rec); err != nil {
			return err
		}
	}

	tx.State = StateCommitted
	delete(m.active, tx.ID)
	return nil
}

// Rollback reverts all changes made by the transaction.
func (m *Manager) Rollback(tx *Transaction) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if tx.State != StateActive {
		return fmt.Errorf("transaction %d is not active", tx.ID)
	}

	// 1. Undo row changes in reverse order
	for i := len(tx.rowUndos) - 1; i >= 0; i-- {
		u := tx.rowUndos[i]
		tree := btree.NewTree(m.bp, u.RootPageID)
		switch u.Type {
		case UndoInsert:
			_ = tree.Delete(u.RowID)
		case UndoDelete:
			_ = tree.Insert(u.RowID, u.OldPayload)
		case UndoUpdate:
			_ = tree.Delete(u.RowID)
			_ = tree.Insert(u.RowID, u.OldPayload)
		}
	}

	// 2. Undo page logs
	for i := len(tx.undoLogs) - 1; i >= 0; i-- {
		rec := tx.undoLogs[i]
		frame, err := m.bp.FetchPage(rec.PageID)
		if err == nil {
			copy(frame.Data[rec.Offset:rec.Offset+uint16(len(rec.Before))], rec.Before)
			_ = m.bp.Unpin(rec.PageID, true)
		}
	}

	if m.wal != nil {
		rec := &wal.LogRecord{
			Type: wal.RecordTypeRollback,
			TxID: tx.ID,
		}
		_ = m.wal.Append(rec)
	}

	tx.State = StateRolledBack
	delete(m.active, tx.ID)
	return nil
}
