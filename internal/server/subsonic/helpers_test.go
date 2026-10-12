package subsonic

import (
	"context"
	"crypto/md5" //nolint:gosec // the Subsonic token scheme is defined as md5(password+salt)
	"encoding/hex"
	"fmt"
	"maps"
	"net/url"

	g "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/user"
)

// signedAs is signedQuery for another account.
func signedAs(email, password string, extra url.Values) url.Values {
	const salt = "xyz789"
	sum := md5.Sum([]byte(password + salt))
	q := url.Values{
		"u": {email},
		"t": {hex.EncodeToString(sum[:])},
		"s": {salt},
		"f": {"json"},
	}
	maps.Copy(q, extra)
	return q
}

func (f *fixture) callAs(email, endpoint string, extra url.Values) map[string]any {
	g.GinkgoHelper()
	return f.callWith(endpoint, signedAs(email, "pw-"+email, extra))
}

// addUser creates an account whose Subsonic password is "pw-"+email.
func (f *fixture) addUser(email string, role user.Role) *ent.User {
	g.GinkgoHelper()
	u, err := f.client.User.Create().
		SetEmail(email).
		SetRole(role).
		SetSubsonicPassword("pw-" + email).
		Save(context.Background())
	Expect(err).NotTo(HaveOccurred())
	return u
}

// giveFile attaches a file at path to t, making it playable.
func (f *fixture) giveFile(t *ent.Track, path string) {
	g.GinkgoHelper()
	_, err := f.client.MediaFile.Create().
		SetPath(path).
		SetSize(500).
		SetFormat("flac").
		SetDurationSeconds(200).
		SetTrackID(t.ID).
		Save(context.Background())
	Expect(err).NotTo(HaveOccurred())
}

func okResp(resp map[string]any) map[string]any {
	g.GinkgoHelper()
	Expect(resp["status"]).To(Equal("ok"), fmt.Sprint(resp["error"]))
	return resp
}

func obj(m map[string]any, key string) map[string]any {
	g.GinkgoHelper()
	v, found := m[key].(map[string]any)
	Expect(found).To(BeTrue(), "no object %q in %v", key, m)
	return v
}

func list(m map[string]any, key string) []map[string]any {
	g.GinkgoHelper()
	raw, _ := m[key].([]any)
	out := make([]map[string]any, 0, len(raw))
	for _, v := range raw {
		out = append(out, v.(map[string]any))
	}
	return out
}

func ids(items []map[string]any) []string {
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, it["id"].(string))
	}
	return out
}

func tr(t *ent.Track) string  { return trackID(t.ID) }
func al(a *ent.Album) string  { return albumID(a.ID) }
func ar(a *ent.Artist) string { return artistID(a.ID) }

// addArtist seeds an artist with one album and no tracks.
func (f *fixture) addArtist(mbid, name, genre string) *ent.Artist {
	g.GinkgoHelper()
	ctx := context.Background()
	a, err := f.client.Artist.Create().
		SetMbid(mbid).
		SetName(name).
		SetGenre(genre).
		Save(ctx)
	Expect(err).NotTo(HaveOccurred())
	return a
}
