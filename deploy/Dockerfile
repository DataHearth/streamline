# syntax=docker/dockerfile:1.7

# --- Frontend assets (esbuild + TailwindCSS) ---
FROM node:24-alpine AS frontend
WORKDIR /app

RUN corepack enable

COPY package.json pnpm-lock.yaml ./
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

RUN pnpm exec routify build && \
    node web/app/esbuild.config.mjs

RUN pnpm exec tailwindcss \
    -i web/static/css/input.css \
    -o web/static/css/style.css \
    --minify

# --- Go codegen (oapi-codegen + ent via `go tool`) ---
FROM golang:1.26-alpine AS codegen
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

# --- Go binary ---
FROM golang:1.26-alpine AS builder
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

# --- Runtime ---
FROM debian:bookworm-slim
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates \
    && rm -rf /var/lib/apt/lists/* \
    && groupadd --gid 1000 streamline \
    && useradd --uid 1000 --gid 1000 --no-create-home streamline \
    && mkdir -p /data \
    && chown 1000:1000 /data
COPY --from=builder /out/streamline /usr/local/bin/streamline

EXPOSE 8080
VOLUME ["/data"]
# Numeric so Kubernetes runAsNonRoot can verify it without resolving the name,
# and the uid/gid match the chart's podSecurityContext defaults (1000).
USER 1000:1000
ENTRYPOINT ["/usr/local/bin/streamline"]
CMD ["--config", "/etc/streamline/config.yaml"]
