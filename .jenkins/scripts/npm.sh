#!/usr/bin/env bash
# Tags, after release.sh: publishes the libraries at the tag's version. A version already on npm is skipped.
source "$(dirname "$0")/lib.sh"
[[ -n "$TAG" ]] || { echo "not a tag build, nothing to publish"; exit 0; }
: "${NPM_TOKEN:?missing Jenkins credential npm-token}"

log "npm $VERSION"
cd libs
pnpm install --frozen-lockfile
pnpm -r exec npm pkg set version="$VERSION" # in the workspace only, never committed
pnpm build && pnpm test
env "pnpm_config_//registry.npmjs.org/:_authToken=$NPM_TOKEN" pnpm -r publish --access public --no-git-checks
