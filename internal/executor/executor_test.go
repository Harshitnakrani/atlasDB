package executor

import (
	"path/filepath"
	"testing"

	"atlasDB/internal/buffer"
	"atlasDB/internal/catalog"
	"atlasDB/internal/common"
	"atlasDB/internal/config"
	"atlasDB/internal/pager"
	"atlasDB/internal/parser"
	"atlasDB/internal/planner"
	"atlasDB/internal/storage"
)

func TestExecutorEndToEnd(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "executor_test.db")

	f, err := storage.Open(dbPath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	opts := config.DefaultOptions()
	p, err := pager.Open(f, opts)
	if err != nil {
		t.Fatalf("pager.Open failed: %v", err)
	}
	bp := buffer.NewBufferPool(p, 64)

	cat, err := catalog.LoadCatalog(bp, p.CatalogRoot())
	if err != nil {
		t.Fatalf("LoadCatalog failed: %v", err)
	}

	qPlanner := planner.NewPlanner(cat)
	tc := common.NewTraceContext()

	runSQL := func(sql string) (*Result, error) {
		stmt, err := parser.ParseSQL(sql)
		if err != nil {
			return nil, err
		}
		plan, err := qPlanner.Plan(stmt, tc)
		if err != nil {
			return nil, err
		}
		return Dispatch(plan, bp, cat, tc, nil)
	}

	// 1. Create table
	_, err = runSQL("CREATE TABLE users (id INT PRIMARY KEY, name TEXT, score REAL)")
	if err != nil {
		t.Fatalf("CREATE TABLE failed: %v", err)
	}

	// 2. Insert rows
	_, err = runSQL("INSERT INTO users (id, name, score) VALUES (1, 'Alice', 95.5), (2, 'Bob', 80.0), (3, 'Charlie', 89.0)")
	if err != nil {
		t.Fatalf("INSERT failed: %v", err)
	}

	// 3. Select all
	res, err := runSQL("SELECT * FROM users")
	if err != nil {
		t.Fatalf("SELECT * failed: %v", err)
	}
	if len(res.Rows) != 3 {
		t.Fatalf("Expected 3 rows, got %d", len(res.Rows))
	}

	// 4. Point lookup on primary key
	res, err = runSQL("SELECT id, name FROM users WHERE id = 2")
	if err != nil {
		t.Fatalf("SELECT point lookup failed: %v", err)
	}
	if len(res.Rows) != 1 || res.Rows[0][1].Text != "Bob" {
		t.Fatalf("Expected Bob, got %+v", res.Rows)
	}

	// 5. Create index
	_, err = runSQL("CREATE INDEX idx_users_name ON users (name)")
	if err != nil {
		t.Fatalf("CREATE INDEX failed: %v", err)
	}

	// 6. Index scan on name
	res, err = runSQL("SELECT * FROM users WHERE name = 'Alice'")
	if err != nil {
		t.Fatalf("SELECT index scan failed: %v", err)
	}
	if len(res.Rows) != 1 || res.Rows[0][1].Text != "Alice" {
		t.Fatalf("Expected Alice, got %+v", res.Rows)
	}

	// 7. Update
	_, err = runSQL("UPDATE users SET score = 99.0 WHERE id = 1")
	if err != nil {
		t.Fatalf("UPDATE failed: %v", err)
	}
	res, err = runSQL("SELECT score FROM users WHERE id = 1")
	if err != nil || len(res.Rows) != 1 || res.Rows[0][0].Float != 99.0 {
		t.Fatalf("Expected score 99.0, got %+v", res.Rows)
	}

	// 8. Order by score desc limit 1
	res, err = runSQL("SELECT name, score FROM users ORDER BY score DESC LIMIT 1")
	if err != nil {
		t.Fatalf("SELECT ORDER BY failed: %v", err)
	}
	if len(res.Rows) != 1 || res.Rows[0][0].Text != "Alice" {
		t.Fatalf("Expected Alice as top score, got %+v", res.Rows)
	}

	// 9. Delete
	_, err = runSQL("DELETE FROM users WHERE id = 2")
	if err != nil {
		t.Fatalf("DELETE failed: %v", err)
	}
	res, err = runSQL("SELECT * FROM users")
	if err != nil || len(res.Rows) != 2 {
		t.Fatalf("Expected 2 rows after delete, got %d", len(res.Rows))
	}

	_ = bp.FlushAll()
	_ = bp.Close()
	_ = p.Close()
}
