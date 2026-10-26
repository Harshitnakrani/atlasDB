# AtlasDB Documentation

Welcome to the internal engineering documentation and architectural reference for **AtlasDB**, a from-scratch embedded relational database engine implemented in Go.

---

## Documentation Roadmap

```
docs/
├── README.md                          <-- (You are here) Master documentation index
├── architecture/                      <-- Subsystem architectures and design deep-dives
│   ├── overview.md                    <-- End-to-end architecture & execution pipeline
│   ├── storage-and-pager.md           <-- File I/O, 4KB page structure, Page 0 & free list
│   ├── buffer-pool.md                 <-- LRU cache, frame pinning, WAL flush ordering
│   ├── btree-storage-engine.md        <-- Slotted page B+ tree, root swapping, cursor scans
│   ├── query-engine.md                <-- SQL parser, planner, Volcano executor, joins, aggs
│   └── wal-and-recovery.md            <-- Write-Ahead Log format, CRC32, crash recovery protocol
├── decisions/                         <-- Architecture Decision Records (ADRs)
│   ├── 0001-fixed-page-size.md        <-- Selection of 4096-byte pages and slotted layout
│   ├── 0002-wal-durability-protocol.md<-- WAL over rollback journaling & durability rule
│   ├── 0003-b+tree-root-swapping.md   <-- Permanent root page IDs via SQLite-style root swapping
│   ├── 0004-volcano-iterator-model.md <-- Push vs pull execution model selection
│   └── folder-structure.md            <-- Subsystem-oriented repository structure
├── guides/                            <-- Developer and Operator manuals
│   ├── cli-and-observability.md       <-- Tracing, page/WAL inspection, metrics, REPL
│   └── sql-reference.md               <-- Complete SQL grammar, functions, and query types
├── book-notes/                        <-- Systems literature summaries & research references
│   └── notes.md                       <-- SQLite architecture breakdown & database internals
└── journal/                           <-- Implementation logs & engineering lessons learned
    ├── 01-storage-pager-buffer.md     <-- Foundations: raw I/O, paging, and buffer frames
    ├── 02-btree-and-root-swapping.md  <-- Slotted pages, defragmentation, and root migration
    └── 03-query-pipeline-and-wal.md   <-- Volcano operators, WAL replay, and ACID transactions
```

---

## Architectural Principles

AtlasDB is constructed around five core systems engineering principles:

1. **Bottom-Up Construction**:
   Storage primitives (file I/O, pages, buffer manager) are established and verified before higher-level relational features (SQL compiler, executor) are introduced.
2. **Explicit Durability Protocol**:
   Durability is not an afterthought. The Write-Ahead Log ordering rule (*log before dirty page*) is enforced at the buffer pool boundary before any page flush.
3. **Internal Observability**:
   AtlasDB rejects the "black box" database paradigm. Queries can be traced across every subsystem via `atlas trace`, and physical slotted pages and WAL records can be inspected via CLI commands.
4. **Permanent Root Invariance**:
   B+ trees use root swapping during splits so that a table or index root page ID is immutable throughout the lifetime of the database.
5. **Zero External Dependencies**:
   The engine relies purely on the Go standard library, keeping the implementation inspectable, clean, and portable.
