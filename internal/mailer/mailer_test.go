package mailer

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/mail"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/testutil/configtest"
)

func useServer(f *fakeSMTP, tlsMode string) {
	GinkgoHelper()
	configtest.Setup(map[string]any{"smtp": map[string]any{
		"host":     "127.0.0.1",
		"port":     f.port(),
		"tls":      tlsMode,
		"username": "streamline",
		"password": "secret",
		"from":     "streamline@example.com",
	}})
}

func att(name string, body []byte) Attachment {
	return Attachment{
		Name: name,
		Body: bytes.NewReader(body),
		Size: int64(len(body)),
	}
}

type part struct {
	fileName string
	encoding string
	body     []byte
}

func parts(raw []byte) []part {
	GinkgoHelper()
	msg, err := mail.ReadMessage(bytes.NewReader(raw))
	Expect(err).NotTo(HaveOccurred())
	_, params, err := mime.ParseMediaType(msg.Header.Get("Content-Type"))
	Expect(err).NotTo(HaveOccurred())
	mr := multipart.NewReader(msg.Body, params["boundary"])
	var out []part
	for {
		p, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			return out
		}
		Expect(err).NotTo(HaveOccurred())
		body, err := io.ReadAll(p)
		Expect(err).NotTo(HaveOccurred())
		out = append(
			out,
			part{
				fileName: p.FileName(),
				encoding: p.Header.Get("Content-Transfer-Encoding"),
				body:     body,
			},
		)
	}
}

var _ = Describe("Mailer", Label("unit", "mailer"), func() {
	ctx := context.Background()

	It("delivers attachments byte-identical", func() {
		f := newFakeSMTP(false, false)
		useServer(f, "none")
		epub := []byte("PK\x03\x04 pretend epub")
		blob := make([]byte, 300*1024)
		_, err := rand.Read(blob)
		Expect(err).NotTo(HaveOccurred())

		Expect(
			New().Send(ctx, "me@kindle.com", "Books", []Attachment{att("a.epub", epub), att("b.bin", blob)}),
		).To(Succeed())

		ps := parts(f.captured())
		Expect(ps).To(HaveLen(3))
		for i, want := range [][]byte{epub, blob} {
			p := ps[i+1]
			Expect(p.encoding).To(Equal("base64"))
			enc := p.body
			for line := range strings.SplitSeq(strings.TrimSpace(string(enc)), "\n") {
				Expect(
					len(strings.TrimSuffix(line, "\r")),
				).To(BeNumerically("<=", 76))
			}
			got, err := base64.StdEncoding.DecodeString(
				strings.NewReplacer("\r", "", "\n", "").Replace(string(enc)),
			)
			Expect(err).NotTo(HaveOccurred())
			Expect(got).To(Equal(want))
		}
	})

	It("encodes non-ASCII filenames", func() {
		f := newFakeSMTP(false, false)
		useServer(f, "none")
		names := []string{"Les Misérables.epub", "進撃の巨人 01.epub", "a:b=c.epub"}
		atts := make([]Attachment, 0, len(names))
		for _, n := range names {
			atts = append(atts, att(n, []byte("x")))
		}

		Expect(New().Send(ctx, "me@kindle.com", "Books", atts)).To(Succeed())

		ps := parts(f.captured())
		Expect(ps).To(HaveLen(len(names) + 1))
		for i, n := range names {
			Expect(ps[i+1].fileName).To(Equal(n))
		}
	})

	It("upgrades with STARTTLS before AUTH", func() {
		f := newFakeSMTP(true, false)
		useServer(f, "starttls")

		Expect(
			New().Send(ctx, "me@kindle.com", "Books", []Attachment{att("a.epub", []byte("x"))}),
		).To(Succeed())

		seen, onTLS := f.auth()
		Expect(seen).To(BeTrue())
		Expect(onTLS).To(BeTrue())
	})

	It("refuses to send when starttls is required but not offered", func() {
		f := newFakeSMTP(false, false)
		useServer(f, "starttls")

		err := New().Send(ctx, "me@kindle.com", "Books", []Attachment{att("a.epub", []byte("x"))})

		Expect(err).To(MatchError(ErrStartTLSUnsupported))
		seen, _ := f.auth()
		Expect(seen).To(BeFalse())
		Expect(f.captured()).To(BeEmpty())
	})

	It("surfaces the server's rejection text", func() {
		f := newFakeSMTP(false, true)
		useServer(f, "none")

		err := New().Send(ctx, "me@kindle.com", "Books", []Attachment{att("a.epub", []byte("x"))})

		Expect(err).To(MatchError(ContainSubstring("no such user")))
	})
})
