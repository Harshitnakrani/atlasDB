package engine

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	"atlasDB/internal/buffer"
	"atlasDB/internal/catalog"
	"atlasDB/internal/common"
	"atlasDB/internal/concurrency"
	"atlasDB/internal/config"
	"atlasDB/internal/executor"
	"atlasDB/internal/pager"
	"atlasDB/internal/parser"
	"atlasDB/internal/planner"
	"atlasDB/internal/recovery"
	"atlasDB/internal/storage"
	"atlasDB/internal/transaction"
	"atlasDB/internal/wal"
)

// DBStats contains system-wide metrics for observability.
type DBStats struct {
	TotalPages     common.PageID
	BufferHits     uint64
	BufferMisses   uint64
	BufferEvicts   uint64
	DirtyPages     int
	PinnedPages    int
	TableCount     int
}

// Engine is the central coordinator of AtlasDB.
type Engine struct {
	mu        sync.Mutex
	opts      config.Options
	storage   *storage.File
	pager     *pager.Pager
	buffer    *buffer.BufferPool
	wal       *wal.WAL
	txMgr     *transaction.Manager
	catalog   *catalog.Catalog
	planner   *planner.Planner
	lockMgr   *concurrency.LockManager
	activeTx  *transaction.Transaction
}

// Open initializes and starts an AtlasDB database instance.
func Open(path string, optList ...config.Options) (*Engine, error) {
	opts := config.DefaultOptions()
	if len(optList) > 0 {
		opts = optList[0]
	}

	storageFile, err := storage.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open storage file: %w", err)
	}

	p, err := pager.Open(storageFile, opts)
	if err != nil {
		_ = storageFile.Close()
		return nil, fmt.Errorf("failed to open pager: %w", err)
	}

	walPath := path + ".wal"
	walLog, err := wal.Open(walPath, opts.WALSyncOnCommit)
	if err != nil {
		_ = p.Close()
		return nil, fmt.Errorf("failed to open WAL: %w", err)
	}

	bp := buffer.NewBufferPool(p, opts.BufferPoolSize)

	// Durability invariant: dirty pages must not become durable before WAL is synced
	bp.SetWALFlushHook(func(pageID common.PageID) error {
		return walLog.Sync()
	})

	// Run recovery on startup
	recStats, err := recovery.Recover(walLog, bp)
	if err != nil {
		_ = bp.Close()
		_ = p.Close()
		_ = walLog.Close()
		return nil, fmt.Errorf("recovery failed: %w", err)
	}

	txMgr := transaction.NewManager(walLog, bp)
	if recStats.MaxTxID > 0 {
		txMgr.SetNextTxID(recStats.MaxTxID)
	}

	cat, err := catalog.LoadCatalog(bp, p.CatalogRoot())
	if err != nil {
		_ = bp.Close()
		_ = p.Close()
		_ = walLog.Close()
		return nil, fmt.Errorf("failed to load catalog: %w", err)
	}

	qPlanner := planner.NewPlanner(cat)
	lockMgr := concurrency.NewLockManager()

	return &Engine{
		opts:    opts,
		storage: storageFile,
		pager:   p,
		buffer:  bp,
		wal:     walLog,
		txMgr:   txMgr,
		catalog: cat,
		planner: qPlanner,
		lockMgr: lockMgr,
	}, nil
}

// Exec executes a SQL string and returns the result.
func (e *Engine) Exec(sql string) (*executor.Result, error) {
	_, res, err := e.execute(sql, nil)
	return res, err
}

// Query is an alias for Exec.
func (e *Engine) Query(sql string) (*executor.Result, error) {
	return e.Exec(sql)
}

// Trace executes a SQL query while recording events across all internal subsystems.
func (e *Engine) Trace(sql string) (*common.TraceContext, *executor.Result, error) {
	tc := common.NewTraceContext()
	return e.execute(sql, tc)
}

func (e *Engine) execute(sql string, tc *common.TraceContext) (*common.TraceContext, *executor.Result, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	// 1. Parser
	stmt, err := parser.ParseSQL(sql)
	if err != nil {
		if tc != nil {
			tc.Add("Parser", "-> ERR: %v", err)
		}
		return tc, nil, err
	}
	if tc != nil {
		tc.Add("Parser", "-> OK")
	}

	// 2. Transaction statements
	switch stmt.(type) {
	case *parser.BeginStmt:
		if e.activeTx != nil {
			return tc, nil, common.ErrTxActive
		}
		tx, err := e.txMgr.Begin()
		if err != nil {
			return tc, nil, err
		}
		e.activeTx = tx
		return tc, &executor.Result{}, nil

	case *parser.CommitStmt:
		if e.activeTx == nil {
			return tc, nil, common.ErrNoActiveTx
		}
		err := e.txMgr.Commit(e.activeTx)
		e.activeTx = nil
		if err != nil {
			return tc, nil, err
		}
		_ = e.buffer.FlushAll()
		return tc, &executor.Result{}, nil

	case *parser.RollbackStmt:
		if e.activeTx == nil {
			return tc, nil, common.ErrNoActiveTx
		}
		err := e.txMgr.Rollback(e.activeTx)
		e.activeTx = nil
		return tc, &executor.Result{}, err
	}

	// 3. Planner
	plan, err := e.planner.Plan(stmt, tc)
	if err != nil {
		return tc, nil, err
	}

	// Snapshot buffer stats before execution for tracing
	var hitsBefore, missesBefore uint64
	if tc != nil {
		hitsBefore, missesBefore, _, _, _ = e.buffer.Stats()
	}

	// 4. Executor
	res, err := executor.Dispatch(plan, e.buffer, e.catalog, tc, e.activeTx)
	if err != nil {
		return tc, nil, err
	}

	if tc != nil {
		hitsAfter, missesAfter, _, _, _ := e.buffer.Stats()
		tc.Add("Buffer", "-> hit=%d miss=%d", hitsAfter-hitsBefore, missesAfter-missesBefore)
	}

	// Auto-flush catalog & buffer on DDL / DML if outside explicit transaction
	if e.activeTx == nil {
		switch plan.Type() {
		case planner.PlanTypeCreateTable, planner.PlanTypeCreateIndex, planner.PlanTypeInsert,
			planner.PlanTypeUpdate, planner.PlanTypeDelete, planner.PlanTypeDropTable,
			planner.PlanTypeDropIndex, planner.PlanTypeTruncateTable, planner.PlanTypeAlterTable:
			_ = e.catalog.Save()
			_ = e.buffer.FlushAll()
		}
	}

	return tc, res, nil
}

// Stats returns system metrics.
func (e *Engine) Stats() DBStats {
	e.mu.Lock()
	defer e.mu.Unlock()

	hits, misses, evicts, dirty, pinned := e.buffer.Stats()
	return DBStats{
		TotalPages:   e.pager.NumPages(),
		BufferHits:   hits,
		BufferMisses: misses,
		BufferEvicts: evicts,
		DirtyPages:   dirty,
		PinnedPages:  pinned,
		TableCount:   len(e.catalog.ListTables()),
	}
}

// Catalog returns the schema catalog.
func (e *Engine) Catalog() *catalog.Catalog {
	return e.catalog
}

// BufferPool returns the buffer pool.
func (e *Engine) BufferPool() *buffer.BufferPool {
	return e.buffer
}

// Pager returns the pager.
func (e *Engine) Pager() *pager.Pager {
	return e.pager
}

// WAL returns the WAL instance.
func (e *Engine) WAL() *wal.WAL {
	return e.wal
}

// Close gracefully flushes all memory frames and closes underlying files.
func (e *Engine) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.activeTx != nil {
		_ = e.txMgr.Rollback(e.activeTx)
		e.activeTx = nil
	}

	_ = e.catalog.Save()
	_ = e.buffer.FlushAll()
	_ = e.buffer.Close()
	_ = e.pager.Close()
	if e.wal != nil {
		_ = e.wal.Close()
	}
	return nil
}

var _ = filepath.Join
var _ = strings.ToUpper
