package audiotags

import (
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/bogem/id3v2/v2"
	"github.com/go-flac/flacvorbis/v2"
	flac "github.com/go-flac/go-flac/v2"
)

var ErrUnsupportedFormat = errors.New(
	"audiotags: unsupported format for tag writing",
)

type WriteTags struct {
	Artist, AlbumArtist, Album, Title           string
	Track                                       uint16
	Disc                                        uint8
	Year                                        uint16
	MBArtistID, MBReleaseGroupID, MBRecordingID string
}

// Write replaces the file's primary tags in place. MP3 via ID3v2.4 frames
// (TXXX frames for MusicBrainz IDs, Picard-compatible descriptors
// "MusicBrainz Artist Id" / "MusicBrainz Release Group Id" /
// "MusicBrainz Track Id"); FLAC via vorbis comments (MUSICBRAINZ_ARTISTID /
// MUSICBRAINZ_RELEASEGROUPID / MUSICBRAINZ_TRACKID, TRACKNUMBER, DISCNUMBER,
// DATE). Other formats: ErrUnsupportedFormat, and the caller imports without
// writing and logs.
func Write(path string, t WriteTags) error {
	var err error
	switch strings.ToLower(filepath.Ext(path)) {
	case ".mp3":
		err = writeMP3(path, t)
	case ".flac":
		err = writeFLAC(path, t)
	default:
		return ErrUnsupportedFormat
	}
	if err != nil {
		return fmt.Errorf("audiotags: write %s: %w", filepath.Base(path), err)
	}
	return nil
}

func writeMP3(path string, t WriteTags) error {
	tg, err := id3v2.Open(path, id3v2.Options{Parse: true})
	if err != nil {
		return err
	}
	// An existing v2.3 tag would keep its version, and UTF-8 frames are only
	// valid in v2.4.
	tg.SetVersion(4)
	tg.DeleteAllFrames()
	tg.SetDefaultEncoding(id3v2.EncodingUTF8)

	if t.Artist != "" {
		tg.SetArtist(t.Artist)
	}
	if t.Album != "" {
		tg.SetAlbum(t.Album)
	}
	if t.Title != "" {
		tg.SetTitle(t.Title)
	}
	text := map[string]string{
		"TPE2": t.AlbumArtist,
		"TRCK": numStr(uint64(t.Track)),
		"TPOS": numStr(uint64(t.Disc)),
		"TDRC": numStr(uint64(t.Year)),
	}
	for id, v := range text {
		if v != "" {
			tg.AddTextFrame(id, tg.DefaultEncoding(), v)
		}
	}
	for desc, v := range map[string]string{
		"MusicBrainz Artist Id":        t.MBArtistID,
		"MusicBrainz Release Group Id": t.MBReleaseGroupID,
		"MusicBrainz Track Id":         t.MBRecordingID,
	} {
		if v != "" {
			tg.AddUserDefinedTextFrame(id3v2.UserDefinedTextFrame{
				Encoding:    id3v2.EncodingUTF8,
				Description: desc,
				Value:       v,
			})
		}
	}

	saveErr := tg.Save()
	return errors.Join(saveErr, tg.Close())
}

func writeFLAC(path string, t WriteTags) error {
	f, err := flac.ParseFile(path)
	if err != nil {
		return err
	}
	// Save closes the file-backed stream itself, so this Close is a no-op
	// whose error carries nothing.
	defer f.Close()

	cmt := flacvorbis.New()
	idx := -1
	for i, blk := range f.Meta {
		if blk.Type != flac.VorbisComment {
			continue
		}
		idx = i
		old, err := flacvorbis.ParseFromMetaDataBlock(*blk)
		if err != nil {
			return err
		}
		cmt.Vendor = old.Vendor
		break
	}

	fields := []struct{ key, val string }{
		{"ARTIST", t.Artist},
		{"ALBUMARTIST", t.AlbumArtist},
		{"ALBUM", t.Album},
		{"TITLE", t.Title},
		{"TRACKNUMBER", numStr(uint64(t.Track))},
		{"DISCNUMBER", numStr(uint64(t.Disc))},
		{"DATE", numStr(uint64(t.Year))},
		{"MUSICBRAINZ_ARTISTID", t.MBArtistID},
		{"MUSICBRAINZ_RELEASEGROUPID", t.MBReleaseGroupID},
		{"MUSICBRAINZ_TRACKID", t.MBRecordingID},
	}
	for _, fl := range fields {
		if fl.val == "" {
			continue
		}
		if err := cmt.Add(fl.key, fl.val); err != nil {
			return err
		}
	}

	blk := cmt.Marshal()
	if idx >= 0 {
		f.Meta[idx] = &blk
	} else {
		f.Meta = append(f.Meta, &blk)
	}
	return f.Save(path)
}

func numStr(n uint64) string {
	if n == 0 {
		return ""
	}
	return strconv.FormatUint(n, 10)
}
