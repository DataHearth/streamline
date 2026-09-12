# syntax=docker/dockerfile:1.27

# Every base image is pinned by digest, not just by tag: a tag is mutable, so a
# tag-only pin means the bytes that ship can change without a commit. Refresh one
# with `docker buildx imagetools inspect <image>:<tag> --format '{{.Manifest.Digest}}'`.
#
# TRAP: where a digest sits next to a version ARG, the digest is what resolves —
# bumping the version alone silently keeps building the old image. Bump both.

# Pinned, not floating `1.26-alpine`: a moving tag made grype's stdlib findings
# drift between builds independently of our code. The cost is that stdlib CVE
# fixes now require bumping this by hand — grype failing on `stdlib` is the signal.
ARG GO_VERSION=1.26.6
ARG GO_DIGEST=sha256:af8d6740070b8906d12eae1c3e3ea0957fb63f492051ea05e354c38ef9fe88df

# static-ffmpeg publishes hardened static PIE builds with no external deps,
# which is what lets a bare COPY of the binaries into distroless (no package
# manager, no libc to link against) work at all.
ARG FFMPEG_VERSION=9.0
ARG FFMPEG_DIGEST=sha256:b90574a4e2ae62b763c39c384526689e7eb435da6398f4fb3f6c3f1c6a14ce33

# --- Frontend assets (esbuild + TailwindCSS) ---
FROM node:24-alpine@sha256:e67514e5d0f6c46656005e1b693b2ec9d52e80b641307de684d4a015ba7a4eaf AS frontend
WORKDIR /app

RUN corepack enable

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
FROM golang:${GO_VERSION}-alpine@${GO_DIGEST} AS codegen
WORKDIR /app

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
# so there's nothing to `apk add`/`apt install` ffmpeg into — see FFMPEG_DIGEST
# above. Pinned by digest like every other base image; bump the tag AND digest
# together.
FROM docker.io/mwader/static-ffmpeg:${FFMPEG_VERSION}@${FFMPEG_DIGEST} AS ffmpeg

# --- Go binary ---
FROM golang:${GO_VERSION}-alpine@${GO_DIGEST} AS builder
WORKDIR /app

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

# The runtime image has no shell to mkdir with, so stage the data dir here and
# copy it over with the right ownership.
RUN mkdir -p /out/data

# --- Runtime, VAAPI variant (`--target vaapi`, published as `<tag>-vaapi`) ---
# The default image below cannot hardware-encode: static-ffmpeg is built without
# libva (`-vaapi_device` is an unrecognized option in it), and even with it the
# VAAPI drivers are shared objects libva dlopen()s from a glibc install.
#
# No distro's packaged ffmpeg builds libvmaf in — checked Debian bookworm and
# trixie, Ubuntu noble, Alpine 3.22, and jellyfin-ffmpeg, none of which carry
# it (Debian doesn't package libvmaf at all: it's absent from main, contrib,
# non-free and non-free-firmware alike). The old Debian-bookworm base for this
# stage was therefore silently a no-op on `transcoding.verify.min_vmaf`: the
# key parsed and stored fine, the check just never ran. `linuxserver/ffmpeg`
# builds ffmpeg from source with libvmaf and VAAPI both compiled in, and
# bundles the VA drivers (Mesa, Intel iHD/i965, radeonsi) as shared objects
# under `LIBVA_DRIVERS_PATH`/`LD_LIBRARY_PATH`, already set in its env — so
# this stage stops installing anything and just builds on top of it. The
# trade is a ~780 MB Ubuntu base instead of a Debian install stripped to the
# render deps, and one more upstream (linuxserver) in the trust chain.
#
# The render node is root:render 0660 on most hosts. Nothing in the image can
# grant that group: pass it at run time with `--group-add <render gid>` (compose
# `group_add`, the chart's hwAccel.supplementalGroups), gid from
# `stat -c %g /dev/dri/renderD128`. This is why the stage sits ABOVE the
# distroless one: a plain `docker build` must still produce the hardened image.
FROM lscr.io/linuxserver/ffmpeg:9.0-cli-ls81@sha256:ec8ac72754e6c115d330ec37aa18b2ae1ba86a25b89ef46a4efd4f3e57d2e6c5 AS vaapi

# linuxserver's own ENTRYPOINT is an s6 wrapper (`/ffmpegwrapper.sh`) built for
# running ffmpeg as the container's job; streamline is a long-running server,
# so ENTRYPOINT/CMD below replace it entirely rather than layering on top.
# LIBVA_DRIVERS_PATH/LD_LIBRARY_PATH are inherited from the base image — do
# NOT unset them, ffmpeg resolves the VAAPI drivers through them.
#
# The digest pin holds the base layer steady; it does not hold back security
# updates. Grype scanning still runs against this image in CI, same as every
# other stage — the upgrade step below covers only what apt itself tracks
# (ca-certificates, tzdata), the rest of the base is linuxserver's to patch.
RUN apt-get update && \
    apt-get upgrade -y --no-install-recommends && \
    apt-get install -y --no-install-recommends \
      ca-certificates \
      tzdata && \
    rm -rf /var/lib/apt/lists/*

WORKDIR /

COPY --from=builder --chown=1000:1000 /out/data /data
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
FROM gcr.io/distroless/static-debian12:nonroot@sha256:1b7b9f0f0e0a1d2155f531db587cc48ec26aaf97ab64364225f5bf18a054e66a

# MUST stay. distroless sets WorkingDir=/home/nonroot (mode 0700, owned by
# 65532) where debian-slim left it empty, i.e. `/`. The binary's default
# data_dir is the RELATIVE `./data` — the exact value `streamline config init`
# writes — and `validate:"required,dir"` fails config load before anything
# else. Under /home/nonroot that resolves to a path uid 1000 cannot even
# create, so the container exits at boot instead of using the /data volume
# below. Drop this line and the documented `docker run -v streamline-data:/data`
# quick start breaks.
WORKDIR /

COPY --from=builder --chown=1000:1000 /out/data /data
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
