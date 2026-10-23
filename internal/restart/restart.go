// Package restart tracks whether the running process has accumulated
// configuration changes that require a restart to take effect. Kept in its
// own package (not config) so any request handler — webui, restapi, OIDC —
// can flip the flag without pulling in unrelated config dependencies.
//
// Current users: OIDC provider mutations (add/update/delete). Session TTL
// and registration_mode take effect immediately and do NOT mark the flag.
package restart

import "sync/atomic"

var pending atomic.Bool

// Mark records that the process now holds unapplied config. The flag is
// process-scoped: it resets to false on restart.
func Mark() { pending.Store(true) }

// Pending reports whether a restart is currently required.
func Pending() bool { return pending.Load() }

// ResetForTest clears the flag. Tests only.
func ResetForTest() { pending.Store(false) }
