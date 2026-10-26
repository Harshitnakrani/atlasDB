package tests

import (
	"bytes"
	"fmt"
	"math/rand"
	"path/filepath"
	"testing"
	"time"

	"atlasDB/internal/btree"
	"atlasDB/internal/buffer"
	"atlasDB/internal/common"
	"atlasDB/internal/config"
	"atlasDB/internal/engine"
	"atlasDB/internal/pager"
	"atlasDB/internal/record"
	"atlasDB/internal/recovery"
	"atlasDB/internal/storage"
	"atlasDB/internal/wal"
)

// 1. Page round-trip tests: write arbitrary bytes, close, reopen, and verify exact contents.
func TestPageRoundTrip(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "page_roundtrip.db")

	f, err := storage.Open(dbPath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	opts := config.DefaultOptions()
	p, err := pager.Open(f, opts)
	if err != nil {
		t.Fatalf("pager.Open failed: %v", err)
	}

	payloads := make(map[common.PageID][]byte)
	rnd := rand.New(rand.NewSource(42))

	for i := 0; i < 5; i++ {
		page, err := p.AllocatePage()
		if err != nil {
			t.Fatalf("AllocatePage failed: %v", err)
		}
		randomBytes := make([]byte, opts.PageSize)
		rnd.Read(randomBytes)
		copy(page.Data, randomBytes)
		if err := p.WritePage(page); err != nil {
			t.Fatalf("WritePage failed: %v", err)
		}
		payloads[page.ID] = randomBytes
	}

	if err := p.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// Reopen and verify
	fReopen, err := storage.Open(dbPath)
	if err != nil {
		t.Fatalf("Reopen storage failed: %v", err)
	}
	pReopen, err := pager.Open(fReopen, opts)
	if err != nil {
		t.Fatalf("Reopen pager failed: %v", err)
	}
	defer pReopen.Close()

	for pgID, expected := range payloads {
		page, err := pReopen.ReadPage(pgID)
		if err != nil {
			t.Fatalf("ReadPage(%d) failed: %v", pgID, err)
		}
		if !bytes.Equal(page.Data, expected) {
			t.Fatalf("Data round-trip mismatch on page %d", pgID)
		}
	}
}

// 2. B+ tree invariant tests after random insertion and deletion sequences.
func TestBTreeRandomInvariants(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "btree_invariants.db")

	f, err := storage.Open(dbPath)
	if err != nil {
		t.Fatalf("storage.Open failed: %v", err)
	}
	opts := config.DefaultOptions()
	p, err := pager.Open(f, opts)
	if err != nil {
		t.Fatalf("pager.Open failed: %v", err)
	}
	bp := buffer.NewBufferPool(p, 128)
	tree, err := btree.CreateTree(bp)
	if err != nil {
		t.Fatalf("CreateTree failed: %v", err)
	}

	rnd := rand.New(rand.NewSource(1337))
	keys := rnd.Perm(500) // 500 keys in random order

	// Insert all keys
	for _, k := range keys {
		rowID := int64(k + 1)
		payload := []byte(fmt.Sprintf("val_%d_testing_invariants", rowID))
		if err := tree.Insert(rowID, payload); err != nil {
			t.Fatalf("Insert(%d) failed: %v", rowID, err)
		}
	}

	// Invariants check
	if err := tree.VerifyInvariants(); err != nil {
		t.Fatalf("Tree invariants failed after insertions: %v", err)
	}

	// Delete 100 random keys
	for i := 0; i < 100; i++ {
		rowID := int64(keys[i] + 1)
		if err := tree.Delete(rowID); err != nil {
			t.Fatalf("Delete(%d) failed: %v", rowID, err)
		}
	}

	// Verify invariants hold after deletions
	if err := tree.VerifyInvariants(); err != nil {
		t.Fatalf("Tree invariants failed after deletions: %v", err)
	}

	_ = bp.Close()
	_ = p.Close()
}

// 3. Range-scan tests verifying sorted and complete output.
func TestRangeScanCompleteness(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "range_scan.db")

	db, err := engine.Open(dbPath)
	if err != nil {
		t.Fatalf("engine.Open failed: %v", err)
	}
	defer db.Close()

	_, _ = db.Exec("CREATE TABLE numbers (id INT PRIMARY KEY, val TEXT)")

	nRows := 250
	for i := 1; i <= nRows; i++ {
		sql := fmt.Sprintf("INSERT INTO numbers (id, val) VALUES (%d, 'item_%d')", i, i)
		if _, err := db.Exec(sql); err != nil {
			t.Fatalf("Insert failed: %v", err)
		}
	}

	res, err := db.Query("SELECT id, val FROM numbers ORDER BY id ASC")
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}

	if len(res.Rows) != nRows {
		t.Fatalf("Expected %d rows, got %d", nRows, len(res.Rows))
	}

	for i := 0; i < nRows; i++ {
		expectedID := int64(i + 1)
		actualID := res.Rows[i][0].Int
		if actualID != expectedID {
			t.Fatalf("Range scan order violation at index %d: expected %d, got %d", i, expectedID, actualID)
		}
	}
}

// 4. Record serialization round trips across all supported data types.
func TestRecordAllTypesRoundTrip(t *testing.T) {
	testCases := []common.Value{
		common.IntValue(-9223372036854775807),
		common.IntValue(0),
		common.IntValue(9223372036854775806),
		common.FloatValue(3.1415926535),
		common.FloatValue(-0.0000001),
		common.TextValue("A string containing unicode: 日本語 and quotes ' \""),
		common.TextValue(""),
		common.BoolValue(true),
		common.BoolValue(false),
		common.NullValue(),
		common.BlobValue([]byte{0x00, 0xFF, 0x42, 0x1A}),
	}

	encoded, err := record.EncodeValues(testCases)
	if err != nil {
		t.Fatalf("EncodeValues failed: %v", err)
	}

	decoded, err := record.Decode(encoded)
	if err != nil {
		t.Fatalf("Decode failed: %v", err)
	}

	if len(decoded) != len(testCases) {
		t.Fatalf("Length mismatch: expected %d, got %d", len(testCases), len(decoded))
	}

	for i := range testCases {
		if testCases[i].Compare(decoded[i]) != 0 {
			t.Fatalf("Value mismatch at %d: %v vs %v", i, testCases[i], decoded[i])
		}
	}
}

// 5. Transaction tests for commit and rollback behavior.
func TestTransactionCommitAndRollback(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "tx_test.db")

	db, err := engine.Open(dbPath)
	if err != nil {
		t.Fatalf("engine.Open failed: %v", err)
	}
	defer db.Close()

	_, _ = db.Exec("CREATE TABLE accounts (id INT PRIMARY KEY, balance REAL)")
	_, _ = db.Exec("INSERT INTO accounts (id, balance) VALUES (1, 500.0), (2, 200.0)")

	// Tx 1: Commit transfer
	_, _ = db.Exec("BEGIN")
	_, _ = db.Exec("UPDATE accounts SET balance = 400.0 WHERE id = 1")
	_, _ = db.Exec("UPDATE accounts SET balance = 300.0 WHERE id = 2")
	_, err = db.Exec("COMMIT")
	if err != nil {
		t.Fatalf("COMMIT failed: %v", err)
	}

	res, err := db.Query("SELECT balance FROM accounts WHERE id = 1")
	if err != nil || res.Rows[0][0].Float != 400.0 {
		t.Fatalf("Committed change was not persisted")
	}

	// Tx 2: Rollback transfer
	_, _ = db.Exec("BEGIN")
	_, _ = db.Exec("UPDATE accounts SET balance = 0.0 WHERE id = 1")
	_, _ = db.Exec("INSERT INTO accounts (id, balance) VALUES (3, 999.0)")
	_, err = db.Exec("ROLLBACK")
	if err != nil {
		t.Fatalf("ROLLBACK failed: %v", err)
	}

	// Verify balance reverted and row 3 was not created
	res, _ = db.Query("SELECT balance FROM accounts WHERE id = 1")
	if res.Rows[0][0].Float != 400.0 {
		t.Fatalf("Rollback failed to restore balance: got %f", res.Rows[0][0].Float)
	}

	res, _ = db.Query("SELECT * FROM accounts WHERE id = 3")
	if len(res.Rows) != 0 {
		t.Fatalf("Rollback failed to remove inserted row 3")
	}
}

// 6. Crash-recovery tests at selected WAL and page-write boundaries.
func TestCrashRecoveryIntegrity(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "crash_test.db")
	walPath := dbPath + ".wal"

	// 1. Initialize base database
	f, err := storage.Open(dbPath)
	if err != nil {
		t.Fatalf("storage.Open failed: %v", err)
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
	copy(page2.Data, []byte("before_crash_baseline_data"))
	_ = p.WritePage(page2)
	_ = p.Close()

	// 2. Simulate WAL activity
	w, err := wal.Open(walPath, true)
	if err != nil {
		t.Fatalf("wal.Open failed: %v", err)
	}

	// Committed Tx 100
	tx100 := common.TxID(100)
	_ = w.Append(&wal.LogRecord{Type: wal.RecordTypeBegin, TxID: tx100})
	_ = w.Append(&wal.LogRecord{
		Type:   wal.RecordTypePageUpdate,
		TxID:   tx100,
		PageID: 2,
		Offset: 0,
		Before: []byte("before_crash_baseline_data"),
		After:  []byte("tx100_COMMITTED_DATA______"),
	})
	_ = w.Append(&wal.LogRecord{Type: wal.RecordTypeCommit, TxID: tx100})

	// Uncommitted Tx 101 (torn write at crash)
	tx101 := common.TxID(101)
	_ = w.Append(&wal.LogRecord{Type: wal.RecordTypeBegin, TxID: tx101})
	_ = w.Append(&wal.LogRecord{
		Type:   wal.RecordTypePageUpdate,
		TxID:   tx101,
		PageID: 2,
		Offset: 0,
		Before: []byte("tx100_COMMITTED_DATA______"),
		After:  []byte("tx101_UNCOMMITTED_GARBAGE_"),
	})
	_ = w.Sync()
	_ = w.Close()

	// 3. Reopen via engine to trigger automatic crash recovery
	db, err := engine.Open(dbPath)
	if err != nil {
		t.Fatalf("engine.Open with recovery failed: %v", err)
	}
	defer db.Close()

	// Verify Page 2 contains Tx 100 changes, not Tx 101
	frame, err := db.BufferPool().FetchPage(2)
	if err != nil {
		t.Fatalf("FetchPage(2) failed: %v", err)
	}
	defer db.BufferPool().Unpin(2, false)

	expected := []byte("tx100_COMMITTED_DATA______")
	if !bytes.Equal(frame.Data[:len(expected)], expected) {
		t.Fatalf("Recovery failed: expected %s, got %s", expected, string(frame.Data[:len(expected)]))
	}
}

var _ = time.Now
var _ = recovery.Recover
