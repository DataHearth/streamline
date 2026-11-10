package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("instance", Label("unit", "demoshots"), func() {
	var logPath string

	BeforeEach(func() {
		logPath = filepath.Join(GinkgoT().TempDir(), "server.log")
	})

	// unanswered is a loopback address nothing listens on.
	unanswered := func() string {
		port, err := freePort()
		Expect(err).NotTo(HaveOccurred())
		return "http://127.0.0.1:" + strconv.Itoa(port)
	}

	It(
		"reports the exit status and the log as soon as the process dies",
		func(ctx SpecContext) {
			in, err := startInstance(
				exec.Command("sh", "-c", "echo refused the config >&2; exit 3"),
				logPath,
			)
			Expect(err).NotTo(HaveOccurred())

			start := time.Now()
			err = in.waitHealthy(ctx, unanswered(), 2*time.Minute)
			Expect(err).To(MatchError(And(
				ContainSubstring("exited before it answered /health"),
				ContainSubstring("exit status 3"),
				ContainSubstring(logPath),
			)))
			Expect(time.Since(start)).To(BeNumerically("<", 5*time.Second))
			Expect(os.ReadFile(logPath)).To(ContainSubstring("refused the config"))

			By("stopping a process that is already gone without waiting on it again")
			in.stop()
		},
		SpecTimeout(30*time.Second),
	)

	It("returns once /health answers 200", func(ctx SpecContext) {
		srv := httptest.NewServer(
			http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/health" {
					w.WriteHeader(http.StatusNotFound)
				}
			}),
		)
		DeferCleanup(srv.Close)
		in, err := startInstance(exec.Command("sleep", "60"), logPath)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(in.stop)

		Expect(in.waitHealthy(ctx, srv.URL, 10*time.Second)).To(Succeed())
	}, SpecTimeout(30*time.Second))

	It(
		"times out on a process that stays up without answering",
		func(ctx SpecContext) {
			in, err := startInstance(exec.Command("sleep", "60"), logPath)
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(in.stop)

			err = in.waitHealthy(ctx, unanswered(), 600*time.Millisecond)
			Expect(err).To(BeAssignableToTypeOf(&pollTimeout{}))
		},
		SpecTimeout(30*time.Second),
	)

	It("terminates a running process on stop", func() {
		in, err := startInstance(exec.Command("sleep", "60"), logPath)
		Expect(err).NotTo(HaveOccurred())

		start := time.Now()
		in.stop()
		Expect(in.exited).To(BeClosed())
		Expect(time.Since(start)).To(BeNumerically("<", 5*time.Second))
		Expect(in.cmd.ProcessState.Exited()).To(BeFalse(), "ended by the signal")
	})

	It(
		"returns the context error when the run is interrupted",
		func(ctx SpecContext) {
			cctx, cancel := context.WithCancel(ctx)
			in, err := startInstance(exec.Command("sleep", "60"), logPath)
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(in.stop)

			time.AfterFunc(200*time.Millisecond, cancel)
			Expect(
				in.waitHealthy(cctx, unanswered(), time.Minute),
			).To(MatchError(context.Canceled))
		},
		SpecTimeout(30*time.Second),
	)
})
