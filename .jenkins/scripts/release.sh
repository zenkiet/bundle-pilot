#!/usr/bin/env bash
# Tags: archives, checksums and the GitHub release, plus the amd64/arm64 image on ghcr.io.
source "$(dirname "$0")/lib.sh"
[[ -n "$TAG" ]] || { echo "not a tag build, nothing to release"; exit 0; }
: "${GITHUB_TOKEN:?missing Jenkins credential github-token}"
: "${GHCR_TOKEN:?missing Jenkins credential ghcr-token}"
: "${NPM_TOKEN:?missing Jenkins credential npm-token}"

# npm tokens expire: fail here, before anything is published.
log "npm token"
curl -fsS -H "Authorization: Bearer $NPM_TOKEN" https://registry.npmjs.org/-/whoami ||
	{ echo "npm-token was rejected: renew it" >&2; exit 1; }

log "Release $TAG"
echo "$GHCR_TOKEN" | docker login ghcr.io -u "${GHCR_USER:-zenkiet}" --password-stdin
trap 'docker logout ghcr.io >/dev/null' EXIT
goreleaser release --clean
