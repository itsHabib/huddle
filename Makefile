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

# test-e2e runs against a real Slack workspace. Requires HUDDLE_E2E=1 and
# HUDDLE_SLACK_BOT_TOKEN. NOT part of `make test` or CI.
test-e2e:
	HUDDLE_E2E=1 go test -tags=e2e -count=1 -v ./test/e2e/...

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
