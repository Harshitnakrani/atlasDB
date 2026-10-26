package recovery

import (
	"fmt"

	"atlasDB/internal/buffer"
	"atlasDB/internal/common"
	"atlasDB/internal/wal"
)

type RecoveryStats struct {
	TotalRecords    int
	CommittedTxs    int
	UncommittedTxs  int
	RedoOperations  int
	MaxTxID         common.TxID
}

// Recover scans the WAL, identifies committed transactions, and applies redo logs to pages.
func Recover(w *wal.WAL, bp *buffer.BufferPool) (*RecoveryStats, error) {
	if w == nil {
		return &RecoveryStats{}, nil
	}

	records, err := w.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("failed to read WAL records: %w", err)
	}

	stats := &RecoveryStats{
		TotalRecords: len(records),
	}

	if len(records) == 0 {
		return stats, nil
	}

	// 1. Analysis Pass: determine transaction status
	committed := make(map[common.TxID]bool)
	allTxs := make(map[common.TxID]bool)

	for _, r := range records {
		if r.TxID > stats.MaxTxID {
			stats.MaxTxID = r.TxID
		}
		allTxs[r.TxID] = true

		if r.Type == wal.RecordTypeCommit {
			committed[r.TxID] = true
		} else if r.Type == wal.RecordTypeRollback {
			committed[r.TxID] = false
		}
	}

	for txID := range allTxs {
		if committed[txID] {
			stats.CommittedTxs++
		} else {
			stats.UncommittedTxs++
		}
	}

	// 2. Redo Pass: replay committed modifications
	for _, r := range records {
		if r.Type == wal.RecordTypePageUpdate && committed[r.TxID] {
			frame, err := bp.FetchPage(r.PageID)
			if err != nil {
				return nil, fmt.Errorf("recovery failed to fetch page %d: %w", r.PageID, err)
			}
			copy(frame.Data[r.Offset:r.Offset+uint16(len(r.After))], r.After)
			_ = bp.Unpin(r.PageID, true)
			stats.RedoOperations++
		}
	}

	// 3. Flush changes and truncate WAL
	if err := bp.FlushAll(); err != nil {
		return nil, fmt.Errorf("failed to flush buffer pool during recovery: %w", err)
	}

	if err := w.Truncate(); err != nil {
		return nil, fmt.Errorf("failed to truncate WAL after recovery: %w", err)
	}

	return stats, nil
}
