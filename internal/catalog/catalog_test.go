package catalog

import (
	"path/filepath"
	"testing"

	"atlasDB/internal/buffer"
	"atlasDB/internal/common"
	"atlasDB/internal/config"
	"atlasDB/internal/pager"
	"atlasDB/internal/storage"
)

func TestCatalogPersistence(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "catalog_test.db")

	f, err := storage.Open(dbPath)
	if err != nil {
		t.Fatalf("storage.Open failed: %v", err)
	}

	opts := config.DefaultOptions()
	p, err := pager.Open(f, opts)
	if err != nil {
		t.Fatalf("pager.Open failed: %v", err)
	}
	bp := buffer.NewBufferPool(p, 16)

	cat, err := LoadCatalog(bp, p.CatalogRoot())
	if err != nil {
		t.Fatalf("LoadCatalog failed: %v", err)
	}

	cols := []ColumnDef{
		{Name: "id", Type: common.TypeInt, PrimaryKey: true},
		{Name: "name", Type: common.TypeText},
		{Name: "score", Type: common.TypeFloat},
	}
	_, err = cat.CreateTable("users", cols, 2)
	if err != nil {
		t.Fatalf("CreateTable failed: %v", err)
	}

	_, err = cat.CreateIndex("idx_users_name", "users", "name", 3, false)
	if err != nil {
		t.Fatalf("CreateIndex failed: %v", err)
	}

	if err := cat.Save(); err != nil {
		t.Fatalf("cat.Save failed: %v", err)
	}
	_ = bp.FlushAll()

	// Reload catalog from disk
	catReloaded, err := LoadCatalog(bp, p.CatalogRoot())
	if err != nil {
		t.Fatalf("Reload failed: %v", err)
	}

	tbl, ok := catReloaded.GetTable("users")
	if !ok {
		t.Fatalf("Table 'users' not found after reload")
	}
	if len(tbl.Columns) != 3 {
		t.Fatalf("Expected 3 columns, got %d", len(tbl.Columns))
	}
	if tbl.Columns[0].Name != "id" || !tbl.Columns[0].PrimaryKey {
		t.Fatalf("Column 0 mismatch")
	}

	idx, ok := catReloaded.GetIndex("idx_users_name")
	if !ok {
		t.Fatalf("Index 'idx_users_name' not found after reload")
	}
	if idx.RootPageID != 3 || idx.ColumnName != "name" {
		t.Fatalf("Index properties mismatch")
	}

	_ = bp.Close()
	_ = p.Close()
}
