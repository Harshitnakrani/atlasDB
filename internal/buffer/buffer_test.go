package buffer

import (
	"bytes"
	"path/filepath"
	"testing"

	"atlasDB/internal/config"
	"atlasDB/internal/pager"
	"atlasDB/internal/storage"
)

func TestBufferPoolBasic(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "buffer_test.db")

	f, err := storage.Open(dbPath)
	if err != nil {
		t.Fatalf("storage.Open failed: %v", err)
	}

	opts := config.DefaultOptions()
	opts.PageSize = 4096
	p, err := pager.Open(f, opts)
	if err != nil {
		t.Fatalf("pager.Open failed: %v", err)
	}

	// Buffer pool with small capacity of 2 pages to test eviction
	bp := NewBufferPool(p, 2)

	// Allocate page A
	frameA, err := bp.NewPage()
	if err != nil {
		t.Fatalf("NewPage failed: %v", err)
	}
	copy(frameA.Data, []byte("data in page A"))
	pageAID := frameA.PageID
	_ = bp.Unpin(pageAID, true)

	// Allocate page B
	frameB, err := bp.NewPage()
	if err != nil {
		t.Fatalf("NewPage B failed: %v", err)
	}
	copy(frameB.Data, []byte("data in page B"))
	pageBID := frameB.PageID
	_ = bp.Unpin(pageBID, true)

	// Fetch page A (should hit cache)
	frameA2, err := bp.FetchPage(pageAID)
	if err != nil {
		t.Fatalf("FetchPage A failed: %v", err)
	}
	if !bytes.Equal(frameA2.Data[:14], []byte("data in page A")) {
		t.Fatalf("Page A data mismatch")
	}
	_ = bp.Unpin(pageAID, false)

	hits, misses, evictions, _, _ := bp.Stats()
	if hits != 1 {
		t.Fatalf("Expected 1 hit, got %d", hits)
	}
	if misses != 0 {
		t.Fatalf("Expected 0 misses for A, got %d", misses)
	}

	// Allocate page C (since capacity is 2 and A was MRU, B should be evicted)
	frameC, err := bp.NewPage()
	if err != nil {
		t.Fatalf("NewPage C failed: %v", err)
	}
	copy(frameC.Data, []byte("data in page C"))
	pageCID := frameC.PageID
	_ = bp.Unpin(pageCID, true)

	_, _, evictions, _, _ = bp.Stats()
	if evictions != 1 {
		t.Fatalf("Expected 1 eviction, got %d", evictions)
	}

	// Fetch page B (should be a cache miss and loaded back from disk)
	frameB2, err := bp.FetchPage(pageBID)
	if err != nil {
		t.Fatalf("FetchPage B failed: %v", err)
	}
	if !bytes.Equal(frameB2.Data[:14], []byte("data in page B")) {
		t.Fatalf("Page B data mismatch after reload: %s", string(frameB2.Data[:14]))
	}
	_ = bp.Unpin(pageBID, false)

	if err := bp.FlushAll(); err != nil {
		t.Fatalf("FlushAll failed: %v", err)
	}
	if err := bp.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}
	if err := p.Close(); err != nil {
		t.Fatalf("pager.Close failed: %v", err)
	}
}
