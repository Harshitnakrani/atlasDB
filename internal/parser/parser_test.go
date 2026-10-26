package parser

import (
	"testing"

	"atlasDB/internal/common"
)

func TestParseStatements(t *testing.T) {
	// 1. Create table
	stmt, err := ParseSQL("CREATE TABLE users (id INT PRIMARY KEY, name TEXT, score REAL);")
	if err != nil {
		t.Fatalf("Parse CREATE TABLE failed: %v", err)
	}
	createStmt, ok := stmt.(*CreateTableStmt)
	if !ok || createStmt.Table != "users" || len(createStmt.Columns) != 3 {
		t.Fatalf("Unexpected create stmt: %+v", stmt)
	}
	if !createStmt.Columns[0].PrimaryKey || createStmt.Columns[0].Type != common.TypeInt {
		t.Fatalf("Primary key not detected properly")
	}

	// 2. Create index
	stmt, err = ParseSQL("CREATE INDEX idx_users_name ON users (name)")
	if err != nil {
		t.Fatalf("Parse CREATE INDEX failed: %v", err)
	}
	idxStmt, ok := stmt.(*CreateIndexStmt)
	if !ok || idxStmt.IndexName != "idx_users_name" || idxStmt.Table != "users" || idxStmt.Column != "name" {
		t.Fatalf("Unexpected index stmt: %+v", stmt)
	}

	// 3. Insert
	stmt, err = ParseSQL("INSERT INTO users (id, name, score) VALUES (1, 'Alice', 95.5), (2, 'Bob', 88.0)")
	if err != nil {
		t.Fatalf("Parse INSERT failed: %v", err)
	}
	insertStmt, ok := stmt.(*InsertStmt)
	if !ok || insertStmt.Table != "users" || len(insertStmt.Values) != 2 {
		t.Fatalf("Unexpected insert stmt: %+v", stmt)
	}

	// 4. Select with WHERE, ORDER BY, LIMIT
	stmt, err = ParseSQL("SELECT id, name FROM users WHERE score >= 90.0 ORDER BY score DESC LIMIT 5")
	if err != nil {
		t.Fatalf("Parse SELECT failed: %v", err)
	}
	selectStmt, ok := stmt.(*SelectStmt)
	if !ok || selectStmt.Table != "users" || len(selectStmt.Columns) != 2 {
		t.Fatalf("Unexpected select stmt: %+v", stmt)
	}
	if selectStmt.Limit != 5 || selectStmt.OrderBy == nil || !selectStmt.OrderBy.Desc {
		t.Fatalf("Unexpected limit or order by: %+v", selectStmt)
	}

	// 5. Update
	stmt, err = ParseSQL("UPDATE users SET score = 99.5 WHERE id = 1")
	if err != nil {
		t.Fatalf("Parse UPDATE failed: %v", err)
	}
	updStmt, ok := stmt.(*UpdateStmt)
	if !ok || updStmt.Table != "users" || len(updStmt.Assignments) != 1 {
		t.Fatalf("Unexpected update stmt: %+v", stmt)
	}

	// 6. Delete
	stmt, err = ParseSQL("DELETE FROM users WHERE id = 2")
	if err != nil {
		t.Fatalf("Parse DELETE failed: %v", err)
	}
	delStmt, ok := stmt.(*DeleteStmt)
	if !ok || delStmt.Table != "users" || delStmt.Where == nil {
		t.Fatalf("Unexpected delete stmt: %+v", stmt)
	}

	// 7. Transaction control
	stmt, _ = ParseSQL("BEGIN")
	if _, ok := stmt.(*BeginStmt); !ok {
		t.Fatalf("Expected BeginStmt")
	}
	stmt, _ = ParseSQL("COMMIT")
	if _, ok := stmt.(*CommitStmt); !ok {
		t.Fatalf("Expected CommitStmt")
	}
	stmt, _ = ParseSQL("ROLLBACK")
	if _, ok := stmt.(*RollbackStmt); !ok {
		t.Fatalf("Expected RollbackStmt")
	}

	// 8. Aggregations, GROUP BY, HAVING
	stmt, err = ParseSQL("SELECT COUNT(*), SUM(score), AVG(score) FROM users GROUP BY name HAVING AVG(score) >= 90.0")
	if err != nil {
		t.Fatalf("Parse Aggregation failed: %v", err)
	}
	aggSelect, ok := stmt.(*SelectStmt)
	if !ok || len(aggSelect.Columns) != 3 || len(aggSelect.GroupBy) != 1 || aggSelect.Having == nil {
		t.Fatalf("Unexpected aggSelect: %+v", aggSelect)
	}

	// 9. JOIN
	stmt, err = ParseSQL("SELECT orders.id, users.name FROM orders INNER JOIN users ON orders.user_id = users.id")
	if err != nil {
		t.Fatalf("Parse JOIN failed: %v", err)
	}
	joinSelect, ok := stmt.(*SelectStmt)
	if !ok || len(joinSelect.Joins) != 1 || joinSelect.Joins[0].Table != "users" {
		t.Fatalf("Unexpected joinSelect: %+v", joinSelect)
	}

	// 10. Extended predicates: LIKE, IN, BETWEEN, IS NOT NULL
	stmt, err = ParseSQL("SELECT * FROM users WHERE score BETWEEN 80.0 AND 100.0 AND name LIKE 'A%' AND id IN (1, 2) AND active IS NOT NULL")
	if err != nil {
		t.Fatalf("Parse extended predicates failed: %v", err)
	}

	// 11. DDL: DROP, TRUNCATE, ALTER
	stmt, err = ParseSQL("DROP TABLE users")
	if _, ok := stmt.(*DropTableStmt); !ok || err != nil {
		t.Fatalf("Parse DROP TABLE failed: %v", err)
	}

	stmt, err = ParseSQL("DROP INDEX idx_name")
	if _, ok := stmt.(*DropIndexStmt); !ok || err != nil {
		t.Fatalf("Parse DROP INDEX failed: %v", err)
	}

	stmt, err = ParseSQL("TRUNCATE TABLE users")
	if _, ok := stmt.(*TruncateTableStmt); !ok || err != nil {
		t.Fatalf("Parse TRUNCATE TABLE failed: %v", err)
	}

	stmt, err = ParseSQL("ALTER TABLE users ADD COLUMN age INT")
	if _, ok := stmt.(*AlterTableStmt); !ok || err != nil {
		t.Fatalf("Parse ALTER TABLE failed: %v", err)
	}
}
