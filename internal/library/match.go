package library

import (
	"regexp"
	"strings"

	"golang.org/x/text/unicode/norm"

	"github.com/datahearth/streamline/ent"
)

var (
	titleNonAlnum    = regexp.MustCompile(`[^a-z0-9]+`)
	titleLatinLetter = regexp.MustCompile(`[a-z]`)
	// A letter of any script but Latin: "not (a non-letter or a Latin one)".
	titleOtherScript = regexp.MustCompile(`[^\P{L}\p{Latin}]`)
	// An accent goes where a release name would not spell it; in other
	// scripts a mark is part of its letter (が is か plus a voicing mark).
	titleAccent      = regexp.MustCompile(`([\p{Latin}\p{Greek}\p{Cyrillic}])\p{M}+`)
	titleNonLetter   = regexp.MustCompile(`[^\p{L}\p{N}\p{M}]+`)
	titleArticleHead = regexp.MustCompile(`^(the|a|an)`)
	// TVDB disambiguates same-named shows inside the title string itself
	// ("Foundation (2021)"), so the year has to come off before comparing or
	// the disambiguated entry never equals the folder it belongs to.
	titleYearSuffix = regexp.MustCompile(`\s*\((?:19|20)\d{2}\)\s*$`)
)

// unaccentable spells out the Latin letters Unicode decomposition leaves
// whole, the way a tracker types them: "Kælan Mikla" is uploaded as "Kaelan
// Mikla" and "Mgła" as "Mgla", and dropped as non-ASCII they compared as
// "klan" and "mga".
var unaccentable = strings.NewReplacer(
	"æ", "ae", "œ", "oe", "ø", "o", "ð", "d", "đ", "d", "þ", "th",
	"ł", "l", "ß", "ss", "ı", "i",
)

// normalizeTitle lowercases, strips non-alphanumerics, and strips a leading
// article ("the"/"a"/"an") for tolerant title comparison (e.g. "The Batman" vs
// "the.batman" vs "Batman").
//
// NFD is what makes an accented library title comparable to a scene release,
// which never carries one: decomposing splits "é" into "e" plus a combining
// mark, and titleNonAlnum drops that mark like any other non-ASCII byte.
// Without it "Détective Conan" would normalize to "dtectiveconan" and match
// none of its own releases, so preferTitleMatches would find nothing to prefer
// and fall back to handing the whole result set to the season-pack grabber.
//
// A title with no Latin letter at all ("Сказка", "進撃の巨人") keeps the
// letters of its own script, Cyrillic romanized so that it equals the
// "Skazka" a scene release writes. A title holding any Latin letter is
// reduced to those: a tracker's "Левиафан / Leviathan" has to stay equal to
// "Leviathan". normalizePair is what compares two names that both hold
// another script.
func normalizeTitle(s string) string {
	s = titleYearSuffix.ReplaceAllString(s, "")
	s = unaccentable.Replace(strings.ToLower(norm.NFD.String(s)))
	if titleLatinLetter.MatchString(s) {
		s = titleNonAlnum.ReplaceAllString(s, "")
	} else {
		s = foldAllScripts(s)
	}
	s = titleArticleHead.ReplaceAllString(s, "")
	return s
}

func foldAllScripts(s string) string {
	s = cyrillicToLatin.Replace(titleAccent.ReplaceAllString(s, "$1"))
	return titleNonLetter.ReplaceAllString(s, "")
}

// normalizeAllScripts is normalizeTitle keeping every script's letters,
// whatever the title holds.
func normalizeAllScripts(s string) string {
	s = titleYearSuffix.ReplaceAllString(s, "")
	s = unaccentable.Replace(strings.ToLower(norm.NFD.String(s)))
	return titleArticleHead.ReplaceAllString(foldAllScripts(s), "")
}

// normalizePair normalizes two names for comparison with each other. When
// both hold letters of another script they are compared on every letter:
// reduced to their Latin ones, "患者A" was "a" and "勇気のReason" was
// "reason", equal to any release spelling that fragment. When either is
// Latin alone the other is read by its Latin letters, which is what keeps a
// two-script release name equal to the Latin title it carries.
func normalizePair(a, b string) (string, string) {
	if titleOtherScript.MatchString(a) && titleOtherScript.MatchString(b) {
		return normalizeAllScripts(a), normalizeAllScripts(b)
	}
	return normalizeTitle(a), normalizeTitle(b)
}

// cyrillicToLatin is the common English romanization ("zh", "ya", "kh"), the
// one a Cyrillic name is compared under; cyrillicSpellings covers the others.
// Runs after NFD, where й and ё are и and е plus a combining mark.
var cyrillicToLatin = strings.NewReplacer(
	"а", "a", "б", "b", "в", "v", "г", "g", "д", "d", "е", "e", "ж", "zh",
	"з", "z", "и", "i", "к", "k", "л", "l", "м", "m", "н", "n", "о", "o",
	"п", "p", "р", "r", "с", "s", "т", "t", "у", "u", "ф", "f", "х", "kh",
	"ц", "ts", "ч", "ch", "ш", "sh", "щ", "shch", "ъ", "", "ы", "y", "ь", "",
	"э", "e", "ю", "yu", "я", "ya", "і", "i", "є", "e", "ґ", "g",
)

// cyrillicScientific is the scholarly romanization as a release name carries
// it, its carons dropped: "Сладкая жизнь" is uploaded as "Sladkaja_žizn".
// cyrillicLookalike is the Latin letter or digit each Cyrillic letter looks
// like, for the names typed on a Latin keyboard: "СКАЗКА" as "CKA3KA".
var (
	cyrillicScientific = strings.NewReplacer(
		"а", "a", "б", "b", "в", "v", "г", "g", "д", "d", "е", "e", "ё", "e",
		"ж", "z", "з", "z", "и", "i", "й", "j", "к", "k", "л", "l", "м", "m",
		"н", "n", "о", "o", "п", "p", "р", "r", "с", "s", "т", "t", "у", "u",
		"ф", "f", "х", "h", "ц", "c", "ч", "c", "ш", "s", "щ", "sc", "ъ", "",
		"ы", "y", "ь", "", "э", "e", "ю", "ju", "я", "ja", "і", "i", "є", "je",
	)
	cyrillicLookalike = strings.NewReplacer(
		"а", "a", "в", "b", "е", "e", "з", "3", "к", "k", "м", "m", "н", "h",
		"о", "o", "р", "p", "с", "c", "т", "t", "у", "y", "х", "x", "ч", "4",
		"б", "6", "і", "i",
	)
	cyrillicLetter = regexp.MustCompile(`\p{Cyrillic}`)
)

// cyrillicSpellings is a Cyrillic name under the Latin spellings a release
// may carry besides the common romanization: the scholarly one, and its
// look-alike letters when every letter has one. Nothing for a name with no
// Cyrillic in it.
func cyrillicSpellings(s string) []string {
	s = strings.ToLower(norm.NFC.String(s))
	if !cyrillicLetter.MatchString(s) {
		return nil
	}
	out := []string{cyrillicScientific.Replace(s)}
	if alike := cyrillicLookalike.Replace(s); !cyrillicLetter.MatchString(alike) {
		out = append(out, alike)
	}
	return out
}

// TitleComparable reports whether a title survives normalization: one made of
// punctuation alone ("!!!") normalizes to nothing and can be compared to no
// other.
func TitleComparable(s string) bool { return normalizeAllScripts(s) != "" }

// TitleMatches reports whether two titles are equal after normalization.
func TitleMatches(a, b string) bool {
	na, nb := normalizePair(a, b)
	return na == nb
}

// TitleMatchesStrict is TitleMatches for callers deciding whether two names
// denote the same thing: a name that normalizes to nothing ("!!!") matches
// nothing, where TitleMatches would call two of them equal.
func TitleMatchesStrict(a, b string) bool {
	na, nb := normalizePair(a, b)
	return na != "" && na == nb
}

// TitleMatchesAny reports whether title equals name or any of alts after
// normalization. alts are alternate names for the same work (TVDB aliases and
// translations), which is the only way a romaji folder name reaches an entry
// TVDB stores under its English title.
func TitleMatchesAny(title, name string, alts []string) bool {
	if TitleMatches(title, name) {
		return true
	}
	for _, a := range alts {
		if TitleMatches(title, a) {
			return true
		}
	}
	return false
}

// TitlePrefixMatches reports whether one normalized title starts with the
// other, both being non-empty. It is a weaker signal than TitleMatches and is
// only used to rank candidates, never to confirm one.
func TitlePrefixMatches(a, b string) bool {
	na, nb := normalizePair(a, b)
	if na == "" || nb == "" {
		return false
	}
	return strings.HasPrefix(na, nb) || strings.HasPrefix(nb, na)
}

// titleTagSuffix matches what may sit between a release's parsed title and the
// show it names while still leaving it the same show: the tags extractTitle
// could not cut. Normalization has already stripped the separators, hence the
// repetition — "Breaking Bad COMPLETE MULTI" leaves "completemulti".
//
// The list is deliberately short. A word that is not on it is treated as part
// of the title, so a release naming a longer work is refused; adding a word
// that can distinguish two works would let that release match again.
var titleTagSuffix = regexp.MustCompile(
	`^(?:complet|complete|integral|integrale|multi|french|truefrench|` +
		`vostfr|vff|vf|vo|uncut|extended|remastered|repack|proper)+$`,
)

// TitleNamesSameWork reports whether a release title names the same work as
// title. It is the confirming counterpart to TitlePrefixMatches, which a
// caller that grabs unattended cannot use: a prefix is shared by every longer
// work built on the same name, and bare prefix tolerance handed eight
// "Narcos Mexico" episodes to Narcos' season 2.
//
// The two directions are not the same question, so they get different answers.
// What a *release* carries beyond the title is either a tag the parser could
// not cut ("Breaking Bad COMPLETE") or the rest of another work's name
// ("Narcos Mexico"), and only the first is allowed. What a *library entry*
// carries beyond the release is the provider's disambiguating subtitle, which
// releases routinely drop — TMDB's "… Infinity Castle Chapter 1: The Return of
// Akaza" against a tracker's "… Infinity Castle" — so that direction stays
// tolerant. It leaves a release named for the shorter work matching the longer
// one, which is the only reading that reaches a title held under a fuller
// name.
func TitleNamesSameWork(release, title string) bool {
	nr, nt := normalizePair(release, title)
	if nr == "" || nt == "" {
		return false
	}
	if strings.HasPrefix(nt, nr) {
		return true
	}
	rest, found := strings.CutPrefix(nr, nt)
	return found && titleTagSuffix.MatchString(rest)
}

// MatchEpisode resolves a parsed release to an episode within the show's
// seasons. Anime packs match on absolute number; everything else on
// season+episode number. Returns nil when nothing matches.
func MatchEpisode(
	parsed ParseResult,
	seasons []*ent.Season,
	anime bool,
) *ent.Episode {
	_, ep := MatchEpisodeInSeason(parsed, seasons, anime)
	return ep
}

// MatchEpisodeInSeason is MatchEpisode plus the number of the season the match
// lives in — episodes don't carry it, and callers rendering a destination path
// need it. Returns (0, nil) when nothing matches.
func MatchEpisodeInSeason(
	parsed ParseResult,
	seasons []*ent.Season,
	anime bool,
) (uint16, *ent.Episode) {
	for _, se := range seasons {
		for _, e := range se.Edges.Episodes {
			if anime && parsed.AbsoluteNumber > 0 {
				if e.AbsoluteNumber == parsed.AbsoluteNumber {
					return se.Number, e
				}
				continue
			}
			if se.Number == parsed.Season && e.Number == parsed.Episode {
				return se.Number, e
			}
		}
	}
	// An anime group may write the absolute number in SxxExx form: TLC and
	// Tsundere-Raws both publish "The.Elusive.Samurai.S02E15" for the episode
	// TVDB numbers S02E03 (absolute 15), listing it on the indexer as S02E03
	// and naming the file inside the torrent S02E15. The parser reads a real
	// season and episode there, so AbsoluteNumber is 0 and the loop above never
	// consults it — a grab whose keep-set matched nothing then fails with
	// ErrNoWantedFiles before any client is contacted, and the episode is never
	// filled no matter how many acceptable releases exist.
	//
	// Only reached once the exact season+episode found nothing, so a season
	// that genuinely holds that episode still wins.
	if anime && parsed.AbsoluteNumber == 0 && parsed.Episode > 0 {
		for _, se := range seasons {
			for _, e := range se.Edges.Episodes {
				if e.AbsoluteNumber == parsed.Episode {
					return se.Number, e
				}
			}
		}
	}
	return 0, nil
}
