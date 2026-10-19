#!/usr/bin/env bash
# Branches and PRs: build every archive into release/ without publishing. main also builds the image.
source "$(dirname "$0")/lib.sh"

log "Snapshot"
if [[ "${BRANCH_NAME:-}" == main ]]; then
	goreleaser release --snapshot --clean
else
	goreleaser release --snapshot --clean --skip=docker
fi
