# syntax=docker/dockerfile:1.7
# Multi-stage build: web client -> Go binary (web embedded) -> small runtime.
# Build context is the repository root: docker build -f deploy/Dockerfile .

FROM --platform=$BUILDPLATFORM node:22-slim AS web
WORKDIR /src/web
RUN corepack enable
COPY web/package.json web/pnpm-lock.yaml web/.npmrc ./
RUN pnpm install --frozen-lockfile
COPY api /src/api
COPY web ./
RUN mkdir -p /src/server/internal/webui/dist && pnpm build

FROM --platform=$BUILDPLATFORM golang:1.26-trixie AS server
WORKDIR /src/server
COPY server/go.mod server/go.sum ./
RUN go mod download
COPY server ./
COPY --from=web /src/server/internal/webui/dist ./internal/webui/dist
ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev
ARG COMMIT=unknown
# Pure Go (SQLite through modernc.org/sqlite), so cross-compiling needs no C toolchain.
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath \
    -ldflags "-s -w -X github.com/mmzlopasa/locarr/server/internal/version.Version=${VERSION} -X github.com/mmzlopasa/locarr/server/internal/version.Commit=${COMMIT}" \
    -o /out/locarr ./cmd/locarr

# A small static ffmpeg for subtitle sync (D-054): audio decoders and the
# container demuxers only, about 3.5 MB instead of the ~100 MB of the Debian
# package. Cross-compiled on the build platform.
FROM --platform=$BUILDPLATFORM debian:trixie-slim AS ffmpeg
ARG TARGETARCH
ARG FFMPEG_VERSION=8.1.3
ARG FFMPEG_SHA256=7138d28c96d9d3e3af4ee3d8cad72741f8ffb40da90c1112235dea3ecd3178a3
RUN set -eux; \
    apt-get update; \
    apt-get install -y --no-install-recommends build-essential nasm xz-utils curl ca-certificates; \
    build="$(dpkg --print-architecture)"; \
    if [ "$TARGETARCH" != "$build" ]; then \
      case "$TARGETARCH" in \
        arm64) apt-get install -y --no-install-recommends gcc-aarch64-linux-gnu libc6-dev-arm64-cross ;; \
        amd64) apt-get install -y --no-install-recommends gcc-x86-64-linux-gnu libc6-dev-amd64-cross ;; \
      esac; \
    fi; \
    rm -rf /var/lib/apt/lists/*
RUN set -eux; \
    curl -fsSLo /tmp/ffmpeg.tar.xz "https://ffmpeg.org/releases/ffmpeg-${FFMPEG_VERSION}.tar.xz"; \
    echo "${FFMPEG_SHA256}  /tmp/ffmpeg.tar.xz" | sha256sum -c -; \
    mkdir /tmp/ffmpeg && tar -xJf /tmp/ffmpeg.tar.xz -C /tmp/ffmpeg --strip-components=1; \
    cd /tmp/ffmpeg; \
    cross=""; strip=strip; \
    if [ "$TARGETARCH" != "$(dpkg --print-architecture)" ]; then \
      case "$TARGETARCH" in \
        arm64) cross="--enable-cross-compile --cross-prefix=aarch64-linux-gnu- --arch=aarch64 --target-os=linux"; strip=aarch64-linux-gnu-strip ;; \
        amd64) cross="--enable-cross-compile --cross-prefix=x86_64-linux-gnu- --arch=x86_64 --target-os=linux"; strip=x86_64-linux-gnu-strip ;; \
      esac; \
    fi; \
    ./configure $cross --prefix=/opt/ffmpeg --disable-everything --disable-autodetect --disable-doc --disable-debug --disable-network \
      --disable-ffplay --disable-ffprobe --disable-avdevice --disable-swscale --enable-small --enable-static --disable-shared \
      --extra-ldflags=-static --enable-protocol=file,pipe --enable-demuxer=matroska,mov,avi,mpegts,flv,ogg,wav \
      --enable-decoder=subrip,ass,ssa,webvtt,movtext,text,aac,aac_latm,ac3,eac3,dca,truehd,mlp,opus,vorbis,flac,mp3,mp2,alac,pcm_s16le,pcm_s24le,pcm_s16be,pcm_bluray,pcm_dvd \
      --enable-parser=aac,ac3,dca,opus,vorbis,flac,mpegaudio,mlp --enable-muxer=pcm_s16le,srt --enable-encoder=pcm_s16le,subrip \
      --enable-filter=aresample,aformat,anull,atrim --enable-swresample; \
    make -j"$(nproc)"; \
    "$strip" ffmpeg; \
    install -D -m 0755 ffmpeg /out/ffmpeg

FROM debian:trixie-slim AS runtime
RUN set -eux; \
    apt-get update; \
    apt-get install -y --no-install-recommends ca-certificates tzdata; \
    rm -rf /var/lib/apt/lists/*; \
    groupadd --gid 1000 locarr; \
    useradd --uid 1000 --gid 1000 --no-create-home --home-dir /config --shell /usr/sbin/nologin locarr; \
    mkdir -p /config /data && chown locarr:locarr /config
COPY --from=server /out/locarr /app/locarr
COPY --from=ffmpeg /out/ffmpeg /usr/local/bin/ffmpeg
COPY deploy/entrypoint.sh /app/entrypoint.sh
ENV LOCARR_HTTP_ADDR=:8484 \
    LOCARR_CONFIG_DIR=/config \
    LOCARR_DATA_ROOTS=/data
EXPOSE 8484
VOLUME ["/config"]
HEALTHCHECK --interval=15s --timeout=5s --start-period=10s --retries=5 CMD ["/app/locarr", "healthcheck"]
ENTRYPOINT ["/app/entrypoint.sh"]
CMD ["serve"]
