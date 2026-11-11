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
COPY cmd ./cmd
COPY internal ./internal
COPY web/embed.go ./web/embed.go
COPY --from=web /src/web/dist ./web/dist
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/rustdesk-manager ./cmd/server

# 3. Minimal runtime image
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=server /out/rustdesk-manager /rustdesk-manager
ENV DATA_DIR=/data LISTEN=:21114
VOLUME /data
EXPOSE 21114
ENTRYPOINT ["/rustdesk-manager"]
