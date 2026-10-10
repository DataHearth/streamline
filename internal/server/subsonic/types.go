package subsonic

type child struct {
	ID          string `xml:"id,attr"                json:"id"`
	Parent      string `xml:"parent,attr"            json:"parent"`
	Title       string `xml:"title,attr"             json:"title"`
	Album       string `xml:"album,attr"             json:"album"`
	Artist      string `xml:"artist,attr"            json:"artist"`
	Track       uint16 `xml:"track,attr"             json:"track"`
	DiscNumber  uint8  `xml:"discNumber,attr"        json:"discNumber"`
	Year        uint16 `xml:"year,attr,omitempty"    json:"year,omitempty"`
	CoverArt    string `xml:"coverArt,attr"          json:"coverArt"`
	Size        int64  `xml:"size,attr"              json:"size"`
	ContentType string `xml:"contentType,attr"       json:"contentType"`
	Suffix      string `xml:"suffix,attr"            json:"suffix"`
	Duration    uint32 `xml:"duration,attr"          json:"duration"`
	BitRate     uint32 `xml:"bitRate,attr,omitempty" json:"bitRate,omitempty"`
	IsDir       bool   `xml:"isDir,attr"             json:"isDir"`
	AlbumID     string `xml:"albumId,attr"           json:"albumId"`
	ArtistID    string `xml:"artistId,attr"          json:"artistId"`
	Type        string `xml:"type,attr"              json:"type"`
}

type artistID3 struct {
	ID         string `xml:"id,attr"                 json:"id"`
	Name       string `xml:"name,attr"               json:"name"`
	AlbumCount int    `xml:"albumCount,attr"         json:"albumCount"`
	CoverArt   string `xml:"coverArt,attr,omitempty" json:"coverArt,omitempty"`
}

type albumID3 struct {
	ID        string `xml:"id,attr"             json:"id"`
	Name      string `xml:"name,attr"           json:"name"`
	Artist    string `xml:"artist,attr"         json:"artist"`
	ArtistID  string `xml:"artistId,attr"       json:"artistId"`
	CoverArt  string `xml:"coverArt,attr"       json:"coverArt"`
	SongCount int    `xml:"songCount,attr"      json:"songCount"`
	Duration  uint32 `xml:"duration,attr"       json:"duration"`
	Year      uint16 `xml:"year,attr,omitempty" json:"year,omitempty"`
	Created   string `xml:"created,attr"        json:"created"`
}

type artistWith struct {
	artistID3
	Album []albumID3 `xml:"album" json:"album"`
}

type albumWith struct {
	albumID3
	Song []child `xml:"song" json:"song"`
}

type index struct {
	Name   string      `xml:"name,attr" json:"name"`
	Artist []artistID3 `xml:"artist"    json:"artist"`
}

type indexes struct {
	IgnoredArticles string  `xml:"ignoredArticles,attr" json:"ignoredArticles"`
	Index           []index `xml:"index"                json:"index"`
}

type albumList2 struct {
	Album []albumID3 `xml:"album" json:"album"`
}

type search3 struct {
	Artist []artistID3 `xml:"artist" json:"artist"`
	Album  []albumID3  `xml:"album"  json:"album"`
	Song   []child     `xml:"song"   json:"song"`
}

type musicFolder struct {
	ID   int    `xml:"id,attr"   json:"id"`
	Name string `xml:"name,attr" json:"name"`
}

type musicFolders struct {
	MusicFolder []musicFolder `xml:"musicFolder" json:"musicFolder"`
}

type license struct {
	Valid bool `xml:"valid,attr" json:"valid"`
}
