package pager

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"sync"

	"atlasDB/internal/common"
	"atlasDB/internal/config"
	"atlasDB/internal/storage"
)

var (
	MagicBytes = []byte("ATLAS_DB")
	ErrBadMagic = errors.New("atlas: invalid database file signature")
	ErrInvalidPageID = errors.New("atlas: invalid page id")
)

const (
	HeaderMagicOffset       = 0
	HeaderPageSizeOffset    = 8
	HeaderVersionOffset     = 12
	HeaderPageCountOffset   = 16
	HeaderFreeListOffset    = 24
	HeaderCatalogRootOffset = 32
	DbHeaderLength          = 100
)

// Page represents a fixed-size disk page.
type Page struct {
	ID   common.PageID
	Data []byte
}

// Clone returns an independent deep copy of the page data.
func (p *Page) Clone() *Page {
	cp := make([]byte, len(p.Data))
	copy(cp, p.Data)
	return &Page{
		ID:   p.ID,
		Data: cp,
	}
}

// Pager is responsible for page-level access, allocation, and translation
// between logical PageIDs and physical file offsets.
type Pager struct {
	mu          sync.RWMutex
	file        *storage.File
	pageSize    int
	numPages    common.PageID
	freeListHead common.PageID
	catalogRoot common.PageID
}

// Open initializes or loads a pager over the provided storage file.
func Open(file *storage.File, opts config.Options) (*Pager, error) {
	pageSize := opts.PageSize
	if pageSize <= 0 {
		pageSize = 4096
	}

	size, err := file.Size()
	if err != nil {
		return nil, err
	}

	p := &Pager{
		file:         file,
		pageSize:     pageSize,
		freeListHead: common.InvalidPageID,
		catalogRoot:  1,
	}

	if size == 0 {
		// Initialize brand-new database file
		if err := p.initNewDatabase(); err != nil {
			return nil, err
		}
	} else {
		// Read existing header from Page 0
		if err := p.readDatabaseHeader(); err != nil {
			return nil, err
		}
	}

	return p, nil
}

func (p *Pager) PageSize() int {
	return p.pageSize
}

func (p *Pager) offset(id common.PageID) int64 {
	return int64(id) * int64(p.pageSize)
}

func (p *Pager) initNewDatabase() error {
	p.numPages = 2 // Page 0: DB Header, Page 1: Schema Catalog Root
	p.freeListHead = common.InvalidPageID
	p.catalogRoot = 1

	// Page 0: DB Header
	headerPage := make([]byte, p.pageSize)
	copy(headerPage[HeaderMagicOffset:], MagicBytes)
	binary.BigEndian.PutUint32(headerPage[HeaderPageSizeOffset:], uint32(p.pageSize))
	binary.BigEndian.PutUint32(headerPage[HeaderVersionOffset:], 1)
	binary.BigEndian.PutUint64(headerPage[HeaderPageCountOffset:], uint64(p.numPages))
	binary.BigEndian.PutUint64(headerPage[HeaderFreeListOffset:], uint64(p.freeListHead))
	binary.BigEndian.PutUint64(headerPage[HeaderCatalogRootOffset:], uint64(p.catalogRoot))

	if err := p.file.WriteAt(0, headerPage); err != nil {
		return err
	}

	// Page 1: Empty catalog root page
	catalogPage := make([]byte, p.pageSize)
	if err := p.file.WriteAt(p.offset(1), catalogPage); err != nil {
		return err
	}

	return p.file.Sync()
}

func (p *Pager) readDatabaseHeader() error {
	buf := make([]byte, p.pageSize)
	if err := p.file.ReadAt(0, buf); err != nil {
		return fmt.Errorf("failed to read db header: %w", err)
	}

	for i := 0; i < len(MagicBytes); i++ {
		if buf[HeaderMagicOffset+i] != MagicBytes[i] {
			return ErrBadMagic
		}
	}

	p.pageSize = int(binary.BigEndian.Uint32(buf[HeaderPageSizeOffset:]))
	p.numPages = common.PageID(binary.BigEndian.Uint64(buf[HeaderPageCountOffset:]))
	p.freeListHead = common.PageID(binary.BigEndian.Uint64(buf[HeaderFreeListOffset:]))
	p.catalogRoot = common.PageID(binary.BigEndian.Uint64(buf[HeaderCatalogRootOffset:]))

	// Reconcile with actual file size
	fileSize, err := p.file.Size()
	if err == nil && p.pageSize > 0 {
		actualPages := common.PageID(fileSize / int64(p.pageSize))
		if actualPages > p.numPages {
			p.numPages = actualPages
		}
	}

	return nil
}

func (p *Pager) flushDatabaseHeader() error {
	buf := make([]byte, p.pageSize)
	if err := p.file.ReadAt(0, buf); err != nil && !errors.Is(err, io.EOF) {
		// If read fails, initialize buffer
		buf = make([]byte, p.pageSize)
	}
	copy(buf[HeaderMagicOffset:], MagicBytes)
	binary.BigEndian.PutUint32(buf[HeaderPageSizeOffset:], uint32(p.pageSize))
	binary.BigEndian.PutUint32(buf[HeaderVersionOffset:], 1)
	binary.BigEndian.PutUint64(buf[HeaderPageCountOffset:], uint64(p.numPages))
	binary.BigEndian.PutUint64(buf[HeaderFreeListOffset:], uint64(p.freeListHead))
	binary.BigEndian.PutUint64(buf[HeaderCatalogRootOffset:], uint64(p.catalogRoot))

	return p.file.WriteAt(0, buf)
}

// ReadPage loads the page data for id from disk.
func (p *Pager) ReadPage(id common.PageID) (*Page, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	buf := make([]byte, p.pageSize)
	if err := p.file.ReadAt(p.offset(id), buf); err != nil {
		return nil, fmt.Errorf("pager read error on page %d: %w", id, err)
	}
	return &Page{
		ID:   id,
		Data: buf,
	}, nil
}

// WritePage writes page contents directly to disk at its designated offset.
func (p *Pager) WritePage(page *Page) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if int64(page.ID) >= int64(p.numPages) {
		p.numPages = page.ID + 1
		_ = p.flushDatabaseHeader()
	}

	return p.file.WriteAt(p.offset(page.ID), page.Data)
}

// AllocatePage assigns a new or recycled PageID and creates an empty page buffer.
func (p *Pager) AllocatePage() (*Page, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	var newID common.PageID
	if p.freeListHead != common.InvalidPageID {
		// Pop from free list
		newID = p.freeListHead
		buf := make([]byte, p.pageSize)
		if err := p.file.ReadAt(p.offset(newID), buf); err == nil {
			nextFree := common.PageID(binary.BigEndian.Uint64(buf[:8]))
			p.freeListHead = nextFree
		} else {
			p.freeListHead = common.InvalidPageID
		}
	} else {
		newID = p.numPages
		p.numPages++
	}

	if err := p.flushDatabaseHeader(); err != nil {
		return nil, err
	}

	data := make([]byte, p.pageSize)
	page := &Page{
		ID:   newID,
		Data: data,
	}

	// Persist empty page
	if err := p.file.WriteAt(p.offset(newID), data); err != nil {
		return nil, err
	}

	return page, nil
}

// FreePage marks a page as freed, pushing it onto the free list.
func (p *Pager) FreePage(id common.PageID) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if id <= 1 || id >= p.numPages {
		return ErrInvalidPageID
	}

	buf := make([]byte, p.pageSize)
	binary.BigEndian.PutUint64(buf[:8], uint64(p.freeListHead))
	if err := p.file.WriteAt(p.offset(id), buf); err != nil {
		return err
	}

	p.freeListHead = id
	return p.flushDatabaseHeader()
}

func (p *Pager) NumPages() common.PageID {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.numPages
}

func (p *Pager) CatalogRoot() common.PageID {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.catalogRoot
}

func (p *Pager) SetCatalogRoot(id common.PageID) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.catalogRoot = id
	return p.flushDatabaseHeader()
}

// Sync flushes all pending file changes to stable storage.
func (p *Pager) Sync() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.file.Sync()
}

// Close flushes the database header and closes the file.
func (p *Pager) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	_ = p.flushDatabaseHeader()
	_ = p.file.Sync()
	return p.file.Close()
}
