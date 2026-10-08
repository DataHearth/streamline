package ebookmeta

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"

	g "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const testOPF = `<?xml version="1.0" encoding="utf-8"?>
<package xmlns="http://www.idpf.org/2007/opf" xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:opf="http://www.idpf.org/2007/opf" version="2.0">
  <metadata>
    <dc:title>Elantris</dc:title>
    <dc:creator opf:role="aut">Brandon Sanderson</dc:creator>
    <dc:identifier opf:scheme="ISBN">9780765311771</dc:identifier>
    <dc:date>2005-04-21</dc:date>
  </metadata>
</package>`

const containerXML = `<?xml version="1.0"?>` +
	`<container xmlns="urn:oasis:names:tc:opendocument:xmlns:container" version="1.0">` +
	`<rootfiles><rootfile full-path="content.opf" media-type="application/oebps-package+xml"/></rootfiles>` +
	`</container>`

func writeEpub(path, opf string) {
	g.GinkgoHelper()
	f, err := os.Create(path)
	Expect(err).NotTo(HaveOccurred())
	zw := zip.NewWriter(f)
	w, err := zw.Create("META-INF/container.xml")
	Expect(err).NotTo(HaveOccurred())
	_, err = w.Write([]byte(containerXML))
	Expect(err).NotTo(HaveOccurred())
	w, err = zw.Create("content.opf")
	Expect(err).NotTo(HaveOccurred())
	_, err = w.Write([]byte(opf))
	Expect(err).NotTo(HaveOccurred())
	Expect(zw.Close()).To(Succeed())
	Expect(f.Close()).To(Succeed())
}

var _ = g.Describe("ReadEpub", g.Label("unit"), func() {
	g.It("extracts title, author, ISBN and year from the embedded OPF", func() {
		dir := g.GinkgoT().TempDir()
		path := filepath.Join(dir, "elantris.epub")
		writeEpub(path, testOPF)
		info, err := ReadEpub(path)
		Expect(err).NotTo(HaveOccurred())
		Expect(info.Title).To(Equal("Elantris"))
		Expect(info.Author).To(Equal("Brandon Sanderson"))
		Expect(info.ISBN).To(Equal("9780765311771"))
		Expect(info.Year).To(Equal(uint16(2005)))
	})

	g.It("returns zero values, not an error, for an epub without metadata", func() {
		dir := g.GinkgoT().TempDir()
		path := filepath.Join(dir, "bare.epub")
		writeEpub(path, `<?xml version="1.0"?>`+
			`<package xmlns="http://www.idpf.org/2007/opf"><metadata/></package>`)
		info, err := ReadEpub(path)
		Expect(err).NotTo(HaveOccurred())
		Expect(info.Title).To(BeEmpty())
	})

	g.It("errors on a non-zip file", func() {
		dir := g.GinkgoT().TempDir()
		path := filepath.Join(dir, "broken.epub")
		Expect(os.WriteFile(path, []byte("not a zip"), 0o644)).To(Succeed())
		_, err := ReadEpub(path)
		Expect(err).To(HaveOccurred())
	})
})

var _ = g.Describe("ReadSidecar", g.Label("unit"), func() {
	g.It("parses a Calibre metadata.opf", func() {
		dir := g.GinkgoT().TempDir()
		path := filepath.Join(dir, "metadata.opf")
		Expect(os.WriteFile(path, []byte(testOPF), 0o644)).To(Succeed())
		info, err := ReadSidecar(path)
		Expect(err).NotTo(HaveOccurred())
		Expect(info.ISBN).To(Equal("9780765311771"))
	})

	g.It("extracts urn:isbn identifiers without a scheme attr", func() {
		opf := strings.Replace(testOPF,
			`<dc:identifier opf:scheme="ISBN">9780765311771</dc:identifier>`,
			`<dc:identifier>urn:isbn:9780765311771</dc:identifier>`, 1)
		dir := g.GinkgoT().TempDir()
		path := filepath.Join(dir, "metadata.opf")
		Expect(os.WriteFile(path, []byte(opf), 0o644)).To(Succeed())
		info, err := ReadSidecar(path)
		Expect(err).NotTo(HaveOccurred())
		Expect(info.ISBN).To(Equal("9780765311771"))
	})
})
