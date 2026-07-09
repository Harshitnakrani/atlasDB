## Atlas Folder Structure

> Atlas is a relational database engine written in Go.
> The project is organized by **database components**, not by Go layers (controllers, services, repositories, etc.). Every directory represents a major subsystem found in a real database engine.

---

# Root Structure

```text
atlas/
│
├── cmd/
├── internal/
├── pkg/
├── tests/
├── benchmarks/
├── examples/
├── docs/
├── scripts/
├── data/
│
├── go.mod
├── Makefile
├── README.md
└── LICENSE
```

---

# cmd/

```
cmd/
└── atlas/
    └── main.go
```

This is the application's entry point.

Responsibilities:

* Parse command-line arguments
* Initialize the database engine
* Open the database file
* Execute commands
* Start interactive mode (future)

This folder should contain **very little logic**.

---

# internal/

This is where the actual database engine lives.

Everything inside `internal/` is considered private to Atlas.

---

## engine/

```
internal/
└── engine/
```

The engine is the public interface of Atlas.

Example:

```go
db, err := engine.Open("atlas.db")

db.Exec("CREATE TABLE users (...)")
db.Exec("INSERT INTO users VALUES (...)")
```

The engine coordinates all database components.

It communicates with:

* Pager
* Parser
* Planner
* Executor
* Transaction Manager

---

## storage/

```
internal/
└── storage/
```

Lowest level of the database.

Responsible for interacting directly with disk.

Responsibilities:

* Open database file
* Read pages
* Write pages
* Allocate new pages
* File management

This package should know **nothing about SQL**.

---

## pager/

```
internal/
└── pager/
```

The pager is the bridge between memory and disk.

It is one of the most important components of the database.

Responsibilities:

* Request pages from storage
* Cache pages
* Mark dirty pages
* Flush pages to disk
* Allocate page IDs

SQLite is heavily built around its pager.

---

## buffer/

```
internal/
└── buffer/
```

Manages the Buffer Pool.

Instead of reading from disk every time, frequently used pages stay in memory.

Future responsibilities:

* LRU eviction
* Clock replacement
* Pin counts
* Dirty page tracking

---

## wal/

```
internal/
└── wal/
```

Write Ahead Log.

Before modifying the database file, every change is written here.

Responsibilities:

* Append log records
* Replay logs
* Checkpoint
* Crash safety

---

## recovery/

```
internal/
└── recovery/
```

Responsible for recovering the database after a crash.

Reads the WAL and restores the database to a consistent state.

---

## btree/

```
internal/
└── btree/
```

Implements the B+ Tree.

This is where indexes and table storage live.

Responsibilities:

* Search
* Insert
* Delete
* Node split
* Node merge
* Tree traversal

---

## catalog/

```
internal/
└── catalog/
```

Stores metadata about the database.

Examples:

* Tables
* Columns
* Indexes
* Schemas

Think of it as Atlas's internal system catalog.

---

## parser/

```
internal/
└── parser/
```

Converts SQL into an Abstract Syntax Tree (AST).

Example:

```sql
SELECT * FROM users;
```

↓

```
SelectStatement
 ├── Table(users)
 └── Projection(*)
```

The parser only understands SQL syntax.

It does **not** execute queries.

---

## planner/

```
internal/
└── planner/
```

Creates an execution plan from the parsed SQL.

Example:

```
SQL

↓

AST

↓

Execution Plan

↓

Executor
```

Future optimizations may include:

* Index selection
* Predicate pushdown
* Join ordering

---

## executor/

```
internal/
└── executor/
```

Runs the execution plan.

Responsibilities:

* Table scan
* Index scan
* Filtering
* Projection
* Aggregation
* Result generation

This is where SQL actually executes.

---

## transaction/

```
internal/
└── transaction/
```

Manages ACID transactions.

Responsibilities:

* BEGIN
* COMMIT
* ROLLBACK

Future features:

* MVCC
* Isolation levels
* Locking

---

## concurrency/

```
internal/
└── concurrency/
```

Handles synchronization between multiple database operations.

Future responsibilities:

* Locks
* Latches
* Deadlock prevention
* Read/write synchronization

Initially this package may remain mostly empty.

---

## cache/

```
internal/
└── cache/
```

Optional higher-level caches.

Possible future use cases:

* Metadata cache
* Query cache
* Prepared statement cache

---

## common/

```
internal/
└── common/
```

Shared types and utilities.

Examples:

* Constants
* Error definitions
* Logger
* Page IDs
* Record IDs
* Utility functions

Every subsystem may depend on this package.

---

## config/

```
internal/
└── config/
```

Database configuration.

Examples:

* Page size
* Cache size
* WAL configuration
* Buffer pool size

Keeping configuration centralized makes Atlas easier to tune.

---

# pkg/

```
pkg/
```

Reusable libraries that could be imported by external projects.

Atlas will likely keep this directory small or even empty.

Most code belongs inside `internal/`.

---

# tests/

```
tests/
```

Integration tests.

Examples:

* SQL execution
* Transaction behavior
* Recovery after crash
* End-to-end database tests

Unit tests should live beside the package they test.

---

# benchmarks/

```
benchmarks/
```

Performance testing.

Examples:

* Insert throughput
* Lookup latency
* Range scan performance
* WAL write speed
* Buffer pool efficiency

Every major optimization should be measurable.

---

# examples/

```
examples/
```

Small programs demonstrating Atlas features.

Examples:

* Creating a database
* Executing SQL
* Using transactions

Useful for both documentation and manual testing.

---

# docs/

```
docs/
```

Contains all project documentation.

Suggested structure:

```
docs/
├── architecture/
├── decisions/
├── book-notes/
└── journal/
```

### architecture/

System diagrams and subsystem explanations.

### decisions/

Architecture Decision Records (ADRs).

Example:

```
0001-page-size.md
```

Document why important technical decisions were made.

### book-notes/

Summaries and insights from *Database Internals* and other resources.

### journal/

Development log documenting progress, challenges, and lessons learned after each coding session.

---

# scripts/

```
scripts/
```

Developer automation.

Examples:

* Build scripts
* Benchmark scripts
* Test runners
* Database cleanup

---

# data/

```
data/
```

Local development database files.

Examples:

```
atlas.db
wal.log
```

These should **never** be committed to Git.

---

# Project Philosophy

Atlas is organized around the architecture of a real database engine.

The flow of a SQL query is:

```
Client
   │
   ▼
Engine
   │
   ▼
Parser
   │
   ▼
Planner
   │
   ▼
Executor
   │
   ▼
B+ Tree
   │
   ▼
Pager
   │
   ▼
Buffer Pool
   │
   ▼
Storage
   │
   ▼
Disk
```

Each directory represents one of these layers, making the codebase easier to understand, maintain, and extend as Atlas grows.
 Atlas Folder Structure

> Atlas is a relational database engine written in Go.
> The project is organized by **database components**, not by Go layers (controllers, services, repositories, etc.). Every directory represents a major subsystem found in a real database engine.

---

# Root Structure

```text
atlas/
│
├── cmd/
├── internal/
├── pkg/
├── tests/
├── benchmarks/
├── examples/
├── docs/
├── scripts/
├── data/
│
├── go.mod
├── Makefile
├── README.md
└── LICENSE
```

---

# cmd/

```
cmd/
└── atlas/
    └── main.go
```

This is the application's entry point.

Responsibilities:

* Parse command-line arguments
* Initialize the database engine
* Open the database file
* Execute commands
* Start interactive mode (future)

This folder should contain **very little logic**.

---

# internal/

This is where the actual database engine lives.

Everything inside `internal/` is considered private to Atlas.

---

## engine/

```
internal/
└── engine/
```

The engine is the public interface of Atlas.

Example:

```go
db, err := engine.Open("atlas.db")

db.Exec("CREATE TABLE users (...)")
db.Exec("INSERT INTO users VALUES (...)")
```

The engine coordinates all database components.

It communicates with:

* Pager
* Parser
* Planner
* Executor
* Transaction Manager

---

## storage/

```
internal/
└── storage/
```

Lowest level of the database.

Responsible for interacting directly with disk.

Responsibilities:

* Open database file
* Read pages
* Write pages
* Allocate new pages
* File management

This package should know **nothing about SQL**.

---

## pager/

```
internal/
└── pager/
```

The pager is the bridge between memory and disk.

It is one of the most important components of the database.

Responsibilities:

* Request pages from storage
* Cache pages
* Mark dirty pages
* Flush pages to disk
* Allocate page IDs

SQLite is heavily built around its pager.

---

## buffer/

```
internal/
└── buffer/
```

Manages the Buffer Pool.

Instead of reading from disk every time, frequently used pages stay in memory.

Future responsibilities:

* LRU eviction
* Clock replacement
* Pin counts
* Dirty page tracking

---

## wal/

```
internal/
└── wal/
```

Write Ahead Log.

Before modifying the database file, every change is written here.

Responsibilities:

* Append log records
* Replay logs
* Checkpoint
* Crash safety

---

## recovery/

```
internal/
└── recovery/
```

Responsible for recovering the database after a crash.

Reads the WAL and restores the database to a consistent state.

---

## btree/

```
internal/
└── btree/
```

Implements the B+ Tree.

This is where indexes and table storage live.

Responsibilities:

* Search
* Insert
* Delete
* Node split
* Node merge
* Tree traversal

---

## catalog/

```
internal/
└── catalog/
```

Stores metadata about the database.

Examples:

* Tables
* Columns
* Indexes
* Schemas

Think of it as Atlas's internal system catalog.

---

## parser/

```
internal/
└── parser/
```

Converts SQL into an Abstract Syntax Tree (AST).

Example:

```sql
SELECT * FROM users;
```

↓

```
SelectStatement
 ├── Table(users)
 └── Projection(*)
```

The parser only understands SQL syntax.

It does **not** execute queries.

---

## planner/

```
internal/
└── planner/
```

Creates an execution plan from the parsed SQL.

Example:

```
SQL

↓

AST

↓

Execution Plan

↓

Executor
```

Future optimizations may include:

* Index selection
* Predicate pushdown
* Join ordering

---

## executor/

```
internal/
└── executor/
```

Runs the execution plan.

Responsibilities:

* Table scan
* Index scan
* Filtering
* Projection
* Aggregation
* Result generation

This is where SQL actually executes.

---

## transaction/

```
internal/
└── transaction/
```

Manages ACID transactions.

Responsibilities:

* BEGIN
* COMMIT
* ROLLBACK

Future features:

* MVCC
* Isolation levels
* Locking

---

## concurrency/

```
internal/
└── concurrency/
```

Handles synchronization between multiple database operations.

Future responsibilities:

* Locks
* Latches
* Deadlock prevention
* Read/write synchronization

Initially this package may remain mostly empty.

---

## cache/

```
internal/
└── cache/
```

Optional higher-level caches.

Possible future use cases:

* Metadata cache
* Query cache
* Prepared statement cache

---

## common/

```
internal/
└── common/
```

Shared types and utilities.

Examples:

* Constants
* Error definitions
* Logger
* Page IDs
* Record IDs
* Utility functions

Every subsystem may depend on this package.

---

## config/

```
internal/
└── config/
```

Database configuration.

Examples:

* Page size
* Cache size
* WAL configuration
* Buffer pool size

Keeping configuration centralized makes Atlas easier to tune.

---

# pkg/

```
pkg/
```

Reusable libraries that could be imported by external projects.

Atlas will likely keep this directory small or even empty.

Most code belongs inside `internal/`.

---

# tests/

```
tests/
```

Integration tests.

Examples:

* SQL execution
* Transaction behavior
* Recovery after crash
* End-to-end database tests

Unit tests should live beside the package they test.

---

# benchmarks/

```
benchmarks/
```

Performance testing.

Examples:

* Insert throughput
* Lookup latency
* Range scan performance
* WAL write speed
* Buffer pool efficiency

Every major optimization should be measurable.

---

# examples/

```
examples/
```

Small programs demonstrating Atlas features.

Examples:

* Creating a database
* Executing SQL
* Using transactions

Useful for both documentation and manual testing.

---

# docs/

```
docs/
```

Contains all project documentation.

Suggested structure:

```
docs/
├── architecture/
├── decisions/
├── book-notes/
└── journal/
```

### architecture/

System diagrams and subsystem explanations.

### decisions/

Architecture Decision Records (ADRs).

Example:

```
0001-page-size.md
```

Document why important technical decisions were made.

### book-notes/

Summaries and insights from *Database Internals* and other resources.

### journal/

Development log documenting progress, challenges, and lessons learned after each coding session.

---

# scripts/

```
scripts/
```

Developer automation.

Examples:

* Build scripts
* Benchmark scripts
* Test runners
* Database cleanup

---

# data/

```
data/
```

Local development database files.

Examples:

```
atlas.db
wal.log
```

These should **never** be committed to Git.

---

# Project Philosophy

Atlas is organized around the architecture of a real database engine.

The flow of a SQL query is:

```
Client
   │
   ▼
Engine
   │
   ▼
Parser
   │
   ▼
Planner
   │
   ▼
Executor
   │
   ▼
B+ Tree
   │
   ▼
Pager
   │
   ▼
Buffer Pool
   │
   ▼
Storage
   │
   ▼
Disk
```

Each directory represents one of these layers, making the codebase easier to understand, maintain, and extend as Atlas grows.
