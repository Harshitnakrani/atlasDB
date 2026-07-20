# SQLite Architecture & Design Notes

> **Source**: *SQLite: Past, Present, and Future* (Gaffney et al., VLDB 2022)  
> **Target Project**: `atlasDB` (Building an SQLite-compatible / inspired RDBMS clone from scratch in Go)

---

## 1. High-Level Design Principles & Vision

SQLite is an **in-process**, **embedded**, **zero-configuration**, **transactional** relational database engine. When designing `atlasDB` from scratch in Go, keep these foundational goals in mind:

1. **Embedded / In-Process**:
   - No separate server process, socket connection, or network daemon.
   - Applications interact with the engine via direct method/function calls.
2. **Single-File Storage Format**:
   - The entire database (tables, indexes, schema metadata) resides in a single, cross-platform binary file.
   - Portable across 32-bit/64-bit and little-endian/big-endian systems.
3. **Compact & Self-Contained**:
   - Zero external dependencies beyond standard runtime environment.
   - Minimal memory footprint and binary footprint.
4. **Acid Compliance & Reliability**:
   - Full ACID transactional guarantees (Atomicity, Consistency, Isolation, Durability).
   - Defensive programming and high test coverage (fuzz testing, crash simulation, power-loss resilience).

---

## 2. Modular Architecture Breakdown

SQLite follows a strict multi-layered modular architecture divided into four main groups:

```
+-------------------------------------------------------------+
|                      User Application                       |
+-------------------------------------------------------------+
                               |
+------------------------------v------------------------------+
|                         CORE ENGINE                         |
|  +--------------------+   +------------------------------+  |
|  |     Interface      |   |    SQL Command Processor     |  |
|  +--------------------+   +------------------------------+  |
|  |                 Virtual Machine (VDBE)                |  |
|  +-------------------------------------------------------+  |
+------------------------------|------------------------------+
                               | Bytecode
+------------------------------v------------------------------+
|                         SQL COMPILER                        |
|  +------------------+  +------------------+  +-----------+  |
|  |    Tokenizer     |->|      Parser      |->|   Code    |  |
|  |                  |  |                  |  | Generator |  |
|  +------------------+  +------------------+  +-----------+  |
+------------------------------|------------------------------+
                               | Operations
+------------------------------v------------------------------+
|                           BACKEND                           |
|  +-------------------------------------------------------+  |
|  |                        B-Tree                         |  |
|  +-------------------------------------------------------+  |
|  |                      Page Cache                       |  |
|  +-------------------------------------------------------+  |
|  |               OS Interface (VFS Layer)                |  |
|  +-------------------------------------------------------+  |
+-------------------------------------------------------------+
```

---

### 2.1 The SQL Compiler (Frontend)
The compiler pipelines user SQL strings into executable virtual bytecode programs.

1. **Tokenizer**: Scans SQL input string and converts it into a stream of tokens (keywords, identifiers, operators).
2. **Parser**: Constructs an Abstract Syntax Tree (AST) checking for grammatical correctness (SQLite uses `Lemon` parser generator).
3. **Code Generator / Query Optimizer**:
   - Transforms AST into an optimized execution plan.
   - Generates a sequence of low-level virtual instructions (**VDBE Bytecode**).

---

### 2.2 Core Execution Engine (Virtual Machine / VDBE)

The **Virtual Database Engine (VDBE)** is the central heartbeat of SQLite.

- **Bytecode Execution**: Executes the bytecode program emitted by the SQL Compiler.
- **Register-Based VM Structure**: Operates on fixed registers (P1, P2, P3, P4, P5 operands per instruction).
- **Execution Loop**: Implemented as a massive, fast `switch` statement loop executing instructions sequentially or jumping based on conditionals until encountering a `Halt` instruction.
- **Key Bytecode Opcodes**:
  - `Init`: Initialize execution state and jump to start address.
  - `OpenRead` / `OpenWrite`: Open cursor on table or index B-tree.
  - `SeekRowid`: Search B-tree index for a specific 64-bit row ID.
  - `Column`: Extract column $P_2$ from record under cursor and store in register $P_3$.
  - `Lt` / `Gt` / `Ge` / `Ne` / `Eq`: Branching / conditional check opcodes.
  - `ResultRow`: Return current register payload as a row to caller.
  - `Halt`: Terminate execution and release temporary resources.

---

### 2.3 Backend Modules (Storage & I/O)

#### A. B-Tree Module
SQLite database files are structured as a set of B-trees:
- **Table B-trees (B+ Trees)**:
  - Key is always a 64-bit signed integer (`rowid`).
  - Table data / column values are stored exclusively in leaf nodes.
  - Tables with `INTEGER PRIMARY KEY` alias `rowid` directly.
- **Index B-trees (B-Trees)**:
  - Store arbitrary key values (e.g., text, real, composite keys).
  - Contain no separate leaf data payload—all payload is embedded inside index keys.
- **`WITHOUT ROWID` Tables**:
  - Stored entirely inside Index B-trees where key = Primary Key columns + remaining columns.

#### B. Page Cache (Pager Layer)
- Manages reading/writing fixed-size database pages (default 4 KiB) between memory and disk.
- Implements caching algorithms (LRU / Clock) and tracks dirty pages.
- Coordinates transactional rollback and WAL logging.

#### C. OS Interface (Virtual File System / VFS)
- Provides an abstract file system abstraction layer (`vfs`).
- Encapsulates low-level OS calls (`open`, `read`, `write`, `sync`, `flock`).
- Ensures portability across operating systems (Windows, Linux, macOS, custom OS environments).

---

## 3. Transaction Model & Concurrency

SQLite supports two primary modes for achieving ACID guarantees: **Rollback Journal Mode** and **Write-Ahead Logging (WAL) Mode**.

### 3.1 Rollback Journal Mode
- **Mechanism**: Modifies pages directly in the database file after backing up original un-modified pages into a separate `.db-journal` file.
- **Lock Progression**:
  1. `UNLOCKED`
  2. `SHARED` (Reading allowed)
  3. `RESERVED` (One writer modifying pages in user space memory; journal created)
  4. `EXCLUSIVE` (Journal synced to disk; modified pages written to DB file)
- **Commit**: Invalidate journal file (`DELETE`, `TRUNCATE`, or `PERSIST` header zeroing).
- **Concurrency Limitation**: Writer blocks all readers; single-writer, single-reader execution model.

### 3.2 Write-Ahead Log (WAL) Mode
- **Mechanism**: Inverts rollback mode! Original pages remain untouched in the database file; new/updated pages append sequentially to a `.wal` file.
- **WAL Index (`.shm`)**: Shared memory hash table accelerating WAL page lookups for active readers.
- **Checkpointing**: Periodically transfers updated pages from `.wal` back to main `.db` file.
- **Advantages**:
  - **High Concurrency**: Readers do not block writers, and writers do not block readers.
  - **Sequential Writes**: Faster write throughput.

---

## 4. Record Format & Flexible Typing

SQLite uses a dynamically-typed, row-oriented record format.

### 4.1 Flexible Typing System
- Columns are not restricted to fixed static data types (except `INTEGER PRIMARY KEY`).
- Data type information is stored alongside each individual *value*, not locked per *column*.

### 4.2 Record Byte Layout
Each record stored in a B-tree leaf node consists of two main sections:

```
+-------------------------------------------------------------------------+
|                          RECORD BINARY LAYOUT                           |
+-----------------------------+-------------------------------------------+
|         HEADER              |                   BODY                    |
| [Header Size] [Serial Types]| [Value 1] [Value 2] ... [Value N]         |
+-----------------------------+-------------------------------------------+
```

1. **Header**:
   - `Header Size` (varint encoding header size in bytes).
   - `Serial Type Codes` (varints encoding the payload data type and byte length for each column).
2. **Body**:
   - Raw binary bytes for column values laid out contiguously.

### 4.3 Value Extraction Overhead
- To extract column $K$, SQLite reads `Header Size`, then iterates sequentially through serial types $0 \dots K-1$, accumulating byte lengths to compute offset into the record body.
- *Performance note*: Random column access incurs variable offset calculation overhead compared to fixed-offset columnar layouts.

---

## 5. OLAP vs. OLTP Considerations & Optimizations

While SQLite excels at transactional workloads (OLTP), analytical queries (OLAP) face bottlenecks due to row-oriented storage and B-tree probe overhead.

### 5.1 Analytical Bottlenecks Identified
1. **`SeekRowid` Overhead**: Inner-loop index B-tree lookups during multi-table star joins.
2. **`Column` Extraction**: Header-walking serialization overhead.

### 5.2 Key Optimization: Bloom Filters (Lookahead Information Passing / LIP)
- **Problem**: In multi-table join pipelines (e.g., SSB / TPC-H star schemas), SQLite checks outer table rows against inner dimension table indexes even if join conditions fail.
- **Solution (LIP with Bloom Filters)**:
  - Build Bloom filters on inner dimension tables before join execution.
  - Insert virtual instructions (`FilterAdd` and `Filter`) into VDBE bytecode.
  - Probe Bloom filters upfront *before* executing expensive `SeekRowid` B-tree probes.
- **Result**: Up to **4.2X overall query speedup** on analytical benchmarks without altering backwards-compatible file formats.

---

## 6. Takeaways & Blueprint for `atlasDB` (Go Implementation)

When building `atlasDB` in Go, consider structuring the project around these SQLite architectural layers:

| Layer | Recommended Go Architecture / Package | Purpose |
| :--- | :--- | :--- |
| **VFS** | `pkg/vfs` | Interface over `os.File` for cross-platform OS operations & memory mapping. |
| **Pager** | `pkg/pager` | Page cache managing fixed byte buffers (`[]byte`), dirty flags, WAL & locking. |
| **B-Tree** | `pkg/btree` | Table B+Tree and Index B-Tree implementations working on page frames. |
| **Record Encoding** | `pkg/record` | Encoder/Decoder for SQLite dynamic header/body varint record format. |
| **VDBE Engine** | `pkg/vdbe` | Register machine loop executing bytecode opcodes on database cursors. |
| **SQL Compiler** | `pkg/parser`, `pkg/planner` | Tokenizer, AST parser, cost-based optimizer, and bytecode code generator. |

---