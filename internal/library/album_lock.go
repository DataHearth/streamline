package library

import "sync"

// albumLocks serializes the changes to one music album's files across the
// process: the tag writer's copy-and-rename, the album importer's transfers
// and replacements, the renamer's moves and the deletes. Without it a tag
// write that copied a file before an upgrade, a rename or a delete renamed
// its copy over the path afterwards, putting the old bytes back where the
// upgrade had landed, or a deleted file back on disk with no row.
var albumLocks = struct {
	mu    sync.Mutex
	locks map[uint32]*albumLock
}{locks: map[uint32]*albumLock{}}

type albumLock struct {
	sync.Mutex
	users int
}

// LockAlbumFiles takes the album's file lock and returns its release. It is
// not reentrant: a holder never calls anything that takes it again (the
// importer releases it before it asks the tag writer to tag the album). A
// caller holding several takes them in ascending id order.
func LockAlbumFiles(albumID uint32) (unlock func()) {
	albumLocks.mu.Lock()
	l, ok := albumLocks.locks[albumID]
	if !ok {
		l = &albumLock{}
		albumLocks.locks[albumID] = l
	}
	l.users++
	albumLocks.mu.Unlock()

	l.Lock()
	var once sync.Once
	return func() {
		once.Do(func() {
			l.Unlock()
			albumLocks.mu.Lock()
			if l.users--; l.users == 0 {
				delete(albumLocks.locks, albumID)
			}
			albumLocks.mu.Unlock()
		})
	}
}
