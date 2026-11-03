package testutil

import (
	"os"
	"strings"
)

// inherited is the STREAMLINE_* environment the test binary started with —
// the session's, never a spec's.
var inherited = func() map[string]string {
	out := map[string]string{}
	for _, kv := range os.Environ() {
		key, value, _ := strings.Cut(kv, "=")
		if strings.HasPrefix(key, "STREAMLINE_") &&
			!strings.HasPrefix(key, "STREAMLINE_E2E_") {
			out[key] = value
		}
	}
	return out
}()

// IsolateEnv unsets the STREAMLINE_* variables the test process inherited and
// returns the function that puts them back. The config loader reads that
// prefix as an override layer, so a session that exports real keys — a cloud
// environment supplying STREAMLINE_METADATA__TMDB_API_KEY as a secret, a
// shell with a dev instance's settings — leaks them into every spec that
// loads config: the TMDB mock saw the real key instead of the spec's, and the
// *_file specs failed excluded_with against a key nobody set.
//
// Only what the process started with goes, and only while it still holds the
// inherited value: a spec that sets its own variable before loading config is
// testing exactly that layer. STREAMLINE_E2E_* stays: those are the e2e
// suites' own switches, not config.
//
// Pair it with DeferCleanup: DeferCleanup(testutil.IsolateEnv()).
func IsolateEnv() func() {
	removed := map[string]string{}
	for key, value := range inherited {
		if current, ok := os.LookupEnv(key); ok && current == value {
			removed[key] = value
			_ = os.Unsetenv(key)
		}
	}
	return func() {
		for key, value := range removed {
			_ = os.Setenv(key, value)
		}
	}
}
