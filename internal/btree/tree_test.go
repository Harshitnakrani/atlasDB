package btree

import (
	"bytes"
	"fmt"
	"path/filepath"
	"testing"

	"atlasDB/internal/buffer"
	"atlasDB/internal/common"
	"atlasDB/internal/config"
	"atlasDB/internal/pager"
	"atlasDB/internal/storage"
)

func TestBTreeInsertAndSeek(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "btree_test.db")

	f, err := storage.Open(dbPath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	opts := config.DefaultOptions()
	opts.PageSize = 4096
	p, err := pager.Open(f, opts)
	if err != nil {
		t.Fatalf("pager.Open failed: %v", err)
	}

	bp := buffer.NewBufferPool(p, 64)

	tree, err := CreateTree(bp)
	if err != nil {
		t.Fatalf("CreateTree failed: %v", err)
	}

	// Insert 1500 entries to force leaf splits and multi-level root splits
	nEntries := 1500
	for i := 1; i <= nEntries; i++ {
		payload := []byte(fmt.Sprintf("record_payload_%04d_atlas_relational_database", i))
		if err := tree.Insert(int64(i), payload); err != nil {
			t.Fatalf("Insert failed at %d: %v", i, err)
		}
	}

	// Verify tree invariants
	if err := tree.VerifyInvariants(); err != nil {
		t.Fatalf("Tree invariants violated: %v", err)
	}

	// Seek every row
	tc := common.NewTraceContext()
	for i := 1; i <= nEntries; i++ {
		expectedPayload := []byte(fmt.Sprintf("record_payload_%04d_atlas_relational_database", i))
		gotPayload, err := tree.SeekRowid(int64(i), tc)
		if err != nil {
			t.Fatalf("SeekRowid failed for row %d: %v", i, err)
		}
		if !bytes.Equal(gotPayload, expectedPayload) {
			t.Fatalf("Payload mismatch at %d: got %s, expected %s", i, gotPayload, expectedPayload)
		}
	}

	// Range scan with Cursor
	cursor, err := NewCursor(tree, 1)
	if err != nil {
		t.Fatalf("NewCursor failed: %v", err)
	}
	defer cursor.Close()

	count := 0
	for {
		rowID, payload, ok, err := cursor.Next()
		if err != nil {
			t.Fatalf("Cursor error: %v", err)
		}
		if !ok {
			break
		}
		count++
		if rowID != int64(count) {
			t.Fatalf("Cursor out of order: expected row %d, got %d", count, rowID)
		}
		expectedPayload := []byte(fmt.Sprintf("record_payload_%04d_atlas_relational_database", count))
		if !bytes.Equal(payload, expectedPayload) {
			t.Fatalf("Cursor payload mismatch at %d", count)
		}
	}
	if count != nEntries {
		t.Fatalf("Cursor returned %d records, expected %d", count, nEntries)
	}

	// Test Deletion
	if err := tree.Delete(50); err != nil {
		t.Fatalf("Delete row 50 failed: %v", err)
	}
	_, err = tree.SeekRowid(50, tc)
	if err != common.ErrNotFound {
		t.Fatalf("Expected ErrNotFound after deletion, got %v", err)
	}

	// Test Secondary Index
	idxTree, err := CreateIndexTree(bp)
	if err != nil {
		t.Fatalf("CreateIndexTree failed: %v", err)
	}
	if err := idxTree.Insert([]byte("alice"), 10); err != nil {
		t.Fatalf("idxTree.Insert failed: %v", err)
	}
	if err := idxTree.Insert([]byte("bob"), 20); err != nil {
		t.Fatalf("idxTree.Insert failed: %v", err)
	}
	if err := idxTree.Insert([]byte("alice"), 30); err != nil {
		t.Fatalf("idxTree.Insert failed: %v", err)
	}

	results, err := idxTree.Find([]byte("alice"))
	if err != nil {
		t.Fatalf("idxTree.Find failed: %v", err)
	}
	if len(results) != 2 || results[0] != 10 || results[1] != 30 {
		t.Fatalf("Unexpected index search results: %v", results)
	}

	_ = bp.FlushAll()
	_ = bp.Close()
	_ = p.Close()
}
