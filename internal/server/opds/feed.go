package opds

import (
	"encoding/xml"
	"net/http"
	"time"
)

const (
	navigationType  = "application/atom+xml;profile=opds-catalog;kind=navigation"
	acquisitionType = "application/atom+xml;profile=opds-catalog;kind=acquisition"
)

type feedKind int

const (
	kindNavigation feedKind = iota
	kindAcquisition
)

func (k feedKind) mediaType() string {
	if k == kindAcquisition {
		return acquisitionType
	}
	return navigationType
}

type feed struct {
	XMLName xml.Name `xml:"http://www.w3.org/2005/Atom feed"`
	ID      string   `xml:"id"`
	Title   string   `xml:"title"`
	Updated atomTime `xml:"updated"`
	Links   []link   `xml:"link"`
	Entries []entry  `xml:"entry"`
}

type entry struct {
	ID      string       `xml:"id"`
	Title   string       `xml:"title"`
	Updated atomTime     `xml:"updated"`
	Authors []atomAuthor `xml:"author"`
	Content *content     `xml:"content,omitempty"`
	Links   []link       `xml:"link"`
}

type atomAuthor struct {
	Name string `xml:"name"`
}

type content struct {
	Type string `xml:"type,attr"`
	Text string `xml:",chardata"`
}

type link struct {
	Rel  string `xml:"rel,attr"`
	Href string `xml:"href,attr"`
	Type string `xml:"type,attr,omitempty"`
}

type atomTime time.Time

func (t atomTime) MarshalXML(e *xml.Encoder, start xml.StartElement) error {
	v := time.Time(t)
	if v.IsZero() {
		v = time.Unix(0, 0)
	}
	return e.EncodeElement(v.UTC().Format(time.RFC3339), start)
}

func selfLink(href string, kind feedKind) link {
	return link{Rel: "self", Href: href, Type: kind.mediaType()}
}

func writeFeed(w http.ResponseWriter, kind feedKind, f *feed) error {
	w.Header().Set("Content-Type", kind.mediaType())
	if _, err := w.Write([]byte(xml.Header)); err != nil {
		return err
	}
	return xml.NewEncoder(w).Encode(f)
}
