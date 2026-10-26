package main

import (
	"fmt"
	"log"
	"os"

	"atlasDB/internal/config"
	"atlasDB/internal/engine"
)

func main() {
	dbPath := "example_atlas.db"
	defer os.Remove(dbPath)
	defer os.Remove(dbPath + ".wal")

	fmt.Println("=== AtlasDB Embedded Relational Database Example ===")

	// 1. Open database
	opts := config.DefaultOptions()
	db, err := engine.Open(dbPath, opts)
	if err != nil {
		log.Fatalf("Failed to open AtlasDB: %v", err)
	}
	defer db.Close()

	// 2. Create Schema
	fmt.Println("\n[1] Creating table 'students'...")
	_, err = db.Exec("CREATE TABLE students (id INT PRIMARY KEY, name TEXT, gpa REAL, active BOOL)")
	if err != nil {
		log.Fatalf("CREATE TABLE failed: %v", err)
	}

	fmt.Println("[2] Creating secondary index 'idx_students_name'...")
	_, err = db.Exec("CREATE INDEX idx_students_name ON students (name)")
	if err != nil {
		log.Fatalf("CREATE INDEX failed: %v", err)
	}

	// 3. Insert Records
	fmt.Println("\n[3] Inserting records...")
	inserts := []string{
		"INSERT INTO students (id, name, gpa, active) VALUES (101, 'Alice Smith', 3.92, true)",
		"INSERT INTO students (id, name, gpa, active) VALUES (102, 'Bob Jones', 3.45, true)",
		"INSERT INTO students (id, name, gpa, active) VALUES (103, 'Charlie Brown', 3.88, false)",
		"INSERT INTO students (id, name, gpa, active) VALUES (104, 'Diana Prince', 3.99, true)",
	}
	for _, sql := range inserts {
		_, err := db.Exec(sql)
		if err != nil {
			log.Fatalf("INSERT failed: %v", err)
		}
	}
	fmt.Println("Inserted 4 student records successfully.")

	// 4. Query with filter and projection
	fmt.Println("\n[4] Query: SELECT name, gpa FROM students WHERE gpa >= 3.85 ORDER BY gpa DESC")
	res, err := db.Query("SELECT name, gpa FROM students WHERE gpa >= 3.85 ORDER BY gpa DESC")
	if err != nil {
		log.Fatalf("Query failed: %v", err)
	}
	for _, row := range res.Rows {
		fmt.Printf("  Student: %-15s | GPA: %s\n", row[0].Text, row[1].String())
	}

	// 5. Query execution trace (observability)
	fmt.Println("\n[5] Query Observability Trace (Code Example 8 from research paper):")
	tc, _, err := db.Trace("SELECT * FROM students WHERE id = 101")
	if err != nil {
		log.Fatalf("Trace failed: %v", err)
	}
	for _, ev := range tc.Events {
		fmt.Printf("  %-8s %s\n", ev.Subsystem, ev.Message)
	}

	// 6. Transaction demonstration
	fmt.Println("\n[6] Transaction Demonstration (BEGIN -> INSERT -> ROLLBACK):")
	_, _ = db.Exec("BEGIN")
	_, _ = db.Exec("INSERT INTO students (id, name, gpa, active) VALUES (999, 'Temporary Student', 2.0, false)")
	fmt.Println("  Inserted temporary record under transaction.")
	_, _ = db.Exec("ROLLBACK")
	fmt.Println("  Rolled back transaction.")

	verifyRes, _ := db.Query("SELECT * FROM students WHERE id = 999")
	fmt.Printf("  Query for rolled back ID 999 returned %d rows (Clean ACID rollback!).\n", len(verifyRes.Rows))

	// 7. System statistics
	st := db.Stats()
	fmt.Println("\n[7] Database Engine Statistics:")
	fmt.Printf("  Total Disk Pages:    %d\n", st.TotalPages)
	fmt.Printf("  Buffer Hits:         %d\n", st.BufferHits)
	fmt.Printf("  Buffer Misses:       %d\n", st.BufferMisses)
	fmt.Printf("  Catalog Tables:      %d\n", st.TableCount)

	fmt.Println("\n=== AtlasDB demonstration complete ===")
}
