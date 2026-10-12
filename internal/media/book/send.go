package book

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/mediafile"
	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/mailer"
	"github.com/datahearth/streamline/internal/observability"
	"github.com/datahearth/streamline/internal/otelx"
)

const (
	maxEmailAttachments = 25
	bytesPerMB          = 1 << 20
	reasonMissingFile   = "file is missing on disk"
)

var (
	ErrSMTPNotConfigured = errors.New("sending is not configured")
	ErrNoDeviceEmail     = errors.New("no device email set on your account")
	ErrNotSendable       = errors.New("not sendable")
)

// Sendable picks the ebook file an email can carry: the EPUB, else the PDF.
// The reason is empty exactly when a file is returned.
func Sendable(files []*ent.MediaFile, maxBytes int64) (*ent.MediaFile, string) {
	var epub, pdf, other *ent.MediaFile
	for _, f := range files {
		if f.BookKind != mediafile.BookKindEbook {
			continue
		}
		switch f.Quality {
		case "EPUB":
			if epub == nil {
				epub = f
			}
		case "PDF":
			if pdf == nil {
				pdf = f
			}
		default:
			if other == nil {
				other = f
			}
		}
	}
	pick := epub
	if pick == nil {
		pick = pdf
	}
	if pick == nil {
		if other == nil {
			return nil, "no ebook file"
		}
		format := other.Quality
		if format == "" {
			format = "this format"
		}
		return nil, format + " isn't accepted by email; use OPDS"
	}
	info, err := os.Stat(pick.Path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, reasonMissingFile
	}
	if err != nil {
		return nil, "file can't be read: " + err.Error()
	}
	if info.Size() > maxBytes {
		return nil, fmt.Sprintf("file is %.1f MB, over the %d MB limit",
			float64(info.Size())/bytesPerMB, maxBytes/bytesPerMB)
	}
	return pick, ""
}

type SendState struct {
	OK     bool
	Reason string
}

// SendState folds the SMTP, address and file checks into the one answer the
// send button reads.
func (s *Service) SendState(
	ctx context.Context,
	userID uint32,
	b *ent.Book,
) SendState {
	if _, err := s.deviceEmail(ctx, userID); err != nil {
		return SendState{Reason: err.Error()}
	}
	if _, reason := Sendable(
		b.Edges.MediaFiles, config.Get().SMTP.MaxMessageBytes(),
	); reason != "" {
		return SendState{Reason: reason}
	}
	return SendState{OK: true}
}

func (s *Service) deviceEmail(ctx context.Context, userID uint32) (string, error) {
	if s.mail == nil {
		return "", ErrSMTPNotConfigured
	}
	u, err := s.db.FindUserByID(ctx, userID)
	if err != nil {
		return "", err
	}
	if u.DeviceEmail == "" {
		return "", ErrNoDeviceEmail
	}
	return u.DeviceEmail, nil
}

type sendItem struct {
	bookID uint32
	title  string
	path   string
	name   string
	size   int64
}

func newSendItem(b *ent.Book, f *ent.MediaFile) (sendItem, error) {
	info, err := os.Stat(f.Path)
	if err != nil {
		return sendItem{}, err
	}
	return sendItem{
		bookID: b.ID,
		title:  b.Title,
		path:   f.Path,
		name:   attachmentName(b.Title, f.Quality),
		size:   info.Size(),
	}, nil
}

var pathSeparators = strings.NewReplacer("/", " ", "\\", " ")

func attachmentName(title, format string) string {
	name := strings.TrimSpace(pathSeparators.Replace(title))
	if name == "" {
		name = "book"
	}
	return name + "." + strings.ToLower(format)
}

// packEmails places each item, in order, into the first email that still has
// room for it.
func packEmails(items []sendItem, maxBytes int64, maxAttachments int) [][]sendItem {
	var emails [][]sendItem
	var sizes []int64
	for _, it := range items {
		placed := false
		for i := range emails {
			if len(emails[i]) < maxAttachments && sizes[i]+it.size <= maxBytes {
				emails[i] = append(emails[i], it)
				sizes[i] += it.size
				placed = true
				break
			}
		}
		if !placed {
			emails = append(emails, []sendItem{it})
			sizes = append(sizes, it.size)
		}
	}
	return emails
}

func (s *Service) sendEmail(
	ctx context.Context,
	to, subject string,
	items []sendItem,
) error {
	atts := make([]mailer.Attachment, 0, len(items))
	defer func() { closeAll(ctx, atts) }()
	for _, it := range items {
		f, err := os.Open(it.path)
		if err != nil {
			return fmt.Errorf("open %s: %w", it.name, err)
		}
		atts = append(atts, mailer.Attachment{Name: it.name, Body: f, Size: it.size})
	}
	return s.mail.Send(ctx, to, subject, atts)
}

func closeAll(ctx context.Context, atts []mailer.Attachment) {
	for _, a := range atts {
		if c, ok := a.Body.(io.Closer); ok {
			if err := c.Close(); err != nil {
				slog.DebugContext(ctx, "Close attachment failed", "error", err)
			}
		}
	}
}

// SendBook emails the book's ebook file to the user's device address.
func (s *Service) SendBook(ctx context.Context, userID, bookID uint32) error {
	ctx, span := tracer.Start(ctx, "book.send",
		trace.WithAttributes(attribute.Int("book.id", int(bookID))))
	defer span.End()

	to, err := s.deviceEmail(ctx, userID)
	if err != nil {
		return otelx.RecordSpanError(span, err)
	}
	b, err := s.GetBook(ctx, bookID)
	if err != nil {
		return otelx.RecordSpanError(span, err)
	}
	file, reason := Sendable(b.Edges.MediaFiles, config.Get().SMTP.MaxMessageBytes())
	if reason != "" {
		err := fmt.Errorf("%w: %s", ErrNotSendable, reason)
		return otelx.RecordSpanError(span, err)
	}
	item, err := newSendItem(b, file)
	if err != nil {
		return otelx.RecordSpanError(span, err)
	}
	return otelx.RecordSpanError(
		span,
		s.sendEmail(ctx, to, b.Title, []sendItem{item}),
	)
}

type SkippedVolume struct {
	BookID uint32
	Title  string
	Reason string
}

type SeriesSendPlan struct {
	Emails  int
	Volumes int
	Skipped []SkippedVolume
}

// SendSeries plans the emails for a series' hydrated volumes, returns the plan
// and sends in the background. A failed email is logged and the rest go out.
func (s *Service) SendSeries(
	ctx context.Context,
	userID, seriesID uint32,
) (SeriesSendPlan, error) {
	ctx, span := tracer.Start(ctx, "book.send_series",
		trace.WithAttributes(attribute.Int("series.id", int(seriesID))))
	defer span.End()

	to, err := s.deviceEmail(ctx, userID)
	if err != nil {
		return SeriesSendPlan{}, otelx.RecordSpanError(span, err)
	}
	series, err := s.GetSeries(ctx, seriesID)
	if err != nil {
		return SeriesSendPlan{}, otelx.RecordSpanError(span, err)
	}

	maxBytes := config.Get().SMTP.MaxMessageBytes()
	var items []sendItem
	var plan SeriesSendPlan
	for _, v := range series.Edges.Volumes {
		if v.LastRefreshedAt == nil {
			continue
		}
		file, reason := Sendable(v.Edges.MediaFiles, maxBytes)
		var item sendItem
		if reason == "" {
			var statErr error
			item, statErr = newSendItem(v, file)
			if statErr != nil {
				reason = reasonMissingFile
			}
		}
		if reason != "" {
			plan.Skipped = append(plan.Skipped,
				SkippedVolume{BookID: v.ID, Title: v.Title, Reason: reason})
			continue
		}
		items = append(items, item)
	}
	if len(items) == 0 {
		return SeriesSendPlan{}, otelx.RecordSpanError(span,
			fmt.Errorf("%w: no volume is sendable", ErrNotSendable))
	}

	emails := packEmails(items, maxBytes, maxEmailAttachments)
	plan.Emails = len(emails)
	plan.Volumes = len(items)
	span.SetAttributes(
		attribute.Int("send.emails", plan.Emails),
		attribute.Int("send.volumes", plan.Volumes),
		attribute.Int("send.skipped", len(plan.Skipped)),
	)

	bg := context.WithoutCancel(ctx)
	go func() {
		defer observability.RecoverPanic(bg, "book.send_series", func() {})
		for _, email := range emails {
			titles := make([]string, len(email))
			for i, it := range email {
				titles[i] = it.title
			}
			subject := titles[0]
			if len(email) > 1 {
				subject = series.Title
			}
			if err := s.sendEmail(bg, to, subject, email); err != nil {
				slog.WarnContext(bg, "Series email failed",
					"titles", titles, "error", err)
			}
		}
	}()
	return plan, nil
}
