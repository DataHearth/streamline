package config

import (
	"fmt"
	"sort"
	"strings"
)

// secretKeyParts mark a flattened key whose value must never reach the log.
// Matched as substrings, so this deliberately catches `session_secret_file`
// too: a path is not a secret, but the cost of redacting one is a line that
// says "changed" instead of naming a file.
var secretKeyParts = []string{"password", "secret", "api_key", "token"}

func isSecretKey(key string) bool {
	lower := strings.ToLower(key)
	for _, p := range secretKeyParts {
		if strings.Contains(lower, p) {
			return true
		}
	}
	return false
}

// changedKeys renders the difference between two flattened config views as
// sorted "key: old → new" strings.
//
// Seven config sections are patchable at runtime by any admin, and the write
// used to log only that a file was saved. Which setting moved, and from what,
// was then recoverable only from version control on a file that is typically
// not in version control — so "who turned selective downloads on last Tuesday,
// and what was it before" had no answer anywhere.
func changedKeys(prev, next map[string]any) []string {
	out := make([]string, 0, 8)
	for k, pv := range prev {
		nv, ok := next[k]
		if !ok {
			out = append(out, k+": removed")
			continue
		}
		if fmt.Sprintf("%v", pv) == fmt.Sprintf("%v", nv) {
			continue
		}
		out = append(out, describeChange(k, pv, nv))
	}
	for k, nv := range next {
		if _, ok := prev[k]; !ok {
			out = append(out, describeChange(k, nil, nv))
		}
	}
	sort.Strings(out)
	return out
}

func describeChange(key string, prev, next any) string {
	if isSecretKey(key) {
		return key + ": changed"
	}
	return fmt.Sprintf("%s: %v → %v", key, prev, next)
}
