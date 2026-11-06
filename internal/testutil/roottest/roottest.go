// Package roottest reports chosen library roots offline for one spec.
package roottest

import (
	"context"

	. "github.com/onsi/ginkgo/v2"

	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/library/roothealth"
)

// Roots answers roothealth with one unreachable file per offline root.
type Roots struct {
	offline map[db.LibraryRoot]bool
}

// Offline registers roots as offline until the spec ends.
func Offline(roots ...db.LibraryRoot) *Roots {
	GinkgoHelper()
	r := &Roots{offline: make(map[db.LibraryRoot]bool, len(roots))}
	for _, root := range roots {
		r.offline[root] = true
	}
	roothealth.Register(r)
	DeferCleanup(func() { roothealth.Register(nil) })
	return r
}

// Back brings a root online.
func (r *Roots) Back(root db.LibraryRoot) {
	r.offline[root] = false
}

func (r *Roots) CountRootFiles(context.Context, db.LibraryRoot) (int, error) {
	return 1, nil
}

func (r *Roots) RootFilePathAt(
	_ context.Context,
	root db.LibraryRoot,
	_ int,
) (string, error) {
	if r.offline[root] {
		return "/nonexistent/streamline/file", nil
	}
	return "", nil
}
