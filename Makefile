SHELL := /bin/sh

GO_CACHE ?= /tmp/sema-go-build
GO_MOD_CACHE ?= /tmp/sema-go-mod
LAMBDAS := scheduler feed-worker item-worker api rescore
GO_SOURCES := $(shell find cmd internal -name '*.go') go.mod go.sum
STACK ?= dev

.PHONY: all test build lambdas web infra preview deploy rescore replay clean

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

rescore:
	aws lambda invoke --function-name sema-$(STACK)-rescore --cli-binary-format raw-in-base64-out --payload '{"on_demand":true}' /tmp/sema-rescore.json

replay:
	replay_model_version='$(MODEL_VERSION)'; \
	if [ -z "$$replay_model_version" ]; then replay_model_version=$$(cd infra && pulumi stack output modelVersion); fi; \
	TABLE_NAME=sema-$(STACK) ITEMS_QUEUE_URL=$$(cd infra && pulumi stack output itemsQueueUrl) MODEL_VERSION="$$replay_model_version" GOCACHE=$(GO_CACHE) GOMODCACHE=$(GO_MOD_CACHE) go run ./cmd/replay

clean:
	rm -rf bin web/dist
