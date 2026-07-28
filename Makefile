BINARY  := plumbline
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: build test lint fmt tidy run install clean

build:            ## build the binary
	go build -ldflags "$(LDFLAGS)" -o $(BINARY) ./cmd/plumbline

test:             ## run tests with the race detector
	go test -race ./...

lint:             ## run golangci-lint (v2)
	golangci-lint run ./...

fmt:              ## apply formatters (gofmt + goimports)
	golangci-lint fmt

tidy:             ## tidy go.mod/go.sum
	go mod tidy

run:              ## run: make run ARGS="audit --owner <you>"
	go run ./cmd/plumbline $(ARGS)

install:          ## go install the binary
	go install -ldflags "$(LDFLAGS)" ./cmd/plumbline

clean:            ## remove build artifacts
	rm -f $(BINARY)
	rm -rf dist
