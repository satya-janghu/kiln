# kiln — see CLAUDE.md. There are no test targets by design (docs/review-process.md).

BINARY  := dist/kiln
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
LDFLAGS := -s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT)

.PHONY: build lint vuln release-check tidy clean

## build: cross-compile the static linux/amd64 binary into dist/kiln
build:
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags '$(LDFLAGS)' -o $(BINARY) ./cmd/kiln

## lint: vet + golangci-lint for both darwin (dev) and linux (target, so *_linux.go is checked) + shellcheck
lint:
	go mod tidy -diff
	go vet ./...
	GOOS=linux GOARCH=amd64 go vet ./...
	golangci-lint run ./...
	GOOS=linux GOARCH=amd64 golangci-lint run ./...
	shellcheck install.sh scripts/field-check.sh

## vuln: known-vulnerability scan of code and dependencies
vuln:
	govulncheck ./...

## release-check: validate the GoReleaser config
release-check:
	goreleaser check

tidy:
	go mod tidy

clean:
	rm -rf dist
