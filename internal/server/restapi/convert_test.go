package restapi

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/movie"
	"github.com/datahearth/streamline/ent/user"
	"github.com/datahearth/streamline/internal/config"
)

var _ = Describe("convert", Label("unit", "server"), func() {
	Describe("movieToAPI", func() {
		It("omits Overview when ent field is empty", func() {
			m := &ent.Movie{
				ID:     1,
				Title:  "Interstellar",
				Year:   2014,
				Status: movie.StatusWanted,
				TmdbID: 157336,
			}
			api := movieToAPI(m)
			Expect(api.Id).To(Equal(uint32(1)))
			Expect(api.Title).To(Equal("Interstellar"))
			Expect(api.Overview).To(BeNil())
		})

		It("sets Overview when ent field is populated", func() {
			m := &ent.Movie{
				ID:       2,
				Title:    "Inception",
				Overview: "dream heist",
				Status:   movie.StatusDownloading,
				TmdbID:   27205,
			}
			api := movieToAPI(m)
			Expect(api.Overview).NotTo(BeNil())
			Expect(*api.Overview).To(Equal("dream heist"))
		})
	})

	Describe("toAPIUser", func() {
		It("omits DisplayName when ent field is empty", func() {
			u := &ent.User{
				ID:         1,
				Email:      "a@x.com",
				Role:       user.RoleMember,
				AuthMethod: user.AuthMethodLocal,
				CreateTime: time.Now(),
			}
			api := toAPIUser(u)
			Expect(api.Id).To(Equal(uint32(1)))
			Expect(string(api.Email)).To(Equal("a@x.com"))
			Expect(api.DisplayName).To(BeNil())
			Expect(string(api.Role)).To(Equal("member"))
			Expect(string(api.AuthMethod)).To(Equal("local"))
		})

		It("sets DisplayName when ent field is populated", func() {
			u := &ent.User{
				ID:          2,
				Email:       "b@x.com",
				DisplayName: "Beatrice",
				Role:        user.RoleAdmin,
				AuthMethod:  user.AuthMethodBoth,
				CreateTime:  time.Now(),
			}
			api := toAPIUser(u)
			Expect(api.DisplayName).NotTo(BeNil())
			Expect(*api.DisplayName).To(Equal("Beatrice"))
			Expect(string(api.Role)).To(Equal("admin"))
			Expect(string(api.AuthMethod)).To(Equal("both"))
		})
	})

	Describe("toAPIInvite", func() {
		It("leaves Email nil when the invite is not bound to an email", func() {
			now := time.Now()
			inv := &ent.Invite{
				ID:         3,
				Role:       "member",
				ExpiresAt:  now.Add(time.Hour),
				CreateTime: now,
			}
			api := toAPIInvite(inv)
			Expect(api.Id).To(Equal(uint32(3)))
			Expect(api.Email).To(BeNil())
			Expect(api.UsedAt).To(BeNil())
		})

		It("populates Email + UsedAt when the invite has them", func() {
			now := time.Now()
			used := now.Add(-time.Minute)
			inv := &ent.Invite{
				ID:         4,
				Email:      "c@x.com",
				Role:       "admin",
				ExpiresAt:  now.Add(time.Hour),
				UsedAt:     &used,
				CreateTime: now,
			}
			api := toAPIInvite(inv)
			Expect(api.Email).NotTo(BeNil())
			Expect(string(*api.Email)).To(Equal("c@x.com"))
			Expect(api.UsedAt).NotTo(BeNil())
		})
	})

	Describe("downloadClientToAPI", func() {
		It("omits Username and reports secrets unset when entry is bare", func() {
			e := config.DownloadClientEntry{
				Name:       "qbit",
				ClientType: "qbittorrent",
				Host:       "127.0.0.1",
				Port:       8080,
				AuthMethod: "password",
				Enabled:    true,
			}
			api := downloadClientToAPI(e)
			Expect(api.Username).To(BeNil())
			Expect(api.Enabled).To(BeTrue())
			Expect(api.ApiKeySet).To(BeFalse())
			Expect(api.PasswordSet).To(BeFalse())
		})

		It("populates optional fields and secret-set flags when present", func() {
			e := config.DownloadClientEntry{
				Name:       "tx",
				ClientType: "qbittorrent",
				Host:       "tx.local",
				Port:       9091,
				AuthMethod: "password",
				Enabled:    false,
				Username:   "admin",
				Password:   "secret",
				UseSSL:     true,
				Priority:   5,
			}
			api := downloadClientToAPI(e)
			Expect(api.Username).NotTo(BeNil())
			Expect(*api.Username).To(Equal("admin"))
			Expect(api.UseSsl).NotTo(BeNil())
			Expect(*api.UseSsl).To(BeTrue())
			Expect(api.Priority).NotTo(BeNil())
			Expect(*api.Priority).To(Equal(uint8(5)))
			Expect(api.PasswordSet).To(BeTrue())
		})
	})

	Describe("indexerToAPI", func() {
		It("reports ApiKeySet false when no key is set", func() {
			e := config.IndexerEntry{
				Name:     "torznab",
				Host:     "idx.example",
				Port:     443,
				Path:     "/api",
				UseSSL:   true,
				Protocol: "torznab",
				Enabled:  true,
			}
			api := indexerToAPI(e)
			Expect(api.ApiKeySet).To(BeFalse())
			Expect(api.Path).NotTo(BeNil())
			Expect(*api.Path).To(Equal("/api"))
		})

		It("populates Priority + ApiKeySet when entry carries them", func() {
			e := config.IndexerEntry{
				Name:     "torznab",
				Host:     "idx.example",
				Port:     443,
				Path:     "/api",
				UseSSL:   true,
				Protocol: "torznab",
				Enabled:  true,
				Priority: 10,
				APIKey:   "secret",
			}
			api := indexerToAPI(e)
			Expect(api.Priority).NotTo(BeNil())
			Expect(*api.Priority).To(Equal(uint8(10)))
			Expect(api.ApiKeySet).To(BeTrue())
		})
	})
})
