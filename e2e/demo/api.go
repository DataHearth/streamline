package main

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
	"strconv"
	"strings"
	"time"
)

// api is a thin REST client logged in as one user.
type api struct {
	base  string
	token string
	hc    *http.Client
}

func (in *instance) client() *api {
	return &api{base: in.base, hc: &http.Client{Timeout: 5 * time.Minute}}
}

// statusError is a non-2xx answer.
type statusError struct {
	Method, Path string
	Status       int
	Body         string
	RetryAfter   time.Duration
}

func (e *statusError) Error() string {
	return fmt.Sprintf("%s %s: %d %s", e.Method, e.Path, e.Status, strings.TrimSpace(e.Body))
}

func (a *api) login(ctx context.Context, email, password string) error {
	body, _ := json.Marshal(map[string]string{"email": email, "password": password})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.base+"/auth/login", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := a.hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		b, _ := io.ReadAll(resp.Body)
		return &statusError{Method: "POST", Path: "/auth/login", Status: resp.StatusCode, Body: string(b)}
	}
	for _, c := range resp.Cookies() {
		if c.Name == "streamline_session" {
			a.token = c.Value
			return nil
		}
	}
	return fmt.Errorf("login: no session cookie")
}

// do sends one request. A 429 (a provider asking to slow down, MusicBrainz
// most often) is waited out and retried a few times, not reported: a skipped
// artist would leave a hole in the recording.
func (a *api) do(ctx context.Context, method, path string, in, out any) error {
	for attempt := 0; ; attempt++ {
		err := a.once(ctx, method, path, in, out)
		var se *statusError
		if attempt >= 5 || !errors.As(err, &se) || se.Status != http.StatusTooManyRequests {
			return err
		}
		wait := 30 * time.Second
		if se.RetryAfter > 0 {
			wait = se.RetryAfter
		}
		slog.Info("rate limited, waiting", "request", method+" "+path, "wait", wait)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
	}
}

func (a *api) once(ctx context.Context, method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		raw, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, a.base+"/api/v1"+path, body)
	if err != nil {
		return err
	}
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Authorization", "Bearer "+a.token)
	resp, err := a.hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode/100 != 2 {
		se := &statusError{Method: method, Path: path, Status: resp.StatusCode, Body: string(raw)}
		if secs, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil && secs > 0 {
			se.RetryAfter = time.Duration(secs) * time.Second
		}
		return se
	}
	if out != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			return fmt.Errorf("%s %s: decode: %w", method, path, err)
		}
	}
	return nil
}

func (a *api) get(ctx context.Context, path string, q url.Values, out any) error {
	if len(q) > 0 {
		path += "?" + q.Encode()
	}
	return a.do(ctx, http.MethodGet, path, nil, out)
}

func (a *api) post(ctx context.Context, path string, in, out any) error {
	return a.do(ctx, http.MethodPost, path, in, out)
}

func (a *api) patch(ctx context.Context, path string, in, out any) error {
	return a.do(ctx, http.MethodPatch, path, in, out)
}

// waitFor polls cond every interval until it reports done, fails, or the
// timeout passes.
func waitFor(ctx context.Context, what string, timeout, interval time.Duration, cond func() (bool, error)) error {
	deadline := time.Now().Add(timeout)
	for {
		done, err := cond()
		if err != nil {
			return fmt.Errorf("%s: %w", what, err)
		}
		if done {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("%s: gave up after %s", what, timeout)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(interval):
		}
	}
}
