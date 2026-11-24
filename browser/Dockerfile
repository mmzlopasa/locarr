# syntax=docker/dockerfile:1.7
# Locarr's optional companion browser: headless Chromium behind a small
# control server (see main.go). Build context is this folder:
#   docker build -t locarr-browser browser/
FROM --platform=$BUILDPLATFORM golang:1.26-trixie AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY *.go ./
ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath \
    -ldflags "-s -w -X main.Version=${VERSION}" -o /out/locarr-browser .

FROM debian:trixie-slim
# Chromium without recommended extras; fonts so pages lay out like they do
# for people. No shell tools are needed at run time.
RUN set -eux; \
    apt-get update; \
    apt-get install -y --no-install-recommends chromium ca-certificates fonts-liberation; \
    rm -rf /var/lib/apt/lists/* /usr/share/doc /usr/share/man /usr/share/locale/*/LC_MESSAGES/*.mo; \
    # Hardware GPU drivers (Mesa, LLVM) are never loaded: Chromium runs with
    # --disable-gpu and draws with its own software renderer.
    rm -rf /usr/lib/*-linux-gnu/dri /usr/lib/*-linux-gnu/libgallium-*.so /usr/lib/*-linux-gnu/libLLVM*.so* \
      /usr/lib/*-linux-gnu/libz3.so* /usr/lib/chromium/libVkLayer_khronos_validation.so /usr/share/icons/Adwaita; \
    groupadd --gid 1000 browser; \
    useradd --uid 1000 --gid 1000 --create-home --home-dir /home/browser --shell /usr/sbin/nologin browser
COPY --from=build /out/locarr-browser /usr/local/bin/locarr-browser
USER 1000:1000
ENV HOME=/tmp \
    LOCARR_BROWSER_ADDR=:8490 \
    LOCARR_BROWSER_CHROMIUM=/usr/bin/chromium
EXPOSE 8490
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 CMD ["/usr/local/bin/locarr-browser", "healthcheck"]
ENTRYPOINT ["/usr/local/bin/locarr-browser"]
