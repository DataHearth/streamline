package library

import "context"

// RenameOperation is one rename in a plan: move `From` to `To` on disk.
type RenameOperation struct {
	MediaFileID uint32
	From        string
	To          string
}

// RenamePlan is the set of file moves needed to bring a title's media files in
// line with the library naming pattern. Empty Operations means every file
// already matches its target.
type RenamePlan struct {
	Operations []RenameOperation
}

// Renamer computes and applies media-file rename plans for one title (movie or
// series), keyed by its numeric ID. Backs the manual "rename files" UI action.
type Renamer interface {
	Preview(ctx context.Context, id uint32) (RenamePlan, error)
	Apply(ctx context.Context, id uint32) (RenamePlan, error)
}
