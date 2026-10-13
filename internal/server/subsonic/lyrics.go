package subsonic

import (
	"bufio"
	"io"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/datahearth/streamline/internal/library/audiotags"
)

// maxLyricsFile bounds a sidecar .lrc read: real ones are a few KB.
const maxLyricsFile = 256 << 10

type parsedLyrics struct {
	synced bool
	lines  []lyricLine
}

// plain is the lyrics as text, timestamps dropped.
func (p parsedLyrics) plain() string {
	out := make([]string, 0, len(p.lines))
	for _, l := range p.lines {
		out = append(out, l.Value)
	}
	return strings.Join(out, "\n")
}

// readLyrics reads a song's lyrics: a sidecar .lrc of the same name first,
// the tag embedded in the file second. Either may be LRC-timed.
func readLyrics(audioPath string) (parsedLyrics, bool) {
	sidecar := strings.TrimSuffix(audioPath, extOf(audioPath)) + ".lrc"
	if text, ok := readSidecar(sidecar); ok {
		if p := parseLRC(text); len(p.lines) > 0 {
			return p, true
		}
	}
	text, err := audiotags.Lyrics(audioPath)
	if err != nil || text == "" {
		return parsedLyrics{}, false
	}
	p := parseLRC(text)
	return p, len(p.lines) > 0
}

func extOf(path string) string {
	if i := strings.LastIndexByte(path, '.'); i > strings.LastIndexByte(path, '/') {
		return path[i:]
	}
	return ""
}

func readSidecar(path string) (string, bool) {
	f, err := os.Open(path) //nolint:gosec // derived from a library row's path
	if err != nil {
		return "", false
	}
	defer f.Close()
	buf, err := io.ReadAll(io.LimitReader(f, maxLyricsFile))
	if err != nil || len(buf) == 0 {
		return "", false
	}
	return string(buf), true
}

var (
	lrcTime = regexp.MustCompile(`^\[(\d{1,3}):(\d{1,2})(?:[.:](\d{1,3}))?\]`)
	lrcTag  = regexp.MustCompile(`^\[([a-zA-Z#]+):(.*)\]\s*$`)
)

// parseLRC reads LRC ("[01:02.34]words", several stamps allowed on a line,
// [offset:+ms] honoured) into timed lines; text with no stamp at all is
// returned line by line, untimed.
func parseLRC(text string) parsedLyrics {
	var (
		timed   []lyricLine
		untimed []lyricLine
		offset  int64
	)
	sc := bufio.NewScanner(strings.NewReader(strings.TrimPrefix(text, "\ufeff")))
	sc.Buffer(make([]byte, 0, 64<<10), maxLyricsFile)
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r")
		var stamps []int64
		rest := strings.TrimSpace(line)
		for {
			m := lrcTime.FindStringSubmatch(rest)
			if m == nil {
				break
			}
			stamps = append(stamps, lrcMillis(m))
			rest = strings.TrimSpace(rest[len(m[0]):])
		}
		if len(stamps) > 0 {
			for _, ms := range stamps {
				start := ms
				timed = append(timed, lyricLine{Start: &start, Value: rest})
			}
			continue
		}
		if m := lrcTag.FindStringSubmatch(rest); m != nil {
			if strings.EqualFold(m[1], "offset") {
				if n, err := strconv.ParseInt(
					strings.TrimSpace(m[2]),
					10,
					64,
				); err == nil {
					offset = n
				}
			}
			continue
		}
		untimed = append(untimed, lyricLine{Value: rest})
	}
	if len(timed) > 0 {
		sort.SliceStable(
			timed,
			func(i, j int) bool { return *timed[i].Start < *timed[j].Start },
		)
		for i := range timed {
			// A positive offset shows the words earlier.
			*timed[i].Start = max(*timed[i].Start-offset, 0)
		}
		return parsedLyrics{synced: true, lines: timed}
	}
	// Trim blank lines at either end; keep the stanza breaks between.
	for len(untimed) > 0 && untimed[0].Value == "" {
		untimed = untimed[1:]
	}
	for len(untimed) > 0 && untimed[len(untimed)-1].Value == "" {
		untimed = untimed[:len(untimed)-1]
	}
	return parsedLyrics{lines: untimed}
}

func lrcMillis(m []string) int64 {
	minutes, _ := strconv.ParseInt(m[1], 10, 64)
	seconds, _ := strconv.ParseInt(m[2], 10, 64)
	var frac int64
	if m[3] != "" {
		frac, _ = strconv.ParseInt(m[3], 10, 64)
		switch len(m[3]) {
		case 1:
			frac *= 100
		case 2:
			frac *= 10
		}
	}
	return (minutes*60+seconds)*1000 + frac
}
