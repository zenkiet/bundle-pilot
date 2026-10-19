#!/usr/bin/env bash
# Shared by every step: strict mode, tools on PATH, caches, and VERSION on tag builds.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/../.."

export PATH="$HOME/go/bin:/opt/homebrew/bin:/usr/local/bin:$PATH"
if command -v fnm >/dev/null; then
	eval "$(fnm env --shell bash)"
	fnm use --install-if-missing --silent-if-unchanged "$(cat frontend/.nvmrc)" >/dev/null
fi

CACHE="${CI_CACHE:-$HOME/.cache/bundle-pilot-ci}"
export GOMODCACHE="$CACHE/gomod" GOCACHE="$CACHE/gobuild" npm_config_store_dir="$CACHE/pnpm"

TAG="${TAG_NAME:-}" # set by Jenkins on tag builds
if [[ -n "$TAG" ]]; then
	[[ "$TAG" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] || { echo "tag must look like v1.2.3, got '$TAG'" >&2; exit 1; }
	VERSION="${TAG#v}"
	export GORELEASER_CURRENT_TAG="$TAG"
fi

log() { printf '\n\033[1m==> %s\033[0m\n' "$*"; }
