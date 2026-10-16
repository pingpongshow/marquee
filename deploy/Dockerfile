# syntax=docker/dockerfile:1
# Marquee: single image with the Go server, embedded web client, jellyfin-ffmpeg and comskip.
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

# ---- comskip (commercial detection in Live TV recordings; GPL, run as a separate program) ----
# Built statically against a minimal FFmpeg (decoders and demuxers for broadcast recordings
# only), so the runtime image gains one binary and no libraries.
FROM debian:trixie-slim AS comskip
ARG FFMPEG_VERSION=7.1.2
ARG FFMPEG_SHA256=089bc60fb59d6aecc5d994ff530fd0dcb3ee39aa55867849a2bbc4e555f9c304
# Comskip 0.83.001 (V0.83 plus a build fix).
ARG COMSKIP_COMMIT=a140b6ac8bc8f596729e9052819affc779c3b377
RUN apt-get update \
 && apt-get install -y --no-install-recommends ca-certificates curl xz-utils git build-essential \
      autoconf automake libtool pkg-config nasm libargtable2-dev \
 && rm -rf /var/lib/apt/lists/*
WORKDIR /build
RUN curl -fsSL -o ffmpeg.tar.xz https://ffmpeg.org/releases/ffmpeg-${FFMPEG_VERSION}.tar.xz \
 && echo "${FFMPEG_SHA256}  ffmpeg.tar.xz" | sha256sum -c - \
 && tar xf ffmpeg.tar.xz && cd ffmpeg-${FFMPEG_VERSION} \
 && ./configure --prefix=/opt/ffmpeg --pkg-config-flags=--static --enable-static --disable-shared \
      --disable-programs --disable-doc --disable-network --disable-autodetect --disable-everything \
      --disable-avdevice --disable-avfilter --disable-swresample \
      --enable-protocol=file \
      --enable-demuxer=mpegts,mpegps,matroska,mov,h264,hevc,mpegvideo,ac3,eac3,aac \
      --enable-parser=mpegvideo,h264,hevc,vc1,mpeg4video,ac3,aac,aac_latm,mpegaudio \
      --enable-decoder=mpeg1video,mpeg2video,h264,hevc,vc1,mpeg4,ac3,eac3,aac,aac_latm,mp2,mp2float,mp3,mp3float \
 && make -j"$(nproc)" && make install
RUN git init -q comskip && cd comskip \
 && git fetch -q --depth 1 https://github.com/erikkaashoek/Comskip ${COMSKIP_COMMIT} \
 && git checkout -q FETCH_HEAD \
 && ./autogen.sh \
 && PKG_CONFIG_PATH=/opt/ffmpeg/lib/pkgconfig ./configure --enable-static --disable-gui \
 && make -j"$(nproc)" \
 && strip comskip && install -D comskip /out/comskip \
 && /out/comskip --help >/dev/null 2>&1; test -x /out/comskip

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

COPY --from=comskip /out/comskip /usr/local/bin/comskip
COPY deploy/comskip.ini /usr/local/share/marquee/comskip.ini
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
