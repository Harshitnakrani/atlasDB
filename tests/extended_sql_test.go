package tests

import (
	"path/filepath"
	"testing"

	"atlasDB/internal/engine"
)

func TestExtendedMySQLQueries(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "mysql_features.db")

	db, err := engine.Open(dbPath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer db.Close()

	// 1. Create Tables
	_, err = db.Exec("CREATE TABLE departments (id INT PRIMARY KEY, name TEXT)")
	if err != nil {
		t.Fatalf("CREATE departments failed: %v", err)
	}

	_, err = db.Exec("CREATE TABLE employees (id INT PRIMARY KEY, name TEXT, dept_id INT, salary REAL, status TEXT)")
	if err != nil {
		t.Fatalf("CREATE employees failed: %v", err)
	}

	// 2. Insert Data
	_, _ = db.Exec("INSERT INTO departments (id, name) VALUES (1, 'Engineering'), (2, 'Design'), (3, 'Marketing')")
	_, _ = db.Exec("INSERT INTO employees (id, name, dept_id, salary, status) VALUES " +
		"(101, 'Alice Smith', 1, 120000.0, 'active'), " +
		"(102, 'Bob Jones', 1, 95000.0, 'active'), " +
		"(103, 'Charlie Brown', 2, 85000.0, 'leave'), " +
		"(104, 'Diana Prince', 2, 110000.0, 'active'), " +
		"(105, 'Edward Elric', 99, 70000.0, 'probation')") // dept 99 has no matching department

	// 3. INNER JOIN Test
	res, err := db.Query("SELECT employees.name, departments.name FROM employees INNER JOIN departments ON employees.dept_id = departments.id")
	if err != nil {
		t.Fatalf("INNER JOIN failed: %v", err)
	}
	if len(res.Rows) != 4 {
		t.Fatalf("Expected 4 inner join rows, got %d", len(res.Rows))
	}

	// 4. LEFT JOIN Test
	res, err = db.Query("SELECT employees.name, departments.name FROM employees LEFT JOIN departments ON employees.dept_id = departments.id")
	if err != nil {
		t.Fatalf("LEFT JOIN failed: %v", err)
	}
	if len(res.Rows) != 5 {
		t.Fatalf("Expected 5 left join rows (including unmatched Edward), got %d", len(res.Rows))
	}

	// 5. Aggregations: COUNT(*), SUM, AVG, MIN, MAX
	res, err = db.Query("SELECT COUNT(*), SUM(salary), AVG(salary), MIN(salary), MAX(salary) FROM employees")
	if err != nil {
		t.Fatalf("Aggregations failed: %v", err)
	}
	if len(res.Rows) != 1 {
		t.Fatalf("Expected 1 row for scalar aggregation, got %d", len(res.Rows))
	}
	if res.Rows[0][0].Int != 5 {
		t.Fatalf("Expected count 5, got %d", res.Rows[0][0].Int)
	}
	if res.Rows[0][1].Float != 480000.0 {
		t.Fatalf("Expected sum 480000, got %f", res.Rows[0][1].Float)
	}

	// 6. GROUP BY and HAVING
	res, err = db.Query("SELECT dept_id, COUNT(*), AVG(salary) FROM employees GROUP BY dept_id HAVING COUNT(*) >= 2")
	if err != nil {
		t.Fatalf("GROUP BY failed: %v", err)
	}
	if len(res.Rows) != 2 {
		t.Fatalf("Expected 2 departments with >= 2 employees, got %d", len(res.Rows))
	}

	// 7. DISTINCT
	res, err = db.Query("SELECT DISTINCT status FROM employees")
	if err != nil {
		t.Fatalf("DISTINCT failed: %v", err)
	}
	if len(res.Rows) != 3 { // active, leave, probation
		t.Fatalf("Expected 3 distinct statuses, got %d", len(res.Rows))
	}

	// 8. LIKE with wildcard %
	res, err = db.Query("SELECT * FROM employees WHERE name LIKE 'A%'")
	if err != nil || len(res.Rows) != 1 || res.Rows[0][1].Text != "Alice Smith" {
		t.Fatalf("LIKE 'A%%%%' failed: %+v", res)
	}

	// 9. IN (...) and NOT IN (...)
	res, err = db.Query("SELECT * FROM employees WHERE id IN (101, 103)")
	if err != nil || len(res.Rows) != 2 {
		t.Fatalf("IN failed: %+v", res)
	}

	// 10. BETWEEN ... AND ...
	res, err = db.Query("SELECT * FROM employees WHERE salary BETWEEN 90000.0 AND 115000.0")
	if err != nil || len(res.Rows) != 2 {
		t.Fatalf("BETWEEN failed: expected 2 rows, got %d", len(res.Rows))
	}

	// 11. IS NOT NULL
	res, err = db.Query("SELECT * FROM employees WHERE salary IS NOT NULL")
	if err != nil || len(res.Rows) != 5 {
		t.Fatalf("IS NOT NULL failed: got %d rows", len(res.Rows))
	}

	// 12. LIMIT with OFFSET (MySQL syntax)
	res, err = db.Query("SELECT * FROM employees ORDER BY id ASC LIMIT 2 OFFSET 1")
	if err != nil || len(res.Rows) != 2 || res.Rows[0][0].Int != 102 {
		t.Fatalf("LIMIT OFFSET failed: %+v", res)
	}

	// 13. ALTER TABLE ADD COLUMN
	_, err = db.Exec("ALTER TABLE employees ADD COLUMN bonus REAL")
	if err != nil {
		t.Fatalf("ALTER TABLE ADD COLUMN failed: %v", err)
	}
	empTbl, ok := db.Catalog().GetTable("employees")
	if !ok || len(empTbl.Columns) != 6 {
		t.Fatalf("Column bonus not added properly")
	}

	// 14. TRUNCATE TABLE
	_, err = db.Exec("TRUNCATE TABLE employees")
	if err != nil {
		t.Fatalf("TRUNCATE TABLE failed: %v", err)
	}
	res, err = db.Query("SELECT * FROM employees")
	if err != nil || len(res.Rows) != 0 {
		t.Fatalf("Expected 0 rows after TRUNCATE, got %d", len(res.Rows))
	}

	// 15. DROP TABLE
	_, err = db.Exec("DROP TABLE departments")
	if err != nil {
		t.Fatalf("DROP TABLE failed: %v", err)
	}
	if _, exists := db.Catalog().GetTable("departments"); exists {
		t.Fatalf("Table departments still exists after DROP")
	}
}
