package transcoding

import (
	"bufio"
	"io"
	"strconv"
	"strings"
	"time"
)

// Snapshot is a point-in-time view of a running encode.
type Snapshot struct {
	Percent float64
	Speed   float64
	ETA     time.Duration
}

// readProgress consumes ffmpeg -progress key=value output and calls emit at
// each block boundary (progress=continue|end). Returns on EOF.
func readProgress(r io.Reader, total time.Duration, emit func(Snapshot)) {
	scanner := bufio.NewScanner(r)
	var elapsed time.Duration
	var speed float64

	for scanner.Scan() {
		key, value, ok := strings.Cut(scanner.Text(), "=")
		if !ok {
			continue
		}

		switch strings.TrimSpace(key) {
		case "out_time_us":
			if us, err := strconv.ParseInt(
				strings.TrimSpace(value),
				10,
				64,
			); err == nil {
				// ffmpeg emits a math.MinInt64 sentinel before the first real
				// timestamp; multiplying it by time.Microsecond overflows.
				if us < 0 {
					us = 0
				}
				elapsed = time.Duration(us) * time.Microsecond
			}
		case "speed":
			speed = 0
			if f, err := strconv.ParseFloat(
				strings.TrimSuffix(strings.TrimSpace(value), "x"),
				64,
			); err == nil {
				speed = f
			}
		case "progress":
			snap := Snapshot{Speed: speed}
			if total > 0 {
				snap.Percent = min(100, float64(elapsed)/float64(total)*100)
			}
			if speed > 0 {
				snap.ETA = time.Duration(float64(total-elapsed) / speed)
			}
			emit(snap)
		}
	}
}
