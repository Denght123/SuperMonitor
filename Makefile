.PHONY: dev web-install web-generate web-check web-build go-check test build verify

dev:
	go run ./cmd/supermonitor

web-install:
	cd web && npm ci

web-generate:
	cd web && npm run api:generate

web-check:
	cd web && npm run check

web-build:
	cd web && npm run build

go-check:
	gofmt -w cmd internal
	go vet ./...

test:
	go test ./...
	cd web && npm test -- --run

build: web-build
	go build -trimpath -ldflags "-s -w -X main.version=$${VERSION:-dev}" -o bin/supermonitor ./cmd/supermonitor

verify: web-generate web-check web-build
	go vet ./...
	go test ./...
