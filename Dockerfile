FROM --platform=$BUILDPLATFORM golang:alpine AS build

ARG TARGETOS TARGETARCH
WORKDIR /src

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY . .
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w" -o /out/gateway ./cmd/gateway

FROM gcr.io/distroless/static-debian13:nonroot

WORKDIR /app
COPY --from=build /out/gateway /app/gateway

EXPOSE 8080
HEALTHCHECK --interval=2s --timeout=2s --start-period=30s --retries=2 CMD ["/app/gateway", "health"]
ENTRYPOINT ["/app/gateway"]
