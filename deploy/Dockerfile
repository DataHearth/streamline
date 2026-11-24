# syntax=docker/dockerfile:1.29

# Every base image is pinned by digest, not just by tag: a tag is mutable, so a
# tag-only pin means the bytes that ship can change without a commit. Refresh one
# with `docker buildx imagetools inspect <image>:<tag> --format '{{.Manifest.Digest}}'`.
#
# TRAP: the digest is what resolves — a tag moved without its digest silently
# keeps building the old image. Renovate's dockerfile manager moves both, which
# it can only do for a literal `image:tag@digest`, never one assembled from ARGs.

# --- Frontend assets (esbuild + TailwindCSS) ---
FROM node:26.13.1-alpine@sha256:6bd9ab43c0ec0190ca7b2d193906cb8b7edf6e7f4afa43ddeccd6452710bffb5 AS frontend
WORKDIR /app

# Node ships no corepack from 25 on, so pnpm comes from npm. It is
# package.json's packageManager version: renovate moves the two in one PR and
# renovate-pins.yaml fails a branch where they differ. Its install script swaps
# the Node.js entry points for the native binary, which npm runs only when
# allowed.
ARG PNPM_VERSION=12.21.0
RUN npm install -g --allow-scripts=pnpm pnpm@${PNPM_VERSION}

COPY package.json pnpm-lock.yaml pnpm-workspace.yaml ./
RUN --mount=type=cache,id=pnpm,target=/root/.local/share/pnpm/store \
    pnpm install --frozen-lockfile

COPY web/ web/
COPY routify.config.js ./

RUN mkdir -p /tmp/bundle web/static/dist && \
    pnpm exec esbuild web/static/js/docs.js \
      --bundle --minify \
      --outdir=/tmp/bundle --entry-names=[name].min && \
    mv /tmp/bundle/docs.min.js web/static/js/docs.min.js && \
    mv /tmp/bundle/docs.min.css web/static/css/docs.min.css

# Keep in sync with the `build:js` task — paraglide writes its own `.gitignore`
# into the outdir, so `web/app/lib/paraglide/` never reaches the build context.
RUN pnpm exec paraglide-js compile --project ./web/app/project.inlang \
      --outdir ./web/app/lib/paraglide --is-server false \
      --emit-ts-declarations \
      --strategy localStorage preferredLanguage baseLocale && \
    pnpm exec routify build && \
    node web/app/esbuild.config.mjs

RUN pnpm exec tailwindcss \
    -i web/static/css/input.css \
    -o web/static/css/style.css \
    --minify

# --- Go codegen (oapi-codegen + ent via `go tool`) ---
# Pinned, not floating `1.27-alpine`: a moving tag makes grype's stdlib findings
# drift between builds independently of our code. The same tag and digest as the
# builder stage; renovate moves the two together.
FROM golang:1.27.4-alpine@sha256:70bd7ddea8b6cbe5d6943cde279d6b67feb10cc924fb9100d1d8757c1575b280 AS codegen
WORKDIR /app

# "|" falls back to a module's origin on any proxy error, where the default
# "," does so only on 404 and 410. The fallback clones over git, which the
# alpine image does not ship. A fetched module is still checked against go.sum.
ENV GOPROXY="https://proxy.golang.org|direct"
RUN apk add --no-cache git

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY api/ api/
COPY ent/ ent/
COPY internal/ internal/
COPY cmd/ cmd/

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go tool oapi-codegen --config api/oapi-codegen.yaml api/openapi.yaml && \
    go generate ./ent

# --- ffprobe (media probe) ---
# The runtime stage below is distroless static: no shell, no package manager,
# so there's nothing to `apk add`/`apt install` ffmpeg into. static-ffmpeg
# publishes hardened static PIE builds with no external deps, which is what lets
# a bare COPY of the binaries into distroless (no libc to link against) work at
# all.
FROM docker.io/mwader/static-ffmpeg:9.0.2-1@sha256:f5d100184884eb3e82b0eb14bb5ebf98da99826ac824989720297fb9ef21c822 AS ffmpeg

# --- Go binary ---
FROM golang:1.27.4-alpine@sha256:70bd7ddea8b6cbe5d6943cde279d6b67feb10cc924fb9100d1d8757c1575b280 AS builder
WORKDIR /app

# Same fallback as the codegen stage: the module cache mount can start empty,
# and this build then downloads what it needs.
ENV GOPROXY="https://proxy.golang.org|direct"
RUN apk add --no-cache git

COPY --from=codegen /app /app
COPY --from=frontend /app/web /app/web

ARG VERSION=dev
ARG COMMIT=none
ARG BUILD_DATE=unknown

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build \
      -trimpath \
      -ldflags="-s -w -X github.com/datahearth/streamline/internal/buildinfo.Version=${VERSION} -X github.com/datahearth/streamline/internal/buildinfo.Commit=${COMMIT} -X github.com/datahearth/streamline/internal/buildinfo.Date=${BUILD_DATE}" \
      -o /out/streamline ./cmd

# The runtime image has no shell to mkdir with, so stage the data and config
# dirs here and copy them over with the right ownership. The config dir is not
# a VOLUME, but a fresh named volume mounted there copies this ownership: owned
# by root, it would refuse the config file Streamline writes on first start.
RUN mkdir -p /out/data /out/config

# --- Runtime, VAAPI variant (`--target vaapi`, published as `<tag>-vaapi`) ---
# The default image below cannot hardware-encode: static-ffmpeg is built without
# libva (`-vaapi_device` is an unrecognized option in it), and even with it the
# VAAPI drivers are shared objects libva dlopen()s from a glibc install.
#
# No distro's packaged ffmpeg builds libvmaf in — checked Debian bookworm and
# trixie, Ubuntu noble, Alpine 3.22, and jellyfin-ffmpeg, none of which carry
# it (Debian doesn't package libvmaf at all: it's absent from main, contrib,
# non-free and non-free-firmware alike). A Debian base for this stage is
# therefore silently a no-op on `transcoding.verify.min_vmaf`: the key parses
# and stores fine, the check just never runs. `linuxserver/ffmpeg`
# builds ffmpeg from source with libvmaf and VAAPI both compiled in, and
# bundles the VA drivers (Mesa, Intel iHD/i965, radeonsi) as shared objects
# under `LIBVA_DRIVERS_PATH`/`LD_LIBRARY_PATH`, already set in its env — so
# this stage installs nothing for ffmpeg and just builds on top of it. The
# trade is a ~780 MB Ubuntu base instead of a Debian install stripped to the
# render deps, and one more upstream (linuxserver) in the trust chain.
#
# The render node is root:render 0660 on most hosts. Nothing in the image can
# grant that group: pass it at run time with `--group-add <render gid>` (compose
# `group_add`, the chart's hwAccel.supplementalGroups), gid from
# `stat -c %g /dev/dri/renderD128`. This is why the stage sits ABOVE the
# distroless one: a plain `docker build` must still produce the hardened image.
FROM lscr.io/linuxserver/ffmpeg:9.0.2-cli-ls89@sha256:822d04f0b2d0c4847430f5cc90f20ea48be866bb502ece63fdeb729400bf5361 AS vaapi

# linuxserver's own ENTRYPOINT is an s6 wrapper (`/ffmpegwrapper.sh`) built for
# running ffmpeg as the container's job; streamline is a long-running server,
# so ENTRYPOINT/CMD below replace it entirely rather than layering on top.
# LIBVA_DRIVERS_PATH/LD_LIBRARY_PATH are inherited from the base image — do
# NOT unset them, ffmpeg resolves the VAAPI drivers through them.
#
# The digest pin holds the base layer steady; it does not hold back security
# updates. Grype scanning still runs against this image in CI, same as every
# other stage — the upgrade step below takes every dpkg package (curl, libx11,
# libxml2…) to what the Ubuntu archive has patched; only what sits outside dpkg
# (ffmpeg itself, the bundled VA drivers) is linuxserver's to patch.
#
# /usr/bin/pebble is a Go binary the Ubuntu base drops in outside dpkg. Nothing
# here runs it, but grype reports the stdlib it was built with as if it were
# ours — a lagging Go toolchain there fails the scan on `stdlib` while
# streamline itself is built by the pinned, patched image. Deleting the file is
# the only way to take it off the report; no package owns it to purge.
#
# BUILD_DATE is declared here only to keep this layer out of the build cache.
# image.yaml restores every layer from the GHA cache, and a cached upgrade is
# the upgrade from whenever the layer was first built: it can hold a curl days
# behind the one resolute-security ships for a CVE, and grype then fails both
# vaapi builds on a fix the archive has.
# A changed ARG misses the cache at the first RUN after it, so CI (which passes
# a timestamp) upgrades on every build; a plain `docker build` leaves it at
# `unknown` and keeps its cache.
ARG BUILD_DATE=unknown
RUN apt-get update && \
    apt-get upgrade -y --no-install-recommends && \
    apt-get install -y --no-install-recommends \
      ca-certificates \
      tzdata && \
    rm -rf /var/lib/apt/lists/* /usr/bin/pebble

WORKDIR /

COPY --from=builder --chown=1000:1000 /out/data /data
COPY --from=builder --chown=1000:1000 /out/config /etc/streamline
COPY --from=builder /out/streamline /usr/local/bin/streamline

EXPOSE 8080
VOLUME ["/data"]
USER 1000:1000
ENTRYPOINT ["/usr/local/bin/streamline"]
CMD ["--config", "/etc/streamline/config.yaml"]

# --- Runtime ---
# distroless static: no shell, no package manager, no libc — nothing an attacker
# who lands RCE can pivot through. It still carries the only OS files the app
# needs: ca-certificates (outbound TMDB/TVDB/indexer HTTPS), tzdata (schedules
# and air dates), /etc/passwd and a world-writable /tmp. Everything the app
# serves is baked into the binary via go:embed. Requires CGO_ENABLED=0 above.
# The :nonroot tag keeps the default user non-root even if USER below is lost.
FROM gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab

# MUST stay. distroless sets WorkingDir=/home/nonroot (mode 0700, owned by
# 65532), not `/`. The binary's default data_dir is the RELATIVE `./data` —
# the exact value `streamline config init` writes — and config load creates
# data_dir (ensureDataDir) before anything else, failing the boot when it
# cannot. Under /home/nonroot that resolves to a path uid 1000 cannot create,
# so the container exits at boot instead of using the /data volume below.
# Drop this line and the documented `docker run -v streamline-data:/data`
# quick start breaks.
WORKDIR /

COPY --from=builder --chown=1000:1000 /out/data /data
COPY --from=builder --chown=1000:1000 /out/config /etc/streamline
COPY --from=builder /out/streamline /usr/local/bin/streamline

# /usr/local/bin is on distroless' default PATH, so this satisfies
# ffmpeg.path's default (empty = resolve via $PATH) with no config needed.
COPY --from=ffmpeg /ffmpeg /ffprobe /usr/local/bin/

EXPOSE 8080
VOLUME ["/data"]
# Numeric so Kubernetes runAsNonRoot can verify it without resolving the name,
# and the uid/gid match the chart's podSecurityContext defaults (1000). Kept at
# 1000 rather than distroless' own nonroot uid (65532) because existing /data
# volumes and bind-mounted media libraries are already owned by 1000 — adopting
# 65532 would lock running installs out of their database.
USER 1000:1000
ENTRYPOINT ["/usr/local/bin/streamline"]
CMD ["--config", "/etc/streamline/config.yaml"]
