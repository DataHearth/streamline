package containers

import (
	"context"
	"errors"
	"fmt"
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
	qbImage = "lscr.io/linuxserver/qbittorrent:5.0.3"

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

var (
	qbOnce sync.Once
	qb     *QBittorrent
	qbErr  error
)

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
