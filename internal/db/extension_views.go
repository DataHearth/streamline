package db

type ExtensionView struct {
	Name, Description string
	// Bundle groups views a guest is granted together: music, books, or
	// empty for a view that stands alone.
	Bundle  string
	Columns []string
	SQL     string
	Tables  []string
}

var extensionViews = []ExtensionView{
	{
		Name:        "users",
		Description: "See user names and roles",
		Columns:     []string{"id", "display_name", "role", "locked"},
		SQL: `SELECT id, COALESCE(display_name, '') AS display_name, role,
	-- The driver stores a time as Go prints it ("2006-01-02 15:04:05.9 -0700 MST"),
	-- which julianday cannot read; one it still cannot place counts as locked.
	CASE WHEN locked_until IS NULL THEN 0 ELSE COALESCE(COALESCE(
		julianday(locked_until),
		julianday(substr(locked_until, 1, 19)
			|| substr(substr(locked_until, 20), instr(substr(locked_until, 20), ' ') + 1, 3)
			|| ':'
			|| substr(substr(locked_until, 20), instr(substr(locked_until, 20), ' ') + 4, 2))
	) > julianday('now'), 1) END AS locked
FROM core.users`,
		Tables: []string{"users"},
	},
	{
		Name:        "user_logins",
		Description: "See users' email addresses",
		Columns:     []string{"user_id", "email"},
		SQL:         `SELECT id AS user_id, lower(email) AS email FROM core.users`,
		Tables:      []string{"users"},
	},
	{
		Name:        "books",
		Bundle:      "books",
		Description: "Read the book library",
		Columns: []string{
			"id", "title", "original_title", "sort_title", "author_name", "kind",
			"genre", "release_year", "release_date", "overview",
			"preferred_language", "series_id", "series_position", "added_at",
			"updated_at",
		},
		SQL: `SELECT id, title, original_title, sort_title, author_name, kind,
	genre, release_year, release_date, overview, preferred_language,
	book_series_volumes AS series_id, series_position,
	create_time AS added_at, update_time AS updated_at
FROM core.books`,
		Tables: []string{"books"},
	},
	{
		Name:        "book_series",
		Bundle:      "books",
		Description: "Read the book series",
		Columns: []string{
			"id", "title", "original_title", "sort_title", "author_name", "kind",
			"overview", "ongoing",
		},
		SQL: `SELECT id, title, original_title, sort_title, author_name, kind,
	overview, ongoing
FROM core.book_series`,
		Tables: []string{"book_series"},
	},
	{
		Name:        "authors",
		Bundle:      "books",
		Description: "Read book authors",
		Columns:     []string{"id", "name", "sort_name"},
		SQL:         `SELECT id, name, sort_name FROM core.authors`,
		Tables:      []string{"authors"},
	},
	{
		Name:        "book_contributions",
		Bundle:      "books",
		Description: "Read who made each book",
		Columns: []string{
			"id",
			"author_id",
			"book_id",
			"series_id",
			"role",
			"language",
			"position",
		},
		SQL: `SELECT id, author_contributions AS author_id,
	book_contributions AS book_id,
	book_series_contributions AS series_id,
	role, language, "order" AS position
FROM core.book_contributions`,
		Tables: []string{"book_contributions"},
	},
	{
		Name:        "book_files",
		Bundle:      "books",
		Description: "Read which book files exist",
		Columns: []string{
			"id", "book_id", "slot", "format", "size", "filename", "added_at",
			"updated_at",
		},
		SQL: `SELECT id, book_media_files AS book_id, book_kind AS slot,
	quality AS format, size,
	replace(path, rtrim(path, replace(path, '/', '')), '') AS filename,
	create_time AS added_at, update_time AS updated_at
FROM core.media_files
WHERE book_kind IS NOT NULL AND missing_since IS NULL`,
		Tables: []string{"media_files"},
	},
	{
		Name:        "artists",
		Bundle:      "music",
		Description: musicDescription,
		Columns: []string{
			"id", "mbid", "name", "sort_name", "type", "origin", "since", "genre",
			"overview", "added_at", "updated_at", "overview_fr",
		},
		SQL: `SELECT id, mbid, name, sort_name, type, origin, since, genre,
	overview, create_time AS added_at, update_time AS updated_at,
	COALESCE(overview_fr, '') AS overview_fr
FROM core.artists`,
		Tables: []string{"artists"},
	},
	{
		Name:        "artist_members",
		Bundle:      "music",
		Description: musicDescription,
		Columns: []string{
			"id", "artist_id", "name", "mbid", "instruments", "from_year",
			"to_year", "ordinal",
		},
		SQL: `SELECT id, artist_members AS artist_id, name, mbid, instruments,
	from_year, to_year, ordinal
FROM core.artist_members`,
		Tables: []string{"artist_members"},
	},
	{
		Name:        "albums",
		Bundle:      "music",
		Description: musicDescription,
		Columns: []string{
			"id", "mbid", "artist_id", "title", "type", "release_date",
			"original_date", "edition_date", "genres", "label", "country",
			"status", "added_at", "updated_at",
		},
		SQL: `SELECT id, mbid, artist_albums AS artist_id, title, type,
	release_date, original_date, edition_date, genres, label, country, status,
	create_time AS added_at, update_time AS updated_at
FROM core.albums`,
		Tables: []string{"albums"},
	},
	{
		Name:        "album_artists",
		Bundle:      "music",
		Description: musicDescription,
		Columns:     []string{"album_id", "artist_id", "owner"},
		SQL: `SELECT id AS album_id, artist_albums AS artist_id, 1 AS owner
FROM core.albums
UNION ALL
SELECT album_id, artist_id, 0 AS owner FROM core.artist_co_albums`,
		Tables: []string{"albums", "artist_co_albums"},
	},
	{
		Name:        "tracks",
		Bundle:      "music",
		Description: musicDescription,
		Columns: []string{
			"id", "mbid", "album_id", "title", "disc", "position", "duration",
			"bonus", "artist_credit", "added_at", "updated_at",
		},
		SQL: `SELECT id, mbid, album_tracks AS album_id, title, disc, position,
	duration, bonus, artist_credit,
	create_time AS added_at, update_time AS updated_at
FROM core.tracks`,
		Tables: []string{"tracks"},
	},
	{
		Name:        "music_credits",
		Bundle:      "music",
		Description: musicDescription,
		Columns: []string{
			"id", "album_id", "track_id", "kind", "role", "name", "mbid",
			"instruments", "guest", "ordinal",
		},
		SQL: `SELECT id, album_credits AS album_id, track_credits AS track_id,
	kind, role, name, mbid, instruments, guest, ordinal
FROM core.music_credits`,
		Tables: []string{"music_credits"},
	},
	{
		Name:        "music_files",
		Bundle:      "music",
		Description: musicDescription,
		Columns: []string{
			"id", "track_id", "path", "format", "quality", "size", "duration",
			"bitrate", "sample_rate", "bit_depth", "channels", "missing", "added_at",
		},
		SQL: `SELECT id, track_media_files AS track_id, path, format, quality,
	size, duration_seconds AS duration, bitrate,
	NULL AS sample_rate, NULL AS bit_depth, audio_channels AS channels,
	missing_since IS NOT NULL AS missing, create_time AS added_at
FROM core.media_files
WHERE track_media_files IS NOT NULL`,
		Tables: []string{"media_files"},
	},
	{
		Name:        "genres",
		Bundle:      "music",
		Description: musicDescription,
		Columns:     []string{"name", "album_count"},
		SQL: `SELECT g.value AS name, count(DISTINCT a.id) AS album_count
FROM core.albums AS a, json_each(a.genres) AS g
WHERE a.genres IS NOT NULL
GROUP BY g.value`,
		Tables: []string{"albums"},
	},
}

const musicDescription = "Read the music library"

func ExtensionViews() []ExtensionView {
	out := make([]ExtensionView, len(extensionViews))
	copy(out, extensionViews)
	return out
}

func ExtensionViewByName(name string) (ExtensionView, bool) {
	for _, v := range extensionViews {
		if v.Name == name {
			return v, true
		}
	}
	return ExtensionView{}, false
}
