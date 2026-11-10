package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// api is one signed-in user's view of the REST API.
type api struct {
	base  string
	token string
	http  *http.Client
}

// apiError is a non-2xx answer, kept whole so a caller can tell a refused
// grab from a broken one.
type apiError struct {
	Status int
	Body   string
}

func (e *apiError) Error() string { return fmt.Sprintf("HTTP %d: %s", e.Status, e.Body) }

func login(ctx context.Context, base, email, password string) (*api, error) {
	body, _ := json.Marshal(map[string]string{"email": email, "password": password})
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		base+"/auth/login",
		bytes.NewReader(body),
	)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf(
			"login %s: %w",
			email,
			&apiError{resp.StatusCode, string(b)},
		)
	}
	for _, c := range resp.Cookies() {
		if c.Name == "streamline_session" {
			return &api{
				base:  base,
				token: c.Value,
				http:  &http.Client{Timeout: 5 * time.Minute},
			}, nil
		}
	}
	return nil, errors.New("login answered without a session cookie")
}

// do sends body as JSON to /api/v1+path and decodes a JSON answer into out
// when out is non-nil.
func (a *api) do(ctx context.Context, method, path string, body, out any) error {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, a.base+"/api/v1"+path, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+a.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := a.http.Do(req)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf(
			"%s %s: %w",
			method,
			path,
			&apiError{resp.StatusCode, string(b)},
		)
	}
	if out != nil && len(b) > 0 {
		if err := json.Unmarshal(b, out); err != nil {
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

func (a *api) post(ctx context.Context, path string, body, out any) error {
	return a.do(ctx, http.MethodPost, path, body, out)
}

// retry runs fn until it succeeds, ctx ends or the budget runs out, backing
// off on a provider's 429 or a 503 the way a person would wait and try again.
func retry(ctx context.Context, budget time.Duration, fn func() error) error {
	deadline := time.Now().Add(budget)
	wait := 2 * time.Second
	for {
		err := fn()
		if err == nil {
			return nil
		}
		var ae *apiError
		transient := errors.As(err, &ae) &&
			(ae.Status == 429 || ae.Status == 502 || ae.Status == 503 || ae.Status == 504 || ae.Status == 500)
		if !transient || time.Now().Add(wait).After(deadline) {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
		wait = min(wait*2, 30*time.Second)
	}
}

// poll calls check every interval until it reports done, an error, or the
// budget runs out.
func poll(
	ctx context.Context,
	budget, every time.Duration,
	what string,
	check func() (bool, error),
) error {
	deadline := time.Now().Add(budget)
	for {
		done, err := check()
		if err != nil {
			return err
		}
		if done {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out after %s waiting for %s", budget, what)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(every):
		}
	}
}
