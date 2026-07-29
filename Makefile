.PHONY: dev build lint fmt deps

dev:
	go run ./cmd/gateway

build:
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o bin/gateway ./cmd/gateway

lint:
	golangci-lint run ./...

fmt:
	golangci-lint fmt ./...

deps:
	go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest
