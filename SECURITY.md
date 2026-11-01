# Security Policy

## Supported versions

| Version              | Supported          |
| -------------------- | ------------------ |
| Latest release       | :white_check_mark: |
| Anything older       | :x:                |

Streamline is maintained by one person. Fixes land in a new patch release on top
of the latest version; older releases are not backported. Upgrade before
reporting.

## Reporting a vulnerability

**Do not open a public issue.**

Use GitHub's private vulnerability reporting:
[Report a vulnerability](https://github.com/datahearth/streamline/security/advisories/new).
If that is unavailable to you, email dev@antoine-langlois.net instead.

Please include:

- The version and how it is deployed (Docker, Helm, Nix, binary)
- What an attacker can do — read other users' data, escalate to admin, reach the
  host filesystem, etc.
- Steps to reproduce, ideally with the relevant config (secrets redacted)

Expect an acknowledgement within a week and a fix or a decision within 30 days
for anything exploitable. This is a spare-time project, not a vendor with an
on-call rotation — if something is actively being exploited, say so up front and
I will prioritise it. I will credit you in the advisory and the changelog unless
you'd rather stay anonymous.

Please give me a chance to ship a fix before disclosing publicly. There is no
bug bounty.

## Scope

In scope — anything that breaks the security model of a correctly configured
instance:

- Authentication or session bypass (`streamline_session` cookie, JWT, API keys,
  the Subsonic `/rest` and OPDS `/opds` credentials)
- Reaching more through a public share link (`/share/{token}`) than the tracks
  it was made for, or after its owner lost the right to share
- Privilege escalation between users, or past RBAC on `/api/v1/*`
- OIDC flow flaws — account takeover via identity linking, state/nonce/PKCE
  handling
- Path traversal or arbitrary write via import, rename, or media paths
- SSRF via indexer, download-client, media-server, or metadata endpoints, or
  the artwork proxies (`/posters/...`)
- SQL injection, stored XSS in the SPA, secrets leaking into API responses or
  logs
- Supply-chain issues in the release pipeline — signing, SBOM, image build

Out of scope:

- Exposing Streamline to the internet without TLS or a reverse proxy. It is
  built to sit behind one.
- The Subsonic protocol's own design. Its token scheme is `md5(password + salt)`,
  so the per-user Subsonic password is stored recoverable by necessity; it is
  separate from the account password and never accepted by the web UI or the
  REST API.
- Anything requiring existing admin credentials. Admins can already write config
  and read the library by design.
- Resource exhaustion from your own configuration — huge libraries, aggressive
  RSS intervals, an indexer returning millions of results.
- Vulnerabilities in Plex, Jellyfin, Emby, qBittorrent, Transmission, Deluge or
  Prowlarr themselves. Report those upstream.
- Dependency CVEs with no demonstrated path to exploitation in Streamline. A
  scanner's raw output is not a report; every image is already grype-scanned
  before it is tagged, and a fixable high or critical finding blocks the release.
- Missing hardening headers, cookie-flag nitpicks, or best-practice findings
  with no attack behind them.

## What's already in place

Release artifacts are signed with cosign (keyless, GitHub OIDC) and ship SPDX
SBOMs; images are signed, SBOM-attested and grype-scanned on every push. See
[Verifying images](README.md#verifying-images) for the verification commands.
