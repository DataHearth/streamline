package main

// The demo library: public-domain films and TV, the Blender Studio's open
// films, Creative Commons music and public-domain books. Titles are looked up
// by name through the instance, so no provider id is written down here; a
// title the provider no longer answers for is skipped with a warning.

// film is a movie file adopted into the library by the bulk import.
type film struct {
	Title  string
	Year   int
	Height int
	SizeMB int64
	HDR    bool
	Source string // release source, for the file name
	Group  string
}

var libraryFilms = []film{
	{"A Trip to the Moon", 1902, 1080, 1330, false, "BluRay", "NTb"},
	{"The Cabinet of Dr. Caligari", 1920, 1080, 6900, false, "BluRay", "AMIABLE"},
	{"Nosferatu", 1922, 1080, 7800, false, "BluRay", "AMIABLE"},
	{"Battleship Potemkin", 1925, 2160, 24600, true, "UHD.BluRay", "SiGMA"},
	{"The General", 1926, 1080, 9100, false, "BluRay", "CiNEFiLE"},
	{"Metropolis", 1927, 2160, 31200, true, "UHD.BluRay", "SiGMA"},
	{"His Girl Friday", 1940, 1080, 8800, false, "BluRay", "AMIABLE"},
	{"Carnival of Souls", 1962, 1080, 7900, false, "BluRay", "KOGi"},
	{"Charade", 1963, 2160, 14300, false, "UHD.BluRay", "KOGi"},
	{"Night of the Living Dead", 1968, 2160, 31800, true, "UHD.BluRay", "SiGMA"},
	{"Elephants Dream", 2006, 1080, 820, false, "WEB-DL", "KOGi"},
	{"Big Buck Bunny", 2008, 2160, 4200, false, "WEB-DL", "KOGi"},
	{"Sintel", 2010, 2160, 5100, true, "WEB-DL", "KOGi"},
	{"Caminandes: Llama Drama", 2013, 1080, 1000, false, "BluRay", "KOGi"},
	{"Agent 327: Operation Barbershop", 2017, 1080, 744, false, "BluRay", "KOGi"},
	{"Coffee Run", 2020, 1080, 947, false, "WEB-DL", "KOGi"},
	{"Sprite Fright", 2021, 2160, 3100, true, "WEB-DL", "KOGi"},
	{"Charge", 2022, 2160, 2300, true, "WEB-DL", "KOGi"},
}

// incomingFilms land in the movie root after the first adoption; a second
// scan of it is left awaiting review: the library import page.
var incomingFilms = []film{
	{"Nanook of the North", 1922, 1080, 6100, false, "BluRay", "AMIABLE"},
	{"Safety Last!", 1923, 1080, 7300, false, "BluRay", "CiNEFiLE"},
	{"Sherlock Jr.", 1924, 1080, 4200, false, "BluRay", "CiNEFiLE"},
	{"The Phantom of the Opera", 1925, 1080, 8100, false, "BluRay", "AMIABLE"},
	{"Steamboat Willie", 1928, 1080, 310, false, "WEB-DL", "KOGi"},
}

// grabbedFilm is added wanted, then grabbed from the fake tracker.
type grabbedFilm struct {
	Title  string
	Year   int
	Height int // what the release name claims
	Actual int // what the file holds; differing makes the import hold
	// SizeMB: small ones complete before the shots, large ones stay in the
	// queue. Over 50 MB, or the importer skips the file as a sample.
	SizeMB int64
}

// Grabbed first and waited for: one imports, one is held on its resolution.
var quickFilms = []grabbedFilm{
	{"Plan 9 from Outer Space", 1957, 1080, 1080, 120},
	{"Spring", 2019, 2160, 1080, 140},
}

// Grabbed last, so they are still downloading when the queue is shot. The
// tracker hashes every byte of what it seeds twice before it comes up (once
// to author the torrent, once to verify it), so these stay a few GB: large
// enough to look like films, small enough to start in seconds.
var slowFilms = []grabbedFilm{
	{"Tears of Steel", 2012, 2160, 2160, 3100},
	{"Cosmos Laundromat", 2015, 2160, 2160, 2400},
	{"Nothing Sacred", 1937, 2160, 2160, 2900},
}

// wantedFilms are monitored with nothing on disk. The first is the manual
// search shot: the tracker lists it at every resolution.
var wantedFilms = []film{
	{Title: "Detour", Year: 1945},
	{Title: "The Little Shop of Horrors", Year: 1960},
	{Title: "House on Haunted Hill", Year: 1959},
}

// show is a series adopted into the library; Seasons maps a season to the
// episodes on disk.
type show struct {
	Title   string
	Year    int
	Seasons map[int]int
	EpMB    int64
}

var libraryShows = []show{
	{"The Lone Ranger", 1949, map[int]int{1: 12}, 410},
	{"The Cisco Kid", 1950, map[int]int{1: 13, 2: 13}, 390},
	{"Annie Oakley", 1954, map[int]int{1: 13}, 380},
	{"Bonanza", 1959, map[int]int{1: 10}, 720},
	{"One Step Beyond", 1959, map[int]int{1: 12}, 360},
	{"The Beverly Hillbillies", 1962, map[int]int{1: 12}, 350},
	{"The Lucy Show", 1962, map[int]int{1: 15}, 360},
	{"Petticoat Junction", 1963, map[int]int{1: 14}, 370},
}

// seasonPack is a season grabbed from the tracker and left downloading.
type seasonPack struct {
	Show     string
	Season   int
	Episodes int
	EpMB     int64
}

var slowPacks = []seasonPack{
	{"The Cisco Kid", 3, 10, 350},
	{"Petticoat Junction", 2, 10, 350},
}

// artist is followed with every release; Albums are the ones adopted from
// disk (matched by title), Grab the ones fetched from the tracker.
type artist struct {
	Name   string
	Albums []string
	Grab   []string
}

var artists = []artist{
	{Name: "Brad Sucks", Albums: []string{"I Don't Know What I'm Doing"}, Grab: []string{"Out of It"}},
	{Name: "Jonathan Coulton", Albums: []string{"Thing a Week One", "Artificial Heart"}},
	{Name: "Broke For Free", Albums: []string{"Layers"}},
	{Name: "Chris Zabriskie", Albums: []string{"Divider"}},
	{Name: "Josh Woodward", Albums: []string{"Breadcrumbs"}, Grab: []string{"The Simple Life"}},
	{Name: "Kimiko Douglass-Ishizaka", Albums: []string{"Open Goldberg Variations"}},
	// Released under CC BY-NC-SA.
	{Name: "Nine Inch Nails", Albums: []string{"Ghosts I–IV", "The Slip"}},
}

// book is an ebook adopted from disk.
type book struct {
	Title  string
	Author string
}

var libraryBooks = []book{
	{"Pride and Prejudice", "Jane Austen"},
	{"Moby-Dick", "Herman Melville"},
	{"Alice's Adventures in Wonderland", "Lewis Carroll"},
	{"The Picture of Dorian Gray", "Oscar Wilde"},
	{"Treasure Island", "Robert Louis Stevenson"},
	{"Little Women", "Louisa May Alcott"},
	{"The Time Machine", "H. G. Wells"},
	{"The War of the Worlds", "H. G. Wells"},
	{"The Great Gatsby", "F. Scott Fitzgerald"},
	{"Winnie-the-Pooh", "A. A. Milne"},
	// Volumes of the series below: added after the adoption, the series takes
	// them in.
	{"A Study in Scarlet", "Arthur Conan Doyle"},
	{"The Sign of the Four", "Arthur Conan Doyle"},
	{"The Wonderful Wizard of Oz", "L. Frank Baum"},
}

// bookSeries are followed whole; a volume already in the library is adopted.
var bookSeries = []book{
	{"Sherlock Holmes", "Arthur Conan Doyle"},
	{"Oz", "L. Frank Baum"},
}

// Books added wanted and grabbed from the tracker: an ebook and an audiobook.
var (
	grabEbook     = book{"Frankenstein", "Mary Shelley"}
	grabAudiobook = book{"Dracula", "Bram Stoker"}
	wantedBooks   = []book{{"The Count of Monte Cristo", "Alexandre Dumas"}}
)

// request is something the member asks for.
type request struct {
	Kind   string // movie, series, album, book
	Title  string
	Year   int
	Artist string // album
	Decide string // "", "approve" or "deny"
}

var requests = []request{
	{Kind: "movie", Title: "The Gold Rush", Year: 1925},
	{Kind: "movie", Title: "The Kid", Year: 1921, Decide: "approve"},
	{Kind: "movie", Title: "The Lost World", Year: 1925},
	{Kind: "series", Title: "Dragnet", Year: 1951},
	{Kind: "album", Title: "Bitter Suite", Artist: "Kevin MacLeod"},
	{Kind: "book", Title: "Twenty Thousand Leagues Under the Seas", Year: 1870},
	{Kind: "book", Title: "Around the World in Eighty Days", Year: 1872, Decide: "deny"},
}
