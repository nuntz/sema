SHELL := /bin/sh

GO_CACHE ?= /tmp/sema-go-build
GO_MOD_CACHE ?= /tmp/sema-go-mod
LAMBDAS := scheduler feed-worker item-worker api
GO_SOURCES := $(shell find cmd internal -name '*.go') go.mod go.sum

.PHONY: all test build lambdas web infra preview deploy clean

all: test build

test:
	GOCACHE=$(GO_CACHE) GOMODCACHE=$(GO_MOD_CACHE) go test ./...
	cd infra && GOCACHE=$(GO_CACHE) GOMODCACHE=$(GO_MOD_CACHE) go test ./...
	cd web && bun run test

build: lambdas web

lambdas: $(LAMBDAS:%=bin/%.zip)

bin/%.zip: $(GO_SOURCES)
	mkdir -p bin/$*
	GOCACHE=$(GO_CACHE) GOMODCACHE=$(GO_MOD_CACHE) GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o bin/$*/bootstrap ./cmd/$*
	cd bin/$* && zip -q -j ../$*.zip bootstrap

web:
	cd web && bun install --frozen-lockfile
	cd web && bun run build

infra:
	cd infra && pulumi up

preview:
	cd infra && pulumi preview

deploy: infra

clean:
	rm -rf bin web/dist
