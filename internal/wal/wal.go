package wal

import (
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"sync"

	"atlasDB/internal/common"
	"atlasDB/internal/storage"
)

type RecordType uint8

const (
	RecordTypeBegin RecordType = 1
	RecordTypeCommit RecordType = 2
	RecordTypeRollback RecordType = 3
	RecordTypePageUpdate RecordType = 4
	RecordTypeCheckpoint RecordType = 5
)

// LogRecord encapsulates a WAL entry describing a transactional page-level change.
type LogRecord struct {
	Type   RecordType
	TxID   common.TxID
	PageID common.PageID
	Offset uint16
	Before []byte
	After  []byte
}

// WAL provides Write-Ahead Logging for durability and crash recovery.
type WAL struct {
	mu           sync.Mutex
	file         *storage.File
	path         string
	syncOnCommit bool
	lastOffset   int64
}

func Open(path string, syncOnCommit bool) (*WAL, error) {
	f, err := storage.Open(path)
	if err != nil {
		return nil, err
	}

	size, err := f.Size()
	if err != nil {
		_ = f.Close()
		return nil, err
	}

	return &WAL{
		file:         f,
		path:         path,
		syncOnCommit: syncOnCommit,
		lastOffset:   size,
	}, nil
}

// Append serializes and appends a LogRecord to the WAL file.
func (w *WAL) Append(r *LogRecord) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	data := w.encodeRecord(r)
	if err := w.file.WriteAt(w.lastOffset, data); err != nil {
		return fmt.Errorf("wal append failed: %w", err)
	}
	w.lastOffset += int64(len(data))

	if r.Type == RecordTypeCommit && w.syncOnCommit {
		return w.file.Sync()
	}
	return nil
}

func (w *WAL) encodeRecord(r *LogRecord) []byte {
	beforeLen := uint16(len(r.Before))
	afterLen := uint16(len(r.After))
	// Header: Type(1) + TxID(8) + PageID(8) + Offset(2) + BeforeLen(2) + AfterLen(2) = 23 bytes
	// Payload: BeforeLen + AfterLen
	// Checksum: CRC32(4)
	totalLen := 23 + int(beforeLen) + int(afterLen) + 4
	buf := make([]byte, totalLen)

	buf[0] = byte(r.Type)
	binary.BigEndian.PutUint64(buf[1:9], uint64(r.TxID))
	binary.BigEndian.PutUint64(buf[9:17], uint64(r.PageID))
	binary.BigEndian.PutUint16(buf[17:19], r.Offset)
	binary.BigEndian.PutUint16(buf[19:21], beforeLen)
	binary.BigEndian.PutUint16(buf[21:23], afterLen)

	idx := 23
	if beforeLen > 0 {
		copy(buf[idx:], r.Before)
		idx += int(beforeLen)
	}
	if afterLen > 0 {
		copy(buf[idx:], r.After)
		idx += int(afterLen)
	}

	csum := crc32.ChecksumIEEE(buf[:idx])
	binary.BigEndian.PutUint32(buf[idx:], csum)

	return buf
}

// ReadAll iterates over the WAL file and returns all uncorrupted log records.
func (w *WAL) ReadAll() ([]*LogRecord, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	size, err := w.file.Size()
	if err != nil {
		return nil, err
	}
	if size == 0 {
		return nil, nil
	}

	raw := make([]byte, size)
	if err := w.file.ReadAt(0, raw); err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}

	var records []*LogRecord
	var offset int64 = 0

	for offset+27 <= size { // minimum record size: 23 header + 4 checksum
		recType := RecordType(raw[offset])
		txID := common.TxID(binary.BigEndian.Uint64(raw[offset+1 : offset+9]))
		pageID := common.PageID(binary.BigEndian.Uint64(raw[offset+9 : offset+17]))
		pageOffset := binary.BigEndian.Uint16(raw[offset+17 : offset+19])
		beforeLen := binary.BigEndian.Uint16(raw[offset+19 : offset+21])
		afterLen := binary.BigEndian.Uint16(raw[offset+21 : offset+23])

		bodyLen := int(beforeLen) + int(afterLen)
		recTotalLen := 23 + bodyLen + 4
		if offset+int64(recTotalLen) > size {
			// Incomplete torn record at the end
			break
		}

		csumExpected := binary.BigEndian.Uint32(raw[offset+int64(23+bodyLen) : offset+int64(recTotalLen)])
		csumActual := crc32.ChecksumIEEE(raw[offset : offset+int64(23+bodyLen)])
		if csumExpected != csumActual {
			// Torn write or corrupted record; stop replay at clean boundary
			break
		}

		var before, after []byte
		idx := offset + 23
		if beforeLen > 0 {
			before = make([]byte, beforeLen)
			copy(before, raw[idx:idx+int64(beforeLen)])
			idx += int64(beforeLen)
		}
		if afterLen > 0 {
			after = make([]byte, afterLen)
			copy(after, raw[idx:idx+int64(afterLen)])
		}

		records = append(records, &LogRecord{
			Type:   recType,
			TxID:   txID,
			PageID: pageID,
			Offset: pageOffset,
			Before: before,
			After:  after,
		})

		offset += int64(recTotalLen)
	}

	return records, nil
}

// Truncate clears the WAL after a successful checkpoint.
func (w *WAL) Truncate() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if err := w.file.Truncate(0); err != nil {
		return err
	}
	w.lastOffset = 0
	return w.file.Sync()
}

// Sync flushes pending WAL writes to stable storage.
func (w *WAL) Sync() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.file.Sync()
}

// Close closes the WAL file.
func (w *WAL) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.file.Close()
}
