package mailer

import (
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/textproto"
	"net/url"
	"strings"
	"time"
)

const base64LineLen = 76

func writeMessage(w io.Writer, from, to, subject string, atts []Attachment) error {
	mw := multipart.NewWriter(w)
	header := strings.Join([]string{
		"From: " + from,
		"To: " + to,
		"Subject: " + mime.QEncoding.Encode("utf-8", subject),
		"Date: " + time.Now().Format(time.RFC1123Z),
		"MIME-Version: 1.0",
		"Content-Type: multipart/mixed; boundary=" + mw.Boundary(),
	}, "\r\n") + "\r\n\r\n"
	if _, err := io.WriteString(w, header); err != nil {
		return err
	}

	text, err := mw.CreatePart(textproto.MIMEHeader{
		"Content-Type": {"text/plain; charset=utf-8"},
	})
	if err != nil {
		return err
	}
	if _, err := io.WriteString(text, "Sent by Streamline.\r\n"); err != nil {
		return err
	}

	for _, a := range atts {
		part, err := mw.CreatePart(textproto.MIMEHeader{
			"Content-Type":              {"application/octet-stream"},
			"Content-Transfer-Encoding": {"base64"},
			"Content-Disposition": {
				"attachment; filename*=UTF-8''" + encodeFilename(a.Name),
			},
		})
		if err != nil {
			return err
		}
		if err := writeBase64(part, a.Body); err != nil {
			return fmt.Errorf("attachment %q: %w", a.Name, err)
		}
	}
	return mw.Close()
}

// encodeFilename escapes everything outside RFC 2231's attribute-char set;
// url.PathEscape leaves tspecials such as ':' and '=' bare.
func encodeFilename(name string) string {
	return strings.ReplaceAll(url.QueryEscape(name), "+", "%20")
}

func writeBase64(w io.Writer, r io.Reader) error {
	lw := &lineWriter{w: w}
	enc := base64.NewEncoder(base64.StdEncoding, lw)
	if _, err := io.Copy(enc, r); err != nil {
		return err
	}
	if err := enc.Close(); err != nil {
		return err
	}
	return lw.finish()
}

type lineWriter struct {
	w io.Writer
	n int
}

func (l *lineWriter) Write(p []byte) (int, error) {
	written := 0
	for len(p) > 0 {
		chunk := min(base64LineLen-l.n, len(p))
		if _, err := l.w.Write(p[:chunk]); err != nil {
			return written, err
		}
		written += chunk
		p = p[chunk:]
		l.n += chunk
		if l.n == base64LineLen {
			if _, err := io.WriteString(l.w, "\r\n"); err != nil {
				return written, err
			}
			l.n = 0
		}
	}
	return written, nil
}

func (l *lineWriter) finish() error {
	if l.n == 0 {
		return nil
	}
	_, err := io.WriteString(l.w, "\r\n")
	return err
}
