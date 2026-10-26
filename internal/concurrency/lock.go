package concurrency

import (
	"sync"
)

// LockMode specifies whether a lock is shared (read) or exclusive (write).
type LockMode int

const (
	LockShared LockMode = iota
	LockExclusive
)

// LockManager manages logical read/write locks on database tables.
type LockManager struct {
	mu    sync.Mutex
	locks map[string]*sync.RWMutex
}

func NewLockManager() *LockManager {
	return &LockManager{
		locks: make(map[string]*sync.RWMutex),
	}
}

func (lm *LockManager) getLock(resource string) *sync.RWMutex {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	l, exists := lm.locks[resource]
	if !exists {
		l = &sync.RWMutex{}
		lm.locks[resource] = l
	}
	return l
}

func (lm *LockManager) LockShared(resource string) {
	lm.getLock(resource).RLock()
}

func (lm *LockManager) UnlockShared(resource string) {
	lm.getLock(resource).RUnlock()
}

func (lm *LockManager) LockExclusive(resource string) {
	lm.getLock(resource).Lock()
}

func (lm *LockManager) UnlockExclusive(resource string) {
	lm.getLock(resource).Unlock()
}
