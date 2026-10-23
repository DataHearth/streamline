package containers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

const (
	// Prowlarr's own image rather than linuxserver's: it takes the same env
	// and skips the s6 init layer, so first boot is the app writing its
	// config and nothing else.
	prowlarrImage = "ghcr.io/hotio/prowlarr:release-1.37.0.5076"

	prowlarrHTTP     = "9696"
	prowlarrHTTPPort = prowlarrHTTP + "/tcp"

	// The key is supplied rather than read back: Servarr generates one into
	// /config/config.xml on first boot, and pinning it through the
	// environment saves a copy-out and a parse for a value we choose anyway.
	prowlarrAPIKey = "e2e0e2e0e2e0e2e0e2e0e2e0e2e0e2e0"

	// Split for the same reason as qBittorrent's: a cold `docker pull` runs
	// before the readiness probe starts, and Prowlarr's first boot writes a
	// fresh config and migrates an empty database before it answers.
	prowlarrReadyTimeout = 120 * time.Second
	prowlarrStartTimeout = 5 * time.Minute
)

// Prowlarr locates the shared container's API.
type Prowlarr struct {
	Host   string
	Port   uint16
	APIKey string

	// HostPort is the host-side port the container was told to reach back
	// through. testcontainers publishes it at host.testcontainers.internal,
	// which is the only address a fake indexer running in the spec process is
	// reachable at from inside the container.
	HostPort int
}

// BaseURL is what an indexer entry's host/port pair renders to, and what the
// streamline Prowlarr client is pointed at.
func (p *Prowlarr) BaseURL() string {
	return fmt.Sprintf("http://%s:%d", p.Host, p.Port)
}

// TorznabIndexerCaps is the search-capability surface a registered indexer
// declares. It is the lever every spec here turns on: Prowlarr drops an
// indexer from a search entirely when handed a parameter its caps omit, so
// what an entry claims to support decides whether it is queried at all.
type TorznabIndexerCaps struct {
	// Name is the indexer's name in Prowlarr.
	Name string
	// FeedURL is where Prowlarr fetches, normally a
	// http://host.testcontainers.internal:<HostPort> address.
	FeedURL string
	// APIKey is the credential Prowlarr sends to the feed.
	APIKey string
	// Categories are the newznab roots the entry maps, e.g. 5000 for TV.
	Categories []int
}

var (
	prowlarrOnce sync.Once
	prowlarr     *Prowlarr
	prowlarrErr  error

	// A timeout rather than http.DefaultClient: a container that accepts
	// connections but stops answering must fail the spec, not hang the suite.
	prowlarrHTTPClient = &http.Client{Timeout: 30 * time.Second}
)

// StartProwlarr starts (once per process) a Prowlarr container with
// authentication disabled and a pinned API key. hostPort is a port on the
// spec process's own machine that the container must be able to reach — the
// fake Torznab indexer the specs register — and is fixed for the life of the
// container, so every caller must name the same one.
func StartProwlarr(hostPort int) *Prowlarr {
	GinkgoHelper()
	prowlarrOnce.Do(func() { prowlarr, prowlarrErr = startProwlarr(hostPort) })
	Expect(prowlarrErr).NotTo(HaveOccurred())
	// One container serves the whole process, and the host-port forward is
	// established at start — a later caller naming a different port would
	// otherwise get a container that cannot reach its fake.
	Expect(hostPort).To(
		Equal(prowlarr.HostPort),
		"Prowlarr is already running forwarding a different host port",
	)
	return prowlarr
}

// AddTorznabIndexer registers a Generic Torznab indexer and returns its id.
// Prowlarr probes the feed's t=caps on save and rejects an entry it cannot
// reach, so a failure here usually means the fake is not answering on
// host.testcontainers.internal rather than that the payload is wrong.
func (p *Prowlarr) AddTorznabIndexer(caps TorznabIndexerCaps) int {
	GinkgoHelper()

	body := map[string]any{
		"enable":         true,
		"name":           caps.Name,
		"implementation": "Torznab",
		"configContract": "TorznabSettings",
		"protocol":       "torrent",
		"appProfileId":   1,
		"priority":       25,
		"fields": []map[string]any{
			{"name": "baseUrl", "value": caps.FeedURL},
			{"name": "apiPath", "value": "/api"},
			{"name": "apiKey", "value": caps.APIKey},
			{"name": "categories", "value": caps.Categories},
		},
	}

	var created struct {
		ID int `json:"id"`
	}
	p.do(http.MethodPost, "/api/v1/indexer", body, &created)
	Expect(created.ID).NotTo(BeZero(), "Prowlarr returned no indexer id")
	return created.ID
}

// do issues an authenticated API call and decodes a 2xx body into out, which
// may be nil.
func (p *Prowlarr) do(method, path string, in, out any) {
	GinkgoHelper()

	var payload []byte
	if in != nil {
		var err error
		payload, err = json.Marshal(in)
		Expect(err).NotTo(HaveOccurred())
	}

	req, err := http.NewRequestWithContext(
		context.Background(),
		method,
		p.BaseURL()+path,
		bytes.NewReader(payload),
	)
	Expect(err).NotTo(HaveOccurred())
	req.Header.Set("X-Api-Key", p.APIKey)
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := prowlarrHTTPClient.Do(req)
	Expect(err).NotTo(HaveOccurred())
	defer resp.Body.Close()

	// Read up front: the body is either small JSON to decode or the only
	// description of why Prowlarr refused.
	raw := drain(resp)
	Expect(resp.StatusCode).To(
		BeNumerically("<", 300),
		"%s %s failed (%d): %s", method, path, resp.StatusCode, raw,
	)
	if out != nil {
		Expect(json.Unmarshal([]byte(raw), out)).To(Succeed())
	}
}

func startProwlarr(hostPort int) (*Prowlarr, error) {
	ctx, cancel := context.WithTimeout(context.Background(), prowlarrStartTimeout)
	defer cancel()

	pw, err := testcontainers.Run(ctx, prowlarrImage,
		testcontainers.WithEnv(map[string]string{
			"TZ": "UTC",
			// Servarr maps a doubled underscore onto a config section, so
			// these name auth.method / auth.apikey. External+None is what
			// leaves the API open to the spec without a login round-trip.
			"PROWLARR__AUTH__METHOD":       "External",
			"PROWLARR__AUTH__REQUIRED":     "DisabledForLocalAddresses",
			"PROWLARR__AUTH__APIKEY":       prowlarrAPIKey,
			"PROWLARR__SERVER__PORT":       prowlarrHTTP,
			"PROWLARR__ANALYTICS__ENABLED": "False",
		}),
		testcontainers.WithExposedPorts(prowlarrHTTPPort),
		// Publishes the spec process's fake indexer inside the container at
		// host.testcontainers.internal. Without it Prowlarr has no route back
		// and every registered indexer fails its caps probe.
		testcontainers.WithHostPortAccess(hostPort),
		testcontainers.WithWaitStrategy(
			// /ping is unauthenticated and answers only once the database has
			// migrated, which is the point the API is usable.
			wait.ForHTTP("/ping").
				WithPort(prowlarrHTTPPort).
				WithStartupTimeout(prowlarrReadyTimeout),
		),
	)
	if err != nil {
		// Run hands the container back even when startup failed, so its log
		// tail is available to say why — a readiness timeout is otherwise
		// indistinguishable from a config the image rejected.
		wrapped := fmt.Errorf("start %s: %w%s", prowlarrImage, err, logTail(ctx, pw))
		if pw == nil {
			return nil, wrapped
		}
		tctx, tcancel := context.WithTimeout(
			context.WithoutCancel(ctx),
			logTailGrace,
		)
		defer tcancel()
		return nil, errors.Join(wrapped, pw.Terminate(tctx))
	}

	host, err := pw.Host(ctx)
	if err != nil {
		return nil, err
	}
	mapped, err := pw.MappedPort(ctx, prowlarrHTTPPort)
	if err != nil {
		return nil, err
	}
	port, err := strconv.ParseUint(mapped.Port(), 10, 16)
	if err != nil {
		return nil, err
	}
	return &Prowlarr{
		Host:     host,
		Port:     uint16(port),
		APIKey:   prowlarrAPIKey,
		HostPort: hostPort,
	}, nil
}
