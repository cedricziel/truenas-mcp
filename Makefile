BINARY  := truenas-mcp
PKG     := github.com/cedricziel/truenas-mcp
VERSION ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo dev)
IMAGE   ?= ghcr.io/cedricziel/truenas-mcp

.PHONY: all build test lint format tidy image clean

all: format lint test build

build:
	go build -ldflags "-s -w -X main.version=$(VERSION)" -o dist/$(BINARY) ./cmd/$(BINARY)

test:
	go test ./...

# Integration tests need a live TrueNAS target and are excluded from `make test`.
test-integration:
	go test -tags=integration ./...

lint:
	go vet ./...
	@command -v golangci-lint >/dev/null 2>&1 && golangci-lint run || \
		echo "golangci-lint not installed; ran go vet only"

format:
	gofmt -w .

tidy:
	go mod tidy

image:
	docker build --build-arg VERSION=$(VERSION) -t $(IMAGE):$(VERSION) -t $(IMAGE):dev .

clean:
	rm -rf dist
