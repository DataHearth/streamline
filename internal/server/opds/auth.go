package opds

import (
	"crypto/subtle"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/user"
	"github.com/datahearth/streamline/internal/appaccess"
	"github.com/datahearth/streamline/internal/auth"
)

var errUnauthorized = errors.New("opds: unauthorized")

// checkToken compares the stored OPDS token hash with the hash of the
// presented Basic password. Empty stored token disables OPDS for the user
// entirely.
func checkToken(stored, presented string) bool {
	if stored == "" {
		return false
	}
	return subtle.ConstantTimeCompare(
		[]byte(stored),
		[]byte(auth.HashOPDSToken(presented)),
	) == 1
}

func (h *Handler) authenticate(r *http.Request) (*ent.User, error) {
	name, pass, ok := r.BasicAuth()
	if !ok {
		return nil, errUnauthorized
	}
	u, err := h.client.User.Query().
		Where(user.EmailEQ(strings.ToLower(name))).
		Only(r.Context())
	if ent.IsNotFound(err) {
		return nil, errUnauthorized
	}
	if err != nil {
		return nil, err
	}
	if !checkToken(u.OpdsToken, pass) {
		return nil, errUnauthorized
	}
	h.tracker.Touch(
		r.Context(),
		u.ID,
		appaccess.OPDS,
		deref(u.OpdsCreatedAt),
		clientFromUserAgent(r.UserAgent()),
	)
	return u, nil
}

func (h *Handler) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, err := h.authenticate(r)
		switch {
		case errors.Is(err, errUnauthorized):
			w.Header().Set("WWW-Authenticate", `Basic realm="Streamline OPDS"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
		case err != nil:
			slog.ErrorContext(
				r.Context(),
				"OPDS authentication failed",
				"error",
				err,
			)
			http.Error(w, "internal error", http.StatusInternalServerError)
		default:
			next.ServeHTTP(w, r)
		}
	})
}

func deref(t *time.Time) time.Time {
	if t == nil {
		return time.Time{}
	}
	return *t
}

// clientFromUserAgent keeps the first product token of a User-Agent:
// "KOReader/2024.11 (Linux)" is "KOReader".
func clientFromUserAgent(ua string) string {
	product, _, _ := strings.Cut(strings.TrimSpace(ua), " ")
	name, _, _ := strings.Cut(product, "/")
	return name
}
