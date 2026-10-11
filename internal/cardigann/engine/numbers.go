package engine

import (
	"strconv"
	"strings"
	"unicode"
)

// normalizeNumber is ParseUtil.NormalizeNumber: keep digits, '.' and ',', read
// '-' as 0, and settle which separator is the decimal one. An integer keeps
// both separators only when it has both; otherwise '.' groups thousands. A
// float reads ',' as the decimal point and keeps only the last '.'.
func normalizeNumber(s string, isInt bool) string {
	var b strings.Builder
	for _, r := range s {
		if unicode.IsDigit(r) || r == '.' || r == ',' {
			b.WriteRune(r)
		}
	}
	v := strings.ReplaceAll(strings.TrimSpace(b.String()), "-", "0")
	if isInt {
		if strings.Contains(v, ",") && strings.Contains(v, ".") {
			return v
		}
		if v == "" {
			return "0"
		}
		return strings.ReplaceAll(v, ".", ",")
	}
	if v == "" {
		return "0"
	}
	v = strings.ReplaceAll(v, ",", ".")
	if strings.Count(v, ".") > 1 {
		last := strings.LastIndex(v, ".")
		v = strings.ReplaceAll(v[:last], ".", "") + v[last:]
	}
	return v
}

// coerceFloat is ParseUtil.CoerceDouble.
func coerceFloat(s string) (float64, bool) {
	f, err := strconv.ParseFloat(normalizeNumber(s, false), 64)
	return f, err == nil
}

// coerceInt is ParseUtil.CoerceLong. .NET's NumberStyles.Any reads ',' as a
// thousands separator, so the separators an integer kept are dropped here.
func coerceInt(s string) (int64, bool) {
	v := strings.NewReplacer(",", "", ".", "").Replace(normalizeNumber(s, true))
	n, err := strconv.ParseInt(v, 10, 64)
	return n, err == nil
}

// leadingInt is ParseUtil.GetLongFromString: the first run of digits.
func leadingInt(s string) int64 {
	start := strings.IndexFunc(s, isASCIIDigit)
	if start < 0 {
		return 0
	}
	end := start
	for end < len(s) && isASCIIDigit(rune(s[end])) {
		end++
	}
	n, err := strconv.ParseInt(s[start:end], 10, 64)
	if err != nil {
		return 0
	}
	return n
}

func isASCIIDigit(r rune) bool { return r >= '0' && r <= '9' }

// parseSize is ParseUtil.GetBytes: a number and a binary unit taken from the
// value's letters ("1,5 GiB", "700MB").
func parseSize(s string) (int64, bool) {
	var unit strings.Builder
	for _, r := range s {
		if unicode.IsLetter(r) {
			unit.WriteRune(r)
		}
	}
	v, ok := coerceFloat(s)
	if !ok {
		return 0, false
	}
	u := strings.ToLower(strings.ReplaceAll(unit.String(), "i", ""))
	switch {
	case strings.Contains(u, "kb"):
		v *= 1 << 10
	case strings.Contains(u, "mb"):
		v *= 1 << 20
	case strings.Contains(u, "gb"):
		v *= 1 << 30
	case strings.Contains(u, "tb"):
		v *= 1 << 40
	}
	return int64(v), true
}
