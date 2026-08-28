# Troubleshooting

Ordered roughly by how often each one bites people.

- [Searching finds nothing at all](#searching-finds-nothing-at-all)
- [Nothing ever gets grabbed](#nothing-ever-gets-grabbed)
- [Downloads finish but never import](#downloads-finish-but-never-import)
- [Permission denied on import](#permission-denied-on-import)
- [I can't log in](#i-cant-log-in)
- [Login loops back to the login page](#login-loops-back-to-the-login-page)
- [Settings are greyed out](#settings-are-greyed-out)
- [OIDC changes do nothing](#oidc-changes-do-nothing)
- [Torrents seed forever but upload nothing](#torrents-seed-forever-but-upload-nothing)
- [My media server doesn't notice new files](#my-media-server-doesnt-notice-new-files)
- [Database is locked](#database-is-locked)
- [Where the logs are](#where-the-logs-are)
- [Filing a good bug report](#filing-a-good-bug-report)

---

## Searching finds nothing at all

Not "finds no releases" — finds no *titles*. You type "Blade Runner" and get an empty list.

**You haven't set a metadata API key.** Streamline ships without one. No TMDB key, no movie search; no TVDB key, no TV search.

```yaml
metadata:
  tmdb_api_key: "..."
  tvdb_api_key: "..."
```

Restart afterwards. Keys are free — see [First-Run Setup](First-Run-Setup#2-get-metadata-api-keys).

**One long title finds nothing while part of it finds the show.** TVDB's search scores a show's own full title badly enough to return nothing at all — "Zom 100 Bucket List of the Dead" comes back empty, "Zom 100 Bucket List" returns the show. Streamline retries an empty search with each half of what you typed, which covers most of these, but a title that survives both halves needs help: type fewer words, add the year ("Baki 2018"), or search the original-language title, which TVDB ranks best of all.

---

## Nothing ever gets grabbed

Titles sit at **Wanted** forever. Work down this list in order.

**1. Is there an enabled indexer that passes its test?** Settings → Indexers → Test. A red result means Streamline can't reach it or the API key is wrong.

**2. Is there an enabled download client that passes its test?** Settings → Download clients → Test. With no working client, Streamline won't even search — all three automation jobs bail out immediately when there's nowhere to send a grab. This is the single most common cause, and it's silent unless you read the logs.

**3. Does a manual search return results?** Open the title → **Search**. If this comes back empty, the problem is upstream of Streamline: your indexers genuinely have nothing.

**4. Does a manual search return results that are never auto-grabbed?** Then your quality profile is rejecting them. Two rules do most of the rejecting:

- **A release whose title doesn't state a resolution is always rejected.** Streamline parses quality from the release name and refuses to guess.
- **With "upgrade allowed" off, only the exact preferred resolution is accepted.** A 2160p release is rejected by a 1080p-preferred profile just as firmly as a 480p one.

Loosen the profile, or grab the release manually — a manual grab bypasses the profile entirely.

**5. Has it been long enough?** RSS sync runs every 15 minutes; missing search every 12 hours. A title that failed to match once is also on a `library.no_match_cooldown` (6h default) before it's searched again. Force it with **Search now** on the title, or run the job from Settings → Schedules.

**6. Is it marked Failed?** After `library.max_grab_failures` (default 3) consecutive failures Streamline stops trying. **Search now** resets it.

---

## Downloads finish but never import

The torrent completes in your client, and then nothing.

**Check the queue first — it may be waiting on you.** A finished download whose file disagrees with what the release claimed is [held](Activity-and-Calendar#the-queue), not failed: **Activity → Queue** shows it as **Held** with the checks it failed, and nothing moves until you choose import / delete / delete-and-search. From the outside this looks identical to a stall, so rule it out before reading a single log line. If holds aren't something you want, the checks are all configurable — see [Import verification](Configuration-Reference#import-verification).

Otherwise this is nearly always a **path problem**, and the logs will name it.

**Streamline and your download client disagree about where files are.** Your client reports `/downloads/Some.Movie.2024/`; Streamline looks under its own `library.download_path` and finds nothing. In Docker, the two containers must see the same files at the *same paths*.

```yaml
# Both containers, identically:
volumes:
  - /srv/data:/srv/data
```

Then set `library.download_path: /srv/data/downloads` and configure your torrent client to save there.

**`invalid cross-device link`.** The classic. Hardlinks can't cross filesystems, and your downloads and media directories are on different ones — which, in Docker, includes being on the same disk but bind-mounted as two separate mounts.

Fix the mounts (one parent mount, per [the folder rule](Installation#before-you-start-the-folder-rule)), or change `library.import_mode` to `copy` or `move`.

**`destination already exists`.** A file is already at the target path. Streamline won't silently overwrite. Either delete the old file, or re-grab with **Replace existing files** ticked.

**`save_path not in allowed download roots`.** You've set `library.allowed_download_roots` and the torrent's save path isn't under any of them. This is a safety fence — it stops a compromised or misconfigured download client persuading Streamline to import from arbitrary paths. Add the correct root, or clear the list to disable the check.

**Season pack matched no episodes.** A pack was downloaded but Streamline couldn't map its files onto episodes you're missing — usually non-standard episode numbering. Import the files by hand via an [import scan](Importing-an-Existing-Library).

**An import scan entry fails with `only N of M files matched an episode`.** The folder is refused rather than adopted into the wrong show. Two things cause it. Either the folder really is a different show — accept a different match in the review list. Or the filenames carry numbering the show doesn't have: `S02E23` for a season with 22 episodes is a re-numbered DVD rip, and the guard is right to refuse it. Check the season and episode numbers on the series page against the filenames before treating it as a bug.

---

## Permission denied on import

Streamline runs as **uid/gid 1000** in the official image. If your media directories are owned by someone else, it can't write.

```bash
# Check
ls -ln /srv/data/media

# Either fix ownership
sudo chown -R 1000:1000 /srv/data

# Or run the container as the owning user
```

```yaml
services:
  streamline:
    user: "1001:1001"
```

Whatever you choose, your download client should run as the same user, or the hardlinks will be created but unreadable.

On Kubernetes this shows up as a crashloop with `unable to open database file (14)` — a root-owned RWO volume that the non-root container can't open. The chart sets `fsGroup: 1000` to handle it; if you've overridden `podSecurityContext`, put it back.

---

## I can't log in

**You never saw a password.** Streamline generated one and wrote it into your config file:

```bash
grep -A 3 seed_admin config/config.yaml
```

**Too many failed attempts.** Two separate limits apply. The account locks after 10 failures in 15 minutes; clear it with:

```bash
streamline auth unlock you@example.com
```

There's also a per-IP rate limit of 5 attempts per 15 minutes that is *not* clearable — wait it out.

**You've genuinely lost the admin password.** `auth.seed_admin` only acts when the user table is empty, so you can't use it to reset an existing install. Another admin can reset the password from Settings → Users. If there's no other admin, you're editing the database directly — stop the service and back up `data/` first.

---

## Login loops back to the login page

You log in, get redirected, and land back at the login form.

**You're behind a reverse proxy that isn't forwarding the scheme.** Streamline marks the session cookie `Secure` when it believes the connection is HTTPS, and browsers won't send a `Secure` cookie back over a connection they consider plain HTTP. If your proxy terminates TLS but doesn't tell Streamline, the cookie is set and then never returned.

Make sure your proxy sets `X-Forwarded-Proto`:

```nginx
proxy_set_header X-Forwarded-Proto $scheme;
proxy_set_header X-Forwarded-For   $proxy_add_x_forwarded_for;
proxy_set_header Host              $host;
```

Traefik and Caddy do this by default.

**Streamline ignores `X-Forwarded-*` from an untrusted peer.** Sending the header is only half of it: `server.trusted_proxies` is empty by default, which trusts nobody, so the headers are dropped and the connection still looks like plain HTTP. List the proxy itself, as narrowly as you can:

```yaml
server:
  trusted_proxies:
    - 10.42.0.7/32   # the ingress/proxy pod or host, not the client subnet
```

Naming a range clients can also occupy (a pod CIDR, `192.168.0.0/16`) makes every host in it a proxy as far as this gate is concerned.

**Clock skew.** Sessions are JWTs with time-based validity. A container clock badly out of sync will reject tokens the moment they're issued.

---

## Settings are greyed out

A banner reads *"This instance is configured externally and runs read-only."*

That's `read_only: true`. Every runtime config write is refused deliberately — config comes from your config file (or git), not the UI. The Helm chart sets it by default.

Edit your config file and restart, or set `read_only: false` if you'd rather manage things from the UI. See [GitOps and Kubernetes](GitOps-and-Kubernetes).

---

## OIDC changes do nothing

**OIDC providers are only discovered at process start.** Adding or editing one in the UI saves it, but the provider isn't live until you restart. The Settings → SSO page says so, but it's easy to miss.

Also: **a provider whose discovery fails at startup is skipped silently.** If your IdP was down, or the issuer URL is wrong, the provider simply won't appear on the login page and no error is shown in the UI. Check the startup logs.

The redirect URI to register at your IdP is:

```
<your-public-url>/auth/oidc/<provider-name>/callback
```

using the exact `name` you gave the provider.

**"Invalid parameter: redirect_uri" from the IdP.** The URI Streamline sends is built per-request from the scheme and host the request arrived on, so behind a TLS-terminating proxy it comes out as `http://…` — never matching the `https://…` you registered — unless `server.trusted_proxies` names that proxy. Same fix as [Login loops back to the login page](#login-loops-back-to-the-login-page). `STREAMLINE_PUBLIC_URL` does **not** override it; it only sets the base for invite links.

Read the failing `redirect_uri` straight out of the query string on the IdP's error page to see exactly what was sent.

---

## Torrents seed forever but upload nothing

Downloads work, the built-in engine reports torrents as **Seeding**, and upload sits at `0 B/s` with **Aggregate ↑** showing `—`. Every seeding torrent shows 0 seeds and 0 peers.

That combination — outbound fine, inbound dead — means **nothing can open a connection to you**. Your own dials build NAT state on the way out, so trackers and downloads are unaffected; incoming connections have no state to match and need an explicit forward.

Behind a commercial VPN this is the usual cause, and it has a specific trap:

- **The engine's port defaults to 42069** when `listen_port` is unset. No provider forwards that by coincidence, so a forward you set up elsewhere points at a port nothing is listening on.
- **Both halves must agree.** A forward on port X does nothing while the engine listens on 42069, and setting the port does nothing without the forward.
- **A per-session forwarded port can't live in the config file.** Providers reassign it on reconnect. Use [`torrent_listen_port`](Configuration-Reference#torrent_listen_port) for the initial bind, and have your VPN sidecar call `PUT /api/v1/torrents/listen-port` on every reassignment — no restart needed.

Check what the engine actually bound:

```bash
curl -sS -H "X-API-Key: $KEY" "$SL/api/v1/download-clients" \
  | jq '.[] | select(.client_type=="builtin") | {listen_port, port_bound, interface_bound}'
```

If `port_bound` is `42069` and `listen_port` is `0`, nothing ever told the engine which port is forwarded. If it reports the forwarded port and upload is still zero, the forward itself isn't reaching the tunnel address in `interface_bound` — verify from outside with `nc -vz <exit-ip> <port>`.

Allow **both TCP and UDP** on that port: BitTorrent uses TCP, uTP and DHT use UDP, all on the same number. A TCP-only rule looks like it half-works. Most VPN sidecars that manage forwarding open both themselves.

After a fix, give it 10–20 minutes — peers have to find you through tracker and DHT announces before they can connect.

---

## My media server doesn't notice new files

Streamline pokes Plex/Jellyfin/Emby to rescan on import. If nothing happens:

- **Test the connection.** Settings → Media servers → Test.
- **Check your media server can see the files.** Streamline importing successfully doesn't mean Plex has the path mounted. They're separate containers with separate mounts.
- **For Plex, set both section keys.** Settings → Media servers → **Discover**, then fill in **Movie library section** and **TV library section**. Left blank, Streamline falls back to rescanning every section — so the scan does happen, but it is broader than it needs to be, and a log line will say so:

  ```
  WARN plex: no section matches the library path, refreshing all sections
  ```

  That message is expected on any containerised install: Plex reports its *own* mount paths, which don't match Streamline's. Setting the two keys is the fix.

Check the log for the import itself too. A refresh that failed outright logs at WARN and never stops the import, so the file lands on disk with nothing to show for it:

```
WARN media server refresh failed  name=plex  error=...
```

Failing that, media servers have their own scan schedules and will find the files eventually.

---

## My library emptied itself after a remount

Streamline stores absolute paths. If the mount moves — the claim was at
`/mnt/media-shared` and is now at `/srv`, a bind mount changed, a chart value
was edited — every stored path dangles. The files are fine; the records point
at nothing. The `drift-check` job then removes those records once
`drift_grace_ticks` elapses, so left alone this becomes real data loss.

Streamline logs a `CRITICAL` line at boot when a configured root matches none
of the paths stored for it:

```
CRITICAL library root does not match any stored path — records will be pruned
         library.root=movies library.path=/srv/streamline/movies records.total=621
```

Fix it by re-rooting rather than re-importing. Check what the server sees:

```bash
curl -sS -H "X-API-Key: $KEY" "$SL/api/v1/library/path-migration/roots"
```

`tracked: 0` with a non-zero `total` is the divergence. Preview, then run:

```bash
curl -sS -X POST -H "X-API-Key: $KEY" -H 'Content-Type: application/json' \
  -d '{"root":"movies","from":"/mnt/media-shared/movies","to":"/srv/streamline/movies"}' \
  "$SL/api/v1/library/path-migration/preview"
```

Drop `/preview` to apply. Add `"move_files": true` only if the files also need
relocating; without it Streamline expects them to already be at the new path
and just rewrites the records. Nothing on the server remembers the old prefix,
so you have to name it yourself.

The `downloads` root is the softer case: nothing prunes download records on
drift, so its warning reads `downloads cannot import` instead — the risk is
in-flight downloads failing to import, not data loss. Only live rows
(downloading, importing, held, pending adoption proposals) count toward it;
finished history can't hold the warning on after the root legitimately moved.

---

## Database is locked

Streamline uses SQLite, which allows exactly one writer.

**You're running more than one replica.** Don't. `replicaCount` must stay 1.

**Two processes share one data directory.** An old service still running, or a stray container.

**Your data directory is on a network filesystem.** SQLite locking over NFS or SMB is unreliable and will corrupt your database. Put `data_dir` on local storage. Your *media* can live on the network; your database can't.

The database is `<data_dir>/streamline.db`, accompanied by `-wal` and `-shm` files. If you ever copy it, copy all three.

---

## Where the logs are

Docker: `docker compose logs -f streamline`. systemd: `journalctl -u streamline -f`. Kubernetes: `kubectl logs -n streamline deploy/streamline -f`.

Turn up the detail:

```yaml
log:
  app:
    level: debug
```

For a full treatment — file output, rotation, JSON, and OpenTelemetry — see [Observability and Logging](Observability-and-Logging).

---

## Filing a good bug report

[Open an issue](https://github.com/datahearth/streamline/issues) with:

- Version and build info — **Settings → General** shows it, and it's there for exactly this reason
- How you're running it (Docker / binary / Helm) and on what
- Your config with secrets removed
- The relevant log lines, at `debug` if you can

Validate your config before reporting a startup failure — it often answers the question outright:

```bash
streamline config validate --config /etc/streamline/config.yaml
```

Found a **security** vulnerability? Don't open an issue — follow [SECURITY.md](https://github.com/datahearth/streamline/blob/main/SECURITY.md).
