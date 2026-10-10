package metadata

import (
	"sync"
	"time"
)

type memoEntry[V any] struct {
	v   V
	exp time.Time
}

// memo is a small TTL map. The zero value is ready to use; the lifetime is
// chosen per write, so one type serves answers with different freshness.
type memo[V any] struct {
	mu    sync.Mutex
	items map[string]memoEntry[V]
}

func (m *memo[V]) get(key string) (V, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.items[key]
	if !ok || time.Now().After(e.exp) {
		delete(m.items, key)
		var zero V
		return zero, false
	}
	return e.v, true
}

func (m *memo[V]) put(key string, v V, ttl time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.items == nil {
		m.items = make(map[string]memoEntry[V])
	}
	if len(m.items) >= hcMemoCap {
		m.evict()
	}
	m.items[key] = memoEntry[V]{v: v, exp: time.Now().Add(ttl)}
}

// evict drops what has expired and, if that is not enough, the entry closest
// to expiring.
func (m *memo[V]) evict() {
	now := time.Now()
	var oldestKey string
	var oldest time.Time
	for k, e := range m.items {
		if now.After(e.exp) {
			delete(m.items, k)
			continue
		}
		if oldestKey == "" || e.exp.Before(oldest) {
			oldestKey, oldest = k, e.exp
		}
	}
	if len(m.items) >= hcMemoCap && oldestKey != "" {
		delete(m.items, oldestKey)
	}
}
