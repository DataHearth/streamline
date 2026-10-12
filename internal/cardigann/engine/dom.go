package engine

import (
	"encoding/xml"
	"errors"
	"io"
	"net/url"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"golang.org/x/net/html"
)

// parseDocument parses an HTML page, or an XML one into the same node tree so
// the selectors run over both alike. Going through the HTML parser would
// mangle XML: <link> is a void element there, and every RSS item's link
// would come out empty.
func parseDocument(body string, isXML bool) (*goquery.Document, error) {
	if !isXML {
		return goquery.NewDocumentFromReader(strings.NewReader(body))
	}
	root := &html.Node{Type: html.DocumentNode}
	cur := root
	d := xml.NewDecoder(strings.NewReader(body))
	d.Strict = false
	for {
		tok, err := d.RawToken()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			n := &html.Node{Type: html.ElementNode, Data: xmlName(t.Name)}
			for _, a := range t.Attr {
				n.Attr = append(n.Attr, html.Attribute{
					Key: xmlName(a.Name), Val: a.Value,
				})
			}
			cur.AppendChild(n)
			cur = n
		case xml.EndElement:
			if cur.Parent != nil {
				cur = cur.Parent
			}
		case xml.CharData:
			cur.AppendChild(&html.Node{Type: html.TextNode, Data: string(t)})
		}
	}
	return goquery.NewDocumentFromNode(root), nil
}

// xmlName keeps a prefixed name as written ("torznab:attr"), the way
// AngleSharp's XML DOM names it; CSS reaches it as torznab\:attr. RawToken
// leaves the prefix in Space rather than resolving it to a namespace.
func xmlName(n xml.Name) string {
	if n.Space == "" {
		return n.Local
	}
	return n.Space + ":" + n.Local
}

// querySelectorAll runs a selector from sel, a leading ":root" starting it
// from the document instead, as Prowlarr's QuerySelector special-cases it
// for AngleSharp.
func querySelectorAll(sel *goquery.Selection, selector string) *goquery.Selection {
	if rest, ok := strings.CutPrefix(strings.TrimSpace(selector), ":root"); ok {
		top := sel
		for top.Parent().Length() > 0 {
			top = top.Parent()
		}
		if strings.TrimSpace(rest) == "" {
			return top
		}
		return top.Find(rest)
	}
	return sel.Find(selector)
}

// matchOrFind is the element itself when it matches, else the first
// descendant that does.
func matchOrFind(sel *goquery.Selection, selector string) *goquery.Selection {
	if sel.Is(selector) {
		return sel
	}
	return querySelectorAll(sel, selector).First()
}

// publicTrackers are the announce URLs Prowlarr's MagnetLinkBuilder adds to a
// magnet it builds from a bare info hash.
var publicTrackers = []string{
	"udp://tracker.opentrackr.org:1337/announce",
	"udp://open.demonii.com:1337/announce",
	"udp://open.stealth.si:80/announce",
	"udp://tracker.torrent.eu.org:451/announce",
	"udp://exodus.desync.com:6969/announce",
	"udp://tracker.moeking.me:6969/announce",
	"udp://explodie.org:6969/announce",
	"udp://tracker.dler.org:6969/announce",
}

func magnetLink(hash, title string) string {
	var b strings.Builder
	b.WriteString("magnet:?xt=urn:btih:")
	b.WriteString(strings.TrimSpace(hash))
	if title != "" {
		b.WriteString("&dn=")
		b.WriteString(url.QueryEscape(title))
	}
	for _, t := range publicTrackers {
		b.WriteString("&tr=")
		b.WriteString(url.QueryEscape(t))
	}
	return b.String()
}
