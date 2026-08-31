FROM --platform=$BUILDPLATFORM golang:1.25-alpine AS build

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

# The Go binary embeds its own copy of the tz database, so the bot does not need
# this. It is here so that anything else running in the container -- a shell, date,
# the healthcheck -- reports the same wall clock the logs do.
RUN apk add --no-cache tzdata

WORKDIR /bot

COPY --from=build /build/bot /bot/mgbot
COPY --from=build /build/db/migrations/ /bot/db/migrations/
COPY --from=build /build/assets/ /bot/assets/

EXPOSE 8081

# /health returns 503 unless both Discord and the database are up, which wget
# surfaces as a non-zero exit. start-period covers migrations and the gateway
# handshake so a slow boot is not reported as a failure.
HEALTHCHECK --interval=30s --timeout=5s --start-period=90s --retries=3 \
    CMD wget --quiet --tries=1 --spider http://127.0.0.1:8081/health || exit 1

ENTRYPOINT ["/bot/mgbot"]

CMD ["-config", "/var/lib/config.toml"]