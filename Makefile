BINARY := huddle
GOBIN  ?= $(shell go env GOBIN)
ifeq ($(GOBIN),)
  GOBIN = $(shell go env GOPATH)/bin
endif

.PHONY: build install uninstall test test-cover test-e2e vet lint lint-fix fmt check run clean

build:
	go build -o $(BINARY) ./cmd/huddle

install: build
	cp $(BINARY) $(GOBIN)/$(BINARY)
	@echo "Installed $(BINARY) to $(GOBIN)/$(BINARY)"

uninstall:
	rm -f $(GOBIN)/$(BINARY)
	@echo "Removed $(BINARY) from $(GOBIN)"

test:
	go test ./...

test-cover:
	go test -cover ./...

# test-e2e drives the huddle MCP binary as a subprocess against a real
# Slack workspace via `cmd/smoke`. Requires HUDDLE_SLACK_BOT_TOKEN (and
# optionally HUDDLE_ORCHESTRATOR_SLACK_USER_ID for the auto-invite).
# NOT part of `make test` or CI — this hits live Slack.
test-e2e:
	go run ./cmd/smoke

vet:
	go vet ./...

lint:
	go vet ./...
	go tool golangci-lint run ./...

lint-fix:
	go tool golangci-lint run --fix ./...

fmt:
	gofmt -w .
	go tool goimports -w . || true

check: lint test build

run:
	go run ./cmd/huddle

clean:
	rm -f $(BINARY) $(BINARY).exe
