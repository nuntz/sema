SHELL := /bin/sh

GO_CACHE ?= /tmp/sema-go-build
GO_MOD_CACHE ?= /tmp/sema-go-mod
LAMBDAS := scheduler feed-worker item-worker api rescore vector-cleanup delivery-worker
GO_SOURCES := $(shell find cmd internal -name '*.go') go.mod go.sum
STACK ?= prod
PULUMI_ORG ?= untz
PULUMI_STACK ?= $(PULUMI_ORG)/sema/$(STACK)
AWS_REGION ?= us-east-1
ZIP_EPOCH := 198001010000
# The Go toolchain go.mod pins (its toolchain line, else its go line), which CI installs.
# A go line without a patch version names a language, not a toolchain, so 1.26 becomes go1.26.0.
GO_TOOLCHAIN := $(shell awk '$$1 == "toolchain" { t = $$2 } $$1 == "go" { g = "go" $$2 (split($$2, v, ".") == 2 ? ".0" : "") } END { print (t != "" ? t : g) }' go.mod)

.PHONY: all check check-reproducible e2e e2e-all screenshot test build lambdas web infra preview deploy rescore replay redrive redrive-deliveries backfill-archive-identities backfill-feed-cadence backfill-image-signals backfill-item-identities backfill-item-vectors backfill-media-variants backfill-reddit-connector backfill-search-text backfill-stories backfill-unread-pk backfill-vectors backfill-youtube-connector purge-legacy-vectors clean

all: test build

test:
	GOCACHE=$(GO_CACHE) GOMODCACHE=$(GO_MOD_CACHE) go test ./...
	cd infra && GOCACHE=$(GO_CACHE) GOMODCACHE=$(GO_MOD_CACHE) go test ./...
	cd web && bun run test

# check runs everything CI's test job runs (CI calls it), so a green check is a green CI test job.
# It runs the unit tests against DynamoDB Local (scripts/with-dynamodb-local.sh), starting a
# container when DYNAMODB_ENDPOINT isn't set. The browser tests are CI's separate e2e job: make e2e.
check:
	bash scripts/lint-ci-files.sh
	bash scripts/lint-doc-paths.sh
	@unformatted="$$(gofmt -l .)"; if [ -n "$$unformatted" ]; then echo "gofmt needed: $$unformatted" >&2; exit 1; fi
	GOCACHE=$(GO_CACHE) GOMODCACHE=$(GO_MOD_CACHE) go vet ./...
	cd infra && GOCACHE=$(GO_CACHE) GOMODCACHE=$(GO_MOD_CACHE) go vet ./...
	cd web && bun install --frozen-lockfile
	cd web && bun run lint
	bash scripts/with-dynamodb-local.sh $(MAKE) test
	cd web && bun run build
	$(MAKE) check-reproducible

# The Lambda ZIPs must come out byte-identical from any build of the same code, or every deploy
# updates all seven functions. Two builds 2s apart (a ZIP's clock ticks in 2s) must match, no
# binary may embed VCS info, which changes with every commit, and every binary must be built with
# the toolchain go.mod pins.
check-reproducible:
	$(MAKE) -B lambdas
	sha256sum bin/*.zip > bin/reproducible.sha256
	sleep 2
	$(MAKE) -B lambdas
	sha256sum --check --quiet bin/reproducible.sha256
	@if go version -m bin/*/bootstrap | grep "vcs\."; then echo "The Lambda binaries embed VCS info" >&2; exit 1; fi
	@for bootstrap in bin/*/bootstrap; do \
		built="$$(go version "$$bootstrap" | awk '{ print $$2 }')"; \
		if [ "$$built" != "$(GO_TOOLCHAIN)" ]; then echo "$$bootstrap was built with $$built, not $(GO_TOOLCHAIN) from go.mod" >&2; exit 1; fi; \
	done

# e2e is CI's e2e job: every Playwright spec except those tagged @local. e2e-all runs them all.
e2e:
	cd web && bun install --frozen-lockfile
	cd web && bunx playwright test --grep-invert @local

e2e-all:
	cd web && bun install --frozen-lockfile
	cd web && bunx playwright test

# screenshot redraws docs/screenshot.png, the README's front page, from a mocked API.
screenshot:
	cd web && bun install --frozen-lockfile
	cd web && SEMA_SCREENSHOT=1 bunx playwright test readme-screenshot

build: lambdas web

lambdas: $(LAMBDAS:%=bin/%.zip)

# The Go runtime ships in every binary, so the ZIPs match CI's only when built with the toolchain
# go.mod pins, not a newer local Go. GOTOOLCHAIN downloads it into GOMODCACHE on first use.
# Only the shipped binaries need it; tests and maintenance tools run on the local Go.
bin/%.zip: $(GO_SOURCES)
	mkdir -p bin/$*
	GOTOOLCHAIN=$(GO_TOOLCHAIN) GOCACHE=$(GO_CACHE) GOMODCACHE=$(GO_MOD_CACHE) GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -buildvcs=false -ldflags="-s -w" -o bin/$*/bootstrap ./cmd/$*
	chmod 0755 bin/$*/bootstrap
	touch -t $(ZIP_EPOCH) bin/$*/bootstrap
	rm -f bin/$*.zip
	cd bin/$* && zip -q -X -j ../$*.zip bootstrap

web:
	cd web && bun install --frozen-lockfile
	cd web && bun run build

infra:
	cd infra && pulumi up --stack $(PULUMI_STACK)

preview:
	cd infra && pulumi preview --stack $(PULUMI_STACK)

deploy: infra
	aws cloudfront create-invalidation --region $(AWS_REGION) --distribution-id "$$(cd infra && pulumi stack output distributionId --stack $(PULUMI_STACK))" --paths '/*'

rescore:
	aws lambda invoke --region $(AWS_REGION) --function-name sema-$(STACK)-rescore --cli-binary-format raw-in-base64-out --payload '{"on_demand":true}' /tmp/sema-rescore.json

replay:
	replay_model_version='$(MODEL_VERSION)'; \
	if [ -z "$$replay_model_version" ]; then replay_model_version=$$(cd infra && pulumi stack output modelVersion --stack $(PULUMI_STACK)); fi; \
	AWS_REGION=$(AWS_REGION) TABLE_NAME=sema-$(STACK) ITEMS_QUEUE_URL=$$(cd infra && pulumi stack output itemsQueueUrl --stack $(PULUMI_STACK)) MODEL_VERSION="$$replay_model_version" GOCACHE=$(GO_CACHE) GOMODCACHE=$(GO_MOD_CACHE) go run ./cmd/replay $(FLAGS)

redrive:
	aws sqs start-message-move-task --region $(AWS_REGION) --source-arn "$$(cd infra && pulumi stack output feedsDlqArn --stack $(PULUMI_STACK))" --destination-arn "$$(cd infra && pulumi stack output feedsQueueArn --stack $(PULUMI_STACK))"
	aws sqs start-message-move-task --region $(AWS_REGION) --source-arn "$$(cd infra && pulumi stack output itemsDlqArn --stack $(PULUMI_STACK))" --destination-arn "$$(cd infra && pulumi stack output itemsQueueArn --stack $(PULUMI_STACK))"

# Outward-facing: each redriven Send POSTs to a user's Destination, so read RUNBOOK.md first.
# With no --destination-arn, SQS returns each message to the deliveries queue it came from.
redrive-deliveries:
	aws sqs start-message-move-task --region $(AWS_REGION) --source-arn "$$(cd infra && pulumi stack output deliveriesDlqArn --stack $(PULUMI_STACK))"

backfill-image-signals:
	AWS_REGION=$(AWS_REGION) TABLE_NAME=sema-$(STACK) CONTENT_BUCKET=$$(cd infra && pulumi stack output contentBucket --stack $(PULUMI_STACK)) VECTOR_BUCKET=$$(cd infra && pulumi stack output vectorBucket --stack $(PULUMI_STACK)) IMAGE_VECTOR_INDEX=$$(cd infra && pulumi stack output imageVectorIndex --stack $(PULUMI_STACK)) IMAGE_MODEL_VERSION=$$(cd infra && pulumi stack output imageModelVersion --stack $(PULUMI_STACK)) GOCACHE=$(GO_CACHE) GOMODCACHE=$(GO_MOD_CACHE) go run ./cmd/backfill-image-signals $(BACKFILL_ARGS)

backfill-archive-identities:
	AWS_REGION=$(AWS_REGION) TABLE_NAME=sema-$(STACK) GOCACHE=$(GO_CACHE) GOMODCACHE=$(GO_MOD_CACHE) go run ./cmd/backfill-archive-identities $(BACKFILL_ARGS)

backfill-unread-pk:
	AWS_REGION=$(AWS_REGION) TABLE_NAME=sema-$(STACK) GOCACHE=$(GO_CACHE) GOMODCACHE=$(GO_MOD_CACHE) go run ./cmd/backfill-unread-pk $(BACKFILL_ARGS)

backfill-item-identities:
	AWS_REGION=$(AWS_REGION) TABLE_NAME=sema-$(STACK) GOCACHE=$(GO_CACHE) GOMODCACHE=$(GO_MOD_CACHE) go run ./cmd/backfill-item-identities $(BACKFILL_ARGS)

backfill-item-vectors:
	AWS_REGION=$(AWS_REGION) TABLE_NAME=sema-$(STACK) VECTOR_BUCKET=$$(cd infra && pulumi stack output vectorBucket --stack $(PULUMI_STACK)) VECTOR_INDEX=$$(cd infra && pulumi stack output vectorIndex --stack $(PULUMI_STACK)) GOCACHE=$(GO_CACHE) GOMODCACHE=$(GO_MOD_CACHE) go run ./cmd/backfill-item-vectors $(BACKFILL_ARGS)

backfill-media-variants:
	AWS_REGION=$(AWS_REGION) TABLE_NAME=sema-$(STACK) CONTENT_BUCKET=$$(cd infra && pulumi stack output contentBucket --stack $(PULUMI_STACK)) GOCACHE=$(GO_CACHE) GOMODCACHE=$(GO_MOD_CACHE) go run ./cmd/backfill-media-variants $(BACKFILL_ARGS)

backfill-reddit-connector:
	AWS_REGION=$(AWS_REGION) TABLE_NAME=sema-$(STACK) GOCACHE=$(GO_CACHE) GOMODCACHE=$(GO_MOD_CACHE) go run ./cmd/backfill-reddit-connector $(BACKFILL_ARGS)

backfill-search-text:
	AWS_REGION=$(AWS_REGION) TABLE_NAME=sema-$(STACK) GOCACHE=$(GO_CACHE) GOMODCACHE=$(GO_MOD_CACHE) go run ./cmd/backfill-search-text $(BACKFILL_ARGS)

backfill-stories:
	AWS_REGION=$(AWS_REGION) TABLE_NAME=sema-$(STACK) GOCACHE=$(GO_CACHE) GOMODCACHE=$(GO_MOD_CACHE) go run ./cmd/backfill-stories $(BACKFILL_ARGS)

backfill-vectors:
	AWS_REGION=$(AWS_REGION) TABLE_NAME=sema-$(STACK) VECTOR_BUCKET=$$(cd infra && pulumi stack output vectorBucket --stack $(PULUMI_STACK)) VECTOR_INDEX=$$(cd infra && pulumi stack output vectorIndex --stack $(PULUMI_STACK)) IMAGE_VECTOR_INDEX=$$(cd infra && pulumi stack output imageVectorIndex --stack $(PULUMI_STACK)) GOCACHE=$(GO_CACHE) GOMODCACHE=$(GO_MOD_CACHE) go run ./cmd/backfill-vectors $(BACKFILL_ARGS)

purge-legacy-vectors:
	AWS_REGION=$(AWS_REGION) TABLE_NAME=sema-$(STACK) VECTOR_BUCKET=$$(cd infra && pulumi stack output vectorBucket --stack $(PULUMI_STACK)) VECTOR_INDEX=$$(cd infra && pulumi stack output vectorIndex --stack $(PULUMI_STACK)) IMAGE_VECTOR_INDEX=$$(cd infra && pulumi stack output imageVectorIndex --stack $(PULUMI_STACK)) GOCACHE=$(GO_CACHE) GOMODCACHE=$(GO_MOD_CACHE) go run ./cmd/purge-legacy-vectors $(BACKFILL_ARGS)

backfill-youtube-connector:
	AWS_REGION=$(AWS_REGION) TABLE_NAME=sema-$(STACK) CONTENT_BUCKET=$$(cd infra && pulumi stack output contentBucket --stack $(PULUMI_STACK)) GOCACHE=$(GO_CACHE) GOMODCACHE=$(GO_MOD_CACHE) go run ./cmd/backfill-youtube-connector $(BACKFILL_ARGS)

clean:
	rm -rf bin web/dist

backfill-feed-cadence:
	AWS_REGION=$(AWS_REGION) TABLE_NAME=sema-$(STACK) GOCACHE=$(GO_CACHE) GOMODCACHE=$(GO_MOD_CACHE) go run ./cmd/backfill-feed-cadence $(BACKFILL_ARGS)
