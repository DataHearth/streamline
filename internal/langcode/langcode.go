// Package langcode canonicalises the language codes a media container carries.
// Leaf package with no internal imports: internal/ffmpeg normalises at probe
// time so the stored column has one spelling, and internal/quality normalises a
// condition's value so an operator's spelling reaches the same place. Neither
// may import the other, and duplicating the table would let the two drift into
// silently never matching.
package langcode

import "strings"

// aliases folds every spelling onto ISO 639-2/T. ISO 639-2 gives twenty-odd
// languages both a bibliographic and a terminological code and containers carry
// either, so the same French track arrives as "fre" from one muxer and "fra"
// from the next; two-letter 639-1 codes turn up as well. A real 5425-file
// library held 5292 "fre" tags and not one "fra" — a condition written the
// other way would have matched nothing at all.
var aliases = map[string]string{
	"fre": "fra", "fr": "fra",
	"ger": "deu", "de": "deu",
	"dut": "nld", "nl": "nld",
	"chi": "zho", "zh": "zho",
	"cze": "ces", "cs": "ces",
	"gre": "ell", "el": "ell",
	"per": "fas", "fa": "fas",
	"rum": "ron", "ro": "ron",
	"ice": "isl", "is": "isl",
	"slo": "slk", "sk": "slk",
	"en": "eng", "es": "spa", "it": "ita", "ja": "jpn",
	"pt": "por", "ru": "rus", "ko": "kor", "ar": "ara",
	"pl": "pol", "sv": "swe", "da": "dan", "no": "nor",
	"fi": "fin", "tr": "tur", "he": "heb", "hi": "hin",
}

// Canonical lowercases and folds one code. An unrecognised code is returned
// lowercased and otherwise untouched — the table covers the spellings that
// actually collide, not every language that exists.
func Canonical(code string) string {
	c := strings.ToLower(strings.TrimSpace(code))
	if canon, ok := aliases[c]; ok {
		return canon
	}
	return c
}
