package mediaserver

import (
	"context"
	"errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/testutil/configtest"
)

// stubServer is a hand-rolled Server impl used by DeepLinker tests; the
// generated MockServer lives in this package's mocks/ subdir and would
// create an import cycle when consumed from within the package itself.
type stubServer struct {
	url string
	err error
}

func (s stubServer) TestConnection(
	context.Context,
) error {
	return nil
}

func (s stubServer) RefreshLibrary(
	context.Context,
	string,
	string,
) error {
	return nil
}

func (s stubServer) MovieDeepLink(
	_ context.Context, _ string, _ uint32, _ string, _ uint16,
) (string, error) {
	return s.url, s.err
}

func (s stubServer) TVShowDeepLink(
	_ context.Context, _ string, _ uint32, _ string, _ uint16,
) (string, error) {
	return s.url, s.err
}

func mediaServerConfig(servers ...map[string]any) map[string]any {
	return map[string]any{"media_server": map[string]any{"servers": servers}}
}

var _ = Describe("DeepLinker", Label("unit", "mediaserver"), func() {
	var ctx context.Context

	BeforeEach(func() {
		ctx = context.Background()
	})

	It("returns empty when no enabled servers exist", func() {
		configtest.Setup()
		linker := NewDeepLinker(func(config.MediaServerEntry) (Server, error) {
			return stubServer{}, nil
		})

		out := linker.Resolve(ctx, 1, "Title", 2024)
		Expect(out).To(BeEmpty())
	})

	It("marks resolved when MovieDeepLink succeeds", func() {
		configtest.Setup(mediaServerConfig(map[string]any{
			"name": "Home Plex", "server_type": "plex",
			"host": "http://plex:32400", "api_key": "tok", "enabled": true,
		}))
		linker := NewDeepLinker(func(config.MediaServerEntry) (Server, error) {
			return stubServer{url: "http://plex:32400/web/details"}, nil
		})

		out := linker.Resolve(ctx, 100, "Title", 2024)
		Expect(out).To(HaveLen(1))
		Expect(out[0].Status).To(Equal(StatusResolved))
		Expect(out[0].URL).To(ContainSubstring("/web/"))
		Expect(out[0].Fallback).To(BeFalse())
	})

	It("falls back to home URL on ErrMovieNotFound", func() {
		configtest.Setup(mediaServerConfig(map[string]any{
			"name": "Jelly", "server_type": "jellyfin",
			"host": "http://jelly:8096", "api_key": "tok", "enabled": true,
		}))
		linker := NewDeepLinker(func(config.MediaServerEntry) (Server, error) {
			return stubServer{err: ErrMovieNotFound}, nil
		})

		out := linker.Resolve(ctx, 100, "Title", 2024)
		Expect(out[0].Status).To(Equal(StatusFallback))
		Expect(out[0].URL).To(Equal("http://jelly:8096/web/"))
		Expect(out[0].Fallback).To(BeTrue())
	})

	It("marks unavailable on transport errors", func() {
		configtest.Setup(mediaServerConfig(map[string]any{
			"name": "Down", "server_type": "plex",
			"host": "http://broken", "api_key": "tok", "enabled": true,
		}))
		linker := NewDeepLinker(func(config.MediaServerEntry) (Server, error) {
			return stubServer{err: errors.New("connection refused")}, nil
		})

		out := linker.Resolve(ctx, 100, "Title", 2024)
		Expect(out[0].Status).To(Equal(StatusUnavailable))
		Expect(out[0].URL).To(BeEmpty())
	})

	It("ResolveTV marks resolved when TVShowDeepLink succeeds", func() {
		configtest.Setup(mediaServerConfig(map[string]any{
			"name": "Home Plex", "server_type": "plex",
			"host": "http://plex:32400", "api_key": "tok", "enabled": true,
		}))
		linker := NewDeepLinker(func(config.MediaServerEntry) (Server, error) {
			return stubServer{url: "http://plex:32400/web/show"}, nil
		})

		out := linker.ResolveTV(ctx, 100, "Show", 2024)
		Expect(out).To(HaveLen(1))
		Expect(out[0].Status).To(Equal(StatusResolved))
		Expect(out[0].URL).To(ContainSubstring("/web/"))
	})

	It("ResolveTV falls back to home URL on ErrShowNotFound", func() {
		configtest.Setup(mediaServerConfig(map[string]any{
			"name": "Jelly", "server_type": "jellyfin",
			"host": "http://jelly:8096", "api_key": "tok", "enabled": true,
		}))
		linker := NewDeepLinker(func(config.MediaServerEntry) (Server, error) {
			return stubServer{err: ErrShowNotFound}, nil
		})

		out := linker.ResolveTV(ctx, 100, "Show", 2024)
		Expect(out[0].Status).To(Equal(StatusFallback))
		Expect(out[0].URL).To(Equal("http://jelly:8096/web/"))
		Expect(out[0].Fallback).To(BeTrue())
	})
})
