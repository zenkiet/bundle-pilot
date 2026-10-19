#!/usr/bin/env bash
# Checks the tools on the agent and installs dependencies.
source "$(dirname "$0")/lib.sh"

log "Tools"
for tool in git go node pnpm goreleaser golangci-lint docker; do
	command -v "$tool" >/dev/null || { echo "missing tool: $tool" >&2; exit 1; }
done

log "Dependencies"
go mod download
(cd frontend && pnpm install --frozen-lockfile)
(cd libs && pnpm install --frozen-lockfile)
