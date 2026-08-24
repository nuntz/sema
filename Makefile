SHELL := /bin/sh

GO_CACHE ?= /tmp/sema-go-build
GO_MOD_CACHE ?= /tmp/sema-go-mod
LAMBDAS := scheduler feed-worker item-worker api rescore vector-cleanup
GO_SOURCES := $(shell find cmd internal -name '*.go') go.mod go.sum
STACK ?= dev
AWS_REGION ?= us-east-1

.PHONY: all test build lambdas web infra preview deploy rescore replay redrive backfill-item-identities backfill-search-text backfill-vectors backfill-youtube-connector clean

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
	aws cloudfront create-invalidation --region $(AWS_REGION) --distribution-id "$$(cd infra && pulumi stack output distributionId)" --paths '/*'

rescore:
	aws lambda invoke --region $(AWS_REGION) --function-name sema-$(STACK)-rescore --cli-binary-format raw-in-base64-out --payload '{"on_demand":true}' /tmp/sema-rescore.json

replay:
	replay_model_version='$(MODEL_VERSION)'; \
	if [ -z "$$replay_model_version" ]; then replay_model_version=$$(cd infra && pulumi stack output modelVersion); fi; \
	AWS_REGION=$(AWS_REGION) TABLE_NAME=sema-$(STACK) ITEMS_QUEUE_URL=$$(cd infra && pulumi stack output itemsQueueUrl) MODEL_VERSION="$$replay_model_version" GOCACHE=$(GO_CACHE) GOMODCACHE=$(GO_MOD_CACHE) go run ./cmd/replay $(FLAGS)

redrive:
	aws sqs start-message-move-task --region $(AWS_REGION) --source-arn "$$(cd infra && pulumi stack output feedsDlqArn)" --destination-arn "$$(cd infra && pulumi stack output feedsQueueArn)"
	aws sqs start-message-move-task --region $(AWS_REGION) --source-arn "$$(cd infra && pulumi stack output itemsDlqArn)" --destination-arn "$$(cd infra && pulumi stack output itemsQueueArn)"

backfill-item-identities:
	AWS_REGION=$(AWS_REGION) TABLE_NAME=sema-$(STACK) GOCACHE=$(GO_CACHE) GOMODCACHE=$(GO_MOD_CACHE) go run ./cmd/backfill-item-identities $(BACKFILL_ARGS)

backfill-search-text:
	AWS_REGION=$(AWS_REGION) TABLE_NAME=sema-$(STACK) GOCACHE=$(GO_CACHE) GOMODCACHE=$(GO_MOD_CACHE) go run ./cmd/backfill-search-text $(BACKFILL_ARGS)

backfill-vectors:
	AWS_REGION=$(AWS_REGION) TABLE_NAME=sema-$(STACK) VECTOR_BUCKET=$$(cd infra && pulumi stack output vectorBucket) VECTOR_INDEX=$$(cd infra && pulumi stack output vectorIndex) GOCACHE=$(GO_CACHE) GOMODCACHE=$(GO_MOD_CACHE) go run ./cmd/backfill-vectors $(BACKFILL_ARGS)

backfill-youtube-connector:
	AWS_REGION=$(AWS_REGION) TABLE_NAME=sema-$(STACK) CONTENT_BUCKET=$$(cd infra && pulumi stack output contentBucket) GOCACHE=$(GO_CACHE) GOMODCACHE=$(GO_MOD_CACHE) go run ./cmd/backfill-youtube-connector $(BACKFILL_ARGS)

clean:
	rm -rf bin web/dist
