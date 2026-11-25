// Package imagecachetest wires the image cache's mocks for specs.
package imagecachetest

import (
	"context"

	"github.com/stretchr/testify/mock"

	"github.com/datahearth/streamline/internal/imagecache/mocks"
)

// NewMocks returns a Manager mock and the Store mock its Posters() answers.
// A title's add, refresh and re-identify all call Refresh, so that call is
// expected, never required: a spec that checks it replaces the stand-in with
// ExpectRefresh.
func NewMocks(t interface {
	mock.TestingT
	Cleanup(func())
},
) (*mocks.MockManager, *mocks.MockStore) {
	mgr := mocks.NewMockManager(t)
	store := mocks.NewMockStore(t)
	mgr.EXPECT().Posters().Return(store).Maybe()
	mgr.EXPECT().
		Refresh(mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(func(context.Context) {}).Maybe()
	return mgr, store
}

// ExpectRefresh requires one Refresh of the row with these addresses, in place
// of NewMocks' stand-in, and returns a channel closed once the func Refresh
// handed out has run. id is a uint32 or a matcher.
func ExpectRefresh(
	mgr *mocks.MockManager,
	kind string,
	id any,
	poster string,
	logos ...string,
) <-chan struct{} {
	ran := make(chan struct{})
	mgr.EXPECT().
		Refresh(mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Unset()
	if logos == nil {
		logos = []string{}
	}
	mgr.EXPECT().Refresh(kind, id, poster, logos).
		Return(func(context.Context) { close(ran) }).Once()
	return ran
}
