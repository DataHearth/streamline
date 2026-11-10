package main

import (
	"fmt"
	"strings"
	"time"
)

// The demo library: public-domain films and television, Creative Commons
// music and public-domain books, so the screenshots can show real artwork.
// Each title carries the state it should end in. "import" titles are grabbed
// through the fake indexer and imported from real files, "download" ones stay
// in the queue at a fixed progress, "held" fails verification, and "wanted"
// ones are only added.

const (
	planImport   = "import"
	planDownload = "download"
	planHeld     = "held"
	planWanted   = "wanted"
)

const gb = int64(1_000_000_000)

type progress struct {
	State string
	Pct   float64
	Speed int64 // bytes/s
	ETA   int64 // seconds
}

var (
	fast    = func(pct float64, eta int64) progress { return progress{"downloading", pct, 1_900_000, eta} }
	slow    = func(pct float64, eta int64) progress { return progress{"downloading", pct, 480_000, eta} }
	stalled = func(pct float64) progress { return progress{"stalledDL", pct, 0, 8640000} }
	paused  = func(pct float64) progress { return progress{"stoppedDL", pct, 0, 8640000} }
)

type movieSpec struct {
	TMDB   int
	Title  string
	Year   int
	Plan   string
	Res    string // 1080p or 2160p
	Source string // BluRay or WEB-DL
	Group  string
	Size   int64
	Prog   progress
}

var movies = []movieSpec{
	{
		TMDB:   19,
		Title:  "Metropolis",
		Year:   1927,
		Plan:   planImport,
		Res:    "1080p",
		Source: "BluRay",
		Group:  "SiGMA",
		Size:   17 * gb,
	},
	{
		TMDB:   10331,
		Title:  "Night of the Living Dead",
		Year:   1968,
		Plan:   planImport,
		Res:    "2160p",
		Source: "BluRay",
		Group:  "TOMMY",
		Size:   31 * gb,
	},
	{
		TMDB:   653,
		Title:  "Nosferatu",
		Year:   1922,
		Plan:   planImport,
		Res:    "1080p",
		Source: "BluRay",
		Group:  "KOGi",
		Size:   9 * gb,
	},
	{
		TMDB:   775,
		Title:  "A Trip to the Moon",
		Year:   1902,
		Plan:   planImport,
		Res:    "1080p",
		Source: "WEB-DL",
		Group:  "NTb",
		Size:   gb + gb/3,
	},
	{
		TMDB:   961,
		Title:  "The General",
		Year:   1926,
		Plan:   planImport,
		Res:    "1080p",
		Source: "BluRay",
		Group:  "EDITH",
		Size:   11 * gb,
	},
	{
		TMDB:   992,
		Title:  "Sherlock Jr.",
		Year:   1924,
		Plan:   planImport,
		Res:    "1080p",
		Source: "BluRay",
		Group:  "KOGi",
		Size:   6 * gb,
	},
	{
		TMDB:   234,
		Title:  "The Cabinet of Dr. Caligari",
		Year:   1920,
		Plan:   planImport,
		Res:    "1080p",
		Source: "BluRay",
		Group:  "SiGMA",
		Size:   12 * gb,
	},
	{
		TMDB:   4808,
		Title:  "Charade",
		Year:   1963,
		Plan:   planImport,
		Res:    "2160p",
		Source: "WEB-DL",
		Group:  "HONE",
		Size:   19 * gb,
	},
	{
		TMDB:   3085,
		Title:  "His Girl Friday",
		Year:   1940,
		Plan:   planImport,
		Res:    "1080p",
		Source: "BluRay",
		Group:  "EDITH",
		Size:   8 * gb,
	},
	{
		TMDB:   10513,
		Title:  "Plan 9 from Outer Space",
		Year:   1957,
		Plan:   planImport,
		Res:    "1080p",
		Source: "WEB-DL",
		Group:  "playWEB",
		Size:   5 * gb,
	},
	{
		TMDB:   16093,
		Title:  "Carnival of Souls",
		Year:   1962,
		Plan:   planImport,
		Res:    "1080p",
		Source: "BluRay",
		Group:  "FLUX",
		Size:   10 * gb,
	},
	{
		TMDB:   24452,
		Title:  "The Little Shop of Horrors",
		Year:   1960,
		Plan:   planImport,
		Res:    "1080p",
		Source: "WEB-DL",
		Group:  "EDITH",
		Size:   4 * gb,
	},
	{
		TMDB:   20367,
		Title:  "Detour",
		Year:   1945,
		Plan:   planImport,
		Res:    "1080p",
		Source: "BluRay",
		Group:  "KOGi",
		Size:   7 * gb,
	},
	{
		TMDB:   15856,
		Title:  "House on Haunted Hill",
		Year:   1959,
		Plan:   planImport,
		Res:    "1080p",
		Source: "BluRay",
		Group:  "TOMMY",
		Size:   9 * gb,
	},
	{
		TMDB:   10378,
		Title:  "Big Buck Bunny",
		Year:   2008,
		Plan:   planImport,
		Res:    "2160p",
		Source: "WEB-DL",
		Group:  "KOGi",
		Size:   3 * gb,
	},
	{
		TMDB:   45745,
		Title:  "Sintel",
		Year:   2010,
		Plan:   planImport,
		Res:    "2160p",
		Source: "WEB-DL",
		Group:  "HONE",
		Size:   4 * gb,
	},
	{
		TMDB:   9761,
		Title:  "Elephants Dream",
		Year:   2006,
		Plan:   planImport,
		Res:    "1080p",
		Source: "WEB-DL",
		Group:  "NTb",
		Size:   gb,
	},
	{
		TMDB:   24018,
		Title:  "Glen or Glenda",
		Year:   1953,
		Plan:   planImport,
		Res:    "1080p",
		Source: "BluRay",
		Group:  "playWEB",
		Size:   17 * gb,
	},
	{
		TMDB:   43353,
		Title:  "Robot Monster",
		Year:   1953,
		Plan:   planImport,
		Res:    "1080p",
		Source: "WEB-DL",
		Group:  "EDITH",
		Size:   5 * gb,
	},

	{
		TMDB:   358332,
		Title:  "Cosmos Laundromat",
		Year:   2015,
		Plan:   planHeld,
		Res:    "2160p",
		Source: "WEB-DL",
		Group:  "KOGi",
		Size:   6 * gb,
	},

	{
		TMDB:   133701,
		Title:  "Tears of Steel",
		Year:   2012,
		Plan:   planDownload,
		Res:    "2160p",
		Source: "WEB-DL",
		Group:  "BYNDR",
		Size:   13 * gb,
		Prog:   fast(0.86, 18*60),
	},
	{
		TMDB:   643,
		Title:  "Battleship Potemkin",
		Year:   1925,
		Plan:   planDownload,
		Res:    "2160p",
		Source: "BluRay",
		Group:  "SiGMA",
		Size:   24 * gb,
		Prog:   fast(0.62, 46*60),
	},
	{
		TMDB:   10098,
		Title:  "The Kid",
		Year:   1921,
		Plan:   planDownload,
		Res:    "1080p",
		Source: "BluRay",
		Group:  "KOGi",
		Size:   8 * gb,
		Prog:   slow(0.37, 2*3600+25*60),
	},
	{
		TMDB:   3596,
		Title:  "Things to Come",
		Year:   1936,
		Plan:   planDownload,
		Res:    "1080p",
		Source: "BluRay",
		Group:  "FLUX",
		Size:   9 * gb,
		Prog:   stalled(0.18),
	},
	{
		TMDB:   25768,
		Title:  "Steamboat Bill, Jr.",
		Year:   1928,
		Plan:   planDownload,
		Res:    "1080p",
		Source: "BluRay",
		Group:  "EDITH",
		Size:   7 * gb,
		Prog:   paused(0.44),
	},

	{
		TMDB:  457784,
		Title: "Agent 327: Operation Barbershop",
		Year:  2017,
		Plan:  planWanted,
	},
	{TMDB: 22596, Title: "Safety Last!", Year: 1923, Plan: planWanted},
	{TMDB: 962, Title: "The Gold Rush", Year: 1925, Plan: planWanted},
	{TMDB: 17058, Title: "Scarlet Street", Year: 1945, Plan: planWanted},
	{TMDB: 28503, Title: "Dementia 13", Year: 1963, Plan: planWanted},
	{TMDB: 31655, Title: "The Terror", Year: 1963, Plan: planWanted},
}

// heldMovie is the release whose file is a 1080p encode under a 2160p name,
// which is what the importer's verification holds.
func (m movieSpec) release() string {
	codec := "x264"
	if m.Res == "2160p" {
		codec = "x265.10bit"
	}
	audio := "DDP5.1"
	if m.Source == "BluRay" {
		audio = "DTS-HD.MA.5.1"
	}
	return fmt.Sprintf(
		"%s.%d.%s.%s.%s.%s-%s",
		dotted(m.Title),
		m.Year,
		m.Res,
		m.Source,
		codec,
		audio,
		m.Group,
	)
}

type seasonSpec struct {
	N     int
	Plan  string
	Group string
	Size  int64
	Prog  progress
}

type seriesSpec struct {
	TVDB    int
	Title   string
	Seasons []seasonSpec
}

var series = []seriesSpec{
	{TVDB: 77754, Title: "The Andy Griffith Show", Seasons: []seasonSpec{
		{N: 1, Plan: planImport, Group: "SiGMA", Size: 21 * gb},
		{N: 2, Plan: planImport, Group: "SiGMA", Size: 22 * gb},
		{
			N:     3,
			Plan:  planDownload,
			Group: "RAWR",
			Size:  23 * gb,
			Prog:  slow(0.48, 3600+46*60),
		},
	}},
	{TVDB: 70695, Title: "The Lucy Show", Seasons: []seasonSpec{
		{N: 1, Plan: planImport, Group: "EDITH", Size: 18 * gb},
		{
			N:     2,
			Plan:  planDownload,
			Group: "HONE",
			Size:  18 * gb,
			Prog:  fast(0.65, 3600+8*60),
		},
	}},
	{TVDB: 77365, Title: "Annie Oakley", Seasons: []seasonSpec{
		{N: 1, Plan: planImport, Group: "SiGMA", Size: 6 * gb},
		{N: 2, Plan: planImport, Group: "SiGMA", Size: 6 * gb},
	}},
	{TVDB: 72059, Title: "The Lone Ranger", Seasons: []seasonSpec{
		{N: 1, Plan: planImport, Group: "playWEB", Size: 25 * gb},
	}},
	{TVDB: 77404, Title: "The Cisco Kid", Seasons: []seasonSpec{
		{N: 1, Plan: planImport, Group: "HONE", Size: 7 * gb},
		{N: 3, Plan: planDownload, Group: "HONE", Size: 6 * gb, Prog: fast(1, 26)},
	}},
	{TVDB: 71569, Title: "Petticoat Junction", Seasons: []seasonSpec{
		{N: 1, Plan: planImport, Group: "EDITH", Size: 19 * gb},
		{
			N:     2,
			Plan:  planDownload,
			Group: "HONE",
			Size:  18 * gb,
			Prog:  stalled(0.21),
		},
	}},
	{TVDB: 76479, Title: "One Step Beyond", Seasons: []seasonSpec{
		{N: 1, Plan: planImport, Group: "KOGi", Size: 11 * gb},
	}},
	{TVDB: 73378, Title: "Bonanza"},
	{TVDB: 77403, Title: "Adventures of Superman"},
	{TVDB: 71471, Title: "The Beverly Hillbillies"},
}

func (s seriesSpec) release(n seasonSpec) string {
	return fmt.Sprintf(
		"%s.S%02d.1080p.WEB-DL.x264.AAC2.0-%s",
		dotted(s.Title),
		n.N,
		n.Group,
	)
}

func (s seriesSpec) episodeFile(n seasonSpec, ep int) string {
	return fmt.Sprintf(
		"%s.S%02dE%02d.1080p.WEB-DL.x264.AAC2.0-%s.mkv",
		dotted(s.Title),
		n.N,
		ep,
		n.Group,
	)
}

type albumSpec struct {
	Title string
	Plan  string
	Size  int64
	Prog  progress
}

type artistSpec struct {
	MBID   string
	Name   string
	Albums []albumSpec
}

var artists = []artistSpec{
	{
		MBID: "d8df7087-06d5-4545-9024-831bb8558ad1",
		Name: "Jonathan Coulton",
		Albums: []albumSpec{
			{Title: "Thing a Week One", Plan: planImport},
			{
				Title: "Thing a Week Two",
				Plan:  planDownload,
				Size:  412_000_000,
				Prog:  slow(0.62, 46*60),
			},
		},
	},
	{
		MBID: "9c070e4f-28c1-4b98-b371-d8514c31ba55",
		Name: "Brad Sucks",
		Albums: []albumSpec{
			{Title: "Out of It", Plan: planImport},
			{Title: "I Don't Know What I'm Doing", Plan: planImport},
		},
	},
	{
		MBID: "b09c8a50-7bfa-434f-a2cf-f9bf9f6f91a5",
		Name: "Josh Woodward",
		Albums: []albumSpec{
			{Title: "Ashes", Plan: planImport},
		},
	},
	{
		MBID: "069a1c1f-14eb-4d36-b0a0-77dffbd67713",
		Name: "Broke For Free",
		Albums: []albumSpec{
			{Title: "Layers", Plan: planImport},
		},
	},
	{
		MBID: "cd661c5c-c9ce-42ff-83f8-7b1dd1515d64",
		Name: "Chris Zabriskie",
		Albums: []albumSpec{
			{Title: "Divider", Plan: planImport},
		},
	},
}

func (a artistSpec) release(al albumSpec, year string) string {
	return fmt.Sprintf("%s - %s (%s) [FLAC 16-44.1 WEB]", a.Name, al.Title, year)
}

type bookSpec struct {
	Hardcover int
	Title     string // what the release is named after
	Author    string
	Ebook     string // plan for the ebook slot
	Audiobook string // plan for the audiobook slot
	Prog      progress
}

var books = []bookSpec{
	{
		Hardcover: 2984470,
		Title:     "The Adventures of Tom Sawyer",
		Author:    "Mark Twain",
		Ebook:     planImport,
		Audiobook: planImport,
	},
	{
		Hardcover: 3059898,
		Title:     "A Christmas Carol",
		Author:    "Charles Dickens",
		Ebook:     planImport,
	},
	{
		Hardcover: 2973643,
		Title:     "Moby-Dick",
		Author:    "Herman Melville",
		Ebook:     planImport,
	},
	{
		Hardcover: 379647,
		Title:     "Pride and Prejudice",
		Author:    "Jane Austen",
		Ebook:     planImport,
	},
	{
		Hardcover: 428657,
		Title:     "Dracula",
		Author:    "Bram Stoker",
		Ebook:     planImport,
		Audiobook: planDownload,
		Prog:      fast(0.71, 9*60),
	},
	{
		Hardcover: 28722,
		Title:     "Frankenstein",
		Author:    "Mary Shelley",
		Ebook:     planWanted,
		Audiobook: planWanted,
	},
	{
		Hardcover: 2332122,
		Title:     "Dr. Jekyll and Mr. Hyde",
		Author:    "Robert Louis Stevenson",
		Ebook:     planWanted,
		Audiobook: planWanted,
	},
	{
		Hardcover: 377193,
		Title:     "The Great Gatsby",
		Author:    "F. Scott Fitzgerald",
		Ebook:     planWanted,
	},
	{
		Hardcover: 382607,
		Title:     "Treasure Island",
		Author:    "Robert Louis Stevenson",
		Ebook:     planImport,
	},
	{
		Hardcover: 374377,
		Title:     "Alice's Adventures in Wonderland",
		Author:    "Lewis Carroll",
		Ebook:     planImport,
	},
	{
		Hardcover: 69589,
		Title:     "The Time Machine",
		Author:    "H. G. Wells",
		Ebook:     planWanted,
	},
}

type bookSeriesSpec struct {
	Hardcover int
	Title     string
	// Volumes are imported as ebooks, by volume number.
	Volumes []int
}

var bookSeries = []bookSeriesSpec{
	{Hardcover: 6023, Title: "Oz", Volumes: []int{1, 2}},
	{Hardcover: 1098, Title: "Sherlock Holmes", Volumes: []int{1, 2}},
}

func ebookRelease(author, title string) string {
	return fmt.Sprintf("%s - %s (EPUB)", author, title)
}

func audiobookRelease(author, title string) string {
	return fmt.Sprintf("%s - %s (Unabridged) [MP3] Audiobook", author, title)
}

// People who ask for things, and what they ask for. Each request goes in as
// its requester; the decisions are the admin's.
type userSpec struct {
	Email, Name, Role string
}

var people = []userSpec{
	{"chloe@streamline.local", "Chloé Dubois", "request_only"},
	{"alice@streamline.local", "Alice Martin", "member"},
	{"emma@streamline.local", "Emma Lindqvist", "request_only"},
	{"ben@streamline.local", "Ben Okafor", "member"},
	{"dev@streamline.local", "Dev Patel", "request_only"},
}

type requestSpec struct {
	By        string // email
	MediaType string // movie, series, artist, album, book, book_series
	ID        int
	MBID      string
	Title     string
	Decision  string // "", approve, deny
	Reason    string
}

var requests = []requestSpec{
	{
		By:        "dev@streamline.local",
		MediaType: "movie",
		ID:        2760,
		Title:     "The Lodger: A Story of the London Fog",
	},
	{
		By:        "chloe@streamline.local",
		MediaType: "book_series",
		ID:        225328,
		Title:     "Peter Pan",
	},
	{
		By:        "ben@streamline.local",
		MediaType: "book_series",
		ID:        5894,
		Title:     "Tarzan",
	},
	{
		By:        "emma@streamline.local",
		MediaType: "artist",
		MBID:      "8292bf44-2dd1-4bad-8019-276ec38f28e7",
		Title:     "Kevin MacLeod",
	},
	{
		By:        "alice@streamline.local",
		MediaType: "movie",
		ID:        2981,
		Title:     "The Lost World",
	},
	{By: "chloe@streamline.local", MediaType: "movie", ID: 57283, Title: "Häxan"},
	{
		By:        "alice@streamline.local",
		MediaType: "movie",
		ID:        32574,
		Title:     "Meet John Doe",
		Decision:  "approve",
	},
	{
		By:        "ben@streamline.local",
		MediaType: "movie",
		ID:        15263,
		Title:     "McLintock!",
		Decision:  "approve",
	},
	{
		By:        "dev@streamline.local",
		MediaType: "movie",
		ID:        541,
		Title:     "The Man with the Golden Arm",
		Decision:  "deny",
		Reason:    "Not public domain yet",
	},
}

// adoptions is the folder the library-import screenshot reviews: films that
// are not in the library yet, named the way a hand-kept library names them,
// and two the matcher cannot place, which is what leaves the scan waiting
// on a person.
var adoptions = []struct {
	Folder, File string
}{
	{
		"A Trip to the Moon (1902)",
		"A.Trip.to.the.Moon.1902.1080p.WEB-DL.x265.10bit.DDP5.1-NTb.mkv",
	},
	{
		"Agent 327 Operation Barbershop (2017)",
		"Agent.327.Operation.Barbershop.2017.1080p.BluRay.x265.10bit.AAC5.1-KOGi.mkv",
	},
	{
		"Battleship Potemkin (1925)",
		"Battleship.Potemkin.1925.2160p.UHD.BluRay.x265.DV.HDR.TrueHD.7.1.Atmos-SiGMA.mkv",
	},
	{
		"Caminandes Llama Drama (2013)",
		"Caminandes.Llama.Drama.2013.1080p.BluRay.x265.10bit.AAC5.1-KOGi.mkv",
	},
	{
		"Carnival of Souls (1962)",
		"Carnival.of.Souls.1962.1080p.BluRay.x264.DTS-HD.MA.5.1-FLUX.mkv",
	},
	{"Dementia 13 (1963)", "Dementia.13.1963.1080p.BluRay.x264.FLAC.2.0-EDITH.mkv"},
	{
		"Meet John Doe (1941)",
		"Meet.John.Doe.1941.1080p.BluRay.x264.FLAC.1.0-KOGi.mkv",
	},
	{
		"Scarlet Street (1945)",
		"Scarlet.Street.1945.1080p.BluRay.x264.FLAC.1.0-TOMMY.mkv",
	},
	{
		"The Gold Rush (1925)",
		"The.Gold.Rush.1925.1080p.BluRay.x264.FLAC.2.0-SiGMA.mkv",
	},
	{
		"The Terror (1963)",
		"The.Terror.1963.1080p.BluRay.x264.DTS-HD.MA.2.0-FLUX.mkv",
	},
	{"home movies", "VID_20190714_183522.mkv"},
	{"misc", "untitled-final-v2.mkv"},
}

// dotted spells a title the way a scene release does.
func dotted(title string) string {
	r := strings.NewReplacer(":", "", "'", "", "!", "", ",", "", "?", "", "&", "and")
	return strings.Join(strings.Fields(r.Replace(title)), ".")
}

// releases lists every release the fake indexer serves and the fake client
// can report. Albums and book volumes need the library's own facts (a year,
// a track list), so they are added once those are known.
func baseReleases() []*release {
	var out []*release
	for _, m := range movies {
		if m.Plan == planWanted {
			continue
		}
		r := &release{
			Name:     m.release(),
			Category: "2000",
			Size:     m.Size,
			Match:    words(m.Title),
			Age:      30 * 24 * time.Hour,
		}
		applyProgress(r, m.Prog)
		out = append(out, r)
	}
	for _, s := range series {
		for _, n := range s.Seasons {
			r := &release{
				Name:     s.release(n),
				Category: "5000",
				Size:     n.Size,
				Match:    words(s.Title),
				Age:      21 * 24 * time.Hour,
			}
			applyProgress(r, n.Prog)
			out = append(out, r)
		}
	}
	return append(out, manualReleases()...)
}

// manualSearchMovie is the wanted film the manual-search screenshot opens:
// the indexer lists several releases of it, none grabbed.
const manualSearchMovie = "The Gold Rush"

var manualSearchReleases = []struct {
	res, source, group string
	size               int64
	age                time.Duration
	seeders            int
}{
	{"1080p", "BluRay", "SiGMA", 24 * gb, 30 * 24 * time.Hour, 214},
	{"2160p", "BluRay", "HONE", 12 * gb, 30 * 24 * time.Hour, 96},
	{"2160p", "WEB-DL", "RAWR", 20 * gb, 3 * 24 * time.Hour, 61},
	{"2160p", "BluRay", "EDITH", 16 * gb, 210 * 24 * time.Hour, 23},
	{"1080p", "WEB-DL", "KOGi", 9 * gb, 30 * 24 * time.Hour, 132},
	{"720p", "BluRay", "FLUX", 5 * gb, 400 * 24 * time.Hour, 8},
	{"2160p", "WEB-DL", "TOMMY", 40 * gb, 150 * 24 * time.Hour, 57},
}

func manualReleases() []*release {
	out := make([]*release, 0, len(manualSearchReleases))
	for _, v := range manualSearchReleases {
		m := movieSpec{
			Title:  manualSearchMovie,
			Year:   1925,
			Res:    v.res,
			Source: v.source,
			Group:  v.group,
		}
		out = append(out, &release{
			Name:     m.release(),
			Category: "2000",
			Size:     v.size,
			Match:    words(manualSearchMovie),
			Age:      v.age,
			Seeders:  v.seeders,
			State:    "downloading",
		})
	}
	return out
}

func applyProgress(r *release, p progress) {
	if p.State == "" {
		r.State, r.Progress = "downloading", 0
		return
	}
	r.State, r.Progress, r.Speed, r.ETA = p.State, p.Pct, p.Speed, p.ETA
}
