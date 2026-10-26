package benchmarks

import (
	"fmt"
	"math/rand"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"atlasDB/internal/btree"
	"atlasDB/internal/buffer"
	"atlasDB/internal/common"
	"atlasDB/internal/config"
	"atlasDB/internal/engine"
	"atlasDB/internal/pager"
	"atlasDB/internal/recovery"
	"atlasDB/internal/storage"
	"atlasDB/internal/wal"
)

func printBenchmarkEnv(b *testing.B, workload string) {
	b.Logf("Benchmark Environment:")
	b.Logf("  Workload:     %s", workload)
	b.Logf("  OS/Arch:      %s/%s", runtime.GOOS, runtime.GOARCH)
	b.Logf("  Go Version:   %s", runtime.Version())
	b.Logf("  Page Size:    4096 bytes")
	b.Logf("  Cache Size:   256 frames (1 MB)")
}

// 1. Insertion Throughput
func BenchmarkInsertThroughput(b *testing.B) {
	tempDir := b.TempDir()
	dbPath := filepath.Join(tempDir, "bench_insert.db")

	db, err := engine.Open(dbPath)
	if err != nil {
		b.Fatalf("Open failed: %v", err)
	}
	defer db.Close()

	_, _ = db.Exec("CREATE TABLE bench_insert (id INT PRIMARY KEY, name TEXT, val REAL)")

	payload := "Benchmark record payload string for AtlasDB engine throughput"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rowID := int64(i + 1)
		sql := fmt.Sprintf("INSERT INTO bench_insert (id, name, val) VALUES (%d, '%s', %f)", rowID, payload, float64(i)*1.5)
		if _, err := db.Exec(sql); err != nil {
			b.Fatalf("Insert error at %d: %v", i, err)
		}
	}
	b.StopTimer()
}

// 2. Point Lookup Latency
func BenchmarkPointLookup(b *testing.B) {
	tempDir := b.TempDir()
	dbPath := filepath.Join(tempDir, "bench_lookup.db")

	db, err := engine.Open(dbPath)
	if err != nil {
		b.Fatalf("Open failed: %v", err)
	}
	defer db.Close()

	_, _ = db.Exec("CREATE TABLE bench_lookup (id INT PRIMARY KEY, name TEXT)")

	nRows := 500
	for i := 1; i <= nRows; i++ {
		if _, err := db.Exec(fmt.Sprintf("INSERT INTO bench_lookup (id, name) VALUES (%d, 'user_%d')", i, i)); err != nil {
			b.Fatalf("Insert failed at %d: %v", i, err)
		}
	}

	rnd := rand.New(rand.NewSource(42))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		targetID := rnd.Intn(nRows) + 1
		res, err := db.Query(fmt.Sprintf("SELECT * FROM bench_lookup WHERE id = %d", targetID))
		if err != nil || len(res.Rows) != 1 {
			b.Fatalf("Lookup failed for %d: %v (rows=%d)", targetID, err, len(res.Rows))
		}
	}
	b.StopTimer()
}

// 3. Sequential Range Scans
func BenchmarkSequentialRangeScan(b *testing.B) {
	tempDir := b.TempDir()
	dbPath := filepath.Join(tempDir, "bench_scan.db")

	f, _ := storage.Open(dbPath)
	p, _ := pager.Open(f, config.DefaultOptions())
	bp := buffer.NewBufferPool(p, 512)
	tree, _ := btree.CreateTree(bp)

	nRows := 2000
	for i := 1; i <= nRows; i++ {
		_ = tree.Insert(int64(i), []byte(fmt.Sprintf("range_scan_data_%d", i)))
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cursor, err := btree.NewCursor(tree, 1)
		if err != nil {
			b.Fatalf("Cursor failed: %v", err)
		}
		scanned := 0
		for {
			_, _, ok, err := cursor.Next()
			if err != nil || !ok {
				break
			}
			scanned++
		}
		_ = cursor.Close()
		if scanned != nRows {
			b.Fatalf("Scanned %d rows, expected %d", scanned, nRows)
		}
	}
	b.StopTimer()
	_ = bp.Close()
	_ = p.Close()
}

// 4. Deletion Throughput
func BenchmarkDeletionThroughput(b *testing.B) {
	tempDir := b.TempDir()
	dbPath := filepath.Join(tempDir, "bench_delete.db")

	f, _ := storage.Open(dbPath)
	p, _ := pager.Open(f, config.DefaultOptions())
	defer p.Close()
	bp := buffer.NewBufferPool(p, 512)
	defer bp.Close()
	tree, _ := btree.CreateTree(bp)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		rowID := int64(i + 1)
		_ = tree.Insert(rowID, []byte("delete_payload"))
		b.StartTimer()
		if err := tree.Delete(rowID); err != nil {
			b.Fatalf("Delete error at %d: %v", rowID, err)
		}
	}
	b.StopTimer()
}

// 5. Buffer Pool Hit Ratio
func BenchmarkBufferHitRatio(b *testing.B) {
	tempDir := b.TempDir()
	dbPath := filepath.Join(tempDir, "bench_buf.db")

	f, _ := storage.Open(dbPath)
	p, _ := pager.Open(f, config.DefaultOptions())
	bp := buffer.NewBufferPool(p, 64)

	// Allocate 100 pages
	for i := 0; i < 100; i++ {
		_, _ = p.AllocatePage()
	}

	rnd := rand.New(rand.NewSource(12345))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// 80% access to first 20 pages (hot set), 20% to remaining 80 pages (cold set)
		var pageID common.PageID
		if rnd.Float64() < 0.8 {
			pageID = common.PageID(rnd.Intn(20) + 1)
		} else {
			pageID = common.PageID(rnd.Intn(80) + 21)
		}
		frame, err := bp.FetchPage(pageID)
		if err == nil {
			_ = bp.Unpin(pageID, false)
			_ = frame
		}
	}
	b.StopTimer()

	hits, misses, evictions, _, _ := bp.Stats()
	total := hits + misses
	var ratio float64
	if total > 0 {
		ratio = float64(hits) / float64(total) * 100.0
	}
	b.Logf("Buffer Pool Performance: %d hits, %d misses (%.2f%% hit ratio), %d evictions",
		hits, misses, ratio, evictions)

	_ = bp.Close()
	_ = p.Close()
}

// 6. WAL Append Throughput
func BenchmarkWALAppendThroughput(b *testing.B) {
	tempDir := b.TempDir()
	walPath := filepath.Join(tempDir, "bench_wal.wal")

	w, err := wal.Open(walPath, false)
	if err != nil {
		b.Fatalf("Open WAL failed: %v", err)
	}
	defer w.Close()

	rec := &wal.LogRecord{
		Type:   wal.RecordTypePageUpdate,
		TxID:   1,
		PageID: 2,
		Offset: 64,
		Before: []byte("before_image_sample_bytes_12345"),
		After:  []byte("after_image_sample_bytes_67890_"),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := w.Append(rec); err != nil {
			b.Fatalf("Append failed: %v", err)
		}
	}
	b.StopTimer()
}

// 7. Recovery Time
func BenchmarkRecoveryTime(b *testing.B) {
	tempDir := b.TempDir()
	dbPath := filepath.Join(tempDir, "bench_rec.db")
	walPath := filepath.Join(tempDir, "bench_rec.wal")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		// Setup DB and WAL with 200 committed page updates
		f, _ := storage.Open(dbPath)
		p, _ := pager.Open(f, config.DefaultOptions())
		for j := 0; j < 5; j++ {
			_, _ = p.AllocatePage()
		}
		_ = p.Close()

		w, _ := wal.Open(walPath, false)
		for j := 0; j < 200; j++ {
			txID := common.TxID(j + 1)
			_ = w.Append(&wal.LogRecord{Type: wal.RecordTypeBegin, TxID: txID})
			_ = w.Append(&wal.LogRecord{
				Type:   wal.RecordTypePageUpdate,
				TxID:   txID,
				PageID: common.PageID(2 + (j % 4)),
				Offset: 0,
				Before: []byte("old_data"),
				After:  []byte("new_recovered_data"),
			})
			_ = w.Append(&wal.LogRecord{Type: wal.RecordTypeCommit, TxID: txID})
		}
		_ = w.Close()

		// Benchmark Recovery execution
		fRec, _ := storage.Open(dbPath)
		pRec, _ := pager.Open(fRec, config.DefaultOptions())
		bpRec := buffer.NewBufferPool(pRec, 64)
		wRec, _ := wal.Open(walPath, false)

		b.StartTimer()
		_, err := recovery.Recover(wRec, bpRec)
		b.StopTimer()

		if err != nil {
			b.Fatalf("Recover failed: %v", err)
		}

		_ = bpRec.Close()
		_ = pRec.Close()
		_ = wRec.Close()
	}
}

var _ = time.Now
var _ = printBenchmarkEnv
