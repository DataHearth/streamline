// Package mailer sends mail with attachments over SMTP. It is a leaf: stdlib
// and the config singleton only.
package mailer

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/smtp"
	"strconv"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/otelx"
)

const (
	defaultTimeout = 2 * time.Minute
	helloName      = "localhost"
)

var (
	tracer = otel.Tracer("github.com/datahearth/streamline/internal/mailer")
	meter  = otel.Meter("github.com/datahearth/streamline/internal/mailer")

	sends metric.Int64Counter

	ErrStartTLSUnsupported = errors.New("smtp server does not offer STARTTLS")
)

func init() {
	sends = otelx.Must(meter.Int64Counter(
		"streamline.mailer.sends",
		metric.WithDescription("Outgoing emails by outcome"),
	))
	sends.Add(context.Background(), 0)
}

type Attachment struct {
	Name string
	Body io.Reader
	Size int64
}

type Sender interface {
	Send(ctx context.Context, to, subject string, atts []Attachment) error
}

type Mailer struct{}

func New() *Mailer { return &Mailer{} }

func (*Mailer) Send(
	ctx context.Context,
	to, subject string,
	atts []Attachment,
) error {
	ctx, span := tracer.Start(ctx, "mailer.send")
	defer span.End()

	var total int64
	for _, a := range atts {
		total += a.Size
	}
	span.SetAttributes(
		attribute.Int("mailer.attachments", len(atts)),
		attribute.Int64("mailer.bytes", total),
	)

	err := send(ctx, config.Get().SMTP, to, subject, atts)
	outcome := "ok"
	if err != nil {
		outcome = "error"
	}
	sends.Add(ctx, 1, metric.WithAttributes(attribute.String("outcome", outcome)))
	return otelx.RecordSpanError(span, err)
}

func send(
	ctx context.Context,
	cfg config.SMTPConfig,
	to, subject string,
	atts []Attachment,
) error {
	addr := net.JoinHostPort(cfg.Host, strconv.Itoa(int(cfg.Port)))
	tlsCfg := &tls.Config{ServerName: cfg.Host, MinVersion: tls.VersionTLS12}

	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(defaultTimeout)
	}

	var conn net.Conn
	var err error
	if cfg.TLS == "implicit" {
		d := tls.Dialer{NetDialer: &net.Dialer{}, Config: tlsCfg}
		conn, err = d.DialContext(ctx, "tcp", addr)
	} else {
		var d net.Dialer
		conn, err = d.DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return fmt.Errorf("connect to %s: %w", addr, err)
	}
	if err := conn.SetDeadline(deadline); err != nil {
		return errors.Join(err, conn.Close())
	}

	c, err := smtp.NewClient(conn, cfg.Host)
	if err != nil {
		return errors.Join(fmt.Errorf("smtp greeting: %w", err), conn.Close())
	}
	defer c.Close()

	if err := c.Hello(helloName); err != nil {
		return fmt.Errorf("smtp hello: %w", err)
	}
	if cfg.TLS == "starttls" {
		if ok, _ := c.Extension("STARTTLS"); !ok {
			return ErrStartTLSUnsupported
		}
		if err := c.StartTLS(tlsCfg); err != nil {
			return fmt.Errorf("smtp starttls: %w", err)
		}
	}
	if cfg.Username != "" {
		password := config.SecretValue(cfg.Password, cfg.PasswordFile)
		if err := c.Auth(
			smtp.PlainAuth("", cfg.Username, password, cfg.Host),
		); err != nil {
			return fmt.Errorf("smtp auth: %w", err)
		}
	}
	if err := c.Mail(cfg.From); err != nil {
		return fmt.Errorf("smtp sender %s: %w", cfg.From, err)
	}
	if err := c.Rcpt(to); err != nil {
		return fmt.Errorf("smtp recipient %s: %w", to, err)
	}
	w, err := c.Data()
	if err != nil {
		return fmt.Errorf("smtp data: %w", err)
	}
	// Closing w would send the terminating dot and deliver a truncated book;
	// returning lets the deferred c.Close drop the connection mid-DATA, which
	// makes the server discard the message.
	if err := writeMessage(w, cfg.From, to, subject, atts); err != nil {
		return fmt.Errorf("write message: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("smtp deliver: %w", err)
	}
	if err := c.Quit(); err != nil {
		slog.DebugContext(ctx, "smtp quit failed after delivery", "error", err)
	}
	return nil
}
