package extension

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	pathpkg "path"
	"slices"
	"strings"
	"time"

	"github.com/datahearth/streamline/internal/auth"
	"github.com/datahearth/streamline/internal/otelx"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

const (
	authSelf    = "self"
	authSession = "session"

	serviceServeFile   = "serve_file"
	serviceServePoster = "serve_poster"
)

var requests = otelx.Must(
	otel.Meter("github.com/datahearth/streamline/internal/extension").
		Int64Counter(
			"streamline.extension.requests",
			metric.WithDescription("Requests dispatched to extensions by outcome"),
		),
)

var sqlDuration = otelx.Must(
	otel.Meter("github.com/datahearth/streamline/internal/extension").
		Float64Histogram(
			"streamline.extension.sql.duration",
			metric.WithDescription("Duration of the SQL statements extensions run"),
			metric.WithUnit("s"),
		),
)

// posterViews maps a poster kind a guest may ask for to the view grant that
// makes the title behind it the extension's to know about.
var posterViews = map[string]string{"books": "books", "authors": "authors"}

var droppedResponseHeaders = []string{
	"Set-Cookie", "Content-Length", "Transfer-Encoding", "Connection",
	"Content-Security-Policy", "X-Content-Type-Options",
}

// guestCSP keeps a document an extension serves from running script or
// reading the session of the origin it shares with the web UI.
const guestCSP = "sandbox; default-src 'none'; img-src 'self' data:; media-src 'self'; " +
	"style-src 'unsafe-inline'"

type FileOpener interface {
	// BookFile returns the path and modification time of a present book file.
	BookFile(
		ctx context.Context,
		id uint32,
	) (path string, modTime time.Time, err error)
}

type PosterServer interface {
	Serve(w http.ResponseWriter, r *http.Request, kind string, id uint32)
}

// route finds the extension a path is addressed to. handled is false when the
// path is none of an extension's, and true with a nil run when it is but the
// extension is not running, or the path names an extension's subtree in a
// form the auth middleware did not see (a dot segment).
func (m *Manager) route(
	path string,
) (run *Running, rel, authKind string, handled bool) {
	if rest, ok := strings.CutPrefix(path, "/api/v1/extensions/"); ok {
		name, after, _ := strings.Cut(rest, "/")
		sub := "/" + after
		if name == "" || !isExtensionSubtree(sub) {
			return nil, "", "", false
		}
		if clean := pathpkg.Clean(path); path != clean && path != clean+"/" {
			return nil, "", "", true
		}
		run, ok := m.AcquireNamed(name)
		if !ok {
			return nil, "", "", true
		}
		// A settings card is written for another account by an admin, through
		// the host's own endpoint and its value check. Reached here, the guest
		// would see any signed-in user as the caller with no target.
		if run.settingsCardPath(sub) {
			run.Release()
			return nil, "", "", true
		}
		return run, sub, authSession, true
	}

	prefix, tail := splitMount(path)
	if prefix == "" {
		return nil, "", "", false
	}
	run, mt, ok := m.Acquire(prefix)
	if !ok {
		return nil, "", "", (*m.stopped.Load())[prefix]
	}
	return run, tail, mt.Auth, true
}

func (r *Running) settingsCardPath(sub string) bool {
	rest, ok := strings.CutPrefix(sub, "/cards/")
	if !ok {
		return false
	}
	id, _, _ := strings.Cut(rest, "/")
	return slices.ContainsFunc(
		r.module.manifest.AccountCards,
		func(c AccountCard) bool {
			return c.ID == id && c.Kind == CardSettings
		},
	)
}

func isExtensionSubtree(sub string) bool {
	for _, p := range []string{"/cards", "/api"} {
		if sub == p || strings.HasPrefix(sub, p+"/") {
			return true
		}
	}
	return false
}

func splitMount(path string) (prefix, tail string) {
	if len(path) < 2 || path[0] != '/' {
		return "", ""
	}
	i := strings.IndexByte(path[1:], '/')
	if i < 0 {
		return path, "/"
	}
	return path[:i+1], path[i+1:]
}

// SelfAuthenticated reports whether path lies under a live mount that checks
// its own credentials, so the auth middleware must let it through.
func (m *Manager) SelfAuthenticated(path string) bool {
	prefix, _ := splitMount(path)
	if prefix == "" {
		return false
	}
	e, ok := (*m.table.Load())[prefix]
	return ok && e.mount.Auth == authSelf
}

func (m *Manager) Middleware(
	files FileOpener,
	posters PosterServer,
) func(http.Handler) http.Handler {
	d := &dispatcher{files: files, posters: posters}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			run, rel, authKind, handled := m.route(r.URL.Path)
			switch {
			case !handled:
				next.ServeHTTP(w, r)
			case run == nil:
				http.NotFound(w, r)
			default:
				d.serve(w, r, run, rel, authKind)
			}
		})
	}
}

type dispatcher struct {
	files   FileOpener
	posters PosterServer
}

func (d *dispatcher) serve(
	w http.ResponseWriter,
	r *http.Request,
	run *Running,
	rel, authKind string,
) {
	defer run.Release()
	ctx, span := tracer.Start(
		r.Context(),
		"extension.request",
		trace.WithAttributes(
			attribute.String("extension.name", run.name),
			attribute.String("extension.mount_auth", authKind),
		),
	)
	defer span.End()

	w.Header().Set("Content-Security-Policy", guestCSP)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	outcome := d.handle(ctx, w, r, run, rel, authKind, span)
	requests.Add(ctx, 1, metric.WithAttributes(
		attribute.String("extension", run.name),
		attribute.String("outcome", outcome),
	))
}

func (d *dispatcher) handle(
	ctx context.Context,
	w http.ResponseWriter,
	r *http.Request,
	run *Running,
	rel, authKind string,
	span trace.Span,
) string {
	req, err := buildRequest(r, rel, authKind)
	if err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) || errors.Is(err, errBodyTooLarge) {
			http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
		} else {
			http.Error(w, "unreadable request body", http.StatusBadRequest)
		}
		return "bad_request"
	}

	res, err := run.call(ctx, req)
	if err != nil {
		return failRequest(ctx, w, span, run.name, err)
	}
	resp := res.Response
	if resp.Status != 0 && (resp.Status < 100 || resp.Status > 599) {
		return failRequest(
			ctx,
			w,
			span,
			run.name,
			errors.New("the guest answered an invalid status code"),
		)
	}

	switch {
	case resp.ServeFile != nil:
		if !res.fileOK {
			http.NotFound(w, r)
			return "not_found"
		}
		return d.serveFile(ctx, w, r, run, resp.ServeFile)
	case resp.ServePoster != nil:
		return d.servePoster(ctx, w, r, run, resp.ServePoster)
	case resp.SendFile != nil:
		return d.sendFile(ctx, w, r, run, res.folders, resp.SendFile)
	case resp.StreamFFmpeg != nil:
		return d.streamFFmpeg(ctx, w, r, run, res.folders, resp.StreamFFmpeg)
	}
	writeResponse(ctx, w, resp)
	return "ok"
}

// callResult is what one guest call leaves the dispatcher: the answer,
// whether a serve_file answer names a file the extension may hand out, and
// the folders the call had mounted.
type callResult struct {
	Response
	fileOK  bool
	folders []Folder
}

// call runs the guest on one pooled connection and returns it before the
// answer is written, so a slow download holds none.
func (r *Running) call(ctx context.Context, req Request) (callResult, error) {
	conn, err := r.store.Conn(ctx)
	if err != nil {
		return callResult{}, err
	}
	defer func() {
		if err := conn.Release(); err != nil {
			slog.WarnContext(
				ctx,
				"Extension store connection not returned cleanly",
				"extension",
				r.name,
				"error",
				err,
			)
		}
	}()

	folders := r.mgr.folders(ctx, r)
	mounts, closeMounts, err := mountConfig(folders)
	if err != nil {
		return callResult{}, err
	}
	defer closeMounts()

	req.Folders = folders
	ctx = context.WithValue(ctx, runningKey{}, r)
	tc := tracedConn{c: conn, name: r.name}
	resp, err := r.module.Handle(ctx, tc, req, mounts)
	if err != nil {
		return callResult{}, err
	}
	res := callResult{Response: resp, folders: folders}
	if resp.ServeFile != nil {
		res.fileOK = r.fileGranted(ctx, tc, resp.ServeFile)
	}
	return res, nil
}

func (r *Running) fileGranted(
	ctx context.Context,
	conn tracedConn,
	sf *ServeFile,
) bool {
	if !slices.Contains(r.grants.Services, serviceServeFile) {
		slog.WarnContext(ctx, "Extension asked for a service it was not granted",
			"extension", r.name, "service", serviceServeFile)
		return false
	}
	res, err := conn.Query(
		ctx,
		"SELECT 1 FROM streamline_book_files WHERE id = ?",
		[]any{int64(sf.FileID)},
	)
	if res.Error != "" {
		slog.WarnContext(ctx, "Extension file lookup refused",
			"extension", r.name, "error", res.Error)
	}
	return err == nil && res.Error == "" && len(res.Rows) > 0
}

// Call hands one request to the named running extension, for a host endpoint
// that speaks to an extension on a caller's behalf.
func (m *Manager) Call(
	ctx context.Context,
	name string,
	req Request,
) (Response, bool, error) {
	run, ok := m.AcquireNamed(name)
	if !ok {
		return Response{}, false, nil
	}
	defer run.Release()
	res, err := run.call(ctx, req)
	return res.Response, true, err
}

func failRequest(
	ctx context.Context,
	w http.ResponseWriter,
	span trace.Span,
	name string,
	err error,
) string {
	slog.ErrorContext(ctx, "Extension request failed",
		"extension", name, "error", err)
	span.RecordError(err)
	span.SetStatus(codes.Error, err.Error())
	http.Error(w, "internal error", http.StatusInternalServerError)
	return "error"
}

var errBodyTooLarge = errors.New("request body too large")

func buildRequest(r *http.Request, rel, authKind string) (Request, error) {
	var body []byte
	if r.Body != nil {
		var err error
		body, err = io.ReadAll(io.LimitReader(r.Body, MaxResponseBody+1))
		if err != nil {
			return Request{}, err
		}
		if len(body) > MaxResponseBody {
			return Request{}, errBodyTooLarge
		}
	}

	headers := r.Header.Clone()
	headers.Del("Cookie")
	req := Request{
		Method:  r.Method,
		Path:    rel,
		Query:   r.URL.RawQuery,
		Headers: headers,
		Body:    body,
	}
	if authKind == authSelf {
		return req, nil
	}
	headers.Del("Authorization")
	headers.Del("X-Api-Key")
	if c := auth.ClaimsFromContext(r.Context()); c != nil {
		req.Caller = &Caller{ID: c.UserID, Role: c.Role}
	}
	return req, nil
}

func writeResponse(ctx context.Context, w http.ResponseWriter, resp Response) {
	h := w.Header()
	for k, vs := range resp.Headers {
		if slices.ContainsFunc(droppedResponseHeaders, func(d string) bool {
			return strings.EqualFold(d, k)
		}) {
			continue
		}
		for _, v := range vs {
			h.Add(k, v)
		}
	}
	status := resp.Status
	if status == 0 {
		status = http.StatusOK
	}
	w.WriteHeader(status)
	if _, err := w.Write(resp.Body); err != nil {
		slog.DebugContext(ctx, "Extension response not delivered", "error", err)
	}
}

func (d *dispatcher) serveFile(
	ctx context.Context,
	w http.ResponseWriter,
	r *http.Request,
	run *Running,
	sf *ServeFile,
) string {
	path, modTime, err := d.files.BookFile(ctx, sf.FileID)
	if err != nil {
		http.NotFound(w, r)
		return "not_found"
	}
	//nolint:gosec // the path is the one FileOpener resolved from a book file row
	f, err := os.Open(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			slog.WarnContext(ctx, "Extension file could not be opened",
				"extension", run.name, "file.id", sf.FileID, "error", err)
		}
		http.NotFound(w, r)
		return "not_found"
	}
	defer f.Close()

	ct := sf.ContentType
	if ct == "" {
		ct = "application/octet-stream"
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set(
		"Content-Disposition",
		`attachment; filename="`+attachmentName(sf.Filename)+`"`,
	)
	http.ServeContent(
		slowClientWriter{w, http.NewResponseController(w)},
		r,
		"",
		modTime,
		f,
	)
	return "ok"
}

// sendFile runs after the guest has returned, so it holds no store
// connection while the bytes go out. The file is opened through os.Root, so a
// dot segment or a link leading out of the folder is a 404, never the file.
func (d *dispatcher) sendFile(
	ctx context.Context,
	w http.ResponseWriter,
	r *http.Request,
	run *Running,
	folders []Folder,
	sf *SendFile,
) string {
	root, rel, ok := resolveInFolder(folders, sf.Path)
	if !ok {
		slog.WarnContext(ctx, "Extension asked for a file outside its folders",
			"extension", run.name)
		http.NotFound(w, r)
		return "not_found"
	}
	defer func() {
		if err := root.Close(); err != nil {
			slog.DebugContext(ctx, "Extension folder root not closed",
				"extension", run.name, "error", err)
		}
	}()

	f, err := root.Open(rel)
	if err != nil {
		http.NotFound(w, r)
		return "not_found"
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || st.IsDir() {
		http.NotFound(w, r)
		return "not_found"
	}

	ct := sf.ContentType
	if ct == "" {
		ct = "application/octet-stream"
	}
	w.Header().Set("Content-Type", ct)
	if sf.Filename != "" {
		w.Header().Set(
			"Content-Disposition",
			`attachment; filename="`+attachmentName(sf.Filename)+`"`,
		)
	}
	http.ServeContent(
		slowClientWriter{w, http.NewResponseController(w)},
		r,
		"",
		st.ModTime(),
		f,
	)
	return "ok"
}

func attachmentName(s string) string {
	var b strings.Builder
	for _, c := range s {
		if c == '"' || c == '/' || c == '\\' || c < 0x20 || c == 0x7f {
			continue
		}
		b.WriteRune(c)
	}
	if name := strings.TrimSpace(b.String()); name != "" {
		return name
	}
	return "download"
}

func (d *dispatcher) servePoster(
	ctx context.Context,
	w http.ResponseWriter,
	r *http.Request,
	run *Running,
	sp *ServePoster,
) string {
	view, known := posterViews[sp.Kind]
	if !known ||
		!slices.Contains(run.grants.Services, serviceServePoster) ||
		!slices.Contains(run.grants.Views, view) {
		slog.WarnContext(ctx, "Extension asked for a poster it was not granted",
			"extension", run.name, "poster.kind", sp.Kind)
		http.NotFound(w, r)
		return "not_found"
	}
	d.posters.Serve(w, r, sp.Kind, sp.ID)
	return "ok"
}

// tracedConn puts each statement a guest runs under its own span.
type tracedConn struct {
	c    *Conn
	name string
}

func (t tracedConn) Query(
	ctx context.Context,
	query string,
	args []any,
) (SQLResult, error) {
	return t.run(ctx, "query", t.c.Query, query, args)
}

func (t tracedConn) Exec(
	ctx context.Context,
	query string,
	args []any,
) (SQLResult, error) {
	return t.run(ctx, "exec", t.c.Exec, query, args)
}

func (t tracedConn) run(
	ctx context.Context,
	op string,
	fn func(context.Context, string, []any) (SQLResult, error),
	query string,
	args []any,
) (SQLResult, error) {
	ctx, span := tracer.Start(ctx, "extension.sql", trace.WithAttributes(
		attribute.String("extension.name", t.name),
		attribute.String("db.operation.name", op),
	))
	defer span.End()
	start := time.Now()
	res, err := fn(ctx, query, args)
	sqlDuration.Record(ctx, time.Since(start).Seconds(), metric.WithAttributes(
		attribute.String("extension", t.name),
		attribute.String("db.operation.name", op),
	))
	if err == nil && res.Error != "" {
		span.SetStatus(codes.Error, res.Error)
	}
	return res, otelx.RecordSpanError(span, err)
}

// slowClientWriter gives each write its own deadline. Under the listener's
// single WriteTimeout a large file to a slow client is cut mid-download; with
// this only a client that stops reading is.
type slowClientWriter struct {
	http.ResponseWriter
	rc *http.ResponseController
}

func (w slowClientWriter) Write(p []byte) (int, error) {
	if err := w.rc.SetWriteDeadline(
		time.Now().Add(streamWriteDeadline),
	); err != nil &&
		!errors.Is(err, http.ErrNotSupported) {
		return 0, err
	}
	return w.ResponseWriter.Write(p)
}
