package web

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"net/http"
	"regexp"
	"strings"

	webassets "github.com/datahearth/streamline/web"
)

// shellRef is a stylesheet or script a shell links. Fonts stay unversioned:
// the CSS loads them by their bare URL, and a versioned preload would fetch
// each one twice.
var shellRef = regexp.MustCompile(
	`((?:href|src)="/static/((?:css|dist|js)/[^"?]+))"`,
)

type staticAssets struct {
	// versions holds each embedded file's content hash, by its path below
	// static/.
	versions map[string]string
	spaShell []byte
	apiDocs  []byte
}

var static = loadStatic()

// loadStatic panics on an unreadable file: the assets are embedded, so that is
// a broken build.
func loadStatic() staticAssets {
	versions := map[string]string{}
	err := fs.WalkDir(
		webassets.Assets,
		"static",
		func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			raw, err := webassets.Assets.ReadFile(p)
			if err != nil {
				return err
			}
			sum := sha256.Sum256(raw)
			versions[strings.TrimPrefix(p, "static/")] = hex.EncodeToString(sum[:8])
			return nil
		},
	)
	if err != nil {
		panic("hash the embedded static assets: " + err.Error())
	}
	versioned := func(shell []byte) []byte {
		return shellRef.ReplaceAllFunc(shell, func(ref []byte) []byte {
			m := shellRef.FindSubmatch(ref)
			v, ok := versions[string(m[2])]
			if !ok {
				return ref
			}
			return []byte(string(m[1]) + "?v=" + v + `"`)
		})
	}
	return staticAssets{
		versions: versions,
		spaShell: versioned(webassets.SPAShell),
		apiDocs:  versioned(webassets.APIDocsShell),
	}
}

// staticFiles serves the embedded assets. The shells link their stylesheets
// and scripts with `?v=` set to the file's hash, and that URL never changes
// content, so it is cached for good; any other request revalidates against
// the ETag, so a new release reaches the browser on its next load.
func staticFiles(files http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if v, ok := static.versions[r.URL.Path]; ok {
			w.Header().Set("ETag", `"`+v+`"`)
			if r.URL.Query().Get("v") == v {
				w.Header().
					Set("Cache-Control", "public, max-age=31536000, immutable")
			} else {
				w.Header().Set("Cache-Control", "no-cache")
			}
		}
		files.ServeHTTP(w, r)
	})
}
