APP_NAME := xmanager
PKG := github.com/lyracorp/xmanager
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
BUILD_TIME := $(shell date -u '+%Y-%m-%dT%H:%M:%SZ')
LDFLAGS := -s -w \
	-X $(PKG)/internal/config.Version=$(VERSION) \
	-X $(PKG)/internal/config.Commit=$(COMMIT) \
	-X $(PKG)/internal/config.BuildTime=$(BUILD_TIME)

.PHONY: build run test lint clean install fmt vet

build:
	CGO_ENABLED=1 go build -ldflags "$(LDFLAGS)" -o bin/$(APP_NAME) ./cmd/xmanager

run: build
	./bin/$(APP_NAME)

test:
	go test -race -cover ./...

lint:
	golangci-lint run ./...

fmt:
	gofmt -s -w .
	goimports -w .

vet:
	go vet ./...

clean:
	rm -rf bin/ dist/

install: build
	cp bin/$(APP_NAME) /usr/local/bin/$(APP_NAME)
	ln -sf /usr/local/bin/$(APP_NAME) /usr/local/bin/vpsm
