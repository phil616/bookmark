FROM golang:1.26-bookworm AS backend
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=1 go build -trimpath -ldflags="-s -w" -o /server ./cmd/server
FROM debian:bookworm-slim
LABEL org.opencontainers.image.title="online-bookmarks" \
      org.opencontainers.image.version="1.0.0" \
      org.opencontainers.image.authors="phil616" \
      org.opencontainers.image.licenses="MIT"
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates wget && rm -rf /var/lib/apt/lists/* && useradd -u 10001 -m bookmarks && mkdir /data && chown bookmarks /data
COPY --from=backend /server /usr/local/bin/online-bookmarks
COPY LICENSE THIRD_PARTY_NOTICES.md /usr/share/doc/online-bookmarks/
COPY docs/licenses/ /usr/share/doc/online-bookmarks/docs/licenses/
ENV DATA_DIR=/data LISTEN_ADDR=:8080
USER bookmarks
EXPOSE 8080
VOLUME ["/data"]
HEALTHCHECK --interval=30s --timeout=3s CMD wget -q -O /dev/null http://127.0.0.1:8080/healthz || exit 1
ENTRYPOINT ["online-bookmarks"]
