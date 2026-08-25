# Authentication and SSO

- [Auth modes](#auth-modes)
- [Transport split](#transport-split)
- [Roles and RBAC](#roles-and-rbac)
- [Sessions](#sessions)
- [API keys](#api-keys)
- [Registration and invites](#registration-and-invites)
- [Rate limiting and lockout](#rate-limiting-and-lockout)
- [OIDC](#oidc)
- [Reverse proxies](#reverse-proxies)

---

## Auth modes

`auth.mode` picks one of three postures.

### `full` (default)

Every request authenticates. This is the only mode appropriate for anything reachable from outside your network.

### `trusted-network`

Requests originating from a CIDR in `auth.trusted_networks` are **automatically granted `auth.trusted_role`** without credentials. Everything else authenticates normally.

```yaml
auth:
  mode: trusted-network
  trusted_networks:
    - 10.0.0.0/8
    - 192.168.1.0/24
  trusted_role: member
```

The client IP is resolved by chi's `ClientIPFromXFFTrustedProxies(1)` middleware — it derives the client from `X-Forwarded-For` **assuming exactly one trusted proxy in front of Streamline**.

That assumption is load-bearing. If Streamline is exposed directly, or sits behind a different number of proxies, or behind one that appends to a client-supplied `X-Forwarded-For` instead of overwriting it, **a client can forge a trusted source IP and be granted `trusted_role` with no credentials at all.**

Only use this mode when you control the proxy chain, and set `trusted_role` to the least-privileged role that does the job. The default is `admin`, which is rarely what you want here.

### `disabled`

No authentication whatsoever. Every request passes through unauthenticated.

Reasonable for local development. Never for anything else — this is not "auth handled by my proxy", it's *no identity at all*, so every request is anonymous and unattributable.

---

## Transport split

In `full` mode, the middleware picks its credential source by path prefix. This is deliberate: browsers and API clients have different threat models.

**`/api/v1/*`** accepts, in order:

1. `X-API-Key: <key>`
2. `Authorization: Bearer <jwt>`
3. The `streamline_session` cookie — **only** when the browser sends `Sec-Fetch-Site: same-origin`

Failure is a `401` with a JSON body.

That third case exists so the SPA can call the API without a second credential. It's gated twice: `SameSite=Lax` on the cookie already blocks cross-origin POSTs, and the `Sec-Fetch-Site` check additionally blocks cross-origin `GET`-via-`fetch`. The header being absent fails closed.

**Everything else** authenticates by session cookie only. Failure is a `302` to `/login?next=<escaped>`. Bearer tokens are ignored here.

### Unauthenticated paths

These bypass auth entirely:

| Path | Purpose |
| --- | --- |
| `/health` | Liveness/readiness probe. Bare JSON, not in the OpenAPI spec |
| `/login`, `/register` | SPA shell |
| `/auth/login`, `/auth/register` | The POST endpoints behind them |
| `/auth/oidc/` | Prefix match — the whole OIDC start/callback flow |

---

## Roles and RBAC

| Role | Rank | Scope |
| --- | --- | --- |
| `admin` | 3 | Everything |
| `member` | 2 | Library, downloads, approving requests. No settings |
| `request_only` | 1 | Create and view own requests only |

Roughly 76 API operations are admin-gated: all of `/config/*`, `/users/*`, `/indexers/*`, `/download-clients/*`, `/media-servers/*`, `/quality-profiles/*`, `/schedules/*`, `/library/*`, `/torrents/*`, `/activity/pending/*`, plus invites and system info.

On requests specifically:

| Operation | Required |
| --- | --- |
| Create a request | Any authenticated user |
| List requests | Any — but `request_only` users are scoped server-side to their own |
| Approve | `admin` or `member` |
| Deny, Reopen | `admin` |

Streamline refuses to delete or demote the **last remaining admin**.

---

## Sessions

Sessions are JWTs signed with HMAC using `auth.session_secret`, carried in the `streamline_session` cookie:

- `HttpOnly`
- `SameSite=Lax`
- `Secure` when the request arrived over TLS **or** carried `X-Forwarded-Proto: https`

Each JWT carries a `jti` matched against a server-side session row, so sessions are genuinely revocable rather than merely expiring. Every authenticated request touches the row asynchronously to refresh last-seen.

`auth.session_ttl` defaults to `168h`. A `purge-sessions` system job sweeps expired rows hourly.

### The signing secret

Generated on first boot and persisted to your config file via an atomic YAML write-back.

**If the config has no writable backing file, the secret is ephemeral** — regenerated at every start, invalidating all sessions on restart. Any deployment with a read-only config must supply `auth.session_secret` (or `auth.session_secret_file`) explicitly.

Rotating it logs everyone out. There's also `POST /api/v1/auth/jwt/rotate` for doing that deliberately.

---

## API keys

Created per user at **Account settings → API keys**, or `POST /api/v1/auth/me/api-keys`. Shown once at creation.

```bash
curl -H "X-API-Key: $KEY" https://streamline.example.com/api/v1/movies
```

An API key inherits the full permissions of its owning user — an admin's key is an admin key. Scripts that only need read access should use a key on a member account.

One carve-out: keys are **read-only on the identity surface**. Any non-GET request under `/api/v1/auth/me`, `/auth/password`, `/auth/invites`, `/auth/jwt`, or `/users` returns `403` when authenticated with a key — creating or revoking keys, changing passwords, managing sessions, administering users, and rotating the JWT secret all require a logged-in session (Bearer JWT or the browser cookie). A leaked key therefore can't mint replacement credentials or reshape accounts; it grabs and browses, nothing more. Media and settings endpoints are unaffected.

Admins can revoke any user's keys from Settings → Users.

---

## Registration and invites

`auth.registration_mode`, runtime-editable:

| Mode | Behaviour |
| --- | --- |
| `disabled` | No self-registration. Default |
| `invite` | Requires a valid invite token |
| `open` | Anyone can register |

An admin is **always** seeded on a fresh install, so the user table is never empty at request time. There is no first-user-registration special case to race.

Invites: `POST /api/v1/auth/invites` returns the raw token **once**. The SPA fetches `GET /auth/invite/{token}` to prefill the form; that lookup deliberately skips the email match so the page can render, while `RegisterWithInvite` enforces the binding atomically inside a transaction at submit time. Registration failures are mapped to user-safe messages — raw service errors are logged, never returned.

---

## Rate limiting and lockout

Two independent mechanisms.

**Per-IP rate limit** on login and registration: **5 attempts / 15 minutes**. Not configurable, not clearable. Wait it out.

**Per-account lockout**, configurable:

```yaml
auth:
  lockout:
    threshold: 10    # failures before locking
    window: 15m      # counted over this window
    duration: 15m    # lock lasts this long
```

Clear a lock from Settings → Users, or:

```bash
streamline auth unlock user@example.com
```

---

## OIDC

Multi-provider. Each entry is discovered at startup; **a provider whose discovery fails is skipped silently** — check the boot logs if one doesn't appear on the login page.

```yaml
auth:
  oidc:
    - name: authentik
      issuer: https://auth.example.com/application/o/streamline/
      client_id: streamline
      client_secret_file: /run/secrets/oidc-client-secret
      role_claim: groups
      role_mapping:
        streamline-admins: admin
        streamline-users: member
        family: request_only
```

### Flow

Authorization code with **PKCE (S256)**, plus state and nonce. All three are held in short-lived `_oidc_*` cookies scoped to `/auth/oidc/`.

Redirect URI:

```
<STREAMLINE_PUBLIC_URL or http://server.host:server.port>/auth/oidc/<name>/callback
```

Register that exact URI at your IdP, using the `name` you configured.

> The redirect URI is derived per-request from the host you connect on, so multi-domain SSO works without extra config — register each domain's callback at the IdP. `STREAMLINE_PUBLIC_URL` only sets the canonical base for invite links.

### Account linking

On callback, in order:

1. **Known `(provider, subject)`** → log that user in.
2. **`email_verified` is false** → reject (`oidc_email_unverified`). Streamline will not link on an unverified email; that would let anyone who can assert an address take over the matching account.
3. **Existing user with that (lowercased) email** → link the identity and promote `auth_method` from `local` to `both`.
4. **New user** → apply `registration_mode`:
   - `open` → create with `auth.oidc_default_role`
   - `invite` → consume the earliest unused, unexpired invite bound to that email; no match → `oidc_no_invite`
   - `disabled` → `oidc_registration_disabled`

### Role mapping

With `role_claim` and `role_mapping` both set, the claim is **authoritative** — the mapped role is applied on every login, so demotions in your IdP take effect. The claim value may be a string or an array; every value is checked and the **highest-privilege match wins** (`admin` 3 > `member` 2 > `request_only` 1).

With no mapping configured, new users get `auth.oidc_default_role` and existing users keep whatever role they have.

### Errors

Callback failures redirect to `/login?error=<code>`:

| Code | Meaning |
| --- | --- |
| `oidc_state_missing`, `oidc_state_mismatch`, `oidc_nonce_mismatch` | Flow cookies expired or were tampered with — usually just a stale tab |
| `oidc_email_unverified` | IdP reported the email as unverified |
| `oidc_registration_disabled` | New user, `registration_mode: disabled` |
| `oidc_no_invite` | New user, `invite` mode, no matching invite |
| `oidc_provider_error` | The IdP returned an error |

### Restart requirement

**OIDC providers are only loaded at process start.** UI edits persist but don't take effect until you restart. The Settings → SSO page says so; it's the most common OIDC support question.

---

## Reverse proxies

Forward the scheme, or the `Secure` cookie will be set and then never returned by the browser — producing a login that silently loops.

```nginx
location / {
    proxy_pass         http://streamline:8080;
    proxy_set_header   Host              $host;
    proxy_set_header   X-Forwarded-Proto $scheme;
    proxy_set_header   X-Forwarded-For   $proxy_add_x_forwarded_for;
    proxy_set_header   Upgrade           $http_upgrade;
    proxy_set_header   Connection        "upgrade";
}
```

Caddy and Traefik set these by default.

Set `STREAMLINE_PUBLIC_URL=https://streamline.example.com` so invite links are absolute and correct.

**If you use `trusted-network` mode behind a proxy**, ensure the proxy *overwrites* `X-Forwarded-For` rather than appending to a client-supplied value. Otherwise a client can forge a trusted source IP and be handed `trusted_role` without credentials.
