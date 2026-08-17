# Review — PR #42: `ci: enable gosec repo-wide and fix the findings it surfaces`

**Head reviewed:** `518b9c6` (`ci/gosec`) · **Base:** `main` @ `a875085` · 61 files, +762/−381, 24 commits

## Verdict

The PR does what it says. No correctness bug found in the diff. Three minor issues below, all in the "inconsistency / inaccurate comment" class rather than defects.

**Verification performed** — worktree at `518b9c6`, `go:embed` inputs seeded:

| Check | Result |
| --- | --- |
| `go build ./...` | clean |
| `go vet` (restapi, movie) | clean |
| `ginkgo --label-filter=unit ./internal/server/restapi/...` | 254 passed, 0 failed, 1 skipped |

## What holds up

- **The page-overflow fix is genuine.** `runtime.BindStringToObject` (oapi-codegen runtime v1.6.0) does call `OverflowUint`, so `?page=70000` now fails at bind time and reaches the new `ErrorHandlerFunc` (`restapi/server.go:126`) as a JSON 400, instead of wrapping into `uint16` and silently serving the wrong rows. The reported live bug was real and is fixed.
- **Pagination defaults are consistent with the spec.** Every `positiveOr` default matches its spec `default` (20/20/50/20/50/50), and every `clampLimit` ceiling matches the spec `maximum`. No `page-1` underflow is reachable — each is guarded by a `>= 1` value.
- **`diskUsage` arithmetic is sound.** The divide-first branch cannot divide by zero (`used > MaxInt64/100` implies `total > 100`), and the `used` clamp correctly precedes `humanBytes`, so the `"-524288000 B"` rendering is genuinely gone.
- **The rest of the sweep checks out:** `int`-ification of counts, `clampUint16`, the `strconv.ParseUint` swap (regex is `S(\d{2})`, so it is a behavioural no-op), the 0750/0755 directory split, and the multi-line `//nolint` groups expanding to the following statement.

## Findings

### 1. Over-max `limit` gets two different answers — `internal/server/restapi/handler_movies.go:33`

`limit` is declared `x-go-type: uint16` with `maximum: 100`. The cutoff that decides the response is the **Go type**, not the spec bound:

- `?limit=500` → binds fine → silently clamped to 100 → **200 OK**
- `?limit=70000` → overflows `uint16` at bind → **400**

Both violate `maximum: 100` identically. The 400 body is `err.Error()` verbatim, which leaks the Go type (`value 70000 out of range for uint16`) to API consumers. Same shape on `/requests` (`uint32`) at a different threshold.

This sits awkwardly against the PR's own stated principle — "one pagination strategy everywhere — reject". `page=0` and `page=70000` both reject; `limit=500` does not. Either clamp both ends or reject both.

**Severity:** minor — no wrong data served, just an inconsistent contract and a leaked internal type.

### 2. `nolint` reason is factually wrong — `internal/server/web/auth.go:486`

```go
//nolint:gosec // url is the configured provider's AuthCodeURL; the URL
// param only selects which configured provider, it never enters the URL
http.Redirect(w, r, url, http.StatusFound)
```

The `{name}` param **does** enter the URL — `oidcRedirectURI(r, name)` two lines above interpolates it straight into the `redirect_uri` query parameter (`auth.go:462`).

The code is safe, but for a different reason than stated: `name` already passed `h.oidc.Get(name)` at `auth.go:471`, which 404s unknown providers, so it can only ever be a configured provider name.

This matters more than usual here because the same PR turns on `nolintlint` with `require-explanation`. The explanations are now a checked artifact, so they should be accurate.

### 3. `nolint` rationale does not cover all callers — `internal/library/import.go:440`

```go
//nolint:gosec // src is a download-client path; dst was template-rendered
// through SanitizePath and the ErrUnsafePath root check in placeFile
```

`copyFile` has a second caller path: `MoveFile` (`import.go:421`) → called from `pathmigrate.go:281`. That path builds `to` by string-rewriting stored DB paths against operator-configured `From`/`To` (`pathmigrate.go:406-450`) — it never runs `SanitizePath` and never hits the `ErrUnsafePath` root check.

Operator-driven input, so not exploitable, but the justification as written is false for that caller. Worth either widening the comment or noting the `pathmigrate` path explicitly.

## Correction to an earlier draft of this review

An earlier pass flagged `limit=0` as still silently coerced on `/activity/*`, `/users` and `/dashboard`. **That finding was valid against `c8b5f14`** and was fixed mid-review by `518b9c6` ("finish the zero-limit sweep on activity, history and users"), which routes all three through `positiveOr`, widens it to `~int`, rejects `<= 0`, and adds the missing `minimum: 1` plus a declared 400 to the `/users` spec. Verified fixed at the current head.

I initially reported this as "refuted" because I fetched the PR ref after that commit landed while still reading the older PR metadata. The finding was real; it is now resolved.

## Note on the new commit (`518b9c6`)

Reviewed on its own: correct and self-consistent. `positiveOr` widening to `~int` is needed because the activity params bind as plain `int` (so negatives were reachable too). The absent-limit path still yields `0`, which `db/user.go:154` substitutes with 25, matching the spec's `default: 25`. The `internal/media/movie/service.go` hunk is a comment correction only.
