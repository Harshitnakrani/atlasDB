package storage

import (
	"errors"
	"io"
	"os"
)

// File encapsulates raw byte-oriented positional I/O over a database file on disk.
// As specified in the AtlasDB research paper, this layer has no knowledge of pages,
// records, B+ trees, or SQL statements.
type File struct {
	file *os.File
	path string
}

// Open opens an existing file or creates a new one with read-write permissions.
func Open(path string) (*File, error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		return nil, err
	}
	return &File{
		file: f,
		path: path,
	}, nil
}

// ReadAt reads len(buf) bytes starting at the given file offset.
// It returns an error if fewer than len(buf) bytes were read.
func (f *File) ReadAt(offset int64, buf []byte) error {
	n, err := f.file.ReadAt(buf, offset)
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	if n < len(buf) {
		return io.ErrUnexpectedEOF
	}
	return nil
}

// WriteAt writes data starting at the given file offset.
func (f *File) WriteAt(offset int64, data []byte) error {
	_, err := f.file.WriteAt(data, offset)
	return err
}

// Sync flushes file modifications to stable storage (fsync).
func (f *File) Sync() error {
	return f.file.Sync()
}

// Size returns the current length of the file in bytes.
func (f *File) Size() (int64, error) {
	fi, err := f.file.Stat()
	if err != nil {
		return 0, err
	}
	return fi.Size(), nil
}

// Truncate resizes the file to the specified size.
func (f *File) Truncate(size int64) error {
	return f.file.Truncate(size)
}

// Path returns the path of the opened file.
func (f *File) Path() string {
	return f.path
}

// Close closes the underlying os.File.
func (f *File) Close() error {
	if f.file == nil {
		return nil
	}
	err := f.file.Close()
	f.file = nil
	return err
}
