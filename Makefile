.PHONY: dev build gen lint fmt deps

dev:
	go run ./cmd/gateway

build:
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o bin/gateway ./cmd/gateway

gen:
	PATH="$(PATH):$(shell go env GOPATH)/bin" buf generate

lint:
	golangci-lint run ./...

fmt:
	golangci-lint fmt ./...

deps:
	go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest
	go install github.com/bufbuild/buf/cmd/buf@latest
	go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
	go install connectrpc.com/connect/cmd/protoc-gen-connect-go@latest
