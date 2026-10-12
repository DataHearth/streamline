package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/datahearth/streamline/internal/otelx"
)

// ErrChallenge is a site behind a browser challenge (Cloudflare, DDoS-Guard)
// that no solver got past.
var ErrChallenge = errors.New("the site is behind a browser challenge")

// Solver is a FlareSolverr-compatible service (FlareSolverr, trawl, Byparr):
// it opens a challenged page in a real browser and hands back the cookies
// and user agent that got through.
type Solver struct {
	URL     string
	Timeout time.Duration
	// Client reaches the solver itself; its own timeout is raised to cover
	// the solve.
	Client *http.Client
	// Proxy is the proxy the solver's browser should use, so the clearance
	// it earns is for the address the engine's own requests leave from.
	Proxy *url.URL
}

var challengeServers = []string{"cloudflare", "cloudflare-nginx", "ddos-guard"}

// isChallenge is CloudFlareDetectionService.IsCloudflareProtected.
func isChallenge(resp *response) bool {
	server := strings.ToLower(resp.header.Get("Server"))
	found := false
	for _, s := range challengeServers {
		found = found || server == s
	}
	if !found {
		return false
	}
	if resp.status == http.StatusServiceUnavailable ||
		resp.status == http.StatusForbidden {
		b := resp.body
		if strings.Contains(b, "<title>Just a moment...</title>") ||
			strings.Contains(b, "<title>Access denied</title>") ||
			strings.Contains(b, "<title>Attention Required! | Cloudflare</title>") ||
			strings.TrimSpace(b) == "error code: 1020" ||
			strings.Contains(strings.ToLower(b), "<title>ddos-guard</title>") {
			return true
		}
	}
	// The custom protection a handful of Dutch trackers run.
	return resp.header.Get("Vary") == "Accept-Encoding,User-Agent" &&
		resp.header.Get("Content-Encoding") == "" &&
		strings.Contains(strings.ToLower(resp.body), "ddos")
}

type solverRequest struct {
	Cmd        string       `json:"cmd"`
	URL        string       `json:"url"`
	MaxTimeout int64        `json:"maxTimeout"`
	PostData   string       `json:"postData,omitempty"`
	Proxy      *solverProxy `json:"proxy,omitempty"`
}

type solverProxy struct {
	URL      string `json:"url"`
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
}

type solverResponse struct {
	Status   string `json:"status"`
	Message  string `json:"message"`
	Solution struct {
		UserAgent string `json:"userAgent"`
		Cookies   []struct {
			Name  string `json:"name"`
			Value string `json:"value"`
		} `json:"cookies"`
	} `json:"solution"`
}

// maxSolverBody bounds the solver's answer, which carries the solved page.
const maxSolverBody = 16 << 20

// solve asks the solver to open r in its browser, then takes the cookies and
// user agent it got through with (FlareSolverr.PostResponse).
func (e *Engine) solve(ctx context.Context, r request) error {
	s := e.solver
	if s == nil {
		return fmt.Errorf(
			"%w: set cardigann.solver_url to a FlareSolverr-compatible service",
			ErrChallenge,
		)
	}
	req := solverRequest{
		Cmd: "request.get", URL: r.url, MaxTimeout: s.Timeout.Milliseconds(),
	}
	if r.method == http.MethodPost {
		if r.contentType != "" {
			return fmt.Errorf(
				"%w: a solver cannot post %s",
				ErrChallenge,
				r.contentType,
			)
		}
		req.Cmd, req.PostData = "request.post", r.form
	}
	if p := s.Proxy; p != nil {
		sp := &solverProxy{URL: (&url.URL{Scheme: p.Scheme, Host: p.Host}).String()}
		if p.User != nil {
			sp.Username = p.User.Username()
			sp.Password, _ = p.User.Password()
		}
		req.Proxy = sp
	}
	body, err := json.Marshal(req)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, s.Timeout+5*time.Second)
	defer cancel()
	hr, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimSuffix(s.URL, "/")+"/v1", bytes.NewReader(body))
	if err != nil {
		return err
	}
	hr.Header.Set("Content-Type", "application/json")
	client := *s.Client
	client.Timeout = s.Timeout + 10*time.Second
	resp, err := client.Do(hr)
	if err != nil {
		return fmt.Errorf(
			"%w: solver: %w",
			ErrChallenge,
			otelx.RedactTransportError(err),
		)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK &&
		resp.StatusCode != http.StatusInternalServerError {
		return fmt.Errorf("%w: solver answered %d", ErrChallenge, resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxSolverBody))
	if err != nil {
		return err
	}
	var sr solverResponse
	if err := json.Unmarshal(raw, &sr); err != nil {
		return fmt.Errorf("%w: unreadable solver answer: %w", ErrChallenge, err)
	}
	if len(sr.Solution.Cookies) == 0 {
		msg := sr.Message
		if msg == "" {
			msg = "no cookies came back"
		}
		return fmt.Errorf("%w: solver: %s", ErrChallenge, msg)
	}
	target, err := url.Parse(r.url)
	if err != nil {
		return err
	}
	cookies := make([]*http.Cookie, 0, len(sr.Solution.Cookies))
	for _, c := range sr.Solution.Cookies {
		//nolint:gosec // G124: a client-side jar entry, sent back where it came from
		cookies = append(
			cookies,
			&http.Cookie{Name: c.Name, Value: c.Value, Path: "/"},
		)
	}
	e.client.Jar.SetCookies(target, cookies)
	if target.Host != e.site.Host {
		e.client.Jar.SetCookies(e.site, cookies)
	}
	if sr.Solution.UserAgent != "" {
		e.ua = sr.Solution.UserAgent
	}
	return nil
}
