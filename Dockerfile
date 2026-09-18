FROM node:24-alpine AS web
WORKDIR /src/web
COPY web/package*.json ./
RUN npm ci
COPY web ./
COPY api ../api
RUN npm run api:generate && npm run build

FROM golang:1.25-alpine AS backend
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/internal/webui/assets ./internal/webui/assets
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/supermonitor ./cmd/supermonitor

FROM alpine:3.21
RUN addgroup -S supermonitor && adduser -S -G supermonitor supermonitor
WORKDIR /app
COPY --from=backend /out/supermonitor /usr/local/bin/supermonitor
RUN mkdir -p /app/data && chown -R supermonitor:supermonitor /app
USER supermonitor
ENV SUPMON_LISTEN=0.0.0.0:8080 SUPMON_DATA_DIR=/app/data SUPMON_ENVIRONMENT=docker
EXPOSE 8080
VOLUME ["/app/data"]
ENTRYPOINT ["supermonitor"]
