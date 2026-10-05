---
name: verify
description: Use when confirming a streamline change works in the real app rather than only in tests, before reporting a fix or feature done, or when asked to run, start, screenshot or drive the app.
---

# Verifying streamline in a throwaway instance

Build the current tree and boot **your own** instance: scratch data dir, scratch
config, its own port, its own admin. Drive the change through it, then tear it
down. Tests exercise functions. The running app exercises the wiring (the
scheduler, the builtin engine, migrations, the embedded SPA), and that's where
the bugs that pass tests show up.

Never touch a server you did not start. Someone may already be running one on
:8080 against `./tmp/config.yaml`, and that process and its data belong to them.

## 1. Toolchain

The environment is the user's to set up: Go, Node, `task`, `pnpm` on PATH and
`node_modules` installed. Don't install any of them. If one is missing, stop
and say which one, so the user can fix the environment.

If a hook blocks a bare `go`, call it as `"$(command -v go)"`.

## 2. Boot

Shell state does not survive between Bash calls, so run this as one block and
reuse the printed `S`, `PORT`, `TRACKER_HOST` and `TRACKER` literally from then
on.

The fake tracker must not listen on loopback. Its magnets announce to the
address it listens on, and the builtin engine refuses a tracker on loopback or
link-local (`internal/bittorrent/egress.go`), so on `127.0.0.1` every torrent
sits at "fetching" with no peers and nothing is ever downloaded. Prefer the
docker bridge, which only this machine can reach; otherwise take the address the
default route leaves from.

```sh
mkdir -p "${TMPDIR:-/tmp}/streamline"; S=$(mktemp -d "${TMPDIR:-/tmp}/streamline/verify.XXXXXX"); mkdir -p "$S"/{data,downloads,movies,series}
PORT=18080; while curl -s -m1 -o /dev/null "127.0.0.1:$PORT"; do PORT=$((PORT+1)); done
TRACKER_HOST=$(ip -4 -o addr show docker0 2>/dev/null | awk '{sub("/.*", "", $4); print $4}')
[ -n "$TRACKER_HOST" ] || TRACKER_HOST=$(ip -4 route get 1.1.1.1 | awk '{for (i = 1; i < NF; i++) if ($i == "src") print $(i+1)}')
TRACKER=$((PORT+1)); while curl -s -m1 -o /dev/null "$TRACKER_HOST:$TRACKER"; do TRACKER=$((TRACKER+1)); done
TMDB=${STREAMLINE_METADATA__TMDB_API_KEY:-$(sed -n 's/^ *tmdb_api_key: *//p' tmp/config.yaml 2>/dev/null | head -1)}
TVDB=${STREAMLINE_METADATA__TVDB_API_KEY:-$(sed -n 's/^ *tvdb_api_key: *//p' tmp/config.yaml 2>/dev/null | head -1)}
cat > "$S/config.yaml" <<EOF
data_dir: $S/data
server: {host: 127.0.0.1, port: $PORT}
auth:
  session_secret: verify-session-secret-0123456789abcdef
  seed_admin: {email: verify@streamline.local, password: Verify-Passw0rd!}
library: {download_path: $S/downloads, movie_path: $S/movies, series_path: $S/series}
metadata: {tmdb_api_key: "$TMDB", tvdb_api_key: "$TVDB"}
download_clients:
  - {name: builtin, client_type: builtin, enabled: true, download_dir: $S/downloads}
indexers:
  - {name: faketracker, protocol: torznab, host: $TRACKER_HOST, port: $TRACKER, api_key: fake-key, enabled: true}
EOF
task build:app BINARY="$S/streamline" && echo "S=$S PORT=$PORT TRACKER_HOST=$TRACKER_HOST TRACKER=$TRACKER"
```

Then start it with `run_in_background`. The `env -u` strips every inherited
`STREAMLINE_*` variable, since the environment overrides the file and a
leftover `STREAMLINE_DATA_DIR` would point the instance at real data:

```sh
env $(env | sed -n 's/^\(STREAMLINE_[A-Z0-9_]*\)=.*/-u \1/p') "$S/streamline" --config "$S/config.yaml" > "$S/server.log" 2>&1
```

Wait for `curl -sf 127.0.0.1:$PORT/health` before going further. When it fails,
the answer is in `$S/server.log`.

No TMDB/TVDB key resolved? The instance runs, but adding or refreshing a title
fails. Say so in the report instead of passing a check that never ran. A cloud
environment supplies the keys as `STREAMLINE_METADATA__TMDB_API_KEY` /
`STREAMLINE_METADATA__TVDB_API_KEY` secrets.

## 3. Drive the change

Log in. `/api/v1/*` rejects the cookie alone, so send the session JWT as a Bearer token:

```sh
TOKEN=$(curl -s -o /dev/null -c - -H 'Content-Type: application/json' \
  -d '{"email":"verify@streamline.local","password":"Verify-Passw0rd!"}' \
  127.0.0.1:$PORT/auth/login | awk '$6=="streamline_session"{print $7}')
curl -s -H "Authorization: Bearer $TOKEN" 127.0.0.1:$PORT/api/v1/...
```

| Change | How |
|---|---|
| REST / service logic | `curl` with the Bearer token. The spec is `api/openapi.yaml` |
| UI (`web/app/**`) | A browser tool if the session has one (Playwright MCP, Chrome); else `go run ./.claude/skills/verify/shot -url http://127.0.0.1:$PORT -out "$S" /movies /settings/indexers` (add `-width 390` for a phone). It screenshots each path and prints console errors, exceptions and failed requests (a 409 from `/transcoding/queue` only means transcoding is off). **Read every PNG** before judging |
| Grab / download / import | `go run ./e2e/faketracker -listen $TRACKER_HOST:$TRACKER -data "$S/tracker" -magnets -pack "Reacher:1:8"` in the background. It is the instance's indexer already, and `-magnets` skips the indexer-trust check |
| Background job | `POST /api/v1/schedules/{name}/run`. `GET /api/v1/schedules` lists the names |
| DB state | `sqlite3 "$S/data/streamline.db"`, for reading only |

The SPA is embedded in the binary. After editing `web/app/**`, rerun the build,
kill the instance and boot it again on the same `S`.

**State machines:** seed the state, then wait for
`scheduled_jobs.last_finished_at` to move **past** the seeding time before
snapshotting. Don't trust a poll that raced your own write.

## 4. Prove it ran

Include a control: something that *should* change next to something that
should not. A check that sees nothing happen hasn't shown the code ran, so it
doesn't count as a pass.

## 5. Tear down and report

Stop the instance and the faketracker, then `rm -rf "$S"`. With
`S=/tmp/streamline/verify.AbC123`:

```sh
pkill -f 'verify\.AbC123/(streamline|tracker)'
```

Write the pattern as a regex, not as `pkill -f "$S/"`: the shell running pkill
has the literal path in its own command line, so the plain form kills that
shell too and the step reports exit 144. The escaped dot and the alternation
match the instance and the tracker, never the text of the pattern itself.

Report what you drove, the evidence (responses, screenshots read), and anything
left unverified with the reason.
