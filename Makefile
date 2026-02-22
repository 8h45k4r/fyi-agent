.PHONY: build test lint clean

VERSION ?= $(shell git describe --tags --always --dirty)
LDFLAGS := -ldflags "-X main.version=$(VERSION)"

build:
	go build $(LDFLAGS) ./cmd/fyi-agent/...

test:
	go test ./... -v -race -count=1

coverage:
	go test ./... -coverprofile=coverage.out
	go tool cover -html=coverage.out -o coverage.html

lint:
	golangci-lint run

clean:
	rm -f coverage.out coverage.html
	rm -rf bin/

build-windows:
	GOOS=windows GOARCH=amd64 go build $(LDFLAGS) -o bin/fyi-agent.exe ./cmd/fyi-agent/windows/

build-darwin:
	GOOS=darwin GOARCH=arm64 go build $(LDFLAGS) -o bin/fyi-agent-darwin ./cmd/fyi-agent/darwin/

all: lint test build-windows build-darwin
