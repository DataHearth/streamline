package indexer

import (
	"context"
	"errors"
	"net"
	"strings"
)

// ErrNoIndexerAnswered marks a search that every enabled indexer failed: not
// one query came back. It is not an empty result, and a caller must not treat
// it as one — an automatic search that stamped its cooldown on it would wait
// out library.no_match_cooldown for a title nobody was asked about.
var ErrNoIndexerAnswered = errors.New("no indexer answered the search")

// IndexerFailure names one indexer that answered none of a search's queries
// and the failure of the last request it was sent.
type IndexerFailure struct {
	Indexer string
	// Outcome is the streamline.indexer.queries outcome label of the failure
	// ("timeout", "unreachable", "unauthorized", …).
	Outcome string
	Err     error
}

// UnavailableError is the error a search returns when no indexer answered.
// It matches ErrNoIndexerAnswered, and ErrUnreachable as well when every
// failure was a transport failure, so the grab-strike guard
// (TransportFailure) reads an outage the way it reads one on the grab.
type UnavailableError struct {
	Failures []IndexerFailure
}

func (e *UnavailableError) Error() string {
	parts := make([]string, 0, len(e.Failures))
	for _, f := range e.Failures {
		parts = append(parts, f.Indexer+": "+describeOutcome(f.Outcome))
	}
	return ErrNoIndexerAnswered.Error() + " (" + strings.Join(parts, "; ") + ")"
}

func (e *UnavailableError) Unwrap() []error {
	out := []error{ErrNoIndexerAnswered}
	transport := len(e.Failures) > 0
	for _, f := range e.Failures {
		if !errors.Is(f.Err, ErrUnreachable) {
			transport = false
			break
		}
	}
	if transport {
		out = append(out, ErrUnreachable)
	}
	return out
}

// TimedOut reports whether every indexer that failed ran out of time, which is
// what an indexer still working on an answer looks like from here.
func (e *UnavailableError) TimedOut() bool {
	if len(e.Failures) == 0 {
		return false
	}
	for _, f := range e.Failures {
		if f.Outcome != outcomeTimeout {
			return false
		}
	}
	return true
}

// Unavailable reports whether err says no indexer answered a search.
func Unavailable(err error) bool {
	return errors.Is(err, ErrNoIndexerAnswered)
}

const outcomeTimeout = "timeout"

// timedOut reports a request that ran out of time: the HTTP client's own
// timeout or a deadline on the query's context.
func timedOut(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var ne net.Error
	return errors.As(err, &ne) && ne.Timeout()
}

// describeOutcome is the operator-facing reading of an outcome label.
func describeOutcome(outcome string) string {
	switch outcome {
	case outcomeTimeout:
		return "timed out"
	case "unreachable":
		return "unreachable"
	case "unauthorized":
		return "API key rejected"
	case "login_failed":
		return "tracker login failed"
	case "challenge":
		return "blocked by a browser challenge"
	case "bad_response":
		return "unreadable response"
	case "bad_request":
		return "refused the query"
	case "unexpected_status":
		return "unexpected HTTP status"
	case "misconfigured":
		return "misconfigured"
	default:
		return "failed"
	}
}
