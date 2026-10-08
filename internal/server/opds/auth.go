package opds

import (
	"crypto/subtle"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/user"
)

var errUnauthorized = errors.New("opds: unauthorized")

// checkToken compares the stored OPDS token with the presented Basic
// password. Empty stored token disables OPDS for the user entirely.
func checkToken(stored, presented string) bool {
	if stored == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(stored), []byte(presented)) == 1
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
