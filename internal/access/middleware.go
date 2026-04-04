package access

import (
	"sync"
)

type Middleware struct {
	allowed  map[int]struct{}
	mu       sync.Mutex
	active   map[int]int
	maxInFly int
}

func NewMiddleware(allowed []int, maxInFly int) *Middleware {
	m := &Middleware{
		allowed:  make(map[int]struct{}, len(allowed)),
		active:   make(map[int]int),
		maxInFly: maxInFly,
	}
	for _, id := range allowed {
		m.allowed[id] = struct{}{}
	}
	if m.maxInFly <= 0 {
		m.maxInFly = 1
	}
	return m
}

func (m *Middleware) IsAllowed(userID int) bool {
	if len(m.allowed) == 0 {
		return true
	}
	_, ok := m.allowed[userID]
	return ok
}

func (m *Middleware) Acquire(userID int) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.active[userID] >= m.maxInFly {
		return false
	}
	m.active[userID]++
	return true
}

func (m *Middleware) Release(userID int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.active[userID] > 0 {
		m.active[userID]--
	}
	if m.active[userID] == 0 {
		delete(m.active, userID)
	}
}
