package library

import (
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/tvshow"
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

// minSpecialTitleChars is the shortest special title a release is matched
// by, counted in characters on its compared words: the alias harvest's floor
// (internal/metadata/aliases.go) for the same reason, a short common word
// matching most of a show's releases.
const minSpecialTitleChars = 4

var (
	// squareTagRe is a bracketed tag, which in a release name is never part
	// of a title: a fansub group, a resolution, a checksum.
	squareTagRe = regexp.MustCompile(`\[[^\]]*\]`)
	yearWordRe  = regexp.MustCompile(`^(?:19|20)\d{2}$`)
	digitsRe    = regexp.MustCompile(`^\d+$`)
	// seasonWordRe is a season token as titleWords leaves it, with or without
	// its episode.
	seasonWordRe = regexp.MustCompile(`^s\d{2}(?:e\d{2,3})?$`)
	// wordGlue closes an apostrophe up, as release names do ("Chesters"), and
	// spells the ampersand out.
	wordGlue = strings.NewReplacer("'", "", "’", "", "&", " and ")

	numberWords = map[string]string{
		"one": "1", "two": "2", "three": "3", "four": "4", "five": "5",
		"six": "6", "seven": "7", "eight": "8", "nine": "9", "ten": "10",
	}
	// specialMarkers are the words a release may put between the show and a
	// special's title, with or without a number after them ("OAD 01 - Title").
	specialMarkers = map[string]bool{
		"special": true, "ova": true, "oav": true, "oad": true,
	}
	// genericSpecialWords name no special in particular: a title made of
	// them, numbers and release tags ("Episode 3", "Pilot", "Extras") is
	// carried by too many releases to match on.
	genericSpecialWords = map[string]bool{
		"episode": true, "part": true, "chapter": true, "ova": true,
		"oav": true, "oad": true, "pilot": true, "extra": true,
		"extras": true, "bonus": true, "trailer": true,
	}
)

// titleWords splits a title into the words it is compared by: lowercased,
// accents and apostrophes dropped, "&" read as "and", a number as its digits
// whether spelled out or zero-padded, and "part" dropped before a number, so
// "Part One", "Part 01" and "(1)" are the same word.
func titleWords(s string) []string {
	s = unaccentable.Replace(strings.ToLower(norm.NFD.String(s)))
	words := splitWords(wordGlue.Replace(titleAccent.ReplaceAllString(s, "$1")))
	for i, w := range words {
		if n, ok := numberWords[w]; ok {
			words[i] = n
		} else if digitsRe.MatchString(w) && len(w) > 1 {
			words[i] = strings.TrimLeft(w[:len(w)-1], "0") + w[len(w)-1:]
		}
	}
	out := make([]string, 0, len(words))
	for i, w := range words {
		if (w == "part" || w == "pt") && i+1 < len(words) &&
			digitsRe.MatchString(words[i+1]) {
			continue
		}
		out = append(out, w)
	}
	return out
}

// sansArticle drops a leading article, a whole word: "Abba" keeps its A.
func sansArticle(words []string) []string {
	if len(words) > 1 &&
		(words[0] == "the" || words[0] == "a" || words[0] == "an") {
		return words[1:]
	}
	return words
}

// titleCore drops what a release and the library add to the end of a
// special's title independently of each other: the word "special", the tags
// titleTagSuffix knows and one year ("Polar" / "Polar Special", "The
// Reunion" / "The Reunion Extended 2012"). The first word always stays.
func titleCore(words []string) []string {
	year := false
	for n := len(words); n > 1; n-- {
		switch last := words[n-1]; {
		case last == "special" || titleTagSuffix.MatchString(last):
		case !year && yearWordRe.MatchString(last):
			year = true
		default:
			return words[:n]
		}
	}
	return words[:min(len(words), 1)]
}

// trailingYear is the year titleCore drops, empty without one.
func trailingYear(words []string) string {
	for _, w := range words[len(titleCore(words)):] {
		if yearWordRe.MatchString(w) {
			return w
		}
	}
	return ""
}

// cutWords returns words past prefix, false when they do not start with it.
func cutWords(words, prefix []string) ([]string, bool) {
	if len(words) < len(prefix) || !slices.Equal(words[:len(prefix)], prefix) {
		return nil, false
	}
	return words[len(prefix):], true
}

// showWords is a show's name as a release spells it: no year suffix, no
// leading article.
func showWords(show string) []string {
	return sansArticle(titleWords(titleYearSuffix.ReplaceAllString(show, "")))
}

// specialWords is a special's title as a release spells it: less a repeat of
// the show's name ("Top Gear: Polar Special") and a leading article. nil for
// a title that names nothing: the show's own name, or a titleCore under
// minSpecialTitleChars or made of generic words, numbers and release tags
// only, in one word ("Extended") or two ("Blu-ray").
func specialWords(special string, show []string) []string {
	w := sansArticle(titleWords(special))
	if rest, ok := cutWords(w, show); ok {
		w = sansArticle(rest)
	}
	core := titleCore(w)
	joined := strings.Join(core, "")
	if utf8.RuneCountInString(joined) < minSpecialTitleChars ||
		isNonGroupTag(joined) ||
		!slices.ContainsFunc(core, func(t string) bool {
			return !genericSpecialWords[t] && !isNonGroupTag(t)
		}) {
		return nil
	}
	return w
}

// releaseWords is a release name's words up to its first technical tag, the
// container extension and every bracketed tag left out. The release group is
// not cut: with no technical tag before it, it reads as one more word.
func releaseWords(release string) []string {
	if i := strings.LastIndex(release, "."); i > 0 &&
		mediaExtensions[strings.ToLower(release[i+1:])] {
		release = release[:i]
	}
	release = squareTagRe.ReplaceAllString(release, " ")
	return titleWords(release[:techIndex(release)])
}

// NamesSpecial returns which of titles, the titles of a show's specials, a
// release names: "Doctor.Who.2005.The.Day.of.the.Doctor.1080p", the way most
// specials are released. -1 when it names none, or no single one. Unattended
// grabs trust the answer, so titles are compared word for word, never by
// prefix or substring.
//
// The name must open with one of shows (the library's titles and aliases),
// then may carry a year, a season token and specialMarkers. A release that
// numbers a regular episode or reads as a pack never passes, so the season
// token left is a special's: S00Exx under whatever number the tracker gave
// it, SxxE00, or the season a titled special is filed under. What follows, up
// to the first technical tag, is the title (pickTitle): a word after it
// ("Trailer", "Part 2") or before it ("Confidential") is another release.
func NamesSpecial(release string, shows, titles []string) int {
	if p := Parse(release); (p.Season > 0 && p.Episode > 0) ||
		ParseSeasonSpan(release) != (SeasonSpan{}) {
		return -1
	}
	words := sansArticle(releaseWords(release))
	for _, show := range shows {
		sw := showWords(show)
		tail, ok := cutWords(words, sw)
		if len(sw) == 0 || !ok {
			continue
		}
		if len(tail) > 0 && yearWordRe.MatchString(tail[0]) {
			tail = tail[1:]
		}
		if len(tail) > 0 && seasonWordRe.MatchString(tail[0]) {
			tail = tail[1:]
		}
		if i := pastMarkers(tail, titles, sw); i >= 0 {
			return i
		}
	}
	return -1
}

// pastMarkers picks the title tail spells (pickTitle), as it stands or past
// leading specialMarkers: a title is free to open with one itself ("Special
// Delivery"), so the tail is tried before each is dropped.
func pastMarkers(tail, titles, show []string) int {
	for marker := false; ; tail = tail[1:] {
		if i, found := pickTitle(sansArticle(tail), titles, show); found {
			return i
		}
		switch {
		case len(tail) > 0 && specialMarkers[tail[0]]:
			marker = true
		case marker && len(tail) > 0 && digitsRe.MatchString(tail[0]):
			marker = false
		default:
			return -1
		}
	}
}

// pickTitle picks the title that words spell. Titles sharing words' titleCore
// are the candidates, and the season's other specials decide between them:
// the one equal to words word for word wins ("Christmas Special 2012" over
// "Christmas Special"), else a candidate standing alone and dated no other year.
// Two exact titles, or several candidates and none exact
// ("…Christmas.Special.2011" against those two), name no single special: found
// with -1, so no shorter reading is tried.
func pickTitle(words, titles, show []string) (index int, found bool) {
	core := titleCore(words)
	year := trailingYear(words)
	var exact, candidates []int
	otherYear := false
	for i, t := range titles {
		tw := specialWords(t, show)
		if tw == nil || !slices.Equal(titleCore(tw), core) {
			continue
		}
		candidates = append(candidates, i)
		if y := trailingYear(tw); y != "" && year != "" && y != year {
			otherYear = true
		}
		if slices.Equal(tw, words) {
			exact = append(exact, i)
		}
	}
	switch {
	case len(candidates) == 0:
		return -1, false
	case len(exact) == 1:
		return exact[0], true
	case len(exact) == 0 && len(candidates) == 1 && !otherYear:
		return candidates[0], true
	}
	return -1, true
}

// SpecialQuery is the text asked of an indexer for a special by its title:
// the show's name then the special's, each without its "(year)" suffix and
// its punctuation, the show's name said once. Empty when the title names
// nothing (specialWords), which would ask for the show alone.
func SpecialQuery(show, special string) string {
	plain := func(s string) []string {
		return splitWords(apostrophes.Replace(
			titleYearSuffix.ReplaceAllString(s, ""),
		))
	}
	sw, tw := plain(show), plain(special)
	if len(sw) == 0 || specialWords(special, showWords(show)) == nil {
		return ""
	}
	if len(tw) > len(sw) && slices.EqualFunc(tw[:len(sw)], sw, strings.EqualFold) {
		tw = tw[len(sw):]
	}
	return strings.Join(append(sw, tw...), " ")
}

var apostrophes = strings.NewReplacer("'", "", "’", "")

// ShowNames is every name a release may carry for the show.
func ShowNames(show *ent.TVShow) []string {
	return append([]string{show.Title, show.OriginalTitle}, show.Aliases...)
}

// Specials is the show's season 0 episodes.
func Specials(show *ent.TVShow) []*ent.Episode {
	for _, se := range show.Edges.Seasons {
		if se.Number == 0 {
			return se.Edges.Episodes
		}
	}
	return nil
}

// SpecialOf is the show's season 0 episode holding id, nil when id is none of
// its specials.
func SpecialOf(show *ent.TVShow, id uint32) *ent.Episode {
	for _, e := range Specials(show) {
		if e.ID == id {
			return e
		}
	}
	return nil
}

// SpecialTitles is what a search for the show's episode id hands
// NamesSpecial: that special's title first, then the titles of the show's
// other specials. nil when id is not one of its specials.
func SpecialTitles(show *ent.TVShow, id uint32) []string {
	special := SpecialOf(show, id)
	if special == nil {
		return nil
	}
	titles := []string{special.Title}
	for _, other := range Specials(show) {
		if other.ID != id {
			titles = append(titles, other.Title)
		}
	}
	return titles
}

// NamedSpecial is the special of the show a release names by its title
// (NamesSpecial against every special's title), nil for none.
func NamedSpecial(release string, show *ent.TVShow) *ent.Episode {
	specials := Specials(show)
	titles := make([]string, len(specials))
	for i, e := range specials {
		titles[i] = e.Title
	}
	if i := NamesSpecial(release, ShowNames(show), titles); i >= 0 {
		return specials[i]
	}
	return nil
}

// MatchFileEpisode resolves one file of a release to an episode of the show.
// special is the season 0 episode the release was taken for, nil for any
// other release: a file naming it by title (NamedSpecial) is that episode,
// whatever number it carries. Every other file goes by MatchEpisodeInSeason.
func MatchFileEpisode(
	name string,
	show *ent.TVShow,
	special *ent.Episode,
) (uint16, *ent.Episode) {
	if special != nil {
		if named := NamedSpecial(
			name,
			show,
		); named != nil &&
			named.ID == special.ID {
			return 0, special
		}
	}
	return MatchEpisodeInSeason(
		Parse(name), show.Edges.Seasons, show.Type == tvshow.TypeAnime,
	)
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
	// that genuinely holds that episode still wins. Never for season 0: a
	// special's number counts the specials, so S00E05 is not absolute 5.
	if anime && parsed.AbsoluteNumber == 0 && parsed.Season > 0 &&
		parsed.Episode > 0 {
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
