package buffer

import (
	"fmt"
	"sync"
	"sync/atomic"

	"atlasDB/internal/common"
	"atlasDB/internal/pager"
)

// Frame holds an in-memory cached database page.
type Frame struct {
	PageID common.PageID
	Data   []byte
	Dirty  bool
	Pin    uint32

	// Doubly-linked list pointers for LRU management
	prev *Frame
	next *Frame
}

// Stats tracks buffer pool operational metrics.
type Stats struct {
	Hits      uint64
	Misses    uint64
	Evictions uint64
}

// BufferPool manages a bounded collection of page frames in memory with LRU replacement.
type BufferPool struct {
	mu        sync.RWMutex
	pager     *pager.Pager
	capacity  int
	frames    map[common.PageID]*Frame
	lruHead   *Frame // Most recently used
	lruTail   *Frame // Least recently used
	hits      uint64
	misses    uint64
	evictions uint64

	// Optional WAL flush callback before dirty page eviction (durability rule)
	onBeforeFlush func(pageID common.PageID) error
}

// NewBufferPool creates a new buffer manager with the specified capacity.
func NewBufferPool(p *pager.Pager, capacity int) *BufferPool {
	if capacity <= 0 {
		capacity = 256
	}
	return &BufferPool{
		pager:    p,
		capacity: capacity,
		frames:   make(map[common.PageID]*Frame, capacity),
	}
}

// SetWALFlushHook registers a hook invoked before a dirty page is flushed to disk.
func (bp *BufferPool) SetWALFlushHook(hook func(pageID common.PageID) error) {
	bp.mu.Lock()
	defer bp.mu.Unlock()
	bp.onBeforeFlush = hook
}

// FetchPage loads a page into memory or retrieves it from cache, pinning it.
func (bp *BufferPool) FetchPage(id common.PageID) (*Frame, error) {
	bp.mu.Lock()
	defer bp.mu.Unlock()

	if frame, exists := bp.frames[id]; exists {
		atomic.AddUint64(&bp.hits, 1)
		frame.Pin++
		bp.moveToFront(frame)
		return frame, nil
	}

	atomic.AddUint64(&bp.misses, 1)

	// Need to read page from pager
	if len(bp.frames) >= bp.capacity {
		if err := bp.evictOne(); err != nil {
			return nil, err
		}
	}

	page, err := bp.pager.ReadPage(id)
	if err != nil {
		return nil, err
	}

	frame := &Frame{
		PageID: id,
		Data:   page.Data,
		Dirty:  false,
		Pin:    1,
	}

	bp.frames[id] = frame
	bp.addToFront(frame)
	return frame, nil
}

// NewPage allocates a new page on disk and places it in the buffer pool, pinned.
func (bp *BufferPool) NewPage() (*Frame, error) {
	bp.mu.Lock()
	defer bp.mu.Unlock()

	if len(bp.frames) >= bp.capacity {
		if err := bp.evictOne(); err != nil {
			return nil, err
		}
	}

	page, err := bp.pager.AllocatePage()
	if err != nil {
		return nil, err
	}

	frame := &Frame{
		PageID: page.ID,
		Data:   page.Data,
		Dirty:  true,
		Pin:    1,
	}

	bp.frames[page.ID] = frame
	bp.addToFront(frame)
	return frame, nil
}

// Unpin decrements the pin count of a frame and marks it dirty if modified.
func (bp *BufferPool) Unpin(id common.PageID, isDirty bool) error {
	bp.mu.Lock()
	defer bp.mu.Unlock()

	frame, exists := bp.frames[id]
	if !exists {
		return fmt.Errorf("atlas buffer: unpin of non-existent frame %d", id)
	}

	if frame.Pin > 0 {
		frame.Pin--
	}
	if isDirty {
		frame.Dirty = true
	}
	return nil
}

// FlushPage writes the page data to disk if dirty and clears the dirty flag.
func (bp *BufferPool) FlushPage(id common.PageID) error {
	bp.mu.Lock()
	defer bp.mu.Unlock()

	frame, exists := bp.frames[id]
	if !exists {
		return nil
	}

	return bp.flushFrame(frame)
}

func (bp *BufferPool) flushFrame(frame *Frame) error {
	if !frame.Dirty {
		return nil
	}

	if bp.onBeforeFlush != nil {
		if err := bp.onBeforeFlush(frame.PageID); err != nil {
			return err
		}
	}

	page := &pager.Page{
		ID:   frame.PageID,
		Data: frame.Data,
	}
	if err := bp.pager.WritePage(page); err != nil {
		return err
	}

	frame.Dirty = false
	return nil
}

// FlushAll flushes all dirty pages to disk and synchronizes the pager.
func (bp *BufferPool) FlushAll() error {
	bp.mu.Lock()
	defer bp.mu.Unlock()

	for _, frame := range bp.frames {
		if frame.Dirty {
			if err := bp.flushFrame(frame); err != nil {
				return err
			}
		}
	}
	return bp.pager.Sync()
}

func (bp *BufferPool) evictOne() error {
	// Traverse from tail (LRU) toward head to find first unpinned frame
	curr := bp.lruTail
	for curr != nil {
		if curr.Pin == 0 {
			// Found candidate for eviction
			if curr.Dirty {
				if err := bp.flushFrame(curr); err != nil {
					return fmt.Errorf("failed to flush dirty page during eviction: %w", err)
				}
			}
			bp.removeNode(curr)
			delete(bp.frames, curr.PageID)
			atomic.AddUint64(&bp.evictions, 1)
			return nil
		}
		curr = curr.prev
	}
	return common.ErrBufferPoolFull
}

func (bp *BufferPool) addToFront(frame *Frame) {
	frame.prev = nil
	frame.next = bp.lruHead
	if bp.lruHead != nil {
		bp.lruHead.prev = frame
	}
	bp.lruHead = frame
	if bp.lruTail == nil {
		bp.lruTail = frame
	}
}

func (bp *BufferPool) removeNode(frame *Frame) {
	if frame.prev != nil {
		frame.prev.next = frame.next
	} else {
		bp.lruHead = frame.next
	}
	if frame.next != nil {
		frame.next.prev = frame.prev
	} else {
		bp.lruTail = frame.prev
	}
	frame.prev = nil
	frame.next = nil
}

func (bp *BufferPool) moveToFront(frame *Frame) {
	if bp.lruHead == frame {
		return
	}
	bp.removeNode(frame)
	bp.addToFront(frame)
}

// Stats returns current buffer pool metrics.
func (bp *BufferPool) Stats() (hits, misses, evictions uint64, dirtyCount, pinCount int) {
	bp.mu.RLock()
	defer bp.mu.RUnlock()

	dirty := 0
	pinned := 0
	for _, frame := range bp.frames {
		if frame.Dirty {
			dirty++
		}
		if frame.Pin > 0 {
			pinned++
		}
	}

	return atomic.LoadUint64(&bp.hits),
		atomic.LoadUint64(&bp.misses),
		atomic.LoadUint64(&bp.evictions),
		dirty,
		pinned
}

// ResetStats zeroes out hit/miss/eviction counters.
func (bp *BufferPool) ResetStats() {
	atomic.StoreUint64(&bp.hits, 0)
	atomic.StoreUint64(&bp.misses, 0)
	atomic.StoreUint64(&bp.evictions, 0)
}

// Close flushes all dirty pages to disk and synchronizes the pager.
func (bp *BufferPool) Close() error {
	return bp.FlushAll()
}
