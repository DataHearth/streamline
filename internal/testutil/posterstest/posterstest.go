// Package posterstest wires the image cache's mocks for specs.
package posterstest

import (
	"github.com/stretchr/testify/mock"

	"github.com/datahearth/streamline/internal/posters/mocks"
)

// NewMocks returns a Manager mock and the Store mock its Posters() answers.
func NewMocks(t interface {
	mock.TestingT
	Cleanup(func())
},
) (*mocks.MockManager, *mocks.MockStore) {
	mgr := mocks.NewMockManager(t)
	store := mocks.NewMockStore(t)
	mgr.EXPECT().Posters().Return(store).Maybe()
	return mgr, store
}
