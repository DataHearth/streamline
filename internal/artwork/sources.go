// Package artwork serves art for titles that are not in the library yet: the
// hits of the add flow and the reviewer's request panel. The server fetches and
// the browser sees only same-origin bytes, because the CSP allows no image host
// beyond TMDB and TVDB and the Cover Art Archive redirects through a rotating
// set of hosts.
//
// What to fetch is never supplied by the client. The lookup services remember
// the provider's own image reference as they serve a payload (Remember), and
// the proxy resolves a request from that memory.
package artwork

import (
	"container/list"
	"sync"
	"time"
)

const (
	sourceCapacity = 2000
	sourceTTL      = 24 * time.Hour
)

// Kind is the lookup art kind: the first path segment after /posters/lookup.
type Kind string

const (
	KindArtists Kind = "artists"
	KindAlbums  Kind = "albums"
	KindBooks   Kind = "books"
)

// Source is what a lookup service remembered about where a title's art lives.
// Artists carry a Deezer id (from MusicBrainz's url-rels) and/or the name to
// search by; books carry the image URL the Hardcover payload already held.
type Source struct {
	URL      string
	DeezerID uint32
	Name     string
}

type sourceKey struct {
	kind Kind
	key  string
}

type sourceEntry struct {
	k   sourceKey
	src Source
	at  time.Time
}

// Sources is a bounded in-memory map of remembered sources: oldest evicted
// past capacity, entries expiring after 24 h. It is never persisted, so after
// a restart a previously listed tile answers 404 until the lookup runs again.
type Sources struct {
	mu    sync.Mutex
	items map[sourceKey]*list.Element
	order *list.List
}

func NewSources() *Sources {
	return &Sources{
		items: make(map[sourceKey]*list.Element),
		order: list.New(),
	}
}

// Remember records where the art of (kind, key) lives; an empty Source is
// ignored.
func (s *Sources) Remember(kind Kind, key string, src Source) {
	if src == (Source{}) {
		return
	}
	k := sourceKey{kind, key}
	s.mu.Lock()
	defer s.mu.Unlock()
	if el, ok := s.items[k]; ok {
		el.Value = &sourceEntry{k: k, src: src, at: time.Now()}
		s.order.MoveToBack(el)
		return
	}
	s.items[k] = s.order.PushBack(&sourceEntry{k: k, src: src, at: time.Now()})
	for s.order.Len() > sourceCapacity {
		oldest := s.order.Front()
		s.order.Remove(oldest)
		delete(s.items, oldest.Value.(*sourceEntry).k)
	}
}

// Lookup returns the remembered source, false when none was remembered or it
// has expired.
func (s *Sources) Lookup(kind Kind, key string) (Source, bool) {
	k := sourceKey{kind, key}
	s.mu.Lock()
	defer s.mu.Unlock()
	el, ok := s.items[k]
	if !ok {
		return Source{}, false
	}
	e := el.Value.(*sourceEntry)
	if time.Since(e.at) > sourceTTL {
		s.order.Remove(el)
		delete(s.items, k)
		return Source{}, false
	}
	return e.src, true
}

var defaultSources = NewSources()

// Remember records where the art of (kind, key) lives. The lookup services
// call it as they serve a payload: the books service with the Hardcover image
// URL of every book hit and volume it returns, the music service with each
// artist hit's Deezer id and name. An empty Source is ignored.
func Remember(kind Kind, key string, src Source) {
	defaultSources.Remember(kind, key, src)
}
