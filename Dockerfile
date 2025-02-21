FROM --platform=$BUILDPLATFORM golang:1.22-alpine AS build

WORKDIR /build

COPY go.mod go.sum ./

RUN go mod download

COPY . .

ARG VERSION
ARG COMMIT

LABEL org.opencontainers.image.version=${VERSION}
LABEL org.opencontainers.image.revision=${COMMIT}

RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg \
    CGO_ENABLED=0 \
    GOOS=$TARGETOS \
    GOARCH=$TARGETARCH \
	go build -ldflags "-X main.version=$VERSION -X main.commit=$COMMIT" -o bot .

FROM alpine

WORKDIR /bot

COPY --from=build /build/bot /bot/mgbot
COPY --from=build /build/db/migrations /bot/db/migrations
COPY --from=build /build/assets/ /bot/assets/

ENTRYPOINT ["/bot/mgbot"]

CMD ["-config", "/var/lib/config.toml"]