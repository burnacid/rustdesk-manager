# 1. Build the React admin UI
FROM node:26-alpine AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

# 2. Build the Go server (pure Go, no CGO) with the UI embedded
FROM golang:1.27-alpine AS server
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY VERSION ./
COPY cmd ./cmd
COPY internal ./internal
COPY web/embed.go ./web/embed.go
COPY --from=web /src/web/dist ./web/dist
RUN CGO_ENABLED=0 go build -trimpath     -ldflags="-s -w -X rustdesk-manager/internal/version.Version=$(cat VERSION)"     -o /out/rustdesk-manager ./cmd/server
# The data folder must belong to the non-root user the server runs as (65532).
RUN mkdir -p /out/data/recordings

# 3. Minimal runtime image
FROM gcr.io/distroless/static-debian12:nonroot
ARG VERSION=dev
LABEL org.opencontainers.image.title="RustDesk Manager"       org.opencontainers.image.description="Self-hosted API server and web console for RustDesk"       org.opencontainers.image.version="${VERSION}"
COPY --from=server /out/rustdesk-manager /rustdesk-manager
COPY --from=server --chown=65532:65532 /out/data /data
ENV DATA_DIR=/data LISTEN=:21114
VOLUME /data
EXPOSE 21114
ENTRYPOINT ["/rustdesk-manager"]
