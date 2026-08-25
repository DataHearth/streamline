package quality

func mustFormat(name, description string, conds ...Condition) Format {
	f, err := NewFormat(name, conds)
	if err != nil {
		panic(err)
	}
	f.Description = description
	return f
}

func title(pat string) Condition {
	return Condition{Type: ConditionReleaseTitle, Pattern: pat}
}

var builtins = []Format{
	mustFormat(
		"remux",
		"Untouched Blu-ray video stream — the highest quality source, and the largest files.",
		Condition{
			Type: ConditionReleaseTitle, Pattern: `(?i)\bremux\b`, Required: true,
		},
	),
	// title-or-codec pairs: two optional conditions = "any of".
	mustFormat("x265",
		"HEVC/x265 encodes — much smaller files at the same visual quality.",
		title(`(?i)\b(x[ .]?265|hevc|h[ .]?265)\b`),
		Condition{Type: ConditionCodec, Value: "hevc"}),
	mustFormat(
		"x264",
		"H.264/x264 encodes — the most widely compatible codec, at the cost of larger files.",
		title(`(?i)\b(x[ .]?264|h[ .]?264|avc)\b`),
		Condition{Type: ConditionCodec, Value: "h264"},
	),
	mustFormat(
		"av1",
		"AV1 encodes — the newest codec, smallest files, but needs modern hardware to decode smoothly.",
		title(`(?i)\bav1\b`),
		Condition{Type: ConditionCodec, Value: "av1"},
	),
	mustFormat(
		"hdr",
		"HDR10, HDR10+ or Dolby Vision releases — needs an HDR-capable display to look better than SDR.",
		Condition{
			Type:     ConditionReleaseTitle,
			Pattern:  `(?i)\b(hdr10\+|hdr10|hdr|dv|dovi|dolby[ ._-]?vision)\b`,
			Required: true,
		},
	),
	mustFormat("resolution-2160p",
		"Matches any 2160p (4K) release.",
		Condition{
			Type: ConditionResolution, Value: "2160p", Required: true,
		}),
	mustFormat("resolution-1080p",
		"Matches any 1080p release.",
		Condition{
			Type: ConditionResolution, Value: "1080p", Required: true,
		}),
	mustFormat("resolution-720p",
		"Matches any 720p release.",
		Condition{
			Type: ConditionResolution, Value: "720p", Required: true,
		}),
	mustFormat(
		"scene-junk",
		"Cam and telesync rips recorded in a theater — near-universally worth blocking.",
		Condition{
			Type:     ConditionReleaseTitle,
			Pattern:  `(?i)\b(cam(rip)?|hdcam|hdts|telesync|ts[- .]?rip|telecine|dvdscr|screener|workprint)\b`,
			Required: true,
		},
	),
	mustFormat("bad-group",
		"Release groups known for low-bitrate re-encodes (YIFY, YTS, aXXo, …).",
		Condition{
			Type:     ConditionReleaseGroup,
			Pattern:  `(?i)^(yify|yts([ ._-]?(mx|ag|am|lt))?|axxo|msd|fgt|stuttershit)$`,
			Required: true,
		}),
	mustFormat(
		"re-encode",
		"Releases marked as re-encoded from another release, usually a further quality loss.",
		Condition{
			Type: ConditionReleaseTitle, Pattern: `(?i)\bre-?encoded?\b`,
			Required: true,
		},
	),
	mustFormat("multi-audio",
		"Releases carrying more than one audio language.",
		Condition{
			Type:     ConditionReleaseTitle,
			Pattern:  `(?i)\b(multi|dual[ ._-]?audio)\b`,
			Required: true,
		}),
	mustFormat("dubbed",
		"Releases flagged as dubbed audio, rather than the original language track.",
		Condition{
			Type: ConditionReleaseTitle, Pattern: `(?i)\bdubbed\b`, Required: true,
		}),
}

func Builtins() []Format { return builtins }

func BuiltinByName(name string) (Format, bool) {
	for _, f := range builtins {
		if f.Name == name {
			return f, true
		}
	}
	return Format{}, false
}

func IsBuiltinName(name string) bool {
	_, ok := BuiltinByName(name)
	return ok
}
