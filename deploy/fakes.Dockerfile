# syntax=docker/dockerfile:1.7
# Fake Torznab/Newznab indexers for manual and end-to-end testing (scripts/dev-fakes.sh).
FROM golang:1.26-trixie AS build
WORKDIR /src/server
COPY server/go.mod server/go.sum ./
RUN go mod download
COPY server ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /out/locarr-fakes ./cmd/locarr-fakes

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/locarr-fakes /locarr-fakes
EXPOSE 9117
ENTRYPOINT ["/locarr-fakes"]
