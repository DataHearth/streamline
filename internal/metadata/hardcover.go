package metadata

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/time/rate"

	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/otelx"
)

const (
	hcEndpoint = "https://api.hardcover.app/v1/graphql"

	// hcBookBatch is how many books one batch query carries; the editions
	// aliases make a larger batch likelier to hit Hardcover's 30 s cut-off.
	hcBookBatch = 20
	hcLookupTTL = 5 * time.Minute
	hcDetailTTL = 10 * time.Minute
	hcMemoCap   = 512

	// Hardcover's free plan allows 5,000 requests a day. The reserve is what a
	// bulk scan leaves behind so adds, refreshes and request approvals keep
	// working after it has eaten the rest of the day. The limits are
	// Hardcover's, not the operator's, so there is no config key.
	hardcoverDailyBudget = 5000
	hardcoverScanReserve = 500

	hcDefaultRetryAfter = time.Minute

	hcFormatAudiobook = 2
	hcFormatEbook     = 4
)

var (
	ErrHardcoverKeyMissing = errors.New(
		"metadata: hardcover api key not configured",
	)
	ErrHardcoverUnauthorized = errors.New(
		"metadata: hardcover token rejected (expired?)",
	)
)

// Hardcover is the BookProvider implementation. GraphQL API, Bearer token,
// 60 req/min budget, max query depth 3 (bibliographies are fetched in two
// queries). Tokens expire every January 1st.
type Hardcover struct {
	client       *http.Client
	token        string
	limiter      *rate.Limiter
	authRejected atomic.Bool

	budgetMu  sync.Mutex
	budgetDay time.Time
	used      int

	lookups memo[[]BookLookupHit]
	books   memo[*BookRecord]
}

func NewHardcover() (*Hardcover, error) {
	m := config.Get().Metadata
	token := strings.TrimSpace(
		config.SecretValue(m.HardcoverAPIKey, m.HardcoverAPIKeyFile),
	)
	if token == "" {
		return nil, ErrHardcoverKeyMissing
	}
	c := *otelx.HTTPClient
	return &Hardcover{
		client:  &c,
		token:   token,
		limiter: rate.NewLimiter(rate.Every(time.Second), 1),
	}, nil
}

// AuthRejected reports whether Hardcover's latest answer was a 401.
func (h *Hardcover) AuthRejected() bool {
	return h.authRejected.Load()
}

var _ Budgeter = (*Hardcover)(nil)

func (h *Hardcover) ScanReserve() int {
	return hardcoverScanReserve
}

// Remaining is the number of requests left in the current UTC day.
func (h *Hardcover) Remaining() int {
	h.budgetMu.Lock()
	defer h.budgetMu.Unlock()
	h.rollDay(time.Now().UTC())
	return hardcoverDailyBudget - h.used
}

func (h *Hardcover) rollDay(now time.Time) {
	day := now.Truncate(24 * time.Hour)
	if !day.Equal(h.budgetDay) {
		h.budgetDay = day
		h.used = 0
	}
}

// take spends one request of today's budget. When none is left it returns how
// long until the budget resets at UTC midnight.
func (h *Hardcover) take() (time.Duration, bool) {
	h.budgetMu.Lock()
	defer h.budgetMu.Unlock()
	now := time.Now().UTC()
	h.rollDay(now)
	if h.used >= hardcoverDailyBudget {
		return h.budgetDay.Add(24 * time.Hour).Sub(now), false
	}
	h.used++
	return 0, true
}

func (h *Hardcover) refund() {
	h.budgetMu.Lock()
	defer h.budgetMu.Unlock()
	if h.used > 0 {
		h.used--
	}
}

func parseRetryAfter(v string, now time.Time) time.Duration {
	v = strings.TrimSpace(v)
	if secs, err := strconv.Atoi(v); err == nil && secs >= 0 {
		return time.Duration(secs) * time.Second
	}
	if t, err := http.ParseTime(v); err == nil {
		return max(t.Sub(now), 0)
	}
	return hcDefaultRetryAfter
}

func (h *Hardcover) query(
	ctx context.Context,
	gql string,
	vars map[string]any,
	out any,
) error {
	ctx, span := tracer.Start(ctx, "metadata.hardcover.query")
	defer span.End()

	if err := h.limiter.Wait(ctx); err != nil {
		return otelx.RecordSpanError(span, err)
	}
	body, err := json.Marshal(map[string]any{"query": gql, "variables": vars})
	if err != nil {
		return otelx.RecordSpanError(span, err)
	}
	req, err := http.NewRequestWithContext(
		ctx, http.MethodPost, hcEndpoint, bytes.NewReader(body),
	)
	if err != nil {
		return otelx.RecordSpanError(span, err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+h.token)

	if wait, ok := h.take(); !ok {
		hardcoverRequests.Add(ctx, 1, outcomeAttr("rate_limited"))
		slog.WarnContext(ctx, "hardcover daily budget exhausted",
			"retry_after", wait.Round(time.Second))
		return otelx.RecordSpanError(span, &RateLimitedError{RetryAfter: wait})
	}
	resp, err := h.client.Do(req)
	if err != nil {
		// Nothing reached Hardcover, so the day is not charged for it. A
		// timed-out request may have arrived; refunding it is the kinder error.
		h.refund()
		hardcoverRequests.Add(ctx, 1, outcomeAttr("error"))
		return otelx.RecordSpanError(span, err)
	}
	defer resp.Body.Close()

	recordProviderStatus(ctx, resp.StatusCode)
	switch {
	case resp.StatusCode == http.StatusTooManyRequests:
		hardcoverRequests.Add(ctx, 1, outcomeAttr("rate_limited"))
		retryAfter := parseRetryAfter(resp.Header.Get("Retry-After"), time.Now())
		slog.WarnContext(ctx, "hardcover rate limited",
			"retry_after", retryAfter.Round(time.Second))
		return otelx.RecordSpanError(span, &RateLimitedError{RetryAfter: retryAfter})
	case resp.StatusCode == http.StatusUnauthorized:
		hardcoverRequests.Add(ctx, 1, outcomeAttr("auth"))
		h.authRejected.Store(true)
		slog.WarnContext(ctx, "hardcover token rejected",
			"http.status_code", resp.StatusCode)
		return otelx.RecordSpanError(span, ErrHardcoverUnauthorized)
	case resp.StatusCode != http.StatusOK:
		hardcoverRequests.Add(ctx, 1, outcomeAttr("error"))
		slog.WarnContext(ctx, "hardcover request non-200",
			"http.status_code", resp.StatusCode)
		return otelx.RecordSpanError(span,
			fmt.Errorf("hardcover: status %d", resp.StatusCode))
	}
	hardcoverRequests.Add(ctx, 1, outcomeAttr("ok"))
	h.authRejected.Store(false)

	var envelope struct {
		Data   json.RawMessage `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := otelx.DecodeJSON(
		resp.Body,
		maxProviderResponse,
		&envelope,
	); err != nil {
		return otelx.RecordSpanError(span, err)
	}
	if len(envelope.Errors) > 0 {
		return otelx.RecordSpanError(span,
			fmt.Errorf("hardcover: %s", envelope.Errors[0].Message))
	}
	return otelx.RecordSpanError(span, json.Unmarshal(envelope.Data, out))
}

// hcImage decodes an image field that is either a bare URL string or an
// object carrying one, depending on the row.
type hcImage string

func (i *hcImage) UnmarshalJSON(b []byte) error {
	var s string
	if json.Unmarshal(b, &s) == nil {
		*i = hcImage(s)
		return nil
	}
	var o struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(b, &o); err != nil {
		return err
	}
	*i = hcImage(o.URL)
	return nil
}

func parseHCDate(s string) *time.Time {
	for _, layout := range []string{"2006-01-02", "2006"} {
		if t, err := time.Parse(layout, s); err == nil {
			return &t
		}
	}
	return nil
}
