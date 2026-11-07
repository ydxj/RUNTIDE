# RUNTIDE self-hosted receipt server. The server stores and serves Run Receipts;
# it never executes commands. Build from the repository root:
#   docker build -f deployments/Dockerfile -t runtide-server .
FROM golang:1.27 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
ENV CGO_ENABLED=0
RUN go build -trimpath -ldflags "-s -w -X github.com/runtide/runtide/internal/version.Version=0.9.1" \
    -o /out/runtide-server ./cmd/runtide-server
RUN mkdir -p /out/data

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/runtide-server /usr/local/bin/runtide-server
# Data (SQLite database + receipt files) lives in a volume owned by the non-root user (uid 65532).
# The image has no shell, so /data is created in the build stage and copied with its owner;
# Docker initialises a new volume from this directory, ownership included.
COPY --from=build --chown=65532:65532 /out/data /data
VOLUME ["/data"]
ENV RUNTIDE_SERVER_DATA=/data \
    RUNTIDE_SERVER_ADDR=0.0.0.0:8484 \
    RUNTIDE_SERVER_ALLOW_REMOTE=1
EXPOSE 8484
# The listener is plain HTTP: publish it only behind a TLS-terminating proxy
# (see docs/SELF_HOSTING.md) and set RUNTIDE_SERVER_BEHIND_TLS_PROXY=1 to acknowledge that.
ENTRYPOINT ["/usr/local/bin/runtide-server"]
