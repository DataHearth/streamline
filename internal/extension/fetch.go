package extension

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strings"

	extism "github.com/extism/go-sdk"

	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/otelx"
)

var (
	errFetchDenied = errors.New("http is not granted to this extension")
	errFetchHost   = errors.New("the host is not in this extension's hosts list")
)

// A redirect is handed back to the guest, never followed: following one would
// let a listed host send the request anywhere.
var guestHTTP = &http.Client{
	Transport: otelx.HTTPClient.Transport,
	Timeout:   otelx.HTTPClient.Timeout,
	CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	},
}

func (r *Running) fetch(ctx context.Context, call FetchCall) (FetchResult, error) {
	if !slices.Contains(r.grants.Services, serviceHTTP) {
		slog.WarnContext(ctx, "Extension asked for a service it was not granted",
			"extension", r.name, "service", serviceHTTP)
		return FetchResult{}, errFetchDenied
	}
	u, err := url.Parse(call.URL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return FetchResult{}, fmt.Errorf("%q is not an http or https URL", call.URL)
	}
	if !slices.Contains(allowedHosts(r.name), strings.ToLower(u.Host)) {
		slog.WarnContext(ctx, "Extension called a host it may not reach",
			"extension", r.name, "host", u.Host)
		return FetchResult{}, errFetchHost
	}
	req, err := http.NewRequestWithContext(
		ctx, call.Method, u.String(), bytes.NewReader(call.Body),
	)
	if err != nil {
		return FetchResult{}, err
	}
	req.Header = http.Header(call.Headers).Clone()
	resp, err := guestHTTP.Do(req)
	if err != nil {
		return FetchResult{}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxResponseBody+1))
	if err != nil {
		return FetchResult{}, err
	}
	if len(body) > MaxResponseBody {
		return FetchResult{}, errors.New("the answer is larger than 16 MiB")
	}
	return FetchResult{
		Status: resp.StatusCode, Headers: resp.Header, Body: body,
	}, nil
}

// allowedHosts reads the entry's list on every call, so the config file is
// the only place a host is ever allowed from.
func allowedHosts(name string) []string {
	cfg := config.Get()
	if cfg == nil {
		return nil
	}
	for _, e := range cfg.Extensions {
		if e.Name == name {
			return e.Hosts
		}
	}
	return nil
}

func fetchHost(ctx context.Context, p *extism.CurrentPlugin, stack []uint64) {
	offset := stack[0]
	stack[0] = 0
	r, _ := ctx.Value(runningKey{}).(*Running)
	if r == nil {
		return
	}
	raw, err := p.ReadBytes(offset)
	if err != nil {
		return
	}
	var call FetchCall
	if err := json.Unmarshal(raw, &call); err != nil {
		return
	}
	res, err := r.fetch(ctx, call)
	if err != nil {
		res = FetchResult{Error: err.Error()}
	}
	out, err := json.Marshal(res)
	if err != nil {
		return
	}
	if stack[0], err = p.WriteBytes(out); err != nil {
		stack[0] = 0
	}
}
