FROM node:alpine AS ui

RUN npm install -g pnpm@latest
WORKDIR /src/frontend

COPY frontend/package.json frontend/pnpm-lock.yaml \
     frontend/pnpm-workspace.yaml frontend/.npmrc ./
RUN pnpm install --frozen-lockfile

COPY frontend ./
RUN pnpm build

FROM --platform=$BUILDPLATFORM golang:alpine AS build

ARG TARGETOS TARGETARCH
WORKDIR /src

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY . .
COPY --from=ui /src/internal/ui/build ./internal/ui/build
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w" -o /out/gateway ./cmd/gateway

FROM gcr.io/distroless/static-debian13:nonroot

WORKDIR /app
COPY --from=build /out/gateway /app/gateway

EXPOSE 8080
ENTRYPOINT ["/app/gateway"]
