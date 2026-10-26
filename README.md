# AtlasDB

**A From-Scratch Embedded Relational Database Engine in Go**

AtlasDB is an embedded relational database engine implemented from first principles in Go, based on the research paper:  
*AtlasDB: Design and Implementation of a From-Scratch Embedded Relational Database Engine in Go* (Harshit Nakrani).

---

## Architecture Overview

AtlasDB is structured bottom-up, progressing from raw file I/O to a page-oriented storage layer, B+ tree indexing, relational query execution, write-ahead logging (WAL), and ACID crash recovery.

```
+-------------------------------------------------------------+
|                      SQL Command / REPL                     |
+-------------------------------------------------------------+
                               |
+------------------------------v------------------------------+
|                        SQL Frontend                         |
|   Parser (AST)  -->  Planner (Physical Plan)  -->  Executor |
+-------------------------------------------------------------+
                               |
+------------------------------v------------------------------+
|                       Storage Engine                        |
|   Table B+ Tree (0x0D/0x05)   |   Index B+ Tree (0x0A/0x02) |
+-------------------------------------------------------------+
                               |
+------------------------------v------------------------------+
|                     Memory & Durability                     |
|        Buffer Pool (LRU)      |      WAL & Crash Recovery   |
+-------------------------------------------------------------+
                               |
+------------------------------v------------------------------+
|                      Pager & Raw File                       |
|           4096-Byte Pages     |      storage.File (I/O)     |
+-------------------------------------------------------------+
```

---

## Subsystem Organization

| Directory | Subsystem | Responsibility |
| :--- | :--- | :--- |
| `internal/storage/` | Raw Storage | Low-level positional byte I/O (`os.File`, `ReadAt`, `WriteAt`, `Sync`). |
| `internal/pager/` | Pager | Fixed 4KB disk page addressing, page allocation, and free-list management. |
| `internal/buffer/` | Buffer Pool | LRU frame eviction, cache hit/miss tracking, frame pinning, WAL flush ordering. |
| `internal/record/` | Record Encoding | Compact, architecture-independent binary serialization for tuples (NULL, INT, FLOAT, TEXT, BOOL, BLOB). |
| `internal/btree/` | B+ Tree | Persistent Slotted-Page B+ Tree for Table (`rowid -> payload`) and Secondary Index (`key -> rowid`). Supports point seeks, range cursors, node splitting with root swapping, and invariant validation. |
| `internal/catalog/`| Catalog | Persistent system schema on Page 1 (`atlas_schema`), table & column metadata, index root page mappings. |
| `internal/parser/` | SQL Parser | Lexer, Tokenizer, and recursive-descent parser for `CREATE TABLE`, `CREATE INDEX`, `INSERT`, `SELECT`, `UPDATE`, `DELETE`, `BEGIN`, `COMMIT`, `ROLLBACK`, `TRACE`. |
| `internal/planner/`| Query Planner | Translates AST into physical execution plans: `IndexScan`, `TableScan`, `Filter`, `Project`, `OrderBy`, `Limit`. |
| `internal/executor/`| Executor | Volcano-iterator query executor (`Init`, `Next`, `Close`) and mutation dispatchers. |
| `internal/wal/` | WAL | Write-Ahead Log: CRC32-checksummed page-level redo/undo log records, sync-on-commit, truncate. |
| `internal/recovery/`| Recovery | Automatic crash recovery: analysis pass, redo replaying of committed transactions, uncommitted transaction discarding. |
| `internal/transaction/`| Transactions | ACID transaction coordinator (`Begin`, `Commit`, `Rollback`). |
| `internal/concurrency/`| Concurrency | Table and page frame locking primitives. |
| `internal/engine/` | Public Engine | Top-level coordinator API: `Open`, `Exec`, `Query`, `Trace`, `Stats`, `Close`. |

## Supported SQL Features

AtlasDB provides a rich relational SQL dialect matching core MySQL & ANSI standards:

### 1. Data Definition Language (DDL)
- `CREATE TABLE [IF NOT EXISTS] table (col type [PRIMARY KEY], ...)`
- `CREATE INDEX index_name ON table (col)`
- `DROP TABLE table`
- `DROP INDEX index_name`
- `TRUNCATE TABLE table`
- `ALTER TABLE table ADD COLUMN col type`

### 2. Data Manipulation Language (DML)
- `INSERT INTO table [(cols...)] VALUES (val1, ...), (val2, ...)`
- `UPDATE table SET col = val WHERE condition`
- `DELETE FROM table WHERE condition`

### 3. Querying & Relational Operators (SELECT)
- **Multi-Table Joins**: `SELECT ... FROM tableA [INNER | LEFT] JOIN tableB ON tableA.id = tableB.fk_id`
- **Aggregations**: `COUNT(*)`, `COUNT(col)`, `SUM(col)`, `AVG(col)`, `MIN(col)`, `MAX(col)`
- **Grouping**: `GROUP BY col1, col2` with `HAVING <aggregate_condition>`
- **Deduplication**: `SELECT DISTINCT ...`
- **Filtering & Predicates**:
  - Comparison: `=`, `!=`, `<>`, `<`, `<=`, `>`, `>=`
  - Logic: `AND`, `OR`
  - Pattern Matching: `col LIKE 'pattern%'` (with `%` and `_` wildcards)
  - Set Membership: `col IN (val1, val2, ...)` / `NOT IN (...)`
  - Range Checking: `col BETWEEN low AND high` / `NOT BETWEEN ...`
  - Null Checking: `col IS NULL` / `col IS NOT NULL`
- **Sorting & Paging**: `ORDER BY col [ASC | DESC] LIMIT [offset,] count` or `LIMIT count OFFSET offset`

### 4. Transactions & Observability
- `BEGIN`, `COMMIT`, `ROLLBACK` (ACID transaction isolation)
- `TRACE <query>` (Subsystem execution tracing matching paper Code Example 8)

---

## Building and Running

### Build CLI
```bash
go build -o atlas.exe ./cmd/atlas
```

### Interactive Shell (REPL)
```bash
./atlas.exe repl atlas.db
```
```sql
atlas> CREATE TABLE users (id INT PRIMARY KEY, name TEXT, score REAL);
atlas> INSERT INTO users (id, name, score) VALUES (302, 'Harshit', 99.5);
atlas> SELECT * FROM users WHERE id = 302;
+-----+---------+-----------+
| id  | name    | score     |
+-----+---------+-----------+
| 302 | Harshit | 99.500000 |
+-----+---------+-----------+
(1 rows, took 1.2ms)
```

### Query Execution Tracing (Code Example 8 in Research Paper)
```bash
./atlas.exe trace atlas.db "SELECT * FROM users WHERE id = 302"
```
Output:
```text
Parser   -> OK
Planner  -> IndexScan(users.id)
BTree    -> root page 2
Executor -> 1 row
Buffer   -> hit=1 miss=0
```

### Inspect Physical Pages & WAL
```bash
# Inspect slotted page layout:
./atlas.exe inspect page atlas.db 2

# Inspect Write-Ahead Log:
./atlas.exe inspect wal atlas.db

# View database and buffer metrics:
./atlas.exe stats atlas.db
```

---

## Embedded Go Usage Example

```go
package main

import (
	"fmt"
	"log"

	"atlasDB/internal/config"
	"atlasDB/internal/engine"
)

func main() {
	db, err := engine.Open("my_database.db", config.DefaultOptions())
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	// DDL
	_, _ = db.Exec("CREATE TABLE employees (id INT PRIMARY KEY, name TEXT, salary REAL)")

	// DML
	_, _ = db.Exec("INSERT INTO employees (id, name, salary) VALUES (1, 'Alice', 125000.0)")

	// Query
	res, _ := db.Query("SELECT name, salary FROM employees WHERE id = 1")
	for _, row := range res.Rows {
		fmt.Printf("Employee: %s | Salary: %s\n", row[0].Text, row[1].String())
	}
}
```

---

## Running Tests and Benchmarks

### Correctness Test Suite (Section 17.1 of Paper)
Includes:
- Page round-trip tests
- B+ tree invariant tests under random insertions/deletions
- Sequential range scan completeness
- Record serialization round trips across all types
- Transaction commit and rollback tests
- Crash-recovery tests

```bash
go test -v ./...
```

### Performance Benchmarks (Section 17.2 of Paper)
```bash
go test -bench . -benchmem atlasDB/benchmarks
```
Typical performance results:
- **Point Lookup Latency**: ~9.5 µs / op (~105,000 ops/sec)
- **Sequential Range Scan**: ~20 µs / op (2,000 rows scanned)
- **Buffer Pool Hit Ratio**: **91.02%** hit ratio under skewed workloads
- **WAL Append Throughput**: ~6.8 µs / op (~147,000 appends/sec)
- **Crash Recovery Time**: ~1.7 ms for 200 transaction replays
