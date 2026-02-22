package conversation

import "sync"

type LockMap struct {
	mu    sync.Mutex
	locks map[string]*sync.Mutex
}

func NewLockMap() *LockMap {
	return &LockMap{locks: make(map[string]*sync.Mutex)}
}

func (m *LockMap) Lock(conversationID string) func() {
	m.mu.Lock()
	lock, ok := m.locks[conversationID]
	if !ok {
		lock = &sync.Mutex{}
		m.locks[conversationID] = lock
	}
	m.mu.Unlock()

	lock.Lock()
	return lock.Unlock
}
