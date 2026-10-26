package engine

import (
	"path/filepath"
	"testing"

	"atlasDB/internal/config"
)

func TestEngineLifecycleAndTrace(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "engine_test.db")

	opts := config.DefaultOptions()
	db, err := Open(dbPath, opts)
	if err != nil {
		t.Fatalf("Engine.Open failed: %v", err)
	}
	defer db.Close()

	// 1. Create table
	_, err = db.Exec("CREATE TABLE users (id INT PRIMARY KEY, name TEXT, score REAL)")
	if err != nil {
		t.Fatalf("CREATE TABLE failed: %v", err)
	}

	// 2. Insert records
	_, err = db.Exec("INSERT INTO users (id, name, score) VALUES (302, 'Harshit', 98.5), (101, 'Ada', 99.0)")
	if err != nil {
		t.Fatalf("INSERT failed: %v", err)
	}

	// 3. Trace query matching Code Example 8 in paper
	tc, res, err := db.Trace("SELECT * FROM users WHERE id = 302")
	if err != nil {
		t.Fatalf("Trace query failed: %v", err)
	}
	if len(res.Rows) != 1 || res.Rows[0][1].Text != "Harshit" {
		t.Fatalf("Unexpected query result: %+v", res.Rows)
	}

	t.Logf("Captured %d trace events:", len(tc.Events))
	for _, ev := range tc.Events {
		t.Logf("  %-8s %s", ev.Subsystem, ev.Message)
	}

	// Verify trace contains expected subsystems
	hasParser := false
	hasPlanner := false
	hasBTree := false
	hasBuffer := false
	hasExecutor := false

	for _, ev := range tc.Events {
		switch ev.Subsystem {
		case "Parser":
			hasParser = true
		case "Planner":
			hasPlanner = true
		case "BTree":
			hasBTree = true
		case "Buffer":
			hasBuffer = true
		case "Executor":
			hasExecutor = true
		}
	}

	if !hasParser || !hasPlanner || !hasBTree || !hasBuffer || !hasExecutor {
		t.Fatalf("Trace missing required subsystem events: parser=%v planner=%v btree=%v buffer=%v executor=%v",
			hasParser, hasPlanner, hasBTree, hasBuffer, hasExecutor)
	}

	// 4. Test Transaction Rollback
	_, err = db.Exec("BEGIN")
	if err != nil {
		t.Fatalf("BEGIN failed: %v", err)
	}
	_, err = db.Exec("INSERT INTO users (id, name, score) VALUES (999, 'Ghost', 50.0)")
	if err != nil {
		t.Fatalf("INSERT in tx failed: %v", err)
	}
	_, err = db.Exec("ROLLBACK")
	if err != nil {
		t.Fatalf("ROLLBACK failed: %v", err)
	}

	res, err = db.Query("SELECT * FROM users WHERE id = 999")
	if err != nil {
		t.Fatalf("SELECT after rollback failed: %v", err)
	}
	if len(res.Rows) != 0 {
		t.Fatalf("Expected 0 rows after rollback, got %d", len(res.Rows))
	}

	// 5. Close and Reopen
	if err := db.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	db2, err := Open(dbPath, opts)
	if err != nil {
		t.Fatalf("Reopen failed: %v", err)
	}
	defer db2.Close()

	res2, err := db2.Query("SELECT * FROM users WHERE id = 302")
	if err != nil {
		t.Fatalf("Query after reopen failed: %v", err)
	}
	if len(res2.Rows) != 1 || res2.Rows[0][1].Text != "Harshit" {
		t.Fatalf("Data did not persist across close and reopen!")
	}
}
