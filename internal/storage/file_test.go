package storage

import (
	"bytes"
	"path/filepath"
	"testing"
)

func TestStorageFile(t *testing.T) {
	tempDir := t.TempDir()
	filePath := filepath.Join(tempDir, "test_storage.db")

	file, err := Open(filePath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer file.Close()

	data1 := []byte("hello atlas storage")
	if err := file.WriteAt(0, data1); err != nil {
		t.Fatalf("WriteAt(0) failed: %v", err)
	}

	data2 := []byte("second block at 4096")
	if err := file.WriteAt(4096, data2); err != nil {
		t.Fatalf("WriteAt(4096) failed: %v", err)
	}

	size, err := file.Size()
	if err != nil {
		t.Fatalf("Size() failed: %v", err)
	}
	expectedMinSize := int64(4096 + len(data2))
	if size < expectedMinSize {
		t.Fatalf("Expected size at least %d, got %d", expectedMinSize, size)
	}

	readBuf := make([]byte, len(data1))
	if err := file.ReadAt(0, readBuf); err != nil {
		t.Fatalf("ReadAt(0) failed: %v", err)
	}
	if !bytes.Equal(readBuf, data1) {
		t.Fatalf("Expected %s, got %s", data1, readBuf)
	}

	readBuf2 := make([]byte, len(data2))
	if err := file.ReadAt(4096, readBuf2); err != nil {
		t.Fatalf("ReadAt(4096) failed: %v", err)
	}
	if !bytes.Equal(readBuf2, data2) {
		t.Fatalf("Expected %s, got %s", data2, readBuf2)
	}

	if err := file.Sync(); err != nil {
		t.Fatalf("Sync() failed: %v", err)
	}

	if err := file.Close(); err != nil {
		t.Fatalf("Close() failed: %v", err)
	}

	// Verify persistence after reopening
	reopened, err := Open(filePath)
	if err != nil {
		t.Fatalf("Reopen failed: %v", err)
	}
	defer reopened.Close()

	readBuf3 := make([]byte, len(data1))
	if err := reopened.ReadAt(0, readBuf3); err != nil {
		t.Fatalf("Reopened ReadAt(0) failed: %v", err)
	}
	if !bytes.Equal(readBuf3, data1) {
		t.Fatalf("Reopened verify failed")
	}
}
