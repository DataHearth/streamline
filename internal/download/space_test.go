package download

import (
	"context"
	"errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/testutil/configtest"
)

type spaceClient struct {
	stubClient

	free     int64
	freeErr  error
	torrents []Torrent
}

func (c *spaceClient) FreeSpace(context.Context) (int64, error) {
	return c.free, c.freeErr
}

func (c *spaceClient) ListTorrents(context.Context) ([]Torrent, error) {
	return c.torrents, nil
}

var _ = Describe("checkFreeSpace", Label("unit", "downloads"), func() {
	var client *spaceClient

	BeforeEach(func() {
		configtest.Setup(map[string]any{
			"download": map[string]any{"space_check": true, "space_reserve_gb": 10},
		})
		// 40 GB free; one torrent half way through 20 GB leaves 10 GB to write,
		// and the reserve takes 10 more, so 20 GB is what a grab may claim.
		client = &spaceClient{
			free: 40 * gigabyte,
			torrents: []Torrent{
				{Size: 20 * gigabyte, Progress: 0.5},
				{Size: 50 * gigabyte, Progress: 1},
			},
		}
	})

	It(
		"lets through a release that fits beside the active downloads and the reserve",
		func() {
			Expect(
				checkFreeSpace(context.Background(), client, 20*gigabyte),
			).To(Succeed())
		},
	)

	It("refuses a release that does not, saying by how much", func() {
		err := checkFreeSpace(context.Background(), client, 20*gigabyte+1)
		Expect(err).To(MatchError(ErrNoSpace))
		Expect(err.Error()).To(ContainSubstring(
			"40.0 GB free with 10.0 GB still downloading and 10.0 GB kept in reserve",
		))
	})

	It("does not check when it is turned off", func() {
		configtest.Setup(map[string]any{
			"download": map[string]any{"space_check": false},
		})
		Expect(
			checkFreeSpace(context.Background(), client, 100*gigabyte),
		).To(Succeed())
	})

	It("does not check a release of unknown size", func() {
		client.free = 0
		Expect(checkFreeSpace(context.Background(), client, 0)).To(Succeed())
	})

	It("lets the grab through when the client cannot report its free space", func() {
		client.freeErr = errors.New("connection refused")
		Expect(
			checkFreeSpace(context.Background(), client, 100*gigabyte),
		).To(Succeed())
	})

	It("does not check a client without FreeSpacer", func() {
		Expect(
			checkFreeSpace(context.Background(), stubClient{}, 100*gigabyte),
		).To(Succeed())
	})
})
