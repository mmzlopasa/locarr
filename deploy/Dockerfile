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

FROM debian:trixie-slim AS runtime
RUN set -eux; \
    apt-get update; \
    apt-get install -y --no-install-recommends ca-certificates tzdata; \
    rm -rf /var/lib/apt/lists/*; \
    groupadd --gid 1000 locarr; \
    useradd --uid 1000 --gid 1000 --no-create-home --home-dir /config --shell /usr/sbin/nologin locarr; \
    mkdir -p /config /data && chown locarr:locarr /config
COPY --from=server /out/locarr /app/locarr
COPY deploy/entrypoint.sh /app/entrypoint.sh
ENV LOCARR_HTTP_ADDR=:8484 \
    LOCARR_CONFIG_DIR=/config \
    LOCARR_DATA_ROOTS=/data
EXPOSE 8484
VOLUME ["/config"]
HEALTHCHECK --interval=15s --timeout=5s --start-period=10s --retries=5 CMD ["/app/locarr", "healthcheck"]
ENTRYPOINT ["/app/entrypoint.sh"]
CMD ["serve"]
