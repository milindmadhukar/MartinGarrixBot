FROM --platform=$BUILDPLATFORM golang:1.22-alpine AS build

WORKDIR /build

COPY go.mod go.sum ./

RUN go mod download

COPY . .

ARG VERSION=dev
ARG COMMIT=unknown
ARG TARGETPLATFORM

LABEL org.opencontainers.image.version=${VERSION}
LABEL org.opencontainers.image.revision=${COMMIT}

RUN export GOOS=$(echo ${TARGETPLATFORM} | cut -d'/' -f1) \
    && export GOARCH=$(echo ${TARGETPLATFORM} | cut -d'/' -f2) \
    && if [ "${GOARCH}" = "arm64" ]; then export GOARCH=arm64; fi \
    && echo "Building for GOOS=${GOOS} GOARCH=${GOARCH}" \
    && CGO_ENABLED=0 GOOS=${GOOS} GOARCH=${GOARCH} \
       go build -ldflags "-X main.version=${VERSION} -X main.commit=${COMMIT}" -o bot .

FROM --platform=$TARGETPLATFORM alpine

WORKDIR /bot

COPY --from=build /build/bot /bot/mgbot
COPY --from=build /build/db/migrations/ /bot/db/migrations/
COPY --from=build /build/assets/ /bot/assets/

ENTRYPOINT ["/bot/mgbot"]

CMD ["-config", "/var/lib/config.toml"]