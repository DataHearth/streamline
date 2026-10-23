package rss

import "github.com/datahearth/streamline/internal/library"

func splitCreatorTitle(name string) (string, string, bool) {
	return library.SplitCreatorTitle(name)
}
