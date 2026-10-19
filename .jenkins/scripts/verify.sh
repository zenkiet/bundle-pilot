#!/usr/bin/env bash
# Every build: format, lint, test and build the gateway, the admin UI and the libraries.
source "$(dirname "$0")/lib.sh"

log "Go"
[[ -z "$(gofmt -l cmd internal)" ]] || { echo "not gofmt'ed:" >&2; gofmt -l cmd internal >&2; exit 1; }
go mod tidy -diff
go vet ./...
go test -race ./...
golangci-lint run ./...
go run golang.org/x/vuln/cmd/govulncheck@latest ./...

log "Admin UI"
(cd frontend && pnpm check && pnpm lint && pnpm test && pnpm build)

log "Libraries"
(cd libs && pnpm build && pnpm test) # build before test: angular needs core's dist/
