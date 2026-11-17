package config

import (
	"errors"
	"fmt"
	"strings"
)

// MinSessionSecretLen is the shortest configured auth.session_secret the
// config accepts, in bytes once surrounding whitespace is trimmed. It is a
// floor, not a strength estimate: what it rules out is a secret that is only
// technically there — a lone space, a byte-order mark, the six bytes
// PowerShell writes for an empty UTF-16 file — and so is as public as the
// empty key. Every secret Streamline generates itself is 32 bytes or more
// before encoding.
const MinSessionSecretLen = 16

// checkSessionSecret holds a configured session secret to MinSessionSecretLen.
// files is what loadSecretFiles read; a session_secret_file missing from it
// was unreadable, which loadSecretFiles reports itself.
//
// Nothing configured is not a failure: the server generates a secret at boot.
// A session_secret_file that reads back empty is one: it is a secret that has
// not arrived, not a request to generate. A generated one would have nowhere
// to go, since the file wins in SecretValue and session_secret cannot be
// written beside it, so the instance would sign with the empty key.
func (c *Config) checkSessionSecret(files map[string]string) error {
	field, value := "auth.session_secret", c.Auth.SessionSecret
	if path := c.Auth.SessionSecretFile; path != "" {
		content, read := files[path]
		if !read {
			return nil
		}
		field, value = fmt.Sprintf("auth.session_secret_file %q", path), content
	} else if value == "" {
		return nil
	}
	switch n := len(strings.TrimSpace(value)); {
	case n == 0:
		return errors.New(
			field + ": the secret is empty: set one, or remove the key to have one generated",
		)
	case n < MinSessionSecretLen:
		return fmt.Errorf(
			"%s: the secret is shorter than %d bytes: set a longer one, or remove the key to have one generated",
			field,
			MinSessionSecretLen,
		)
	}
	return nil
}
