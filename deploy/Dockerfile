# syntax=docker/dockerfile:1
# Marquee: single image with the Go server, embedded web client and jellyfin-ffmpeg.
# Build from the repository root:  docker build -f deploy/Dockerfile -t marquee .

# ---- web client ----
FROM node:24-trixie-slim AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY api /src/api
COPY web ./
RUN npm run build   # writes to /src/server/internal/web/dist

# ---- server ----
FROM golang:1.26-trixie AS server
WORKDIR /src/server
COPY server/go.mod server/go.sum ./
RUN go mod download
COPY server ./
COPY --from=web /src/server/internal/web/dist ./internal/web/dist
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X marquee/internal/config.Version=${VERSION}" -o /out/marquee ./cmd/marquee

# ---- runtime ----
FROM debian:trixie-slim
ARG TARGETARCH
RUN apt-get update \
 && apt-get install -y --no-install-recommends ca-certificates curl gnupg tzdata \
 && curl -fsSL https://repo.jellyfin.org/jellyfin_team.gpg.key | gpg --dearmor -o /usr/share/keyrings/jellyfin.gpg \
 && echo "deb [arch=${TARGETARCH:-amd64} signed-by=/usr/share/keyrings/jellyfin.gpg] https://repo.jellyfin.org/debian trixie main" > /etc/apt/sources.list.d/jellyfin.list \
 && apt-get update \
 && apt-get install -y --no-install-recommends jellyfin-ffmpeg7 \
 && apt-get purge -y curl gnupg && apt-get autoremove -y \
 && rm -rf /var/lib/apt/lists/*

COPY --from=server /out/marquee /usr/local/bin/marquee

# Let the NVIDIA container runtime expose the GPU's encoder/decoder.
ENV NVIDIA_VISIBLE_DEVICES=all \
    NVIDIA_DRIVER_CAPABILITIES=compute,video,utility \
    MARQUEE_CONFIG_DIR=/config \
    MARQUEE_TRANSCODE_DIR=/transcode \
    MARQUEE_PORT=32500

VOLUME ["/config"]
EXPOSE 32500
HEALTHCHECK --interval=30s --timeout=6s --start-period=20s CMD ["marquee", "healthcheck"]
ENTRYPOINT ["marquee"]
