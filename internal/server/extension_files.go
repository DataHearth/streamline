package server

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/mediafile"
)

// bookFiles resolves the file an extension's serve_file directive names, and
// only a book file that is still on disk.
type bookFiles struct{ client *ent.Client }

func (b bookFiles) BookFile(
	ctx context.Context,
	id uint32,
) (string, time.Time, error) {
	mf, err := b.client.MediaFile.Query().
		Where(
			mediafile.IDEQ(id),
			mediafile.BookKindNotNil(),
			mediafile.MissingSinceIsNil(),
		).
		Only(ctx)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("find book file %d: %w", id, err)
	}
	if _, err := os.Stat(mf.Path); err != nil {
		return "", time.Time{}, fmt.Errorf("stat book file %d: %w", id, err)
	}
	return mf.Path, mf.UpdateTime, nil
}
