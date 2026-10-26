package recovery

import (
	"bytes"
	"path/filepath"
	"testing"

	"atlasDB/internal/buffer"
	"atlasDB/internal/common"
	"atlasDB/internal/config"
	"atlasDB/internal/pager"
	"atlasDB/internal/storage"
	"atlasDB/internal/wal"
)

func TestCrashRecovery(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "recovery_test.db")
	walPath := filepath.Join(tempDir, "recovery_test.wal")

	// Setup initial DB
	f, err := storage.Open(dbPath)
	if err != nil {
		t.Fatalf("Open db failed: %v", err)
	}
	opts := config.DefaultOptions()
	p, err := pager.Open(f, opts)
	if err != nil {
		t.Fatalf("pager.Open failed: %v", err)
	}

	page2, err := p.AllocatePage()
	if err != nil {
		t.Fatalf("AllocatePage failed: %v", err)
	}
	copy(page2.Data, []byte("initial_clean_state_"))
	_ = p.WritePage(page2)
	_ = p.Sync()
	_ = p.Close()

	// Simulate WAL writing before crash
	w, err := wal.Open(walPath, true)
	if err != nil {
		t.Fatalf("wal.Open failed: %v", err)
	}

	// Tx 1: Committed modification
	tx1ID := common.TxID(1)
	_ = w.Append(&wal.LogRecord{Type: wal.RecordTypeBegin, TxID: tx1ID})
	_ = w.Append(&wal.LogRecord{
		Type:   wal.RecordTypePageUpdate,
		TxID:   tx1ID,
		PageID: 2,
		Offset: 0,
		Before: []byte("initial_clean_state_"),
		After:  []byte("tx1_committed_update"),
	})
	_ = w.Append(&wal.LogRecord{Type: wal.RecordTypeCommit, TxID: tx1ID})

	// Tx 2: Uncommitted modification (crashed before commit!)
	tx2ID := common.TxID(2)
	_ = w.Append(&wal.LogRecord{Type: wal.RecordTypeBegin, TxID: tx2ID})
	_ = w.Append(&wal.LogRecord{
		Type:   wal.RecordTypePageUpdate,
		TxID:   tx2ID,
		PageID: 2,
		Offset: 0,
		Before: []byte("tx1_committed_update"),
		After:  []byte("tx2_UNCOMMITTED_CRSH"),
	})
	// No commit record for tx2!
	_ = w.Sync()
	_ = w.Close()

	// Perform Recovery on reopen
	fRecover, err := storage.Open(dbPath)
	if err != nil {
		t.Fatalf("reopen db failed: %v", err)
	}
	pRecover, err := pager.Open(fRecover, opts)
	if err != nil {
		t.Fatalf("reopen pager failed: %v", err)
	}
	bpRecover := buffer.NewBufferPool(pRecover, 16)
	wRecover, err := wal.Open(walPath, true)
	if err != nil {
		t.Fatalf("reopen wal failed: %v", err)
	}

	stats, err := Recover(wRecover, bpRecover)
	if err != nil {
		t.Fatalf("Recover failed: %v", err)
	}

	if stats.CommittedTxs != 1 {
		t.Fatalf("Expected 1 committed tx, got %d", stats.CommittedTxs)
	}
	if stats.UncommittedTxs != 1 {
		t.Fatalf("Expected 1 uncommitted tx, got %d", stats.UncommittedTxs)
	}
	if stats.RedoOperations != 1 {
		t.Fatalf("Expected 1 redo op, got %d", stats.RedoOperations)
	}

	// Verify Page 2 has tx1 changes, NOT tx2 changes
	frame, err := bpRecover.FetchPage(2)
	if err != nil {
		t.Fatalf("FetchPage(2) failed: %v", err)
	}
	if !bytes.Equal(frame.Data[:20], []byte("tx1_committed_update")) {
		t.Fatalf("Recovery page content mismatch: got %s", string(frame.Data[:20]))
	}

	_ = bpRecover.Close()
	_ = pRecover.Close()
	_ = wRecover.Close()
}
