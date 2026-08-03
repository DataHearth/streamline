package containers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/moby/moby/api/types/container"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

const (
	// 5.2.1, not the 5.0.x line: /api/v2/torrents/add only reports the
	// infohash of an uploaded .torrent (added_torrent_ids) from WebAPI
	// 2.15 on. 5.0.3 answers a bare "Ok.", which leaves QBittorrent.
	// AddTorrent with no hash to record and fails every file-based grab.
	qbImage = "lscr.io/linuxserver/qbittorrent:5.2.1"

	// The image takes the WebUI port as a bare number; every other use needs
	// the proto-qualified form, so both derive from one value.
	qbWebUI     = "8080"
	qbWebUIPort = qbWebUI + "/tcp"

	qbConfig = "/config/qBittorrent/qBittorrent.conf"

	// qbReadyTimeout budgets the readiness probe only. A cold `docker pull`
	// runs before that probe ever starts, so the whole run needs its own,
	// far more generous ceiling.
	qbReadyTimeout = 90 * time.Second
	qbStartTimeout = 4 * time.Minute
)

// QBittorrent locates the shared container's WebUI.
type QBittorrent struct {
	Host string
	Port uint16 // mapped WebUI port

	// SaveDir is the container's default save path, bind-mounted at this same
	// path on the host.
	SaveDir string
}

// Torrent is the slice of a qBittorrent /torrents/info entry the specs assert
// on and drive their completion simulation from.
type Torrent struct {
	Hash     string  `json:"hash"`
	Name     string  `json:"name"`
	State    string  `json:"state"`
	Progress float64 `json:"progress"`
	// ContentPath is where the payload lives on disk. SaveDir is bind-mounted
	// at the same path on the host, so it is directly readable — and
	// writable — from the spec process.
	ContentPath string `json:"content_path"`
}

var (
	qbOnce sync.Once
	qb     *QBittorrent
	qbErr  error

	// qbHTTP talks to the WebUI. A timeout rather than http.DefaultClient: a
	// container that accepts connections but stops answering must fail the
	// spec, not hang the suite.
	qbHTTP = &http.Client{Timeout: 10 * time.Second}
)

// URL renders an absolute WebUI URL for path.
func (q *QBittorrent) URL(path string) string {
	return fmt.Sprintf("http://%s:%d%s", q.Host, q.Port, path)
}

// Get issues a WebUI GET and returns the response, whose body the caller
// reads and closes. No credentials are sent: the container whitelists every
// subnet.
func (q *QBittorrent) Get(path string) *http.Response {
	GinkgoHelper()
	resp, err := qbHTTP.Get(q.URL(path))
	Expect(err).NotTo(HaveOccurred())
	// Lazily described: an eager drain(resp) argument would be evaluated on
	// the success path too, emptying the body before the caller reads it.
	Expect(resp.StatusCode).To(Equal(http.StatusOK), func() string {
		return fmt.Sprintf("GET %s failed: %s", path, drain(resp))
	})
	return resp
}

// Post issues a WebUI form POST and asserts the daemon accepted it.
func (q *QBittorrent) Post(path string, form url.Values) {
	GinkgoHelper()
	resp, err := qbHTTP.PostForm(q.URL(path), form)
	Expect(err).NotTo(HaveOccurred())
	defer resp.Body.Close()
	// Read up front: nothing here wants the body except a failure message,
	// and the daemon's replies to these verbs are one short line.
	body := drain(resp)
	Expect(resp.StatusCode).To(
		Equal(http.StatusOK),
		"POST %s failed: %s", path, body,
	)
}

// Torrents lists the torrents filed under category.
func (q *QBittorrent) Torrents(category string) []Torrent {
	GinkgoHelper()
	resp := q.Get("/api/v2/torrents/info?category=" + url.QueryEscape(category))
	defer resp.Body.Close()
	var torrents []Torrent
	Expect(json.NewDecoder(resp.Body).Decode(&torrents)).To(Succeed())
	return torrents
}

// Stop halts a torrent, releasing the session's handle on its files.
// qBittorrent 5 renamed the 4.x pause verb to stop.
func (q *QBittorrent) Stop(hash string) {
	GinkgoHelper()
	q.Post("/api/v2/torrents/stop", url.Values{"hashes": {hash}})
}

// Recheck re-hashes a torrent's data on disk against its piece hashes, which
// is how a torrent whose payload arrived out of band reaches 100%.
func (q *QBittorrent) Recheck(hash string) {
	GinkgoHelper()
	q.Post("/api/v2/torrents/recheck", url.Values{"hashes": {hash}})
}

// Remove drops a torrent from the client and leaves its files behind.
func (q *QBittorrent) Remove(hash string) {
	GinkgoHelper()
	q.Post("/api/v2/torrents/delete", url.Values{
		"hashes":      {hash},
		"deleteFiles": {"false"},
	})
}

// drain reads a response body for a failure description. It consumes the
// body, so it belongs only in the message of an assertion that is failing.
func drain(resp *http.Response) string {
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return "<unreadable body: " + err.Error() + ">"
	}
	return string(raw)
}

// StartQBittorrent starts (once per process) a qBittorrent container whose
// WebUI auth is bypassed via subnet whitelist and whose default save path is
// downloadDir, bind-mounted at the SAME path inside the container so paths
// qBittorrent reports are directly readable by the host-side importer.
func StartQBittorrent(downloadDir string) *QBittorrent {
	GinkgoHelper()
	qbOnce.Do(func() { qb, qbErr = startQB(downloadDir) })
	Expect(qbErr).NotTo(HaveOccurred())
	// One container serves the whole process, so a later caller naming a
	// different directory would otherwise silently get the first one's mount.
	Expect(downloadDir).To(
		Equal(qb.SaveDir),
		"qBittorrent is already running with a different download dir",
	)
	return qb
}

// qbConfTemplate is seeded before the first boot. The linuxserver init leaves
// an existing profile in place, so the whitelist is already live when the
// WebUI binds: /api/v2/auth/login then answers "Ok." and hands out a SID
// cookie for any credentials, which is what the client's connection test
// needs without this file having to carry a password hash.
const qbConfTemplate = `[LegalNotice]
Accepted=true

[Preferences]
WebUI\AuthSubnetWhitelist=0.0.0.0/0, ::/0
WebUI\AuthSubnetWhitelistEnabled=true
WebUI\LocalHostAuth=false
WebUI\CSRFProtection=false
WebUI\HostHeaderValidation=false

[BitTorrent]
Session\DefaultSavePath=%s
`

func startQB(downloadDir string) (*QBittorrent, error) {
	ctx, cancel := context.WithTimeout(context.Background(), qbStartTimeout)
	defer cancel()

	conf := fmt.Sprintf(qbConfTemplate, downloadDir)
	// qBittorrent drops to PUID/PGID before touching the bind mount, so it has
	// to run as whoever created downloadDir — the test process itself.
	uid := strconv.Itoa(os.Getuid())
	gid := strconv.Itoa(os.Getgid())

	qbt, err := testcontainers.Run(ctx, qbImage,
		testcontainers.WithEnv(map[string]string{
			"WEBUI_PORT": qbWebUI,
			"PUID":       uid,
			"PGID":       gid,
			"TZ":         "UTC",
		}),
		testcontainers.WithExposedPorts(qbWebUIPort),
		testcontainers.WithFiles(testcontainers.ContainerFile{
			Reader:            strings.NewReader(conf),
			ContainerFilePath: qbConfig,
			FileMode:          0o644,
		}),
		testcontainers.WithHostConfigModifier(func(hc *container.HostConfig) {
			hc.Binds = append(hc.Binds, downloadDir+":"+downloadDir)
		}),
		testcontainers.WithWaitStrategy(
			wait.ForHTTP("/api/v2/app/version").
				WithPort(qbWebUIPort).
				WithStartupTimeout(qbReadyTimeout),
		),
	)
	if err != nil {
		// Run hands the container back even when startup failed, so its log
		// tail is available to say why — a readiness timeout is otherwise
		// indistinguishable from a config the image rejected.
		wrapped := fmt.Errorf("start %s: %w%s", qbImage, err, logTail(ctx, qbt))
		if qbt == nil {
			return nil, wrapped
		}
		tctx, tcancel := context.WithTimeout(
			context.WithoutCancel(ctx),
			logTailGrace,
		)
		defer tcancel()
		return nil, errors.Join(wrapped, qbt.Terminate(tctx))
	}

	host, err := qbt.Host(ctx)
	if err != nil {
		return nil, err
	}
	mapped, err := qbt.MappedPort(ctx, qbWebUIPort)
	if err != nil {
		return nil, err
	}
	port, err := strconv.ParseUint(mapped.Port(), 10, 16)
	if err != nil {
		return nil, err
	}
	return &QBittorrent{
		Host:    host,
		Port:    uint16(port),
		SaveDir: downloadDir,
	}, nil
}
