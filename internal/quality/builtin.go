package quality

func mustFormat(name string, conds ...Condition) Format {
	f, err := NewFormat(name, conds)
	if err != nil {
		panic(err)
	}
	return f
}

func title(pat string) Condition {
	return Condition{Type: ConditionReleaseTitle, Pattern: pat}
}

var builtins = []Format{
	mustFormat("remux", Condition{
		Type: ConditionReleaseTitle, Pattern: `(?i)\bremux\b`, Required: true,
	}),
	// title-or-codec pairs: two optional conditions = "any of".
	mustFormat("x265",
		title(`(?i)\b(x[ .]?265|hevc|h[ .]?265)\b`),
		Condition{Type: ConditionCodec, Value: "hevc"}),
	mustFormat("x264",
		title(`(?i)\b(x[ .]?264|h[ .]?264|avc)\b`),
		Condition{Type: ConditionCodec, Value: "h264"}),
	mustFormat("av1",
		title(`(?i)\bav1\b`),
		Condition{Type: ConditionCodec, Value: "av1"}),
	mustFormat("hdr", Condition{
		Type:     ConditionReleaseTitle,
		Pattern:  `(?i)\b(hdr10\+|hdr10|hdr|dv|dovi|dolby[ ._-]?vision)\b`,
		Required: true,
	}),
	mustFormat("resolution-2160p", Condition{
		Type: ConditionResolution, Value: "2160p", Required: true,
	}),
	mustFormat("resolution-1080p", Condition{
		Type: ConditionResolution, Value: "1080p", Required: true,
	}),
	mustFormat("resolution-720p", Condition{
		Type: ConditionResolution, Value: "720p", Required: true,
	}),
	mustFormat("scene-junk", Condition{
		Type:     ConditionReleaseTitle,
		Pattern:  `(?i)\b(cam(rip)?|hdcam|hdts|telesync|ts[- .]?rip|telecine|dvdscr|screener|workprint)\b`,
		Required: true,
	}),
	mustFormat("bad-group", Condition{
		Type:     ConditionReleaseGroup,
		Pattern:  `(?i)^(yify|yts([ ._-]?(mx|ag|am|lt))?|axxo|msd|fgt|stuttershit)$`,
		Required: true,
	}),
	mustFormat("re-encode", Condition{
		Type: ConditionReleaseTitle, Pattern: `(?i)\bre-?encoded?\b`,
		Required: true,
	}),
	mustFormat("multi-audio", Condition{
		Type:     ConditionReleaseTitle,
		Pattern:  `(?i)\b(multi|dual[ ._-]?audio)\b`,
		Required: true,
	}),
	mustFormat("dubbed", Condition{
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
