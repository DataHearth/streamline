package music

import (
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

// memo caches loads by key for a TTL, collapsing concurrent loads of one key
// into a single call and evicting the oldest entry past max. Failures are not
// cached.
type memo[V any] struct {
	ttl time.Duration
	max int

	mu    sync.Mutex
	items map[string]memoEntry[V]
	flt   singleflight.Group
}

type memoEntry[V any] struct {
	value V
	at    time.Time
}

func newMemo[V any](ttl time.Duration, max int) *memo[V] {
	return &memo[V]{ttl: ttl, max: max, items: make(map[string]memoEntry[V])}
}

func (m *memo[V]) get(key string) (V, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.items[key]
	if !ok || time.Since(e.at) > m.ttl {
		delete(m.items, key)
		var zero V
		return zero, false
	}
	return e.value, true
}

func (m *memo[V]) put(key string, v V) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.items[key] = memoEntry[V]{value: v, at: time.Now()}
	for len(m.items) > m.max {
		oldest, oldestAt := "", time.Now()
		for k, e := range m.items {
			if e.at.Before(oldestAt) {
				oldest, oldestAt = k, e.at
			}
		}
		delete(m.items, oldest)
	}
}

// do returns the cached value for key or loads, caches and returns it.
func (m *memo[V]) do(key string, load func() (V, error)) (V, error) {
	if v, ok := m.get(key); ok {
		return v, nil
	}
	v, err, _ := m.flt.Do(key, func() (any, error) {
		if v, ok := m.get(key); ok {
			return v, nil
		}
		v, err := load()
		if err != nil {
			return nil, err
		}
		m.put(key, v)
		return v, nil
	})
	if err != nil {
		var zero V
		return zero, err
	}
	return v.(V), nil
}
