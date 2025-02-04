FROM --platform=$BUILDPLATFORM golang:1.22-alpine AS build

WORKDIR /build

COPY go.mod go.sum ./

RUN go mod download

COPY . .

ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev
ARG COMMIT=unknown

RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg \
    CGO_ENABLED=0 \
    GOOS=$TARGETOS \
    GOARCH=$TARGETARCH \
	go build -ldflags "-X main.version=$VERSION -X main.commit=$COMMIT" -o bot .

FROM alpine

WORKDIR /bot

COPY --from=build /build/bot /bot/mgbot
COPY --from=build /build/db/ /bot/db/

ENTRYPOINT ["/bot/mgbot"]

CMD ["-config", "/var/lib/config.toml"]