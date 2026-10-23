// Package containers starts real external services in Docker for the
// container-labeled e2e specs. A started container is reaped by
// testcontainers' ryuk when the process exits rather than torn down per spec,
// which keeps lazy sync.Once startup scope-free under Ginkgo; only a failed
// start is terminated eagerly, since ryuk can be disabled.
package containers

import (
	"context"
	"io"
	"os"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	"github.com/testcontainers/testcontainers-go"
)

const (
	enableEnv = "STREAMLINE_E2E_CONTAINERS"

	logTailLines = 20
	logTailGrace = 10 * time.Second
)

// Require skips the calling spec unless container runs are explicitly
// enabled AND a Docker daemon is reachable. STREAMLINE_E2E_CONTAINERS is set
// by `task test:e2e:containers`; the env gate keeps a bare `task test` (no
// label filter) from silently launching containers on dev machines.
func Require() {
	GinkgoHelper()
	if os.Getenv(enableEnv) == "" {
		Skip("container specs disabled — run via task test:e2e:containers")
	}
	provider, err := testcontainers.NewDockerProvider()
	if err != nil {
		Skip("no Docker daemon reachable: " + err.Error())
	}
	// Building the provider falls back to an env-derived client without ever
	// touching the socket, so it succeeds against a dead daemon. Health is the
	// call that proves the socket answers; it closes the provider either way.
	if err := provider.Health(context.Background()); err != nil {
		Skip("no Docker daemon reachable: " + err.Error())
	}
}

// logTail renders a container's last lines for an error message. It tolerates
// a nil container — a start can fail before one exists — and a ctx already
// past its deadline, which is the state a pull or readiness timeout leaves
// behind and precisely when the logs are worth reading.
func logTail(ctx context.Context, c *testcontainers.DockerContainer) string {
	if c == nil {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), logTailGrace)
	defer cancel()

	rc, err := c.Logs(ctx)
	if err != nil {
		return "\ncontainer logs unavailable: " + err.Error()
	}
	defer rc.Close()

	raw, err := io.ReadAll(rc)
	if err != nil && len(raw) == 0 {
		return "\ncontainer logs unreadable: " + err.Error()
	}
	lines := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
	if len(lines) > logTailLines {
		lines = lines[len(lines)-logTailLines:]
	}
	return "\n" + strings.Join(lines, "\n")
}
