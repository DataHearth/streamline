package transcoding

import (
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("readProgress", Label("unit", "transcoding"), func() {
	It("emits a snapshot per block and computes percent/speed/eta", func() {
		input := strings.Join([]string{
			"out_time_us=30000000",
			"speed=2x",
			"progress=continue",
			"out_time_us=60000000",
			"speed=2x",
			"progress=continue",
		}, "\n") + "\n"

		var snapshots []Snapshot
		readProgress(strings.NewReader(input), 120*time.Second, func(s Snapshot) {
			snapshots = append(snapshots, s)
		})

		Expect(snapshots).To(HaveLen(2))
		second := snapshots[1]
		Expect(second.Percent).To(BeNumerically("~", 50, 0.01))
		Expect(second.Speed).To(Equal(2.0))
		Expect(second.ETA).To(Equal(30 * time.Second))
	})

	It("does not panic on speed=N/A and reports zero speed and eta", func() {
		input := "out_time_us=1000000\nspeed=N/A\nprogress=end\n"
		var snapshots []Snapshot
		Expect(func() {
			readProgress(strings.NewReader(input), 10*time.Second, func(s Snapshot) {
				snapshots = append(snapshots, s)
			})
		}).NotTo(Panic())

		Expect(snapshots).To(HaveLen(1))
		Expect(snapshots[0].Speed).To(Equal(0.0))
		Expect(snapshots[0].ETA).To(Equal(time.Duration(0)))
	})

	It("ignores unknown keys", func() {
		input := "bogus_key=whatever\nout_time_us=5000000\nspeed=1x\nprogress=continue\n"
		var snapshots []Snapshot
		readProgress(strings.NewReader(input), 10*time.Second, func(s Snapshot) {
			snapshots = append(snapshots, s)
		})
		Expect(snapshots).To(HaveLen(1))
		Expect(snapshots[0].Percent).To(BeNumerically("~", 50, 0.01))
	})

	It("caps percent at 100", func() {
		input := "out_time_us=200000000\nspeed=1x\nprogress=continue\n"
		var snapshots []Snapshot
		readProgress(strings.NewReader(input), 10*time.Second, func(s Snapshot) {
			snapshots = append(snapshots, s)
		})
		Expect(snapshots).To(HaveLen(1))
		Expect(snapshots[0].Percent).To(Equal(100.0))
	})

	It("clamps ffmpeg's negative out_time_us startup sentinel to zero", func() {
		input := "out_time_us=-9223372036854775808\nspeed=1x\nprogress=continue\n"
		var snapshots []Snapshot
		readProgress(strings.NewReader(input), 10*time.Second, func(s Snapshot) {
			snapshots = append(snapshots, s)
		})
		Expect(snapshots).To(HaveLen(1))
		Expect(snapshots[0].Percent).To(Equal(0.0))
		Expect(snapshots[0].ETA).To(Equal(10 * time.Second))
	})

	It("returns on EOF with no trailing progress line", func() {
		input := "out_time_us=1000000\nspeed=1x\n"
		var calls int
		readProgress(strings.NewReader(input), 10*time.Second, func(s Snapshot) {
			calls++
		})
		Expect(calls).To(Equal(0))
	})
})
