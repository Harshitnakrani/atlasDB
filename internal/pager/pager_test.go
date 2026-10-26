package pager

import (
	"bytes"
	"path/filepath"
	"testing"

	"atlasDB/internal/config"
	"atlasDB/internal/storage"
)

func TestPagerLifecycle(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "pager_test.db")

	f, err := storage.Open(dbPath)
	if err != nil {
		t.Fatalf("storage.Open failed: %v", err)
	}

	opts := config.DefaultOptions()
	opts.PageSize = 4096

	p, err := Open(f, opts)
	if err != nil {
		t.Fatalf("pager.Open failed: %v", err)
	}

	if p.NumPages() != 2 {
		t.Fatalf("Expected 2 initial pages, got %d", p.NumPages())
	}
	if p.CatalogRoot() != 1 {
		t.Fatalf("Expected CatalogRoot to be 1, got %d", p.CatalogRoot())
	}

	// Allocate page 2
	page2, err := p.AllocatePage()
	if err != nil {
		t.Fatalf("AllocatePage failed: %v", err)
	}
	if page2.ID != 2 {
		t.Fatalf("Expected page ID 2, got %d", page2.ID)
	}

	copy(page2.Data, []byte("page 2 test payload"))
	if err := p.WritePage(page2); err != nil {
		t.Fatalf("WritePage failed: %v", err)
	}

	// Read page 2 back
	readBack, err := p.ReadPage(2)
	if err != nil {
		t.Fatalf("ReadPage failed: %v", err)
	}
	if !bytes.Equal(readBack.Data[:19], []byte("page 2 test payload")) {
		t.Fatalf("ReadPage data mismatch")
	}

	// Test page allocation and freeing
	page3, err := p.AllocatePage()
	if err != nil {
		t.Fatalf("AllocatePage failed: %v", err)
	}
	if page3.ID != 3 {
		t.Fatalf("Expected page ID 3, got %d", page3.ID)
	}

	if err := p.FreePage(3); err != nil {
		t.Fatalf("FreePage failed: %v", err)
	}

	// Next allocation should reuse page 3
	recycled, err := p.AllocatePage()
	if err != nil {
		t.Fatalf("AllocatePage after free failed: %v", err)
	}
	if recycled.ID != 3 {
		t.Fatalf("Expected recycled page ID 3, got %d", recycled.ID)
	}

	if err := p.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// Reopen and verify persistence
	fReopen, err := storage.Open(dbPath)
	if err != nil {
		t.Fatalf("storage.Open on reopen failed: %v", err)
	}
	pReopen, err := Open(fReopen, opts)
	if err != nil {
		t.Fatalf("pager.Open on reopen failed: %v", err)
	}
	defer pReopen.Close()

	if pReopen.NumPages() < 4 {
		t.Fatalf("Expected at least 4 pages on reopen, got %d", pReopen.NumPages())
	}

	readBack2, err := pReopen.ReadPage(2)
	if err != nil {
		t.Fatalf("ReadPage on reopen failed: %v", err)
	}
	if !bytes.Equal(readBack2.Data[:19], []byte("page 2 test payload")) {
		t.Fatalf("Reopened page 2 data mismatch")
	}
}
